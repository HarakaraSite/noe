package observation

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDecisionObservationPreservesParentModelAndPhase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeEvent(t, path, 1, "agent.started", map[string]any{"runId": "parent", "model": "chat/model"})
	writeEvent(t, path, 2, "run_js.started", map[string]any{"runId": "parent", "toolCallId": "tool", "text": "// classify"})
	q := Query{LogPath: path, RunID: "parent"}
	initial, err := Observe(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{"decision.started", "decision.completed"} {
		writeEvent(t, path, uint64(i+3), kind, map[string]any{"runId": "parent", "model": "chat/model", "decisionModel": "decision/model", "responseModel": "decision/revision", "requestId": "d", "toolCallId": "tool"})
	}
	q.Cursor = &initial.Cursor
	got, err := Observe(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "chat/model" || got.Phase != "run_js" || got.Status != "running" || len(got.Events) != 2 {
		t.Fatalf("snapshot=%+v", got)
	}
	for _, e := range got.Events {
		if e["decisionModel"] != "decision/model" || e["responseModel"] != "decision/revision" {
			t.Fatalf("missing model fields: %+v", e)
		}
	}
}
