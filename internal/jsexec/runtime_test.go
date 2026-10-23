package jsexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	lower "forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestManagedExecutionDrivesPromisesExitAndDetachedDiagnostics(t *testing.T) {
	cases := []struct {
		name, code string
		status     int
		failure    string
	}{
		{"timer", `await new Promise(r=>setTimeout(r,2));console.log("done");`, 0, ""},
		{"exit", `process.exit(7);throw new Error("continued after exit");`, 7, ""},
		{"exit-code", `process.exitCode=4;`, 4, ""},
		{"root-reject-with-interval", `setInterval(()=>{},1000);await Promise.reject(new Error("root-failure"));`, 1, "root-failure"},
		{"unhandled", `Promise.reject(new Error("unhandled-failure"));setInterval(()=>{},1000);`, 1, "unhandled-failure"},
		{"pending", `await new Promise(()=>{});`, 1, "nothing is left"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var output bytes.Buffer
			result, err := Execute(ctx, Source{Name: "memory:" + tc.name, Module: true, Virtual: true, Code: tc.code}, Profile{Environment: Environment{Stdout: &output}, Modules: Modules{ProcessInfo: true}})
			if result.Code != tc.status {
				t.Fatalf("code=%d err=%v", result.Code, err)
			}
			if tc.failure == "" && err != nil {
				t.Fatal(err)
			}
			if tc.failure != "" && (err == nil || !strings.Contains(Diagnostic(err), tc.failure)) {
				t.Fatalf("diagnostic=%q error=%v", Diagnostic(err), err)
			}
			if errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("execution waited for timeout: %v", err)
			}
			if tc.name == "timer" && output.String() != "done\n" {
				t.Fatalf("output=%q", &output)
			}
		})
	}
}

func TestManagedRuntimeCancelCPUAndSingleEntry(t *testing.T) {
	entered := make(chan struct{})
	runtime, err := NewRuntime(context.Background(), Profile{Initialize: func(ctx context.Context, b *Bindings) (func(), error) {
		return nil, b.Runtime().Set("started", func() { close(entered) })
	}})
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-entered:
			runtime.Cancel()
		case <-time.After(3 * time.Second):
			runtime.Cancel()
		}
	}()
	_, err = runtime.Run(Source{Name: "memory:cpu", Virtual: true, Code: `started();for(;;){}`})
	<-stopped
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CPU run error=%v", err)
	}
	if _, err := runtime.Run(Source{Name: "second", Code: `1`}); err == nil {
		t.Fatal("second entry was accepted")
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

type heldOutput struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	closed  atomic.Int32
}

func (w *heldOutput) Write(p []byte) (int, error) {
	return 0, errors.New("uncancellable Write was used")
}
func (w *heldOutput) WriteContext(ctx context.Context, p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-w.release:
		return len(p), nil
	}
}
func (w *heldOutput) Close() error { w.closed.Add(1); return nil }

type heldInput struct {
	entered chan struct{}
	once    sync.Once
	closed  atomic.Int32
}

func (r *heldInput) Read([]byte) (int, error) { return 0, errors.New("uncancellable Read was used") }
func (r *heldInput) ReadContext(ctx context.Context, p []byte) (int, error) {
	r.once.Do(func() { close(r.entered) })
	<-ctx.Done()
	return 0, ctx.Err()
}
func (r *heldInput) Close() error { r.closed.Add(1); return nil }

func TestManagedCancelStopsSynchronousAndNativeStdioWithoutClosingBorrowedStreams(t *testing.T) {
	for name, source := range map[string]string{
		"console":        `console.log("blocked");`,
		"process-output": `process.stdout.write("blocked");`,
		"native-output":  `import {stdout} from "noe:io";const w=stdout.getWriter();await w.write(new Uint8Array([1]));`,
		"process-input":  `process.stdin.read();`,
		"native-input":   `import {stdin} from "noe:io";await stdin.getReader().read();`,
	} {
		t.Run(name, func(t *testing.T) {
			writer := &heldOutput{entered: make(chan struct{}), release: make(chan struct{})}
			reader := &heldInput{entered: make(chan struct{})}
			runtime, err := NewRuntime(context.Background(), Profile{Environment: Environment{Stdin: reader, Stdout: writer}, Modules: Modules{Stdio: true, ProcessInfo: true}})
			if err != nil {
				t.Fatal(err)
			}
			enter := writer.entered
			if strings.Contains(name, "input") {
				enter = reader.entered
			}
			cancelled := make(chan bool, 1)
			go func() {
				select {
				case <-enter:
					cancelled <- true
					runtime.Cancel()
				case <-time.After(3 * time.Second):
					cancelled <- false
					runtime.Cancel()
				}
			}()
			_, err = runtime.Run(Source{Name: "memory:" + name, Code: source, Module: true, Virtual: true})
			if !<-cancelled {
				t.Fatal("I/O did not enter its context-aware method")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("I/O cancellation error=%v", err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
			if reader.closed.Load() != 0 || writer.closed.Load() != 0 {
				t.Fatal("borrowed source/sink was closed")
			}
			close(writer.release)
			if n, err := writer.WriteContext(context.Background(), []byte("still usable")); err != nil || n != 12 {
				t.Fatalf("borrowed sink after Close=%d %v", n, err)
			}
		})
	}
}

type failedOutput struct{ err error }

func (w failedOutput) Write(p []byte) (int, error)                       { return 0, w.err }
func (w failedOutput) WriteContext(context.Context, []byte) (int, error) { return 0, w.err }
func TestConsoleIOFailureSurvivesIgnoredWriteAndStopsDrive(t *testing.T) {
	failure := errors.New("fixture sink failed")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result, err := Execute(ctx, Source{Name: "memory:io-failure", Module: true, Virtual: true, Code: `console.log("lost");setInterval(()=>{},1000);`}, Profile{Environment: Environment{Stdout: failedOutput{failure}}})
	if result.Code != 1 || !errors.Is(err, failure) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestManagedFreshLoaderWorkerInitializerAndVirtualImports(t *testing.T) {
	cwd := t.TempDir()
	var factories, roots, workers, cleanups atomic.Int32
	profile := Profile{
		Environment: Environment{Cwd: cwd, Stdin: strings.NewReader("parent-only")},
		Modules:     Modules{Workers: &WorkerOptions{}, Stdio: true},
		Initialize: func(ctx context.Context, b *Bindings) (func(), error) {
			if b.Worker() {
				workers.Add(1)
			} else {
				roots.Add(1)
			}
			return func() { cleanups.Add(1) }, nil
		},
		NewLoader: func(ctx context.Context, origin Origin) (quickjs.ModuleLoader, error) {
			factories.Add(1)
			return func(specifier, referrer string) (string, string, error) {
				switch specifier {
				case "./worker-entry.mjs":
					return `import {parentPort} from "node:worker_threads";import {stdin} from "noe:io";import {value} from "./dep.mjs";if(!(await stdin.getReader().read()).done)throw new Error("worker inherited stdin");parentPort.postMessage(value);`, "memory:worker-entry", nil
				case "./dep.mjs":
					// The virtual root uses cwd, while a loaded Worker module keeps its ID.
					if referrer != "memory:worker-entry" && filepath.Dir(referrer) != cwd {
						return "", "", fmt.Errorf("unexpected import referrer %q", referrer)
					}
					return `globalThis.moduleLoads=(globalThis.moduleLoads||0)+1;export const value=moduleLoads;`, "memory:dep", nil
				}
				return "", "", fmt.Errorf("unknown module %q", specifier)
			}, nil
		},
	}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		result, err := Execute(ctx, Source{Name: "memory:root", Virtual: true, Module: true, Code: `import {Worker} from "node:worker_threads";import {value} from "./dep.mjs";if(value!==1)throw new Error("shared module cache");const w=new Worker("./worker-entry.mjs",{type:"module"});const result=await new Promise((resolve,reject)=>{w.once("message",resolve);w.once("error",reject)});if(result!==1)throw new Error("worker module cache");`}, profile)
		cancel()
		if err != nil || result.Code != 0 {
			t.Fatalf("execution %d result=%#v error=%s", i, result, Diagnostic(err))
		}
	}
	if factories.Load() != 4 || roots.Load() != 2 || workers.Load() != 2 || cleanups.Load() != 4 {
		t.Fatalf("factory/root/worker/cleanup=%d/%d/%d/%d", factories.Load(), roots.Load(), workers.Load(), cleanups.Load())
	}
}

func TestInitializerFailureCleansPartialBindingAndBorrowedRuntimeSurvives(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := lower.NewLoop(rt)
	defer loop.Close()
	var cleaned bool
	failure := errors.New("initializer failed")
	_, err := Attach(rt, loop, Profile{Initialize: func(ctx context.Context, b *Bindings) (func(), error) { return func() { cleaned = true }, failure }})
	if !errors.Is(err, failure) || !cleaned {
		t.Fatalf("Attach error=%v cleanup=%v", err, cleaned)
	}
	if loop.Context().Err() != nil {
		t.Fatal("borrowed Loop was closed")
	}
	value, err := rt.Eval(`21*2`)
	if err != nil || value.Int() != 42 {
		t.Fatalf("borrowed VM after failed Attach=%v %v", value, err)
	}
}

func TestManagedFullPipeCancelLeavesOriginalFileUsable(t *testing.T) {
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()
	runtime, err := NewRuntime(context.Background(), Profile{Environment: Environment{Stdout: output}})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { buf := make([]byte, 1); _, err := input.Read(buf); runtime.Cancel(); done <- err }()
	_, err = runtime.Run(Source{Name: "memory:pipe", Virtual: true, Code: `console.log("x".repeat(8*1024*1024));`})
	if readErr := <-done; readErr != nil {
		t.Fatal(readErr)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("pipe cancellation error=%v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Stat(); err != nil {
		t.Fatalf("borrowed output closed: %v", err)
	}
	drained := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, input); close(drained) }()
	if _, err := output.Write([]byte("still-open")); err != nil {
		t.Fatalf("original output unusable: %v", err)
	}
	_ = output.Close()
	<-drained
}

func TestAttachCloseCancelsFetchBeforeDeliveryAndPreservesCallerLoop(t *testing.T) {
	started, ended := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/after" {
			fmt.Fprint(w, "usable")
			return
		}
		close(started)
		<-r.Context().Done()
		close(ended)
	}))
	defer server.Close()
	rt := quickjs.New()
	defer rt.Close()
	loop := lower.NewLoop(rt)
	defer loop.Close()
	view, err := Attach(rt, loop, Profile{Modules: Modules{Fetch: &FetchOptions{Client: server.Client()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	code := fmt.Sprintf(`globalThis.fetchDelivered=false;fetch(%q).then(()=>fetchDelivered=true,()=>fetchDelivered=true);`, server.URL)
	if _, err := rt.Eval(code); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("fetch did not start")
	}
	if err := view.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("fetch request survived Attach close")
	}
	callerRan := false
	loop.Post(func() { callerRan = true })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	delivered, err := rt.Get("fetchDelivered")
	if err != nil || delivered.Bool() || !callerRan || loop.Context().Err() != nil {
		t.Fatalf("closed fetch delivered=%v callerRan=%v loop=%v error=%v", delivered, callerRan, loop.Context().Err(), err)
	}
	response, err := server.Client().Get(server.URL + "/after")
	if err != nil {
		t.Fatalf("borrowed HTTP client after close: %v", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || string(data) != "usable" {
		t.Fatalf("borrowed HTTP response=%q %v", data, err)
	}
}
