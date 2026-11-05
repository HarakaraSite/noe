package events

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"io"
	"strings"
	"testing"
	"time"
)

func TestJSONV2WriterKeepsJSONLFraming(t *testing.T) {
	var out bytes.Buffer
	w := New(&out, "exec")
	w.Emit(Event{"type": "data", "values": []string(nil), "text": "<東京>"})
	w.Emit(Event{"type": "data", "counter": uint64(9007199254740993)})
	if w.Err() != nil {
		t.Fatal(w.Err())
	}
	if bytes.Count(out.Bytes(), []byte{'\n'}) != 2 || !bytes.HasSuffix(out.Bytes(), []byte{'\n'}) {
		t.Fatalf("expected two complete JSONL records: %s", out.Bytes())
	}
	decoder := jsontext.NewDecoder(&out)
	var first, second map[string]jsonv1.RawMessage
	if err := json.UnmarshalDecode(decoder, &first); err != nil {
		t.Fatal(err)
	}
	if err := json.UnmarshalDecode(decoder, &second); err != nil {
		t.Fatal(err)
	}
	if string(first["values"]) != "[]" || string(first["text"]) != `"<東京>"` || string(second["counter"]) != "9007199254740993" {
		t.Fatalf("JSON v2 values changed: %#v %#v", first, second)
	}
	if err := json.UnmarshalDecode(decoder, new(any)); err != io.EOF {
		t.Fatalf("unexpected trailing JSON record: %v", err)
	}
}

type completeRecordWriter struct {
	t      *testing.T
	writes int
}

func (w *completeRecordWriter) Write(data []byte) (int, error) {
	w.t.Helper()
	if !bytes.HasSuffix(data, []byte{'\n'}) || !jsontext.Value(bytes.TrimSuffix(data, []byte{'\n'})).IsValid() {
		w.t.Fatal("event writer sent a partial JSONL record")
	}
	w.writes++
	return len(data), nil
}

func TestJSONV2WriterCommitsOneCompleteRecord(t *testing.T) {
	out := &completeRecordWriter{t: t}
	writer := New(out, "exec")
	writer.Emit(Event{"type": "stdout", "text": strings.Repeat("x", 128<<10)})
	if writer.Err() != nil || out.writes != 1 {
		t.Fatalf("writes=%d, error=%v", out.writes, writer.Err())
	}
}

func TestJSONV2WriterPreservesDurationUnits(t *testing.T) {
	var out bytes.Buffer
	event := Event{"type": "model.finished", "duration": 28 * time.Millisecond}
	writer := New(&out, "exec")
	writer.Emit(event)
	if writer.Err() != nil {
		t.Fatal(writer.Err())
	}
	var record struct {
		Duration int64 `json:"duration"`
	}
	if err := json.Unmarshal(out.Bytes(), &record); err != nil || record.Duration != 28000000 {
		t.Fatalf("duration record=%s, error=%v", out.Bytes(), err)
	}
	if _, ok := event["duration"].(time.Duration); !ok {
		t.Fatal("encoding changed the Go observer's event")
	}
}

func TestJSONV2WriterRejectsInvalidRawValues(t *testing.T) {
	for _, raw := range []jsonv1.RawMessage{jsonv1.RawMessage(`{"name":1,"name":2}`), jsonv1.RawMessage("\"\xff\"")} {
		var out bytes.Buffer
		writer := New(&out, "exec")
		writer.Emit(Event{"type": "data", "payload": raw})
		if writer.Err() == nil {
			t.Fatalf("accepted invalid or ambiguous raw JSON: %q", raw)
		}
	}
}
