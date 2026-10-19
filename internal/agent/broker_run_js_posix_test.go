//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestRunJSBrokerUsesAndClosesChildScope(t *testing.T) {
	const privateCode = `globalThis.fixtureOnly = "private-run-js-source";`
	executable := writeFakeNOE(t, `
if [ "$1" != "run" ]; then exit 90; fi
printf 'connection=%s\n' "$NOE_AGENT_CONNECTION"
cat "$2"
`)
	var trace strings.Builder
	h := NewHost(Options{
		Provider: &fixtureProvider{}, Executable: executable, Cwd: t.TempDir(), Trace: &trace,
	})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startFixtureRun(t, op)
	code := privateCode
	payload, err := json.Marshal(runJSRequest{RunID: runID, Code: &code})
	if err != nil {
		t.Fatal(err)
	}
	response, err := op.Call("run_js", string(payload))
	if err != nil {
		t.Fatal(err)
	}
	var result Result
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		t.Fatalf("decode runner result %q: %v", response, err)
	}
	if result.Reason != "completed" || result.ExitCode != 0 || !strings.Contains(result.Stdout, privateCode) {
		t.Fatalf("run_js result = %#v", result)
	}

	var connection *Connection
	for _, line := range strings.Split(result.Stdout, "\n") {
		if strings.HasPrefix(line, "connection=") {
			var decoded Connection
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "connection=")), &decoded); err != nil {
				t.Fatalf("decode child capability: %v", err)
			}
			connection = &decoded
			break
		}
	}
	if connection == nil {
		t.Fatalf("fake child did not receive its broker connection: %q", result.Stdout)
	}
	if connection.Defaults != (Config{Provider: "openrouter", Model: "fixture/model", MaxTokens: 128, Effort: "auto"}) {
		t.Fatalf("child inherited defaults = %#v, want provider/model/maxTokens from this run", connection.Defaults)
	}
	h.mu.Lock()
	b := h.broker
	_, parentOperationActive := h.operations[op]
	activeOperations := len(h.operations)
	h.mu.Unlock()
	if b == nil || connection.URL != b.url || connection.Token != b.token || connection.Scope == "" || connection.Scope == op.scope {
		t.Fatalf("child capability = %#v, parent operation scope %q", connection, op.scope)
	}
	if !parentOperationActive || activeOperations != 1 {
		t.Fatalf("child scope was not closed after run_js: active operations=%d parent=%v", activeOperations, parentOperationActive)
	}
	traceText := trace.String()
	for _, want := range []string{`"status":"js_result"`, `"exitCode":0`, `"reason":"completed"`, `"name":"fixture"`, `"model":"fixture/model"`} {
		if !strings.Contains(traceText, want) {
			t.Errorf("trace missing %s: %s", want, traceText)
		}
	}
	for _, private := range []string{privateCode, result.Stdout, result.Stderr, connection.Token} {
		if private != "" && strings.Contains(traceText, private) {
			t.Errorf("trace leaked tool data %q: %s", private, traceText)
		}
	}
	if err := finishFixtureRun(op, runID); err != nil {
		t.Fatal(err)
	}
}

func TestRunJSObservationIncludesCommentOutputAndFailure(t *testing.T) {
	var observed []events.Event
	var observedMu sync.Mutex
	appendObserved := func(event events.Event) {
		observedMu.Lock()
		defer observedMu.Unlock()
		copy := make(events.Event, len(event))
		for key, value := range event {
			copy[key] = value
		}
		observed = append(observed, copy)
	}
	executable := writeFakeNOE(t, `
source=$(cat "$2")
case "$source" in
  *FAIL*) printf 'failed stdout\n'; printf 'failed stderr\n' >&2; exit 7 ;;
  *SUCCESS*) printf 'successful stdout\n'; printf 'successful stderr\n' >&2 ;;
  *) printf 'commentless stdout\n' ;;
esac
`)
	h := NewHost(Options{Provider: &fixtureProvider{}, Executable: executable, Cwd: t.TempDir(), Observe: appendObserved})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startFixtureRun(t, op)

	success := callRunJS(t, op, runID, "tool-success", "// inspect successful output\nconst result = 'SUCCESS';")
	if success.Reason != "completed" || success.ExitCode != 0 {
		t.Fatalf("successful run_js result = %#v", success)
	}
	failure := callRunJS(t, op, runID, "tool-failure", "// inspect failed output\nconst result = 'FAIL';")
	if failure.Reason != "failed" || failure.ExitCode != 7 {
		t.Fatalf("failed run_js result = %#v", failure)
	}
	commentless := callRunJS(t, op, runID, "tool-commentless", "const result = 'NO_COMMENT';")
	if commentless.Reason != "completed" || commentless.ExitCode != 0 {
		t.Fatalf("commentless run_js result = %#v", commentless)
	}
	if err := finishFixtureRun(op, runID); err != nil {
		t.Fatal(err)
	}

	observedMu.Lock()
	defer observedMu.Unlock()
	starts := make(map[string]events.Event)
	finishes := make(map[string]events.Event)
	outputs := make(map[string][]events.Event)
	for _, event := range observed {
		if event["runId"] != runID || event["parentRunId"] != "" || event["name"] != "fixture" || event["model"] != "fixture/model" {
			t.Errorf("run_js observation has incomplete run metadata: %#v", event)
		}
		toolCallID, _ := event["toolCallId"].(string)
		switch event["type"] {
		case "run_js.started":
			starts[toolCallID] = event
		case "run_js.finished":
			finishes[toolCallID] = event
		case "stdout", "stderr":
			if event["source"] != "run_js" {
				t.Errorf("run_js output source = %v", event["source"])
			}
			outputs[toolCallID] = append(outputs[toolCallID], event)
		}
	}
	if len(starts) != 3 || len(finishes) != 3 {
		t.Fatalf("run_js event counts: started=%d finished=%d", len(starts), len(finishes))
	}
	if starts["tool-success"]["text"] != "// inspect successful output" || starts["tool-failure"]["text"] != "// inspect failed output" {
		t.Errorf("run_js start comments = success:%v failure:%v", starts["tool-success"]["text"], starts["tool-failure"]["text"])
	}
	if _, present := starts["tool-commentless"]["text"]; present {
		t.Errorf("commentless code unexpectedly produced a comment: %#v", starts["tool-commentless"])
	}
	if finishes["tool-success"]["success"] != true || finishes["tool-success"]["exitCode"] != 0 || finishes["tool-success"]["reason"] != "completed" {
		t.Errorf("successful run_js finish = %#v", finishes["tool-success"])
	}
	if finishes["tool-failure"]["success"] != false || finishes["tool-failure"]["exitCode"] != 7 || finishes["tool-failure"]["reason"] != "failed" {
		t.Errorf("failed run_js finish = %#v", finishes["tool-failure"])
	}
	for toolCallID, wantText := range map[string]string{
		"tool-success": "successful stdout\n", "tool-failure": "failed stdout\n", "tool-commentless": "commentless stdout\n",
	} {
		found := false
		for _, event := range outputs[toolCallID] {
			if event["type"] == "stdout" && event["text"] == wantText {
				found = true
			}
		}
		if !found {
			t.Errorf("stdout event for %s did not contain %q: %#v", toolCallID, wantText, outputs[toolCallID])
		}
	}
	for toolCallID, want := range map[string]string{
		"tool-success":     "// inspect successful output\nconst result = 'SUCCESS';",
		"tool-failure":     "// inspect failed output\nconst result = 'FAIL';",
		"tool-commentless": "const result = 'NO_COMMENT';",
	} {
		if starts[toolCallID]["code"] != want {
			t.Errorf("run_js source for %s = %q, want %q", toolCallID, starts[toolCallID]["code"], want)
		}
	}
}

func TestParallelRunJSObservationKeepsToolIdentities(t *testing.T) {
	var observed []events.Event
	var observedMu sync.Mutex
	executable := writeFakeNOE(t, `
source=$(cat "$2")
case "$source" in
  *LEFT*) sleep 0.05; printf 'left output\n' ;;
  *RIGHT*) printf 'right output\n' ;;
  *) exit 9 ;;
esac
`)
	h := NewHost(Options{Provider: &fixtureProvider{}, Executable: executable, Cwd: t.TempDir(), Observe: func(event events.Event) {
		observedMu.Lock()
		defer observedMu.Unlock()
		copy := make(events.Event, len(event))
		for key, value := range event {
			copy[key] = value
		}
		observed = append(observed, copy)
	}})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startFixtureRun(t, op)
	type result struct {
		toolCallID string
		value      Result
		err        error
	}
	results := make(chan result, 2)
	go func() {
		value, err := callRunJSResult(op, runID, "tool-left", "// inspect left\nconst item = 'LEFT';")
		results <- result{"tool-left", value, err}
	}()
	go func() {
		value, err := callRunJSResult(op, runID, "tool-right", "// inspect right\nconst item = 'RIGHT';")
		results <- result{"tool-right", value, err}
	}()
	for range 2 {
		item := <-results
		if item.err != nil || item.value.Reason != "completed" || item.value.ExitCode != 0 {
			t.Fatalf("parallel %s result = %#v, error %v", item.toolCallID, item.value, item.err)
		}
		want := strings.TrimPrefix(item.toolCallID, "tool-") + " output\n"
		if !strings.Contains(item.value.Stdout, want) {
			t.Errorf("parallel %s stdout = %q, want %q", item.toolCallID, item.value.Stdout, want)
		}
	}
	if err := finishFixtureRun(op, runID); err != nil {
		t.Fatal(err)
	}

	observedMu.Lock()
	defer observedMu.Unlock()
	started := make(map[string]string)
	finished := make(map[string]bool)
	stdout := make(map[string]string)
	for _, event := range observed {
		if event["runId"] != runID {
			t.Errorf("parallel event runId = %v, want %s", event["runId"], runID)
		}
		toolCallID, _ := event["toolCallId"].(string)
		switch event["type"] {
		case "run_js.started":
			started[toolCallID], _ = event["text"].(string)
		case "run_js.finished":
			finished[toolCallID], _ = event["success"].(bool)
		case "stdout":
			stdout[toolCallID], _ = event["text"].(string)
		}
	}
	if started["tool-left"] != "// inspect left" || started["tool-right"] != "// inspect right" {
		t.Errorf("parallel start events lost tool identity: %#v", started)
	}
	if !finished["tool-left"] || !finished["tool-right"] {
		t.Errorf("parallel finish events lost tool identity: %#v", finished)
	}
	if stdout["tool-left"] != "left output\n" || stdout["tool-right"] != "right output\n" {
		t.Errorf("parallel stdout events crossed tool identities: %#v", stdout)
	}
	for _, event := range observed {
		if event["type"] == "run_js.started" && (event["toolCallId"] != "tool-left" && event["toolCallId"] != "tool-right") {
			t.Errorf("parallel start event has unknown toolCallId: %#v", event)
		}
	}
}

func callRunJS(t *testing.T, op *Operation, runID, toolCallID, code string) Result {
	t.Helper()
	result, err := callRunJSResult(op, runID, toolCallID, code)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func callRunJSResult(op *Operation, runID, toolCallID, code string) (Result, error) {
	payload, err := json.Marshal(runJSRequest{RunID: runID, ToolCallID: toolCallID, Code: &code})
	if err != nil {
		return Result{}, err
	}
	response, err := op.Call("run_js", string(payload))
	if err != nil {
		return Result{}, err
	}
	var result Result
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func TestRunJSDefaultsComeFromTheLaunchingRun(t *testing.T) {
	executable := writeFakeNOE(t, `
if [ "$1" != "run" ]; then exit 90; fi
printf '%s\n' "$NOE_AGENT_CONNECTION"
`)
	h := NewHost(Options{Provider: &fixtureProvider{}, Executable: executable, Cwd: t.TempDir()})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)

	configs := []runConfig{
		{Provider: "openrouter", Model: "fixture/left", Name: "left", MaxTokens: 37, Effort: "high"},
		{Provider: "openrouter", Model: "fixture/right", Name: "right", MaxTokens: 0, Effort: "auto"},
	}
	for _, config := range configs {
		runID := startFixtureRunWithConfig(t, op, config)
		code := "// fixture"
		payload, err := json.Marshal(runJSRequest{RunID: runID, Code: &code})
		if err != nil {
			t.Fatal(err)
		}
		response, err := op.Call("run_js", string(payload))
		if err != nil {
			t.Fatalf("run_js for %s: %v", config.Name, err)
		}
		var result Result
		if err := json.Unmarshal([]byte(response), &result); err != nil {
			t.Fatalf("decode %s result %q: %v", config.Name, response, err)
		}
		if result.ExitCode != 0 || result.Reason != "completed" {
			t.Fatalf("run_js for %s = %#v", config.Name, result)
		}
		var connection Connection
		if err := json.Unmarshal([]byte(strings.TrimSpace(result.Stdout)), &connection); err != nil {
			t.Fatalf("decode %s child connection %q: %v", config.Name, result.Stdout, err)
		}
		want := Config{Provider: config.Provider, Model: config.Model, MaxTokens: config.MaxTokens, Effort: config.Effort}
		if connection.Defaults != want {
			t.Errorf("%s child defaults = %#v, want %#v", config.Name, connection.Defaults, want)
		}
		if connection.Scope == "" || connection.Scope == op.scope {
			t.Errorf("%s child scope = %q, parent scope %q", config.Name, connection.Scope, op.scope)
		}
		if err := finishFixtureRun(op, runID); err != nil {
			t.Fatalf("finish %s: %v", config.Name, err)
		}
	}
}

func TestRunJSBrokerCancellationStopsAndClosesChildScope(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("NOE_RUNNER_CHILD_PID_FILE", pidFile)
	executable := writeFakeNOE(t, `
if [ "$1" != "run" ]; then exit 90; fi
printf 'captured before cancellation\n'
sleep 30 &
echo "$!" > "$NOE_RUNNER_CHILD_PID_FILE"
wait
`)
	var trace strings.Builder
	var observed []events.Event
	var observedMu sync.Mutex
	h := NewHost(Options{
		Provider: &fixtureProvider{}, Executable: executable, Trace: &trace,
		Observe: func(event events.Event) {
			observedMu.Lock()
			defer observedMu.Unlock()
			copy := make(events.Event, len(event))
			for key, value := range event {
				copy[key] = value
			}
			observed = append(observed, copy)
		},
	})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startFixtureRun(t, op)
	code := "// wait for cancellation\nawait new Promise(() => {});"
	payload, err := json.Marshal(runJSRequest{RunID: runID, ToolCallID: "tool-cancel", Code: &code})
	if err != nil {
		t.Fatal(err)
	}
	resultCh := make(chan error, 1)
	go func() {
		_, err := op.Call("run_js", string(payload))
		resultCh <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(pidFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake NOE child did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	finishPayload, err := json.Marshal(finishRequest{RunID: runID, Status: "cancelled"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := op.Call("finish", string(finishPayload)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-resultCh:
		var rpcErr *RPCError
		if !errors.As(err, &rpcErr) || rpcErr.Code != "cancelled" {
			t.Fatalf("cancelled run_js error = %v, want cancelled broker error", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run_js did not return after its parent run was cancelled")
	}
	h.mu.Lock()
	activeOperations, activeRuns := len(h.operations), len(h.runs)
	_, parentOperationActive := h.operations[op]
	h.mu.Unlock()
	if !parentOperationActive || activeOperations != 1 || activeRuns != 0 {
		t.Fatalf("run_js cancellation leaked broker state: operations=%d runs=%d parent=%v", activeOperations, activeRuns, parentOperationActive)
	}
	if !strings.Contains(trace.String(), `"status":"js_result"`) || !strings.Contains(trace.String(), `"reason":"cancelled"`) {
		t.Fatalf("trace omitted cancelled JS result: %s", trace.String())
	}
	observedMu.Lock()
	defer observedMu.Unlock()
	foundOutput, foundFinished := false, false
	for _, event := range observed {
		if event["toolCallId"] != "tool-cancel" && (event["type"] == "stdout" || event["type"] == "stderr" || event["type"] == "run_js.finished") {
			t.Errorf("cancelled run_js event lost tool identity: %#v", event)
		}
		if event["type"] == "stdout" && event["text"] == "captured before cancellation\n" {
			foundOutput = true
		}
		if event["type"] == "run_js.finished" {
			if event["success"] != false || event["reason"] != "cancelled" || event["exitCode"] != -1 {
				t.Errorf("cancelled run_js finish = %#v", event)
			}
			foundFinished = true
		}
	}
	if !foundOutput || !foundFinished {
		t.Fatalf("cancelled run_js events omitted captured output or finish: output=%v finished=%v events=%#v", foundOutput, foundFinished, observed)
	}
}

func TestFinishingRunCancelsAndReclaimsChildScopes(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var trace strings.Builder
	p := &fixtureProvider{gen: func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release
		return provider.Response{}, ctx.Err()
	}}
	h := NewHost(Options{Provider: p, Trace: &trace})
	t.Cleanup(h.Close)
	parentOp := h.NewOperation(context.Background())
	t.Cleanup(parentOp.Close)
	parentID := startFixtureRun(t, parentOp)

	childOp := h.NewOperation(context.Background())
	childConfig := runConfig{Provider: "openrouter", Model: "fixture/child", Name: "child", MaxTokens: 211}
	childOp.parentRunID = parentID
	h.mu.Lock()
	parentRun := h.runs[parentID]
	parentRun.childScopes[childOp] = struct{}{}
	h.mu.Unlock()
	childID := startFixtureRunWithConfig(t, childOp, childConfig)
	message, _ := json.Marshal(map[string]string{"role": "user", "content": "blocked child"})
	modelPayload, _ := json.Marshal(modelRequest{
		RunID: childID, Model: childConfig.Model, MaxTokens: childConfig.MaxTokens,
		Messages: []json.RawMessage{message},
	})
	modelDone := make(chan error, 1)
	go func() {
		_, err := childOp.Call("model", string(modelPayload))
		modelDone <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("child provider request did not start")
	}

	finishDone := make(chan error, 1)
	go func() { finishDone <- finishFixtureRun(parentOp, parentID) }()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("parent finish did not cancel the child provider request")
	}
	select {
	case err := <-finishDone:
		t.Fatalf("parent finish returned before child request cleanup: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-finishDone:
		if err != nil {
			t.Fatalf("finish parent: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("parent finish did not complete after child cleanup")
	}
	select {
	case err := <-modelDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("child model error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("child model call did not return")
	}

	h.mu.Lock()
	_, parentStillRunning := h.runs[parentID]
	_, childStillRunning := h.runs[childID]
	_, childScopeActive := h.operations[childOp]
	_, parentScopeActive := h.operations[parentOp]
	h.mu.Unlock()
	if parentStillRunning || childStillRunning || childScopeActive || !parentScopeActive {
		t.Fatalf("finish left stale state: parentRun=%v childRun=%v childScope=%v parentScope=%v", parentStillRunning, childStillRunning, childScopeActive, parentScopeActive)
	}
	if !strings.Contains(trace.String(), `"parentRunId":"`+parentID+`"`) || !strings.Contains(trace.String(), `"model":"fixture/child"`) {
		t.Fatalf("trace omitted child lineage/config: %s", trace.String())
	}
}

func TestSummarizeUsageIncludesOnlyKnownNumericFields(t *testing.T) {
	raw := json.RawMessage(`{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":25},"cost":0.0001,"vendor_extension":"private"}`)
	summary := summarizeUsage(raw)
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{`"promptTokens":100`, `"completionTokens":20`, `"totalTokens":120`, `"cachedTokens":25`, `"cost":0.0001`} {
		if !strings.Contains(text, want) {
			t.Errorf("usage summary missing %s: %s", want, text)
		}
	}
	if strings.Contains(text, "vendor_extension") || strings.Contains(text, "private") {
		t.Fatalf("usage summary retained provider extension: %s", text)
	}
}
