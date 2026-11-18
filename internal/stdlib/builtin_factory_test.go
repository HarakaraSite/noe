package stdlib

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestBuiltinProgramsKeepRuntimeStateSeparate(t *testing.T) {
	tests := []struct {
		name   string
		random byte
		opts   []quickjs.Option
	}{
		{name: "standard-1", random: 1},
		{name: "standard-2", random: 2},
		{name: "node", random: 3, opts: []quickjs.Option{quickjs.WithNodeQuirks()}},
		{name: "debugger", random: 4, opts: []quickjs.Option{quickjs.WithDebugger()}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			rt := quickjs.New(test.opts...)
			defer rt.Close()
			cfg := Config{Random: bytes.NewReader([]byte{test.random}), Loop: NewLoop(rt)}
			defer cfg.Loop.Close()
			EnableBuiltinProgramReuse(&cfg)
			if err := InstallProfile(rt, cfg, nil); err != nil {
				t.Fatal(err)
			}
			result, err := rt.Eval(`
if ("testMarker" in URL.prototype) throw new Error("shared URL prototype");
URL.prototype.testMarker = true;
if (new Response(new ReadableStream()).body.constructor !== ReadableStream) {
  throw new Error("stream from a different runtime");
}
crypto.getRandomValues(Buffer.alloc(1))[0];
`)
			if err != nil || result.Int() != int(test.random) {
				t.Fatalf("random byte = %v, %v, want %d", result, err, test.random)
			}
		})
	}
}

func TestBuiltinFactoryCompileErrorUsesRuntimeParsing(t *testing.T) {
	mode := builtinEvaluation{reusePrograms: true}
	factory := fixedBuiltin("<builtin-syntax>", `"use strict"; function f() {} if (false) f() = 1; host => host.cloned`)

	rt := quickjs.New()
	defer rt.Close()
	_, err := mode.withHost(rt, factory, quickjs.Value{})
	var syntax *quickjs.SyntaxError
	if !errors.As(err, &syntax) || !strings.Contains(err.Error(), factory.name) {
		t.Fatalf("standard runtime error = %v, want a SyntaxError with %s", err, factory.name)
	}
	rt.Close()
	if _, err := mode.withHost(rt, factory, quickjs.Value{}); !errors.Is(err, quickjs.ErrClosed) {
		t.Fatalf("closed runtime error = %v, want ErrClosed", err)
	}

	node := quickjs.New(quickjs.WithNodeQuirks())
	defer node.Close()
	host := node.NewObject()
	if err := host.Set("cloned", 42); err != nil {
		t.Fatal(err)
	}
	value, err := mode.withHost(node, factory, host)
	if err != nil || value.Int() != 42 {
		t.Fatalf("Node quirks evaluation = %v, %v, want 42", value, err)
	}
}

func TestBuiltinFactoryCancellation(t *testing.T) {
	mode := builtinEvaluation{reusePrograms: true}
	factory := fixedBuiltin("<builtin-abort>", `if (globalThis.iterate) while (true) {} () => 42`)

	rt := quickjs.New()
	defer rt.Close()
	if err := rt.Set("iterate", true); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	close(stop)
	rt.AbortOn(stop)
	if _, err := mode.withHost(rt, factory, quickjs.Value{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted evaluation = %v, want context.Canceled", err)
	}

	fresh := quickjs.New()
	defer fresh.Close()
	value, err := mode.withHost(fresh, factory, quickjs.Value{})
	if err != nil || value.Int() != 42 {
		t.Fatalf("fresh runtime evaluation = %v, %v, want 42", value, err)
	}
}
