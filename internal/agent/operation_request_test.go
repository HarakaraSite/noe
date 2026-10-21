package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestOperationWithRequestJoinsPostProviderWork(t *testing.T) {
	h := NewHost(Options{})
	op := h.NewOperation(context.Background())
	workEntered := make(chan struct{})
	workCanceled := make(chan struct{})
	releaseWork := make(chan struct{})
	var releaseOnce sync.Once
	workDone := make(chan error, 1)
	go func() {
		_, err := op.WithRequest(context.Background(), func(ctx context.Context) (any, error) {
			close(workEntered)
			<-ctx.Done()
			close(workCanceled)
			<-releaseWork
			return nil, ctx.Err()
		})
		workDone <- err
	}()
	defer func() {
		releaseOnce.Do(func() { close(releaseWork) })
		h.Close()
	}()
	select {
	case <-workEntered:
	case <-time.After(time.Second):
		t.Fatal("request work was not entered")
	}
	closeDone := make(chan struct{})
	go func() { op.Close(); close(closeDone) }()
	select {
	case <-workCanceled:
	case <-time.After(time.Second):
		t.Fatal("operation close did not cancel request work")
	}
	select {
	case <-closeDone:
		t.Fatal("operation close returned before request work completed")
	case <-time.After(30 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(releaseWork) })
	select {
	case <-closeDone:
	case <-time.After(time.Second):
		t.Fatal("operation close did not finish after request work released")
	}
	if err := <-workDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("request error=%v, want context.Canceled", err)
	}
}
