package events

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestConcurrentRecordsHaveOneSequence(t *testing.T) {
	var out bytes.Buffer
	w := New(&out, "execution")
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Go(func() {
			for j := 0; j < 20; j++ {
				w.Emit(Event{"type": "thinking", "text": "日本語\nfragment"})
			}
		})
	}
	group.Wait()
	decoder := json.NewDecoder(&out)
	for i := 1; i <= 160; i++ {
		var event Event
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event["seq"] != float64(i) || event["executionId"] != "execution" || event["time"] == "" {
			t.Fatalf("event: %+v", event)
		}
	}
	if decoder.More() {
		t.Fatal("unexpected records")
	}
}

type blockedOutput struct{ started chan struct{} }

func (w *blockedOutput) Write(p []byte) (int, error) { return 0, errors.New("legacy write used") }
func (w *blockedOutput) WriteContext(ctx context.Context, p []byte) (int, error) {
	close(w.started)
	<-ctx.Done()
	return 0, ctx.Err()
}

func TestContextOutputAndWaitingProducerCanCancel(t *testing.T) {
	out := &blockedOutput{make(chan struct{})}
	w := New(out, "execution")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.EmitContext(ctx, Event{"type": "stdout", "text": "data"}) }()
	<-out.started
	other, stop := context.WithCancel(context.Background())
	stop()
	if err := w.EmitContext(other, Event{"type": "stderr"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting emit=%v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("event write did not cancel")
	}
	if w.Err() != nil {
		t.Fatalf("zero-byte cancellation poisoned output: %v", w.Err())
	}
}

type partialCancelledOutput struct{ started chan struct{} }

func (*partialCancelledOutput) Write(p []byte) (int, error) { return len(p), nil }
func (w *partialCancelledOutput) WriteContext(ctx context.Context, p []byte) (int, error) {
	close(w.started)
	<-ctx.Done()
	return 1, ctx.Err()
}

func TestPartialRecordCancellationRemainsOutputFailure(t *testing.T) {
	out := &partialCancelledOutput{make(chan struct{})}
	w := New(out, "partial")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.EmitContext(ctx, Event{"type": "stdout"}) }()
	<-out.started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("emit=%v", err)
	}
	if !errors.Is(w.Err(), context.Canceled) {
		t.Fatalf("partial error=%v", w.Err())
	}
}
