package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"noe/internal/agent/provider"
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
	h := NewHost(Options{Provider: p, Observe: nil})
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
	notice, _ := json.Marshal(map[string]string{"role": "system", "content": prepared.Notification})
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

func transcriptWithoutContextStatus(t testing.TB, messages []json.RawMessage) []json.RawMessage {
	t.Helper()
	if len(messages) == 0 {
		t.Fatal("provider request is missing the context status tail")
	}
	var tail struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(messages[len(messages)-1], &tail); err != nil || tail.Role != "system" || !strings.HasPrefix(tail.Content, "NOE_CONTEXT_STATUS\n") {
		t.Fatalf("provider request has no tagged context status tail: %s (%v)", messages[len(messages)-1], err)
	}
	return append([]json.RawMessage(nil), messages[:len(messages)-1]...)
}
