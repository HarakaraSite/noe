package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Both entry points block, so the test detects a synchronous legacy Write as
// well as a WriteContext that was given the wrong lifetime.
type blockedObserveOutput struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	final   bool
	closed  bool
}

func (w *blockedObserveOutput) write(ctx context.Context, p []byte) (int, error) {
	if w.final && string(p) != "\n" {
		return len(p), nil
	}
	w.once.Do(func() { close(w.started) })
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-w.release:
		return len(p), nil
	}
}

func (w *blockedObserveOutput) Write(p []byte) (int, error) {
	return w.write(context.Background(), p)
}

func (w *blockedObserveOutput) WriteContext(ctx context.Context, p []byte) (int, error) {
	return w.write(ctx, p)
}

func (w *blockedObserveOutput) Close() error { w.closed = true; return nil }

func TestObserveCancelsBlockedOutput(t *testing.T) {
	for _, mode := range []string{"display", "final-newline", "json"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out := &blockedObserveOutput{started: make(chan struct{}), release: make(chan struct{}), final: mode == "final-newline"}
			var diagnostic bytes.Buffer
			args := []string{"observe"}
			input := `{"type":"agent.started","runId":"r","name":"worker"}` + "\n"
			if mode == "final-newline" {
				input = `{"type":"thinking","runId":"r","name":"worker","text":"pending"}` + "\n"
			}
			if mode == "json" {
				args = append(args, "--json", "--run", "r", filepath.Join(t.TempDir(), "events.jsonl"))
				if err := os.WriteFile(args[len(args)-1], []byte(`{"version":1,"executionId":"exec","seq":1,"time":"2026-10-24T00:00:00Z","type":"agent.started","runId":"r"}`+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan int, 1)
			go func() { done <- Run(ctx, args, strings.NewReader(input), out, &diagnostic) }()
			finished := false
			defer func() {
				close(out.release)
				if !finished {
					select {
					case <-done:
					case <-time.After(3 * time.Second):
						t.Error("observer did not stop after releasing test output")
					}
				}
			}()
			select {
			case <-out.started:
			case <-time.After(3 * time.Second):
				t.Fatal("observer never attempted the blocked write")
			}
			cancel()
			select {
			case code := <-done:
				finished = true
				if code != 130 || diagnostic.Len() != 0 {
					t.Fatalf("cancelled output: exit=%d stderr=%q", code, diagnostic.String())
				}
				if out.closed {
					t.Fatal("observer closed its borrowed output")
				}
			case <-time.After(time.Second):
				t.Fatal("cancellation remained blocked on output")
			}
		})
	}
}

type failedObserveOutput struct{ err error }

func (w failedObserveOutput) Write([]byte) (int, error) { return 0, w.err }

func TestObservePreservesLegacyWriterErrors(t *testing.T) {
	var diagnostic bytes.Buffer
	out := failedObserveOutput{errors.New("legacy output failed")}
	code := Run(context.Background(), []string{"observe"}, strings.NewReader(`{"type":"agent.started","runId":"r"}`+"\n"), out, &diagnostic)
	if code != 1 || !strings.Contains(diagnostic.String(), out.err.Error()) {
		t.Fatalf("exit=%d stderr=%q", code, diagnostic.String())
	}
}
