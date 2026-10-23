package stdlib_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

type workerParentInput struct{ reads atomic.Int32 }

func (r *workerParentInput) Read(p []byte) (int, error) {
	return r.ReadContext(context.Background(), p)
}
func (r *workerParentInput) ReadContext(ctx context.Context, _ []byte) (int, error) {
	r.reads.Add(1)
	return 0, io.EOF
}

// Preserve the former composition-layer nested Worker contract through the
// public managed entry, including each worker's docs, empty stdin and cleanup.
func TestManagedNestedWorkersOwnInitializationAndEmptyStdin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	input := new(workerParentInput)
	var output bytes.Buffer
	var initialized, cleaned atomic.Int32
	sources := map[string]string{
		"./child.mjs": `import {parentPort,Worker} from "node:worker_threads";import {stdin} from "noe:io";import {docs} from "noe:docs";
 if(!(await stdin.getReader().read()).done || !docs("noe:io"))throw Error("child extras");
 const w=new Worker("./grand.mjs",{type:"module"});const value=await new Promise((ok,fail)=>{w.once("message",ok);w.once("error",fail)});parentPort.postMessage("child:"+value);`,
		"./grand.mjs": `import {parentPort} from "node:worker_threads";import {stdin} from "noe:io";import {docs} from "noe:docs";
 if(!(await stdin.getReader().read()).done || !docs("noe:io"))throw Error("grandchild extras");parentPort.postMessage("grand");`,
	}
	profile := stdlib.Profile{Environment: stdlib.Environment{Stdin: input, Stdout: &output}, Modules: stdlib.Modules{Stdio: true, Workers: &stdlib.WorkerOptions{Max: -1}},
		Initialize: func(context.Context, *stdlib.Bindings) (func(), error) {
			initialized.Add(1)
			return func() { cleaned.Add(1) }, nil
		},
		NewLoader: func(context.Context, stdlib.Origin) (quickjs.ModuleLoader, error) {
			return func(specifier, referrer string) (string, string, error) {
				source, ok := sources[specifier]
				if !ok {
					return "", "", fmt.Errorf("missing worker %q", specifier)
				}
				return source, specifier, nil
			}, nil
		},
	}
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[nested workers]", Virtual: true, Module: true, Code: `import {Worker} from "node:worker_threads";const w=new Worker("./child.mjs",{type:"module"});console.log(await new Promise((ok,fail)=>{w.once("message",ok);w.once("error",fail)}));`}, profile)
	if err != nil || result.Code != 0 || output.String() != "child:grand\n" || initialized.Load() != 3 || cleaned.Load() != 3 || input.reads.Load() != 0 {
		t.Fatalf("result=%+v err=%v output=%q init=%d cleanup=%d input reads=%d", result, err, output.String(), initialized.Load(), cleaned.Load(), input.reads.Load())
	}
}

func TestWorkerInitializerFailureLeavesParentOutputUsable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var output bytes.Buffer
	var initialized, cleaned atomic.Int32
	profile := stdlib.Profile{Environment: stdlib.Environment{Stdout: &output}, Modules: stdlib.Modules{Stdio: true, Workers: &stdlib.WorkerOptions{}}, Initialize: func(_ context.Context, b *stdlib.Bindings) (func(), error) {
		initialized.Add(1)
		cleanup := func() { cleaned.Add(1) }
		if b.Worker() {
			return cleanup, errors.New("worker initializer sentinel")
		}
		return cleanup, nil
	}}
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[worker failure]", Virtual: true, Module: true, Code: `import {Worker} from "node:worker_threads";import {stdout} from "noe:io";
 const w=new Worker("throw Error('must not execute')",{eval:true,type:"module"});
 const message=await new Promise(ok=>w.once("error",e=>ok(e.message)));if(!message.includes("worker initializer sentinel"))throw Error(message);
 console.log("parent alive");const writer=stdout.getWriter();await writer.write(new TextEncoder().encode("native alive"));writer.releaseLock();`}, profile)
	if err != nil || result.Code != 0 || !strings.Contains(output.String(), "parent alive\nnative alive") || initialized.Load() != 2 || cleaned.Load() != 2 {
		t.Fatalf("result=%+v err=%v output=%q init=%d cleanup=%d", result, err, output.String(), initialized.Load(), cleaned.Load())
	}
	output.WriteString(";borrowed alive")
}

// Closing parentPort ends messaging, not the Worker. Neither its exit event
// nor the managed owner's return may precede its initializer cleanup.
func TestWorkerPortCloseWaitsForCleanup(t *testing.T) {
	for _, waitForExit := range []bool{false, true} {
		t.Run(fmt.Sprintf("waitForExit=%t", waitForExit), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			cleanupEntered, releaseCleanup, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			var cleaned atomic.Bool
			var result stdlib.Result
			var executionErr error
			profile := stdlib.Profile{
				Modules: stdlib.Modules{Workers: &stdlib.WorkerOptions{Max: 1}},
				Initialize: func(_ context.Context, b *stdlib.Bindings) (func(), error) {
					if b.Worker() {
						return func() { close(cleanupEntered); <-releaseCleanup; cleaned.Store(true) }, nil
					}
					return nil, b.Runtime().Set("cleanupCompleted", func() bool { return cleaned.Load() })
				},
			}
			code := `import {Worker} from "node:worker_threads";
const w=new Worker('import {parentPort} from "node:worker_threads";parentPort.postMessage(42);parentPort.close();',{eval:true,type:"module"});
`
			if waitForExit {
				code += `const values=[];w.on("message",v=>values.push(v));
const exit=await new Promise((ok,fail)=>{w.once("exit",ok);w.once("error",fail)});
if(!cleanupCompleted())throw Error("exit before cleanup");
if(exit!==0||values.length!==1||values[0]!==42)throw Error("messages or exit lost");`
			} else {
				code += `if(await new Promise((ok,fail)=>{w.once("message",ok);w.once("error",fail)})!==42)throw Error("message lost");`
			}
			go func() {
				result, executionErr = stdlib.Execute(ctx, stdlib.Source{Name: "[worker port close]", Virtual: true, Module: true, Code: code}, profile)
				close(done)
			}()
			defer func() { releaseOnce.Do(func() { close(releaseCleanup) }); cancel(); <-done }()
			select {
			case <-cleanupEntered:
			case <-done:
				t.Fatalf("execution ended without entering cleanup: result=%+v err=%v", result, executionErr)
			case <-ctx.Done():
				t.Fatal("worker did not enter cleanup")
			}
			select {
			case <-done:
				t.Fatalf("execution returned while Worker cleanup was blocked: result=%+v err=%v", result, executionErr)
			case <-time.After(50 * time.Millisecond):
			}
			releaseOnce.Do(func() { close(releaseCleanup) })
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("execution did not join the released Worker")
			}
			if executionErr != nil || result.Code != 0 || !cleaned.Load() {
				t.Fatalf("result=%+v err=%v cleaned=%t", result, executionErr, cleaned.Load())
			}
		})
	}
}
