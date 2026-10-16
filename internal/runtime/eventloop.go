package runtime

import (
	"context"
	"errors"

	quickjs "github.com/go-quickjs/go-quickjs"
	"github.com/go-quickjs/go-quickjs/stdlib"
)

// eventLoop drives stdlib and engine jobs on the runtime owner.
type eventLoop struct {
	h                  *host
	std                *stdlib.Loop
	cancelDrive        context.CancelFunc
	timers             map[int]bool
	clearTimer         quickjs.Value
	resources          map[uint64]struct{}
	resourceGeneration uint64
	observe            quickjs.Value
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
	l := &eventLoop{h: h, std: stdlib.NewLoop(h.rt), observe: observe, timers: make(map[int]bool), resources: make(map[uint64]struct{})}
	h.rt.OnClose(l.std.Close)
	return l, nil
}

// Await and the subsequent drain use the same stdlib loop, which also drives
// the engine's AsyncWork queue. A rejected root or an unhandled error stops
// that loop even when a server or interval would otherwise keep it alive.
func (l *eventLoop) await(ctx context.Context, value quickjs.Value, fulfilled ...func(quickjs.Value) error) (quickjs.Value, error) {
	var commitErr error
	settled, rejected := false, false
	p, err := l.observe.CallContext(ctx, value, func(result quickjs.Value) {
		settled = true
		if len(fulfilled) != 0 {
			commitErr = fulfilled[0](result)
			if commitErr != nil {
				l.interrupt()
			}
		}
	}, func() {
		settled, rejected = true, true
		l.interrupt()
	})
	if err != nil {
		return quickjs.Value{}, err
	}
	if !rejected && commitErr == nil {
		err = l.drain(ctx)
	}
	if ctx.Err() != nil {
		return quickjs.Value{}, ctx.Err()
	}
	if rejected {
		return p.Await(ctx)
	}
	if commitErr != nil {
		return quickjs.Value{}, commitErr
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
		return &loopFailure{err}
	}
	return nil
}

// stdlib's loop has runtime lifetime, and imported module functions retain it.
// Its active I/O cannot be moved to a fresh loop after input interruption.
func (l *eventLoop) hasStdlibWork() bool {
	return l.std.Pending() || len(l.timers) != 0 || len(l.resources) != 0 || l.std.Context().Err() != nil
}

func (l *eventLoop) hasResourcesSince(generation uint64) bool {
	for id := range l.resources {
		if id > generation {
			return true
		}
	}
	return false
}

// Track upstream timer handles without changing how timers fire. This lets
// REPL interruption detect resources that belong to the stdlib loop.
func (l *eventLoop) trackTimers() error {
	var err error
	l.clearTimer, err = l.h.rt.Get("clearTimeout")
	if err != nil {
		return err
	}
	factory, err := l.h.rt.EvalFile("<noe-timers>", `((armed, cleared) => {
  const timeout = setTimeout, interval = setInterval, cancel = clearTimeout;
  globalThis.setTimeout = (fn, ms, ...args) => {
   if (typeof fn !== "function") return timeout(fn, ms, ...args);
   let id;
   id = timeout((...values) => { cleared(id); return fn(...values); }, ms, ...args);
   armed(id); return id;
  };
  globalThis.setInterval = (fn, ms, ...args) => {
   const id = interval(fn, ms, ...args); armed(id); return id;
  };
  globalThis.clearTimeout = globalThis.clearInterval = id => { cleared(id); cancel(id); };
 })`)
	if err != nil {
		return err
	}
	_, err = factory.Call(func(id int) { l.timers[id] = true }, func(id int) { delete(l.timers, id) })
	return err
}

// Active stdlib I/O ends the session before this path. Flush jobs left by
// interrupted evaluation under the cancelled context without running stale JS.
func (l *eventLoop) drainCancelled(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	err := l.h.rt.RunJobsContext(ctx)
	if errors.Is(err, ctx.Err()) {
		return nil
	}
	return err
}
