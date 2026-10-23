package nativeio

import (
	"context"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
)

func signalRuntime(t *testing.T) (*quickjs.Runtime, *Host) {
	t.Helper()
	rt, h, _ := streamRuntime(t, Options{})
	return rt, h
}

func TestSignalsImportIsInactiveAndUnsupportedNameThrowsCode(t *testing.T) {
	rt, _ := signalRuntime(t)
	if _, err := rt.EvalModule("signals-import.mjs", `
		import {subscribe} from "noe:signals";
		let code;
		try { subscribe("SIGKILL"); } catch (error) { code = error.code; }
		globalThis.unsupportedSignalCode = code;
	`); err != nil {
		t.Fatal(err)
	}
	code, err := rt.Eval(`unsupportedSignalCode`)
	if err != nil || code.String() != "ERR_UNSUPPORTED_SIGNAL" {
		t.Fatalf("unsupported signal code = %v, err=%v", code, err)
	}
	if rt.Busy() {
		t.Fatalf("module import or rejected subscription activated runtime: busy=%v", rt.Busy())
	}
}

func TestSignalQueuedNotificationClosesAndResolvesPendingNextFIFO(t *testing.T) {
	rt, h := signalRuntime(t)
	if _, err := rt.EvalModule("signals-pending.mjs", `
		import {subscribe} from "noe:signals";
		globalThis.signalIterator = subscribe("SIGINT");
		globalThis.signalNext1 = signalIterator.next();
		globalThis.signalNext2 = signalIterator.next();
	`); err != nil {
		t.Fatal(err)
	}
	s := findSignalSubscription(t, h)
	if !rt.Busy() {
		t.Fatalf("subscription did not keep owner alive: busy=%v", rt.Busy())
	}

	// The subscription has two pending next requests. A signal resolves the
	// oldest first; the next notification resolves the following request.
	s.enqueue()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	first, err := mustEval(t, rt, "signalNext1").Await(ctx)
	if err != nil {
		t.Fatalf("first next: %v", err)
	}
	assertSignalResult(t, first, "SIGINT", false)
	s.enqueue()
	second, err := mustEval(t, rt, "signalNext2").Await(ctx)
	if err != nil {
		t.Fatalf("second next: %v", err)
	}
	assertSignalResult(t, second, "SIGINT", false)

	closeResult, err := rt.Eval(`signalIterator.close()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closeResult.Await(ctx); err != nil {
		t.Fatalf("close: %v", err)
	}
	if rt.Busy() {
		t.Fatalf("close retained runtime: busy=%v", rt.Busy())
	}
	if got := len(h.resources); got != 3 {
		t.Fatalf("owned resources after close = %d, want only stdio endpoints", got)
	}
}

func TestSignalsWithoutPendingNextCoalesceOneNotification(t *testing.T) {
	rt, h := signalRuntime(t)
	if _, err := rt.EvalModule("signals-coalesce.mjs", `
		import {subscribe} from "noe:signals";
		globalThis.signalIterator = subscribe("SIGINT");
	`); err != nil {
		t.Fatal(err)
	}
	s := findSignalSubscription(t, h)
	s.enqueue()
	s.enqueue()
	s.enqueue()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	first, err := mustEval(t, rt, "signalIterator.next()").Await(ctx)
	if err != nil {
		t.Fatalf("next after queued signals: %v", err)
	}
	assertSignalResult(t, first, "SIGINT", false)

	shortCtx, stopShort := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopShort()
	if _, err := mustEval(t, rt, "signalIterator.next()").Await(shortCtx); err != context.DeadlineExceeded {
		t.Fatalf("second next error = %v, want a pending notification timeout", err)
	}
	if _, err := rt.Eval(`signalIterator.close()`); err != nil {
		t.Fatal(err)
	}
}

func TestSignalCloseResolvesAllPendingNextAndReturnIsIdempotent(t *testing.T) {
	rt, _ := signalRuntime(t)
	if _, err := rt.EvalModule("signals-close.mjs", `
		import {subscribe} from "noe:signals";
		globalThis.signalIterator = subscribe("SIGINT");
		globalThis.signalNext1 = signalIterator.next();
		globalThis.signalNext2 = signalIterator.next();
	`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	returned, err := rt.Eval(`signalIterator.return()`)
	if err != nil {
		t.Fatal(err)
	}
	returnResult, err := returned.Await(ctx)
	if err != nil {
		t.Fatalf("iterator return: %v", err)
	}
	assertSignalResult(t, returnResult, "", true)
	first, err := mustEval(t, rt, "signalNext1").Await(ctx)
	if err != nil {
		t.Fatalf("first pending next after return: %v", err)
	}
	assertSignalResult(t, first, "", true)
	second, err := mustEval(t, rt, "signalNext2").Await(ctx)
	if err != nil {
		t.Fatalf("second pending next after return: %v", err)
	}
	assertSignalResult(t, second, "", true)
	if rt.Busy() {
		t.Fatalf("return retained runtime: busy=%v", rt.Busy())
	}
	if _, err := rt.Eval(`signalIterator.close()`); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestSignalRuntimeCloseDoesNotNeedJSDelivery(t *testing.T) {
	rt, h := signalRuntime(t)
	if _, err := rt.EvalModule("signals-runtime-close.mjs", `
		import {subscribe} from "noe:signals";
		globalThis.signalIterator = subscribe("SIGINT");
		globalThis.signalNext = signalIterator.next();
	`); err != nil {
		t.Fatal(err)
	}
	s := findSignalSubscription(t, h)
	if err := rt.Close(); err != nil {
		t.Fatalf("runtime close: %v", err)
	}
	if !s.closed || rt.Busy() {
		t.Fatalf("runtime close left subscription active: closed=%v active=%v", s.closed, rt.Busy())
	}
}

func findSignalSubscription(t *testing.T, h *Host) *signalSubscription {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for resource := range h.resources {
		if s, ok := resource.(*signalSubscription); ok {
			return s
		}
	}
	t.Fatal("signal subscription not owned by native host")
	return nil
}

func mustEval(t *testing.T, rt *quickjs.Runtime, src string) quickjs.Value {
	t.Helper()
	value, err := rt.Eval(src)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func assertSignalResult(t *testing.T, result quickjs.Value, name string, done bool) {
	t.Helper()
	doneValue, err := result.Get("done")
	if err != nil || doneValue.Bool() != done {
		t.Fatalf("iterator done=%v, err=%v, want %v", doneValue, err, done)
	}
	value, err := result.Get("value")
	if err != nil {
		t.Fatal(err)
	}
	if done {
		if !value.IsUndefined() {
			t.Fatalf("closed iterator value = %v, want undefined", value)
		}
	} else if value.String() != name {
		t.Fatalf("signal iterator value = %q, want %q", value.String(), name)
	}
}
