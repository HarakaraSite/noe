package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
)

func TestObserveDisplaysDetachedDiagnosticsWithoutReclassifyingStderr(t *testing.T) {
	event := events.Event{
		"type": "run_js.finished", "runId": "run", "success": false, "reason": "failed",
		"data": map[string]any{"largeResult": "kept in recording"},
		"diagnostics": []executionresult.Diagnostic{{
			Code: "serialization_error", Stage: "serialize", Message: "cycle after write",
			Stack:     "Error: cycle after write\n at task.mjs:3:1",
			Exception: map[string]any{"code": "USER_CODE"},
		}},
	}
	for _, saved := range []bool{false, true} {
		input := event
		if saved {
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &input); err != nil {
				t.Fatal(err)
			}
		}
		var out bytes.Buffer
		if err := newAgentRenderer(&out).render(input); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		for _, want := range []string{"run_js failed", "diagnostic serialization_error [serialize]", "task.mjs:3:1", "USER_CODE"} {
			if !strings.Contains(text, want) {
				t.Fatalf("saved=%v missing %q: %s", saved, want, text)
			}
		}
		if strings.Contains(text, "stderr:") || strings.Contains(text, "largeResult") {
			t.Fatalf("diagnostic was mixed with output: %s", text)
		}
	}
}
