package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"noe/internal/events"
)

func TestLogicalStepKeepsSteerAndHookAccountingWithoutObservation(t *testing.T) {
	cwd := t.TempDir()
	modulePath := filepath.Join(cwd, "hook.mjs")
	if err := os.WriteFile(modulePath, []byte("export async function beforeModelRequest() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	providerFixture := &fixtureProvider{}
	h := NewHost(Options{Provider: providerFixture, Cwd: cwd, Observe: nil})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startControlRunWithHooks(t, op, []string{"./hook.mjs"})
	group := &executionGroup{id: "test-root-agent"}
	h.mu.Lock()
	run := h.runs[runID]
	run.group = group
	h.groups[group.id] = group
	h.mu.Unlock()

	messages := []json.RawMessage{json.RawMessage(`{"role":"user","content":"start"}`)}
	firstStep := beginLogicalStep(t, op, runID, messages)
	firstEvent := callLogicalHook(t, op, h, runID, firstStep, "./hook.mjs")
	if firstEvent["phase"] != "beforeModelRequest" || firstEvent["causedBy"] == nil || firstEvent["activityId"] == "" {
		t.Fatalf("hook activity did not reference its logical step: %+v", firstEvent)
	}
	controlCall(t, op, "steer_agent", steerRequest{RunID: runID, Instruction: "prepare with steer", CommandID: "step-steer"})
	step, err := json.Marshal(modelRequest{
		RunID: runID, ModelStepID: firstStep, Model: "fixture/model", Messages: messages,
		MaxTokens: 128, Effort: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	responseText, err := op.Call("model", string(step))
	if err != nil {
		t.Fatal(err)
	}
	var steerResponse modelResponse
	if err := json.Unmarshal([]byte(responseText), &steerResponse); err != nil || len(steerResponse.Steers) != 1 || providerFixture.callCount() != 0 {
		t.Fatalf("steer-only response=%+v provider calls=%d err=%v", steerResponse, providerFixture.callCount(), err)
	}
	controlCall(t, op, "apply_steers", applySteersRequest{RunID: runID, CommandIDs: []string{"step-steer"}})
	responseText, err = op.Call("model", string(step))
	if err != nil {
		t.Fatal(err)
	}
	var response modelResponse
	if err := json.Unmarshal([]byte(responseText), &response); err != nil || response.RequestID == "" || providerFixture.callCount() != 1 {
		t.Fatalf("provider response=%+v provider calls=%d err=%v", response, providerFixture.callCount(), err)
	}
	if group.counters.HookCalls != 1 || group.counters.AgentProviderCalls != 1 {
		t.Fatalf("no-observe counters = %+v", group.counters)
	}

	// A rejected final commit ends the candidate's logical step. The next step
	// receives a fresh ID while preserving maxSteps/provider-call accounting.
	controlCall(t, op, "steer_agent", steerRequest{RunID: runID, Instruction: "revise final", CommandID: "final-steer"})
	commitText := controlCall(t, op, "commit_final", commitFinalRequest{RunID: runID, Answer: "candidate"})
	var commit commitFinalResponse
	if err := json.Unmarshal([]byte(commitText), &commit); err != nil || commit.Committed || len(commit.Instructions) != 1 {
		t.Fatalf("final candidate was not returned for a fresh step: %+v err=%v", commit, err)
	}
	controlCall(t, op, "apply_steers", applySteersRequest{RunID: runID, CommandIDs: []string{"final-steer"}})
	secondStep := beginLogicalStep(t, op, runID, append(messages, json.RawMessage(`{"role":"assistant","content":"candidate"}`)))
	if firstStep == secondStep {
		t.Fatalf("new logical step reused ID %q", secondStep)
	}
	callLogicalHook(t, op, h, runID, secondStep, "./hook.mjs")
	if group.counters.HookCalls != 2 || group.counters.ProviderCalls != 1 {
		t.Fatalf("hook/provider counters after second preparation = %+v", group.counters)
	}
}

func startControlRunWithHooks(t *testing.T, op *Operation, hooks []string) string {
	t.Helper()
	text, err := op.Call("start", mustJSON(t, startRequest{
		Provider: "openrouter", Model: "fixture/model", Name: "logical-step-test",
		MaxTokens: 128, Effort: "auto", MaxSteps: 2, Hooks: hooks,
	}))
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal([]byte(text), &response); err != nil || response.RunID == "" {
		t.Fatalf("invalid start response %q: %v", text, err)
	}
	return response.RunID
}

func beginLogicalStep(t *testing.T, op *Operation, runID string, messages []json.RawMessage) string {
	t.Helper()
	text, err := op.Call("begin_model_step", mustJSON(t, beginModelStepRequest{RunID: runID, Messages: messages}))
	if err != nil {
		t.Fatal(err)
	}
	var response beginModelStepResponse
	if err := json.Unmarshal([]byte(text), &response); err != nil || response.ModelStepID == "" {
		t.Fatalf("invalid model step response %q: %v", text, err)
	}
	return response.ModelStepID
}

func callLogicalHook(t *testing.T, op *Operation, h *Host, runID, stepID, module string) events.Event {
	t.Helper()
	index := 0
	request := hookScopeRequest{
		RunID: runID, Phase: "beforeModelRequest", HookIndex: &index,
		Module: module, ModelStepID: stepID,
	}
	text, err := op.Call("begin_hook_scope", mustJSON(t, request))
	if err != nil {
		t.Fatal(err)
	}
	var response hookScopeResponse
	if err := json.Unmarshal([]byte(text), &response); err != nil || response.ScopeID == "" {
		t.Fatalf("invalid registered hook scope response %q: %v", text, err)
	}
	if response.Event["phase"] != request.Phase || response.Event["modelStepId"] != stepID {
		t.Fatalf("hook callback metadata mismatch: %+v", response.Event)
	}
	hookActivityID, _ := response.Event["activityId"].(string)
	parentCauses := causedByIDs(t, response.Event)
	if hookActivityID == "" || len(parentCauses) != 1 {
		t.Fatalf("hook callback lacks its invocation or parent activity: %+v", response.Event)
	}
	h.mu.Lock()
	var scope *Operation
	for operation := range h.operations {
		if operation.scope == response.ScopeID {
			scope = operation
			break
		}
	}
	if scope == nil || scope.hookInvocation == nil || len(scope.causeActivityIDs) != 1 || scope.causeActivityIDs[0] != hookActivityID {
		h.mu.Unlock()
		t.Fatalf("registered hook scope cause was not bound to the hook invocation: %+v", scope)
	}
	h.mu.Unlock()
	if _, err := op.Call("end_hook_scope", mustJSON(t, hookScopeRequest{ScopeID: response.ScopeID, Status: "completed"})); err != nil {
		t.Fatal(err)
	}
	return response.Event
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
