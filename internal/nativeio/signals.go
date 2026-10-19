package nativeio

import (
	_ "embed"
	"errors"
	"fmt"
	"sync"

	"noe/internal/signals"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed js/signals.js
var signalsSource string

// installSignals registers the runtime-local AsyncIterator adapter backed by
// the process-wide signal broker.
func (h *Host) installSignals() error {
	api := h.rt.NewObject()
	if err := api.Set("subscribe", h.subscribeSignal); err != nil {
		return err
	}
	factory, err := h.rt.EvalFile("<noe-signals>", signalsSource)
	if err != nil {
		return err
	}
	values, err := factory.Call(api)
	if err != nil {
		return err
	}
	subscribe, err := values.Get("subscribe")
	if err != nil {
		return err
	}
	return h.rt.SetModuleValues("noe:signals", map[string]quickjs.Value{"subscribe": subscribe})
}

func (h *Host) subscribeSignal(name string) (quickjs.Value, error) {
	if _, err := signals.Lookup(name); err != nil {
		errValue := h.rt.NewError("Error", fmt.Sprintf("unsupported signal %q on this operating system", name))
		_ = errValue.Set("code", signals.UnsupportedCode)
		return quickjs.Value{}, h.rt.Throw(errValue)
	}
	s := &signalSubscription{h: h, name: name, work: h.rt.StartAsyncWork()}
	obj := h.rt.NewObject()
	if err := errors.Join(obj.Set("next", s.next), obj.Set("close", s.closeForJS)); err != nil {
		_ = s.Close()
		return quickjs.Value{}, err
	}
	if !h.own(s) {
		return quickjs.Value{}, h.rt.ThrowError("runtime is closing")
	}
	remove, err := signals.Subscribe(name, s.enqueue)
	if err != nil {
		_ = s.Close()
		errValue := h.rt.NewError("Error", err.Error())
		_ = errValue.Set("code", signals.UnsupportedCode)
		return quickjs.Value{}, h.rt.Throw(errValue)
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		remove()
	} else {
		s.remove = remove
		s.mu.Unlock()
	}
	return obj, nil
}

type signalSubscription struct {
	h    *Host
	name string
	work *quickjs.AsyncWork

	mu      sync.Mutex
	closed  bool
	remove  func()
	pending bool
	posted  bool
	waiters []*quickjs.Promise
}

func (s *signalSubscription) next() *quickjs.Promise {
	p := s.h.rt.NewPromise()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = p.Resolve(iteratorResult(s.h.rt, "", true))
		return p
	}
	// If an earlier next is waiting, an event already queued for it belongs to
	// that earlier request even if its owner callback has not run yet.
	if s.pending && len(s.waiters) == 0 {
		s.pending = false
		name := s.name
		s.mu.Unlock()
		_ = p.Resolve(iteratorResult(s.h.rt, name, false))
		return p
	}
	s.waiters = append(s.waiters, p)
	shouldPost := s.pending && !s.posted
	if shouldPost {
		s.posted = true
	}
	s.mu.Unlock()
	if shouldPost {
		s.postDelivery()
	}
	return p
}

func (s *signalSubscription) enqueue() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	// os/signal and the OS may coalesce events. Keep at most one notification
	// for this subscription while it has no owner-thread delivery available.
	s.pending = true
	shouldPost := len(s.waiters) != 0 && !s.posted
	if shouldPost {
		s.posted = true
	}
	s.mu.Unlock()
	if shouldPost {
		s.postDelivery()
	}
}

func (s *signalSubscription) postDelivery() {
	s.work.Post(func(err error) {
		if err != nil {
			return
		}
		s.deliver()
	})
}

// deliver runs only on the runtime owner through AsyncWork.
func (s *signalSubscription) deliver() {
	s.mu.Lock()
	s.posted = false
	if s.closed || !s.pending || len(s.waiters) == 0 {
		s.mu.Unlock()
		return
	}
	p := s.waiters[0]
	copy(s.waiters, s.waiters[1:])
	s.waiters[len(s.waiters)-1] = nil
	s.waiters = s.waiters[:len(s.waiters)-1]
	s.pending = false
	name := s.name
	s.mu.Unlock()
	_ = p.Resolve(iteratorResult(s.h.rt, name, false))
}

func (s *signalSubscription) closeForJS() *quickjs.Promise {
	p := s.h.rt.NewPromise()
	waiters := s.close(true)
	for _, waiter := range waiters {
		_ = waiter.Resolve(iteratorResult(s.h.rt, "", true))
	}
	_ = p.Resolve(nil)
	return p
}

// Close is Go-only runtime teardown. It releases the owner reference without
// resolving JS promises or requiring the runtime's event loop to run.
func (s *signalSubscription) Close() error {
	s.close(false)
	return nil
}

func (s *signalSubscription) close(resolvePending bool) []*quickjs.Promise {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.pending = false
	s.posted = false
	remove := s.remove
	s.remove = nil
	var waiters []*quickjs.Promise
	if resolvePending {
		waiters = s.waiters
	}
	// Runtime teardown deliberately drops QuickJS values without touching the
	// closing runtime. Explicit JS close resolves the returned iterators below.
	s.waiters = nil
	s.mu.Unlock()

	if remove != nil {
		remove()
	}
	s.work.Done()
	s.h.release(s)
	return waiters
}

func iteratorResult(rt *quickjs.Runtime, name string, done bool) quickjs.Value {
	result := rt.NewObject()
	if !done {
		_ = result.Set("value", name)
	}
	_ = result.Set("done", done)
	return result
}
