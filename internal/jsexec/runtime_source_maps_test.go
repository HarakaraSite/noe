package jsexec

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

const sourceMapProgram = "\"use strict\";\nfunction compiled(){\n throw new Error(\"mapped marker\");\n}\ncompiled();\n"

func runtimeMapJSON(source string) string {
	return `{"version":3,"sources":["` + source + `"],"names":["original"],"mappings":";;AAKAA"}`
}

func inlineMappedProgram() string {
	return sourceMapProgram + "//# sourceMappingURL=data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte(runtimeMapJSON("../src/original.ts")))
}

func TestRuntimeSourceMapsInlineByDefault(t *testing.T) {
	for _, module := range []bool{false, true} {
		name := "script"
		if module {
			name = "module"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := Execute(ctx, Source{Name: "/project/dist/bundle.js", Code: inlineMappedProgram(), Module: module, Virtual: true}, Profile{})
			requireMappedDiagnostic(t, result, err, "original.ts:6:1")
		})
	}
}

func TestRuntimeSourceMapSidecarUsesReadOnlyRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "dist", "maps"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dist", "maps", "bundle.js.map"), []byte(runtimeMapJSON("../../src/original.ts")), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := Execute(ctx, Source{Name: "bundle.js", Code: sourceMapProgram + "//# sourceMappingURL=maps/bundle.js.map", Module: true}, Profile{
		Environment: Environment{Cwd: "/dist"},
		Modules:     Modules{FS: &FileSystemOptions{Root: root, ReadOnly: true}},
	})
	requireMappedDiagnostic(t, result, err, "/src/original.ts:6:1")
	if strings.Contains(Diagnostic(err), root) {
		t.Fatalf("physical root leaked in mapped diagnostic: %s", Diagnostic(err))
	}
}

func TestRuntimeSourceMapsFallbackAndPrepareStackTrace(t *testing.T) {
	for _, annotation := range []string{"", "missing.map", "data:application/json;base64,invalid", "data:application/json,%7Bbroken", "https://example.invalid/source.map"} {
		t.Run(annotation, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			code := sourceMapProgram
			if annotation != "" {
				code += "//# sourceMappingURL=" + annotation
			}
			result, err := Execute(ctx, Source{Name: "/project/dist/bundle.js", Code: code, Virtual: true}, Profile{})
			if result.Code != 1 || err == nil || !strings.Contains(Diagnostic(err), "bundle.js:3:") || !strings.Contains(Diagnostic(err), "mapped marker") {
				t.Fatalf("map failure replaced generated diagnostic: result=%+v error=%s", result, Diagnostic(err))
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := Execute(ctx, Source{Name: "/project/dist/bundle.js", Code: inlineMappedProgram(), Virtual: true}, Profile{
		Initialize: func(ctx context.Context, b *Bindings) (func(), error) {
			_, err := b.Runtime().EvalContext(ctx, `Error.prepareStackTrace=(_,frames)=>frames[0].getFileName()+":"+frames[0].getLineNumber();`)
			return nil, err
		},
	})
	if result.Code != 1 || err == nil || Diagnostic(err) != "/project/dist/bundle.js:3" {
		t.Fatalf("custom stack formatting changed: result=%+v error=%s", result, Diagnostic(err))
	}
}

func TestRuntimeSourceMapsDuringInitializer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtime, err := NewRuntime(ctx, Profile{Initialize: func(ctx context.Context, b *Bindings) (func(), error) {
		_, err := b.Runtime().EvalFileContext(ctx, "/project/dist/setup.js", inlineMappedProgram())
		return nil, err
	}})
	if runtime != nil || err == nil || !strings.Contains(Diagnostic(err), "original.ts:6:1") {
		t.Fatalf("initializer mapping: runtime=%v error=%s", runtime, Diagnostic(err))
	}
}

func requireMappedDiagnostic(t *testing.T, result Result, err error, original string) {
	t.Helper()
	if err == nil || result.Code != 1 || !strings.Contains(Diagnostic(err), original) || !strings.Contains(Diagnostic(err), "mapped marker") {
		t.Fatalf("mapped diagnostic: result=%+v error=%s", result, Diagnostic(err))
	}
	if len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Stack, original) {
		t.Fatalf("structured stack was not mapped: %+v", result.Diagnostics)
	}
	var live *quickjs.Error
	if errors.As(err, &live) {
		t.Fatal("mapped error retained a live VM value")
	}
}

func TestRuntimeSourceMapsPreserveNativeCause(t *testing.T) {
	cause := errors.New("mapped native failure")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code := strings.Replace(inlineMappedProgram(), `throw new Error("mapped marker");`, `failNative();`, 1)
	result, err := Execute(ctx, Source{Name: "/project/dist/native.js", Code: code, Virtual: true}, Profile{
		Initialize: func(_ context.Context, b *Bindings) (func(), error) {
			return nil, b.Runtime().Set("failNative", func() error { return cause })
		},
	})
	if result.Code != 1 || !errors.Is(err, cause) || !strings.Contains(Diagnostic(err), "original.ts:6:1") {
		t.Fatalf("mapped native cause lost: result=%+v error=%s", result, Diagnostic(err))
	}
}
