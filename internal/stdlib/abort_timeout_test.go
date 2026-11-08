package stdlib

import (
	"context"
	"fmt"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestAbortTimeoutLongDelayAndScopeCleanup(t *testing.T) {
	for _, delay := range []uint64{2147483648, 9223372036855, 9007199254740991} {
		t.Run(fmt.Sprint(delay), func(t *testing.T) {
			rt := quickjs.New()
			defer rt.Close()
			loop := NewLoop(rt)
			defer loop.Close()
			scope, err := NewExecutionScope(loop, t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer scope.Close()
			if err := Install(rt, Config{Loop: loop}); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.Eval(fmt.Sprintf(`globalThis.timeout=AbortSignal.timeout(%d);clearTimeout(1);`, delay)); err != nil {
				t.Fatal(err)
			}
			if len(loop.timers.entries) != 1 {
				t.Fatal("public clearTimeout canceled an internal abort deadline")
			}
			first := loop.timers.entries[0]
			if !first.at.After(time.Now().Add(time.Hour)) {
				t.Fatal("long delay wrapped to an immediate deadline")
			}
			// Advance one internal stage without waiting weeks or overflowing Duration.
			deadline := first.at
			fired := loop.timers.due(deadline)
			if err := loop.fire(context.Background(), fired, deadline); err != nil {
				t.Fatal(err)
			}
			value, err := rt.Eval(`timeout.aborted`)
			if err != nil || value.Bool() {
				t.Fatalf("intermediate stage aborted signal: %v, %v", value, err)
			}
			if len(loop.timers.entries) != 1 || !loop.timers.entries[0].at.After(deadline) {
				t.Fatal("long timeout was not rearmed")
			}
			if delay == 2147483648 {
				last := loop.timers.entries[0].at
				if err := loop.fire(context.Background(), loop.timers.due(last), last); err != nil {
					t.Fatal(err)
				}
				value, err := rt.Eval(`timeout.reason.name`)
				if err != nil || value.String() != "TimeoutError" {
					t.Fatalf("final staged deadline: %v, %v", value, err)
				}
			}
			scope.Close()
			if len(loop.timers.entries) != 0 || len(loop.timers.byID) != 0 {
				t.Fatal("scope teardown retained an abort deadline")
			}
		})
	}
}

func TestAbortTimeoutPrivateTimerAndLegacyInstaller(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	defer loop.Close()
	if err := Install(rt, Config{Loop: loop}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`globalThis.timeout=AbortSignal.timeout(0);clearTimeout(1);globalThis.reason;timeout.onabort=()=>{reason=timeout.reason.name;};`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	value, err := rt.Eval(`reason`)
	if err != nil || value.String() != "TimeoutError" {
		t.Fatalf("private timer: %v, %v", value, err)
	}
	legacy := quickjs.New()
	defer legacy.Close()
	if err := WebAPIs(legacy, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Eval(`const controller=new AbortController();controller.abort();if(controller.signal.reason.name!=="AbortError")throw Error("legacy reason");let error;try{AbortSignal.timeout(0)}catch(value){error=value}if(!(error instanceof TypeError))throw Error("missing Loop was not explicit");`); err != nil {
		t.Fatal(err)
	}
}
