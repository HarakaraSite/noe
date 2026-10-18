package observation

import (
	"context"
	"path/filepath"
	"testing"
)

func TestImageObservationPreservesParentIdentityAndPhase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeEvent(t, path, 1, "agent.started", map[string]any{"runId": "parent", "model": "chat/model"})
	writeEvent(t, path, 2, "run_js.started", map[string]any{"runId": "parent", "toolCallId": "tool", "text": "Read and generate"})
	q := Query{LogPath: path, RunID: "parent"}
	initial, err := Observe(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{"image.read", "image.started", "image.completed", "image.failed", "image.cancelled"} {
		writeEvent(t, path, uint64(i+3), kind, map[string]any{"runId": "parent", "model": "chat/model", "imageModel": "image/model", "path": "/tmp/image.png", "toolCallId": "tool"})
	}
	q.Cursor = &initial.Cursor
	got, err := Observe(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "chat/model" || got.Phase != "run_js" || got.Status != "running" || len(got.Events) != 5 {
		t.Fatalf("snapshot=%+v", got)
	}
}
