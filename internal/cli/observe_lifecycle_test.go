package cli

import (
	"bytes"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestObserveDistinguishesTaskCompletionFromInstanceIdle(t *testing.T) {
	var out bytes.Buffer
	renderer := newAgentRenderer(&out)
	for _, event := range []events.Event{
		{"type": "instance.started", "agentId": "abcdefgh-full", "name": "worker"},
		{"type": "agent.completed", "agentId": "abcdefgh-full", "runId": "12345678-full", "name": "worker"},
		{"type": "instance.idle", "agentId": "abcdefgh-full", "name": "worker"},
		{"type": "instance.stopped", "agentId": "abcdefgh-full", "name": "worker", "reason": "owner_cancelled"},
	} {
		if err := renderer.render(event); err != nil {
			t.Fatal(err)
		}
	}
	text := out.String()
	for _, want := range []string{"[worker/abcdefgh] agent started", "[worker/12345678] completed", "[worker/abcdefgh] agent idle", "[worker/abcdefgh] agent stopped: owner_cancelled"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestObserveShowsControlReceiptAndApplication(t *testing.T) {
	var out bytes.Buffer
	renderer := newAgentRenderer(&out)
	for _, event := range []events.Event{
		{"type": "command.accepted", "operation": "queue", "commandId": "hidden-initial", "status": "accepted", "agentId": "agent-123"},
		{"type": "command.accepted", "operation": "steer", "commandId": "steer-123", "status": "accepted", "runId": "run-123"},
		{"type": "command.applied", "operation": "steer", "commandId": "steer-123", "status": "applied", "runId": "run-123"},
		{"type": "interrupt.accepted", "target": "agent", "agentId": "agent-123", "status": "accepted"},
	} {
		if err := renderer.render(event); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Contains(out.String(), "hidden-initial") || !strings.Contains(out.String(), "steer accepted: steer-123") || !strings.Contains(out.String(), "steer applied: steer-123") || !strings.Contains(out.String(), "interrupt accepted: agent") {
		t.Fatalf("control display: %s", &out)
	}
}
