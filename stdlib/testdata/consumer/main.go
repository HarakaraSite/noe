// The root test copies this program outside the checkout and gives it a
// temporary go.mod that requires the public module with a local replace.
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "consumer-child" {
		if err := writeChildReport(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "consumer-fail" {
		fmt.Fprintln(os.Stderr, "expected fixture child failure")
		os.Exit(2)
	}
	if len(os.Args) > 1 && os.Args[1] == "consumer-wait" {
		time.Sleep(time.Minute)
		return
	}
	if err := constructPublicTypes(); err != nil {
		fail(err)
	}
	mode := "portable"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	var err error
	switch mode {
	case "portable":
		err = runPortable()
	case "reference":
		err = runReference()
	default:
		err = fmt.Errorf("unknown mode %q; use portable or reference", mode)
	}
	if err != nil {
		fail(err)
	}
}

func constructPublicTypes() error {
	// Compile each public alias and callback shape from outside the module.
	loop := (*stdlib.Loop)(nil)
	var loopContract interface {
		Post(func())
		Begin()
		Done()
		Pending() bool
		Close()
		Context() context.Context
		Run(context.Context) error
		RunUntil(context.Context, <-chan struct{}) error
	} = loop
	_ = loopContract
	var resolver stdlib.Resolver = net.DefaultResolver
	var reader stdlib.ContextReader = (*observedReader)(nil)
	var writer stdlib.ContextWriter = (*borrowedSink)(nil)
	var output stdlib.Output = (*borrowedSink)(nil)
	_ = reader
	_ = writer
	_ = output
	_ = stdlib.ExitCode

	config := stdlib.Config{
		Core: stdlib.CoreConfig{
			Stdout:  io.Discard,
			Stderr:  io.Discard,
			Loop:    loop,
			FS:      &stdlib.FS{Root: ".", ReadOnly: true, Loop: loop},
			Process: &stdlib.Process{Args: []string{"consumer"}, Env: map[string]string{"A": "b"}, Cwd: ".", Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard, Exit: func(int) {}, Version: "consumer", Unhandled: func(quickjs.Value) {}},
			OS:      &stdlib.OSInfo{Hostname: "host", Homedir: ".", Tmpdir: ".", Real: false},
			Fetch:   &stdlib.Fetch{Loop: loop, Client: http.DefaultClient, Allow: func(*http.Request) error { return nil }, MaxBodyBytes: 1},
			DNS:     &stdlib.DNS{Loop: loop, Resolver: resolver, Allow: func(string) error { return nil }, Servers: []string{"127.0.0.1"}, Timeout: time.Second},
			Run:     &stdlib.Run{Loop: loop, Allow: func(string, []string) error { return nil }, Dir: ".", Env: map[string]string{"A": "b"}, Timeout: time.Second, MaxOutputBytes: 1024},
			Serve:   &stdlib.Serve{Loop: loop, Allow: func(string) error { return nil }, MaxBodyBytes: 1024, ReadTimeout: time.Second, IdleTimeout: time.Second},
			Sockets: &stdlib.WebSockets{Loop: loop, Allow: func(*url.URL) error { return nil }, Serve: &stdlib.Serve{}, MaxMessageBytes: 1024, HandshakeTimeout: time.Second, TLS: &tls.Config{}},
			Workers: &stdlib.Workers{
				New:       func() (*quickjs.Runtime, error) { return quickjs.New(), nil },
				Installed: func(*quickjs.Runtime) error { return nil },
				Load:      func(string) (string, string, bool, error) { return "", "", false, nil },
				Max:       1,
			},
		},
		IO:   &stdlib.IOConfig{Stdin: strings.NewReader(""), Cwd: "."},
		Docs: true,
	}
	_ = config
	return nil
}

func runPortable() error {
	if err := checkDocsOnly(); err != nil {
		return err
	}
	rt := quickjs.New()
	input := &observedReader{}
	sink := &borrowedSink{}
	bindings, err := stdlib.Install(rt, stdlib.Config{
		Core: stdlib.CoreConfig{
			NoWebAPIs: true,
			Stdout:    sink,
			Stderr:    sink,
			Process:   &stdlib.Process{Stdin: input},
		},
		Docs: true,
	})
	if err != nil {
		_ = rt.Close()
		return fmt.Errorf("install portable config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ns, err := rt.EvalModuleContext(ctx, "consumer-docs.mjs", `
		import {docs} from "noe:docs";
		export const index = docs();
		export const io = docs("noe:io");
	`)
	if err != nil {
		_ = rt.Close()
		return fmt.Errorf("read public Docs catalog: %w", err)
	}
	index, err := ns.Get("index")
	if err != nil {
		_ = rt.Close()
		return err
	}
	docsIndex := index.String()
	if !strings.Contains(docsIndex, "`noe:agent`") || !strings.Contains(docsIndex, "`noe:io`") {
		_ = rt.Close()
		return errors.New("Docs did not return the full NOE catalog")
	}
	ioTopic, err := ns.Get("io")
	if err != nil || !strings.Contains(ioTopic.String(), "Workers receive empty `stdin`") {
		_ = rt.Close()
		return fmt.Errorf("Docs did not return the IO reference: %v", err)
	}

	for _, specifier := range []string{"noe:io", "noe:agent", "fs", "child_process"} {
		if _, err := rt.EvalModuleContext(ctx, "unavailable.mjs", `import "`+specifier+`"`); err == nil {
			_ = rt.Close()
			return fmt.Errorf("unconfigured module %q was importable", specifier)
		}
	}
	webAPIs, err := rt.EvalContext(ctx, `typeof URL === "undefined" && typeof TextEncoder === "undefined" && typeof fetch === "undefined"`)
	if err != nil || !webAPIs.Bool() {
		_ = rt.Close()
		return fmt.Errorf("NoWebAPIs configuration was not respected: %v", err)
	}
	if rt.Busy() {
		_ = rt.Close()
		return errors.New("portable partial install unexpectedly kept the runtime busy")
	}
	if input.reads.Load() != 0 {
		_ = rt.Close()
		return fmt.Errorf("install or Docs read stdin %d times", input.reads.Load())
	}
	if bindings.Stdout == nil || bindings.Stderr == nil {
		_ = rt.Close()
		return errors.New("Install did not return output bindings")
	}
	if _, err := bindings.Stdout.Write([]byte("ordinary ")); err != nil {
		_ = rt.Close()
		return fmt.Errorf("write through public Output: %w", err)
	}
	if _, err := bindings.Stdout.WriteContext(ctx, []byte("context ")); err != nil {
		_ = rt.Close()
		return fmt.Errorf("context write through public Output: %w", err)
	}
	if _, err := bindings.Stderr.Write([]byte("sink")); err != nil {
		_ = rt.Close()
		return fmt.Errorf("write through stderr Output: %w", err)
	}
	if err := rt.Close(); err != nil {
		return fmt.Errorf("close portable runtime: %w", err)
	}
	if sink.wasClosed() {
		return errors.New("runtime closed the caller-owned output sink")
	}
	if got := sink.String(); got != "ordinary context sink" {
		return fmt.Errorf("borrowed sink received %q", got)
	}
	return nil
}

func checkDocsOnly() error {
	rt := quickjs.New()
	defer rt.Close()
	if _, err := stdlib.Install(rt, stdlib.Config{
		Core: stdlib.CoreConfig{NoWebAPIs: true},
		Docs: true,
	}); err != nil {
		return fmt.Errorf("install Docs-only config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ns, err := rt.EvalModuleContext(ctx, "consumer-docs-only.mjs", `
		import {docs} from "noe:docs";
		export const catalog = docs();
	`)
	if err != nil {
		return fmt.Errorf("read Docs-only catalog: %w", err)
	}
	catalog, err := ns.Get("catalog")
	if err != nil || !strings.Contains(catalog.String(), "`noe:agent`") {
		return fmt.Errorf("Docs-only install did not expose the full catalog: %v", err)
	}
	for _, specifier := range []string{"noe:io", "noe:agent", "fs", "child_process"} {
		if _, err := rt.EvalModuleContext(ctx, "docs-only-unavailable.mjs", `import "`+specifier+`"`); err == nil {
			return fmt.Errorf("Docs-only install made %q importable", specifier)
		}
	}
	if rt.Busy() {
		return errors.New("Docs-only install unexpectedly kept the runtime busy")
	}
	return rt.Close()
}

func runReference() error {
	workDir, err := os.MkdirTemp("", "noe-stdlib-consumer-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	executable, err := os.Executable()
	if err != nil {
		return err
	}
	input := &observedReader{}
	sink := &borrowedSink{}
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	defer rt.Close()
	var installed atomic.Int32
	workers := &stdlib.Workers{
		New: func() (*quickjs.Runtime, error) { return quickjs.New(), nil },
		Installed: func(workerRT *quickjs.Runtime) error {
			installed.Add(1)
			_, err := workerRT.EvalModule("consumer-worker-install.mjs", `
				import {docs} from "noe:docs";
				import {stdin} from "noe:io";
				if (typeof docs !== "function" || !(stdin instanceof ReadableStream)) throw Error("worker extras missing");
			`)
			return err
		},
		Load: func(specifier string) (string, string, bool, error) {
			sources := map[string]string{
				"./child.mjs": `
					import {parentPort, Worker} from "node:worker_threads";
					import {stdin} from "noe:io";
					const chunk = await stdin.getReader().read();
					if (!chunk.done || process.stdin.read() !== null) throw Error("child received stdin");
					const grand = new Worker("./grand.mjs");
					grand.on("message", value => { parentPort.postMessage("child:" + value); parentPort.close(); });
					grand.on("error", error => { parentPort.postMessage("child-error:" + error.message); parentPort.close(); });
				`,
				"./grand.mjs": `
					import {parentPort} from "node:worker_threads";
					import {stdin} from "noe:io";
					const chunk = await stdin.getReader().read();
					const empty = chunk.done && process.stdin.read() === null;
					parentPort.postMessage(empty ? "grandchild:empty" : "grandchild:stdin-leak");
					parentPort.close();
				`,
			}
			source, ok := sources[specifier]
			if !ok {
				return "", "", false, fmt.Errorf("missing worker source %q", specifier)
			}
			return source, specifier, true, nil
		},
		Max: 4,
	}
	process := &stdlib.Process{
		Args:   []string{executable, "consumer-reference"},
		Env:    map[string]string{"NOE_CONSUMER_PROCESS_ENV": "root"},
		Cwd:    workDir,
		Stdin:  input,
		Stdout: sink,
		Stderr: sink,
	}
	fetch := &stdlib.Fetch{
		Loop: loop,
		Allow: func(request *http.Request) error {
			if request.URL.Hostname() != "127.0.0.1" {
				return fmt.Errorf("refused non-loopback HTTP target %q", request.URL.Host)
			}
			return nil
		},
	}
	serve := &stdlib.Serve{
		Loop: loop,
		Allow: func(address string) error {
			if !strings.HasPrefix(address, "127.0.0.1:") {
				return fmt.Errorf("refused non-loopback listener %q", address)
			}
			return nil
		},
	}
	_, err = stdlib.Install(rt, stdlib.Config{
		Core: stdlib.CoreConfig{
			Stdout:  sink,
			Stderr:  sink,
			Loop:    loop,
			FS:      &stdlib.FS{Root: workDir, Loop: loop},
			Process: process,
			Fetch:   fetch,
			Serve:   serve,
			Workers: workers,
		},
		IO:   &stdlib.IOConfig{Stdin: input, Cwd: workDir},
		Docs: true,
	})
	if err != nil {
		return fmt.Errorf("install reference config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := rt.EvalModuleContext(ctx, "consumer-reference.mjs", `
		import {Worker} from "node:worker_threads";
		import {fetch, serve} from "noe:http";
		import {writeFileSync, readFileSync} from "node:fs";
		import {stdout} from "noe:io";
		import {spawn} from "noe:process";

		const readText = async stream => {
			const reader = stream.getReader();
			const chunks = [];
			for (;;) {
				const {done, value} = await reader.read();
				if (done) break;
				chunks.push(new TextDecoder().decode(value));
			}
			return chunks.join("");
		};

		writeFileSync("facade.txt", "public facade");
		const file = readFileSync("facade.txt", "utf8");
		const nativeWriter = stdout.getWriter();
		await nativeWriter.write(new TextEncoder().encode("native-io"));
		nativeWriter.releaseLock();
		const childProcess = await spawn(process.argv[0], ["consumer-child"], {stdin:"ignore", stdout:"pipe", stderr:"pipe"});
		const childOutput = readText(childProcess.stdout);
		const childStatus = await childProcess.exited;
		const childError = await readText(childProcess.stderr);
		await childProcess.close();
		if (childStatus.code !== 0) throw Error("child process exited " + childStatus.code + ": " + childError);
		const child = JSON.parse(await childOutput);

		const failedProcess = await spawn(process.argv[0], ["consumer-fail"], {stdin:"ignore", stdout:"ignore", stderr:"pipe"});
		const failedError = readText(failedProcess.stderr);
		const failedStatus = await failedProcess.exited;
		const failureMessage = await failedError;
		await failedProcess.close();
		if (failedStatus.code !== 2 || !failureMessage.includes("expected fixture child failure")) throw Error("child failure was not observed");

		const server = serve({hostname:"127.0.0.1", port:0}, request => new Response("loopback:" + new URL(request.url).pathname));
		const httpText = fetch(server.url + "/consumer").then(response => response.text());
		const workerText = new Promise((resolve, reject) => {
			let message;
			const worker = new Worker("./child.mjs");
			worker.on("message", value => { message = value; });
			worker.on("error", reject);
			worker.on("exit", code => code === 0 && message ? resolve(message) : reject(Error("worker exit " + code + ": " + message)));
		});
		Promise.all([httpText, workerText]).then(([http, worker]) => {
			globalThis.consumerResult = {file, child, http, worker};
		}, error => {
			globalThis.consumerError = error && (error.stack || error.message) || String(error);
		}).finally(() => server.close());
	`); err != nil {
		return fmt.Errorf("evaluate reference consumer: %w", err)
	}
	if err := loop.Run(ctx); err != nil {
		return fmt.Errorf("run reference consumer: %w", err)
	}
	value, err := rt.Eval(`JSON.stringify({result: globalThis.consumerResult, error: globalThis.consumerError})`)
	if err != nil {
		return err
	}
	var got struct {
		Result struct {
			File   string      `json:"file"`
			Child  childReport `json:"child"`
			HTTP   string      `json:"http"`
			Worker string      `json:"worker"`
		} `json:"result"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(value.String()), &got); err != nil {
		return fmt.Errorf("decode reference result %q: %w", value.String(), err)
	}
	if got.Error != "" {
		return fmt.Errorf("reference JS flow failed: %s", got.Error)
	}
	if got.Result.File != "public facade" {
		return fmt.Errorf("FS returned %q", got.Result.File)
	}
	if got.Result.Child.Cwd != workDir || got.Result.Child.Env != "root" {
		return fmt.Errorf("child process environment = %#v, want cwd %q and process env", got.Result.Child, workDir)
	}
	if got.Result.HTTP != "loopback:/consumer" {
		return fmt.Errorf("loopback HTTP response = %q", got.Result.HTTP)
	}
	if got.Result.Worker != "child:grandchild:empty" {
		return fmt.Errorf("worker chain result = %q", got.Result.Worker)
	}
	if n := installed.Load(); n != 2 {
		return fmt.Errorf("Workers.Installed ran %d times, want once for child and grandchild", n)
	}
	if n := input.reads.Load(); n != 0 {
		return fmt.Errorf("root stdin was read while workers ran: %d reads", n)
	}
	if loop.Pending() {
		return errors.New("loop returned with pending host work")
	}
	if err := rt.Close(); err != nil {
		return fmt.Errorf("close reference runtime: %w", err)
	}
	if sink.wasClosed() {
		return errors.New("runtime closed the caller-owned output sink")
	}
	if _, err := sink.Write([]byte(" sink-still-open")); err != nil {
		return fmt.Errorf("caller sink is unusable after runtime close: %w", err)
	}
	if !strings.Contains(sink.String(), "native-io") {
		return fmt.Errorf("native IO did not write to the borrowed output: %q", sink.String())
	}
	if err := runCancellation(executable); err != nil {
		return err
	}
	return nil
}

func runCancellation(executable string) error {
	workDir, err := os.MkdirTemp("", "noe-stdlib-cancel-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	sink := &borrowedSink{}
	if _, err := stdlib.Install(rt, stdlib.Config{
		Core: stdlib.CoreConfig{
			Stdout: sink,
			Stderr: sink,
			Loop:   loop,
			Process: &stdlib.Process{
				Args:   []string{executable, "consumer-cancel"},
				Cwd:    workDir,
				Stdout: sink,
				Stderr: sink,
			},
		},
		IO: &stdlib.IOConfig{Cwd: workDir},
	}); err != nil {
		_ = rt.Close()
		return fmt.Errorf("install cancellation config: %w", err)
	}
	ready := make(chan struct{})
	if err := rt.Set("consumerChildReady", func() { close(ready) }); err != nil {
		_ = rt.Close()
		return fmt.Errorf("register child readiness callback: %w", err)
	}
	if _, err := rt.EvalModule("consumer-cancel.mjs", `
		import {spawn} from "noe:process";
		spawn(process.argv[0], ["consumer-wait"], {stdin:"ignore", stdout:"ignore", stderr:"ignore"}).then(child => {
			globalThis.consumerChildStarted = true;
			globalThis.consumerChildPID = child.pid;
			globalThis.consumerChild = child;
			consumerChildReady();
		}, error => {
			globalThis.consumerChildError = error.message;
			consumerChildReady();
		});
	`); err != nil {
		_ = rt.Close()
		return fmt.Errorf("start pending child: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() {
		select {
		case <-ready:
			cancel()
		case <-ctx.Done():
		}
	}()
	runErr := loop.Run(ctx)
	if !errors.Is(runErr, context.Canceled) {
		_ = rt.Close()
		return fmt.Errorf("Loop.Run after context cancellation = %v, want context.Canceled", runErr)
	}
	started, err := rt.Eval(`globalThis.consumerChildStarted === true`)
	if err != nil || !started.Bool() {
		_ = rt.Close()
		return fmt.Errorf("pending child did not start before cancellation: %v", err)
	}
	pidValue, err := rt.Eval(`globalThis.consumerChildPID`)
	if err != nil {
		_ = rt.Close()
		return fmt.Errorf("read pending child PID: %w", err)
	}
	pid := pidValue.Int()
	if err := rt.Close(); err != nil {
		return fmt.Errorf("close runtime with a pending child: %w", err)
	}
	if runtime.GOOS == "linux" && processIsAlive(pid) {
		return fmt.Errorf("runtime.Close returned while owned child process %d was still alive", pid)
	}
	if sink.wasClosed() {
		return errors.New("runtime close after cancellation closed the caller sink")
	}
	if _, err := sink.Write([]byte("still-open")); err != nil {
		return fmt.Errorf("caller sink is unusable after cancelled runtime close: %w", err)
	}
	return nil
}

type childReport struct {
	Cwd string `json:"cwd"`
	Env string `json:"env"`
}

func writeChildReport() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(childReport{Cwd: cwd, Env: os.Getenv("NOE_CONSUMER_PROCESS_ENV")})
}

type observedReader struct{ reads atomic.Int32 }

func (r *observedReader) Read([]byte) (int, error) {
	r.reads.Add(1)
	return 0, io.EOF
}

func (r *observedReader) ReadContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return r.Read(p)
}

type borrowedSink struct {
	mu     sync.Mutex
	data   strings.Builder
	closed bool
}

func (s *borrowedSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("caller sink is closed")
	}
	return s.data.Write(p)
}

func (s *borrowedSink) WriteContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return s.Write(p)
}

func (s *borrowedSink) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

func (s *borrowedSink) wasClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *borrowedSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.String()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
