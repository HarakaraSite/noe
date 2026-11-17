package provider

import (
	"context"
	"sync"
)

// RunScope owns optional persistent transport resources for one agent run.
// It is carried through the existing model service in context, rather than
// exposing a standalone conversation API or retaining sockets in Registry.
type RunScope struct {
	mu        sync.Mutex
	ctx       context.Context
	resources map[*OpenAI]managedResource
	closed    bool
	done      chan struct{}
	stop      func() bool
}

// Close must detach/abort transport resources without waiting for a Generate
// call or its observer. Agent cancellation may hold the host's state mutex.
type managedResource interface{ Close() }

type runScopeKey struct{}

func NewRunScope(ctx context.Context) *RunScope {
	if ctx == nil {
		ctx = context.Background()
	}
	scope := &RunScope{ctx: ctx, resources: make(map[*OpenAI]managedResource), done: make(chan struct{})}
	stop := context.AfterFunc(ctx, scope.Close)
	scope.mu.Lock()
	if scope.closed {
		scope.mu.Unlock()
		stop()
	} else {
		scope.stop = stop
		scope.mu.Unlock()
	}
	return scope
}

func WithRunScope(ctx context.Context, scope *RunScope) context.Context {
	if scope == nil {
		return ctx
	}
	return context.WithValue(ctx, runScopeKey{}, scope)
}

func managedScope(ctx context.Context) *RunScope {
	if ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(runScopeKey{}).(*RunScope)
	return scope
}

func (s *RunScope) resource(key *OpenAI, create func() managedResource) (managedResource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, providerError(ErrorCancelled, 0, context.Canceled)
	}
	if err := cancellationError(s.ctx); err != nil {
		return nil, err
	}
	if resource := s.resources[key]; resource != nil {
		return resource, nil
	}
	resource := create()
	s.resources[key] = resource
	return resource, nil
}

func (s *RunScope) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.closed {
		done := s.done
		s.mu.Unlock()
		<-done
		return
	}
	s.closed = true
	stop := s.stop
	s.stop = nil
	resources := s.resources
	s.resources = nil
	s.mu.Unlock()
	if stop != nil {
		stop()
	}
	for _, resource := range resources {
		resource.Close()
	}
	close(s.done)
}
