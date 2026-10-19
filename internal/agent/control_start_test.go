package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestInstanceInterruptClosesBootstrapAdmissionBeforeContextCancellation(t *testing.T) {
	observed := make(chan events.Event, 32)
	h := NewHost(Options{Observe: func(event events.Event) { observed <- event }})
	t.Cleanup(h.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	owner := h.NewOperation(ctx)
	t.Cleanup(owner.Close)
	id := spawnLifecycle(t, owner, nil, true)
	awaitLifecycleEvent(t, observed, "instance.idle")
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce, gateOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	h.mu.Lock()
	instance := h.instances[id]
	actualCancel := instance.cancel
	instance.cancel = func() {
		gateOnce.Do(func() { close(entered); <-release })
		actualCancel()
	}
	h.mu.Unlock()
	// This is the task runner's authenticated scope before its first start RPC.
	bootstrap := h.NewOperation(instance.ctx)
	t.Cleanup(bootstrap.Close)
	h.mu.Lock()
	bootstrap.task = &managedTask{instance: instance}
	h.mu.Unlock()
	done := make(chan error, 1)
	go func() { _, err := owner.Call("interrupt_agent", `{"agentId":"`+id+`"}`); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("interrupt did not reach cancellation gate")
	}
	if _, err := bootstrap.Call("start", `{"provider":"openrouter","model":"fixture/model","name":"late-bootstrap","effort":"auto"}`); err == nil {
		t.Fatal("task started after whole-instance stop closed admission")
	}
	h.mu.Lock()
	claimed := bootstrap.task.run != nil
	h.mu.Unlock()
	if claimed {
		t.Fatal("stopping instance claimed a new run")
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("interrupt did not return")
	}
	if snapshot := lifecycleSnapshot(t, owner, id, true); snapshot.State != "stopped" || snapshot.Reason != "interrupted" {
		t.Fatalf("snapshot: %+v", snapshot)
	}
}
