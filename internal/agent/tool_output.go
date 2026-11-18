package agent

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
)

const (
	minToolOutputBytes     int64 = 1024
	defaultToolOutputBytes int64 = 64 * 1024
	maxToolOutputBytes     int64 = 256 * 1024
	maxRunJSTimeoutMs            = math.MaxInt64 / int64(time.Millisecond)
)

type runJSOptions struct {
	outputBytes int64
	timeout     time.Duration
	saveData    bool
}

// parseRunJSOptions reads only the initial line-comment block. The ordinary
// operation comment can stay first, preserving its use in observation events.
func parseRunJSOptions(source string) (runJSOptions, error) {
	defaults := runJSOptions{outputBytes: defaultToolOutputBytes}
	options := defaults
	seen := false
	for line := range strings.SplitSeq(source, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "//") {
			break
		}
		comment := strings.TrimSpace(strings.TrimPrefix(line, "//"))
		if !strings.HasPrefix(comment, "@noe:") {
			continue
		}
		if seen {
			return defaults, errors.New("run_js accepts only one @noe pragma")
		}
		seen = true
		var settings map[string]jsontext.Value
		data := []byte(strings.TrimSpace(strings.TrimPrefix(comment, "@noe:")))
		if err := json.Unmarshal(data, &settings); err != nil {
			return defaults, fmt.Errorf("invalid run_js @noe pragma: %w", err)
		}
		if len(settings) == 0 {
			return defaults, errors.New("run_js @noe pragma must specify outputBytes, timeoutMs or saveData")
		}
		for name, raw := range settings {
			switch name {
			case "outputBytes":
				var value int64
				if err := json.Unmarshal(raw, &value); err != nil || value < minToolOutputBytes || value > maxToolOutputBytes {
					return defaults, fmt.Errorf("run_js outputBytes must be an integer between %d and %d bytes", minToolOutputBytes, maxToolOutputBytes)
				}
				options.outputBytes = value
			case "timeoutMs":
				var value int64
				if err := json.Unmarshal(raw, &value); err != nil || value < 1 || value > maxRunJSTimeoutMs {
					return defaults, fmt.Errorf("run_js timeoutMs must be an integer between 1 and %d milliseconds", maxRunJSTimeoutMs)
				}
				options.timeout = time.Duration(value) * time.Millisecond
			case "saveData":
				var value *bool
				if err := json.Unmarshal(raw, &value); err != nil || value == nil {
					return defaults, errors.New("run_js saveData must be true or false")
				}
				options.saveData = *value
			default:
				return defaults, fmt.Errorf("unknown run_js @noe pragma setting %q", name)
			}
		}
	}
	return options, nil
}

type toolOutputExecution struct {
	ExitCode          *int   `json:"exitCode,omitempty"`
	Reason            string `json:"reason,omitempty"`
	DiagnosticSummary string `json:"diagnosticSummary,omitempty"`
}

type toolOutputLimit struct {
	LimitBytes    int64  `json:"limitBytes"`
	SourceBytes   int    `json:"sourceBytes"`
	ShownBytes    int    `json:"shownBytes"`
	OmittedBytes  int    `json:"omittedBytes"`
	ReturnedBytes int    `json:"returnedBytes"`
	Complete      bool   `json:"complete"`
	Notice        string `json:"notice"`
}

type toolOutputPreview struct {
	OutputPreview string               `json:"outputPreview"`
	OutputLimit   toolOutputLimit      `json:"outputLimit"`
	Execution     *toolOutputExecution `json:"execution,omitempty"`
	toolOutputFiles
}

type toolOutputFiles struct {
	StdoutFile *executionresult.OutputFile `json:"stdoutFile,omitempty"`
	StderrFile *executionresult.OutputFile `json:"stderrFile,omitempty"`
	DataFile   *executionresult.OutputFile `json:"dataFile,omitempty"`
}

// projectToolOutput limits only the model-facing value, after hooks. An
// overflow is a preview of the whole hook result, never a partial data value.
func projectToolOutput(result, rawResult jsonv1.RawMessage, limit int64) (jsonv1.RawMessage, bool, error) {
	if limit <= 0 {
		limit = defaultToolOutputBytes
	}
	if int64(len(result)) <= limit {
		return cloneRawJSON(result), false, nil
	}
	var raw struct {
		toolOutputFiles
		ExitCode    *int   `json:"exitCode"`
		Reason      string `json:"reason"`
		Diagnostics []struct {
			Code    string `json:"code"`
			Stage   string `json:"stage"`
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	var execution *toolOutputExecution
	rawOK := json.Unmarshal(rawResult, &raw) == nil
	summary := ""
	if rawOK {
		var messages []string
		for _, diagnostic := range raw.Diagnostics {
			messages = append(messages, diagnostic.Code+" ("+diagnostic.Stage+"): "+diagnostic.Message)
		}
		summary, _ = previewUTF8(strings.Join(messages, "\n"), int(min(limit/4, 768)))
		if raw.ExitCode != nil || raw.Reason != "" || summary != "" {
			execution = &toolOutputExecution{ExitCode: raw.ExitCode, Reason: raw.Reason, DiagnosticSummary: summary}
		}
	}
	value := toolOutputPreview{
		OutputLimit: toolOutputLimit{
			LimitBytes: limit, SourceBytes: len(result), Complete: false,
			Notice: "This preview is incomplete JSON text. The outputBytes pragma supports per-call budgets up to 262144 bytes. The source was not rerun.",
		},
		Execution: execution,
	}
	if rawOK && (raw.StdoutFile != nil || raw.StderrFile != nil || raw.DataFile != nil) {
		var selected struct {
			StdoutFile jsontext.Value `json:"stdoutFile"`
			StderrFile jsontext.Value `json:"stderrFile"`
			DataFile   jsontext.Value `json:"dataFile"`
		}
		// Carry actual file references through the shared JSON budget only
		// when the hook has chosen to retain them.
		if json.Unmarshal(result, &selected) == nil {
			value.StdoutFile = retainedOutputFile(raw.StdoutFile, selected.StdoutFile)
			value.StderrFile = retainedOutputFile(raw.StderrFile, selected.StderrFile)
			value.DataFile = retainedOutputFile(raw.DataFile, selected.DataFile)
		}
	}
	source := string(result)
	// returnedBytes includes this envelope and JSON escaping. Its own decimal
	// width is part of the encoding, so settle the count before measuring it.
	encode := func(budget int) ([]byte, error) {
		var shown int
		value.OutputPreview, shown = previewUTF8(source, budget)
		value.OutputLimit.ShownBytes = shown
		value.OutputLimit.OmittedBytes = len(result) - shown
		value.OutputLimit.ReturnedBytes = 0
		for {
			encoded, err := json.Marshal(value)
			if err != nil || len(encoded) == value.OutputLimit.ReturnedBytes {
				return encoded, err
			}
			value.OutputLimit.ReturnedBytes = len(encoded)
		}
	}
	encoded, err := encode(0)
	if err != nil {
		return nil, false, err
	}
	// Diagnostic text is escaped too; shorten it if it consumes the envelope.
	for int64(len(encoded)) > limit && execution != nil && execution.DiagnosticSummary != "" {
		execution.DiagnosticSummary, _ = previewUTF8(execution.DiagnosticSummary, len(execution.DiagnosticSummary)/2)
		encoded, err = encode(0)
		if err != nil {
			return nil, false, err
		}
	}
	// An exceptionally long path can exceed even the entire envelope
	// budget. The raw result and observation still carry the reference.
	for _, file := range []**executionresult.OutputFile{&value.StderrFile, &value.StdoutFile, &value.DataFile} {
		if int64(len(encoded)) <= limit {
			break
		}
		if *file != nil {
			*file = nil
			encoded, err = encode(0)
			if err != nil {
				return nil, false, err
			}
		}
	}
	if int64(len(encoded)) > limit {
		return nil, false, errors.New("run_js outputBytes cannot fit the output metadata")
	}
	best := encoded
	low, high := 1, int(limit)
	for low <= high {
		budget := low + (high-low)/2
		encoded, err = encode(budget)
		if err != nil {
			return nil, false, err
		}
		if int64(len(encoded)) <= limit {
			best = encoded
			low = budget + 1
		} else {
			high = budget - 1
		}
	}
	return best, true, nil
}

func retainedOutputFile(raw *executionresult.OutputFile, selected jsontext.Value) *executionresult.OutputFile {
	if raw == nil || len(selected) == 0 {
		return nil
	}
	var file *executionresult.OutputFile
	if json.Unmarshal(selected, &file) != nil || file == nil || *raw != *file {
		return nil
	}
	return raw
}

// previewUTF8 returns valid head/tail slices and counts retained source bytes.
// The marker consumes the text budget, but is not counted as source content.
func previewUTF8(source string, budget int) (string, int) {
	if budget <= 0 {
		return "", 0
	}
	if len(source) <= budget {
		return source, len(source)
	}
	const marker = "\n...[truncated]...\n"
	if budget <= len(marker) {
		prefix := utf8Prefix(source, budget)
		return prefix, len(prefix)
	}
	remaining := budget - len(marker)
	head := utf8Prefix(source, (remaining+1)/2)
	start := len(source) - remaining/2
	for start < len(source) && !utf8.RuneStart(source[start]) {
		start++
	}
	tail := source[start:]
	return head + marker + tail, len(head) + len(tail)
}

func utf8Prefix(source string, size int) string {
	size = min(size, len(source))
	for size > 0 && size < len(source) && !utf8.RuneStart(source[size]) {
		size--
	}
	return source[:size]
}

func sameToolOutput(a, b jsonv1.RawMessage) bool {
	left := jsontext.Value(a).Clone()
	right := jsontext.Value(b).Clone()
	if left.Format() != nil || right.Format() != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(left, right)
}
