package cli

import (
	"bytes"
	"noe/internal/events"
	"strings"
	"testing"
)

func TestImageHumanObservationShowsPathAndFailureKind(t *testing.T) {
	var out bytes.Buffer
	r := newAgentRenderer(&out)
	for _, kind := range []string{"image.read", "image.started", "image.completed", "image.failed", "image.cancelled"} {
		if err := r.render(events.Event{"type": kind, "runId": "parent-run", "name": "parent", "model": "chat/model", "imageModel": "image/model", "path": "/tmp/image.png", "reason": "refusal", "toolCallId": "tool-1"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"image read", "image started", "image completed", "image failed", "image cancelled", "image/model / /tmp/image.png / refusal"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, &out)
		}
	}
}
