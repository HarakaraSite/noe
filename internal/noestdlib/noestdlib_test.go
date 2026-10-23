package noestdlib

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/hostio"
	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
)

func TestValidationHappensBeforeRegistration(t *testing.T) {
	cases := []struct {
		name string
		cfg  ProfileConfig
	}{
		{name: "workers need loop", cfg: ProfileConfig{Core: stdlib.Config{Workers: &stdlib.Workers{}}}},
		{name: "workers need web apis", cfg: ProfileConfig{Core: stdlib.Config{Loop: &stdlib.Loop{}, Workers: &stdlib.Workers{}, NoWebAPIs: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := quickjs.New()
			defer rt.Close()
			if _, err := InstallProfile(rt, tc.cfg); err == nil {
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
	if _, err := InstallProfile(rt, ProfileConfig{Core: stdlib.Config{
		NoWebAPIs: true,
		Process:   &stdlib.Process{Stdin: input},
	}}); err != nil {
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
	cfg := ProfileConfig{Core: stdlib.Config{
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
		if _, err := InstallProfile(rt, cfg); err != nil {
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
		bindings, err := InstallProfile(rt, ProfileConfig{Core: stdlib.Config{
			Stdout: sink, Stderr: sink, Process: &stdlib.Process{Stdout: sink, Stderr: sink},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if bindings.Stdout != bindings.Stderr {
			t.Fatal("same comparable output sink got separate facades")
		}
		if _, err := rt.Eval(`console.log("console"); console.log("process")`); err != nil {
			t.Fatal(err)
		}
		if got := sink.String(); got != "console\nprocess\n" {
			t.Fatalf("output = %q", got)
		}
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := bindings.Stdout.Write([]byte("after close")); !errors.Is(err, hostio.ErrClosed) {
			t.Errorf("owned facade write after runtime close: %v", err)
		}
		if got := sink.String(); got != "console\nprocess\n" {
			t.Errorf("borrowed sink changed after close: %q", got)
		}
	})

	t.Run("non-comparable writer does not panic", func(t *testing.T) {
		var sink uncomparableWriter = make([]byte, 0, 64)
		rt := quickjs.New()
		_, err := InstallProfile(rt, ProfileConfig{Core: stdlib.Config{
			Stdout: sink, Process: &stdlib.Process{Stdout: sink},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rt.Eval(`console.log("ok")`); err != nil {
			t.Fatal(err)
		}
		rt.Close()
	})

	t.Run("context writer is preserved", func(t *testing.T) {
		sink := &contextWriter{}
		rt := quickjs.New()
		bindings, err := InstallProfile(rt, ProfileConfig{Core: stdlib.Config{Stdout: sink}})
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
	bindings, err := InstallProfile(rt, ProfileConfig{Core: stdlib.Config{Stdout: original}})
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
