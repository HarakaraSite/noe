package jsexec

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagedModuleReturnsDetachedDefaultData(t *testing.T) {
	cases := []struct {
		name string
		code string
		data string
	}{
		{name: "no default", code: `export const value=42;`},
		{name: "explicit null", code: `export default null;`, data: "null"},
		{name: "object", code: `export default {answer:42};`, data: `{"answer":42}`},
		{name: "top-level await", code: `export default await Promise.resolve(42);`, data: "42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			result, err := Execute(ctx, Source{Name: "memory:result", Virtual: true, Module: true, Code: tc.code}, Profile{})
			if err != nil || result.Code != 0 || result.Phase != "complete" {
				t.Fatalf("result=%#v error=%v", result, err)
			}
			if string(result.Data) != tc.data {
				t.Fatalf("data=%s, want %q", result.Data, tc.data)
			}
		})
	}
}

func TestManagedModuleRejectsNonJSONDefaultAfterSingleEvaluation(t *testing.T) {
	cases := []struct {
		name      string
		code      string
		wantError string
	}{
		{name: "promise", code: `export default Promise.resolve(42);`, wantError: "export default await"},
		{name: "rejected promise", code: `export default Promise.reject(new Error("inner rejected"));`, wantError: "inner rejected"},
		{name: "rejected promise after top-level await", code: `await Promise.resolve();export default Promise.reject(new Error("inner rejected"));`, wantError: "inner rejected"},
		{name: "pending promise", code: `export default new Promise(()=>{});`, wantError: "export default await"},
		{name: "undefined", code: `export default undefined;`},
		{name: "function", code: `export default function () {};`},
		{name: "symbol", code: `export default Symbol("result");`},
		{name: "bigint", code: `export default 1n;`},
		{name: "cycle", code: `const value={};value.self=value;export default value;`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var evaluations atomic.Int32
			profile := Profile{Initialize: func(_ context.Context, b *Bindings) (func(), error) {
				return nil, b.Runtime().Set("markEvaluated", func() { evaluations.Add(1) })
			}}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			result, err := Execute(ctx, Source{
				Name: "memory:unserializable", Virtual: true, Module: true,
				Code: `markEvaluated();` + tc.code,
			}, profile)
			if err == nil || result.Code != 1 || result.Phase != "serialize" || len(result.Data) != 0 || len(result.Diagnostics) != 1 {
				t.Fatalf("result=%#v error=%v", result, err)
			}
			if result.Diagnostics[0].Code != codeJSResult || result.Diagnostics[0].Phase != "serialize" {
				t.Fatalf("serialize diagnostic = %#v", result.Diagnostics[0])
			}
			if evaluations.Load() != 1 {
				t.Fatalf("module body ran %d times, want 1", evaluations.Load())
			}
			if tc.wantError != "" && !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error %q missing from failure: %v", tc.wantError, err)
			}
		})
	}
}

func TestManagedModuleDefaultPromiseDoesNotHideUnrelatedUnhandledRejection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result, err := Execute(ctx, Source{
		Name: "memory:unrelated-rejection", Virtual: true, Module: true,
		Code: `Promise.reject(new Error("unrelated rejected"));export default Promise.reject(new Error("inner rejected"));`,
	}, Profile{})
	if err == nil || result.Code != 1 || result.Phase != "evaluate" || len(result.Diagnostics) != 1 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if result.Diagnostics[0].Code != codeJSException || !strings.Contains(result.Diagnostics[0].Message, "unrelated rejected") {
		t.Fatalf("unrelated unhandled rejection diagnostic = %#v", result.Diagnostics[0])
	}
}

func TestManagedModuleFailureReturnsStructuredSourceFrames(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	result, err := Execute(ctx, Source{
		Name: "memory:throws.mjs", Virtual: true, Module: true,
		Code: `function fail(){const error=new Error("module boom");error.code="E_FIXTURE";error.stdout="captured";throw error;}fail();export default 42;`,
	}, Profile{})
	if err == nil || result.Code != 1 || result.Phase != "evaluate" || len(result.Diagnostics) != 1 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != codeJSException || diagnostic.Source != "memory:throws.mjs" || diagnostic.Line == 0 || len(diagnostic.Stack) == 0 {
		t.Fatalf("evaluation diagnostic = %#v", diagnostic)
	}
	if diagnostic.Exception["code"] != "E_FIXTURE" || diagnostic.Exception["stdout"] != "captured" {
		t.Fatalf("exception details = %#v", diagnostic.Exception)
	}
}

func TestManagedResultPreservesNativeCauseAlongsideFacts(t *testing.T) {
	cause := errors.New("host result cause")
	profile := Profile{Initialize: func(_ context.Context, b *Bindings) (func(), error) {
		return nil, b.Runtime().Set("fail", func() error { return cause })
	}}
	result, err := Execute(t.Context(), Source{
		Name: "memory:host-error", Virtual: true, Module: true,
		Code: `fail();export default 42;`,
	}, profile)
	if result.Code != 1 || err == nil || !errors.Is(err, cause) || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != codeJSHost {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}
