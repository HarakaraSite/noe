package stdlib

import (
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// armAbortTimeout uses the ordinary timer queue and execution ownership, while
// retaining Web IDL's accepted unsigned-long-long millisecond range (0..2^53-1
// for JavaScript). The binding validates and truncates before calling here.
func armAbortTimeout(rt *quickjs.Runtime, loop *Loop, callback quickjs.Value, milliseconds float64) error {
	if loop == nil {
		return rt.Throw(rt.NewError("TypeError", "AbortSignal.timeout requires an event loop"))
	}
	scope := loop.executionScope()
	if scope != nil && scope.isClosing() {
		return errExecutionScopeClosed
	}
	remaining := uint64(milliseconds)
	delay := min(remaining, uint64(maxTimeoutMS))
	loop.nextID++
	loop.timers.add(&timer{
		id: loop.nextID, at: time.Now().Add(time.Duration(delay) * time.Millisecond),
		fn: callback, actx: rt.AsyncContext(), scope: scope,
		remainingMS: remaining - delay, internal: true,
	})
	return nil
}
