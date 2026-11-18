package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
)

const queuedStartPayload = `{"provider":"fixture","model":"model","name":"managed","effort":"none"}`

func newQueuedStartTestOperation(t *testing.T, h *Host, prior *conversationSnapshot) (*Operation, *agentInstance) {
	t.Helper()
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	instance := &agentInstance{id: "queued-agent", ctx: op.ctx, state: "running", conversation: prior}
	op.task = &managedTask{instance: instance, command: &command{id: "queued-command", instruction: "next instruction"}}
	return op, instance
}

func TestQueuedStartSharesTranscriptAndReturnsIndependentJSON(t *testing.T) {
	h := NewHost(Options{Models: directToolModels{}})
	t.Cleanup(h.Close)
	prior, err := initialConversation("fixed system", "first instruction")
	if err != nil {
		t.Fatal(err)
	}
	assistant := json.RawMessage(`{"role":"assistant","content":"prior answer","native_item":{"id":"opaque-7"}}`)
	if err := prior.appendRaw("block", []json.RawMessage{assistant}); err != nil {
		t.Fatal(err)
	}
	op, instance := newQueuedStartTestOperation(t, h, prior)
	response, err := op.Call("start", queuedStartPayload)
	if err != nil {
		t.Fatal(err)
	}
	var started struct {
		RunID     string                `json:"runId"`
		SessionID string                `json:"sessionId"`
		Snapshot  *conversationSnapshot `json:"snapshot"`
	}
	if err := json.Unmarshal([]byte(response), &started); err != nil {
		t.Fatal(err)
	}
	committed := instance.conversation
	if started.RunID == "" || started.SessionID != instance.id || !conversationSnapshotEqual(started.Snapshot, committed) {
		t.Fatalf("unexpected start response: %s", response)
	}
	if committed.Revision != prior.Revision+1 || committed.Sequence != prior.Sequence+1 || len(committed.Entries) != len(prior.Entries)+1 {
		t.Fatalf("unexpected queued transcript: %+v", committed)
	}
	if &committed.Entries[0] == &prior.Entries[0] || &committed.Entries[2].Messages[0][0] != &prior.Entries[2].Messages[0][0] {
		t.Fatal("the new snapshot must own its entry slice and reuse prior message bytes")
	}
	last := committed.Entries[3]
	if last.ID != "history-4" || last.Kind != "protected" || len(last.Messages) != 1 || !conversationJSONEqual(last.Messages[0], json.RawMessage(`{"role":"user","content":"next instruction"}`)) {
		t.Fatalf("unexpected queued instruction: %+v", last)
	}
	clear(started.Snapshot.Entries[2].Messages[0])
	if !conversationJSONEqual(committed.Entries[2].Messages[0], assistant) {
		t.Fatal("editing the decoded response changed committed history")
	}
}

func TestQueuedStartDoesNotPublishAfterStartupInvalidation(t *testing.T) {
	for _, scenario := range []struct {
		name string
		code string
	}{
		{name: "conversation", code: "conversation"},
		{name: "context cancellation"},
		{name: "accepted cancellation", code: "cancelled"},
		{name: "scope", code: "scope"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			h := NewHost(Options{Models: reviewModels{list: func(ctx context.Context, query model.ModelQuery) ([]model.Model, error) {
				if query.Provider != "" {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
				return nil, ctx.Err()
			}}})
			t.Cleanup(h.Close)
			prior, err := initialConversation("fixed system", "first instruction")
			if err != nil {
				t.Fatal(err)
			}
			op, instance := newQueuedStartTestOperation(t, h, prior)
			result := make(chan error, 1)
			go func() {
				_, err := op.CallContext(ctx, "start", queuedStartPayload)
				result <- err
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("start did not reach the model catalog")
			}
			expected := prior
			h.mu.Lock()
			switch scenario.name {
			case "conversation":
				// A publication must invalidate the candidate even at the same revision.
				expected = cloneConversationSnapshot(prior)
				instance.conversation = expected
			case "context cancellation":
				cancel()
			case "accepted cancellation":
				// Acceptance locks the command before the context callback runs.
				op.task.command.cancelRequested = true
			case "scope":
				instance.state = "stopping"
			}
			h.mu.Unlock()
			close(release)
			err = <-result
			var requestErr *RequestError
			if scenario.code == "" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled start error = %v", err)
				}
			} else if !errors.As(err, &requestErr) || requestErr.Code != scenario.code {
				t.Fatalf("start error = %v, want code %q", err, scenario.code)
			}
			if instance.conversation != expected || op.task.run != nil || len(h.runs) != 0 {
				t.Fatal("a rejected start published a conversation or run")
			}
		})
	}
}

func TestQueuedStartReportsSequenceExhaustionWithoutPublishing(t *testing.T) {
	h := NewHost(Options{})
	t.Cleanup(h.Close)
	prior, err := initialConversation("fixed system", "first instruction")
	if err != nil {
		t.Fatal(err)
	}
	prior.Sequence, prior.Revision = maxSafeContextBudget, maxSafeContextBudget
	op, instance := newQueuedStartTestOperation(t, h, prior)
	response, err := op.Call("start", queuedStartPayload)
	if err != nil {
		t.Fatal(err)
	}
	var started struct {
		RunID             string                `json:"runId"`
		InstructionFailed bool                  `json:"instructionFailed"`
		InstructionError  string                `json:"instructionError"`
		Snapshot          *conversationSnapshot `json:"snapshot"`
	}
	if err := json.Unmarshal([]byte(response), &started); err != nil {
		t.Fatal(err)
	}
	if started.RunID == "" || !started.InstructionFailed || started.InstructionError != "conversation snapshot sequence is exhausted" || started.Snapshot != nil || instance.conversation != prior {
		t.Fatalf("unexpected sequence exhaustion response: %s", response)
	}
}
