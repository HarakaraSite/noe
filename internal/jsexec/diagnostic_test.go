package jsexec

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestInitializerWrappedJSErrorDetachesAndPreservesNativeCause(t *testing.T) {
	cause := errors.New("native initializer failure")
	runtime, err := NewRuntime(context.Background(), Profile{Initialize: func(ctx context.Context, b *Bindings) (func(), error) {
		if err := b.Runtime().Set("failNative", func() error { return cause }); err != nil {
			return nil, err
		}
		_, jsErr := b.Runtime().EvalFileContext(ctx, "initializer.mjs", `failNative();`)
		return nil, fmt.Errorf("initialize binding: %w", jsErr)
	}})
	if runtime != nil || err == nil {
		t.Fatalf("runtime=%v error=%v", runtime, err)
	}
	var jsError *quickjs.Error
	if errors.As(err, &jsError) {
		t.Fatal("a live JS error escaped failed runtime setup")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("native cause was lost: %v", err)
	}
	text := Diagnostic(err)
	if !strings.Contains(text, "initialize binding:") || !strings.Contains(text, "initializer.mjs") || !strings.Contains(text, cause.Error()) {
		t.Fatalf("detached diagnostic=%q", text)
	}
}

func TestDiagnosticDetailsDoNotCallUserGetter(t *testing.T) {
	entered := false
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	runtime, err := NewRuntime(ctx, Profile{Initialize: func(ctx context.Context, b *Bindings) (func(), error) {
		return nil, b.Runtime().Set("entered", func() { entered = true })
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Run(Source{Name: "getter-source.js", Virtual: true, Code: `const e=new Error("diagnostic error");Object.defineProperty(e,"code",{get(){entered();for(;;){}}});throw e;`})
	if closeErr := runtime.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err == nil || entered || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("getter entered=%v error=%v", entered, err)
	}
	if text := Diagnostic(err); !strings.Contains(text, "diagnostic error") || !strings.Contains(text, "getter-source.js") {
		t.Fatalf("diagnostic=%q", text)
	}
}

func TestUnhandledPromiseDiagnosticCapturesFramesBeforeStackRendering(t *testing.T) {
	runtime, err := NewRuntime(context.Background(), Profile{})
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := runtime.Run(Source{
		Name: "unhandled-promise.mjs", Module: true, Virtual: true,
		Code: `Promise.resolve().then(() => { throw new Error("promise rejected"); }); export const ready = true;`,
	})
	closeErr := runtime.Close()
	err = errors.Join(runErr, closeErr)
	if result.Code != 1 || err == nil || len(result.Diagnostics) != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != codeJSException || diagnostic.Phase != "evaluate" || !strings.Contains(diagnostic.Message, "promise rejected") {
		t.Fatalf("unhandled Promise diagnostic = %#v", diagnostic)
	}
	if diagnostic.Source != "unhandled-promise.mjs" || diagnostic.Line == 0 || diagnostic.Column == 0 || len(diagnostic.Stack) == 0 {
		t.Fatalf("unhandled Promise lost structured frames: %#v", diagnostic)
	}
	if diagnostic.Stack[0].Source != "unhandled-promise.mjs" || diagnostic.Stack[0].Line == 0 {
		t.Fatalf("unhandled Promise stack = %#v", diagnostic.Stack)
	}
}

func TestDiagnosticUserConversionHonorsRuntimeCancel(t *testing.T) {
	started := make(chan struct{})
	runtime, err := NewRuntime(context.Background(), Profile{Initialize: func(ctx context.Context, b *Bindings) (func(), error) {
		return nil, b.Runtime().Set("started", func() { close(started) })
	}})
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan bool, 1)
	go func() {
		select {
		case <-started:
			entered <- true
			runtime.Cancel()
		case <-time.After(2 * time.Second):
			entered <- false
			runtime.Cancel()
		}
	}()
	_, err = runtime.Run(Source{Name: "conversion.js", Virtual: true, Code: `const e=new Error("conversion");e.toString=()=>{started();for(;;){}};throw e;`})
	if !<-entered {
		t.Fatal("diagnostic did not enter the fixture conversion")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("diagnostic cancellation error=%v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticProxyTrapHonorsParentDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	cleaned := false
	result, err := Execute(ctx, Source{Name: "proxy.js", Virtual: true, Code: `throw new Proxy({}, {getOwnPropertyDescriptor(){for(;;){}}});`}, Profile{Initialize: func(context.Context, *Bindings) (func(), error) { return func() { cleaned = true }, nil }})
	if result.Code != 1 || !errors.Is(err, context.DeadlineExceeded) || !cleaned {
		t.Fatalf("result=%#v err=%v cleaned=%v", result, err, cleaned)
	}
}

func TestManagedCommandDiagnosticRetainsOutputAfterVMClose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := Execute(ctx, Source{Name: "memory:command-error", Virtual: true, Module: true, Code: `import {execFileSync} from "node:child_process";execFileSync("/bin/sh",["-c","printf fixture-out; printf fixture-err >&2; exit 9"]);`}, Profile{Modules: Modules{Commands: &CommandOptions{Allow: func(string, []string) error { return nil }}}})
	if err == nil {
		t.Fatal("failed command returned success")
	}
	diagnostic := Diagnostic(err)
	for _, want := range []string{"memory:command-error", "fixture-out", "fixture-err", `"code":9`} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("detached diagnostic missing %q: %s", want, diagnostic)
		}
	}
}

func TestWrappedJoinedJSErrorPreservesSiblingNativeCause(t *testing.T) {
	sentinel := errors.New("joined native failure")
	_, err := NewRuntime(context.Background(), Profile{Initialize: func(ctx context.Context, b *Bindings) (func(), error) {
		_, jsErr := b.Runtime().EvalFileContext(ctx, "joined.js", `throw new Error("joined JS failure")`)
		return nil, fmt.Errorf("initialize joined: %w", errors.Join(jsErr, sentinel))
	}})
	var jsErr *quickjs.Error
	if err == nil || errors.As(err, &jsErr) || !errors.Is(err, sentinel) {
		t.Fatalf("detached=%v native=%v", err, errors.Is(err, sentinel))
	}
	if text := Diagnostic(err); !strings.Contains(text, "initialize joined:") || !strings.Contains(text, "joined.js") || !strings.Contains(text, sentinel.Error()) {
		t.Fatalf("diagnostic=%q", text)
	}
}
