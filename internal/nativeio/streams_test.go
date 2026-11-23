package nativeio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func streamRuntime(t *testing.T, opts Options) (*quickjs.Runtime, *Host, *stdlib.Loop) {
	t.Helper()
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	t.Cleanup(func() { _ = rt.Close() })
	if err := stdlib.Install(rt, stdlib.Config{Stdout: io.Discard, Stderr: io.Discard, Loop: loop}); err != nil {
		t.Fatal(err)
	}
	h, err := Install(rt, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := stdlib.ProcessModule(rt, stdlib.ProcessModuleOptions{Commands: &stdlib.Run{}, Spawn: h.Spawn(), Stdio: h.Stdio()}); err != nil {
		t.Fatal(err)
	}
	return rt, h, loop
}

func evalStreams(t *testing.T, rt *quickjs.Runtime, loop *stdlib.Loop, src string) quickjs.Value {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rt.SetModuleLoader(func(spec, ref string) (string, string, error) { return src, "streams-test", nil })
	v, err := rt.EvalContext(ctx, `import("streams-test")`)
	if err == nil {
		err = loop.Run(ctx)
	}
	if err == nil {
		v, err = v.Await(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	return v
}

type observedInput struct{ reads atomic.Int32 }

func (i *observedInput) Read([]byte) (int, error) { i.reads.Add(1); return 0, io.EOF }

func TestImportDoesNotReadOrKeepAlive(t *testing.T) {
	input := &observedInput{}
	rt, _, loop := streamRuntime(t, Options{Stdin: input})
	evalStreams(t, rt, loop, `import {stdin} from "noe:process"; if (!(stdin instanceof ReadableStream)) throw Error("stream");`)
	if input.reads.Load() != 0 || rt.Busy() {
		t.Fatalf("import activated input: reads=%d active=%v busy=%v", input.reads.Load(), rt.Busy(), rt.Busy())
	}
}

func TestPipeBinaryAndPreventClose(t *testing.T) {
	var out bytes.Buffer
	rt, _, loop := streamRuntime(t, Options{Stdin: bytes.NewReader([]byte{0, 255, 10, 128}), Stdout: &out})
	evalStreams(t, rt, loop, `import {stdin, stdout} from "noe:process";
await stdin.pipeTo(stdout, {preventClose:true});
const w=stdout.getWriter(); await w.write(new Uint8Array([42])); w.releaseLock();`)
	if !bytes.Equal(out.Bytes(), []byte{0, 255, 10, 128, 42}) {
		t.Fatalf("output=%v", out.Bytes())
	}
}

type waitingInput struct {
	started chan struct{}
	done    chan struct{}
}

func (i *waitingInput) Read([]byte) (int, error) { return 0, errors.New("synchronous read was used") }
func (i *waitingInput) ReadContext(ctx context.Context, _ []byte) (int, error) {
	close(i.started)
	<-ctx.Done()
	close(i.done)
	return 0, ctx.Err()
}

type waitingOutput struct {
	started   chan struct{}
	release   chan struct{}
	done      chan struct{}
	cancelled atomic.Bool
}

func (i *waitingOutput) Write([]byte) (int, error) {
	return 0, errors.New("synchronous write was used")
}
func (i *waitingOutput) WriteContext(ctx context.Context, chunk []byte) (int, error) {
	close(i.started)
	defer close(i.done)
	select {
	case <-i.release:
		return len(chunk), nil
	case <-ctx.Done():
		i.cancelled.Store(true)
		return 0, ctx.Err()
	}
}

func TestPipeAbortCancelsPendingRead(t *testing.T) {
	in := &waitingInput{make(chan struct{}), make(chan struct{})}
	rt, _, loop := streamRuntime(t, Options{Stdin: in})
	_ = rt.Set("started", func() bool {
		select {
		case <-in.started:
			return true
		default:
			return false
		}
	})
	evalStreams(t, rt, loop, `import {stdin,stdout} from "noe:process";
const c=new AbortController(), reason=new Error("stop read");
const p=stdin.pipeTo(stdout,{signal:c.signal});
const check=()=>{if(started())c.abort(reason);else setTimeout(check,0)}; check();
try { await p; throw Error("resolved"); } catch(e) { if(e!==reason) throw e; }
if(stdin.locked||stdout.locked) throw Error("pipe left streams locked");`)
	select {
	case <-in.done:
	case <-time.After(time.Second):
		t.Fatal("read did not stop")
	}
	if rt.Busy() {
		t.Fatal("native work remains")
	}
}

func TestPipeAbortDrainsPendingWrite(t *testing.T) {
	out := &waitingOutput{started: make(chan struct{}), release: make(chan struct{}), done: make(chan struct{})}
	rt, _, loop := streamRuntime(t, Options{Stdin: bytes.NewReader([]byte{1}), Stdout: out})
	_ = rt.Set("started", func() bool {
		select {
		case <-out.started:
			return true
		default:
			return false
		}
	})
	_ = rt.Set("finishWrite", func() { close(out.release) })
	evalStreams(t, rt, loop, `import {stdin,stdout} from "noe:process";
const c=new AbortController(), reason=new Error("stop write");
const p=stdin.pipeTo(stdout,{signal:c.signal});
await new Promise(resolve=>{ const check=()=>{if(started())resolve();else setTimeout(check,0)}; check(); });
c.abort(reason);
let settled=false;
p.then(()=>{settled=true;},()=>{settled=true;});
await Promise.resolve();
if(settled||!stdin.locked||!stdout.locked)throw Error("pipe finalized before pending write");
finishWrite();
try { await p; throw Error("resolved"); } catch(e) { if(e!==reason) throw e; }
if(stdin.locked||stdout.locked) throw Error("pipe left streams locked");`)
	select {
	case <-out.done:
	case <-time.After(time.Second):
		t.Fatal("write did not finish")
	}
	if out.cancelled.Load() || rt.Busy() {
		t.Fatalf("pending write was cancelled or native work remains: cancelled=%v busy=%v", out.cancelled.Load(), rt.Busy())
	}
}
