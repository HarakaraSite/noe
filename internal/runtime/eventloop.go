package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// eventLoop drives the engine's host queue and jobs on the runtime owner.
// Its registry contains Go cleanup functions, not another JS queue or poller.
type eventLoop struct {
	h       *host
	observe quickjs.Value
	failure error // owner only
	mu      sync.Mutex
	sources map[*eventSource]struct{}
}

// loopFailure ends the current operation; unrelated pending I/O must be
// cancelled rather than delay its diagnostic indefinitely.
type loopFailure struct{ error }

func (e *loopFailure) Unwrap() error { return e.error }

func newEventLoop(h *host) (*eventLoop, error) {
	observe, err := h.rt.EvalFile("<noe-await>", `(() => {
		const P = Promise, resolve = P.resolve, then = P.prototype.then, apply = Reflect.apply;
		return (value, notify) => {
			const p = apply(resolve, P, [value]);
			apply(then, p, [notify, notify]);
			return p;
		};
	})()`)
	if err != nil {
		return nil, err
	}
	l := &eventLoop{h: h, observe: observe, sources: make(map[*eventSource]struct{})}
	h.rt.OnClose(l.stopSources)
	return l, nil
}

// The final engine Await only unwraps an already-settled promise. Waiting
// belongs to NOE, so errors cannot hide behind a pending top-level await.
func (l *eventLoop) await(ctx context.Context, value quickjs.Value) (quickjs.Value, error) {
	settled := false
	p, err := l.observe.CallContext(ctx, value, func() { settled = true })
	if err != nil {
		return quickjs.Value{}, err
	}
	for {
		if err := l.takeFailure(); err != nil {
			l.stopSources()
			return quickjs.Value{}, &loopFailure{err}
		}
		if err := ctx.Err(); err != nil {
			return quickjs.Value{}, err
		}
		if settled {
			return p.Await(ctx)
		}
		if !l.h.rt.Busy() {
			return quickjs.Value{}, quickjs.ErrNeverSettles
		}
		if err := l.turn(ctx); err != nil {
			return quickjs.Value{}, err
		}
	}
}

func (l *eventLoop) takeFailure() error {
	err := errors.Join(l.failure, l.h.takeUnhandled())
	l.failure = nil
	return err
}

func (l *eventLoop) turn(ctx context.Context) error {
	select {
	case <-l.h.rt.Wake():
		return l.h.rt.RunJobsContext(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *eventLoop) drain(ctx context.Context) error {
	for {
		if err := l.takeFailure(); err != nil {
			l.stopSources()
			return &loopFailure{err}
		}
		if !l.h.rt.Busy() {
			return nil
		}
		if err := l.turn(ctx); err != nil {
			return err
		}
	}
}

// The engine has already discarded interrupted JS. Flush only guarded host
// completions after stopping the continuous resources for this input.
func (l *eventLoop) drainCancelled(ctx context.Context) error {
	l.stopSources()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := l.h.rt.RunJobsContext(ctx); err != nil && !errors.Is(err, ctx.Err()) {
		return err
	}
	for l.h.rt.Busy() {
		select {
		case <-l.h.rt.Wake():
			if err := l.h.rt.RunJobsContext(ctx); err != nil && !errors.Is(err, ctx.Err()) {
				return err
			}
		case <-l.h.rt.Context().Done():
			return quickjs.ErrClosed
		}
	}
	return nil
}

// A source is created on the owner, posted to from any goroutine, and
// finished when its Go resource has drained. Abort never waits for JS.
type eventSource struct {
	loop       *eventLoop
	work       *quickjs.AsyncWork
	ctx        context.Context
	inputCtx   context.Context
	runtimeCtx context.Context
	cancel     context.CancelFunc
	stop       func()
	finishOnce sync.Once
	aborted    atomic.Bool
	hooksMu    sync.Mutex
	hooks      []func() bool
}

func (h *host) startEventSource(stop func()) *eventSource {
	runtimeCtx := h.rt.Context()
	inputCtx := h.inputContext
	if inputCtx == nil {
		inputCtx = runtimeCtx
	}
	ctx, cancel := context.WithCancel(inputCtx)
	s := &eventSource{loop: h.loop, work: h.rt.StartAsyncWork(), ctx: ctx,
		inputCtx: inputCtx, runtimeCtx: runtimeCtx, cancel: cancel, stop: stop}
	h.loop.mu.Lock()
	h.loop.sources[s] = struct{}{}
	h.loop.mu.Unlock()
	// Cancellation can start Abort before AfterFunc returns. Cleanup must
	// see both installed hooks, so hold this lock while registering them.
	s.hooksMu.Lock()
	s.hooks = []func() bool{context.AfterFunc(inputCtx, s.Abort), context.AfterFunc(runtimeCtx, s.Abort)}
	s.hooksMu.Unlock()
	return s
}

func (s *eventSource) Post(fn func()) {
	s.work.Post(func(err error) {
		if err != nil || s.aborted.Load() || s.inputCtx.Err() != nil || s.runtimeCtx.Err() != nil {
			return
		}
		fn()
	})
}

func (s *eventSource) Fail(err error) {
	if err != nil {
		s.Post(func() { s.loop.failure = errors.Join(s.loop.failure, err) })
	}
}

// Done preserves queued callbacks, as AsyncWork.Done does.
func (s *eventSource) Done()  { s.finish(false) }
func (s *eventSource) Abort() { s.finish(true) }

func (s *eventSource) finish(abort bool) {
	s.finishOnce.Do(func() {
		s.aborted.Store(abort)
		s.hooksMu.Lock()
		for _, hook := range s.hooks {
			hook()
		}
		s.hooksMu.Unlock()
		s.cancel()
		if abort && s.stop != nil {
			s.stop()
		}
		s.work.Done()
		s.loop.mu.Lock()
		delete(s.loop.sources, s)
		s.loop.mu.Unlock()
	})
}

func (l *eventLoop) stopSources() {
	l.mu.Lock()
	sources := make([]*eventSource, 0, len(l.sources))
	for s := range l.sources {
		sources = append(sources, s)
	}
	l.mu.Unlock()
	for _, s := range sources {
		s.Abort()
	}
}
