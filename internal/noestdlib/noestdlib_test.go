package noestdlib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"noe/internal/hostio"
	"noe/internal/stdlib"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestValidationHappensBeforeRegistration(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{name: "io needs process", cfg: Config{IO: &IOConfig{}, Docs: true}},
		{name: "io needs web apis", cfg: Config{Core: stdlib.Config{Process: &stdlib.Process{}, NoWebAPIs: true}, IO: &IOConfig{}}},
		{name: "workers need loop", cfg: Config{Core: stdlib.Config{Workers: &stdlib.Workers{}}}},
		{name: "workers need web apis", cfg: Config{Core: stdlib.Config{Loop: &stdlib.Loop{}, Workers: &stdlib.Workers{}, NoWebAPIs: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := quickjs.New()
			defer rt.Close()
			if _, err := Install(rt, tc.cfg); err == nil {
				t.Fatal("Install succeeded for an invalid configuration")
			}
			if _, err := rt.EvalModule("probe.mjs", `import "noe:docs"`); err == nil {
				t.Fatal("validation registered docs before rejecting the configuration")
			}
			for _, name := range []string{"noe:io", "noe:process", "noe:fs-handle", "noe:sockets", "noe:signals"} {
				src := `import "` + name + `"`
				if _, err := rt.EvalModule("probe.mjs", src); err == nil {
					t.Errorf("validation registered %s before rejecting the configuration", name)
				}
			}
		})
	}
}

func TestDocsAreIndependentFromIOAndProcess(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	input := &observedInput{}
	if _, err := Install(rt, Config{Core: stdlib.Config{
		NoWebAPIs: true,
		Process:   &stdlib.Process{Stdin: input},
	}, Docs: true}); err != nil {
		t.Fatal(err)
	}
	ns, err := rt.EvalModule("docs-test.mjs", `
		import {docs} from "noe:docs";
		export const text = docs("noe:io");
		export const typeError = (() => { try { docs(1); } catch (e) { return e.name; } })();
		export const missing = (() => { try { docs("no-such-topic"); } catch (e) { return e.code; } })();
	`)
	if err != nil {
		t.Fatal(err)
	}
	text, err := ns.Get("text")
	if err != nil || !strings.Contains(text.String(), "host `stdin`, `stdout`, and `stderr`") {
		t.Fatalf("docs returned %q, %v", text.String(), err)
	}
	typeError, _ := ns.Get("typeError")
	if typeError.String() != "TypeError" {
		t.Errorf("docs argument error = %q, want TypeError", typeError.String())
	}
	missing, _ := ns.Get("missing")
	if missing.String() != "ERR_DOC_NOT_FOUND" {
		t.Errorf("unknown docs code = %q, want ERR_DOC_NOT_FOUND", missing.String())
	}
	if _, err := rt.EvalModule("no-io.mjs", `import "noe:io"`); err == nil {
		t.Error("Docs-only installation registered native I/O")
	}
	if reads := input.reads.Load(); reads != 0 || rt.Busy() {
		t.Errorf("Docs-only import started I/O: reads=%d busy=%v", reads, rt.Busy())
	}
}

func TestInstallClonesSettingsAndCanReuseConfiguration(t *testing.T) {
	shared := &bytes.Buffer{}
	process := &stdlib.Process{Args: []string{"qjs", "main.js"}, Env: map[string]string{"A": "one"}}
	runEnv := map[string]string{"B": "two"}
	dnsServers := []string{"192.0.2.53"}
	cfg := Config{Core: stdlib.Config{
		Stdout:  shared,
		Process: process,
		FS:      &stdlib.FS{Root: "."},
		Fetch:   &stdlib.Fetch{},
		DNS:     &stdlib.DNS{Servers: dnsServers},
		Run:     &stdlib.Run{Env: runEnv},
		Serve:   &stdlib.Serve{MaxBodyBytes: 1024},
	}}

	for range 2 {
		rt := quickjs.New()
		if _, err := Install(rt, cfg); err != nil {
			t.Fatalf("Install on a fresh runtime: %v", err)
		}
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
	}

	if process.Stdout != nil || process.Stderr != nil || process.Env["A"] != "one" || len(process.Args) != 2 {
		t.Fatalf("caller Process was modified: %#v", process)
	}
	if cfg.Core.FS.Loop != nil || cfg.Core.Fetch.Loop != nil || cfg.Core.DNS.Loop != nil || cfg.Core.Run.Loop != nil || cfg.Core.Serve.Loop != nil {
		t.Fatal("Install wrote default loops into caller configuration")
	}
	if runEnv["B"] != "two" || dnsServers[0] != "192.0.2.53" {
		t.Fatal("Install modified caller-owned map or slice settings")
	}
}

func TestCloneConfigKeepsServeAliasAndCopiesSeparateServe(t *testing.T) {
	rootServe := &stdlib.Serve{MaxBodyBytes: 1}
	separateServe := &stdlib.Serve{MaxBodyBytes: 2}
	core := stdlib.Config{Serve: rootServe, Sockets: &stdlib.WebSockets{Serve: rootServe}}
	cloned := cloneConfig(core)
	if cloned.Serve == rootServe || cloned.Sockets.Serve != cloned.Serve {
		t.Fatal("WebSockets.Serve did not keep the copied Core.Serve alias")
	}
	core.Sockets.Serve = separateServe
	cloned = cloneConfig(core)
	if cloned.Sockets.Serve == separateServe || cloned.Sockets.Serve == cloned.Serve {
		t.Fatal("explicit separate WebSockets.Serve was not copied independently")
	}
}

type uncomparableWriter []byte

func (w uncomparableWriter) Write(p []byte) (int, error) {
	return len(append(w, p...)), nil
}

type contextWriter struct {
	calls int
	bytes.Buffer
}

func (w *contextWriter) WriteContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	w.calls++
	return w.Buffer.Write(p)
}

func TestOutputNormalizationAliasesAndBorrowsWriters(t *testing.T) {
	t.Run("comparable aliases share one facade", func(t *testing.T) {
		sink := &bytes.Buffer{}
		rt := quickjs.New()
		bindings, err := Install(rt, Config{Core: stdlib.Config{
			Stdout: sink, Stderr: sink, Process: &stdlib.Process{Stdout: sink, Stderr: sink},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if bindings.Stdout != bindings.Stderr {
			t.Fatal("same comparable output sink got separate facades")
		}
		if _, err := rt.Eval(`console.log("console"); process.stdout.write("process")`); err != nil {
			t.Fatal(err)
		}
		if got := sink.String(); got != "console\nprocess" {
			t.Fatalf("output = %q", got)
		}
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := bindings.Stdout.Write([]byte("after close")); !errors.Is(err, hostio.ErrClosed) {
			t.Errorf("owned facade write after runtime close: %v", err)
		}
		if got := sink.String(); got != "console\nprocess" {
			t.Errorf("borrowed sink changed after close: %q", got)
		}
	})

	t.Run("non-comparable writer does not panic", func(t *testing.T) {
		var sink uncomparableWriter = make([]byte, 0, 64)
		rt := quickjs.New()
		_, err := Install(rt, Config{Core: stdlib.Config{
			Stdout: sink, Process: &stdlib.Process{Stdout: sink},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rt.Eval(`process.stdout.write("ok")`); err != nil {
			t.Fatal(err)
		}
		rt.Close()
	})

	t.Run("context writer is preserved", func(t *testing.T) {
		sink := &contextWriter{}
		rt := quickjs.New()
		bindings, err := Install(rt, Config{Core: stdlib.Config{Stdout: sink}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bindings.Stdout.WriteContext(context.Background(), []byte("ctx")); err != nil {
			t.Fatal(err)
		}
		if sink.calls != 1 || sink.String() != "ctx" {
			t.Fatalf("context write calls=%d output=%q", sink.calls, sink.String())
		}
		rt.Close()
	})
}

func TestExistingSharedWriterIsBorrowed(t *testing.T) {
	sink := &bytes.Buffer{}
	original := hostio.NewSharedWriter(sink)
	rt := quickjs.New()
	bindings, err := Install(rt, Config{Core: stdlib.Config{Stdout: original}})
	if err != nil {
		t.Fatal(err)
	}
	if bindings.Stdout == original {
		t.Fatal("Install returned the caller's SharedWriter instead of an owned facade")
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := original.Write([]byte("still open")); err != nil {
		t.Fatalf("caller SharedWriter was closed: %v", err)
	}
	if got := sink.String(); got != "still open" {
		t.Fatalf("borrowed output = %q", got)
	}
	_ = original.Close()
}

type observedInput struct{ reads atomic.Int32 }

func (i *observedInput) Read([]byte) (int, error) {
	i.reads.Add(1)
	return 0, io.EOF
}

func TestWorkerAndGrandchildGetExtrasAndNoStdin(t *testing.T) {
	input := &observedInput{}
	var output bytes.Buffer
	var installed atomic.Int32
	loopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	defer rt.Close()
	core := stdlib.Config{
		Stdout:  &output,
		Loop:    loop,
		Process: &stdlib.Process{},
		Workers: &stdlib.Workers{
			Max: -1,
			Load: func(specifier string) (string, string, bool, error) {
				sources := map[string]string{
					"./child.mjs": `
						import {parentPort, Worker} from "node:worker_threads";
						import {stdin} from "noe:io";
						const input = await stdin.getReader().read();
						if (!input.done) throw Error("worker inherited stdin");
						const child = new Worker("./grand.mjs");
						child.on("message", value => parentPort.postMessage("child:" + value));
					`,
					"./grand.mjs": `
						import {parentPort} from "node:worker_threads";
						import {stdin} from "noe:io";
						const input = await stdin.getReader().read();
						if (!input.done) throw Error("grandchild inherited stdin");
						parentPort.postMessage("grand");
					`,
				}
				source, ok := sources[specifier]
				if !ok {
					return "", "", false, errors.New("missing worker source: " + specifier)
				}
				return source, specifier, true, nil
			},
			Installed: func(workerRT *quickjs.Runtime) error {
				installed.Add(1)
				_, err := workerRT.EvalModule("installed-check.mjs", `
					import {docs} from "noe:docs";
					import {stdin} from "noe:io";
					export const installed = typeof docs === "function" && stdin instanceof ReadableStream;
				`)
				return err
			},
		},
	}
	bindings, err := Install(rt, Config{Core: core, IO: &IOConfig{Stdin: input}, Docs: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.EvalModule("main.mjs", `
		import {Worker} from "node:worker_threads";
		const worker = new Worker("./child.mjs");
		worker.on("message", message => console.log(message));
		worker.on("error", error => console.error(error.message));
	`); err != nil {
		t.Fatal(err)
	}
	if err := loop.Run(loopContext); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "child:grand\n" {
		t.Fatalf("root output = %q, want child message on shared output", got)
	}
	if installed.Load() != 2 {
		t.Errorf("original Installed callback count = %d, want once for each child", installed.Load())
	}
	if reads := input.reads.Load(); reads != 0 {
		t.Errorf("root stdin was read by workers: %d reads", reads)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := bindings.Stdout.Write([]byte("after close")); !errors.Is(err, hostio.ErrClosed) {
		t.Errorf("output facade after runtime close: %v", err)
	}
}

func TestWorkerInstalledFailureKeepsRootOutputUsable(t *testing.T) {
	loopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var output bytes.Buffer
	var callbackCount atomic.Int32

	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	defer rt.Close()
	bindings, err := Install(rt, Config{
		Core: stdlib.Config{
			Stdout:  &output,
			Loop:    loop,
			Process: &stdlib.Process{},
			Workers: &stdlib.Workers{
				Max: -1,
				Load: func(specifier string) (string, string, bool, error) {
					return `throw Error("worker body should not run")`, specifier, true, nil
				},
				Installed: func(*quickjs.Runtime) error {
					callbackCount.Add(1)
					return errors.New("installed sentinel")
				},
			},
		},
		IO:   &IOConfig{},
		Docs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rt.EvalModule("worker-failure.mjs", `
		import {Worker} from "node:worker_threads";
		import {stdout} from "noe:io";
		const worker = new Worker("./failure.mjs");
		worker.on("error", error => console.log("error", error.message));
		worker.on("exit", async code => {
			console.log("exit", code);
			const writer = stdout.getWriter();
			await writer.write(new TextEncoder().encode("alive"));
			writer.releaseLock();
		});
	`); err != nil {
		t.Fatal(err)
	}
	if err := loop.Run(loopContext); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "error installed sentinel\nexit 1\nalive"; got != want {
		t.Fatalf("root output after child install failure = %q, want %q", got, want)
	}
	if callbackCount.Load() != 1 {
		t.Errorf("original Installed callback count = %d, want 1", callbackCount.Load())
	}
	if _, err := bindings.Stdout.Write([]byte("!")); err != nil {
		t.Errorf("root binding was closed by child failure: %v", err)
	}
}

func TestSpawnUsesProcessEnvironmentAndWorkingDirectory(t *testing.T) {
	defaultDir := t.TempDir()
	optionDir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	loopContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	defer rt.Close()
	_, err = Install(rt, Config{Core: stdlib.Config{
		Loop: loop,
		Process: &stdlib.Process{
			Cwd: defaultDir,
			Env: map[string]string{
				"NOE_TEST_SPAWN_FIXTURE": "1",
				"NOE_TEST_BASE":          "from-process",
				"NOE_TEST_OVERRIDE":      "process-value",
			},
		},
	}, IO: &IOConfig{Cwd: defaultDir}})
	if err != nil {
		t.Fatal(err)
	}

	argsJSON, err := json.Marshal([]string{"-test.run=^TestSpawnFixture$"})
	if err != nil {
		t.Fatal(err)
	}
	exeJSON, err := json.Marshal(executable)
	if err != nil {
		t.Fatal(err)
	}
	optionDirJSON, err := json.Marshal(optionDir)
	if err != nil {
		t.Fatal(err)
	}
	main := fmt.Sprintf(`
		import {spawn} from "noe:process";
		const args = %s;
		const run = async options => {
			const child = await spawn(%s, args, {stdout:"pipe", stderr:"pipe", ...options});
			const reader = child.stdout.getReader();
			let text = "";
			for (;;) {
				const {done, value} = await reader.read();
				if (done) break;
				text += new TextDecoder().decode(value);
			}
			const status = await child.exited;
			await child.close();
			if (status.code !== 0) throw Error("fixture exit " + status.code + ": " + text);
			return JSON.parse(text.split("\n")[0]);
		};
		(async () => {
			const inherited = await run({env:{}});
			const overridden = await run({cwd:%s, env:{NOE_TEST_OVERRIDE:"option-value"}});
			globalThis.spawnResults = {inherited, overridden};
		})().catch(error => { globalThis.spawnError = error.stack || error.message; });
	`, string(argsJSON), string(exeJSON), string(optionDirJSON))
	if _, err := rt.EvalModule("spawn-check.mjs", main); err != nil {
		t.Fatal(err)
	}
	if err := loop.Run(loopContext); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Eval(`JSON.stringify({result: globalThis.spawnResults, error: globalThis.spawnError})`)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Result struct {
			Inherited  spawnFixtureResult `json:"inherited"`
			Overridden spawnFixtureResult `json:"overridden"`
		} `json:"result"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result.String()), &got); err != nil {
		t.Fatalf("decode spawn result %q: %v", result.String(), err)
	}
	if got.Error != "" {
		t.Fatalf("spawn failed: %s", got.Error)
	}
	if got.Result.Inherited.Base != "from-process" || got.Result.Inherited.Override != "process-value" || got.Result.Inherited.Cwd != filepath.Clean(defaultDir) {
		t.Errorf("default spawn settings = %#v", got.Result.Inherited)
	}
	if got.Result.Overridden.Base != "from-process" || got.Result.Overridden.Override != "option-value" || got.Result.Overridden.Cwd != filepath.Clean(optionDir) {
		t.Errorf("explicit spawn settings = %#v", got.Result.Overridden)
	}
}

type spawnFixtureResult struct {
	Base     string `json:"base"`
	Override string `json:"override"`
	Cwd      string `json:"cwd"`
}

func TestSpawnFixture(t *testing.T) {
	if os.Getenv("NOE_TEST_SPAWN_FIXTURE") != "1" {
		return
	}
	result := spawnFixtureResult{
		Base: os.Getenv("NOE_TEST_BASE"), Override: os.Getenv("NOE_TEST_OVERRIDE"), Cwd: mustGetwd(),
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		t.Fatal(err)
	}
}

func mustGetwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return filepath.Clean(cwd)
}
