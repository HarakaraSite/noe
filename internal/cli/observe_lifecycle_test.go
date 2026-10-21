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
		{"type": "agent.started", "agentId": "abcdefgh-full", "runId": "12345678-full", "name": "worker", "model": "fixture/model"},
		{"type": "agent.completed", "agentId": "abcdefgh-full", "runId": "12345678-full", "name": "worker"},
		{"type": "instance.idle", "agentId": "abcdefgh-full", "name": "worker"},
		{"type": "instance.stopped", "agentId": "abcdefgh-full", "name": "worker", "reason": "owner_cancelled"},
	} {
		if err := renderer.render(event); err != nil {
			t.Fatal(err)
		}
	}
	text := out.String()
	for _, want := range []string{"[worker agent=abcdefgh]\nagent started", "[worker agent=abcdefgh run=12345678]\nstarted: fixture/model", "[worker run=12345678]\ncompleted", "[worker agent=abcdefgh]\nagent idle", "[worker agent=abcdefgh]\nagent stopped: owner_cancelled"} {
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

func TestObserveShowsHumanControlInstructionsOnce(t *testing.T) {
	var out bytes.Buffer
	renderer := newAgentRenderer(&out)
	human := map[string]any{"kind": "human"}
	queueInstruction := "**入力をそのまま**\n次の行\n"
	steerInstruction := "  補足してください。\n  インデントも維持"
	for _, event := range []events.Event{
		{"type": "command.accepted", "operation": "queue", "status": "accepted", "commandId": "human-queue", "agentId": "abcdefgh-full", "name": "worker", "origin": human, "instruction": queueInstruction},
		{"type": "command.completed", "operation": "queue", "status": "completed", "commandId": "human-queue", "runId": "12345678-full", "name": "worker", "origin": human, "instruction": queueInstruction},
		{"type": "command.accepted", "operation": "steer", "status": "accepted", "commandId": "human-steer", "runId": "12345678-full", "name": "worker", "origin": human, "instruction": steerInstruction},
		{"type": "command.applied", "operation": "steer", "status": "applied", "commandId": "human-steer", "runId": "12345678-full", "name": "worker", "origin": human, "instruction": steerInstruction},
		{"type": "command.cancelled", "operation": "steer", "status": "cancelled", "commandId": "cancelled-steer", "runId": "12345678-full", "name": "worker", "origin": human},
		{"type": "command.cancelled", "operation": "queue", "status": "cancelled", "commandId": "cancelled-queue", "agentId": "abcdefgh-full", "name": "worker", "origin": human},
		{"type": "command.failed", "operation": "queue", "status": "failed", "commandId": "failed-queue", "runId": "12345678-full", "name": "worker", "origin": human},
	} {
		if err := renderer.render(event); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{
		"[worker agent=abcdefgh]\nqueue accepted: human-queue\n" + queueInstruction,
		"[worker run=12345678]\nqueue completed: human-queue\n",
		"[worker run=12345678]\nsteer accepted: human-steer\n" + steerInstruction + "\n",
		"steer applied: human-steer\n",
		"steer cancelled: cancelled-steer\n",
		"queue cancelled: cancelled-queue\n",
		"queue failed: failed-queue\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, &out)
		}
	}
	for _, instruction := range []string{queueInstruction, steerInstruction} {
		if got := strings.Count(out.String(), instruction); got != 1 {
			t.Errorf("instruction count=%d in:\n%s", got, &out)
		}
	}
}

func TestObserveKeepsInternalQueuesHiddenAndOldControlEventsReadable(t *testing.T) {
	var out bytes.Buffer
	renderer := newAgentRenderer(&out)
	for _, origin := range []any{nil, map[string]any{"kind": "script"}, map[string]any{"kind": "tool"}, map[string]any{"kind": "hook"}} {
		for _, status := range []string{"accepted", "completed", "failed", "cancelled"} {
			if err := renderer.render(events.Event{
				"type": "command." + status, "operation": "queue", "status": status,
				"commandId": "hidden-queue", "origin": origin, "instruction": "hidden queue instruction",
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, event := range []events.Event{
		{"type": "command.accepted", "operation": "steer", "status": "accepted", "commandId": "internal-steer", "origin": map[string]any{"kind": "script"}, "instruction": "hidden steer instruction"},
		{"type": "command.accepted", "operation": "steer", "status": "accepted", "commandId": "old-steer"},
		{"type": "command.accepted", "operation": "queue", "status": "accepted", "commandId": "old-human-queue", "origin": map[string]any{"kind": "human"}},
	} {
		if err := renderer.render(event); err != nil {
			t.Fatal(err)
		}
	}
	for _, hidden := range []string{"hidden-queue", "hidden queue instruction", "hidden steer instruction"} {
		if strings.Contains(out.String(), hidden) {
			t.Errorf("unexpected %q in:\n%s", hidden, &out)
		}
	}
	for _, want := range []string{"steer accepted: internal-steer\n", "steer accepted: old-steer\n", "queue accepted: old-human-queue\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, &out)
		}
	}
}
