// Package signals owns the process-wide OS signal registrations used by NOE
// runtimes. Runtime-specific delivery is supplied by subscribers and must not
// call into QuickJS from the broker goroutine.
package signals

import (
	"errors"
	"os"
	"os/signal"
	"sort"
	"sync"
)

const UnsupportedCode = "ERR_UNSUPPORTED_SIGNAL"

// ErrUnsupportedSignal is returned when a name is unavailable on this OS.
var ErrUnsupportedSignal = errors.New("unsupported signal")

// Lookup resolves one of the signal names exposed by noe:signals.
func Lookup(name string) (os.Signal, error) {
	sig, ok := platformSignals[name]
	if !ok {
		return nil, ErrUnsupportedSignal
	}
	return sig, nil
}

// SupportedNames returns the public names available on this OS.
func SupportedNames() []string {
	names := make([]string, 0, len(platformSignals))
	for name := range platformSignals {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type notifyFunc func(chan<- os.Signal, ...os.Signal)
type stopFunc func(chan<- os.Signal)

// Broker multiplexes NOE subscriptions through one os/signal registration per
// requested signal. Separate channels make stopping one NOE registration
// independent of signal.NotifyContext and other Go consumers.
type Broker struct {
	mu     sync.RWMutex
	nextID uint64
	active map[string]*generation
	notify notifyFunc
	stop   stopFunc
}

type generation struct {
	name      string
	signal    os.Signal
	channel   chan os.Signal
	done      chan struct{}
	stopped   chan struct{}
	nextID    uint64
	callbacks map[uint64]func()
}

// NewBroker creates a broker. Passing nil uses os/signal's process registry.
// The injected functions exist so generation races can be tested without
// delivering real OS signals to the test process.
func NewBroker(notify notifyFunc, stop stopFunc) *Broker {
	if notify == nil {
		notify = signal.Notify
	}
	if stop == nil {
		stop = signal.Stop
	}
	return &Broker{active: make(map[string]*generation), notify: notify, stop: stop}
}

// Subscribe registers deliver for a supported signal and returns an
// idempotent removal function. The callback runs on a broker goroutine and
// must only enqueue work for its owning runtime.
func (b *Broker) Subscribe(sig os.Signal, deliver func()) func() {
	name := sig.String()
	b.mu.Lock()
	g := b.active[name]
	if g == nil {
		g = &generation{
			name:      name,
			signal:    sig,
			channel:   make(chan os.Signal, 1),
			done:      make(chan struct{}),
			stopped:   make(chan struct{}),
			callbacks: make(map[uint64]func()),
		}
		b.notify(g.channel, sig)
		b.active[name] = g
		go b.dispatch(g)
	}
	b.nextID++
	id := b.nextID
	g.callbacks[id] = deliver
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			last := false
			b.mu.Lock()
			delete(g.callbacks, id)
			if len(g.callbacks) == 0 && b.active[name] == g {
				delete(b.active, name)
				// Stop only the registration owned by this generation. Do not
				// call signal.Reset: other Go consumers may share this signal.
				b.stop(g.channel)
				close(g.done)
				last = true
			}
			b.mu.Unlock()
			if last {
				<-g.stopped
			}
		})
	}
}

func (b *Broker) dispatch(g *generation) {
	defer close(g.stopped)
	for {
		select {
		case <-g.done:
			return
		case _, ok := <-g.channel:
			if !ok {
				return
			}
			b.mu.RLock()
			if b.active[g.name] != g {
				b.mu.RUnlock()
				continue
			}
			// Keep the read lock while the short enqueue callbacks run. An
			// unsubscribe then returns only after any in-flight delivery has
			// stopped retaining its runtime subscription.
			for _, deliver := range g.callbacks {
				deliver()
			}
			b.mu.RUnlock()
		}
	}
}

// Process is the process-wide broker used by NOE runtimes.
var Process = NewBroker(nil, nil)

// Subscribe registers one public signal name with the process-wide broker.
func Subscribe(name string, deliver func()) (func(), error) {
	sig, err := Lookup(name)
	if err != nil {
		return nil, ErrUnsupportedSignal
	}
	return Process.Subscribe(sig, deliver), nil
}
