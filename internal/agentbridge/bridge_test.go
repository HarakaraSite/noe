package agentbridge

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func bridgeRuntime(t *testing.T) (*quickjs.Runtime, *stdlib.Loop) {
	t.Helper()
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	t.Cleanup(func() { _ = rt.Close() })
	return rt, loop
}

func TestInstallValidatesInputsAndRejectsSecondBinding(t *testing.T) {
	rt, loop := bridgeRuntime(t)
	if _, err := Install(nil, rt, Config{Loop: loop}); err == nil {
		t.Fatal("nil context was accepted")
	}
	if _, err := Install(context.Background(), rt, Config{}); err == nil {
		t.Fatal("nil loop was accepted")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Install(canceled, rt, Config{Loop: loop}); err == nil {
		t.Fatal("canceled context was accepted")
	}
	if _, err := Install(context.Background(), nil, Config{Loop: loop}); err == nil {
		t.Fatal("nil runtime was accepted")
	}

	binding, err := Install(context.Background(), rt, Config{Loop: loop})
	if err != nil {
		t.Fatal(err)
	}
	binding.Close()
	if _, err := Install(context.Background(), rt, Config{Loop: loop}); err == nil {
		t.Fatal("closed binding allowed a second installation on its runtime")
	}
}

func TestBindingCloseCancelsAndJoinsWithoutClosingBorrowedRuntime(t *testing.T) {
	rt, loop := bridgeRuntime(t)
	binding, err := Install(context.Background(), rt, Config{Loop: loop})
	if err != nil {
		t.Fatal(err)
	}

	const callers = 8
	var wait sync.WaitGroup
	wait.Add(callers)
	for range callers {
		go func() {
			defer wait.Done()
			binding.Close()
		}()
	}
	wait.Wait()
	if binding.Context().Err() == nil {
		t.Fatal("binding context remained live after Close")
	}
	if rt.Context().Err() != nil {
		t.Fatal("binding Close canceled the caller's runtime")
	}
	if loop.Context().Err() != nil {
		t.Fatal("binding Close canceled the caller's loop")
	}
}

func TestInstallUsesBuiltinSourceBeforeFallbackLoader(t *testing.T) {
	rt, loop := bridgeRuntime(t)
	var fallbackCalls atomic.Int32
	var unexpected string
	binding, err := Install(context.Background(), rt, Config{
		Loop: loop,
		Load: func(specifier, _ string) (string, string, error) {
			fallbackCalls.Add(1)
			switch specifier {
			case "fixture:entry":
				return `import {run_agent} from "noe:agent";
import {value} from "fixture:module";
globalThis.bridgeResult = typeof run_agent + ":" + value;`, specifier, nil
			case "fixture:module":
				return "export const value = 42;", specifier, nil
			default:
				unexpected = specifier
				return "", "", errors.New("unexpected fallback module")
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(binding.Close)

	promise, err := rt.Eval(`import("fixture:entry")`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := promise.Await(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := rt.Get("bridgeResult")
	if err != nil || result.String() != "function:42" {
		t.Fatalf("result=%q error=%v", result.String(), err)
	}
	if got := fallbackCalls.Load(); got != 2 {
		t.Fatalf("fallback calls=%d, want 2 (builtins must use bridge sources)", got)
	}
	if unexpected != "" {
		t.Fatalf("unexpected fallback specifier %q", unexpected)
	}
}
