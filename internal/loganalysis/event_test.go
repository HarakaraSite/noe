package loganalysis

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

// legacyAppendProjected is the R2 appendProjected algorithm, retained only
// for output comparison and the bounded before/after microbenchmark.
func legacyAppendProjected(eventsOut *[]events.Event, event events.Event, seq uint64, previousSeq *uint64) {
	if len(*eventsOut) > 0 && seq == *previousSeq+1 && isThinkingType(stringField(event, "type")) {
		previous := (*eventsOut)[len(*eventsOut)-1]
		if stringField(previous, "type") == stringField(event, "type") &&
			stringField(previous, "requestId") == stringField(event, "requestId") {
			previous["text"] = stringField(previous, "text") + stringField(event, "text")
			*previousSeq = seq
			return
		}
	}
	*eventsOut = append(*eventsOut, event)
	*previousSeq = seq
}

func TestAnalyzeThinkingGroups(t *testing.T) {
	tests := []struct {
		name  string
		input string
		query Query
		want  string
	}{
		{
			name: "types requests and projected boundary",
			input: `{"executionId":"e","seq":1,"type":"thinking","runId":"r","requestId":"a","text":"one","marker":"first"}
{"executionId":"e","seq":2,"type":"thinking","runId":"r","requestId":"a","text":" two","marker":"discarded"}
{"executionId":"e","seq":3,"type":"thinking_summary","runId":"r","requestId":"a","text":"sum","marker":"summary"}
{"executionId":"e","seq":4,"type":"thinking_summary","runId":"r","requestId":"a","text":"!"}
{"executionId":"e","seq":5,"type":"thinking","runId":"r","requestId":"b","text":"other"}
{"executionId":"e","seq":6,"type":"thinking","runId":"r","requestId":"a","text":"again"}
{"executionId":"e","seq":7,"type":"agent.message","runId":"r","text":"interrupt"}
{"executionId":"e","seq":8,"type":"thinking","runId":"r","requestId":"a","text":"after"}
`,
			query: Query{RunID: "r", IncludeThinking: true},
			want: `[
{"executionId":"e","seq":1,"type":"thinking","runId":"r","requestId":"a","text":"one two","marker":"first"},
{"executionId":"e","seq":3,"type":"thinking_summary","runId":"r","requestId":"a","text":"sum!","marker":"summary"},
{"executionId":"e","seq":5,"type":"thinking","runId":"r","requestId":"b","text":"other"},
{"executionId":"e","seq":6,"type":"thinking","runId":"r","requestId":"a","text":"again"},
{"executionId":"e","seq":7,"type":"agent.message","runId":"r","text":"interrupt"},
{"executionId":"e","seq":8,"type":"thinking","runId":"r","requestId":"a","text":"after"}]`,
		},
		{
			name: "hidden and other-run raw sequence gaps",
			input: `{"executionId":"e","seq":1,"type":"thinking","runId":"r","requestId":"a","text":"a"}
{"executionId":"e","seq":2,"type":"hidden","runId":"r"}
{"executionId":"e","seq":3,"type":"thinking","runId":"r","requestId":"a","text":"b"}
{"executionId":"e","seq":4,"type":"thinking","runId":"other","requestId":"a","text":"other"}
{"executionId":"e","seq":5,"type":"thinking","runId":"r","requestId":"a","text":"c"}
{"executionId":"e","seq":6,"type":"stdout","runId":"r","text":"hidden output"}
{"executionId":"e","seq":7,"type":"thinking","runId":"r","requestId":"a","text":"d"}
`,
			query: Query{RunID: "r", IncludeThinking: true},
			want: `[
{"executionId":"e","seq":1,"type":"thinking","runId":"r","requestId":"a","text":"a"},
{"executionId":"e","seq":3,"type":"thinking","runId":"r","requestId":"a","text":"b"},
{"executionId":"e","seq":5,"type":"thinking","runId":"r","requestId":"a","text":"c"},
{"executionId":"e","seq":7,"type":"thinking","runId":"r","requestId":"a","text":"d"}]`,
		},
		{
			name: "cursor starts a fresh group",
			input: `{"executionId":"e","seq":1,"type":"thinking","runId":"r","requestId":"a","text":"before","marker":"before"}
{"executionId":"e","seq":2,"type":"thinking","runId":"r","requestId":"a","text":"after","marker":"first visible"}
{"executionId":"e","seq":3,"type":"thinking","runId":"r","requestId":"a","text":" cursor","marker":"later"}
`,
			query: Query{RunID: "r", IncludeThinking: true, Cursor: &Cursor{ExecutionID: "e", Seq: 1}},
			want:  `[{"executionId":"e","seq":2,"type":"thinking","runId":"r","requestId":"a","text":"after cursor","marker":"first visible"}]`,
		},
		{
			name: "empty missing and non-string text",
			input: `{"executionId":"e","seq":1,"type":"thinking","runId":"r","requestId":"a"}
{"executionId":"e","seq":2,"type":"thinking","runId":"r","requestId":"a","text":42}
{"executionId":"e","seq":3,"type":"thinking","runId":"r","requestId":"b","text":""}
{"executionId":"e","seq":4,"type":"thinking","runId":"r","requestId":"b","text":""}
{"executionId":"e","seq":5,"type":"thinking","runId":"r","requestId":"c","text":17}
{"executionId":"e","seq":6,"type":"thinking","runId":"r","requestId":"c","text":"tail"}
{"executionId":"e","seq":7,"type":"thinking","runId":"r","requestId":"d"}
{"executionId":"e","seq":8,"type":"thinking_summary","runId":"r","requestId":"d","text":false}
{"executionId":"e","seq":9,"type":"thinking","runId":"r","text":"missing request"}
{"executionId":"e","seq":10,"type":"thinking","runId":"r","requestId":99,"text":" and non-string request"}
`,
			query: Query{RunID: "r", IncludeThinking: true},
			want: `[
{"executionId":"e","seq":1,"type":"thinking","runId":"r","requestId":"a","text":""},
{"executionId":"e","seq":3,"type":"thinking","runId":"r","requestId":"b","text":""},
{"executionId":"e","seq":5,"type":"thinking","runId":"r","requestId":"c","text":"tail"},
{"executionId":"e","seq":7,"type":"thinking","runId":"r","requestId":"d"},
{"executionId":"e","seq":8,"type":"thinking_summary","runId":"r","requestId":"d","text":false},
{"executionId":"e","seq":9,"type":"thinking","runId":"r","text":"missing request and non-string request"}]`,
		},
		{
			name: "thinking disabled",
			input: `{"executionId":"e","seq":1,"type":"thinking","runId":"r","text":"hidden"}
{"executionId":"e","seq":2,"type":"thinking_summary","runId":"r","text":"hidden"}
{"executionId":"e","seq":3,"type":"agent.message","runId":"r","text":"visible"}
`,
			query: Query{RunID: "r"},
			want:  `[{"executionId":"e","seq":3,"type":"agent.message","runId":"r","text":"visible"}]`,
		},
		{
			name: "thinking disabled with no projected events",
			input: `{"executionId":"e","seq":1,"type":"thinking","runId":"r","text":"hidden"}
`,
			query: Query{RunID: "r"},
			want:  `[]`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, err := Analyze(t.Context(), strings.NewReader(tt.input), tt.query)
			if err != nil {
				t.Fatal(err)
			}
			var want []events.Event
			decoder := json.NewDecoder(strings.NewReader(tt.want))
			decoder.UseNumber()
			if err := decoder.Decode(&want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(snapshot.Events, want) {
				t.Fatalf("events = %#v, want %#v", snapshot.Events, want)
			}

			// Reconstruct just the projected event stream using the R2 algorithm.
			records, err := readRecords(t.Context(), strings.NewReader(tt.input))
			if err != nil {
				t.Fatal(err)
			}
			legacy := make([]events.Event, 0)
			var previousSeq uint64
			for _, rec := range records {
				if rec.executionID == "e" && stringField(rec.event, "runId") == tt.query.RunID && rec.seq > cursorSeq(tt.query.Cursor) {
					if event := projectEvent(rec.event, tt.query); event != nil {
						legacyAppendProjected(&legacy, event, rec.seq, &previousSeq)
					}
				}
			}
			if !reflect.DeepEqual(snapshot.Events, legacy) {
				t.Fatalf("current events = %#v, legacy = %#v", snapshot.Events, legacy)
			}
		})
	}
}

var benchmarkThinkingEvents []events.Event

func BenchmarkThinkingFragments(b *testing.B) {
	const count = 512
	fragment := strings.Repeat("x", 32)
	fixture := make([]events.Event, count)
	for i := range fixture {
		fixture[i] = events.Event{"type": "thinking", "requestId": "request", "seq": uint64(i + 1), "text": fragment, "marker": "metadata"}
	}

	legacy := func() []events.Event {
		var out []events.Event
		var previousSeq uint64
		for i, event := range fixture {
			legacyAppendProjected(&out, cloneEvent(event), uint64(i+1), &previousSeq)
		}
		return out
	}
	current := func() []events.Event {
		var out []events.Event
		group := projectedEvents{events: &out}
		for i, event := range fixture {
			group.append(cloneEvent(event), uint64(i+1))
		}
		group.finishGroup()
		return out
	}
	before, after := legacy(), current()
	if !reflect.DeepEqual(before, after) || len(after) != 1 ||
		stringField(after[0], "text") != strings.Repeat(fragment, count) ||
		after[0]["seq"] != uint64(1) || after[0]["marker"] != "metadata" {
		b.Fatalf("legacy and current output differs or lost text/metadata")
	}

	b.Run("legacy", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			benchmarkThinkingEvents = legacy()
		}
	})
	b.Run("current", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			benchmarkThinkingEvents = current()
		}
	})
}
