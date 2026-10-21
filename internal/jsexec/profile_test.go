package jsexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestAttachSnapshotsEnvironmentAndIsolatesConcurrentExecutions(t *testing.T) {
	type result struct {
		cwd, output string
		err         error
	}
	inputs := []struct {
		cwd, key, value, arg string
	}{
		{cwd: t.TempDir(), key: "FIRST", value: "one", arg: "alpha"},
		{cwd: t.TempDir(), key: "SECOND", value: "two", arg: "beta"},
	}
	results := make(chan result, len(inputs))
	var wg sync.WaitGroup
	for _, input := range inputs {
		input := input
		wg.Add(1)
		go func() {
			defer wg.Done()
			rt := quickjs.New()
			loop := stdlib.NewLoop(rt)
			args := []string{"node", "entry.mjs", input.arg}
			env := map[string]string{input.key: input.value}
			stdin := strings.NewReader("borrowed input")
			stdout, stderr := &strings.Builder{}, &strings.Builder{}
			bindings, err := Attach(rt, loop, Profile{
				Environment: Environment{Cwd: input.cwd, Args: args, Env: env, Stdin: stdin, Stdout: stdout, Stderr: stderr},
				Modules:     Modules{ProcessInfo: true},
			})
			if err != nil {
				loop.Close()
				rt.Close()
				results <- result{err: err}
				return
			}
			args[2], env[input.key] = "mutated", "mutated"
			snapshot := bindings.Environment()
			if snapshot.Args[2] != input.arg || snapshot.Env[input.key] != input.value {
				results <- result{err: fmt.Errorf("execution environment was not snapshotted/borrowed: %#v", snapshot)}
				_ = bindings.Close()
				loop.Close()
				rt.Close()
				return
			}
			data, readErr := io.ReadAll(snapshot.Stdin)
			_, outErr := io.WriteString(snapshot.Stdout, "out")
			_, errErr := io.WriteString(snapshot.Stderr, "err")
			if readErr != nil || outErr != nil || errErr != nil || string(data) != "borrowed input" || stdout.String() != "out" || stderr.String() != "err" {
				results <- result{err: fmt.Errorf("execution adapters lost borrowed streams: %q/%q/%q errors %v/%v/%v", data, stdout.String(), stderr.String(), readErr, outErr, errErr)}
				_ = bindings.Close()
				loop.Close()
				rt.Close()
				return
			}
			snapshot.Args[2], snapshot.Env[input.key] = "caller mutation", "caller mutation"
			again := bindings.Environment()
			if again.Args[2] != input.arg || again.Env[input.key] != input.value {
				results <- result{err: errors.New("Environment returned mutable internal state")}
				_ = bindings.Close()
				loop.Close()
				rt.Close()
				return
			}
			if bindings.FileSystem() != nil {
				results <- result{err: errors.New("unselected filesystem was exposed")}
				_ = bindings.Close()
				loop.Close()
				rt.Close()
				return
			}
			value, evalErr := rt.Eval(`JSON.stringify({cwd: process.cwd(), arg: process.argv[2], value: process.env[` + fmt.Sprintf("%q", input.key) + `], hasStdin: "stdin" in process})`)
			if evalErr == nil {
				// A selected process object does not imply the stdio capability.
				if value.String() != fmt.Sprintf(`{"cwd":%q,"arg":%q,"value":%q,"hasStdin":false}`, filepath.Clean(input.cwd), input.arg, input.value) {
					evalErr = fmt.Errorf("unexpected process snapshot %s", value.String())
				}
			}
			if evalErr == nil {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				_, moduleErr := rt.EvalModuleContext(ctx, "no-stdio.mjs", `import "noe:io"`)
				cancel()
				if moduleErr == nil {
					evalErr = errors.New("unselected noe:io module was importable")
				}
			}
			results <- result{cwd: input.cwd, output: value.String(), err: evalErr}
			_ = bindings.Close()
			loop.Close()
			rt.Close()
		}()
	}
	wg.Wait()
	close(results)
	seen := map[string]bool{}
	for result := range results {
		if result.err != nil {
			t.Error(result.err)
			continue
		}
		seen[result.cwd] = true
	}
	if len(seen) != len(inputs) {
		t.Fatalf("only %d of %d execution snapshots completed", len(seen), len(inputs))
	}
}

func TestAttachFilesystemViewSharesRootAndBindingLifetime(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "virtual")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, "marker.txt"), []byte("same backend"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	bindings, err := Attach(rt, loop, Profile{
		Environment: Environment{Cwd: "/virtual"},
		Modules:     Modules{FS: &FileSystemOptions{Root: root}},
	})
	if err != nil {
		loop.Close()
		rt.Close()
		t.Fatal(err)
	}
	defer func() {
		_ = bindings.Close()
		loop.Close()
		_ = rt.Close()
	}()

	view := bindings.FileSystem()
	if view == nil {
		t.Fatal("selected filesystem was not exposed")
	}
	if got := view.PhysicalCwd(); got != cwd {
		t.Fatalf("physical cwd = %q, want %q", got, cwd)
	}
	data, err := view.ReadFile(context.Background(), "marker.txt")
	if err != nil || string(data) != "same backend" {
		t.Fatalf("read through binding filesystem: %q, %v", data, err)
	}
	if err := view.WriteFileAtomic(context.Background(), "written.txt", []byte("written")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	ns, err := rt.EvalModuleContext(ctx, "path-cwd.mjs", `
		import path from "path";
		export const cwd = path.resolve(".");
		export const relative = path.relative("/virtual", "/virtual/child");
	`)
	cancel()
	if err != nil {
		t.Fatalf("evaluate path helpers without process metadata: %v", err)
	}
	jsCwd, err := ns.Get("cwd")
	if err != nil || jsCwd.String() != "/virtual" {
		t.Fatalf("path.resolve cwd = %q, %v", jsCwd.String(), err)
	}
	jsRelative, err := ns.Get("relative")
	if err != nil || jsRelative.String() != "child" {
		t.Fatalf("path.relative cwd = %q, %v", jsRelative.String(), err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "written.txt")); err != nil {
		t.Fatalf("Go binding and JS profile did not use the same root/cwd: %v", err)
	}

	if err := bindings.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := view.ReadFile(context.Background(), "marker.txt"); !errors.Is(err, context.Canceled) {
		t.Fatalf("filesystem view remained active after Bindings.Close: %v", err)
	}
	if err := loop.Context().Err(); err != nil {
		t.Fatalf("Bindings.Close closed the borrowed loop: %v", err)
	}
	if _, err := rt.Eval(`1 + 1`); err != nil {
		t.Fatalf("Bindings.Close closed the borrowed runtime: %v", err)
	}
}

func TestProfileSelectsStdioWithoutAddingOtherNativeModules(t *testing.T) {
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	bindings, err := Attach(rt, loop, Profile{Modules: Modules{Stdio: true}})
	if err != nil {
		loop.Close()
		_ = rt.Close()
		t.Fatal(err)
	}
	defer func() {
		_ = bindings.Close()
		loop.Close()
		_ = rt.Close()
	}()

	env := bindings.Environment()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	pathNS, err := rt.EvalModuleContext(ctx, "profile-cwd.mjs", `
		import path from "path";
		export const cwd = path.resolve(".");
	`)
	cancel()
	if err != nil {
		t.Fatalf("evaluate path helper without process metadata: %v", err)
	}
	pathCwd, err := pathNS.Get("cwd")
	if err != nil || pathCwd.String() != env.Cwd {
		t.Fatalf("path.resolve cwd = %q, want environment cwd %q (%v)", pathCwd.String(), env.Cwd, err)
	}

	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	ns, err := rt.EvalModuleContext(ctx, "selected-stdio.mjs", `
		import {stdin, stdout, stderr} from "noe:io";
		export const selected = stdin && stdout && stderr;
	`)
	cancel()
	if err != nil {
		t.Fatalf("selected stdio module was unavailable: %v", err)
	}
	selected, err := ns.Get("selected")
	if err != nil || !selected.Bool() {
		t.Fatalf("stdio exports = %v, %v", selected, err)
	}
	processMissing, err := rt.Eval(`typeof process === "undefined"`)
	if err != nil {
		t.Fatal(err)
	}
	if !processMissing.Bool() {
		t.Fatal("stdio selection installed process metadata")
	}
	for _, specifier := range []string{"noe:process", "noe:fs-handle", "noe:sockets", "noe:signals"} {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := rt.EvalModuleContext(ctx, "unselected-"+strings.NewReplacer(":", "-", "/", "-").Replace(specifier)+".mjs", `import `+fmt.Sprintf("%q", specifier))
		cancel()
		if err == nil {
			t.Errorf("unselected native module %q was importable", specifier)
		}
	}
}

func TestProfileWorkerPreservesIdentityAndSharedMax(t *testing.T) {
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	initializedWorkers := make(chan bool, 2)
	bindings, err := Attach(rt, loop, Profile{
		Environment: Environment{
			Args: []string{"qjs", "main.mjs"},
			Env:  map[string]string{"PARENT": "yes"},
		},
		Modules: Modules{
			ProcessInfo: true,
			Workers:     &WorkerOptions{Max: 1},
		},
		Initialize: func(_ context.Context, bindings *Bindings) (func(), error) {
			initializedWorkers <- bindings.Worker()
			return nil, nil
		},
	})
	if err != nil {
		loop.Close()
		_ = rt.Close()
		t.Fatal(err)
	}
	defer func() {
		_ = bindings.Close()
		loop.Close()
		_ = rt.Close()
	}()
	if worker := <-initializedWorkers; worker {
		t.Fatal("main profile initializer reported a worker")
	}

	_, err = rt.EvalModule("profile-worker-main.mjs", `
		import {Worker} from "node:worker_threads";
		globalThis.profileWorkerResult = null;
		globalThis.profileWorkerExit = null;
		globalThis.profileWorkerError = null;
		const code = `+fmt.Sprintf("%q", `
			import {Worker, parentPort, isMainThread, threadId} from "node:worker_threads";
			let nestedMessage = "";
			let nestedRejected = false;
			try {
				new Worker("export {}", {eval: true, type: "module"});
			} catch (error) {
				nestedRejected = true;
				nestedMessage = error.message;
			}
			parentPort.postMessage({
				isMainThread,
				threadId,
				hasParentPort: parentPort !== null && typeof parentPort.postMessage === "function",
				nestedRejected,
				nestedMessage,
				argv: process.argv.slice(2),
				childEnv: process.env.CHILD
			});
			process.exit(7);
		`)+`;
		const worker = new Worker(code, {eval: true, type: "module", argv: ["child-arg"], env: {CHILD: "yes"}});
		worker.on("message", (result) => { globalThis.profileWorkerResult = result; });
		worker.on("error", (error) => { globalThis.profileWorkerError = error.message; });
		worker.on("exit", (code) => { globalThis.profileWorkerExit = code; });
	`)
	if err != nil {
		t.Fatalf("start profile worker: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = loop.Run(ctx)
	cancel()
	if err != nil {
		t.Fatalf("run profile worker: %v", err)
	}
	result, err := rt.Eval(`JSON.stringify({result: profileWorkerResult, exit: profileWorkerExit, error: profileWorkerError})`)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Result *struct {
			IsMainThread   bool     `json:"isMainThread"`
			ThreadID       int      `json:"threadId"`
			HasParentPort  bool     `json:"hasParentPort"`
			NestedRejected bool     `json:"nestedRejected"`
			NestedMessage  string   `json:"nestedMessage"`
			Args           []string `json:"argv"`
			ChildEnv       string   `json:"childEnv"`
		} `json:"result"`
		Exit  *int   `json:"exit"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result.String()), &got); err != nil {
		t.Fatalf("decode profile worker result %s: %v", result.String(), err)
	}
	if got.Error != "" {
		t.Fatalf("profile worker failed: %s", got.Error)
	}
	if got.Result == nil {
		t.Fatalf("profile worker sent no result: %s", result.String())
	}
	if got.Result.IsMainThread || got.Result.ThreadID < 1 || !got.Result.HasParentPort {
		t.Errorf("worker identity = %#v", got.Result)
	}
	if !got.Result.NestedRejected || got.Result.NestedMessage != "too many workers: at most 1 may run at once" {
		t.Errorf("nested worker was not rejected by shared Max: %#v", got.Result)
	}
	if !slices.Equal(got.Result.Args, []string{"child-arg"}) || got.Result.ChildEnv != "yes" {
		t.Errorf("worker environment/argv = %#v", got.Result)
	}
	if got.Exit == nil || *got.Exit != 7 {
		t.Errorf("worker exit code = %v, want 7", got.Exit)
	}
	select {
	case worker := <-initializedWorkers:
		if !worker {
			t.Error("worker profile initializer reported main runtime")
		}
	default:
		t.Error("profile initializer did not run for worker runtime")
	}
}

func TestPrepareComposesOwnerModulesWithFreshProfileLoaders(t *testing.T) {
	root := t.TempDir()
	physicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"one", "two"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var origins []Origin
	loaderRuns := 0
	profiles := []struct{ cwd, owner string }{
		{cwd: "/one", owner: "owner-one"},
		{cwd: "/two", owner: "owner-two"},
	}
	for i, profileInput := range profiles {
		rt := quickjs.New()
		loop := stdlib.NewLoop(rt)
		if i == 0 {
			if err := rt.SetModule("owner:source", map[string]any{"value": "native"}); err != nil {
				t.Fatal(err)
			}
		}
		var bindings *Bindings
		profile := Profile{
			Environment: Environment{Cwd: profileInput.cwd},
			Modules:     Modules{FS: &FileSystemOptions{Root: root}},
			NewLoader: func(_ context.Context, origin Origin) (quickjs.ModuleLoader, error) {
				if origin.FileSystem == nil || bindings == nil || origin.FileSystem != bindings.FileSystem() {
					return nil, errors.New("loader did not receive this execution's filesystem view")
				}
				if got, want := origin.FileSystem.PhysicalCwd(), filepath.Join(physicalRoot, strings.TrimPrefix(origin.Cwd, "/")); got != want {
					return nil, fmt.Errorf("loader physical cwd = %q, want %q", got, want)
				}
				mu.Lock()
				origins = append(origins, origin)
				loaderRuns++
				mu.Unlock()
				return func(specifier, _ string) (string, string, error) {
					if specifier != "factory:source" {
						return "", "", fmt.Errorf("unknown module %q", specifier)
					}
					return `export const value = ` + fmt.Sprintf("%q", origin.Cwd) + `;`, specifier, nil
				}, nil
			},
		}
		bindings, err = Attach(rt, loop, profile)
		if err != nil {
			loop.Close()
			rt.Close()
			t.Fatal(err)
		}
		if err := bindings.DefineModule("owner:source", `export const value = `+fmt.Sprintf("%q", profileInput.owner)+`;`); err != nil {
			t.Fatal(err)
		}
		source, err := bindings.Prepare(Source{
			Name: "[memory-entry]", Module: true, Virtual: true,
			Code: `import {value as owner} from "owner:source"; import {value as cwd} from "factory:source"; export const result = owner + ":" + cwd;`,
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		ns, evalErr := rt.EvalModuleContext(ctx, source.Name, source.Code)
		cancel()
		if evalErr != nil {
			t.Fatalf("evaluate prepared source in %s: %v", profileInput.cwd, evalErr)
		}
		result, err := ns.Get("result")
		wantOwner := profileInput.owner
		if i == 0 {
			wantOwner = "native"
		}
		if err != nil || result.String() != wantOwner+":"+profileInput.cwd {
			t.Fatalf("prepared source result = %q, %v", result.String(), err)
		}
		_ = bindings.Close()
		loop.Close()
		_ = rt.Close()
	}
	if loaderRuns != len(profiles) {
		t.Fatalf("loader factory ran %d times, want %d", loaderRuns, len(profiles))
	}
	if len(origins) != len(profiles) {
		t.Fatalf("got %d loader origins, want %d", len(origins), len(profiles))
	}
	for i, origin := range origins {
		if !origin.Virtual || origin.Entry != "[memory-entry]" || origin.Cwd != profiles[i].cwd || origin.Worker || origin.FileSystem == nil {
			t.Errorf("origin %d = %#v", i, origin)
		}
	}
}

func TestPrepareWithoutProfileLoaderPreservesCallerLoader(t *testing.T) {
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.SetModuleLoader(func(specifier, _ string) (string, string, error) {
		if specifier != "caller:source" {
			return "", "", fmt.Errorf("unknown caller module %q", specifier)
		}
		return `export const value = "caller";`, specifier, nil
	})
	bindings, err := Attach(rt, loop, Profile{})
	if err != nil {
		loop.Close()
		_ = rt.Close()
		t.Fatal(err)
	}
	defer func() {
		_ = bindings.Close()
		loop.Close()
		_ = rt.Close()
	}()
	source, err := bindings.Prepare(Source{
		Name: "[caller-entry]", Virtual: true, Module: true,
		Code: `import {value} from "caller:source"; export const result = value;`,
	})
	if err != nil {
		t.Fatal(err)
	}
	ns, err := rt.EvalModule(source.Name, source.Code)
	if err != nil {
		t.Fatalf("Prepare replaced caller's loader: %v", err)
	}
	result, err := ns.Get("result")
	if err != nil || result.String() != "caller" {
		t.Fatalf("caller loader result = %q, %v", result.String(), err)
	}
}

func TestAttachInitializerCleanupAndMismatch(t *testing.T) {
	var initialized, cleaned int
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	bindings, err := Attach(rt, loop, Profile{Initialize: func(ctx context.Context, b *Bindings) (func(), error) {
		initialized++
		if b.Runtime() != rt || b.Loop() != loop || b.Context() != ctx || b.Worker() {
			return nil, errors.New("initializer received the wrong execution view")
		}
		return func() { cleaned++ }, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if initialized != 1 || cleaned != 0 {
		t.Fatalf("initializer/cleanup counts before close = %d/%d", initialized, cleaned)
	}
	ctx := bindings.Context()
	if err := bindings.Close(); err != nil {
		t.Fatal(err)
	}
	if err := bindings.Close(); err != nil {
		t.Fatal(err)
	}
	if initialized != 1 || cleaned != 1 || ctx.Err() == nil {
		t.Fatalf("initializer/cleanup/context after close = %d/%d/%v", initialized, cleaned, ctx.Err())
	}
	if loop.Context().Err() != nil {
		t.Fatal("Bindings.Close canceled the borrowed loop")
	}
	if _, err := rt.Eval(`40 + 2`); err != nil {
		t.Fatalf("Bindings.Close closed the borrowed runtime: %v", err)
	}
	_ = rt.Close()
	loop.Close()
	if cleaned != 1 {
		t.Fatalf("runtime close repeated cleanup: %d", cleaned)
	}

	failedRT := quickjs.New()
	failedLoop := stdlib.NewLoop(failedRT)
	var failedCleanup int
	_, err = Attach(failedRT, failedLoop, Profile{Initialize: func(context.Context, *Bindings) (func(), error) {
		return func() { failedCleanup++ }, errors.New("initializer failed after acquisition")
	}})
	if err == nil || failedCleanup != 1 {
		t.Fatalf("partial initializer failure = %v, cleanup calls = %d", err, failedCleanup)
	}
	if failedLoop.Context().Err() != nil {
		t.Fatal("initializer error closed the borrowed loop")
	}
	if _, evalErr := failedRT.Eval(`6 * 7`); evalErr != nil {
		t.Fatalf("initializer error closed the borrowed runtime: %v", evalErr)
	}
	_ = failedRT.Close()
	failedLoop.Close()
	if failedCleanup != 1 {
		t.Fatalf("runtime close repeated partial cleanup: %d", failedCleanup)
	}

	otherRT, otherLoop := quickjs.New(), (*stdlib.Loop)(nil)
	otherLoop = stdlib.NewLoop(otherRT)
	defer otherRT.Close()
	defer otherLoop.Close()
	if _, err := Attach(otherRT, loop, Profile{}); err == nil {
		t.Fatal("Attach accepted a loop owned by a different runtime")
	}
}

var _ io.Reader = (*strings.Reader)(nil)
