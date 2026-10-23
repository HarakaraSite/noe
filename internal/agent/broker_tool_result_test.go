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
			Message:      json.RawMessage(`{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"run_js","arguments":"{}"}}]}`),
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
	response := lifecycleCall(t, op, "tool_result", toolResultRequest{
		RunID: runID, ModelStepID: stepID, ToolCallID: "call-1", ToolName: "run_js",
		RawArguments:         json.RawMessage(`{"code":"export default {answer:42}"}`),
		Arguments:            json.RawMessage(`{"code":"export default {answer:42}"}`),
		RawResult:            json.RawMessage(`{"data":{"answer":42},"diagnostics":[{"code":"cleanup_error","stage":"cleanup","message":"close failed"}]}`),
		Result:               json.RawMessage(`{"summary":42}`),
		ModelResultAvailable: boolPointer(true),
	})
	var reply struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(response), &reply); err != nil || string(reply.Result) != `{"summary":42}` {
		t.Fatalf("observer changed model-facing result: %s err=%v", response, err)
	}
	event := takeCausalityEvent(t, observed, "tool.finished")
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["rawResult"]) != `{"data":{"answer":42},"diagnostics":[{"code":"cleanup_error","stage":"cleanup","message":"close failed"}]}` ||
		string(fields["result"]) != `{"summary":42}` {
		t.Fatalf("tool.finished did not preserve raw/effective results: %s", encoded)
	}
}

func boolPointer(value bool) *bool { return &value }
