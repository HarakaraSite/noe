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

func TestObserveProjectsContextTrimmedEventAndSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeEvent(t, path, 1, "agent.started", map[string]any{"runId": "r"})
	writeEvent(t, path, 2, "context.trimmed", map[string]any{
		"runId": "r", "activityId": "trim-1", "modelStepId": "step-2",
		"beforeTokens": 1200, "afterTokens": 700, "removedBlocks": 6,
		"reason": "input_budget", "budgetSource": "fallback",
		"causedBy": []string{"command-7"},
		"origin":   map[string]any{"kind": "context", "runId": "r", "modelStepId": "step-2"},
		"context": map[string]any{
			"executionContext": map[string]any{
				"policy": "auto", "budgetSource": "fallback", "estimatedTokens": 700,
				"budgetTokens": 800, "usedPercent": 87.5, "trimCount": 1,
			},
			"budget": map[string]any{"inputTokens": nil},
		},
	})

	cursor := &Cursor{ExecutionID: "exec", Seq: 1}
	snapshot, err := Observe(t.Context(), Query{LogPath: path, RunID: "r", Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0]["type"] != "context.trimmed" {
		t.Fatalf("trim event was not projected: %#v", snapshot.Events)
	}
	event := snapshot.Events[0]
	if event["activityId"] != "trim-1" || event["budgetSource"] != "fallback" || event["beforeTokens"] != json.Number("1200") || event["afterTokens"] != json.Number("700") || event["removedBlocks"] != json.Number("6") {
		t.Fatalf("trim facts were lost: %#v", event)
	}
	if causes, ok := event["causedBy"].([]any); !ok || len(causes) != 1 || causes[0] != "command-7" {
		t.Fatalf("trim causes were lost: %#v", event["causedBy"])
	}
	var contextSnapshot map[string]any
	if err := json.Unmarshal(snapshot.Context, &contextSnapshot); err != nil {
		t.Fatal(err)
	}
	execution, ok := contextSnapshot["executionContext"].(map[string]any)
	if !ok || execution["estimatedTokens"] != float64(700) || execution["trimCount"] != float64(1) {
		t.Fatalf("execution context snapshot was not retained: %#v", contextSnapshot)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(encoded), "tool stdout") {
		t.Fatalf("trim observation included deleted tool output: %s %v", encoded, err)
	}
}
