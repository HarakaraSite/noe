package loganalysis

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnalyzeSeparatesReturnedDataAndDiagnosticsFromCapturedOutput(t *testing.T) {
	input := `{"executionId":"exec","seq":1,"type":"run_js.finished","runId":"run","toolCallId":"call","success":false,"exitCode":1,"reason":"failed","data":null,"diagnostics":[{"code":"cleanup_error","stage":"cleanup","message":"join failed","exception":{"code":7}}]}` + "\n"
	for _, include := range []bool{false, true} {
		snapshot, err := Analyze(t.Context(), strings.NewReader(input), Query{RunID: "run", IncludeOutput: include})
		if err != nil {
			t.Fatal(err)
		}
		result := snapshot.LastToolResult
		if result == nil || result.Text != "" || len(result.Diagnostics) != 1 || result.Diagnostics[0].Stage != "cleanup" || result.Diagnostics[0].Message != "join failed" {
			t.Fatalf("includeOutput=%v result=%+v", include, result)
		}
		if include && string(result.Data) != "null" || !include && result.Data != nil {
			t.Fatalf("includeOutput=%v returned data=%q", include, result.Data)
		}
		if len(snapshot.Events) != 1 {
			t.Fatalf("projected events=%d", len(snapshot.Events))
		}
		_, present := snapshot.Events[0]["data"]
		if present != include || snapshot.Events[0]["diagnostics"] == nil {
			t.Fatalf("includeOutput=%v event=%+v", include, snapshot.Events[0])
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), `"data":null`) != include {
			t.Fatalf("null/absence changed: %s", encoded)
		}
	}
}
