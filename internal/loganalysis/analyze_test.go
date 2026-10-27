package loganalysis

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnalyzeCursorCoversExecutionWhileStateUsesFullRun(t *testing.T) {
	input := strings.Join([]string{
		`{"executionId":"chosen","seq":1,"type":"agent.started","runId":"target","name":"original"}`,
		`{"executionId":"elsewhere","seq":900,"type":"agent.completed","runId":"other"}`,
		`{"executionId":"chosen","seq":2,"type":"agent.message","runId":"target","text":"before cursor"}`,
		`{"executionId":"chosen","seq":3,"type":"agent.final","runId":"target","text":"after cursor"}`,
		`{"executionId":"chosen","seq":70,"type":"not.projected","runId":"target"}`,
		`{"executionId":"chosen","seq":80,"type":"not.projected","runId":"other"}`,
	}, "\n") + "\n"
	q := Query{RunID: "target", Cursor: &Cursor{ExecutionID: "chosen", Seq: 2}}
	snapshot, err := Analyze(t.Context(), strings.NewReader(input), q)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Cursor != (Cursor{ExecutionID: "chosen", Seq: 80}) {
		t.Fatalf("cursor = %+v, want chosen/80", snapshot.Cursor)
	}
	if snapshot.Status != "running" || snapshot.Name != "original" || snapshot.LastMessage != "before cursor" || snapshot.FinalText != "after cursor" {
		t.Fatalf("state did not reflect full target run: %+v", snapshot)
	}
	if len(snapshot.Events) != 1 || snapshot.Events[0]["type"] != "agent.final" || snapshot.Events[0]["text"] != "after cursor" {
		t.Fatalf("events = %+v", snapshot.Events)
	}

	// With no later other-run event, the unprojected target event still advances
	// the execution cursor; a cursor filters events, not reconstructed state.
	input = strings.Replace(input, `{"executionId":"chosen","seq":80,"type":"not.projected","runId":"other"}`+"\n", "", 1)
	q.Cursor.Seq = 70
	snapshot, err = Analyze(t.Context(), strings.NewReader(input), q)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Cursor.Seq != 70 || snapshot.LastMessage != "before cursor" || snapshot.FinalText != "after cursor" || snapshot.Events == nil || len(snapshot.Events) != 0 {
		t.Fatalf("hidden event cursor or full state changed: %+v", snapshot)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil || !strings.Contains(string(encoded), `"events":[]`) {
		t.Fatalf("empty events JSON = %s, err=%v", encoded, err)
	}
}
