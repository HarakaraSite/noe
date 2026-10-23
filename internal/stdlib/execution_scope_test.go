package stdlib

import (
	"context"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
)

func TestExecutionScopeCancelsAndJoinsWithoutClosingBorrowedRuntime(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scope, err := NewExecutionScope(loop, ctx)
	if err != nil {
		t.Fatal(err)
	}

	cleaned := make(chan struct{})
	joined := make(chan struct{})
	if err := scope.AddCleanup(func() {
		if scope.Context().Err() == nil {
			t.Error("cleanup ran before execution cancellation")
		}
		close(cleaned)
	}); err != nil {
		t.Fatal(err)
	}
	if err := scope.Go(func(ctx context.Context) {
		<-ctx.Done()
		close(joined)
	}); err != nil {
		t.Fatal(err)
	}

	cancel()
	scope.Close()
	select {
	case <-cleaned:
	default:
		t.Fatal("scope cleanup did not run")
	}
	select {
	case <-joined:
	default:
		t.Fatal("scope Close returned before Go work joined")
	}
	if loop.Context().Err() != nil || rt.Context().Err() != nil || loop.isClosed() {
		t.Fatal("closing the execution scope ended its borrowed loop or runtime")
	}
	if _, err := NewExecutionScope(loop, context.Background()); err == nil {
		t.Fatal("a second execution scope was attached to the same loop")
	}
	if _, err := rt.Eval(`globalThis.scopeBorrowedRuntimeStillWorks = 1`); err != nil {
		t.Fatalf("borrowed runtime did not remain usable: %v", err)
	}
}

func TestExecutionScopeDropsQueuedPostAndTimers(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	scope, err := NewExecutionScope(loop, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := Timers(rt, loop); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`
		globalThis.scopeTimerRuns = 0;
		setTimeout(() => scopeTimerRuns++, 0);
	`); err != nil {
		t.Fatal(err)
	}

	posted := false
	loop.postExecution(func() { posted = true })
	scope.Close()

	callerPosted := false
	loop.Post(func() { callerPosted = true })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if posted {
		t.Fatal("an execution callback from the closed scope ran later on the borrowed loop")
	}
	if !callerPosted {
		t.Fatal("public Loop.Post stopped working after the execution scope closed")
	}
	value, err := rt.Get("scopeTimerRuns")
	if err != nil {
		t.Fatal(err)
	}
	if value.Int() != 0 {
		t.Fatalf("a timer from the closed scope ran %d times", value.Int())
	}
	if loop.Context().Err() != nil || loop.isClosed() {
		t.Fatal("scope cleanup closed the borrowed loop")
	}
}
