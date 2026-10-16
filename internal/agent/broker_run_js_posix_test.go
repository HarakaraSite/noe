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

	"noe/internal/agent/provider"
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
	if connection.Defaults != (Config{Provider: "openrouter", Model: "fixture/model", MaxTokens: 128}) {
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
		{Provider: "openrouter", Model: "fixture/left", Name: "left", MaxTokens: 37},
		{Provider: "openrouter", Model: "fixture/right", Name: "right", MaxTokens: 0},
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
		want := Config{Provider: config.Provider, Model: config.Model, MaxTokens: config.MaxTokens}
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
sleep 30 &
echo "$!" > "$NOE_RUNNER_CHILD_PID_FILE"
wait
`)
	var trace strings.Builder
	h := NewHost(Options{Provider: &fixtureProvider{}, Executable: executable, Trace: &trace})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startFixtureRun(t, op)
	code := "await new Promise(() => {});"
	payload, err := json.Marshal(runJSRequest{RunID: runID, Code: &code})
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
