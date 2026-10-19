package runtime

import (
	"context"
	"errors"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// eventLoop drives stdlib and engine jobs on the runtime owner.
type eventLoop struct {
	h           *host
	std         *stdlib.Loop
	cancelDrive context.CancelFunc
	observe     quickjs.Value
}

// loopFailure ends the current operation; unrelated pending I/O must be
// cancelled rather than delay its diagnostic indefinitely.
type loopFailure struct{ error }

func (e *loopFailure) Unwrap() error { return e.error }

func newEventLoop(h *host) (*eventLoop, error) {
	observe, err := h.rt.EvalFile("<noe-await>", `(() => {
		const P = Promise, resolve = P.resolve, then = P.prototype.then, apply = Reflect.apply;
		return (value, fulfilled, rejected) => {
			const p = apply(resolve, P, [value]);
			apply(then, p, [fulfilled, rejected]);
			return p;
		};
	})()`)
	if err != nil {
		return nil, err
	}
	l := &eventLoop{h: h, std: stdlib.NewLoop(h.rt), observe: observe}
	h.rt.OnClose(l.std.Close)
	return l, nil
}

// Await and the subsequent drain use the same stdlib loop, which also drives
// the engine's AsyncWork queue. A rejected root or an unhandled error stops
// that loop even when a server or interval would otherwise keep it alive.
func (l *eventLoop) await(ctx context.Context, value quickjs.Value) (quickjs.Value, error) {
	settled, rejected := false, false
	p, err := l.observe.CallContext(ctx, value, func() {
		settled = true
	}, func() {
		settled, rejected = true, true
		l.interrupt()
	})
	if err != nil {
		return quickjs.Value{}, err
	}
	if !rejected {
		err = l.drain(ctx)
	}
	if ctx.Err() != nil {
		return quickjs.Value{}, ctx.Err()
	}
	if rejected {
		result, err := p.Await(ctx)
		return result, retainException(err)
	}
	if err != nil {
		return quickjs.Value{}, err
	}
	if !settled {
		return quickjs.Value{}, quickjs.ErrNeverSettles
	}
	return p.Await(ctx)
}

func (l *eventLoop) interrupt() {
	if l.cancelDrive != nil {
		l.cancelDrive()
	}
}

func (l *eventLoop) takeFailure() error {
	return l.h.takeUnhandled()
}

func (l *eventLoop) drain(ctx context.Context) error {
	if err := l.takeFailure(); err != nil {
		return &loopFailure{err}
	}
	driveCtx, cancel := context.WithCancel(ctx)
	l.cancelDrive = cancel
	defer func() { cancel(); l.cancelDrive = nil }()
	err := l.std.Run(driveCtx)
	if failure := l.takeFailure(); failure != nil {
		return &loopFailure{failure}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if l.h.exitRequested {
		return quickjs.ErrClosed
	}
	// Internal cancellation means the observed root rejected; await unwraps it.
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if err != nil {
		return &loopFailure{retainException(err)}
	}
	return nil
}
