//go:build lowlevel2_spike && linux

package signals

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
	"github.com/go-quickjs/go-quickjs/stdlib"
)

const helperEnv = "NOE_LOWLEVEL2_SIGNAL_HELPER"

// TestSignalBrokerOSIntegration exercises process signals only in helper
// children started by this test. Never run the signal choreography in the
// go test process itself.
func TestSignalBrokerOSIntegration(t *testing.T) {
	if mode := os.Getenv(helperEnv); mode != "" {
		runSignalHelper(mode)
		return
	}

	t.Run("unsubscribed SIGTERM keeps its default action", func(t *testing.T) {
		child := startChild(t, "idle")
		child.waitEvent(t, "ready", "", "", 0)
		child.signal(t, syscall.SIGTERM)
		child.waitSignaled(t, syscall.SIGTERM)
	})

	t.Run("owner delivery, unsubscribe, NotifyContext, and default restore", func(t *testing.T) {
		child := startChild(t, "active")
		child.waitEvent(t, "ready", "worker", "", 0)

		// A live subscription suppresses SIGTERM's default action. Both real
		// QuickJS runtimes receive it, then the Worker unsubscribes and exits.
		term := syscall.SIGTERM.String()
		child.signal(t, syscall.SIGTERM)
		child.waitEvent(t, "signal", "main", term, 1)
		child.waitEvent(t, "signal", "worker", term, 1)
		child.waitEvent(t, "unsubscribed", "worker", "", 1)
		child.waitEvent(t, "worker-exit", "main", "", 0)
		child.assertStillRunning(t)

		// A signal subscription must keep the main QuickJS loop alive without a
		// JavaScript timer. SIGINT still cancels the existing NotifyContext.
		child.signal(t, os.Interrupt)
		child.waitEvent(t, "signal", "main", os.Interrupt.String(), 2)
		child.waitEvent(t, "notify-context-canceled", "", "", 0)
		child.assertStillRunning(t)

		// The worker has removed its own subscription and exited. The remaining
		// main-runtime subscription still receives the next SIGINT.
		child.signal(t, os.Interrupt)
		child.waitEvent(t, "signal", "main", os.Interrupt.String(), 3)
		child.waitEvent(t, "unsubscribed", "main", "", 3)
		child.waitEvent(t, "loop-returned", "", "", 0)

		// The last unsubscribe called signal.Stop. SIGTERM now has its default
		// process action again.
		child.signal(t, syscall.SIGTERM)
		child.waitSignaled(t, syscall.SIGTERM)
	})
}

// TestGoOwnedCallbackDoesNotKeepRuntimeAlive distinguishes a Go reference to
// a QuickJS value from runtime work. Retaining the value alone is not a
// keepalive; a subscription needs a referenced AsyncWork or equivalent.
func TestGoOwnedCallbackDoesNotKeepRuntimeAlive(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := stdlib.NewLoop(rt)
	callback, err := rt.Eval("() => 1")
	if err != nil {
		t.Fatal(err)
	}
	owned := []quickjs.Value{callback}
	if rt.Busy() {
		t.Fatal("a Go-held callback unexpectedly made the runtime busy")
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(owned) != 1 {
		t.Fatal("test did not retain the callback in Go")
	}
	runtime.KeepAlive(owned)
}

// TestUnsubscribeReleasesGoSubscriberClosure verifies that unsubscribe drops
// the broker's only reference to a subscriber closure without any JS timer.
func TestUnsubscribeReleasesGoSubscriberClosure(t *testing.T) {
	b := newSignalBroker()
	finalized := make(chan struct{})
	unsubscribe := registerCapturedSubscriber(b, finalized)
	if b.subscriberCount() != 1 {
		t.Fatalf("subscriber count = %d, want 1", b.subscriberCount())
	}
	unsubscribe()
	unsubscribe = nil
	if b.subscriberCount() != 0 {
		t.Fatalf("subscriber count after unsubscribe = %d, want 0", b.subscriberCount())
	}
	waitForFinalizer(t, finalized)
}

func TestRuntimeCloseUnsubscribesSignalOwner(t *testing.T) {
	b := newSignalBroker()
	rt := quickjs.New()
	if err := installSignalAPI(rt, b, "test", func(event) {}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`globalThis.stopSignal = subscribeSignal(() => {})`); err != nil {
		rt.Close()
		t.Fatal(err)
	}
	if got := b.subscriberCount(); got != 1 {
		rt.Close()
		t.Fatalf("subscriber count before runtime close = %d, want 1", got)
	}
	if !rt.Busy() {
		rt.Close()
		t.Fatal("signal subscription did not keep the runtime busy")
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if got := b.subscriberCount(); got != 0 {
		t.Fatalf("subscriber count after runtime close = %d, want 0", got)
	}
	if rt.Busy() {
		t.Fatal("closed runtime still reports busy")
	}
}

type captured struct{ value int }

func registerCapturedSubscriber(b *signalBroker, finalized chan struct{}) func() {
	value := &captured{value: 1}
	runtime.SetFinalizer(value, func(*captured) { close(finalized) })
	return b.subscribe(func(os.Signal) { runtime.KeepAlive(value) })
}

func waitForFinalizer(t *testing.T, finalized <-chan struct{}) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		runtime.GC()
		select {
		case <-finalized:
			return
		case <-deadline:
			t.Fatal("unsubscribed Go callback capture was not finalized")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

type signalGeneration struct {
	ch      chan os.Signal
	done    chan struct{}
	stopped chan struct{}
}

type signalBroker struct {
	mu     sync.RWMutex
	nextID uint64
	active *signalGeneration
	subs   map[uint64]func(os.Signal)
}

func newSignalBroker() *signalBroker {
	return &signalBroker{subs: make(map[uint64]func(os.Signal))}
}

// subscribe starts one process-wide os/signal listener for the first owner.
// Each owner callback is only asked to enqueue work; it must not touch JS here.
func (b *signalBroker) subscribe(deliver func(os.Signal)) func() {
	b.mu.Lock()
	if b.active == nil {
		g := &signalGeneration{
			ch:      make(chan os.Signal, 32),
			done:    make(chan struct{}),
			stopped: make(chan struct{}),
		}
		signal.Notify(g.ch, os.Interrupt, syscall.SIGTERM)
		b.active = g
		go b.dispatch(g)
	}
	g := b.active
	b.nextID++
	id := b.nextID
	b.subs[id] = deliver
	b.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			last := false
			b.mu.Lock()
			delete(b.subs, id)
			if len(b.subs) == 0 && b.active == g {
				b.active = nil
				signal.Stop(g.ch)
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

func (b *signalBroker) dispatch(g *signalGeneration) {
	defer close(g.stopped)
	for {
		select {
		case <-g.done:
			return
		case sig := <-g.ch:
			b.mu.RLock()
			if b.active != g {
				b.mu.RUnlock()
				continue
			}
			deliveries := make([]func(os.Signal), 0, len(b.subs))
			for _, deliver := range b.subs {
				deliveries = append(deliveries, deliver)
			}
			b.mu.RUnlock()
			for _, deliver := range deliveries {
				deliver(sig)
			}
		}
	}
}

func (b *signalBroker) subscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

// installSignalAPI is called on a runtime's owner goroutine. The AsyncWork
// keeps that runtime alive while subscribed and runs each callback on the
// same owner goroutine when the loop next runs its jobs.
func installSignalAPI(rt *quickjs.Runtime, broker *signalBroker, owner string, emit func(event)) error {
	if err := rt.Set("emitEvent", func(kind, name string, count int) {
		emit(event{Type: kind, Owner: owner, Signal: name, Count: count})
	}); err != nil {
		return err
	}
	return rt.Set("subscribeSignal", func(r *quickjs.Runtime, handler quickjs.Value) func() {
		work := r.StartAsyncWork()
		unregister := broker.subscribe(func(sig os.Signal) {
			work.Post(func(postErr error) {
				if postErr != nil {
					return
				}
				if _, err := handler.Call(sig.String()); err != nil {
					emit(event{Type: "callback-error", Owner: owner, Signal: err.Error()})
				}
			})
		})
		var once sync.Once
		unsubscribe := func() {
			once.Do(func() {
				unregister()
				work.Done()
			})
		}
		r.OnClose(unsubscribe)
		return unsubscribe
	})
}

type event struct {
	Type   string `json:"type"`
	Owner  string `json:"owner,omitempty"`
	Signal string `json:"signal,omitempty"`
	Count  int    `json:"count,omitempty"`
}

type eventWriter struct {
	mu sync.Mutex
}

func (w *eventWriter) write(e event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = json.NewEncoder(os.Stdout).Encode(e)
}

func runSignalHelper(mode string) {
	writer := &eventWriter{}
	switch mode {
	case "idle":
		writer.write(event{Type: "ready"})
		select {}
	case "active":
		b := newSignalBroker()
		ctx, stopNotifyContext := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stopNotifyContext()
		go func() {
			<-ctx.Done()
			writer.write(event{Type: "notify-context-canceled"})
		}()

		rt := quickjs.New()
		loop := stdlib.NewLoop(rt)
		cfg := stdlib.Config{
			Loop: loop,
			Workers: &stdlib.Workers{
				New: func() (*quickjs.Runtime, error) { return quickjs.New(), nil },
				Installed: func(workerRT *quickjs.Runtime) error {
					return installSignalAPI(workerRT, b, "worker", writer.write)
				},
			},
		}
		if err := stdlib.Install(rt, cfg); err != nil {
			helperFatal(writer, err)
		}
		if err := installSignalAPI(rt, b, "main", writer.write); err != nil {
			helperFatal(writer, err)
		}
		_, err := rt.EvalModule("main.mjs", `
import { Worker } from "node:worker_threads";
let mainCount = 0;
let unsubscribeMain;
unsubscribeMain = subscribeSignal(name => {
  mainCount++;
  emitEvent("signal", name, mainCount);
  if (mainCount === 3) {
    unsubscribeMain();
    emitEvent("unsubscribed", "", mainCount);
  }
});
const workerSource = `+"`"+`
  import { parentPort } from "node:worker_threads";
  let count = 0;
  let unsubscribe;
  unsubscribe = subscribeSignal(name => {
    count++;
    emitEvent("signal", name, count);
    unsubscribe();
    emitEvent("unsubscribed", "", count);
    parentPort.close();
  });
  emitEvent("ready", "", 0);
`+"`"+`;
const worker = new Worker(workerSource, { eval: true, type: "module" });
worker.on("exit", code => emitEvent("worker-exit", String(code), 0));
`)
		if err != nil {
			helperFatal(writer, err)
		}
		if err := loop.Run(context.Background()); err != nil {
			helperFatal(writer, err)
		}
		_ = rt.Close()
		writer.write(event{Type: "loop-returned"})
		// Stay in the helper process so the parent can verify that SIGTERM has
		// returned to its default action after loop/worker shutdown.
		select {}
	default:
		helperFatal(writer, fmt.Errorf("unknown helper mode %q", mode))
	}
}

func helperFatal(w *eventWriter, err error) {
	w.write(event{Type: "helper-error", Signal: err.Error()})
	os.Exit(70)
}

type childProcess struct {
	cmd      *exec.Cmd
	events   chan event
	done     chan struct{}
	waitErr  error
	stderr   bytes.Buffer
	observed []event
}

func startChild(t *testing.T, mode string) *childProcess {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSignalBrokerOSIntegration$")
	var env []string
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, helperEnv+"=") {
			env = append(env, value)
		}
	}
	cmd.Env = append(env, helperEnv+"="+mode)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child := &childProcess{cmd: cmd, events: make(chan event), done: make(chan struct{})}
	cmd.Stderr = &child.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(child.events)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 1024), 64*1024)
		for scanner.Scan() {
			var e event
			if json.Unmarshal(scanner.Bytes(), &e) == nil {
				child.events <- e
			}
		}
	}()
	go func() {
		child.waitErr = cmd.Wait()
		close(child.done)
	}()
	t.Cleanup(func() {
		select {
		case <-child.done:
		default:
			_ = child.cmd.Process.Kill()
			<-child.done
		}
	})
	return child
}

func (c *childProcess) waitEvent(t *testing.T, kind, owner, signalName string, count int) event {
	t.Helper()
	match := func(e event) bool {
		return e.Type == kind && e.Owner == owner && (signalName == "" || e.Signal == signalName) && (count == 0 || e.Count == count)
	}
	for _, e := range c.observed {
		if match(e) {
			return e
		}
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case e, ok := <-c.events:
			if !ok {
				<-c.done
				t.Fatalf("child exited before %s/%s/%s/%d; wait=%v stderr=%s events=%+v", kind, owner, signalName, count, c.waitErr, c.stderr.String(), c.observed)
			}
			c.observed = append(c.observed, e)
			if e.Type == "helper-error" {
				t.Fatalf("helper error: %s", e.Signal)
			}
			if match(e) {
				return e
			}
		case <-c.done:
			t.Fatalf("child exited before %s/%s/%s/%d; wait=%v stderr=%s events=%+v", kind, owner, signalName, count, c.waitErr, c.stderr.String(), c.observed)
		case <-timer.C:
			t.Fatalf("timed out waiting for %s/%s/%s/%d; stderr=%s events=%+v", kind, owner, signalName, count, c.stderr.String(), c.observed)
		}
	}
}

func (c *childProcess) signal(t *testing.T, sig os.Signal) {
	t.Helper()
	if err := c.cmd.Process.Signal(sig); err != nil {
		t.Fatalf("send %s to helper: %v", sig, err)
	}
}

func (c *childProcess) assertStillRunning(t *testing.T) {
	t.Helper()
	select {
	case <-c.done:
		t.Fatalf("helper exited while subscribed: wait=%v stderr=%s events=%+v", c.waitErr, c.stderr.String(), c.observed)
	case <-time.After(75 * time.Millisecond):
	}
}

func (c *childProcess) waitSignaled(t *testing.T, want syscall.Signal) {
	t.Helper()
	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("helper did not exit from %s; events=%+v", want, c.observed)
	}
	if c.cmd.ProcessState == nil {
		t.Fatal("helper has no process state after wait")
	}
	state, ok := c.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !state.Signaled() || state.Signal() != want {
		t.Fatalf("helper exit=%v, want signal %s; stderr=%s events=%+v", c.waitErr, want, c.stderr.String(), c.observed)
	}
}
