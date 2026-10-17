package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

func steerTaskConfig(maxSteps int, cacheKey string) taskConfig {
	return taskConfig{
		Config:   Config{Provider: "openrouter", Model: "fixture/model", Name: "steer-runtime", MaxTokens: 321, Effort: "high"},
		MaxSteps: maxSteps, CacheKey: &cacheKey,
	}
}

func spawnSteerTask(t *testing.T, op *Operation, config taskConfig) string {
	t.Helper()
	instruction := "initial task"
	response := lifecycleCall(t, op, "spawn_agent", spawnRequest{Config: config, Instruction: &instruction})
	var spawned instanceRequest
	if err := json.Unmarshal([]byte(response), &spawned); err != nil || spawned.AgentID == "" {
		t.Fatalf("invalid spawn response %q: %v", response, err)
	}
	return spawned.AgentID
}

func waitForFixtureFile(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for generated JS fixture gate %s: %v", path, ctx.Err())
		case <-ticker.C:
		}
	}
}

func waitFixtureRequest(t *testing.T, ctx context.Context, requests <-chan provider.Request) provider.Request {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-ctx.Done():
		t.Fatalf("waiting for fixture provider request: %v", ctx.Err())
		return provider.Request{}
	}
}

func assertSteeredRequestSettings(t *testing.T, request provider.Request, cacheKey string) {
	t.Helper()
	if request.CacheKey != cacheKey || request.MaxTokens != 321 || request.Effort != "high" || request.Model != "fixture/model" {
		t.Fatalf("steer changed model settings: %+v", request)
	}
}

func TestSteerDuringProviderRequestPreservesCandidateAndSettings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	started := make(chan provider.Request, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseProvider := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseProvider()
	var candidate = json.RawMessage(`{"role":"assistant","content":"candidate answer","reasoning_details":[{"type":"reasoning","text":"retain this"}],"vendor_metadata":{"source":"fixture"}}`)
	calls := 0
	p := &fixtureProvider{gen: func(callCtx context.Context, request provider.Request) (provider.Response, error) {
		calls++
		started <- request
		if calls == 1 {
			select {
			case <-release:
			case <-callCtx.Done():
				return provider.Response{}, callCtx.Err()
			}
			return provider.Response{Message: candidate, FinishReason: "stop"}, nil
		}
		return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"final answer"}`), FinishReason: "stop"}, nil
	}}
	observed := make(chan events.Event, 256)
	h := NewHost(Options{Provider: p, Executable: lifecycleBinary(t), Cwd: t.TempDir(), Observe: func(event events.Event) { observed <- event }})
	t.Cleanup(h.Close)
	op := h.NewOperation(ctx)
	t.Cleanup(op.Close)
	cacheKey := "steer-provider-cache"
	agentID := spawnSteerTask(t, op, steerTaskConfig(2, cacheKey))
	first := waitFixtureRequest(t, ctx, started)
	assertSteeredRequestSettings(t, first, cacheKey)
	snapshot := lifecycleSnapshot(t, op, agentID, false)
	if snapshot.CurrentRunID == "" {
		t.Fatalf("running task has no run ID: %+v", snapshot)
	}
	runID := snapshot.CurrentRunID
	for _, steer := range []steerRequest{
		{RunID: runID, Instruction: "first correction", CommandID: "provider-steer-1"},
		{RunID: runID, Instruction: "second correction", CommandID: "provider-steer-2"},
	} {
		controlCall(t, op, "steer_agent", steer)
	}
	releaseProvider()
	second := waitFixtureRequest(t, ctx, started)
	assertSteeredRequestSettings(t, second, cacheKey)
	if len(second.Messages) != 5 {
		t.Fatalf("message count after final-candidate steers = %d, want 5: %s", len(second.Messages), second.Messages)
	}
	if !jsonEqual(second.Messages[2], candidate) {
		t.Fatalf("full candidate assistant message was not preserved: %s", second.Messages[2])
	}
	var steerMessages []string
	for _, raw := range second.Messages[3:] {
		var message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(raw, &message); err != nil || message.Role != "user" {
			t.Fatalf("invalid steer history message %s: %v", raw, err)
		}
		steerMessages = append(steerMessages, message.Content)
	}
	if strings.Join(steerMessages, ",") != "first correction,second correction" {
		t.Fatalf("steer order = %v", steerMessages)
	}
	snapshot = lifecycleSnapshot(t, op, agentID, true)
	if snapshot.State != "stopped" || snapshot.LastResult == nil || snapshot.LastResult.State != "completed" || snapshot.LastResult.Answer != "final answer" {
		t.Fatalf("task result: %+v", snapshot)
	}
	if p.callCount() != 2 {
		t.Fatalf("actual provider calls=%d, want 2", p.callCount())
	}
	assertSteerLifecycleEvents(t, observed, runID, agentID, []string{"provider-steer-1", "provider-steer-2"}, 1)
}

func TestSteerDuringGeneratedJavaScriptPreservesToolOrderAndBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cwd := t.TempDir()
	toolStarted := filepath.Join(cwd, "tool-started")
	releaseTool := filepath.Join(cwd, "release-tool")
	defer func() { _ = os.WriteFile(releaseTool, []byte("release"), 0o600) }()
	toolCode := fmt.Sprintf(`// Wait for the test to release this generated task.
import {writeFileSync,existsSync} from "node:fs";
writeFileSync(%q,"started");
while (!existsSync(%q)) await new Promise(resolve=>setTimeout(resolve,5));
console.log("generated tool complete");`, toolStarted, releaseTool)
	arguments, err := json.Marshal(map[string]string{"code": toolCode})
	if err != nil {
		t.Fatal(err)
	}
	toolMessage, err := json.Marshal(map[string]any{
		"role": "assistant", "content": "checking",
		"reasoning_details": []map[string]string{{"type": "reasoning", "text": "tool reasoning"}},
		"vendor_metadata":   map[string]string{"source": "tool-fixture"},
		"tool_calls": []map[string]any{{
			"id": "tool-steer-1", "type": "function",
			"function": map[string]string{"name": "run_js", "arguments": string(arguments)},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan provider.Request, 4)
	calls := 0
	p := &fixtureProvider{gen: func(_ context.Context, request provider.Request) (provider.Response, error) {
		calls++
		started <- request
		if calls == 1 {
			return provider.Response{Message: toolMessage, FinishReason: "tool_calls"}, nil
		}
		return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"after tool"}`), FinishReason: "stop"}, nil
	}}
	observed := make(chan events.Event, 256)
	h := NewHost(Options{Provider: p, Executable: lifecycleBinary(t), Cwd: cwd, Observe: func(event events.Event) { observed <- event }})
	t.Cleanup(h.Close)
	op := h.NewOperation(ctx)
	t.Cleanup(op.Close)
	cacheKey := "steer-tool-cache"
	agentID := spawnSteerTask(t, op, steerTaskConfig(2, cacheKey))
	first := waitFixtureRequest(t, ctx, started)
	assertSteeredRequestSettings(t, first, cacheKey)
	waitForFixtureFile(t, ctx, toolStarted)
	snapshot := lifecycleSnapshot(t, op, agentID, false)
	if snapshot.CurrentRunID == "" {
		t.Fatalf("running task has no run ID: %+v", snapshot)
	}
	runID := snapshot.CurrentRunID
	controlCall(t, op, "steer_agent", steerRequest{RunID: runID, Instruction: "inspect the tool output", CommandID: "tool-steer-1"})
	if err := os.WriteFile(releaseTool, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := waitFixtureRequest(t, ctx, started)
	assertSteeredRequestSettings(t, second, cacheKey)
	if len(second.Messages) != 5 {
		t.Fatalf("message count after tool steer = %d, want 5: %s", len(second.Messages), second.Messages)
	}
	var assistant struct {
		ReasoningDetails []map[string]string `json:"reasoning_details"`
		VendorMetadata   map[string]string   `json:"vendor_metadata"`
	}
	if err := json.Unmarshal(second.Messages[2], &assistant); err != nil || assistant.ReasoningDetails[0]["text"] != "tool reasoning" || assistant.VendorMetadata["source"] != "tool-fixture" {
		t.Fatalf("full tool assistant message was not preserved: %s (%v)", second.Messages[2], err)
	}
	var toolResult struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(second.Messages[3], &toolResult); err != nil || toolResult.Role != "tool" || toolResult.ToolCallID != "tool-steer-1" || !strings.Contains(toolResult.Content, "generated tool complete") {
		t.Fatalf("generated tool result was not preserved before steer: %+v (%v)", toolResult, err)
	}
	var steer struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(second.Messages[4], &steer); err != nil || steer.Role != "user" || steer.Content != "inspect the tool output" {
		t.Fatalf("steer was not appended after tool result: %+v (%v)", steer, err)
	}
	snapshot = lifecycleSnapshot(t, op, agentID, true)
	if snapshot.LastResult == nil || snapshot.LastResult.State != "completed" || snapshot.LastResult.Answer != "after tool" {
		t.Fatalf("task result: %+v", snapshot)
	}
	if p.callCount() != 2 {
		t.Fatalf("synthetic steer response consumed maxSteps: actual calls=%d, want 2", p.callCount())
	}
	assertSteerLifecycleEvents(t, observed, runID, agentID, []string{"tool-steer-1"}, 1)
}

func jsonEqual(left, right []byte) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	leftJSON, leftErr := json.Marshal(leftValue)
	rightJSON, rightErr := json.Marshal(rightValue)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}

func assertSteerLifecycleEvents(t *testing.T, observed <-chan events.Event, runID, agentID string, commandIDs []string, finalCount int) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	want := make(map[string]bool, len(commandIDs))
	for _, id := range commandIDs {
		want[id] = true
	}
	seenAccepted := make(map[string]bool)
	seenApplied := make(map[string]bool)
	finals := 0
	for len(seenApplied) < len(commandIDs) || finals < finalCount {
		select {
		case event := <-observed:
			typ, _ := event["type"].(string)
			if typ == "command.accepted" || typ == "command.applied" {
				id, _ := event["commandId"].(string)
				if !want[id] {
					continue // Ignore the instance's own queued-task command events.
				}
				if event["runId"] != runID || event["agentId"] != agentID {
					t.Fatalf("misattributed steer lifecycle event: %#v", event)
				}
				if typ == "command.accepted" {
					seenAccepted[id] = true
				} else {
					if !seenAccepted[id] {
						t.Fatalf("command applied before acceptance: %#v", event)
					}
					seenApplied[id] = true
				}
			}
			if typ == "agent.final" {
				finals++
				if event["runId"] != runID {
					t.Fatalf("final event attributed to wrong run: %#v", event)
				}
			}
		case <-deadline.C:
			t.Fatalf("waiting for steer/final events accepted=%v applied=%v finals=%d", seenAccepted, seenApplied, finals)
		}
	}
	if len(seenAccepted) != len(commandIDs) || finals != finalCount {
		t.Fatalf("steer event counts accepted=%v applied=%v finals=%d", seenAccepted, seenApplied, finals)
	}
}
