package observation

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestObserveContextAndParallelHookSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeEvent(t, path, 1, "agent.started", map[string]any{"runId": "r", "groupId": "group-A"})
	writeEvent(t, path, 2, "context.updated", map[string]any{"runId": "r", "context": map[string]any{"runId": "r", "budget": map[string]any{"inputTokens": 1000}, "previous": nil}, "execution": map[string]any{"groupId": "group-A", "counters": map[string]any{"providerCalls": 1}}})
	writeEvent(t, path, 3, "hook.started", map[string]any{"runId": "r", "invocationId": "a", "phase": "beforeTool", "module": "first.mjs"})
	writeEvent(t, path, 4, "hook.started", map[string]any{"runId": "r", "invocationId": "b", "phase": "afterTool", "module": "second.mjs"})
	writeEvent(t, path, 5, "hook.finished", map[string]any{"runId": "r", "invocationId": "a"})
	snapshot, err := Observe(t.Context(), Query{LogPath: path, RunID: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GroupID != "group-A" || snapshot.Phase != "hook" || snapshot.Action != "afterTool second.mjs" || !strings.Contains(string(snapshot.Context), `"inputTokens":1000`) || !strings.Contains(string(snapshot.Execution), `"providerCalls":1`) {
		t.Fatalf("wrong state: %+v", snapshot)
	}
	cursor := snapshot.Cursor
	writeEvent(t, path, 6, "hook.cancelled", map[string]any{"runId": "r", "invocationId": "b"})
	writeEvent(t, path, 7, "tool.finished", map[string]any{"runId": "r", "toolName": "fixture", "rawResult": map[string]any{"stdout": "captured raw output"}, "result": "changed model output", "modelResultAvailable": true, "toolExecution": map[string]any{"success": true}})
	writeEvent(t, path, 8, "agent.cancelled", map[string]any{"runId": "r"})
	snapshot, err = Observe(t.Context(), Query{LogPath: path, RunID: "r", Cursor: &cursor})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Phase != "settled" || len(snapshot.Events) != 3 || snapshot.GroupID != "group-A" || len(snapshot.Context) == 0 {
		t.Fatalf("delta lost state: %+v", snapshot)
	}
	data, err := json.Marshal(snapshot)
	if err != nil || !strings.Contains(string(data), `"context":{"budget"`) {
		t.Fatalf("context became string: %s %v", data, err)
	}
	if strings.Contains(string(data), "captured raw output") || strings.Contains(string(data), "changed model output") || !strings.Contains(string(data), `"toolExecution":{"success":true}`) {
		t.Fatalf("output opt-in bypassed or execution facts lost: %s", data)
	}
	full, err := Observe(t.Context(), Query{LogPath: path, RunID: "r", Cursor: &cursor, IncludeOutput: true})
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(full)
	if err != nil || !strings.Contains(string(data), "captured raw output") || !strings.Contains(string(data), "changed model output") {
		t.Fatalf("raw/effective output lost from opt-in: %s %v", data, err)
	}
}
