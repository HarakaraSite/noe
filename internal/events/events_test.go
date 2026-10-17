package events

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
)

func TestConcurrentRecordsHaveOneSequence(t *testing.T) {
	var out bytes.Buffer
	w := New(&out, "execution")
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Go(func() {
			for j := 0; j < 20; j++ {
				w.Emit(Event{"type": "thinking", "text": "日本語\nfragment"})
			}
		})
	}
	group.Wait()
	decoder := json.NewDecoder(&out)
	for i := 1; i <= 160; i++ {
		var event Event
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		if event["seq"] != float64(i) || event["executionId"] != "execution" || event["time"] == "" {
			t.Fatalf("event: %+v", event)
		}
	}
	if decoder.More() {
		t.Fatal("unexpected records")
	}
}
