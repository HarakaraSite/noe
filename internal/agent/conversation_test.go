package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func TestRejectedFinalCandidateIsStoredBeforeRunInterrupt(t *testing.T) {
	h := NewHost(Options{Models: modelsWithChat(&fixtureProvider{})})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	instance, run, cancelRun := newConversationTestRun(t, h, op)
	defer cancelRun()

	assistant := json.RawMessage(`{"role":"assistant","content":"candidate answer","reasoning_details":[{"type":"summary","text":"keep"}],"native_item":{"id":"native-7","status":"complete"}}`)
	candidate := cloneConversationSnapshot(instance.conversation)
	if err := candidate.appendRaw("block", []json.RawMessage{assistant}); err != nil {
		t.Fatal(err)
	}
	steer := &steerCommand{
		id: "revise", instruction: "add the missing detail", status: "accepted",
		accepted: make(chan struct{}), settled: make(chan struct{}),
	}
	close(steer.accepted)
	run.steerByID[steer.id] = steer
	run.steerOrder = append(run.steerOrder, steer)
	run.modelStep = &modelStepState{id: "step-1", responseComplete: true, toolCalls: map[string]*toolCallState{}}

	var committed commitFinalResponse
	if err := json.Unmarshal([]byte(controlCall(t, op, "commit_final", commitFinalRequest{
		RunID: run.id, Answer: "candidate answer", Snapshot: candidate,
	})), &committed); err != nil {
		t.Fatal(err)
	}
	if committed.Committed || len(committed.Instructions) != 1 || committed.Instructions[0].CommandID != steer.id {
		t.Fatalf("pending steer did not reject the final candidate: %+v", committed)
	}

	// Interrupt immediately after the rejected gate, before applying the steer.
	var interrupted StopAgentResponse
	if err := json.Unmarshal([]byte(controlCall(t, op, "stop_agent", StopAgentRequest{AgentID: instance.id})), &interrupted); err != nil {
		t.Fatal(err)
	}
	if interrupted.Status != "accepted" {
		t.Fatalf("interrupt response = %+v", interrupted)
	}
	controlCall(t, op, "finish", finishRequest{RunID: run.id, Status: "cancelled"})

	h.mu.Lock()
	stored := cloneConversationSnapshot(instance.conversation)
	h.mu.Unlock()
	if !conversationSnapshotEqual(stored, candidate) {
		t.Fatalf("rejected final candidate was lost at the interrupt boundary: stored=%+v want=%+v", stored, candidate)
	}
	last := stored.Entries[len(stored.Entries)-1]
	if len(last.Messages) != 1 || !conversationJSONEqual(last.Messages[0], assistant) {
		t.Fatalf("provider final assistant fields were not preserved: %s", last.Messages[0])
	}
}

func TestConsumedTrimNoticeDoesNotReplayAcrossQueueRuns(t *testing.T) {
	h := NewHost(Options{Models: modelsWithChat(&fixtureProvider{gen: func(context.Context, provider.Request) (provider.Response, error) {
		return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"ok"}`), FinishReason: "stop"}, nil
	}})})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	instance, run, cancelRun := newConversationTestRun(t, h, op)
	defer cancelRun()
	before := int64(300)
	after := int64(180)
	run.context.trimCount = 1
	run.context.lastTrim = &contextTrimSnapshot{ID: "trim-1", BeforeTokens: before, AfterTokens: after, RemovedBlocks: 2}
	run.context.pendingNotice = &contextTrimSnapshot{ID: "trim-1", BeforeTokens: before, AfterTokens: after, RemovedBlocks: 2}
	run.context.pendingTrimActivityIDs = []string{"trim-1"}
	h.mu.Lock()
	instance.conversationContext = run.context.conversationContinuation()
	h.mu.Unlock()

	controlCall(t, op, "model", modelRequest{
		RunID: run.id, Model: run.config.Model, Messages: []json.RawMessage{json.RawMessage(`{"role":"system","content":"fixed"}`)},
		MaxTokens: run.config.MaxTokens, Effort: run.config.Effort,
	})

	h.mu.Lock()
	continuation := instance.conversationContext
	h.mu.Unlock()
	if continuation == nil || continuation.trimCount != 1 || continuation.lastTrim == nil || continuation.lastTrim.ID != "trim-1" {
		t.Fatalf("trim history did not continue: %+v", continuation)
	}
	if continuation.pendingNotice != nil || len(continuation.pendingTrimActivityIDs) != 0 {
		t.Fatalf("an admitted trim notice was scheduled for replay: %+v", continuation)
	}
	if continuation.previous == nil || continuation.previous.Status != "completed" {
		t.Fatalf("previous measured request was not carried: %+v", continuation.previous)
	}

	ratio := 0.1
	queued := &contextRunState{policy: &ContextPolicy{RetainRatio: &ratio}}
	queued.restoreConversation(continuation)
	queued.prepared = true
	queued.executionTokens = 100
	queued.fixedTokens = 20
	queued.budget = &ContextBudget{InputTokens: 1000}
	notice := contextMessage(queued.snapshot("next-run", time.Now()), nil)
	if strings.Contains(notice, "Context trimmed:") {
		t.Fatalf("already consumed trim notice reappeared in the next queue run: %s", notice)
	}
	status := queued.executionSnapshot()
	if status == nil || status.TrimCount != 1 || status.LastTrim == nil || status.LastTrim.ID != "trim-1" {
		t.Fatalf("trim counters and last-trim fact were not restored: %+v", status)
	}
}

func newConversationTestRun(t *testing.T, h *Host, op *Operation) (*agentInstance, *agentRun, context.CancelFunc) {
	t.Helper()
	instanceContext, cancelInstance := context.WithCancel(context.Background())
	t.Cleanup(cancelInstance)
	instance := &agentInstance{
		host: h, owner: op, id: "conversation-agent", config: taskConfig{Config: Config{Provider: "openrouter", Model: "fixture/model", Name: "managed"}},
		keepAlive: true, state: "running", ctx: instanceContext, cancel: cancelInstance, done: make(chan struct{}),
		runInterruptAccepted: make(map[string]chan struct{}), commands: make(map[string]*command),
	}
	close(instance.done)
	initial, err := initialConversation("fixed system", "original instruction")
	if err != nil {
		t.Fatal(err)
	}
	instance.conversation = initial
	runContext, cancelRun := context.WithCancel(context.Background())
	run := &agentRun{
		id: "conversation-run", operation: op, ctx: runContext, cancel: cancelRun, started: time.Now(),
		config:   runConfig{Provider: "openrouter", Model: "fixture/model", Name: "managed", MaxTokens: 128, Effort: "auto"},
		instance: instance, finalDone: make(chan struct{}), steerByID: make(map[string]*steerCommand),
	}
	instance.currentRunID = run.id
	instance.taskCancel = cancelRun
	op.task = &managedTask{instance: instance, command: &command{id: "conversation-command", instruction: "original instruction"}, run: run}
	h.mu.Lock()
	h.runs[run.id] = run
	h.instances[instance.id] = instance
	h.mu.Unlock()
	return instance, run, cancelRun
}

func TestConversationJSONEqualityPreservesSemanticComparison(t *testing.T) {
	for _, tc := range []struct {
		left, right string
		equal       bool
	}{
		{`{"content":"same","n":12345678901234567890}`, `{"content":"same","n":12345678901234567890}`, true},
		{`{"content":"a","role":"user"}`, `{"role":"user","content":"\u0061"}`, true},
		{`{"n":12345678901234567890}`, `{"n":12345678901234567891}`, false},
		{`{"content":"before"}`, `{"content":"after"}`, false},
		{`{"broken":`, `{"broken":`, false},
	} {
		if got := conversationJSONEqual(json.RawMessage(tc.left), json.RawMessage(tc.right)); got != tc.equal {
			t.Errorf("equal(%s, %s)=%t; want %t", tc.left, tc.right, got, tc.equal)
		}
	}
}
