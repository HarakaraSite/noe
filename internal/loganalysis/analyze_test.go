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

func TestAnalyzeCursorIncludesRecordsBeforeTargetRunIsFound(t *testing.T) {
	input := `{"executionId":"chosen","seq":80,"type":"execution.started"}
{"executionId":"elsewhere","seq":900,"type":"agent.started","runId":"other"}
{"executionId":"chosen","seq":1,"type":"agent.started","runId":"target","name":"original"}
{"executionId":"elsewhere","seq":901,"type":"agent.final","runId":"other","text":"other execution"}
{"executionId":"chosen","seq":3,"type":"agent.final","runId":"target","text":"chosen execution"}
{"executionId":"chosen","seq":99,"type":"agent.completed","runId":"target"}`
	query := Query{RunID: "target", Cursor: &Cursor{ExecutionID: "chosen", Seq: 1}}
	snapshot, err := Analyze(t.Context(), strings.NewReader(input), query)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Cursor != (Cursor{ExecutionID: "chosen", Seq: 80}) || snapshot.Status != "running" ||
		snapshot.Name != "original" || snapshot.FinalText != "chosen execution" ||
		len(snapshot.Events) != 1 || snapshot.Events[0]["type"] != "agent.final" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
}

func TestAnalyzeScanErrorsTakePrecedenceOverSelection(t *testing.T) {
	const first = `{"executionId":"first","seq":1,"type":"agent.started","runId":"target"}` + "\n"
	const second = `{"executionId":"second","seq":1,"type":"agent.started","runId":"target"}` + "\n"
	const invalid = `{"executionId":"other","type":"agent.started","runId":"unrelated"}` + "\n"
	query := Query{RunID: "target", Cursor: &Cursor{ExecutionID: "unrelated", Seq: 1}}
	for _, tc := range []struct {
		name string
		log  string
		want string
	}{
		{"cursor mismatch", first + strings.TrimSuffix(second, "\n"), `cursor executionId "unrelated" does not match`},
		{"ambiguity before cursor mismatch", first + second, `the target is ambiguous`},
		{"invalid line after cursor mismatch", first + invalid, `invalid log at complete line 2:`},
		{"invalid line after ambiguity", first + second + invalid, `invalid log at complete line 3:`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Analyze(t.Context(), strings.NewReader(tc.log), query)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
