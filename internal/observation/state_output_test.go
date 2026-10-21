package observation

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestObserveReusedToolCallIDOutput(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stdout string
		stderr string
		failed bool
	}{
		{name: "stdout", stdout: "new stdout\n"},
		{name: "stderr", stderr: "new stderr\n"},
		{name: "empty"},
		{name: "failed", stdout: "partial stdout\n", stderr: "failure stderr\n", failed: true},
		{name: "failed empty", failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "events.jsonl")
			seq := uint64(0)
			emit := func(step, kind string, fields map[string]any) {
				seq++
				fields["runId"], fields["modelStepId"], fields["toolCallId"] = "r", step, "reused"
				fields["source"] = "run_js"
				writeEvent(t, path, seq, kind, fields)
			}
			emit("step-1", "run_js.started", map[string]any{"text": "// first"})
			emit("step-1", "stdout", map[string]any{"text": "old stdout\n"})
			emit("step-1", "stderr", map[string]any{"text": "old stderr\n"})
			emit("step-1", "run_js.finished", map[string]any{"success": true, "exitCode": 0, "reason": "completed"})
			query := Query{LogPath: path, RunID: "r", IncludeOutput: true}
			first, err := Observe(context.Background(), query)
			if err != nil {
				t.Fatal(err)
			}
			if first.LastToolResult == nil || first.LastToolResult.Text != "old stdout\nold stderr\n" {
				t.Fatalf("first result = %+v", first.LastToolResult)
			}
			query.Cursor = &first.Cursor

			emit("step-2", "run_js.started", map[string]any{"text": "// second"})
			if tc.stdout != "" {
				emit("step-2", "stdout", map[string]any{"text": tc.stdout})
			}
			if tc.stderr != "" {
				emit("step-2", "stderr", map[string]any{"text": tc.stderr})
			}
			exitCode, reason := 0, "completed"
			if tc.failed {
				exitCode, reason = 7, "failed"
			}
			emit("step-2", "run_js.finished", map[string]any{"success": !tc.failed, "exitCode": exitCode, "reason": reason, "duration": 123})
			second, err := Observe(context.Background(), query)
			if err != nil {
				t.Fatal(err)
			}
			want := &ToolResult{ToolCallID: "reused", Success: !tc.failed, ExitCode: exitCode, Reason: reason, Duration: 123, Text: tc.stdout + tc.stderr}
			if !reflect.DeepEqual(second.LastToolResult, want) {
				t.Errorf("second result = %+v, want %+v", second.LastToolResult, want)
			}
			if second.Cursor.Seq != seq || len(second.Events) != int(seq-first.Cursor.Seq) {
				t.Fatalf("cursor/events = %+v/%d, want seq %d and %d events", second.Cursor, len(second.Events), seq, seq-first.Cursor.Seq)
			}
			for _, event := range second.Events {
				if event["modelStepId"] != "step-2" {
					t.Fatalf("cursor leaked earlier event: %+v", event)
				}
			}
			query.Cursor = &second.Cursor
			repeated, err := Observe(context.Background(), query)
			if err != nil {
				t.Fatal(err)
			}
			if len(repeated.Events) != 0 || repeated.Cursor != second.Cursor || !reflect.DeepEqual(repeated.LastToolResult, second.LastToolResult) {
				t.Fatalf("repeated cursor changed state or returned events: %+v", repeated)
			}
			query.IncludeOutput = false
			withoutOutput, err := Observe(context.Background(), query)
			if err != nil {
				t.Fatal(err)
			}
			want.Text = ""
			if !reflect.DeepEqual(withoutOutput.LastToolResult, want) || len(withoutOutput.Events) != 0 {
				t.Fatalf("output-disabled cursor result = %+v", withoutOutput)
			}
		})
	}
}
