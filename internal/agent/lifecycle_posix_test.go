//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

func TestLifecycleProcessExitWithoutReportIsNotSuccess(t *testing.T) {
	executable := writeFakeNOE(t, `printf '{"answer":"stdout is not a completion report"}\n'
exit 0
`)
	p := &fixtureProvider{}
	h := NewHost(Options{Provider: p, Executable: executable, Cwd: t.TempDir()})
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	op := h.NewOperation(ctx)
	defer op.Close()
	task := "task"
	id := spawnLifecycle(t, op, &task, false)
	result := lifecycleSnapshot(t, op, id, true)
	if result.Reason != "task_failed" || result.LastResult == nil || result.LastResult.State != "failed" || result.LastResult.Answer != "" {
		t.Fatalf("stdout/exit incorrectly accepted: %+v", result)
	}
	if p.callCount() != 0 {
		t.Fatal("fake executable called provider")
	}
}

func TestLifecycleCompletionWaitsForRunnerAndRetainsAnswerAfterProcessFailure(t *testing.T) {
	t.Setenv("NOE_LIFECYCLE_REAL_BIN", lifecycleBinary(t))
	executable := writeFakeNOE(t, `"$NOE_LIFECYCLE_REAL_BIN" "$@"
exit 7
`)
	observed := make(chan events.Event, 100)
	h := NewHost(Options{Provider: &fixtureProvider{gen: func(context.Context, provider.Request) (provider.Response, error) {
		return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"committed answer"}`), FinishReason: "stop"}, nil
	}}, Executable: executable, Cwd: t.TempDir(), Observe: func(event events.Event) { observed <- event }})
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op := h.NewOperation(ctx)
	defer op.Close()
	task := "task"
	id := spawnLifecycle(t, op, &task, false)
	result := lifecycleSnapshot(t, op, id, true)
	if result.LastResult == nil || result.LastResult.State != "failed" || result.LastResult.Answer != "committed answer" {
		t.Fatalf("process failure did not override task success while retaining answer: %+v", result)
	}
	var final, failed, completed int
	for len(observed) > 0 {
		switch (<-observed)["type"] {
		case "agent.final":
			final++
		case "agent.failed":
			failed++
		case "agent.completed":
			completed++
		}
	}
	if final != 1 || failed != 1 || completed != 0 {
		t.Fatalf("duplicate/premature task terminal events: final=%d failed=%d completed=%d", final, failed, completed)
	}
}
