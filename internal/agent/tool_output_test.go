package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRunJSOutputBytes(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		want       int64
		invalid    bool
	}{
		{"default", "// Inspect files.\nexport default 1", 10240, false},
		{"operation and pragma", "// Inspect files.\n// @noe: {\"outputBytes\": 12288}\nexport default 1", 12288, false},
		{"after code ignored", "export default 1\n// @noe: {\"outputBytes\": 1}", 10240, false},
		{"small", "// @noe: {\"outputBytes\": 1}", 1, false},
		{"zero", "// @noe: {\"outputBytes\": 0}", 10240, true},
		{"negative", "// @noe: {\"outputBytes\": -1}", 10240, true},
		{"fraction", "// @noe: {\"outputBytes\": 1.5}", 10240, true},
		{"unsafe", "// @noe: {\"outputBytes\": 9007199254740992}", 10240, true},
		{"missing", "// @noe: {}", 10240, true},
		{"null", "// @noe: null", 10240, true},
		{"unknown", "// @noe: {\"outputBytes\": 99,\"other\": 1}", 10240, true},
		{"malformed", "// @noe: {", 10240, true},
		{"trailing", "// @noe: {\"outputBytes\": 99} {}", 10240, true},
		{"duplicate", "// @noe: {\"outputBytes\": 99}\n// @noe: {\"outputBytes\": 90}", 10240, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := runJSOutputBytes(tc.code)
			if got != tc.want || (err != nil) != tc.invalid {
				t.Fatalf("got %d,%v", got, err)
			}
		})
	}
}

func TestProjectToolOutputDefaultBudget(t *testing.T) {
	raw := json.RawMessage(`{"exitCode":0,"reason":"completed"}`)
	for _, size := range []int{6000, 10240, 10241} {
		source := json.RawMessage(`"` + strings.Repeat("x", size-2) + `"`)
		for _, fallback := range []int64{0, -1} {
			got, truncated, err := projectToolOutput(source, raw, fallback)
			if err != nil || truncated != (size > 10240) {
				t.Fatalf("size=%d fallback=%d truncated=%v err=%v", size, fallback, truncated, err)
			}
			if size <= 10240 {
				if string(got) != string(source) {
					t.Fatal("default budget changed a fitting result")
				}
				continue
			}
			var preview toolOutputPreview
			if err := json.Unmarshal(got, &preview); err != nil {
				t.Fatal(err)
			}
			if preview.OutputLimit.LimitBytes != 10240 || preview.OutputLimit.ReturnedBytes > 10240 || preview.OutputLimit.Complete {
				t.Fatalf("incorrect default budget: %+v", preview.OutputLimit)
			}
		}
	}
}

func TestProjectToolOutputBoundariesAndUTF8(t *testing.T) {
	for _, source := range []string{`null`, `{"data":null}`, `{"stdout":"","exitCode":0}`, `"` + strings.Repeat("日本語", 600) + `"`} {
		original := json.RawMessage(source)
		for _, limit := range []int64{int64(len(source)), int64(len(source) + 1)} {
			got, cut, err := projectToolOutput(original, original, limit)
			if err != nil || cut || string(got) != source {
				t.Fatalf("changed under-budget value: %s %v %v", got, cut, err)
			}
			if len(got) > 0 {
				got[0] = 'x'
				if string(original) != source {
					t.Fatal("projection aliases original")
				}
			}
		}
	}
	source := json.RawMessage(`{"stdout":"HEAD` + strings.Repeat("日本語", 2000) + `TAIL","stderr":"err","data":[1,2],"exitCode":0,"reason":"completed"}`)
	got, cut, err := projectToolOutput(source, source, 3072)
	if err != nil || !cut {
		t.Fatalf("projection: cut=%v err=%v", cut, err)
	}
	var preview toolOutputPreview
	if err := json.Unmarshal(got, &preview); err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(preview.OutputPreview) || !strings.HasPrefix(preview.OutputPreview, `{"stdout":"HEAD`) || !strings.HasSuffix(preview.OutputPreview, `"reason":"completed"}`) {
		t.Fatalf("lost UTF8 head/tail: %q", preview.OutputPreview)
	}
	limits := preview.OutputLimit
	if limits.Complete || limits.LimitBytes != 3072 || limits.SourceBytes != len(source) || limits.ShownBytes+limits.OmittedBytes != len(source) || limits.OmittedBytes <= 0 || limits.ReturnedBytes != len(preview.OutputPreview) || limits.ReturnedBytes > 3072 {
		t.Fatalf("incorrect byte accounting: %+v", limits)
	}
	if preview.Execution == nil || preview.Execution.ExitCode == nil || *preview.Execution.ExitCode != 0 || preview.Execution.Reason != "completed" {
		t.Fatalf("lost actual execution: %+v", preview.Execution)
	}
	if strings.Contains(string(got), `"data":`) {
		t.Fatal("overflow masquerades as complete data")
	}
	if _, cut, err := projectToolOutput(source, source, int64(len(source)+1)); err != nil || cut {
		t.Fatalf("explicit larger allowance failed: %v %v", cut, err)
	}
}

func TestProjectToolOutputDiagnosticAndHookShareBudget(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"exitCode": 7, "reason": "failed", "diagnostics": []any{map[string]any{"code": "execution_error", "stage": "execute", "message": strings.Repeat("失敗", 2000), "stack": strings.Repeat("stack", 3000), "exception": map[string]any{"stdout": strings.Repeat("out", 3000), "stderr": strings.Repeat("err", 3000)}}}})
	for _, hook := range []any{map[string]any{"exitCode": 0, "data": strings.Repeat("x", 8000)}, []string{strings.Repeat("y", 9000)}, strings.Repeat("z", 9000), json.RawMessage(raw)} {
		value, _ := json.Marshal(hook)
		got, cut, err := projectToolOutput(value, raw, 3072)
		if err != nil || !cut {
			t.Fatalf("projection: %v %v", cut, err)
		}
		var preview toolOutputPreview
		if err := json.Unmarshal(got, &preview); err != nil {
			t.Fatal(err)
		}
		ex := preview.Execution
		if ex == nil || ex.ExitCode == nil || *ex.ExitCode != 7 || ex.Reason != "failed" || !strings.Contains(ex.DiagnosticSummary, "execution_error (execute)") || len(ex.DiagnosticSummary) > 768 {
			t.Fatalf("raw diagnostic/status lost: %+v", ex)
		}
		if n := len(preview.OutputPreview) + len(ex.DiagnosticSummary); n > 3072 || n != preview.OutputLimit.ReturnedBytes {
			t.Fatalf("budget bypass: %d %+v", n, preview.OutputLimit)
		}
	}
	for budget := 1; budget < 40; budget++ {
		got, shown := previewUTF8(strings.Repeat("日本語", 30), budget)
		if len(got) > budget || shown > len(got) || !utf8.ValidString(got) {
			t.Fatalf("small budget %d invalid: %q %d", budget, got, shown)
		}
	}
}
