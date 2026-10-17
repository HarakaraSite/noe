package observation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureRunID = "423a2834915a42cb0a5fe2533117e0354010ad4ae4e8846f"
const fixtureExecutionID = "7ca3f7070d98c10f"

func TestObserveRealRunFixtureAndRepeatableCursor(t *testing.T) {
	path := filepath.Join("testdata", "real-run.jsonl")
	query := Query{
		LogPath: path, RunID: fixtureRunID, IncludeThinking: true, IncludeOutput: true,
	}
	snapshot, err := Observe(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "completed" || snapshot.Phase != "settled" {
		t.Fatalf("status/phase = %q/%q", snapshot.Status, snapshot.Phase)
	}
	if snapshot.FinalText == "" || !strings.Contains(snapshot.FinalText, "5050") {
		t.Fatalf("finalText = %q", snapshot.FinalText)
	}
	if snapshot.ParentRunID != "" || snapshot.Name != "try-agent" || snapshot.Model != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("run metadata = parent:%q name:%q model:%q", snapshot.ParentRunID, snapshot.Name, snapshot.Model)
	}
	if snapshot.Cursor != (Cursor{ExecutionID: fixtureExecutionID, Seq: 37}) {
		t.Fatalf("cursor = %+v", snapshot.Cursor)
	}
	if snapshot.Usage == nil || snapshot.Usage.PromptTokens != 2057 || snapshot.Usage.CompletionTokens != 159 || snapshot.Usage.TotalTokens != 2216 || snapshot.Usage.CachedTokens != 0 {
		t.Fatalf("usage = %+v", snapshot.Usage)
	}
	if snapshot.LastToolResult == nil || !snapshot.LastToolResult.Success || snapshot.LastToolResult.ExitCode != 0 || !strings.Contains(snapshot.LastToolResult.Text, "5050") {
		t.Fatalf("lastToolResult = %+v", snapshot.LastToolResult)
	}
	if snapshot.Action != "// 1から100までの整数の合計を計算して出力する" {
		t.Fatalf("action = %q", snapshot.Action)
	}
	if !strings.Contains(snapshot.UpdatedAt, "2026-10-16T16:07:") {
		t.Fatalf("updatedAt = %q", snapshot.UpdatedAt)
	}

	var sawMessageDelta, sawFinal, sawCombinedThinking bool
	for _, event := range snapshot.Events {
		switch event["type"] {
		case "message_delta":
			sawMessageDelta = true
		case "agent.final":
			sawFinal = true
		case "thinking":
			sawCombinedThinking = event["text"] == "Done. Report in Japanese."
		}
	}
	if sawMessageDelta || !sawFinal || !sawCombinedThinking {
		t.Fatalf("event projection: message_delta=%v final=%v combinedThinking=%v", sawMessageDelta, sawFinal, sawCombinedThinking)
	}

	query.Cursor = &snapshot.Cursor
	repeated, err := Observe(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	if len(repeated.Events) != 0 {
		t.Fatalf("same cursor returned %d events", len(repeated.Events))
	}
	if repeated.Status != snapshot.Status || repeated.FinalText != snapshot.FinalText || repeated.Usage == nil || repeated.Usage.TotalTokens != 2216 {
		t.Fatalf("cursor filtered reconstructed state: %+v", repeated)
	}

	query.Cursor = &Cursor{ExecutionID: "another-execution", Seq: 100}
	if _, err := Observe(context.Background(), query); err == nil || !strings.Contains(err.Error(), "does not match runId") {
		t.Fatalf("mismatched cursor error = %v", err)
	}
}

func TestObserveUnknownRunHasEmptyEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Observe(context.Background(), Query{LogPath: path, RunID: "not-started"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "unknown" || snapshot.Phase != "unknown" || snapshot.Events == nil || len(snapshot.Events) != 0 {
		t.Fatalf("unknown snapshot = %+v", snapshot)
	}
}

func TestObserveParallelToolsKeepRunActiveAndChooseActiveAction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeEvent(t, path, 1, "agent.started", map[string]any{"runId": "r", "parentRunId": "", "name": "parallel", "model": "m"})
	writeEvent(t, path, 2, "model.started", map[string]any{"runId": "r", "requestId": "req"})
	writeEvent(t, path, 3, "model.finished", map[string]any{"runId": "r", "requestId": "req", "usage": map[string]any{"promptTokens": 3, "completionTokens": 1, "totalTokens": 4, "cost": 0.25}})
	writeEvent(t, path, 4, "run_js.started", map[string]any{"runId": "r", "toolCallId": "a", "text": "// tool A"})
	writeEvent(t, path, 5, "stdout", map[string]any{"runId": "r", "toolCallId": "a", "source": "run_js", "text": "A output\n"})
	writeEvent(t, path, 6, "run_js.started", map[string]any{"runId": "r", "toolCallId": "b", "text": "// tool B"})
	writeEvent(t, path, 7, "run_js.finished", map[string]any{"runId": "r", "toolCallId": "a", "success": false, "exitCode": 2, "reason": "failed", "duration": 900})

	snapshot, err := Observe(context.Background(), Query{LogPath: path, RunID: "r", IncludeOutput: true})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "running" || snapshot.Phase != "run_js" || snapshot.Action != "// tool B" {
		t.Fatalf("parallel state = status:%q phase:%q action:%q", snapshot.Status, snapshot.Phase, snapshot.Action)
	}
	if snapshot.LastToolResult == nil || snapshot.LastToolResult.Success || snapshot.LastToolResult.ExitCode != 2 || snapshot.LastToolResult.Text != "A output\n" {
		t.Fatalf("failed tool result = %+v", snapshot.LastToolResult)
	}
	if snapshot.Usage == nil || snapshot.Usage.TotalTokens != 4 {
		t.Fatalf("usage = %+v", snapshot.Usage)
	}

	writeEvent(t, path, 8, "run_js.finished", map[string]any{"runId": "r", "toolCallId": "b", "success": true, "exitCode": 0, "reason": "completed", "duration": 1000})
	snapshot, err = Observe(context.Background(), Query{LogPath: path, RunID: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Phase != "between_steps" || snapshot.Action != "// tool B" || snapshot.LastToolResult == nil || !snapshot.LastToolResult.Success {
		t.Fatalf("after parallel tools = phase:%q action:%q result:%+v", snapshot.Phase, snapshot.Action, snapshot.LastToolResult)
	}
}

func TestObserveWaitsForLifecycleCompletionAndReturnsImmediatelyAfter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	result := make(chan struct {
		snapshot Snapshot
		err      error
	}, 1)
	go func() {
		snapshot, err := Observe(context.Background(), Query{LogPath: path, RunID: "r", Wait: true})
		result <- struct {
			snapshot Snapshot
			err      error
		}{snapshot, err}
	}()

	// The waiter starts before the target exists, then observes it while active.
	time.Sleep(20 * time.Millisecond)
	writeEvent(t, path, 1, "agent.started", map[string]any{"runId": "r", "name": "waiting", "model": "m"})
	writeEvent(t, path, 2, "agent.message", map[string]any{"runId": "r", "status": "intermediate", "text": "progress"})
	writeEvent(t, path, 3, "agent.final", map[string]any{"runId": "r", "status": "completed", "text": "answer"})
	select {
	case got := <-result:
		t.Fatalf("wait returned before lifecycle terminal: %+v", got)
	case <-time.After(140 * time.Millisecond):
	}

	writeEvent(t, path, 4, "agent.completed", map[string]any{"runId": "r", "status": "completed"})
	select {
	case got := <-result:
		if got.err != nil || got.snapshot.Status != "completed" || got.snapshot.LastMessage != "progress" || got.snapshot.FinalText != "answer" {
			t.Fatalf("wait result = %+v, %v", got.snapshot, got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not observe terminal lifecycle event")
	}

	started := time.Now()
	after, err := Observe(context.Background(), Query{LogPath: path, RunID: "r", Wait: true})
	if err != nil || after.Status != "completed" {
		t.Fatalf("post-completion wait = %+v, %v", after, err)
	}
	if time.Since(started) >= 100*time.Millisecond {
		t.Fatalf("terminal run incurred polling delay: %v", time.Since(started))
	}
}

func TestObserveCancellationInterruptsHistoryRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	var log bytes.Buffer
	encoder := json.NewEncoder(&log)
	for seq := 1; seq <= 2000; seq++ {
		typeName := "agent.message"
		fields := map[string]any{"runId": "r", "text": "progress"}
		if seq == 1 {
			typeName = "agent.started"
			fields["name"], fields["model"] = "large", "m"
		}
		if seq == 2000 {
			typeName = "agent.completed"
			fields["status"] = "completed"
		}
		event := map[string]any{
			"version": 1, "executionId": "exec", "seq": seq,
			"time": "2026-10-17T00:00:00Z", "type": typeName,
		}
		for key, value := range fields {
			event[key] = value
		}
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, log.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	counting := &countErrContext{Context: context.Background()}
	records, err := readRecords(counting, path)
	if err != nil {
		t.Fatal(err)
	}

	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Observe performs three checks before readRecords, then one check for each
	// record in its target-execution and max-sequence passes. Cancel on the fifth
	// record of the state reconstruction pass.
	checksBeforeRebuild := int32(3) + atomic.LoadInt32(&counting.checks) + 2*int32(len(records))
	ctx := &cancelAfterErrChecks{Context: base, cancel: cancel, after: checksBeforeRebuild + 5}
	_, err = Observe(ctx, Query{LogPath: path, RunID: "r"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("rebuild error = %v after %d context checks", err, atomic.LoadInt32(&ctx.checks))
	}
	if got := atomic.LoadInt32(&ctx.checks); got != checksBeforeRebuild+5 {
		t.Fatalf("context checks = %d, want cancellation at rebuild row %d", got, checksBeforeRebuild+5)
	}
}

func TestObserveCanceledContextDoesNotReturnTerminalSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Observe(ctx, Query{
		LogPath: filepath.Join("testdata", "real-run.jsonl"), RunID: fixtureRunID,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled observation error = %v", err)
	}
}

type cancelAfterErrChecks struct {
	context.Context
	cancel context.CancelFunc
	after  int32
	checks int32
}

func (c *cancelAfterErrChecks) Err() error {
	if atomic.AddInt32(&c.checks, 1) >= c.after {
		c.cancel()
	}
	return c.Context.Err()
}

type countErrContext struct {
	context.Context
	checks int32
}

func (c *countErrContext) Err() error {
	atomic.AddInt32(&c.checks, 1)
	return c.Context.Err()
}

func TestObserveWaitReturnsContextCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeEvent(t, path, 1, "agent.started", map[string]any{"runId": "r", "name": "active", "model": "m"})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := Observe(ctx, Query{LogPath: path, RunID: "r", Wait: true})
		result <- err
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("wait did not return after context cancellation")
	}
}

func TestObserveIgnoresIncompleteTailUntilNextRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	writeEvent(t, path, 1, "agent.started", map[string]any{"runId": "r", "name": "tail", "model": "m"})
	partial := `{"executionId":"exec","seq":2,"time":"2026-10-17T00:00:02Z","type":"run_js.started","version":1,"runId":"r","toolCallId":"tool","text":"// complete me"}`
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(strings.TrimSuffix(partial, `"}`)); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	snapshot, err := Observe(context.Background(), Query{LogPath: path, RunID: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Phase != "starting" || snapshot.Cursor.Seq != 1 {
		t.Fatalf("partial row was observed: phase:%q cursor:%+v", snapshot.Phase, snapshot.Cursor)
	}
	file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`"}` + "\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	snapshot, err = Observe(context.Background(), Query{LogPath: path, RunID: "r"})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Phase != "run_js" || snapshot.Action != "// complete me" || snapshot.Cursor.Seq != 2 {
		t.Fatalf("completed row not observed: phase:%q action:%q cursor:%+v", snapshot.Phase, snapshot.Action, snapshot.Cursor)
	}
}

func TestObserveReportsMalformedCompleteRowsAndAcceptsLongRecords(t *testing.T) {
	t.Run("malformed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "events.jsonl")
		if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Observe(context.Background(), Query{LogPath: path, RunID: "r"}); err == nil || !strings.Contains(err.Error(), "complete line 1") {
			t.Fatalf("malformed row error = %v", err)
		}
	})
	t.Run("long row", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "events.jsonl")
		long := strings.Repeat("x", 128*1024)
		writeEvent(t, path, 1, "agent.started", map[string]any{"runId": "r", "name": "long", "model": "m", "extra": long})
		snapshot, err := Observe(context.Background(), Query{LogPath: path, RunID: "r"})
		if err != nil || snapshot.Status != "running" {
			t.Fatalf("long row observation = %+v, %v", snapshot, err)
		}
	})
}

func writeEvent(t *testing.T, path string, seq uint64, typeName string, fields map[string]any) {
	t.Helper()
	event := map[string]any{
		"version": 1, "executionId": "exec", "seq": seq,
		"time": time.Date(2026, 10, 17, 0, 0, int(seq)%60, 0, time.UTC).Format(time.RFC3339Nano),
		"type": typeName,
	}
	for key, value := range fields {
		event[key] = value
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
