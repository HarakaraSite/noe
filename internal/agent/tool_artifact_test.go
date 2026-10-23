package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestToolResultStoreSkipsSmallResultsAndStoresLargeDataJSON(t *testing.T) {
	ctx := t.Context()
	store := new(toolResultStore)
	stdout := &artifactTestReader{reader: strings.NewReader("small output")}
	result := &Result{Stdout: "small output", ExitCode: 0, Reason: "completed", Data: json.RawMessage("null"), StdoutFull: stdout}
	if err := store.retain(ctx, result); err != nil {
		t.Fatal(err)
	}
	if result.ResultRef != "" || string(result.Data) != "null" || store.dir != "" {
		t.Fatalf("small result=%+v storeDir=%q", result, store.dir)
	}
	if !stdout.closed || result.StdoutFull != nil {
		t.Fatalf("small-result reader closed=%v retained=%v", stdout.closed, result.StdoutFull)
	}

	data := json.RawMessage(`{"payload":"` + strings.Repeat("x", toolPreviewLimit) + `"}`)
	large := &Result{Stdout: "ready", ExitCode: 3, Reason: "failed", Data: data, Phase: "serialize"}
	if err := store.retain(ctx, large); err != nil {
		t.Fatal(err)
	}
	if large.ResultRef == "" || len(large.Data) != 0 || large.DataOmittedBytes != int64(len(data)) {
		t.Fatalf("large result preview = %+v", large)
	}
	full := readAllToolArtifact(t, store, large.ResultRef)
	want := marshalFullToolResult(t, "ready", "", 3, "failed", data, nil, "serialize")
	if !bytes.Equal(full, want) {
		t.Fatalf("artifact differs from complete result JSON\n got: %s\nwant: %s", full, want)
	}
	var decoded struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(full, &decoded); err != nil || !bytes.Equal(decoded.Data, data) {
		t.Fatalf("full data=%s decode error=%v", decoded.Data, err)
	}
	dir := store.dir
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("store directory survived close: %q err=%v", dir, err)
	}
}

func TestToolResultStorePagesEscapedUnicodeStream(t *testing.T) {
	ctx := t.Context()
	store := new(toolResultStore)
	stream := strings.Repeat("a", 32767) + "é\"\\<&>\n" + strings.Repeat("🙂e\u2028", 18000) + string([]byte{0xff}) + "tail"
	preview := stream[:24] + "\n... omitted ...\n" + stream[len(stream)-24:]
	reader := &artifactTestReader{reader: strings.NewReader(stream)}
	data := json.RawMessage(`{"answer":42}`)
	result := &Result{
		Stdout: preview, StdoutOmittedBytes: int64(len(stream) - len(preview)), StdoutFull: reader,
		Stderr: "", ExitCode: 0, Reason: "completed", Data: data, Phase: "complete",
	}
	if err := store.retain(ctx, result); err != nil {
		t.Fatal(err)
	}
	if !reader.closed || result.StdoutFull != nil || result.ResultRef == "" {
		t.Fatalf("reader ownership/result ref: closed=%v reader=%v resultRef=%q", reader.closed, result.StdoutFull, result.ResultRef)
	}
	full := readAllToolArtifact(t, store, result.ResultRef)
	want := marshalFullToolResult(t, stream, "", 0, "completed", data, nil, "complete")
	if !bytes.Equal(full, want) {
		t.Fatalf("stream artifact did not preserve the full JSON text")
	}
	for _, escaped := range []string{`\u003c`, `\u003e`, `\u0026`, `\n`, `\"`, `\\`, `\u2028`} {
		if !bytes.Contains(full, []byte(escaped)) {
			t.Fatalf("JSON escaping %q missing from artifact", escaped)
		}
	}
	if !bytes.Contains(full, []byte("�")) {
		t.Fatal("invalid UTF-8 input was not normalized to the replacement character")
	}
	if len(full) <= toolPreviewLimit {
		t.Fatalf("fixture should be larger than preview limit: %d", len(full))
	}

	accent := bytes.Index(full, []byte("é"))
	if accent < 3 {
		t.Fatalf("accented fixture text missing from artifact")
	}
	limit := int64(4)
	page, err := store.read(ctx, toolArtifactRequest{ResultRef: result.ResultRef, Offset: int64(accent - 3), Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if page.Text != "aaa" || page.NextOffset != int64(accent) || !page.Truncated {
		t.Fatalf("split-rune page=%+v, want three ASCII bytes before é", page)
	}
	if _, err := store.read(ctx, toolArtifactRequest{ResultRef: result.ResultRef, Offset: int64(accent + 1), Limit: &limit}); err == nil {
		t.Fatal("offset inside a multibyte rune was accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestToolResultStoreValidatesPagesAndReferenceOwnership(t *testing.T) {
	ctx := t.Context()
	first, second := new(toolResultStore), new(toolResultStore)
	stream := strings.Repeat("page-💡", 16000)
	result := &Result{
		Stdout: "preview", StdoutOmittedBytes: int64(len(stream)),
		StdoutFull: io.NopCloser(strings.NewReader(stream)), Reason: "completed",
	}
	if err := first.retain(ctx, result); err != nil {
		t.Fatal(err)
	}
	if _, err := second.read(ctx, toolArtifactRequest{ResultRef: result.ResultRef}); err == nil {
		t.Fatal("another store accepted this result reference")
	} else {
		requireArtifactOperationError(t, err, http.StatusNotFound)
	}
	if _, err := first.read(ctx, toolArtifactRequest{ResultRef: result.ResultRef, Offset: -1}); err == nil {
		t.Fatal("negative offset was accepted")
	} else {
		requireArtifactOperationError(t, err, http.StatusBadRequest)
	}
	tooSmall, tooLarge := int64(3), int64(toolPreviewLimit+1)
	for _, limit := range []*int64{&tooSmall, &tooLarge} {
		if _, err := first.read(ctx, toolArtifactRequest{ResultRef: result.ResultRef, Limit: limit}); err == nil {
			t.Fatalf("invalid limit %d was accepted", *limit)
		} else {
			requireArtifactOperationError(t, err, http.StatusBadRequest)
		}
	}
	firstPage, err := first.read(ctx, toolArtifactRequest{ResultRef: result.ResultRef})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Text) > toolDefaultPageLimit || firstPage.NextOffset != int64(len(firstPage.Text)) || !firstPage.Truncated {
		t.Fatalf("default page = %+v", firstPage)
	}
	maxLimit := int64(toolPreviewLimit)
	maxPage, err := first.read(ctx, toolArtifactRequest{ResultRef: result.ResultRef, Limit: &maxLimit})
	if err != nil || len(maxPage.Text) > toolPreviewLimit || !maxPage.Truncated {
		t.Fatalf("maximum page=%+v error=%v", maxPage, err)
	}
	endPage, err := first.read(ctx, toolArtifactRequest{ResultRef: result.ResultRef, Offset: firstPage.TotalBytes})
	if err != nil || endPage.Text != "" || endPage.NextOffset != firstPage.TotalBytes || endPage.Truncated {
		t.Fatalf("end page=%+v error=%v", endPage, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := first.read(cancelled, toolArtifactRequest{ResultRef: result.ResultRef}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error=%v", err)
	}
	full := readAllToolArtifact(t, first, result.ResultRef)
	if len(full) != int(firstPage.TotalBytes) {
		t.Fatalf("recovered %d bytes, totalBytes=%d", len(full), firstPage.TotalBytes)
	}
	middleRune := bytes.Index(full, []byte("💡"))
	if middleRune < 0 {
		t.Fatal("Unicode fixture missing from artifact")
	}
	if _, err := first.read(ctx, toolArtifactRequest{ResultRef: result.ResultRef, Offset: int64(middleRune + 1)}); err == nil {
		t.Fatal("offset inside emoji was accepted")
	} else {
		requireArtifactOperationError(t, err, http.StatusBadRequest)
	}
	if _, err := first.read(ctx, toolArtifactRequest{ResultRef: result.ResultRef, Offset: int64(len(full) + 1)}); err == nil {
		t.Fatal("offset beyond artifact was accepted")
	} else {
		requireArtifactOperationError(t, err, http.StatusBadRequest)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := first.read(ctx, toolArtifactRequest{ResultRef: result.ResultRef}); err == nil {
		t.Fatal("closed store accepted a result reference")
	} else {
		requireArtifactOperationError(t, err, http.StatusGone)
	}
}

func TestToolResultStoreReadsFinalPagesNearLimit(t *testing.T) {
	for _, tail := range []int64{1, 2, 3} {
		t.Run(strconv.FormatInt(tail, 10), func(t *testing.T) {
			store := new(toolResultStore)
			empty := marshalFullToolResult(t, "", "", 0, "completed", nil, nil, "")
			text := strings.Repeat("x", toolDefaultPageLimit+int(tail)-len(empty))
			result := &Result{StdoutOmittedBytes: int64(len(text)), StdoutFull: io.NopCloser(strings.NewReader(text)), Reason: "completed"}
			if err := store.retain(t.Context(), result); err != nil {
				t.Fatal(err)
			}
			want := marshalFullToolResult(t, text, "", 0, "completed", nil, nil, "")
			if int64(len(want)) != int64(toolDefaultPageLimit)+tail {
				t.Fatalf("fixture artifact size=%d, want %d", len(want), toolDefaultPageLimit+tail)
			}
			if full := readAllToolArtifact(t, store, result.ResultRef); !bytes.Equal(full, want) {
				t.Fatalf("final page with %d-byte tail was not recovered", tail)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestToolResultStoreCleansCancelledAndFailedRetains(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	store := new(toolResultStore)
	reader := &artifactTestReader{reader: strings.NewReader("full"), closeErr: errors.New("close input")}
	result := &Result{Stdout: "preview", StdoutOmittedBytes: 1, StdoutFull: reader}
	if err := store.retain(ctx, result); !errors.Is(err, context.Canceled) || !errors.Is(err, reader.closeErr) {
		t.Fatalf("cancelled retain error = %v", err)
	}
	if !reader.closed || result.StdoutFull != nil || store.dir != "" {
		t.Fatalf("cancel cleanup reader=%v field=%v dir=%q", reader.closed, result.StdoutFull, store.dir)
	}

	base := t.TempDir()
	dir := filepath.Join(base, "store")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	store = &toolResultStore{dir: dir}
	readErr, closeErr := errors.New("read input"), errors.New("close input")
	failedReader := &artifactTestReader{reader: &artifactFailReader{err: readErr}, closeErr: closeErr}
	largeData := json.RawMessage(`{"payload":"` + strings.Repeat("x", toolPreviewLimit) + `"}`)
	largeDiagnostic := ExecutionDiagnostic{
		Code: "JS_EXCEPTION", Phase: "evaluate", Message: strings.Repeat("failure-💡", 12000),
		Source: "[agent tool]", Line: 4, Exception: map[string]any{"stderr": strings.Repeat("detail-💡", 12000)},
	}
	failedResult := &Result{
		Stdout: "preview", StdoutOmittedBytes: 1, StdoutFull: failedReader,
		Data: largeData, Diagnostics: []ExecutionDiagnostic{largeDiagnostic},
	}
	if err := store.retain(t.Context(), failedResult); !errors.Is(err, readErr) || !errors.Is(err, closeErr) {
		t.Fatalf("failed retain error = %v", err)
	}
	if !failedReader.closed || failedResult.StdoutFull != nil || failedResult.ResultRef != "" || len(failedResult.Data) != 0 || failedResult.DataOmittedBytes != int64(len(largeData)) || len(failedResult.Diagnostics) != 1 || failedResult.DiagnosticsOmittedBytes == 0 {
		t.Fatalf("failed cleanup/preview reader=%v field=%v result=%+v", failedReader.closed, failedResult.StdoutFull, failedResult)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed artifact directory survived: %v", err)
	}
}

func TestToolResultStorePreviewsLargeDiagnosticsAndPreservesFullFacts(t *testing.T) {
	ctx := t.Context()
	store := new(toolResultStore)
	frames := make([]ExecutionFrame, 100)
	for index := range frames {
		frames[index] = ExecutionFrame{Function: "frame", Source: "fixture.js", Line: index + 1}
	}
	diagnostic := ExecutionDiagnostic{
		Code: "JS_EXCEPTION", Phase: "evaluate", Message: strings.Repeat("message-💡", 12000),
		Source: "[agent tool]", Line: 7, Column: 3, Stack: frames,
		Exception: map[string]any{
			"stderr": strings.Repeat("exception-💡", 12000),
			"nested": map[string]any{"message": []any{strings.Repeat("nested-💡", 12000)}},
		},
	}
	originalDiagnostics := []ExecutionDiagnostic{diagnostic}
	originalJSON, err := json.Marshal(originalDiagnostics)
	if err != nil {
		t.Fatal(err)
	}
	result := &Result{Stdout: "", Stderr: "", ExitCode: 1, Reason: "failed", Diagnostics: originalDiagnostics, Phase: "evaluate"}
	if err := store.retain(ctx, result); err != nil {
		t.Fatal(err)
	}
	if result.ResultRef == "" || len(result.Diagnostics) != 1 || result.DiagnosticsOmittedBytes == 0 {
		t.Fatalf("diagnostic preview metadata = %+v", result)
	}
	previewJSON, err := json.Marshal(result.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if len(previewJSON) > toolPreviewLimit || result.DiagnosticsOmittedBytes != int64(len(originalJSON)-len(previewJSON)) {
		t.Fatalf("preview bytes=%d omitted=%d full=%d", len(previewJSON), result.DiagnosticsOmittedBytes, len(originalJSON))
	}
	preview := result.Diagnostics[0]
	if preview.Code != diagnostic.Code || preview.Phase != diagnostic.Phase || preview.Source != diagnostic.Source || preview.Line != diagnostic.Line || preview.Column != diagnostic.Column {
		t.Fatalf("diagnostic identity facts changed: %+v", preview)
	}
	if len(preview.Stack) != 64 || preview.Stack[0] != frames[0] || preview.Stack[47] != frames[47] || preview.Stack[48] != frames[84] || preview.Stack[63] != frames[99] {
		t.Fatalf("stack preview omitted wrong frames: first=%+v last=%+v", preview.Stack[0], preview.Stack[len(preview.Stack)-1])
	}
	if !strings.HasSuffix(preview.Message, "…") || !strings.HasSuffix(preview.Exception["stderr"].(string), "…") {
		t.Fatalf("diagnostic text lacks truncation markers")
	}
	nested := preview.Exception["nested"].(map[string]any)["message"].([]any)[0].(string)
	if !strings.HasSuffix(nested, "…") {
		t.Fatalf("nested exception text lacks truncation marker")
	}
	full := readAllToolArtifact(t, store, result.ResultRef)
	var decoded struct {
		Diagnostics []ExecutionDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(full, &decoded); err != nil {
		t.Fatal(err)
	}
	fullDiagJSON, _ := json.Marshal(decoded.Diagnostics)
	if !bytes.Equal(fullDiagJSON, originalJSON) {
		t.Fatal("artifact did not preserve full diagnostics")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestToolResultStoreClipsLargeDiagnosticSourcesAndFrames(t *testing.T) {
	store := new(toolResultStore)
	largeURL := "https://example.test/" + strings.Repeat("λ", 6000)
	largeFunction := "handler_" + strings.Repeat("関数", 3000)
	frames := make([]ExecutionFrame, 70)
	for index := range frames {
		frames[index] = ExecutionFrame{Function: largeFunction, Source: largeURL, Line: index + 1}
	}
	diagnostic := ExecutionDiagnostic{
		Code: "JS_EXCEPTION", Phase: "evaluate", Message: "inner failure",
		Source: largeURL, Line: 17, Column: 5, Stack: frames,
	}
	originalDiagnostics := []ExecutionDiagnostic{diagnostic}
	originalJSON, err := json.Marshal(originalDiagnostics)
	if err != nil {
		t.Fatal(err)
	}
	result := &Result{ExitCode: 1, Reason: "failed", Diagnostics: originalDiagnostics, Phase: "evaluate"}
	if err := store.retain(t.Context(), result); err != nil {
		t.Fatal(err)
	}
	previewJSON, err := json.Marshal(result.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResultRef == "" || len(previewJSON) > toolPreviewLimit || result.DiagnosticsOmittedBytes <= 0 {
		t.Fatalf("diagnostic preview metadata ref=%q bytes=%d omitted=%d", result.ResultRef, len(previewJSON), result.DiagnosticsOmittedBytes)
	}
	if result.DiagnosticsOmittedBytes != int64(len(originalJSON)-len(previewJSON)) {
		t.Fatalf("omitted=%d, want full-preview=%d", result.DiagnosticsOmittedBytes, len(originalJSON)-len(previewJSON))
	}
	preview := result.Diagnostics[0]
	if preview.Code != diagnostic.Code || preview.Phase != diagnostic.Phase || preview.Line != diagnostic.Line || preview.Column != diagnostic.Column {
		t.Fatalf("diagnostic identity facts changed: %+v", preview)
	}
	if len(preview.Stack) != 64 || preview.Stack[0].Line != frames[0].Line || preview.Stack[47].Line != frames[47].Line || preview.Stack[48].Line != frames[54].Line || preview.Stack[63].Line != frames[69].Line {
		t.Fatalf("stack preview omitted wrong frames: count=%d", len(preview.Stack))
	}
	for name, text := range map[string]string{
		"diagnostic source": preview.Source,
		"frame function":    preview.Stack[0].Function,
		"frame source":      preview.Stack[0].Source,
	} {
		if !utf8.ValidString(text) || len(text) > 256 || !strings.HasSuffix(text, "…") {
			t.Fatalf("%s was not UTF-8 clipped with marker: bytes=%d text=%q", name, len(text), text)
		}
	}
	if !strings.HasPrefix(preview.Source, "https://example.test/") || !strings.HasPrefix(preview.Stack[0].Function, "handler_") || !strings.HasPrefix(preview.Stack[0].Source, "https://example.test/") {
		t.Fatalf("clipped source/frame lost their useful prefixes: %+v", preview)
	}
	full := readAllToolArtifact(t, store, result.ResultRef)
	var decoded struct {
		Diagnostics []ExecutionDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(full, &decoded); err != nil {
		t.Fatal(err)
	}
	fullDiagJSON, err := json.Marshal(decoded.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fullDiagJSON, originalJSON) {
		t.Fatal("artifact did not preserve unmodified source/frame diagnostics")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestToolResultStoreDiagnosticFallbackKeepsOneRecordWithinBudget(t *testing.T) {
	store := new(toolResultStore)
	largeURL := "file:///" + strings.Repeat("路径", 512)
	largeFunction := "worker_" + strings.Repeat("関数", 512)
	const diagnosticCount = 1000
	diagnostics := make([]ExecutionDiagnostic, diagnosticCount)
	for index := range diagnostics {
		diagnostics[index] = ExecutionDiagnostic{
			Code: "JS_EXCEPTION", Phase: "evaluate", Message: "inner failure",
			Source: largeURL, Line: index + 1, Column: 3,
			Stack: []ExecutionFrame{{Function: largeFunction, Source: largeURL, Line: index + 1, Column: 4}},
		}
	}
	originalJSON, err := json.Marshal(diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	result := &Result{ExitCode: 1, Reason: "failed", Diagnostics: diagnostics, Phase: "evaluate"}
	if err := store.retain(t.Context(), result); err != nil {
		t.Fatal(err)
	}
	previewJSON, err := json.Marshal(result.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResultRef == "" || len(previewJSON) > toolPreviewLimit || result.DiagnosticsOmittedBytes <= 0 {
		t.Fatalf("fallback preview metadata ref=%q diagnostics=%d bytes=%d omitted=%d", result.ResultRef, len(result.Diagnostics), len(previewJSON), result.DiagnosticsOmittedBytes)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("fallback kept %d diagnostics, want the first record only", len(result.Diagnostics))
	}
	if result.DiagnosticsOmittedBytes != int64(len(originalJSON)-len(previewJSON)) {
		t.Fatalf("omitted=%d, want full-preview=%d", result.DiagnosticsOmittedBytes, len(originalJSON)-len(previewJSON))
	}
	preview := result.Diagnostics[0]
	if preview.Code != diagnostics[0].Code || preview.Phase != diagnostics[0].Phase || preview.Line != diagnostics[0].Line || preview.Column != diagnostics[0].Column {
		t.Fatalf("fallback changed classification/position: %+v", preview)
	}
	if !utf8.ValidString(preview.Source) || preview.Source != "…" || len(preview.Stack) != 1 || preview.Stack[0].Function != "…" || preview.Stack[0].Source != "…" {
		t.Fatalf("fallback source/frame values were not bounded: %+v", preview)
	}
	full := readAllToolArtifact(t, store, result.ResultRef)
	var decoded struct {
		Diagnostics []ExecutionDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(full, &decoded); err != nil {
		t.Fatal(err)
	}
	fullDiagJSON, err := json.Marshal(decoded.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(fullDiagJSON, originalJSON) {
		t.Fatal("artifact did not preserve all diagnostics after one-record fallback")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestToolResultStoreConcurrentRetainAndRead(t *testing.T) {
	store := new(toolResultStore)
	const workers = 6
	var group sync.WaitGroup
	errCh := make(chan error, workers)
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			text := strings.Repeat(string(rune('a'+index))+"💡", 9000)
			result := &Result{Stdout: "preview", StdoutOmittedBytes: int64(len(text)), StdoutFull: io.NopCloser(strings.NewReader(text)), Reason: "completed"}
			if err := store.retain(context.Background(), result); err != nil {
				errCh <- err
				return
			}
			full := readAllToolArtifact(t, store, result.ResultRef)
			if !bytes.Contains(full, []byte(text)) {
				errCh <- errors.New("concurrent artifact lost its stream")
			}
		}(index)
	}
	group.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestToolResultStoreCloseRacesWithRetainAndRead(t *testing.T) {
	store := new(toolResultStore)
	const workers = 8
	start := make(chan struct{})
	results := make(chan error, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			<-start
			text := strings.Repeat(string(rune('k'+index))+"💡", 30000)
			result := &Result{Stdout: "preview", StdoutOmittedBytes: int64(len(text)), StdoutFull: io.NopCloser(strings.NewReader(text)), Reason: "completed"}
			if err := store.retain(context.Background(), result); err != nil {
				if !errors.Is(err, errToolResultStoreClosed) {
					results <- err
				}
				return
			}
			if _, err := store.read(context.Background(), toolArtifactRequest{ResultRef: result.ResultRef}); err != nil {
				var operationErr *operationError
				if !errors.As(err, &operationErr) || operationErr.status != http.StatusGone {
					results <- err
				}
			}
		}(index)
	}
	closeDone := make(chan error, 1)
	go func() {
		<-start
		closeDone <- store.Close()
	}()
	close(start)
	group.Wait()
	closeErr := <-closeDone
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	close(results)
	for err := range results {
		t.Error(err)
	}
}

type fullToolResultJSON struct {
	Stdout      string                `json:"stdout"`
	Stderr      string                `json:"stderr"`
	ExitCode    int                   `json:"exitCode"`
	Reason      string                `json:"reason"`
	Data        json.RawMessage       `json:"data,omitempty"`
	Diagnostics []ExecutionDiagnostic `json:"diagnostics,omitempty"`
	Phase       string                `json:"phase,omitempty"`
}

func marshalFullToolResult(t *testing.T, stdout, stderr string, exitCode int, reason string, data json.RawMessage, diagnostics []ExecutionDiagnostic, phase string) []byte {
	t.Helper()
	encoded, err := json.Marshal(fullToolResultJSON{
		Stdout: stdout, Stderr: stderr, ExitCode: exitCode, Reason: reason,
		Data: data, Diagnostics: diagnostics, Phase: phase,
	})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func readAllToolArtifact(t *testing.T, store *toolResultStore, ref string) []byte {
	t.Helper()
	var output bytes.Buffer
	var offset, total int64
	for {
		page, err := store.read(t.Context(), toolArtifactRequest{ResultRef: ref, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		if offset == 0 {
			total = page.TotalBytes
		} else if page.TotalBytes != total {
			t.Fatalf("artifact size changed while reading: %d != %d", page.TotalBytes, total)
		}
		if page.Offset != offset || page.NextOffset-offset != int64(len(page.Text)) {
			t.Fatalf("page offsets inconsistent: %+v offset=%d", page, offset)
		}
		output.WriteString(page.Text)
		if !page.Truncated {
			break
		}
		if page.NextOffset <= offset {
			t.Fatalf("page did not advance: %+v", page)
		}
		offset = page.NextOffset
	}
	if int64(output.Len()) != total {
		t.Fatalf("read %d bytes, totalBytes=%d", output.Len(), total)
	}
	return output.Bytes()
}

func requireArtifactOperationError(t *testing.T, err error, status int) {
	t.Helper()
	var operationErr *operationError
	if !errors.As(err, &operationErr) || operationErr.status != status {
		t.Fatalf("operation error=%v, want status %d", err, status)
	}
}

type artifactTestReader struct {
	reader   io.Reader
	closeErr error
	closed   bool
}

func (r *artifactTestReader) Read(p []byte) (int, error) { return r.reader.Read(p) }
func (r *artifactTestReader) Close() error {
	r.closed = true
	return r.closeErr
}

type artifactFailReader struct{ err error }

func (r *artifactFailReader) Read(p []byte) (int, error) {
	return 0, r.err
}
