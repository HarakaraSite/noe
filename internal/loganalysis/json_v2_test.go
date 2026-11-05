package loganalysis

import (
	"context"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"strings"
	"testing"
)

func TestJSONV2LogNumbersAndStrictInput(t *testing.T) {
	line := `{"executionId":"exec","seq":9007199254740993,"type":"agent.message","runId":"run","data":{"counter":18446744073709551615}}`
	records, err := readRecords(context.Background(), strings.NewReader(line+"\n"))
	if err != nil || len(records) != 1 {
		t.Fatalf("records=%v, error=%v", records, err)
	}
	if records[0].seq != 9007199254740993 || records[0].event["data"].(map[string]any)["counter"] != jsonv1.Number("18446744073709551615") {
		t.Fatalf("log numbers lost precision: %#v", records[0])
	}
	for _, invalid := range []string{
		strings.Replace(line, `"runId":"run"`, `"runId":"run","runId":"other"`, 1),
		strings.Replace(line, `"runId":"run"`, "\"runId\":\"\xff\"", 1),
		line + ` {}`,
	} {
		if _, err := readRecords(context.Background(), strings.NewReader(invalid+"\n")); err == nil {
			t.Fatalf("accepted ambiguous or malformed JSON log: %q", invalid)
		}
	}
}

func TestJSONV2ToolDataKeepsExplicitEmptyValues(t *testing.T) {
	for _, value := range []string{"null", "{}", "[]", "0", "false"} {
		encoded, err := json.Marshal(ToolResult{Data: jsonv1.RawMessage(value)})
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]jsonv1.RawMessage
		if err := json.Unmarshal(encoded, &fields); err != nil || string(fields["data"]) != value {
			t.Fatalf("explicit data %s changed: %s, error=%v", value, encoded, err)
		}
	}
	encoded, err := json.Marshal(ToolResult{})
	if err != nil || strings.Contains(string(encoded), `"data"`) {
		t.Fatalf("absent data should remain absent: %s, error=%v", encoded, err)
	}
}
