package stdlib

import (
	"context"
	"errors"
	"sync"
)

var errExecutionScopeClosed = errors.New("stdlib: execution scope is closing")

// ExecutionScope owns the Go work started for one execution attached to a
// borrowed Loop. It does not own the Loop or its Runtime.
type ExecutionScope struct {
	loop     *Loop
	ctx      context.Context
	cancel   context.CancelFunc
	stopLoop func() bool

	mu        sync.Mutex
	closing   bool
	closed    bool
	cleanups  []*executionCleanup
	workers   sync.WaitGroup
	closeDone chan struct{}
}

type executionCleanup struct{ fn func() }

// NewExecutionScope attaches an execution context and Go-resource lifetime to
// loop. The loop and its runtime remain borrowed and are never closed here.
func NewExecutionScope(loop *Loop, ctx context.Context) (*ExecutionScope, error) {
	if loop == nil {
		return nil, errors.New("stdlib: execution scope needs an event loop")
	}
	if ctx == nil {
		return nil, errors.New("stdlib: execution scope needs a context")
	}
	if err := loop.Context().Err(); err != nil {
		return nil, errors.Join(errors.New("stdlib: execution loop is closed"), err)
	}

	scopeCtx, cancel := context.WithCancel(ctx)
	s := &ExecutionScope{
		loop: loop, ctx: scopeCtx, cancel: cancel,
		closeDone: make(chan struct{}),
	}
	loop.mu.Lock()
	if loop.closed {
		loop.mu.Unlock()
		cancel()
		return nil, errors.New("stdlib: execution loop is closed")
	}
	if loop.scope != nil {
		loop.mu.Unlock()
		cancel()
		return nil, errors.New("stdlib: loop already has an execution scope")
	}
	loop.scope = s
	loop.mu.Unlock()

	// A runtime may close independently of the execution owner. This callback
	// only cancels Go work; all cleanup that may join workers is done by Close.
	s.stopLoop = context.AfterFunc(loop.Context(), cancel)
	return s, nil
}

// Context is the context for the execution's host operations.
func (s *ExecutionScope) Context() context.Context {
	if s == nil {
		return context.Background()
	}
	return s.ctx
}

// AddCleanup registers a Go-only stop action. Actions run in reverse order
// after cancellation and before the scope joins its goroutines.
func (s *ExecutionScope) AddCleanup(fn func()) error {
	_, err := s.AddCleanupHandle(fn)
	return err
}

// AddCleanupHandle registers a stop action and returns a function that removes
// it before Close. Removal is useful when a resource is explicitly closed.
func (s *ExecutionScope) AddCleanupHandle(fn func()) (func(), error) {
	if s == nil || fn == nil {
		return nil, errors.New("stdlib: invalid execution cleanup")
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return nil, errExecutionScopeClosed
	}
	entry := &executionCleanup{fn: fn}
	s.cleanups = append(s.cleanups, entry)
	s.mu.Unlock()
	remove := func() {
		s.mu.Lock()
		if !s.closing {
			entry.fn = nil
		}
		s.mu.Unlock()
	}
	return remove, nil
}

// Go starts work owned by this execution. Close first cancels Context, runs
// registered stop actions, and then joins every admitted function.
func (s *ExecutionScope) Go(fn func(context.Context)) error {
	if s == nil || fn == nil {
		return errors.New("stdlib: invalid execution work")
	}
	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()
		return errExecutionScopeClosed
	}
	s.workers.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.workers.Done()
		fn(s.ctx)
	}()
	return nil
}

// Close must be called on the Loop's owner after its current drive returns. It
// ends this execution's context, stops and joins its Go-owned work, and leaves
// the borrowed Loop and Runtime available to their owner. It does not wait for
// callbacks queued to Loop.Post to be delivered.
func (s *ExecutionScope) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closing {
		done := s.closeDone
		s.mu.Unlock()
		<-done
		return
	}
	s.closing = true
	cleanups := append([]*executionCleanup{}, s.cleanups...)
	s.cleanups = nil
	s.mu.Unlock()

	s.cancel()
	if s.stopLoop != nil {
		s.stopLoop()
	}
	s.loop.discardScopeTimers(s)
	for i := len(cleanups) - 1; i >= 0; i-- {
		fn := cleanups[i].fn
		cleanups[i].fn = nil
		if fn != nil {
			fn()
		}
	}
	s.workers.Wait()

	s.mu.Lock()
	s.closed = true
	close(s.closeDone)
	s.mu.Unlock()
}

func (s *ExecutionScope) isClosing() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing || s.ctx.Err() != nil
}

func (s *ExecutionScope) isClosed() bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// FailExecution records an uncaught host-side execution error for the loop to
// return from Run. It is safe to call from another goroutine.
func FailExecution(loop *Loop, err error) {
	if loop != nil && err != nil {
		loop.fail(err)
	}
}
