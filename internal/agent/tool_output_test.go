package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestParseRunJSOptions(t *testing.T) {
	for _, tc := range []struct {
		name, code  string
		wantBytes   int64
		wantTimeout time.Duration
		invalid     bool
	}{
		{"default", "// Inspect files.\nexport default 1", 65536, 0, false},
		{"operation and pragma", "// Inspect files.\n// @noe: {\"outputBytes\": 131072}\nexport default 1", 131072, 0, false},
		{"after code ignored", "export default 1\n// @noe: {\"outputBytes\": 1,\"timeoutMs\": 30000}", 65536, 0, false},
		{"minimum output", `// @noe: {"outputBytes": 1024}`, 1024, 0, false},
		{"too little output", `// @noe: {"outputBytes": 1023}`, 65536, 0, true},
		{"maximum output", `// @noe: {"outputBytes": 262144}`, 262144, 0, false},
		{"too much output", `// @noe: {"outputBytes": 262145}`, 65536, 0, true},
		{"zero output", `// @noe: {"outputBytes": 0}`, 65536, 0, true},
		{"negative output", `// @noe: {"outputBytes": -1}`, 65536, 0, true},
		{"fractional output", `// @noe: {"outputBytes": 1.5}`, 65536, 0, true},
		{"unsafe output", `// @noe: {"outputBytes": 9007199254740992}`, 65536, 0, true},
		{"timeout", `// @noe: {"timeoutMs": 30000}`, 65536, 30 * time.Second, false},
		{"both options", `// @noe: {"outputBytes": 3072,"timeoutMs": 1}`, 3072, time.Millisecond, false},
		{"maximum timeout", `// @noe: {"timeoutMs": 9223372036854}`, 65536, 9223372036854 * time.Millisecond, false},
		{"timeout overflow", `// @noe: {"timeoutMs": 9223372036855}`, 65536, 0, true},
		{"zero timeout", `// @noe: {"timeoutMs": 0}`, 65536, 0, true},
		{"negative timeout", `// @noe: {"timeoutMs": -1}`, 65536, 0, true},
		{"fractional timeout", `// @noe: {"timeoutMs": 1.5}`, 65536, 0, true},
		{"string timeout", `// @noe: {"timeoutMs": "30000"}`, 65536, 0, true},
		{"null timeout", `// @noe: {"outputBytes": 3072,"timeoutMs": null}`, 65536, 0, true},
		{"null output", `// @noe: {"outputBytes": null,"timeoutMs": 30000}`, 65536, 0, true},
		{"missing", `// @noe: {}`, 65536, 0, true},
		{"null", `// @noe: null`, 65536, 0, true},
		{"unknown", `// @noe: {"outputBytes": 1024,"other": 1}`, 65536, 0, true},
		{"malformed", `// @noe: {`, 65536, 0, true},
		{"trailing", `// @noe: {"outputBytes": 1024} {}`, 65536, 0, true},
		{"duplicate pragma", "// @noe: {\"outputBytes\": 1024}\n// @noe: {\"timeoutMs\": 1}", 65536, 0, true},
		{"duplicate output", `// @noe: {"outputBytes": 1024,"outputBytes": 2048}`, 65536, 0, true},
		{"duplicate timeout", `// @noe: {"timeoutMs": 1,"timeoutMs": 2}`, 65536, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseRunJSOptions(tc.code)
			if got.outputBytes != tc.wantBytes || got.timeout != tc.wantTimeout || (err != nil) != tc.invalid {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestParseRunJSSaveData(t *testing.T) {
	options, err := parseRunJSOptions(`// @noe: {"outputBytes": 3072, "timeoutMs": 15, "saveData": true}`)
	if err != nil || !options.saveData || options.outputBytes != 3072 || options.timeout != 15*time.Millisecond {
		t.Fatalf("combined options = %+v, %v", options, err)
	}
	for _, value := range []string{"false", "null", "1", `"true"`} {
		options, err := parseRunJSOptions(`// @noe: {"saveData": ` + value + `}`)
		if options.saveData || (err == nil) != (value == "false") {
			t.Fatalf("saveData %s: options = %+v, err = %v", value, options, err)
		}
	}
}

func TestProjectToolOutputDefaultBudget(t *testing.T) {
	raw := json.RawMessage(`{"exitCode":0,"reason":"completed"}`)
	for _, size := range []int{12000, 51200, 65536, 65537} {
		source := json.RawMessage(`"` + strings.Repeat("x", size-2) + `"`)
		for _, fallback := range []int64{0, -1} {
			got, truncated, err := projectToolOutput(source, raw, fallback)
			if err != nil || truncated != (size > 65536) {
				t.Fatalf("size=%d fallback=%d truncated=%v err=%v", size, fallback, truncated, err)
			}
			if size <= 65536 {
				if string(got) != string(source) {
					t.Fatal("default budget changed a fitting result")
				}
				continue
			}
			var preview toolOutputPreview
			if err := json.Unmarshal(got, &preview); err != nil {
				t.Fatal(err)
			}
			if preview.OutputLimit.LimitBytes != 65536 || preview.OutputLimit.ReturnedBytes > 65536 || preview.OutputLimit.Complete {
				t.Fatalf("incorrect default budget: %+v", preview.OutputLimit)
			}
		}
	}
}

func TestProjectToolOutputRetainsIndependentFileReferences(t *testing.T) {
	raw := json.RawMessage(`{"stdoutFile":{"path":"/out.log","bytes":10},"dataFile":{"path":"/data.json","bytes":2048}}`)
	for _, test := range []struct {
		fields string
		stdout bool
		data   bool
	}{
		{`"stdoutFile":{"path":"/out.log","bytes":10},"dataFile":"hook label"`, true, false},
		{`"stdoutFile":7,"dataFile":{"path":"/data.json","bytes":2048}`, false, true},
		{`"stdoutFile":{"path":"/different.log","bytes":10}`, false, false},
	} {
		result := json.RawMessage(`{` + test.fields + `,"text":"` + strings.Repeat("x", 2048) + `"}`)
		value, cut, err := projectToolOutput(result, raw, 1024)
		if err != nil || !cut {
			t.Fatalf("projection: cut=%v err=%v", cut, err)
		}
		var preview toolOutputPreview
		if err := json.Unmarshal(value, &preview); err != nil ||
			(preview.StdoutFile != nil) != test.stdout || (preview.DataFile != nil) != test.data {
			t.Fatalf("hook fields %s produced stdoutFile=%+v dataFile=%+v err=%v", test.fields, preview.StdoutFile, preview.DataFile, err)
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
	if limits.Complete || limits.LimitBytes != 3072 || limits.SourceBytes != len(source) || limits.ShownBytes+limits.OmittedBytes != len(source) || limits.OmittedBytes <= 0 || limits.ReturnedBytes != len(got) || limits.ReturnedBytes > 3072 {
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
		if n := len(got); n > 3072 || n != preview.OutputLimit.ReturnedBytes {
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

func TestProjectToolOutputIncludesEnvelopeAndEscaping(t *testing.T) {
	for _, limit := range []int64{1024, 65536, 131072, 262144} {
		raw, _ := json.Marshal(map[string]any{
			"exitCode": 7, "reason": "failed",
			"diagnostics": []any{map[string]any{"code": "execution_error", "stage": "execute", "message": strings.Repeat("\x00\"\\<日本語", 2000)}},
		})
		for _, data := range []string{strings.Repeat("\x00\"\\<>&\n", 70000), strings.Repeat("日本語😀", 70000)} {
			source, _ := json.Marshal(map[string]any{"data": data})
			got, cut, err := projectToolOutput(source, raw, limit)
			if err != nil || !cut || int64(len(got)) > limit || !json.Valid(got) {
				t.Fatalf("limit=%d size=%d cut=%v err=%v", limit, len(got), cut, err)
			}
			var preview toolOutputPreview
			if err := json.Unmarshal(got, &preview); err != nil {
				t.Fatal(err)
			}
			if preview.OutputLimit.ReturnedBytes != len(got) || preview.OutputLimit.ShownBytes+preview.OutputLimit.OmittedBytes != len(source) || !utf8.ValidString(preview.OutputPreview) || preview.Execution.ExitCode == nil || *preview.Execution.ExitCode != 7 {
				t.Fatalf("incorrect envelope: %+v", preview.OutputLimit)
			}
		}
	}
}

func TestProjectToolOutputKeepsPreviewSelectionAtDecimalBoundaries(t *testing.T) {
	source := json.RawMessage("{\n\t\"data\": \"" + strings.Repeat("日本語😀<>&\u2028\u2029\\u0000\\\"\\\\", 5000) + "\",\r\n \"ending\":\"TAIL\"\n}")
	raw := json.RawMessage(`{"exitCode":7,"reason":"failed"}`)
	for _, test := range []struct {
		limit         int64
		shown         int
		returnedBytes int
	}{
		{349, 8, 348},
		{999, 546, 999},
		{1000, 543, 997},
		{1001, 546, 1001},
		{9999, 8327, 9999},
		{10000, 8326, 9999},
		{10001, 8327, 10001},
	} {
		got, cut, err := projectToolOutput(source, raw, test.limit)
		if err != nil || !cut {
			t.Fatalf("limit=%d cut=%v err=%v", test.limit, cut, err)
		}
		var preview toolOutputPreview
		if err := json.Unmarshal(got, &preview); err != nil {
			t.Fatal(err)
		}
		limit := preview.OutputLimit
		if limit.LimitBytes != test.limit || limit.ShownBytes != test.shown || limit.OmittedBytes != len(source)-test.shown ||
			limit.ReturnedBytes != test.returnedBytes || len(got) != limit.ReturnedBytes {
			t.Fatalf("limit=%d size=%d outputLimit=%+v", test.limit, len(got), limit)
		}
		if test.limit == 349 && preview.OutputPreview != string(source[:test.shown]) {
			t.Fatalf("small budget preview = %q", preview.OutputPreview)
		}
	}
}

func TestProjectToolOutputTrimsOversizedDiagnosticsAndFileMetadata(t *testing.T) {
	const diagnosticStart = `"exitCode":7,"reason":"failed","diagnostics":[{"code":"execution_error","stage":"execute","message":"`
	const files = `],"stdoutFile":{"path":"/out.log","bytes":10},"dataFile":{"path":"/data.json","bytes":2048},`
	raw := json.RawMessage("{" + diagnosticStart + strings.Repeat(`\u0000`, 1024) + `"}` + files +
		`"stderrFile":{"path":"` + strings.Repeat("s", 1024) + `","bytes":20}}`)
	for _, test := range []struct {
		limit          int64
		keepStderr     bool
		keepDiagnostic bool
	}{
		{1024, false, false},
		{1536, true, true},
	} {
		got, cut, err := projectToolOutput(raw, raw, test.limit)
		if err != nil || !cut {
			t.Fatalf("limit=%d cut=%v err=%v", test.limit, cut, err)
		}
		var preview toolOutputPreview
		if err := json.Unmarshal(got, &preview); err != nil {
			t.Fatal(err)
		}
		if preview.StdoutFile == nil || preview.StdoutFile.Path != "/out.log" ||
			preview.DataFile == nil || preview.DataFile.Path != "/data.json" ||
			(preview.StderrFile != nil) != test.keepStderr {
			t.Fatalf("limit=%d stdout=%+v stderr=%+v data=%+v", test.limit, preview.StdoutFile, preview.StderrFile, preview.DataFile)
		}
		execution := preview.Execution
		if execution == nil || execution.ExitCode == nil || *execution.ExitCode != 7 || execution.Reason != "failed" ||
			(execution.DiagnosticSummary != "") != test.keepDiagnostic || int64(len(execution.DiagnosticSummary)) >= test.limit/4 {
			t.Fatalf("limit=%d execution=%+v", test.limit, execution)
		}
		if int64(len(got)) > test.limit || len(got) != preview.OutputLimit.ReturnedBytes {
			t.Fatalf("limit=%d size=%d outputLimit=%+v", test.limit, len(got), preview.OutputLimit)
		}
	}
}

func TestSameToolOutputPreservesFormattingEquivalence(t *testing.T) {
	for _, test := range []struct {
		left, right string
		equal       bool
	}{
		{`{"data":"日"}`, `{"data":"日"}`, true},
		{`{"data":"日"}`, `{ "data": "\u65e5" }`, true},
		{`{"a":1,"b":2}`, `{"b":2,"a":1}`, false},
		{`{"data":1}`, `{"data":1.0}`, false},
		{`{`, `{`, true},
		{`{`, `[`, false},
	} {
		left, right := json.RawMessage(test.left), json.RawMessage(test.right)
		if got := sameToolOutput(left, right); got != test.equal || string(left) != test.left || string(right) != test.right {
			t.Fatalf("sameToolOutput(%q, %q) = %v, want %v, inputs now %q, %q", test.left, test.right, got, test.equal, left, right)
		}
	}
}
