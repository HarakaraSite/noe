package loganalysis

import (
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"testing"
)

func TestRunStateReleasesOnlyFinishedToolOutput(t *testing.T) {
	s := newRunState("r")
	seq := uint64(0)
	apply := func(kind, id, text string) {
		seq++
		s.apply(record{seq: seq, typeName: kind, event: events.Event{
			"toolCallId": id, "source": "run_js", "text": text, "success": true,
		}}, true)
	}
	apply("run_js.started", "a", "// A")
	apply("run_js.started", "b", "// B")
	apply("stdout", "a", "A stdout\n")
	apply("stdout", "b", "B stdout\n")
	apply("stderr", "a", "A stderr\n")
	apply("run_js.finished", "a", "")
	first := s.snapshot.LastToolResult
	if first == nil || first.Text != "A stdout\nA stderr\n" {
		t.Fatalf("A result = %+v", first)
	}
	if _, ok := s.outputs["a"]; ok {
		t.Error("finished tool A output buffer was retained")
	}
	if output := s.outputs["b"]; output == nil || output.text.String() != "B stdout\n" {
		t.Fatalf("active tool B output was lost: %+v", output)
	}
	if s.currentPhase() != "run_js" || s.currentAction() != "// B" {
		t.Fatalf("active tool B phase/action = %q/%q", s.currentPhase(), s.currentAction())
	}
	apply("stderr", "b", "B stderr\n")
	apply("run_js.finished", "b", "")
	if result := s.snapshot.LastToolResult; result == nil || result.ToolCallID != "b" || result.Text != "B stdout\nB stderr\n" {
		t.Fatalf("B result = %+v", result)
	}
	if len(s.outputs) != 0 {
		t.Errorf("finished output buffers retained: %d", len(s.outputs))
	}
	if first.Text != "A stdout\nA stderr\n" {
		t.Fatalf("retained A result changed: %+v", first)
	}
}
