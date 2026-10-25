package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestHookScopeRecordsOnlyRecoverableFailures(t *testing.T) {
	for _, test := range []struct {
		name         string
		requestError error
		cancelOwner  bool
		fatal        bool
	}{
		{name: "callback only"},
		{name: "API misuse", requestError: newOperationError(400, "request", "invalid argument")},
		{name: "unowned target", requestError: newOperationError(409, "scope", "invalid target")},
		{name: "service IO", requestError: errors.New("catalog file unavailable")},
		{name: "internal broker", requestError: newOperationError(500, "broker", "internal allocation failed"), fatal: true},
		{name: "encoding", requestError: &directResponseEncodingError{err: errors.New("encode failed")}, fatal: true},
		{name: "cancelled owner", cancelOwner: true, fatal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := NewHost(Options{Models: modelsWithChat(&fixtureProvider{})})
			t.Cleanup(h.Close)
			op := h.NewOperation(context.Background())
			t.Cleanup(op.Close)
			runID := startControlRunWithHooks(t, op, []string{"./hook.mjs"})
			stepID := beginLogicalStep(t, op, runID, []json.RawMessage{json.RawMessage(`{"role":"user","content":"start"}`)})
			response, child := beginCausalityHook(t, h, op, runID, stepID, "./hook.mjs")
			if test.requestError != nil {
				_, err := h.withDirectRequest(context.Background(), op, response.ScopeID, func(context.Context, *Operation) (any, error) { return nil, test.requestError })
				if err == nil {
					t.Fatal("fixture request unexpectedly succeeded")
				}
			}
			h.mu.Lock()
			run := h.runs[runID]
			if test.cancelOwner {
				run.cancel()
			}
			h.mu.Unlock()
			_, err := h.endHookScope(op, hookScopeRequest{ScopeID: response.ScopeID, Status: "failed", Error: "callback stack", Message: "callback failed"})
			if (err != nil) != test.fatal {
				t.Fatalf("close error=%v fatal=%v", err, test.fatal)
			}
			if test.fatal && err != nil && strings.Contains(err.Error(), "callback") {
				t.Fatalf("callback hid termination: %v", err)
			}
			h.mu.Lock()
			failures := cloneHookFailures(run.hookFailures)
			h.mu.Unlock()
			if test.fatal {
				if len(failures) != 0 {
					t.Fatalf("fatal error recorded as recovered: %+v", failures)
				}
			} else {
				if len(failures) != 1 || failures[0].InvocationID != response.Event["invocationId"] || failures[0].ModelStepID != stepID || failures[0].HookIndex != 0 || failures[0].Error != "callback failed" || failures[0].NotifiedRequestID != "" {
					t.Fatalf("failure record=%+v", failures)
				}
			}
			if _, err := h.endHookScope(op, hookScopeRequest{ScopeID: response.ScopeID, Status: "failed", Error: "retry"}); err == nil {
				t.Fatal("closed scope accepted")
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			if len(run.hookFailures) != len(failures) || child.hookInvocation.state == "active" || child.hookInvocation.state == "ending" {
				t.Fatalf("duplicate or unfinished scope: %+v", child.hookInvocation)
			}
		})
	}
}

func TestHookScopeJoinsRequestsBeforeRecoveringCallback(t *testing.T) {
	h := NewHost(Options{Models: modelsWithChat(&fixtureProvider{})})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startControlRunWithHooks(t, op, []string{"./hook.mjs"})
	stepID := beginLogicalStep(t, op, runID, []json.RawMessage{json.RawMessage(`{"role":"user","content":"start"}`)})
	response, _ := beginCausalityHook(t, h, op, runID, stepID, "./hook.mjs")
	started := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = h.withDirectRequest(context.Background(), op, response.ScopeID, func(ctx context.Context, _ *Operation) (any, error) {
			close(started)
			<-ctx.Done() // endHookScope cancels this unawaited request before joining it.
			return nil, newOperationError(500, "broker", "late internal failure")
		})
	}()
	<-started
	_, err := h.endHookScope(op, hookScopeRequest{ScopeID: response.ScopeID, Status: "failed", Error: "callback failed"})
	<-finished
	if err == nil || !strings.Contains(err.Error(), "late internal failure") {
		t.Fatalf("late failure was hidden: %v", err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.runs[runID].hookFailures) != 0 {
		t.Fatal("scope recovered before request join")
	}
}

func TestHookFailuresOutcomeCloneAndWireOmission(t *testing.T) {
	retained := TaskOutcome{Answer: "original", HookFailures: []HookFailure{{InvocationID: "invocation", Error: "failure"}}}
	projection := retained
	projection.omitAnswer = true
	cloned := cloneTaskOutcome(projection)
	cloned.HookFailures[0].Error = "changed"
	if retained.HookFailures[0].Error != "failure" || retained.Answer != "original" || cloned.Answer != "" || cloned.AnswerBytes == nil || *cloned.AnswerBytes != 8 {
		t.Fatalf("bad projection: retained=%+v clone=%+v", retained, cloned)
	}
	encoded, err := json.Marshal(projection)
	if err != nil || strings.Contains(string(encoded), "\"answer\":") || !strings.Contains(string(encoded), "\"hookFailures\":[") {
		t.Fatalf("projection wire=%s err=%v", encoded, err)
	}
	normal, err := json.Marshal(TaskOutcome{State: "completed"})
	if err != nil || strings.Contains(string(normal), "hookFailures") {
		t.Fatalf("normal wire changed: %s err=%v", normal, err)
	}
}
