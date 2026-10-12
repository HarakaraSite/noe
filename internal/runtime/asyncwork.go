package runtime

import (
	"context"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// startAsyncWork ties host work to both the current input and the runtime's
// lifetime. Runtime.Go only receives the runtime-lifetime context, which
// would leave work from a cancelled REPL input running until session close.
func (h *host) startAsyncWork(fn func(context.Context) (any, error)) quickjs.Value {
	return h.loop.startAsyncWork(fn)
}

func (l *eventLoop) startAsyncWork(fn func(context.Context) (any, error)) quickjs.Value {
	h := l.h
	runtimeCtx := h.rt.Context()
	inputCtx := h.inputContext
	if inputCtx == nil {
		inputCtx = runtimeCtx
	}
	ctx, cancel := context.WithCancel(inputCtx)
	stopRuntime := context.AfterFunc(runtimeCtx, cancel)
	promise := h.rt.NewPromise()
	work := h.rt.StartAsyncWork()
	stopOnCancel := context.AfterFunc(ctx, work.Done)

	go func() {
		value, err := fn(ctx)
		stopRuntime()
		stopOnCancel()
		work.Complete(func(workErr error) {
			// Done may have ended the work on cancellation, in which case
			// Complete calls us on this goroutine with ErrWorkDone. Do not
			// touch the runtime or Promise unless completion belongs to an
			// open runtime and a live input.
			if workErr != nil || inputCtx.Err() != nil || runtimeCtx.Err() != nil {
				return
			}
			if err != nil {
				promise.RejectError(err)
				return
			}
			if result, ok := value.(asyncResult); ok {
				value = result.jsValue(h.rt)
			}
			if err := promise.Resolve(value); err != nil {
				promise.RejectError(err)
			}
		})
		cancel()
	}()

	return promise.Value()
}
