package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func TestManagedModelUsesCommittedRevisionAndOwnsProviderBuffers(t *testing.T) {
	var received []json.RawMessage
	calls := 0
	h := NewHost(Options{Models: modelsWithChat(&fixtureProvider{gen: func(_ context.Context, request provider.Request) (provider.Response, error) {
		calls++
		for _, raw := range request.Messages {
			received = append(received, append(json.RawMessage(nil), raw...))
		}
		// A user-supplied model service must not be able to corrupt committed history.
		for _, raw := range request.Messages {
			clear(raw)
		}
		return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"done"}`), FinishReason: "stop"}, nil
	}})})
	t.Cleanup(h.Close)
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	instance, run, cancel := newConversationTestRun(t, h, op)
	defer cancel()
	opaque := json.RawMessage(`{"role":"assistant","content":"previous","reasoning_details":[{"type":"opaque","data":"keep"}],"native_item":{"id":"native-7"}}`)
	if err := instance.conversation.appendRaw("block", []json.RawMessage{opaque}); err != nil {
		t.Fatal(err)
	}
	revision := instance.conversation.Revision
	original := instance.conversation.messages()
	var step beginModelStepResponse
	if _, err := op.Call("begin_model_step", mustJSON(t, beginModelStepRequest{RunID: run.id, HistoryRevision: revision - 1})); err == nil {
		t.Fatal("stale begin was accepted")
	}
	foreign := h.NewOperation(t.Context())
	t.Cleanup(foreign.Close)
	if _, err := foreign.Call("begin_model_step", mustJSON(t, beginModelStepRequest{RunID: run.id, HistoryRevision: revision})); err == nil {
		t.Fatal("foreign revision reference was accepted")
	}
	if err := json.Unmarshal([]byte(controlCall(t, op, "begin_model_step", beginModelStepRequest{RunID: run.id, HistoryRevision: revision})), &step); err != nil {
		t.Fatal(err)
	}
	note := "NOE_CONTEXT_STATUS\nfixture"
	request := modelRequest{RunID: run.id, ModelStepID: step.ModelStepID, HistoryRevision: revision, Notification: &note, Model: run.config.Model, MaxTokens: run.config.MaxTokens, Effort: run.config.Effort}
	if _, err := op.Call("model", mustJSON(t, request)); err == nil {
		t.Fatal("unprepared model reference was accepted")
	}
	prepare := contextPrepareRequest{RunID: run.id, ModelStepID: step.ModelStepID, HistoryRevision: revision, Final: true}
	stalePrepare := prepare
	stalePrepare.HistoryRevision--
	if _, err := op.Call("context_prepare", mustJSON(t, stalePrepare)); err == nil {
		t.Fatal("stale prepare was accepted")
	}
	controlCall(t, op, "context_prepare", prepare)
	if _, err := op.Call("model", mustJSON(t, request)); err == nil {
		t.Fatal("unprepared notification text was accepted")
	}
	var prepared contextGetResponse
	if err := json.Unmarshal([]byte(controlCall(t, op, "context_get", contextAccessRequest{RunID: run.id, ModelStepID: step.ModelStepID})), &prepared); err != nil {
		t.Fatal(err)
	}
	note = prepared.Notification
	staleRequest := request
	staleRequest.HistoryRevision--
	if _, err := op.Call("model", mustJSON(t, staleRequest)); err == nil {
		t.Fatal("stale model reference was accepted")
	}
	ambiguous := request
	ambiguous.Messages = original
	if _, err := op.Call("model", mustJSON(t, ambiguous)); err == nil {
		t.Fatal("ambiguous model payload was accepted")
	}
	controlCall(t, op, "model", request)
	if calls != 1 || len(received) != len(original)+1 {
		t.Fatalf("provider calls=%d messages=%d", calls, len(received))
	}
	for index, raw := range original {
		if !bytes.Equal(received[index], raw) || !bytes.Equal(instance.conversation.messages()[index], raw) {
			t.Fatalf("committed message %d changed", index)
		}
	}
	var tail struct{ Role, Content string }
	if err := json.Unmarshal(received[len(received)-1], &tail); err != nil || tail.Role != "user" || tail.Content != note {
		t.Fatalf("notification=%s err=%v", received[len(received)-1], err)
	}
}
