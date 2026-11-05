package agent

import (
	"bytes"
	"context"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"fmt"
	"strings"
	"testing"
)

func TestConversationAppendOwnershipAndValidation(t *testing.T) {
	current, err := initialConversation("policy", "task")
	if err != nil {
		t.Fatal(err)
	}
	entry := conversationEntry{ID: "new-answer", Kind: "block", Messages: []jsonv1.RawMessage{
		jsonv1.RawMessage(`{"role":"assistant","content":"answer","opaque":{"signature":"keep"}}`),
	}}
	delta := conversationAppend{BaseRevision: current.Revision, Entries: []conversationEntry{entry}}
	next, replay, err := conversationAppendCandidate(current, nil, &delta, 1, "block", false)
	if err != nil || replay || next.Revision != current.Revision+1 || next.Sequence != current.Sequence+1 {
		t.Fatalf("append: next=%+v replay=%t err=%v", next, replay, err)
	}
	if len(current.Entries) != 2 || &current.Entries[0].Messages[0][0] != &next.Entries[0].Messages[0][0] {
		t.Fatal("committed prefix was mutated or unnecessarily copied")
	}
	before := append([]byte(nil), next.Entries[2].Messages[0]...)
	clear(entry.Messages[0])
	if !bytes.Equal(next.Entries[2].Messages[0], before) {
		t.Fatal("append retained caller-owned message bytes")
	}
	entry.Messages[0] = before
	delta.Entries[0] = entry
	if _, replay, err := conversationAppendCandidate(next, nil, &delta, 1, "block", true); err != nil || !replay {
		t.Fatalf("identical replay: replay=%t err=%v", replay, err)
	}
	if _, _, err := conversationAppendCandidate(next, nil, &delta, 1, "block", false); err == nil {
		t.Fatal("checkpoint replay accepted as a new append")
	}
	for _, tc := range []struct {
		name string
		edit func(*conversationAppend)
	}{
		{"stale revision", func(d *conversationAppend) { d.BaseRevision-- }},
		{"future revision", func(d *conversationAppend) { d.BaseRevision++ }},
		{"empty", func(d *conversationAppend) { d.Entries = nil }},
		{"existing ID", func(d *conversationAppend) { d.Entries[0].ID = current.Entries[0].ID }},
		{"wrong kind", func(d *conversationAppend) { d.Entries[0].Kind = "protected" }},
		{"incomplete tool block", func(d *conversationAppend) {
			d.Entries[0].Messages = []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"assistant","tool_calls":[{"id":"missing"}]}`)}
		}},
		{"duplicate JSON name", func(d *conversationAppend) {
			d.Entries[0].Messages = []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"assistant","content":"first","content":"second"}`)}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := conversationAppend{BaseRevision: delta.BaseRevision, Entries: []conversationEntry{cloneConversationEntry(entry)}}
			tc.edit(&bad)
			if _, _, err := conversationAppendCandidate(current, nil, &bad, 1, "block", false); err == nil {
				t.Fatal("invalid append accepted")
			}
		})
	}
	if _, _, err := conversationAppendCandidate(current, next, &delta, 1, "block", false); err == nil {
		t.Fatal("ambiguous snapshot and append accepted")
	}
	exhausted := *current
	exhausted.Revision = maxSafeContextBudget
	delta.BaseRevision = exhausted.Revision
	if _, _, err := conversationAppendCandidate(&exhausted, nil, &delta, 1, "block", false); err == nil {
		t.Fatal("exhausted revision accepted")
	}
}

func TestConversationAppendCheckpointScopeAndCompleteExchange(t *testing.T) {
	h := NewHost(Options{Models: modelsWithChat(&fixtureProvider{})})
	t.Cleanup(h.Close)
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	instance, run, cancel := newConversationTestRun(t, h, op)
	defer cancel()
	run.modelStep = &modelStepState{id: "step", responseComplete: true, finished: true,
		toolCalls: map[string]*toolCallState{"call": {resultRecorded: true}}, toolCallOrder: []string{"call"}}
	entry := conversationEntry{ID: "exchange", Kind: "block", Messages: []jsonv1.RawMessage{
		jsonv1.RawMessage(`{"role":"assistant","tool_calls":[{"id":"call"}],"opaque":{"signed":"keep"}}`),
		jsonv1.RawMessage(`{"role":"tool","tool_call_id":"call","content":"result"}`),
	}}
	req := conversationCheckpointRequest{RunID: run.id, Append: &conversationAppend{BaseRevision: instance.conversation.Revision, Entries: []conversationEntry{entry}}}
	foreign := h.NewOperation(t.Context())
	t.Cleanup(foreign.Close)
	if _, err := foreign.Call("conversation_checkpoint", mustJSON(t, req)); err == nil {
		t.Fatal("foreign append accepted")
	}
	run.modelStep.toolCalls["call"].resultRecorded = false
	if _, err := op.Call("conversation_checkpoint", mustJSON(t, req)); err == nil {
		t.Fatal("unfinished exchange accepted")
	}
	run.modelStep.toolCalls["call"].resultRecorded = true
	controlCall(t, op, "conversation_checkpoint", req)
	if len(instance.conversation.Entries) != 3 || !conversationEntryEqual(instance.conversation.Entries[2], entry) {
		t.Fatal("complete exchange was not retained")
	}
	if _, err := op.Call("conversation_checkpoint", mustJSON(t, req)); err == nil {
		t.Fatal("stale checkpoint accepted")
	}
}

func TestConversationAppendRejectedFinalAndSteerReplay(t *testing.T) {
	h := NewHost(Options{Models: modelsWithChat(&fixtureProvider{})})
	t.Cleanup(h.Close)
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	instance, run, cancel := newConversationTestRun(t, h, op)
	defer cancel()
	run.modelStep = &modelStepState{id: "final", responseComplete: true, toolCalls: map[string]*toolCallState{}}
	steer := &steerCommand{id: "steer", instruction: "revise", status: "accepted", accepted: make(chan struct{}), settled: make(chan struct{})}
	close(steer.accepted)
	run.steerByID[steer.id] = steer
	run.steerOrder = append(run.steerOrder, steer)
	final := conversationEntry{ID: "final-candidate", Kind: "block", Messages: []jsonv1.RawMessage{
		jsonv1.RawMessage(`{"role":"assistant","content":"candidate","native_item":{"encrypted":"keep"}}`),
	}}
	req := commitFinalRequest{RunID: run.id, Answer: "candidate", Append: &conversationAppend{BaseRevision: instance.conversation.Revision, Entries: []conversationEntry{final}}}
	response, err := h.commitFinal(context.Background(), op, req)
	if err != nil || response.Committed || len(response.Instructions) != 1 || !conversationEntryEqual(instance.conversation.Entries[2], final) {
		t.Fatalf("rejected candidate: %+v err=%v", response, err)
	}
	delta := &conversationAppend{BaseRevision: instance.conversation.Revision, Entries: []conversationEntry{{ID: "steer-entry", Kind: "protected", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"revise"}`)}}}}
	ack := applySteersRequest{RunID: run.id, CommandIDs: []string{steer.id}, Append: delta}
	controlCall(t, op, "apply_steers", ack)
	revision := instance.conversation.Revision
	controlCall(t, op, "apply_steers", ack)
	ack.Append = &conversationAppend{BaseRevision: revision}
	controlCall(t, op, "apply_steers", ack)
	if instance.conversation.Revision != revision || steer.status != "applied" {
		t.Fatal("steer replay changed the committed history")
	}
}

func BenchmarkConversationCheckpointUpdate(b *testing.B) {
	current, err := initialConversation("policy", "task")
	if err != nil {
		b.Fatal(err)
	}
	for range 256 {
		if err := current.append("block", struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{"assistant", strings.Repeat("x", 4096)}); err != nil {
			b.Fatal(err)
		}
	}
	next := cloneConversationSnapshot(current)
	if err := next.append("block", map[string]string{"role": "assistant", "content": strings.Repeat("y", 4096)}); err != nil {
		b.Fatal(err)
	}
	for _, delta := range []bool{false, true} {
		request := conversationCheckpointRequest{RunID: "fixture", Snapshot: next}
		if delta {
			request.Snapshot = nil
			request.Append = &conversationAppend{BaseRevision: current.Revision, Entries: next.Entries[len(current.Entries):]}
		}
		payload, err := json.Marshal(request)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("append=%t", delta), func(b *testing.B) {
			b.ReportAllocs()
			b.ReportMetric(float64(len(payload)), "request-bytes")
			for b.Loop() {
				var q conversationCheckpointRequest
				if err := json.Unmarshal(payload, &q); err != nil {
					b.Fatal(err)
				}
				if _, _, err := conversationAppendCandidate(current, q.Snapshot, q.Append, 1, "block", false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
