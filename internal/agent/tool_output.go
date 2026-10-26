package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	minToolOutputBytes     int64 = 1024
	defaultToolOutputBytes int64 = 64 * 1024
	maxToolOutputBytes     int64 = 256 * 1024
)

// runJSOutputBytes reads only the initial line-comment block. The ordinary
// operation comment can stay first, preserving its use in observation events.
func runJSOutputBytes(source string) (int64, error) {
	limit := defaultToolOutputBytes
	seen := false
	for _, line := range strings.Split(source, "\n") {
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
			return defaultToolOutputBytes, errors.New("run_js accepts only one @noe output pragma")
		}
		seen = true
		var options struct {
			OutputBytes *int64 `json:"outputBytes"`
		}
		decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(strings.TrimPrefix(comment, "@noe:"))))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&options); err != nil {
			return defaultToolOutputBytes, fmt.Errorf("invalid run_js @noe pragma: %w", err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return defaultToolOutputBytes, errors.New("run_js @noe pragma must contain one JSON object")
		}
		if options.OutputBytes == nil || *options.OutputBytes < minToolOutputBytes || *options.OutputBytes > maxToolOutputBytes {
			return defaultToolOutputBytes, fmt.Errorf("run_js outputBytes must be an integer between %d and %d bytes", minToolOutputBytes, maxToolOutputBytes)
		}
		limit = *options.OutputBytes
	}
	return limit, nil
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
}

// projectToolOutput limits only the model-facing value, after hooks. An
// overflow is a preview of the whole hook result, never a partial data value.
func projectToolOutput(result, rawResult json.RawMessage, limit int64) (json.RawMessage, bool, error) {
	if limit <= 0 {
		limit = defaultToolOutputBytes
	}
	if int64(len(result)) <= limit {
		return cloneRawJSON(result), false, nil
	}
	var raw struct {
		ExitCode    *int   `json:"exitCode"`
		Reason      string `json:"reason"`
		Diagnostics []struct {
			Code    string `json:"code"`
			Stage   string `json:"stage"`
			Message string `json:"message"`
		} `json:"diagnostics"`
	}
	var execution *toolOutputExecution
	summary := ""
	if json.Unmarshal(rawResult, &raw) == nil {
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
			Notice: "This preview is incomplete JSON text. The outputBytes pragma supports per-call budgets up to 262144 bytes. The source was not rerun. Logs retain captured output, not bytes omitted during capture.",
		},
		Execution: execution,
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

func sameToolOutput(a, b json.RawMessage) bool {
	var left, right bytes.Buffer
	if json.Compact(&left, a) != nil || json.Compact(&right, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(left.Bytes(), right.Bytes())
}
