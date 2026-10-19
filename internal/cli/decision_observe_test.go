package cli

import (
	"bytes"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestDecisionHumanObservation(t *testing.T) {
	var out bytes.Buffer
	r := newAgentRenderer(&out)
	for _, kind := range []string{"decision.started", "decision.completed", "decision.failed", "decision.cancelled"} {
		if err := r.render(events.Event{"type": kind, "executionId": "exec", "runId": "parent-run", "name": "parent", "model": "chat/model", "decisionModel": "decision/model", "responseModel": "decision/revision", "requestId": "abcdef012345", "duration": float64(3000000), "toolCallId": "call-tool", "reason": "fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	text := out.String()
	for _, want := range []string{"decision started", "decision completed", "decision failed", "decision cancelled", "decision/model [abcdef01] -> decision/revision", "[parent run=parent-r tool=tool]\n", "3ms"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
}
