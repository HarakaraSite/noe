package agent

import (
	"context"
	"encoding/json"
	"errors"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func TestHookNotificationsBindPreparedTextAndConsumeOnlyAdmittedBatch(t *testing.T) {
	calls := 0
	h := NewHost(Options{Models: modelsWithChat(&fixtureProvider{gen: func(_ context.Context, req provider.Request) (provider.Response, error) {
		calls++
		return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"done"}`), FinishReason: "stop"}, nil
	}})})
	t.Cleanup(h.Close)
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	runID := startControlRunWithHooks(t, op, []string{"./hook.mjs"})
	messages := []json.RawMessage{json.RawMessage(`{"role":"system","content":"system"}`), json.RawMessage(`{"role":"user","content":"task"}`)}
	stepID := beginLogicalStep(t, op, runID, messages)
	recoverFailure := func(message string) string {
		scope, _ := beginCausalityHook(t, h, op, runID, stepID, "./hook.mjs")
		controlCall(t, op, "end_hook_scope", hookScopeRequest{ScopeID: scope.ScopeID, Status: "failed", Message: message, Error: "diagnostic stack"})
		return scope.Event["invocationId"].(string)
	}
	get := func() contextGetResponse {
		var value contextGetResponse
		if err := json.Unmarshal([]byte(controlCall(t, op, "context_get", contextAccessRequest{RunID: runID, ModelStepID: stepID})), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	prepare := func() contextGetResponse {
		controlCall(t, op, "context_prepare", contextPrepareRequest{RunID: runID, ModelStepID: stepID, Messages: messages, Final: true})
		return get()
	}
	request := func(text string) modelRequest {
		tail, _ := json.Marshal(map[string]string{"role": "user", "content": text})
		return modelRequest{RunID: runID, ModelStepID: stepID, Model: "fixture/model", MaxTokens: 128, Effort: "auto", Messages: append(append([]json.RawMessage{}, messages...), tail)}
	}
	assertPending := func(want int) {
		t.Helper()
		h.mu.Lock()
		defer h.mu.Unlock()
		if got := len(pendingHookFailures(h.runs[runID])); got != want {
			t.Fatalf("pending=%d want %d", got, want)
		}
	}
	firstID := recoverFailure("first hook failed")
	first := prepare()
	again := prepare()
	if first.Notification != again.Notification || get().Notification != again.Notification || !strings.Contains(first.Notification, firstID) {
		t.Fatal("preparation/read lost or changed batch")
	}
	assertPending(1)
	controlCall(t, op, "steer_agent", steerRequest{RunID: runID, CommandID: "direction", Instruction: "new direction"})
	var steered modelResponse
	if err := json.Unmarshal([]byte(controlCall(t, op, "model", request(first.Notification))), &steered); err != nil || len(steered.Steers) != 1 || calls != 0 {
		t.Fatalf("steer-only=%+v calls=%d err=%v", steered, calls, err)
	}
	assertPending(1)
	controlCall(t, op, "apply_steers", applySteersRequest{RunID: runID, CommandIDs: []string{"direction"}})
	messages = append(messages, json.RawMessage(`{"role":"user","content":"new direction"}`))
	first = prepare()
	secondID := recoverFailure("late hook failed")
	if get().Notification != first.Notification || strings.Contains(first.Notification, secondID) {
		t.Fatal("late failure changed frozen prepared text")
	}
	assertPending(2)
	actual, _ := json.Marshal(request(first.Notification).Messages)
	tools, _ := json.Marshal(request(first.Notification).Tools)
	if first.Context.Estimate == nil || first.Context.Estimate.Tokens != estimateContextTokens(len(actual), len(tools)) {
		t.Fatalf("prepared estimate=%+v actual=%d", first.Context.Estimate, estimateContextTokens(len(actual), len(tools)))
	}
	if _, err := op.Call("model", mustJSON(t, request(first.Notification+"changed"))); err == nil {
		t.Fatal("modified status text was admitted")
	}
	assertPending(2)
	var sent modelResponse
	if err := json.Unmarshal([]byte(controlCall(t, op, "model", request(first.Notification))), &sent); err != nil {
		t.Fatal(err)
	}
	assertPending(1)
	h.mu.Lock()
	run := h.runs[runID]
	failures := cloneHookFailures(run.hookFailures)
	h.mu.Unlock()
	if failures[0].NotifiedRequestID != sent.RequestID || failures[1].NotifiedRequestID != "" {
		t.Fatalf("wrong admitted batch: %+v", failures)
	}
	if _, err := op.Call("model", mustJSON(t, request(first.Notification))); err == nil {
		t.Fatal("old status reused without preparing pending failures")
	}
	second := prepare()
	if !strings.Contains(second.Notification, secondID) || strings.Contains(second.Notification, firstID) {
		t.Fatalf("new notice repeated/omitted failures: %s", second.Notification)
	}
	if _, err := op.Call("model", mustJSON(t, request(first.Notification))); err == nil {
		t.Fatal("stale body consumed a newer batch")
	}
	assertPending(1)
	var sentSecond modelResponse
	if err := json.Unmarshal([]byte(controlCall(t, op, "model", request(second.Notification))), &sentSecond); err != nil {
		t.Fatal(err)
	}
	assertPending(0)
	h.mu.Lock()
	failures = cloneHookFailures(run.hookFailures)
	h.mu.Unlock()
	if failures[0].NotifiedRequestID != sent.RequestID || failures[1].NotifiedRequestID != sentSecond.RequestID {
		t.Fatalf("delivery changed: %+v", failures)
	}
	if calls != 2 || strings.Contains(get().Notification, "Hook processing failures") {
		t.Fatalf("calls=%d pending notification repeated", calls)
	}
}

func TestHookNotificationDeliveryTracksProviderAdmissionOnFailure(t *testing.T) {
	for _, mode := range []string{"cancel_before", "max_steps", "provider_failure", "cancel_after"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			var cancelRun context.CancelFunc
			h := NewHost(Options{Models: modelsWithChat(&fixtureProvider{gen: func(context.Context, provider.Request) (provider.Response, error) {
				calls++
				return provider.Response{}, errors.New("fixture provider failed")
			}}), Observe: func(e events.Event) {
				if e["type"] == "model.started" && mode == "cancel_after" {
					cancelRun()
				}
			}})
			t.Cleanup(h.Close)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			op := h.NewOperation(ctx)
			t.Cleanup(op.Close)
			runID := startControlRunWithHooks(t, op, []string{"./hook.mjs"})
			messages := []json.RawMessage{json.RawMessage(`{"role":"system","content":"system"}`), json.RawMessage(`{"role":"user","content":"task"}`)}
			stepID := beginLogicalStep(t, op, runID, messages)
			scope, _ := beginCausalityHook(t, h, op, runID, stepID, "./hook.mjs")
			controlCall(t, op, "end_hook_scope", hookScopeRequest{ScopeID: scope.ScopeID, Status: "failed", Error: "hook failed"})
			controlCall(t, op, "context_prepare", contextPrepareRequest{RunID: runID, ModelStepID: stepID, Messages: messages, Final: true})
			var prepared contextGetResponse
			if err := json.Unmarshal([]byte(controlCall(t, op, "context_get", contextAccessRequest{RunID: runID, ModelStepID: stepID})), &prepared); err != nil {
				t.Fatal(err)
			}
			tail, _ := json.Marshal(map[string]string{"role": "user", "content": prepared.Notification})
			h.mu.Lock()
			run := h.runs[runID]
			cancelRun = run.cancel
			if mode == "max_steps" {
				run.providerCalls = run.config.MaxSteps
			}
			h.mu.Unlock()
			if mode == "cancel_before" {
				cancel()
			}
			_, err := op.Call("model", mustJSON(t, modelRequest{RunID: runID, ModelStepID: stepID, Model: "fixture/model", MaxTokens: 128, Effort: "auto", Messages: append(messages, tail)}))
			if err == nil {
				t.Fatal("fixture model unexpectedly succeeded")
			}
			h.mu.Lock()
			failure := run.hookFailures[0]
			h.mu.Unlock()
			wantAdmitted := mode == "provider_failure" || mode == "cancel_after"
			if (failure.NotifiedRequestID != "") != wantAdmitted || (calls == 1) != wantAdmitted {
				t.Fatalf("admitted=%v calls=%d failure=%+v", wantAdmitted, calls, failure)
			}
		})
	}
}
