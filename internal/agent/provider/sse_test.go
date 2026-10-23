package provider

import (
	"encoding/json"
	"testing"
)

func TestObservedBodySnapshotCopiesOuterSliceAndKeepsPayloadStable(t *testing.T) {
	body := newObservedBody(nil)
	body.mu.Lock()
	body.feedLocked([]byte("data: {\"id\":\"first\"}\n\n"))
	body.mu.Unlock()

	snapshot, done, readErr := body.snapshot()
	if done || readErr != nil || len(snapshot) != 1 {
		t.Fatalf("snapshot = %q, done=%t, err=%v", snapshot, done, readErr)
	}
	first := string(snapshot[0])
	body.mu.Lock()
	if &snapshot[0][0] != &body.events[0][0] {
		body.mu.Unlock()
		t.Fatal("snapshot copied immutable payload bytes")
	}
	body.mu.Unlock()

	outerCopy, _, _ := body.snapshot()
	outerCopy[0] = json.RawMessage(`{"id":"changed"}`)
	unchanged, _, _ := body.snapshot()
	if string(unchanged[0]) != first {
		t.Fatalf("snapshot element replacement changed stored event: %s", unchanged[0])
	}

	body.mu.Lock()
	body.feedLocked([]byte("data: {\"id\":\"second\"}\n\n"))
	body.mu.Unlock()
	if string(snapshot[0]) != first {
		t.Fatalf("later feed changed prior snapshot payload: %s", snapshot[0])
	}
}
