package agent

import (
	"context"
	"encoding/json"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestToolResultObservationKeepsRawAndEffectiveExecutionResult(t *testing.T) {
	fixture := &fixtureProvider{gen: func(context.Context, provider.Request) (provider.Response, error) {
		return provider.Response{
			Message: json.RawMessage(`{"role":"assistant","content":null,"tool_calls":[
{"id":"replaced","type":"function","function":{"name":"run_js","arguments":"{}"}},
{"id":"unchanged","type":"function","function":{"name":"run_js","arguments":"{}"}},
{"id":"null","type":"function","function":{"name":"run_js","arguments":"{}"}}]}`),
			FinishReason: "tool_calls",
		}, nil
	}}
	observed := make(chan events.Event, 64)
	h := NewHost(Options{Models: modelsWithChat(fixture), Observe: func(event events.Event) {
		if event["type"] == "tool.finished" {
			value := event["result"].(json.RawMessage)
			event["result"] = cloneRawJSON(value)
			// A consumer may edit its observation; it must not alter the RPC result.
			copy(value, []byte(`{"summary":99}`))
		}
		observed <- event
	}})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startControlRunWithHooks(t, op, nil)
	stepID := beginLogicalStep(t, op, runID, []json.RawMessage{json.RawMessage(`{"role":"user","content":"start"}`)})
	_ = takeCausalityEvent(t, observed, "model.step.started")
	request := modelRequest{
		RunID: runID, ModelStepID: stepID, Model: "fixture/model",
		Messages:  []json.RawMessage{json.RawMessage(`{"role":"user","content":"start"}`)},
		Tools:     []json.RawMessage{json.RawMessage(`{"type":"function","function":{"name":"run_js"}}`)},
		MaxTokens: 128, Effort: "auto",
	}
	lifecycleCall(t, op, "model", request)
	_ = takeCausalityEvent(t, observed, "model.started")
	_ = takeCausalityEvent(t, observed, "model.finished")
	const raw = `{"data":{"answer":42},"diagnostics":[{"code":"cleanup_error","stage":"cleanup","message":"close failed"}]}`
	base := toolResultRequest{
		RunID: runID, ModelStepID: stepID, ToolName: "run_js",
		RawArguments:         json.RawMessage(`{"code":"export default {answer:42}"}`),
		Arguments:            json.RawMessage(`{"code":"export default {answer:42}"}`),
		RawResult:            json.RawMessage(raw),
		ModelResultAvailable: boolPointer(true),
	}
	for _, invalid := range []struct {
		result    json.RawMessage
		useRaw    bool
		available bool
	}{
		{nil, false, true},
		{json.RawMessage("null"), true, true},
		{nil, true, false},
	} {
		report := base
		report.ToolCallID = "unchanged"
		report.Result, report.UseRawResult = invalid.result, invalid.useRaw
		report.ModelResultAvailable = boolPointer(invalid.available)
		encoded, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := op.Call("tool_result", string(encoded)); err == nil {
			t.Fatalf("accepted invalid result selection: %s", encoded)
		}
	}
	for _, check := range []struct {
		id, want string
		result   json.RawMessage
		useRaw   bool
	}{
		{"replaced", `{"summary":42}`, json.RawMessage(`{"summary":42}`), false},
		{"unchanged", raw, nil, true},
		{"null", "null", json.RawMessage("null"), false},
	} {
		report := base
		report.ToolCallID = check.id
		report.Result, report.UseRawResult = check.result, check.useRaw
		response := lifecycleCall(t, op, "tool_result", report)
		var reply struct {
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal([]byte(response), &reply); err != nil || string(reply.Result) != check.want {
			t.Fatalf("observer changed model-facing result: %s err=%v", response, err)
		}
		event := takeCausalityEvent(t, observed, "tool.finished")
		if string(event["rawResult"].(json.RawMessage)) != raw ||
			string(event["result"].(json.RawMessage)) != check.want {
			t.Fatalf("tool.finished did not preserve raw/effective results: %+v", event)
		}
	}
}

func boolPointer(value bool) *bool { return &value }
