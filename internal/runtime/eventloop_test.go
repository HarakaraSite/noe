package runtime

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestEventLoopContinuousDelivery(t *testing.T) {
	var out bytes.Buffer
	h, err := newHost(Options{Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	defer h.rt.Close()
	if err := h.rt.Set("begin", func(deliver quickjs.Value) {
		a, b := h.startEventSource(nil), h.startEventSource(nil)
		go func() {
			for i := 1; i <= 3; i++ {
				a.Post(func() {
					if _, err := deliver.Call(i); err != nil {
						h.loop.failure = err
					}
				})
			}
			a.Done()
			b.Post(func() {
				if _, err := deliver.Call(4); err != nil {
					h.loop.failure = err
				}
			})
			b.Done()
		}()
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	code, err := h.run(ctx, Source{filepath.Join(t.TempDir(), "main.js"), `begin(n => Promise.resolve(n).then(console.log));`})
	if err != nil || code != 0 || out.String() != "1\n2\n3\n4\n" {
		t.Fatalf("code=%d err=%v out=%q", code, err, out.String())
	}
	if h.rt.Busy() {
		t.Fatal("finished sources still keep runtime alive")
	}
}

func TestEventLoopErrorsWhileAwaiting(t *testing.T) {
	for _, kind := range []string{"host failure", "unhandled rejection"} {
		t.Run(kind, func(t *testing.T) {
			h, err := newHost(Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer h.rt.Close()
			var stopped atomic.Int32
			if err := h.rt.Set("begin", func() {
				s := h.startEventSource(func() { stopped.Add(1) })
				go func() {
					if kind == "host failure" {
						s.Fail(errors.New("source failed"))
						return
					}
					s.Post(func() {
						if _, err := h.rt.Eval(`Promise.reject(new Error("source failed"))`); err != nil {
							h.loop.failure = err
						}
					})
				}()
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			code, err := h.run(ctx, Source{filepath.Join(t.TempDir(), "main.js"), `begin(); await new Promise(() => {});`})
			if code != 1 || err == nil || !strings.Contains(err.Error(), "source failed") || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("code=%d err=%v", code, err)
			}
			if stopped.Load() != 1 {
				t.Fatalf("stop count=%d", stopped.Load())
			}
		})
	}
}

func TestEventLoopSessionSourceErrorAndCancellationRecovery(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary error", true: "cancelled input"}[cancelled], func(t *testing.T) {
			s, err := NewSession(Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			var stopped atomic.Int32
			var source *eventSource
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := s.h.rt.Set("begin", func() {
				source = s.h.startEventSource(func() { stopped.Add(1) })
				if cancelled {
					cancel()
				}
			}); err != nil {
				t.Fatal(err)
			}
			input := `var retained = 42; begin(); throw new Error("input failed")`
			if cancelled {
				input = `var retained = 42; begin()`
			}
			result := s.Eval(ctx, input)
			if result.Exit || result.Err == nil {
				t.Fatalf("result=%+v", result)
			}
			if cancelled && !errors.Is(result.Err, context.Canceled) {
				t.Fatalf("result=%+v", result)
			}
			if stopped.Load() != 1 {
				t.Fatalf("stop count=%d", stopped.Load())
			}
			// A delayed post can execute its error callback on this poster;
			// its JS body must be suppressed after input cancellation/error.
			done := make(chan struct{})
			go func() { source.Post(func() { panic("stale JS callback") }); close(done) }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("late post blocked")
			}
			result = s.Eval(context.Background(), `retained + 1`)
			if result.Exit || result.Err != nil || result.Output != "43" {
				t.Fatalf("recovery=%+v", result)
			}
			if s.h.rt.Busy() {
				t.Fatal("cancelled source survived")
			}
		})
	}
}

func TestEventLoopFormatterErrorStopsSource(t *testing.T) {
	s, err := NewSession(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var stopped atomic.Int32
	if err := s.h.rt.Set("begin", func() { s.h.startEventSource(func() { stopped.Add(1) }) }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := s.Eval(ctx, `({get value(){ begin(); throw new Error("format failed"); }})`)
	if result.Exit || result.Err == nil || !strings.Contains(result.Err.Error(), "format failed") || errors.Is(result.Err, context.DeadlineExceeded) {
		t.Fatalf("result=%+v", result)
	}
	if stopped.Load() != 1 {
		t.Fatalf("stopped=%d", stopped.Load())
	}
	result = s.Eval(ctx, `40 + 2`)
	if result.Exit || result.Err != nil || result.Output != "42" {
		t.Fatalf("recovery=%+v", result)
	}
}

func TestEventLoopFailureCancelsPendingInputWork(t *testing.T) {
	for _, awaiting := range []bool{false, true} {
		t.Run(map[bool]string{false: "drain", true: "pending await"}[awaiting], func(t *testing.T) {
			s, err := NewSession(Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			started, stopped := make(chan struct{}), make(chan struct{})
			if err := s.h.rt.Set("blocked", func() quickjs.Value {
				return s.h.startAsyncWork(func(ctx context.Context) (any, error) {
					close(started)
					<-ctx.Done()
					close(stopped)
					return nil, ctx.Err()
				})
			}); err != nil {
				t.Fatal(err)
			}
			if err := s.h.rt.Set("begin", func() {
				source := s.h.startEventSource(nil)
				go func() { <-started; source.Fail(errors.New("source failed")) }()
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			input := `blocked().then(()=>{globalThis.stale = true}); begin();`
			if awaiting {
				input = `await (blocked(), begin(), new Promise(()=>{}))`
			}
			result := s.Eval(ctx, input)
			if result.Exit || result.Err == nil || !strings.Contains(result.Err.Error(), "source failed") || errors.Is(result.Err, context.DeadlineExceeded) {
				t.Fatalf("result=%+v", result)
			}
			select {
			case <-stopped:
			case <-ctx.Done():
				t.Fatal("pending input I/O was not cancelled")
			}
			result = s.Eval(ctx, `typeof stale`)
			if result.Exit || result.Err != nil || result.Output != "'undefined'" {
				t.Fatalf("recovery=%+v", result)
			}
		})
	}
}
