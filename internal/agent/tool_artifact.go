package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	toolPreviewLimit     = 64 * 1024
	toolDefaultPageLimit = 24 * 1024
	toolMinimumPageLimit = 4
)

var errToolResultStoreClosed = errors.New("agent: tool result store is closed")

type toolArtifactRequest struct {
	ResultRef string `json:"resultRef"`
	Offset    int64  `json:"offset"`
	Limit     *int64 `json:"limit,omitempty"`
}

type toolResultPage struct {
	Text       string `json:"text"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"nextOffset"`
	TotalBytes int64  `json:"totalBytes"`
	Truncated  bool   `json:"truncated"`
}

// toolResultStore owns the full JSON results for one task. Its zero value is
// ready to use; storage is created only when a result needs an artifact.
type toolResultStore struct {
	mu     sync.Mutex
	dir    string
	refs   map[string]string
	closed bool
}

// retain stores the complete result when a preview omits information, then
// updates result with the bounded inline view. Full stream readers transfer
// ownership to this method and are always closed.
func (s *toolResultStore) retain(ctx context.Context, result *Result) (retErr error) {
	if result == nil {
		return errors.New("agent: tool result is required")
	}
	stdoutFull, stderrFull := result.StdoutFull, result.StderrFull
	result.StdoutFull, result.StderrFull = nil, nil
	inputsClosed := false
	closeInputs := func() error {
		if inputsClosed {
			return nil
		}
		inputsClosed = true
		var closeErr error
		if stdoutFull != nil {
			closeErr = errors.Join(closeErr, stdoutFull.Close())
		}
		if stderrFull != nil {
			closeErr = errors.Join(closeErr, stderrFull.Close())
		}
		return closeErr
	}
	defer func() { retErr = errors.Join(retErr, closeInputs()) }()
	if ctx == nil {
		return errors.New("agent: tool result context is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if result.ResultRef != "" || result.DataOmittedBytes != 0 {
		return errors.New("agent: tool result was already retained")
	}

	fullDiagnostics, err := json.Marshal(result.Diagnostics)
	if err != nil {
		return fmt.Errorf("agent: encode complete tool diagnostics: %w", err)
	}
	diagnosticsBytes := 0
	if len(result.Diagnostics) != 0 {
		diagnosticsBytes = len(fullDiagnostics)
	}
	needsArtifact := result.StdoutOmittedBytes > 0 || result.StderrOmittedBytes > 0 || len(result.Data) > toolPreviewLimit || diagnosticsBytes > toolPreviewLimit
	if !needsArtifact {
		if s.closed {
			return errToolResultStoreClosed
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}
	previewData, dataOmitted := result.Data, result.DataOmittedBytes
	if len(result.Data) > toolPreviewLimit {
		previewData, dataOmitted = nil, int64(len(result.Data))
	}
	diagnosticPreview, diagnosticsOmitted := result.Diagnostics, result.DiagnosticsOmittedBytes
	if diagnosticsBytes > toolPreviewLimit {
		diagnosticPreview, diagnosticsOmitted = previewDiagnostics(result.Diagnostics, diagnosticsBytes)
	}
	defer func() {
		result.Data, result.DataOmittedBytes = previewData, dataOmitted
		result.Diagnostics, result.DiagnosticsOmittedBytes = diagnosticPreview, diagnosticsOmitted
	}()
	if s.closed {
		return errToolResultStoreClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if result.StdoutOmittedBytes > 0 && stdoutFull == nil {
		return errors.New("agent: complete stdout is unavailable for artifact storage")
	}
	if result.StderrOmittedBytes > 0 && stderrFull == nil {
		return errors.New("agent: complete stderr is unavailable for artifact storage")
	}
	if len(result.Data) != 0 && !json.Valid(result.Data) {
		return errors.New("agent: tool result data is not valid JSON")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ensureDirLocked(); err != nil {
		return fmt.Errorf("agent: create tool result store: %w", err)
	}
	ref, err := s.newRefLocked()
	if err != nil {
		return errors.Join(fmt.Errorf("agent: allocate tool result reference: %w", err), s.cleanupFailedArtifactLocked(""))
	}
	file, err := os.CreateTemp(s.dir, "result-*.json")
	if err != nil {
		return errors.Join(fmt.Errorf("agent: create tool result artifact: %w", err), s.cleanupFailedArtifactLocked(""))
	}
	path := file.Name()
	writeErr := writeToolResultJSON(ctx, file, result, stdoutFull, stderrFull, fullDiagnostics)
	fileCloseErr := file.Close()
	inputCloseErr := closeInputs()
	if err := errors.Join(writeErr, fileCloseErr, inputCloseErr, ctx.Err()); err != nil {
		return fmt.Errorf("agent: write tool result artifact: %w", errors.Join(err, s.cleanupFailedArtifactLocked(path)))
	}
	if s.refs == nil {
		s.refs = make(map[string]string)
	}
	s.refs[ref] = path
	result.ResultRef = ref
	return nil
}

func (s *toolResultStore) ensureDirLocked() error {
	if s.dir != "" {
		return nil
	}
	dir, err := os.MkdirTemp("", "noe-tool-result-")
	if err != nil {
		return err
	}
	s.dir = dir
	return nil
}

func (s *toolResultStore) newRefLocked() (string, error) {
	for attempts := 0; attempts < 3; attempts++ {
		ref, err := randomID()
		if err != nil {
			return "", err
		}
		if _, exists := s.refs[ref]; !exists {
			return ref, nil
		}
	}
	return "", errors.New("result references collided")
}

// read returns one UTF-8-aligned page of the serialized complete result.
func (s *toolResultStore) read(ctx context.Context, request toolArtifactRequest) (page toolResultPage, retErr error) {
	if ctx == nil {
		return toolResultPage{}, newOperationError(http.StatusBadRequest, "request", "tool result context is required")
	}
	if err := ctx.Err(); err != nil {
		return toolResultPage{}, err
	}
	if strings.TrimSpace(request.ResultRef) == "" {
		return toolResultPage{}, newOperationError(http.StatusBadRequest, "request", "resultRef is required")
	}
	limit := int64(toolDefaultPageLimit)
	if request.Limit != nil {
		limit = *request.Limit
	}
	if limit < toolMinimumPageLimit || limit > toolPreviewLimit {
		return toolResultPage{}, newOperationError(http.StatusBadRequest, "range", "limit must be between 4 and 65536 bytes")
	}
	if request.Offset < 0 {
		return toolResultPage{}, newOperationError(http.StatusBadRequest, "range", "offset must be nonnegative")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return toolResultPage{}, err
	}
	if s.closed {
		return toolResultPage{}, newOperationError(http.StatusGone, "result_ref", "tool result reference has expired")
	}
	path, ok := s.refs[request.ResultRef]
	if !ok {
		return toolResultPage{}, newOperationError(http.StatusNotFound, "result_ref", "unknown tool result reference")
	}
	file, err := os.Open(path)
	if err != nil {
		return toolResultPage{}, fmt.Errorf("agent: open tool result artifact: %w", err)
	}
	defer func() { retErr = errors.Join(retErr, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return toolResultPage{}, fmt.Errorf("agent: inspect tool result artifact: %w", err)
	}
	total := info.Size()
	if request.Offset > total {
		return toolResultPage{}, newOperationError(http.StatusBadRequest, "range", "offset exceeds tool result size")
	}
	if request.Offset < total {
		var first [1]byte
		if _, err := file.ReadAt(first[:], request.Offset); err != nil {
			return toolResultPage{}, fmt.Errorf("agent: inspect tool result range: %w", err)
		}
		if first[0]&0xc0 == 0x80 {
			return toolResultPage{}, newOperationError(http.StatusBadRequest, "range", "offset must be a UTF-8 character boundary")
		}
	}
	if err := ctx.Err(); err != nil {
		return toolResultPage{}, err
	}
	remaining := total - request.Offset
	requested := limit
	if requested > remaining {
		requested = remaining
	}
	readSize := requested
	if request.Offset+requested < total {
		readSize += min(int64(3), remaining-requested)
	}
	// Three extra bytes let us step back from a rune split at the page end.
	buffer := make([]byte, int(readSize))
	if len(buffer) != 0 {
		n, readErr := file.ReadAt(buffer, request.Offset)
		if readErr != nil && !(readErr == io.EOF && int64(n) == readSize) {
			return toolResultPage{}, fmt.Errorf("agent: read tool result artifact: %w", readErr)
		}
		buffer = buffer[:n]
	}
	if err := ctx.Err(); err != nil {
		return toolResultPage{}, err
	}
	pageBytes := int(requested)
	if int64(pageBytes) < remaining {
		for pageBytes > 0 && pageBytes < len(buffer) && !utf8.RuneStart(buffer[pageBytes]) {
			pageBytes--
		}
	}
	if pageBytes > len(buffer) {
		pageBytes = len(buffer)
	}
	text := buffer[:pageBytes]
	if !utf8.Valid(text) {
		return toolResultPage{}, errors.New("agent: tool result artifact contains invalid UTF-8")
	}
	next := request.Offset + int64(len(text))
	return toolResultPage{
		Text: string(text), Offset: request.Offset, NextOffset: next,
		TotalBytes: total, Truncated: next < total,
	}, nil
}

// Close invalidates all references and removes the task-owned directory.
func (s *toolResultStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.refs = nil
	return s.removeDirLocked()
}

func (s *toolResultStore) removeDirLocked() error {
	if s.dir == "" {
		return nil
	}
	if err := os.RemoveAll(s.dir); err != nil {
		return err
	}
	s.dir = ""
	return nil
}

func (s *toolResultStore) cleanupFailedArtifactLocked(path string) error {
	var err error
	if path != "" {
		err = os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
	}
	if len(s.refs) == 0 {
		err = errors.Join(err, s.removeDirLocked())
	}
	return err
}

func writeToolResultJSON(ctx context.Context, dst io.Writer, result *Result, stdout, stderr io.Reader, diagnostics []byte) error {
	writer := bufio.NewWriterSize(contextWriter{ctx: ctx, writer: dst}, 32*1024)
	if _, err := io.WriteString(writer, `{"stdout":`); err != nil {
		return err
	}
	if err := writeJSONString(ctx, writer, result.Stdout, stdout); err != nil {
		return err
	}
	if _, err := io.WriteString(writer, `,"stderr":`); err != nil {
		return err
	}
	if err := writeJSONString(ctx, writer, result.Stderr, stderr); err != nil {
		return err
	}
	if _, err := io.WriteString(writer, `,"exitCode":`+strconv.Itoa(result.ExitCode)+`,"reason":`); err != nil {
		return err
	}
	if err := writeJSONString(ctx, writer, result.Reason, nil); err != nil {
		return err
	}
	if len(result.Data) != 0 {
		if _, err := io.WriteString(writer, `,"data":`); err != nil {
			return err
		}
		if _, err := writer.Write(result.Data); err != nil {
			return err
		}
	}
	if len(result.Diagnostics) != 0 {
		if _, err := io.WriteString(writer, `,"diagnostics":`); err != nil {
			return err
		}
		if _, err := writer.Write(diagnostics); err != nil {
			return err
		}
	}
	if result.Phase != "" {
		if _, err := io.WriteString(writer, `,"phase":`); err != nil {
			return err
		}
		if err := writeJSONString(ctx, writer, result.Phase, nil); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(writer, `}`); err != nil {
		return err
	}
	return writer.Flush()
}

type contextWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w contextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(p)
}

func writeJSONString(ctx context.Context, dst io.Writer, fallback string, source io.Reader) error {
	if source == nil {
		source = strings.NewReader(fallback)
	}
	if _, err := io.WriteString(dst, `"`); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(source, 32*1024)
	var readBytes, nextContextCheck int64
	for {
		r, size, err := reader.ReadRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		readBytes += int64(size)
		if readBytes >= nextContextCheck {
			if err := ctx.Err(); err != nil {
				return err
			}
			nextContextCheck = readBytes + 32*1024
		}
		if err := writeJSONRune(dst, r); err != nil {
			return err
		}
	}
	_, err := io.WriteString(dst, `"`)
	return err
}

func writeJSONRune(dst io.Writer, r rune) error {
	switch r {
	case '"', '\\':
		_, err := io.WriteString(dst, `\`+string(r))
		return err
	case '\b':
		_, err := io.WriteString(dst, `\b`)
		return err
	case '\f':
		_, err := io.WriteString(dst, `\f`)
		return err
	case '\n':
		_, err := io.WriteString(dst, `\n`)
		return err
	case '\r':
		_, err := io.WriteString(dst, `\r`)
		return err
	case '\t':
		_, err := io.WriteString(dst, `\t`)
		return err
	case '<':
		_, err := io.WriteString(dst, `\u003c`)
		return err
	case '>':
		_, err := io.WriteString(dst, `\u003e`)
		return err
	case '&':
		_, err := io.WriteString(dst, `\u0026`)
		return err
	case '\u2028':
		_, err := io.WriteString(dst, `\u2028`)
		return err
	case '\u2029':
		_, err := io.WriteString(dst, `\u2029`)
		return err
	}
	if r < 0x20 {
		_, err := io.WriteString(dst, fmt.Sprintf(`\u%04x`, r))
		return err
	}
	_, err := io.WriteString(dst, string(r))
	return err
}

func previewDiagnostics(original []ExecutionDiagnostic, originalBytes int) ([]ExecutionDiagnostic, int64) {
	preview := cloneExecutionDiagnostics(original)
	for index := range preview {
		frames := preview[index].Stack
		if len(frames) > 64 {
			clipped := make([]ExecutionFrame, 0, 64)
			clipped = append(clipped, frames[:48]...)
			clipped = append(clipped, frames[len(frames)-16:]...)
			preview[index].Stack = clipped
		}
	}
	// Each pass is bounded and progressively reduces diagnostic text.
	for _, textLimit := range []int{4096, 1024, 256, 64, 16, 0} {
		for index := range preview {
			clipDiagnosticRecordStrings(&preview[index], textLimit)
			preview[index].Exception = clipDiagnosticException(preview[index].Exception, textLimit)
		}
		encoded, err := json.Marshal(preview)
		if err == nil && len(encoded) <= toolPreviewLimit {
			return preview, max(int64(0), int64(originalBytes-len(encoded)))
		}
	}
	for index := range preview {
		preview[index].Exception = nil
	}
	if encoded, err := json.Marshal(preview); err == nil && len(encoded) <= toolPreviewLimit {
		return preview, max(int64(0), int64(originalBytes-len(encoded)))
	}
	if len(preview) > 1 {
		preview = preview[:1]
	}
	if len(preview) != 0 {
		preview[0].Exception = nil
		clipDiagnosticRecordStrings(&preview[0], 0)
	}
	encoded, _ := json.Marshal(preview)
	if len(encoded) > toolPreviewLimit && len(preview) != 0 {
		// The fixed classification and source positions stay visible. Stack
		// detail is expendable only if those facts alone still exceed the cap.
		preview[0].Stack = nil
		encoded, _ = json.Marshal(preview)
	}
	return preview, max(int64(0), int64(originalBytes-len(encoded)))
}

func clipDiagnosticRecordStrings(diagnostic *ExecutionDiagnostic, limit int) {
	diagnostic.Message = clipDiagnosticText(diagnostic.Message, limit)
	diagnostic.Source = clipDiagnosticText(diagnostic.Source, limit)
	for index := range diagnostic.Stack {
		diagnostic.Stack[index].Function = clipDiagnosticText(diagnostic.Stack[index].Function, limit)
		diagnostic.Stack[index].Source = clipDiagnosticText(diagnostic.Stack[index].Source, limit)
	}
}

func cloneExecutionDiagnostics(source []ExecutionDiagnostic) []ExecutionDiagnostic {
	if len(source) == 0 {
		return nil
	}
	clone := make([]ExecutionDiagnostic, len(source))
	for index, diagnostic := range source {
		clone[index] = diagnostic
		clone[index].Stack = append([]ExecutionFrame(nil), diagnostic.Stack...)
		clone[index].Exception = cloneExceptionMap(diagnostic.Exception)
	}
	return clone
}

func cloneExceptionMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = cloneDiagnosticValue(value)
	}
	return clone
}

func cloneDiagnosticValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneExceptionMap(typed)
	case []any:
		clone := make([]any, len(typed))
		for index, item := range typed {
			clone[index] = cloneDiagnosticValue(item)
		}
		return clone
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}

func clipDiagnosticText(text string, limit int) string {
	if limit == 0 {
		if text == "" {
			return ""
		}
		return "…"
	}
	if len(text) <= limit {
		return text
	}
	marker := "…"
	keep := limit - len(marker)
	if keep < 0 {
		keep = 0
	}
	for keep > 0 && !utf8.RuneStart(text[keep]) {
		keep--
	}
	return text[:keep] + marker
}

func clipDiagnosticException(source map[string]any, limit int) map[string]any {
	if len(source) == 0 {
		return nil
	}
	clone := cloneExceptionMap(source)
	for key, value := range clone {
		clone[key] = clipDiagnosticValue(value, limit)
	}
	return clone
}

func clipDiagnosticValue(value any, limit int) any {
	switch typed := value.(type) {
	case string:
		return clipDiagnosticText(typed, limit)
	case map[string]any:
		return clipDiagnosticException(typed, limit)
	case []any:
		clone := make([]any, len(typed))
		for index, item := range typed {
			clone[index] = clipDiagnosticValue(item, limit)
		}
		return clone
	case []string:
		clone := make([]string, len(typed))
		for index, item := range typed {
			clone[index] = clipDiagnosticText(item, limit)
		}
		return clone
	default:
		return value
	}
}
