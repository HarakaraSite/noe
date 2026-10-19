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
	evalStreams(t, rt, loop, `import {stdin} from "noe:io"; if (!(stdin instanceof ReadableStream)) throw Error("stream");`)
	if input.reads.Load() != 0 || rt.Busy() {
		t.Fatalf("import activated input: reads=%d active=%v busy=%v", input.reads.Load(), rt.Busy(), rt.Busy())
	}
}

func TestPipeBinaryAndPreventClose(t *testing.T) {
	var out bytes.Buffer
	rt, _, loop := streamRuntime(t, Options{Stdin: bytes.NewReader([]byte{0, 255, 10, 128}), Stdout: &out})
	evalStreams(t, rt, loop, `import {stdin, stdout, pipe} from "noe:io";
await pipe(stdin, stdout, {preventClose:true});
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
	started chan struct{}
	done    chan struct{}
}

func (i *waitingOutput) Write([]byte) (int, error) {
	return 0, errors.New("synchronous write was used")
}
func (i *waitingOutput) WriteContext(ctx context.Context, _ []byte) (int, error) {
	close(i.started)
	<-ctx.Done()
	close(i.done)
	return 0, ctx.Err()
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
	evalStreams(t, rt, loop, `import {stdin,stdout,pipe} from "noe:io";
const c=new AbortController(), reason=new Error("stop read");
const p=pipe(stdin,stdout,{signal:c.signal});
const check=()=>{if(started())c.abort(reason);else setTimeout(check,0)}; check();
try { await p; throw Error("resolved"); } catch(e) { if(e!==reason) throw e; }`)
	select {
	case <-in.done:
	case <-time.After(time.Second):
		t.Fatal("read did not stop")
	}
	if rt.Busy() {
		t.Fatal("native work remains")
	}
}

func TestPipeAbortCancelsPendingWrite(t *testing.T) {
	out := &waitingOutput{make(chan struct{}), make(chan struct{})}
	rt, _, loop := streamRuntime(t, Options{Stdin: bytes.NewReader([]byte{1}), Stdout: out})
	_ = rt.Set("started", func() bool {
		select {
		case <-out.started:
			return true
		default:
			return false
		}
	})
	evalStreams(t, rt, loop, `import {stdin,stdout,pipe} from "noe:io";
const c=new AbortController(), reason=new Error("stop write");
const p=pipe(stdin,stdout,{signal:c.signal});
const check=()=>{if(started())c.abort(reason);else setTimeout(check,0)}; check();
try { await p; throw Error("resolved"); } catch(e) { if(e!==reason) throw e; }`)
	select {
	case <-out.done:
	case <-time.After(time.Second):
		t.Fatal("write did not stop")
	}
}
