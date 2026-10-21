package agent

import (
	"context"
	"encoding/json"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestHookWorkAndSteersUseInvocationAsTheirCause(t *testing.T) {
	observed := make(chan events.Event, 128)
	h := NewHost(Options{
		Models:  modelsWithChat(&fixtureProvider{}),
		Observe: func(event events.Event) { observed <- event },
	})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startControlRunWithHooks(t, op, []string{"./hook.mjs"})
	stepID := beginLogicalStep(t, op, runID, []json.RawMessage{json.RawMessage(`{"role":"user","content":"start"}`)})
	stepEvent := takeCausalityEvent(t, observed, "model.step.started")

	response, hookOp := beginCausalityHook(t, h, op, runID, stepID, "./hook.mjs")
	hookActivityID := activityIDFromEvent(t, response.Event)
	if got := causedByIDs(t, response.Event); !containsActivityID(got, activityIDFromEvent(t, stepEvent)) {
		t.Fatalf("hook invocation cause=%v, want model step activity %q", got, stepEvent["activityId"])
	}

	childID := spawnLifecycle(t, hookOp, nil, true)
	lifecycleCall(t, hookOp, "queue_agent", queueRequest{AgentID: childID, Instruction: "queued from hook", CommandID: "hook-queue"})
	h.mu.Lock()
	child := h.instances[childID]
	queued := child.commands["hook-queue"]
	childCauses := append([]string(nil), child.causedBy...)
	childOrigin := child.origin
	queuedCauses := append([]string(nil), queued.causedBy...)
	queuedOrigin := queued.origin
	h.mu.Unlock()
	if len(childCauses) != 1 || childCauses[0] != hookActivityID || childOrigin.Kind != "hook" {
		t.Fatalf("hook-spawned child activity cause/origin = %v / %+v, want invocation %q / hook", childCauses, childOrigin, hookActivityID)
	}
	if len(queuedCauses) != 1 || queuedCauses[0] != hookActivityID || queuedOrigin.Kind != "hook" {
		t.Fatalf("hook-queued task activity cause/origin = %v / %+v, want invocation %q / hook", queuedCauses, queuedOrigin, hookActivityID)
	}

	commandEvent := takeCausalityEvent(t, observed, "command.accepted")
	if got := causedByIDs(t, commandEvent); !containsActivityID(got, hookActivityID) {
		t.Fatalf("steer event cause=%v, want hook invocation %q", got, hookActivityID)
	}
	commandOrigin, _ := commandEvent["origin"].(activityOrigin)
	if commandEvent["activityId"] == "" || commandOrigin.Kind != "hook" {
		t.Fatalf("steer event lacks its stable activity and sender origin: %+v", commandEvent)
	}
	endCausalityHook(t, op, response.ScopeID)
}

func TestSteerAndRejectedCandidateCausalityReachNextRequest(t *testing.T) {
	observed := make(chan events.Event, 256)
	fixture := &fixtureProvider{}
	h := NewHost(Options{
		Models:  modelsWithChat(fixture),
		Observe: func(event events.Event) { observed <- event },
	})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startControlRunWithHooks(t, op, []string{"./hook.mjs"})
	step1 := beginLogicalStep(t, op, runID, []json.RawMessage{json.RawMessage(`{"role":"user","content":"start"}`)})
	step1Event := takeCausalityEvent(t, observed, "model.step.started")
	step1Activity := activityIDFromEvent(t, step1Event)

	hookResponse, hookOp := beginCausalityHook(t, h, op, runID, step1, "./hook.mjs")
	hookActivity := activityIDFromEvent(t, hookResponse.Event)
	steerText := lifecycleCall(t, hookOp, "steer_agent", steerRequest{RunID: runID, Instruction: "revise before requesting", CommandID: "hook-steer"})
	var accepted map[string]string
	if err := json.Unmarshal([]byte(steerText), &accepted); err != nil || accepted["commandId"] != "hook-steer" {
		t.Fatalf("hook steer receipt %q: %v", steerText, err)
	}
	steerAccepted := takeCausalityEvent(t, observed, "command.accepted")
	steerActivity := activityIDFromEvent(t, steerAccepted)
	if got := causedByIDs(t, steerAccepted); len(got) != 1 || got[0] != hookActivity {
		t.Fatalf("hook steer event causedBy=%v, want invocation %q", got, hookActivity)
	}
	steerOrigin, _ := steerAccepted["origin"].(activityOrigin)
	if steerOrigin.Kind != "hook" {
		t.Fatalf("hook steer origin = %+v", steerOrigin)
	}
	endCausalityHook(t, op, hookResponse.ScopeID)

	request := modelRequest{
		RunID: runID, ModelStepID: step1, Model: "fixture/model",
		Messages:  []json.RawMessage{json.RawMessage(`{"role":"user","content":"start"}`)},
		MaxTokens: 128, Effort: "auto",
	}
	firstText := lifecycleCall(t, op, "model", request)
	var steerOnly modelResponse
	if err := json.Unmarshal([]byte(firstText), &steerOnly); err != nil || len(steerOnly.Steers) != 1 || fixture.callCount() != 0 {
		t.Fatalf("model did not deliver the self-steer before provider call: %+v, calls=%d, err=%v", steerOnly, fixture.callCount(), err)
	}
	lifecycleCall(t, op, "apply_steers", applySteersRequest{RunID: runID, CommandIDs: []string{"hook-steer"}})
	_ = takeCausalityEvent(t, observed, "command.applied")
	responseText := lifecycleCall(t, op, "model", request)
	var response modelResponse
	if err := json.Unmarshal([]byte(responseText), &response); err != nil || response.RequestID == "" {
		t.Fatalf("provider model response %q: %v", responseText, err)
	}
	modelStarted := takeCausalityEvent(t, observed, "model.started")
	requestCauses := causedByIDs(t, modelStarted)
	for _, want := range []string{step1Activity, hookActivity, steerActivity} {
		if !containsActivityID(requestCauses, want) {
			t.Errorf("actual model request causedBy=%v, missing %q", requestCauses, want)
		}
	}
	responseEvent := takeCausalityEvent(t, observed, "model.finished")
	responseActivity := activityIDFromEvent(t, responseEvent)

	lifecycleCall(t, op, "steer_agent", steerRequest{RunID: runID, Instruction: "revise final candidate", CommandID: "final-steer"})
	finalSteerAccepted := takeCausalityEvent(t, observed, "command.accepted")
	finalSteerActivity := activityIDFromEvent(t, finalSteerAccepted)
	commitText := lifecycleCall(t, op, "commit_final", commitFinalRequest{RunID: runID, Answer: "fixture answer"})
	var commit commitFinalResponse
	if err := json.Unmarshal([]byte(commitText), &commit); err != nil || commit.Committed || len(commit.Instructions) != 1 {
		t.Fatalf("final candidate was not rejected for steering: %+v, err=%v", commit, err)
	}
	lifecycleCall(t, op, "apply_steers", applySteersRequest{RunID: runID, CommandIDs: []string{"final-steer"}})
	_ = takeCausalityEvent(t, observed, "command.applied")
	step2 := beginLogicalStep(t, op, runID, []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"start"}`),
		json.RawMessage(`{"role":"assistant","content":"fixture answer"}`),
	})
	if step1 == step2 {
		t.Fatalf("steering after final rejection reused logical step %q", step1)
	}
	step2Event := takeCausalityEvent(t, observed, "model.step.started")
	step2Causes := causedByIDs(t, step2Event)
	for _, want := range []string{responseActivity, finalSteerActivity} {
		if !containsActivityID(step2Causes, want) {
			t.Errorf("next model step causedBy=%v, missing prior candidate/steer %q", step2Causes, want)
		}
	}
	step2Activity := activityIDFromEvent(t, step2Event)
	step2Request := request
	step2Request.ModelStepID = step2
	step2Request.Messages = append(step2Request.Messages, json.RawMessage(`{"role":"user","content":"revise final candidate"}`))
	lifecycleCall(t, op, "model", step2Request)
	step2Started := takeCausalityEvent(t, observed, "model.started")
	step2RequestCauses := causedByIDs(t, step2Started)
	for _, want := range []string{step2Activity, finalSteerActivity} {
		if !containsActivityID(step2RequestCauses, want) {
			t.Errorf("steered next request causedBy=%v, missing %q", step2RequestCauses, want)
		}
	}
}

func TestNextLogicalStepJoinsParallelCompletedToolActivities(t *testing.T) {
	fixture := &fixtureProvider{gen: func(context.Context, provider.Request) (provider.Response, error) {
		return provider.Response{
			Message:      json.RawMessage(`{"role":"assistant","content":null,"tool_calls":[{"id":"call-fast","type":"function","function":{"name":"run_js","arguments":"{}"}},{"id":"call-slow","type":"function","function":{"name":"run_js","arguments":"{}"}}]}`),
			FinishReason: "tool_calls",
			Usage:        json.RawMessage(`{"prompt_tokens":1,"completion_tokens":1}`),
		}, nil
	}}
	observed := make(chan events.Event, 128)
	h := NewHost(Options{Models: modelsWithChat(fixture), Observe: func(event events.Event) { observed <- event }})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startControlRunWithHooks(t, op, nil)
	step1 := beginLogicalStep(t, op, runID, []json.RawMessage{json.RawMessage(`{"role":"user","content":"start"}`)})
	_ = takeCausalityEvent(t, observed, "model.step.started")
	request := modelRequest{
		RunID: runID, ModelStepID: step1, Model: "fixture/model",
		Messages:  []json.RawMessage{json.RawMessage(`{"role":"user","content":"start"}`)},
		Tools:     []json.RawMessage{json.RawMessage(`{"type":"function","function":{"name":"run_js"}}`)},
		MaxTokens: 128, Effort: "auto",
	}
	lifecycleCall(t, op, "model", request)
	_ = takeCausalityEvent(t, observed, "model.started")
	responseEvent := takeCausalityEvent(t, observed, "model.finished")
	responseActivity := activityIDFromEvent(t, responseEvent)

	for _, id := range []string{"call-fast", "call-slow"} {
		lifecycleCall(t, op, "tool_result", toolResultRequest{
			RunID: runID, ModelStepID: step1, ToolCallID: id, ToolName: "run_js",
			RawArguments: json.RawMessage(`{}`), Arguments: json.RawMessage(`{}`),
			RawResult: json.RawMessage(`{"exitCode":0,"reason":"completed"}`), Result: json.RawMessage(`"done"`),
		})
	}
	toolFinished := []events.Event{
		takeCausalityEvent(t, observed, "tool.finished"),
		takeCausalityEvent(t, observed, "tool.finished"),
	}
	toolActivities := []string{activityIDFromEvent(t, toolFinished[0]), activityIDFromEvent(t, toolFinished[1])}
	step2 := beginLogicalStep(t, op, runID, []json.RawMessage{
		json.RawMessage(`{"role":"user","content":"start"}`),
		json.RawMessage(`{"role":"assistant","content":null,"tool_calls":[]}`),
	})
	if step1 == step2 {
		t.Fatalf("tool continuation reused logical step %q", step1)
	}
	stepEvent := takeCausalityEvent(t, observed, "model.step.started")
	causes := causedByIDs(t, stepEvent)
	for _, want := range append([]string{responseActivity}, toolActivities...) {
		if !containsActivityID(causes, want) {
			t.Errorf("parallel tool continuation causedBy=%v, missing %q", causes, want)
		}
	}
}

func beginCausalityHook(t *testing.T, h *Host, owner *Operation, runID, stepID, module string) (hookScopeResponse, *Operation) {
	t.Helper()
	index := 0
	text := lifecycleCall(t, owner, "begin_hook_scope", hookScopeRequest{
		RunID: runID, Phase: "beforeModelRequest", HookIndex: &index,
		Module: module, ModelStepID: stepID,
	})
	var response hookScopeResponse
	if err := json.Unmarshal([]byte(text), &response); err != nil || response.ScopeID == "" {
		t.Fatalf("invalid hook scope response %q: %v", text, err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for operation := range h.operations {
		if operation.scope == response.ScopeID {
			return response, operation
		}
	}
	t.Fatalf("hook scope %q was not registered", response.ScopeID)
	return hookScopeResponse{}, nil
}

func endCausalityHook(t *testing.T, owner *Operation, scopeID string) {
	t.Helper()
	lifecycleCall(t, owner, "end_hook_scope", hookScopeRequest{ScopeID: scopeID, Status: "completed"})
}

func takeCausalityEvent(t *testing.T, observed <-chan events.Event, eventType string) events.Event {
	t.Helper()
	for len(observed) != 0 {
		event := <-observed
		if event["type"] == eventType {
			return event
		}
	}
	t.Fatalf("event %q was not observed", eventType)
	return nil
}

func activityIDFromEvent(t *testing.T, event events.Event) string {
	t.Helper()
	activityID, _ := event["activityId"].(string)
	if activityID == "" {
		t.Fatalf("event has no activity ID: %+v", event)
	}
	return activityID
}

func causedByIDs(t *testing.T, event events.Event) []string {
	t.Helper()
	switch causes := event["causedBy"].(type) {
	case []string:
		return causes
	case []any:
		ids := make([]string, 0, len(causes))
		for _, cause := range causes {
			if id, ok := cause.(string); ok {
				ids = append(ids, id)
			}
		}
		return ids
	default:
		return nil
	}
}

func containsActivityID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
