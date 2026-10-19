package signals

import (
	"errors"
	"os"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestPlatformSignalNames(t *testing.T) {
	want := []string{}
	switch runtime.GOOS {
	case "linux", "darwin":
		want = []string{"SIGHUP", "SIGINT", "SIGTERM", "SIGUSR1", "SIGUSR2"}
	case "windows":
		want = []string{"SIGINT"}
	}
	if got := SupportedNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("supported names = %v, want %v", got, want)
	}
	for _, name := range want {
		if _, err := Lookup(name); err != nil {
			t.Errorf("Lookup(%q): %v", name, err)
		}
	}
	if _, err := Lookup("SIGKILL"); !errors.Is(err, ErrUnsupportedSignal) {
		t.Fatalf("Lookup(SIGKILL) error = %v, want ErrUnsupportedSignal", err)
	}
}

type fakeSignal string

func (fakeSignal) Signal()          {}
func (s fakeSignal) String() string { return string(s) }

type fakeRegistration struct {
	channel chan<- os.Signal
	signal  os.Signal
}

type fakeSignalOS struct {
	mu            sync.Mutex
	registrations map[chan<- os.Signal]fakeRegistration
	stopCalls     int
}

func newFakeSignalOS() *fakeSignalOS {
	return &fakeSignalOS{registrations: make(map[chan<- os.Signal]fakeRegistration)}
}

func (f *fakeSignalOS) notify(ch chan<- os.Signal, sigs ...os.Signal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(sigs) != 1 {
		panic("broker registered other than one requested signal")
	}
	f.registrations[ch] = fakeRegistration{channel: ch, signal: sigs[0]}
}

func (f *fakeSignalOS) stop(ch chan<- os.Signal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalls++
	delete(f.registrations, ch)
}

func (f *fakeSignalOS) send(sig os.Signal) {
	f.mu.Lock()
	channels := make([]chan<- os.Signal, 0, len(f.registrations))
	for _, registration := range f.registrations {
		if registration.signal.String() == sig.String() {
			channels = append(channels, registration.channel)
		}
	}
	f.mu.Unlock()
	for _, ch := range channels {
		select {
		case ch <- sig:
		default:
		}
	}
}

func (f *fakeSignalOS) snapshot() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.registrations), f.stopCalls
}

func TestBrokerRegistersRequestedNamesAndUnsubscribesSameNameIndependently(t *testing.T) {
	one, two := fakeSignal("first"), fakeSignal("second")
	bus := newFakeSignalOS()
	b := NewBroker(bus.notify, bus.stop)
	received := make(chan string, 8)

	removeFirst := b.Subscribe(one, func() { received <- "first-a" })
	removeSecond := b.Subscribe(two, func() { received <- "second" })
	removeSame := b.Subscribe(one, func() { received <- "first-b" })
	if got, _ := bus.snapshot(); got != 2 {
		t.Fatalf("registered channels = %d, want one per requested signal", got)
	}

	bus.send(two)
	if got := receive(t, received); got != "second" {
		t.Fatalf("second signal delivered to %q", got)
	}
	removeFirst()
	removeFirst()
	bus.send(one)
	if got := receive(t, received); got != "first-b" {
		t.Fatalf("same-name remaining subscriber received %q", got)
	}

	removeSame()
	if got, stopped := bus.snapshot(); got != 1 || stopped != 1 {
		t.Fatalf("after last first unsubscribe: registered=%d stopped=%d", got, stopped)
	}
	bus.send(one)
	assertNoSignal(t, received)
	removeSecond()
	if got, stopped := bus.snapshot(); got != 0 || stopped != 2 {
		t.Fatalf("after all unsubscribes: registered=%d stopped=%d", got, stopped)
	}
}

func TestBrokerNewGenerationCannotBeStoppedByOldUnsubscribe(t *testing.T) {
	sig := fakeSignal("generation")
	bus := newFakeSignalOS()
	stopEntered := make(chan struct{})
	allowStop := make(chan struct{})
	var stopOnce sync.Once
	b := NewBroker(bus.notify, func(ch chan<- os.Signal) {
		stopOnce.Do(func() {
			close(stopEntered)
			<-allowStop
		})
		bus.stop(ch)
	})

	callbackEntered := make(chan struct{})
	allowCallback := make(chan struct{})
	oldRemove := b.Subscribe(sig, func() {
		close(callbackEntered)
		<-allowCallback
	})
	bus.send(sig)
	wait(t, callbackEntered, "old generation delivery")

	oldUnsubscribed := make(chan struct{})
	go func() { oldRemove(); close(oldUnsubscribed) }()
	close(allowCallback)
	wait(t, stopEntered, "old generation Stop")

	newDelivered := make(chan struct{}, 1)
	newSubscribed := make(chan func(), 1)
	go func() { newSubscribed <- b.Subscribe(sig, func() { newDelivered <- struct{}{} }) }()
	// The old Stop owns the broker lock until its own registration has been
	// removed, so a new generation cannot be installed in the middle of it.
	select {
	case <-newSubscribed:
		t.Fatal("new generation escaped while old Stop was still running")
	default:
	}
	close(allowStop)
	wait(t, oldUnsubscribed, "old unsubscribe")
	newRemove := receive(t, newSubscribed)
	if got, _ := bus.snapshot(); got != 1 {
		t.Fatalf("registrations after generation change = %d, want 1", got)
	}
	bus.send(sig)
	wait(t, newDelivered, "new generation delivery")
	newRemove()
	if got, stopped := bus.snapshot(); got != 0 || stopped != 2 {
		t.Fatalf("final registrations=%d stopped=%d, want 0 and 2", got, stopped)
	}
}

func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		var zero T
		t.Fatal("timed out waiting for value")
		return zero
	}
}

func wait[T any](t *testing.T, ch <-chan T, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func assertNoSignal(t *testing.T, ch <-chan string) {
	t.Helper()
	select {
	case got := <-ch:
		t.Fatalf("unexpected delivery %q", got)
	case <-time.After(20 * time.Millisecond):
	}
}
