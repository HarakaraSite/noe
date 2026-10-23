package jsexec

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
)

func runModuleResult(t *testing.T, code string, profile Profile) (Result, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return Execute(ctx, Source{Name: "memory:result.mjs", Code: code, Module: true, Virtual: true}, profile)
}

func TestManagedModuleDefaultResultContract(t *testing.T) {
	tests := []struct {
		name, source, want string
	}{
		{
			name:   "missing default does not assimilate named then",
			source: `export function then(){throw new Error("namespace then was called")}`,
		},
		{name: "null", source: `export default null`, want: "null"},
		{
			name:   "default promise and thenable",
			source: `export default new Promise(resolve=>setTimeout(()=>resolve(8),1))`,
			want:   "8",
		},
		{
			name:   "default thenable",
			source: `export default {then(resolve){setTimeout(()=>resolve(9),1)}}`,
			want:   "9",
		},
		{name: "top-level await", source: `await new Promise(resolve=>setTimeout(resolve,1));export default 10`, want: "10"},
		{
			name:   "nested values use JSON rules and captured stringify",
			source: `JSON.stringify=()=>"replaced"; export default {omit:undefined,fn(){},symbol:Symbol("x"),infinite:Infinity,list:[undefined,()=>{},Symbol("y")]}`,
			want:   `{"infinite":null,"list":[null,null,null]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := runModuleResult(t, tt.source, Profile{})
			if err != nil || result.Code != 0 {
				t.Fatalf("result=%#v error=%v", result, err)
			}
			if tt.want == "" {
				if result.Data != nil {
					t.Fatalf("missing default returned data %s", result.Data)
				}
			} else if string(result.Data) != tt.want {
				t.Fatalf("data=%s want %s", result.Data, tt.want)
			}
		})
	}
}

func TestManagedPrivateWrapperAvoidsEntryAndDefinedModuleCollisions(t *testing.T) {
	t.Run("entry uses initial helper specifier", func(t *testing.T) {
		managed, err := NewRuntime(context.Background(), Profile{})
		if err != nil {
			t.Fatal(err)
		}
		name := managed.helperName
		result, runErr := managed.Run(Source{Name: name, Code: `export default 17`, Module: true, Virtual: true})
		closeErr := managed.Close()
		if err := errors.Join(runErr, closeErr); err != nil || string(result.Data) != "17" {
			t.Fatalf("result=%#v error=%v", result, err)
		}
	})

	t.Run("entry uses initial wrapper specifier", func(t *testing.T) {
		managed, err := NewRuntime(context.Background(), Profile{})
		if err != nil {
			t.Fatal(err)
		}
		name := managed.helperName + "/entry"
		result, runErr := managed.Run(Source{Name: name, Code: `export default 18`, Module: true, Virtual: true})
		closeErr := managed.Close()
		if err := errors.Join(runErr, closeErr); err != nil || string(result.Data) != "18" {
			t.Fatalf("result=%#v error=%v", result, err)
		}
	})

	t.Run("Profile-defined module keeps its binding", func(t *testing.T) {
		managed, err := NewRuntime(context.Background(), Profile{})
		if err != nil {
			t.Fatal(err)
		}
		name := managed.helperName
		if err := managed.Bindings().DefineModule(name, `export const value=19`); err != nil {
			t.Fatal(err)
		}
		code := fmt.Sprintf(`import {value} from %q;export default value`, name)
		result, runErr := managed.Run(Source{Name: "memory:module-collision.mjs", Code: code, Module: true, Virtual: true})
		closeErr := managed.Close()
		if err := errors.Join(runErr, closeErr); err != nil || string(result.Data) != "19" {
			t.Fatalf("result=%#v error=%v", result, err)
		}
	})
}

func TestManagedSerializationFailureIsReportedWithoutData(t *testing.T) {
	tests := []struct {
		name, source string
	}{
		{"undefined", `export default undefined`},
		{"function is not invoked", `export default (()=>{recordCall();return 1})`},
		{"symbol", `export default Symbol("root")`},
		{"bigint", `export default 1n`},
		{"cycle", `const x={};x.self=x;export default x`},
		{"toJSON throws", `export default {toJSON(){const e=new Error("conversion failed");e.code="SERIALIZE";throw e}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := 0
			result, err := runModuleResult(t, tt.source, Profile{Initialize: func(_ context.Context, b *Bindings) (func(), error) {
				return nil, b.Runtime().Set("recordCall", func() { called++ })
			}})
			if err == nil || result.Code != 1 || result.Data != nil {
				t.Fatalf("result=%#v error=%v", result, err)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "serialization_error" || result.Diagnostics[0].Stage != "serialize" {
				t.Fatalf("diagnostics=%#v", result.Diagnostics)
			}
			if tt.name == "function is not invoked" && called != 0 {
				t.Fatalf("default function was called %d times", called)
			}
			if tt.name == "toJSON throws" && result.Diagnostics[0].Exception["code"] != "SERIALIZE" {
				t.Fatalf("exception details=%#v", result.Diagnostics[0].Exception)
			}
		})
	}
}

func TestManagedSerializationDrivesNewWorkOnceAndDropsDataOnFailure(t *testing.T) {
	t.Run("waits for work queued by stringify without taking another snapshot", func(t *testing.T) {
		called, timerFinished := 0, false
		result, err := runModuleResult(t, `export default {toJSON(){recordSerialize();setTimeout(markTimer,1);return {answer:42}}}`, Profile{Initialize: func(_ context.Context, b *Bindings) (func(), error) {
			if err := b.Runtime().Set("recordSerialize", func() { called++ }); err != nil {
				return nil, err
			}
			return nil, b.Runtime().Set("markTimer", func() { timerFinished = true })
		}})
		if err != nil || string(result.Data) != `{"answer":42}` || called != 1 || !timerFinished {
			t.Fatalf("result=%#v error=%v stringify calls=%d timer=%v", result, err, called, timerFinished)
		}
	})

	t.Run("post-snapshot job failure is an execution failure", func(t *testing.T) {
		result, err := runModuleResult(t, `export default {toJSON(){setTimeout(()=>{throw new Error("after snapshot")},1);return {answer:42}}}`, Profile{})
		if err == nil || result.Data != nil || len(result.Diagnostics) == 0 {
			t.Fatalf("result=%#v error=%v", result, err)
		}
		if got := result.Diagnostics[0]; got.Code != "execution_error" || got.Stage != "execute" || !strings.Contains(got.Message, "after snapshot") {
			t.Fatalf("diagnostic=%#v", got)
		}
	})
}

func TestManagedSerializationUsesExitCodeAfterPostSnapshotLoop(t *testing.T) {
	for _, tc := range []struct {
		name, source string
	}{
		{name: "toJSON", source: `export default {toJSON(){process.exitCode=7;return 42}}`},
		{name: "timer", source: `export default {toJSON(){setTimeout(()=>{process.exitCode=7},1);return 42}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := runModuleResult(t, tc.source, Profile{Modules: Modules{ProcessInfo: true}})
			if err != nil || result.Code != 7 || string(result.Data) != "42" {
				t.Fatalf("result=%#v error=%v", result, err)
			}
		})
	}
}

func TestManagedPostSnapshotUnhandledRejectionPreservesNativeCause(t *testing.T) {
	sentinel := &postSnapshotNativeCause{}
	result, err := runModuleResult(t, `export default {toJSON(){Promise.resolve().then(()=>failNative());return 42}}`, Profile{Initialize: func(_ context.Context, b *Bindings) (func(), error) {
		return nil, b.Runtime().Set("failNative", func() error { return sentinel })
	}})
	var cause *postSnapshotNativeCause
	if !errors.Is(err, sentinel) || !errors.As(err, &cause) || result.Data != nil || len(result.Diagnostics) == 0 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if got := result.Diagnostics[0]; got.Code != "execution_error" || got.Stage != "execute" {
		t.Fatalf("diagnostic=%#v", got)
	}
}

type postSnapshotNativeCause struct{}

func (*postSnapshotNativeCause) Error() string { return "post-snapshot native rejection" }

func TestManagedDiagnosticsDetachPositionCauseAndCommandBytes(t *testing.T) {
	t.Run("syntax position is retained when provided directly", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		result, err := Execute(ctx, Source{Name: "memory:result.js", Code: "let value = 1;\nlet broken = ;", Virtual: true}, Profile{})
		if err == nil || len(result.Diagnostics) != 1 {
			t.Fatalf("result=%#v error=%v", result, err)
		}
		position := result.Diagnostics[0].Position
		if position == nil || position.Source != "memory:result.js" || position.Line != 2 || position.Column <= 0 {
			t.Fatalf("position=%#v diagnostic=%#v", position, result.Diagnostics[0])
		}
	})

	t.Run("native cause survives default rejection", func(t *testing.T) {
		sentinel := errors.New("native default cause")
		result, err := runModuleResult(t, `export default Promise.resolve().then(()=>fail())`, Profile{Initialize: func(_ context.Context, b *Bindings) (func(), error) {
			return nil, b.Runtime().Set("fail", func() error { return sentinel })
		}})
		if !errors.Is(err, sentinel) || len(result.Diagnostics) != 1 || result.Diagnostics[0].Stage != "execute" {
			t.Fatalf("result=%#v error=%v", result, err)
		}
	})

	t.Run("buffer command output is base64 detached data", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("fixture uses POSIX shell")
		}
		result, err := runModuleResult(t, `import {execFile} from "node:child_process";await execFile("/bin/sh",["-c","printf '\\001\\377';printf '\\002\\376' >&2;exit 9"],{encoding:"buffer"})`, Profile{Modules: Modules{Commands: &CommandOptions{Allow: func(string, []string) error { return nil }}}})
		if err == nil || len(result.Diagnostics) != 1 {
			t.Fatalf("result=%#v error=%v", result, err)
		}
		stdout, ok := result.Diagnostics[0].Exception["stdout"].(executionresult.EncodedBytes)
		if !ok || stdout.Encoding != "base64" || stdout.Data != base64.StdEncoding.EncodeToString([]byte{1, 255}) {
			t.Fatalf("stdout detail=%#v", result.Diagnostics[0].Exception["stdout"])
		}
	})

	t.Run("only stdout and stderr accept byte details", func(t *testing.T) {
		result, err := runModuleResult(t, `const e=new Error("byte details");e.code=new Uint8Array([1]);e.signal=new Uint8Array([2]);e.stdout=new Uint8Array([3]);e.stderr=new Uint8Array([4]);throw e`, Profile{})
		if err == nil || len(result.Diagnostics) != 1 {
			t.Fatalf("result=%#v error=%v", result, err)
		}
		details := result.Diagnostics[0].Exception
		if _, ok := details["code"]; ok {
			t.Fatalf("non-scalar code was retained: %#v", details["code"])
		}
		if _, ok := details["signal"]; ok {
			t.Fatalf("non-scalar signal was retained: %#v", details["signal"])
		}
		for key, want := range map[string]byte{"stdout": 3, "stderr": 4} {
			encoded, ok := details[key].(executionresult.EncodedBytes)
			if !ok || encoded.Encoding != "base64" || encoded.Data != base64.StdEncoding.EncodeToString([]byte{want}) {
				t.Fatalf("%s detail=%#v", key, details[key])
			}
		}
	})
}

func TestManagedResultJSONDistinguishesMissingAndNullData(t *testing.T) {
	for _, tc := range []struct {
		data json.RawMessage
		want string
	}{{nil, `{"Code":0}`}, {json.RawMessage("null"), `{"Code":0,"data":null}`}} {
		encoded, err := json.Marshal(Result{Code: 0, Data: tc.data})
		if err != nil || string(encoded) != tc.want {
			t.Fatalf("json=%s error=%v want %s", encoded, err, tc.want)
		}
	}
}

func TestManagedRuntimeCancellationWhileAwaitingDefaultIsDetached(t *testing.T) {
	started := make(chan struct{})
	runtime, err := NewRuntime(context.Background(), Profile{Initialize: func(_ context.Context, b *Bindings) (func(), error) {
		return nil, b.Runtime().Set("startWaiting", func() { close(started) })
	}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		<-started
		runtime.Cancel()
	}()
	result, err := runtime.Run(Source{Name: "memory:cancel-default.mjs", Code: `setInterval(()=>{},1000);export default new Promise(()=>startWaiting())`, Module: true, Virtual: true})
	if !errors.Is(err, context.Canceled) || result.Data != nil || len(result.Diagnostics) != 1 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if got := result.Diagnostics[0]; got.Code != "cancelled" || got.Stage != "execute" {
		t.Fatalf("diagnostic=%#v", got)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}

type resultFailureOutput struct{ err error }

func (w resultFailureOutput) Write([]byte) (int, error) { return 0, w.err }
func (w resultFailureOutput) WriteContext(context.Context, []byte) (int, error) {
	return 0, w.err
}

func TestManagedDiagnosticDoesNotDuplicateExecutionIOFailureAsCleanup(t *testing.T) {
	sentinel := errors.New("output sink failed")
	result, err := runModuleResult(t, `console.log("not written");setInterval(()=>{},1000)`, Profile{Environment: Environment{Stdout: resultFailureOutput{sentinel}}})
	if !errors.Is(err, sentinel) || result.Code != 1 || len(result.Diagnostics) != 1 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if result.Diagnostics[0].Code != "execution_error" || result.Diagnostics[0].Stage != "execute" {
		t.Fatalf("diagnostics=%#v", result.Diagnostics)
	}
}
