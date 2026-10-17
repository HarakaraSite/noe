package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"noe/internal/events"
)

func TestSupervisionFailureClosesInterruptAdmissionBeforePublishingTaskFailure(t *testing.T) {
	failed, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	h := NewHost(Options{Executable: filepath.Join(t.TempDir(), "missing-noe"), Observe: func(event events.Event) {
		if event["type"] == "command.failed" {
			close(failed)
			<-release
		}
	}})
	t.Cleanup(h.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op := h.NewOperation(ctx)
	t.Cleanup(op.Close)
	instruction := "cannot start process"
	id := spawnLifecycle(t, op, &instruction, true)
	select {
	case <-failed:
	case <-ctx.Done():
		t.Fatal("task failure was not published")
	}
	if _, err := op.Call("interrupt_agent", fmt.Sprintf(`{"agentId":%q}`, id)); err == nil {
		t.Fatal("supervision failure accepted an interrupt that could overwrite its terminal reason")
	}
	unblock()
	snapshot := lifecycleSnapshot(t, op, id, true)
	if snapshot.State != "failed" || snapshot.Reason != "supervision_failed" {
		t.Fatalf("supervision result: %+v", snapshot)
	}
}
