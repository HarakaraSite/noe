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

const defaultToolOutputBytes int64 = 10 * 1024

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
		if options.OutputBytes == nil || *options.OutputBytes <= 0 || *options.OutputBytes > 9007199254740991 {
			return defaultToolOutputBytes, errors.New("run_js outputBytes must be a positive safe integer")
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
	preview, shown := previewUTF8(string(result), int(limit)-len(summary))
	value := toolOutputPreview{
		OutputPreview: preview,
		OutputLimit: toolOutputLimit{
			LimitBytes: limit, SourceBytes: len(result), ShownBytes: shown,
			OmittedBytes: len(result) - shown, ReturnedBytes: len(preview) + len(summary), Complete: false,
			Notice: "Incomplete JSON text preview; do not parse it as complete data. For search results, narrow the query or scope, return matching filenames/counts, or read a relevant part. Increase outputBytes explicitly only when the required content itself is larger. The source was not rerun. Original captured output remains in execution logs when enabled.",
		},
		Execution: execution,
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, false, err
	}
	return encoded, true, nil
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
