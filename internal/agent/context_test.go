package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestContextBudgetValidationAndExplicitNullRoundTrip(t *testing.T) {
	for _, raw := range []string{
		`{"inputTokens":0}`,
		`{"inputTokens":-1}`,
		`{"inputTokens":1.5}`,
		`{"inputTokens":9007199254740992}`,
		`{"inputTokens":10,"extra":true}`,
		`{}`,
	} {
		var budget ContextBudget
		if err := json.Unmarshal([]byte(raw), &budget); err == nil {
			t.Errorf("invalid context budget %s was accepted", raw)
		}
	}
	var budget ContextBudget
	if err := json.Unmarshal([]byte(`{"inputTokens":9007199254740991}`), &budget); err != nil || budget.InputTokens != maxSafeContextBudget {
		t.Fatalf("maximum safe context budget = %+v, err=%v", budget, err)
	}

	var config Config
	if err := json.Unmarshal([]byte(`{"contextBudget":null}`), &config); err != nil {
		t.Fatal(err)
	}
	if !config.contextBudgetPresent || config.ContextBudget != nil {
		t.Fatalf("explicit null was not retained: %+v", config)
	}
	encoded, err := json.Marshal(taskConfig{Config: config, MaxSteps: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"contextBudget":null`) || !strings.Contains(string(encoded), `"maxSteps":3`) {
		t.Fatalf("task config lost null or maxSteps: %s", encoded)
	}
	var decoded taskConfig
	if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.Config.contextBudgetPresent || decoded.MaxSteps != 3 {
		t.Fatalf("task config round trip = %+v, err=%v", decoded, err)
	}
}

func TestContextSnapshotKeepsUnknownsAndCachedUsageSeparate(t *testing.T) {
	state := contextRunState{}
	unknown := state.snapshot("run-unknown", time.Unix(1, 0))
	encoded, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"inputTokens":null`, `"tokens":null`, `"source":null`, `"previous":null`, `"estimate":null`, `"remainingTokens":null`, `"usedPercent":null`} {
		if !strings.Contains(string(encoded), field) {
			t.Errorf("unknown snapshot omitted %s: %s", field, encoded)
		}
	}

	budget, window := int64(100), int64(256)
	prompt, cached, estimate := int64(40), int64(80), int64(110)
	state.budget = &ContextBudget{InputTokens: budget}
	state.modelWindow = &window
	state.previous = &contextPrevious{RequestID: "request-1", Status: "completed", PromptTokens: &prompt, CachedTokens: &cached}
	state.estimate = &contextEstimate{Tokens: estimate, Method: "json_utf8_bytes/4", Stage: "preparing", ModelStepID: "step-1"}
	snapshot := state.snapshot("run-1", time.Unix(2, 0))
	if snapshot.RemainingTokens == nil || *snapshot.RemainingTokens != -10 || snapshot.UsedPercent == nil || *snapshot.UsedPercent != 110 ||
		snapshot.Previous.PromptTokens == nil || *snapshot.Previous.PromptTokens != 40 || snapshot.Previous.CachedTokens == nil || *snapshot.Previous.CachedTokens != 80 {
		t.Fatalf("snapshot inferred/re-wrote provider measurements: %+v", snapshot)
	}
}

func TestContextMessageReportsComputedBudgetAndSeparateExplicitSetting(t *testing.T) {
	for _, test := range []struct {
		name     string
		explicit *int64
		total    int64
		want     []string
	}{
		{name: "fallback", total: 1000000, want: []string{"Input budget: 1000000 tokens.", "Explicit input budget: not set.", "Model context window: unknown.", "Remaining input budget: 999150 tokens.", "Estimated budget use: 0.1%."}},
		{name: "computed cap", explicit: new(int64(1000)), total: 900, want: []string{"Input budget: 900 tokens.", "Explicit input budget: 1000 tokens.", "Remaining input budget: 50 tokens.", "Estimated budget use: 94.4%."}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := contextSnapshot{
				Budget:           contextBudgetSnapshot{InputTokens: test.explicit},
				Estimate:         &contextEstimate{Tokens: 850, Stage: "sent"},
				ExecutionContext: &executionContextSnapshot{Policy: "auto", TotalInputBudgetTokens: test.total, BudgetTokens: test.total - 100, BudgetSource: "fallback"},
			}
			text := contextMessage(snapshot, nil)
			for _, want := range test.want {
				if !strings.Contains(text, want) {
					t.Fatalf("notification missing %q: %s", want, text)
				}
			}
		})
	}
}

func TestContextPrepareAndActualRequestMeasurementWithoutObservation(t *testing.T) {
	var sent provider.Request
	p := &fixtureProvider{gen: func(_ context.Context, request provider.Request) (provider.Response, error) {
		sent = request
		if request.Observe != nil {
			t.Error("context tracking enabled provider streaming without an observer")
		}
		return provider.Response{
			Message:      json.RawMessage(`{"role":"assistant","content":"answer"}`),
			FinishReason: "stop",
			Usage:        json.RawMessage(`{"prompt_tokens":40,"prompt_tokens_details":{"cached_tokens":12},"completion_tokens":4,"total_tokens":44}`),
		}, nil
	}}
	h := NewHost(Options{Models: modelsWithChat(p), Observe: nil})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	start, err := op.Call("start", `{"provider":"openrouter","model":"fixture/model","name":"context-test","maxTokens":128,"effort":"auto","contextBudget":{"inputTokens":200}}`)
	if err != nil {
		t.Fatal(err)
	}
	var started struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal([]byte(start), &started); err != nil || started.RunID == "" {
		t.Fatalf("start response=%s err=%v", start, err)
	}
	runID := started.RunID
	messages := []json.RawMessage{json.RawMessage(`{"role":"system","content":"immutable system"}`), json.RawMessage(`{"role":"user","content":"original task"}`)}
	var step beginModelStepResponse
	stepRaw, err := op.Call("begin_model_step", mustJSON(t, beginModelStepRequest{RunID: runID, Messages: messages}))
	if err != nil || json.Unmarshal([]byte(stepRaw), &step) != nil || step.ModelStepID == "" {
		t.Fatalf("begin step response=%s err=%v", stepRaw, err)
	}
	prepareRaw, err := op.Call("context_prepare", mustJSON(t, contextPrepareRequest{RunID: runID, ModelStepID: step.ModelStepID, Messages: messages}))
	if err != nil || !strings.Contains(prepareRaw, `"prepared":true`) {
		t.Fatalf("prepare response=%s err=%v", prepareRaw, err)
	}
	contextRaw, err := op.Call("context_get", mustJSON(t, contextAccessRequest{RunID: runID, ModelStepID: step.ModelStepID}))
	if err != nil {
		t.Fatal(err)
	}
	var prepared contextGetResponse
	if err := json.Unmarshal([]byte(contextRaw), &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Context.Estimate == nil || prepared.Context.Estimate.Tokens <= 0 || !strings.HasPrefix(prepared.Notification, "NOE_CONTEXT_STATUS\n") {
		t.Fatalf("prepared context missing estimate/notice: %+v", prepared)
	}
	notice, _ := json.Marshal(map[string]string{"role": "user", "content": prepared.Notification})
	messages = append(messages, notice)
	payload := modelRequest{RunID: runID, ModelStepID: step.ModelStepID, Model: "fixture/model", Messages: messages, MaxTokens: 128, Effort: "auto"}
	responseRaw, err := op.Call("model", mustJSON(t, payload))
	if err != nil {
		t.Fatal(err)
	}
	var response modelResponse
	if err := json.Unmarshal([]byte(responseRaw), &response); err != nil || response.RequestID == "" {
		t.Fatalf("model response=%s err=%v", responseRaw, err)
	}
	if sent.Messages[0] == nil || len(sent.Messages) != 3 {
		t.Fatalf("provider did not receive the ephemeral tail notice: %s", sent.Messages)
	}
	var final contextGetResponse
	contextRaw, err = op.Call("context_get", mustJSON(t, contextAccessRequest{RunID: runID, ModelStepID: step.ModelStepID}))
	if err != nil || json.Unmarshal([]byte(contextRaw), &final) != nil {
		t.Fatalf("final context=%s err=%v", contextRaw, err)
	}
	previous := final.Context.Previous
	if previous == nil || previous.RequestID != response.RequestID || previous.Status != "completed" || previous.PromptTokens == nil || *previous.PromptTokens != 40 || previous.CachedTokens == nil || *previous.CachedTokens != 12 {
		t.Fatalf("previous usage/request association = %+v", previous)
	}
	if final.Context.Estimate == nil || final.Context.Estimate.Stage != "sent" || final.Context.Estimate.RequestID == nil || *final.Context.Estimate.RequestID != response.RequestID {
		t.Fatalf("sent estimate was not bound to actual request: %+v", final.Context.Estimate)
	}
	if h.opts.Observe != nil || sent.Observe != nil {
		t.Fatal("context state unexpectedly depended on or enabled observation")
	}
}

func TestContextUpdatesOnlyAfterProviderRequest(t *testing.T) {
	for _, status := range []string{"completed", "failed", "cancelled", "invalid_response"} {
		t.Run(status, func(t *testing.T) {
			updates := make(chan events.Event, 8)
			var h *Host
			var runID string
			p := &fixtureProvider{gen: func(_ context.Context, request provider.Request) (provider.Response, error) {
				if len(updates) != 0 {
					t.Error("context notification emitted before the provider responded")
				}
				h.mu.Lock()
				sent := h.runs[runID].context.snapshot(runID, time.Now())
				h.mu.Unlock()
				if sent.Estimate == nil || sent.Estimate.Stage != "sent" || sent.Estimate.RequestID == nil || sent.Estimate.Tokens <= 0 {
					t.Errorf("live context was not updated at send time: %+v", sent)
				}
				switch status {
				case "failed":
					return provider.Response{}, errors.New("fixture error")
				case "cancelled":
					return provider.Response{}, context.Canceled
				case "invalid_response":
					return provider.Response{Message: json.RawMessage(`invalid`)}, nil
				}
				return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"answer"}`), FinishReason: "stop", Usage: json.RawMessage(`{"prompt_tokens":40}`)}, nil
			}}
			h = NewHost(Options{Models: modelsWithChat(p), Observe: func(event events.Event) {
				if event["type"] == "context.updated" {
					updates <- event
				}
			}})
			t.Cleanup(h.Close)
			op := h.NewOperation(t.Context())
			t.Cleanup(op.Close)
			runID = startFixtureRun(t, op)
			messages := []json.RawMessage{json.RawMessage(`{"role":"user","content":"task"}`)}
			var step beginModelStepResponse
			if err := json.Unmarshal([]byte(controlCall(t, op, "begin_model_step", beginModelStepRequest{RunID: runID, Messages: messages})), &step); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				controlCall(t, op, "context_prepare", contextPrepareRequest{RunID: runID, ModelStepID: step.ModelStepID, Messages: messages})
			}
			var prepared contextGetResponse
			if err := json.Unmarshal([]byte(controlCall(t, op, "context_get", contextAccessRequest{RunID: runID, ModelStepID: step.ModelStepID})), &prepared); err != nil {
				t.Fatal(err)
			}
			if len(updates) != 0 || prepared.Context.Estimate == nil || prepared.Context.Estimate.Stage != "preparing" || !strings.HasPrefix(prepared.Notification, "NOE_CONTEXT_STATUS\n") {
				t.Fatalf("preparation must update state and retain the model notice without emitting: %+v", prepared)
			}
			notice, _ := json.Marshal(map[string]string{"role": "user", "content": prepared.Notification})
			_, err := op.Call("model", mustJSON(t, modelRequest{RunID: runID, ModelStepID: step.ModelStepID, Model: "fixture/model", MaxTokens: 128, Effort: "auto", Messages: append(messages, notice)}))
			if (err != nil) != (status != "completed") {
				t.Fatalf("model result for %s: %v", status, err)
			}
			if len(updates) != 1 {
				t.Fatalf("context updates=%d, want one after the request", len(updates))
			}
			event := <-updates
			wantStatus := status
			if wantStatus == "invalid_response" {
				wantStatus = "failed"
			}
			snapshot := event["context"].(contextSnapshot)
			if event["status"] != wantStatus || snapshot.Previous == nil || snapshot.Previous.Status != wantStatus || snapshot.Estimate == nil || snapshot.Estimate.Stage != "sent" || snapshot.Previous.RequestID != event["requestId"] {
				t.Fatalf("terminal context update: %+v", event)
			}
			if status == "completed" && (snapshot.Previous.PromptTokens == nil || *snapshot.Previous.PromptTokens != 40) {
				t.Fatalf("terminal context lost measured usage: %+v", snapshot.Previous)
			}
		})
	}
}
