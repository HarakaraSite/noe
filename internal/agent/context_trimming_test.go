package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestContextTrimReceiptSteerOnlyAndActualAdmission(t *testing.T) {
	calls := 0
	h := NewHost(Options{Provider: &fixtureProvider{gen: func(_ context.Context, r provider.Request) (provider.Response, error) {
		calls++
		return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"done"}`), FinishReason: "stop"}, nil
	}}})
	t.Cleanup(h.Close)
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	start := controlCall(t, op, "start", map[string]any{"provider": "openrouter", "model": "fixture/model", "name": "trim-test", "effort": "auto", "contextBudget": map[string]int{"inputTokens": 400}})
	var started struct {
		RunID string `json:"runId"`
	}
	_ = json.Unmarshal([]byte(start), &started)
	runID := started.RunID
	old, _ := json.Marshal(map[string]string{"role": "assistant", "content": strings.Repeat("old", 2000)})
	messages := []json.RawMessage{json.RawMessage(`{"role":"system","content":"system"}`), json.RawMessage(`{"role":"user","content":"task"}`), old, json.RawMessage(`{"role":"assistant","content":"recent"}`)}
	history := &contextHistoryLayout{Revision: 4, Entries: []contextHistoryEntry{{"s", "protected", 1}, {"u", "protected", 1}, {"old", "block", 1}, {"new", "block", 1}}}
	stepText := controlCall(t, op, "begin_model_step", beginModelStepRequest{RunID: runID, Messages: messages})
	var step beginModelStepResponse
	_ = json.Unmarshal([]byte(stepText), &step)
	prep := contextPrepareRequest{RunID: runID, ModelStepID: step.ModelStepID, Messages: messages, History: history, Final: true}
	var response struct {
		Plan *contextTrimPlan `json:"plan"`
	}
	_ = json.Unmarshal([]byte(controlCall(t, op, "context_prepare", prep)), &response)
	if response.Plan == nil || len(response.Plan.RemoveIDs) != 1 || response.Plan.RemoveIDs[0] != "old" {
		t.Fatalf("plan=%+v", response.Plan)
	}
	model := modelRequest{RunID: runID, ModelStepID: step.ModelStepID, Model: "fixture/model", Messages: messages, Effort: "auto"}
	if _, err := op.Call("model", mustJSON(t, model)); err == nil {
		t.Fatal("provider admitted unacknowledged trim")
	}
	kept := append(append([]json.RawMessage{}, messages[:2]...), messages[3])
	history = &contextHistoryLayout{Revision: 5, Entries: []contextHistoryEntry{{"s", "protected", 1}, {"u", "protected", 1}, {"new", "block", 1}}}
	prep.Messages, prep.History, prep.AppliedPlanID = kept, history, response.Plan.ID
	bad := prep
	bad.AppliedPlanID = "wrong"
	if _, err := op.Call("context_prepare", mustJSON(t, bad)); err == nil {
		t.Fatal("stale receipt accepted")
	}
	controlCall(t, op, "context_prepare", prep)
	if _, err := op.Call("context_prepare", mustJSON(t, prep)); err == nil {
		t.Fatal("duplicate receipt accepted")
	}
	get := func() contextGetResponse {
		var value contextGetResponse
		_ = json.Unmarshal([]byte(controlCall(t, op, "context_get", contextAccessRequest{RunID: runID, ModelStepID: step.ModelStepID})), &value)
		return value
	}
	for range 2 {
		if !strings.Contains(get().Notification, "Context trimmed:") {
			t.Fatal("read consumed pending trim notice")
		}
	}
	controlCall(t, op, "steer_agent", steerRequest{RunID: runID, CommandID: "new-steer", Instruction: "extra direction"})
	model.Messages = kept
	steerText := controlCall(t, op, "model", model)
	var steered modelResponse
	_ = json.Unmarshal([]byte(steerText), &steered)
	if len(steered.Steers) != 1 || calls != 0 || !strings.Contains(get().Notification, "Context trimmed:") {
		t.Fatal("steer-only consumed budget/notice")
	}
	controlCall(t, op, "apply_steers", applySteersRequest{RunID: runID, CommandIDs: []string{"new-steer"}})
	kept = append(kept, json.RawMessage(`{"role":"user","content":"extra direction"}`))
	history.Entries = append(history.Entries, contextHistoryEntry{"steer", "protected", 1})
	history.Revision++
	prep.Messages, prep.History, prep.AppliedPlanID = kept, history, ""
	controlCall(t, op, "context_prepare", prep)
	notice := get().Notification
	tail, _ := json.Marshal(map[string]string{"role": "system", "content": notice})
	model.Messages = append(kept, tail)
	controlCall(t, op, "model", model)
	if calls != 1 || strings.Contains(get().Notification, "Context trimmed:") {
		t.Fatal("actual provider admission failed to consume pending notice")
	}
	final := get().Context.ExecutionContext
	if final.TrimCount != 1 || final.LastTrim == nil || final.LastTrim.ID != response.Plan.ID {
		t.Fatalf("last trim fact lost %+v", final)
	}
}

func TestContextTrimCancellationBeforeAndAfterCandidateCommit(t *testing.T) {
	for _, before := range []bool{true, false} {
		t.Run(map[bool]string{true: "before_commit", false: "after_commit"}[before], func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			trimmed, calls := 0, 0
			h := NewHost(Options{Provider: &fixtureProvider{gen: func(context.Context, provider.Request) (provider.Response, error) {
				calls++
				return provider.Response{}, nil
			}}, Observe: func(e events.Event) {
				if e["type"] == "context.trimmed" {
					trimmed++
				}
			}})
			defer h.Close()
			op := h.NewOperation(ctx)
			defer op.Close()
			start := controlCall(t, op, "start", map[string]any{"provider": "openrouter", "model": "fixture/model", "name": "cancel-trim", "effort": "auto", "contextBudget": map[string]int{"inputTokens": 400}})
			var started struct {
				RunID string `json:"runId"`
			}
			_ = json.Unmarshal([]byte(start), &started)
			old, _ := json.Marshal(map[string]string{"role": "assistant", "content": strings.Repeat("old", 2000)})
			messages := []json.RawMessage{json.RawMessage(`{"role":"system","content":"system"}`), json.RawMessage(`{"role":"user","content":"task"}`), old, json.RawMessage(`{"role":"assistant","content":"recent"}`)}
			layout := &contextHistoryLayout{Revision: 4, Entries: []contextHistoryEntry{{"s", "protected", 1}, {"u", "protected", 1}, {"old", "block", 1}, {"new", "block", 1}}}
			var step beginModelStepResponse
			_ = json.Unmarshal([]byte(controlCall(t, op, "begin_model_step", beginModelStepRequest{RunID: started.RunID, Messages: messages})), &step)
			prep := contextPrepareRequest{RunID: started.RunID, ModelStepID: step.ModelStepID, Messages: messages, History: layout, Final: true}
			var result struct {
				Plan *contextTrimPlan `json:"plan"`
			}
			_ = json.Unmarshal([]byte(controlCall(t, op, "context_prepare", prep)), &result)
			if result.Plan == nil {
				t.Fatal("missing deletion candidate")
			}
			prep.AppliedPlanID = result.Plan.ID
			prep.Messages = append(append([]json.RawMessage{}, messages[:2]...), messages[3])
			prep.History = &contextHistoryLayout{Revision: 5, Entries: []contextHistoryEntry{{"s", "protected", 1}, {"u", "protected", 1}, {"new", "block", 1}}}
			if before {
				cancel()
				if _, err := op.Call("context_prepare", mustJSON(t, prep)); err == nil {
					t.Fatal("cancelled candidate committed")
				}
			} else {
				controlCall(t, op, "context_prepare", prep)
				cancel()
			}
			expected := 1
			if before {
				expected = 0
			}
			h.mu.Lock()
			count := h.runs[started.RunID].context.trimCount
			h.mu.Unlock()
			if trimmed != expected || count != expected || calls != 0 {
				t.Fatalf("trim events=%d count=%d providerCalls=%d", trimmed, count, calls)
			}
		})
	}
}

func TestContextRetentionRatiosKeepDifferentRecentSuffixes(t *testing.T) {
	messages := []json.RawMessage{json.RawMessage(`{"role":"system","content":"system"}`), json.RawMessage(`{"role":"user","content":"task"}`)}
	layout := &contextHistoryLayout{Revision: 22, Entries: []contextHistoryEntry{{"s", "protected", 1}, {"u", "protected", 1}}}
	for i := range 20 {
		raw, _ := json.Marshal(map[string]string{"role": "assistant", "content": fmt.Sprintf("block-%d-", i) + strings.Repeat("x", 600)})
		messages = append(messages, raw)
		layout.Entries = append(layout.Entries, contextHistoryEntry{fmt.Sprintf("b%d", i), "block", 1})
	}
	removed := []int{}
	for _, ratio := range []float64{0.1, 0.2} {
		state := contextRunState{budget: &ContextBudget{InputTokens: 1000}, policy: &ContextPolicy{RetainRatio: &ratio}}
		state.fixedTokens, state.executionTokens, _, _ = historyParts(messages, nil, layout)
		_, _, err := prepareContextEstimate(&state, "r", messages, nil)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := state.planTrim(messages, nil, layout)
		if err != nil || plan == nil {
			t.Fatalf("plan=%+v err=%v", plan, err)
		}
		removed = append(removed, len(plan.RemoveIDs))
		if plan.RemoveIDs[0] != "b0" || plan.RemoveIDs[len(plan.RemoveIDs)-1] != fmt.Sprintf("b%d", len(plan.RemoveIDs)-1) {
			t.Fatal("deletion is not a complete oldest prefix")
		}
	}
	if removed[0] <= removed[1] || removed[1] <= 0 || removed[0] >= 20 {
		t.Fatalf("ratios did not produce different protected suffixes: %v", removed)
	}
}
