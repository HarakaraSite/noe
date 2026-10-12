package runtime

import (
	"context"
	"errors"
	"testing"
)

// Make the cancellation window deterministic: the formatter has queued a JS
// reaction and returned normally, so the engine has not observed an interrupt.
func TestSessionCancellationAfterFormatterReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	value, err := s.h.rt.EvalContext(ctx, `({get value(){ Promise.resolve().then(() => { globalThis.staleCleanupReaction = 1; while(true){} }); return 1; }})`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.h.inspect.CallContext(ctx, value); err != nil {
		t.Fatal(err)
	}
	if !s.h.rt.HasPendingJobs() {
		t.Fatal("formatter did not queue its reaction")
	}
	cancel()
	result := s.finishCancelledInput(ctx, "<cleanup-test>", Evaluation{Err: ctx.Err()})
	if result.Exit || !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("cleanup=%+v", result)
	}
	for _, tc := range []struct{ source, output string }{
		{`typeof staleCleanupReaction`, `'undefined'`},
		{`42`, `42`},
	} {
		result := s.Eval(context.Background(), tc.source)
		if result.Exit || result.Err != nil || result.Output != tc.output {
			t.Fatalf("next input=%s result=%+v", tc.source, result)
		}
	}
}
