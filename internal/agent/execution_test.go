package agent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/decision"
	"noe/internal/events"
)

func newManagedTestRun(t *testing.T, h *Host, instance *agentInstance, runID, activityID, commandID, parentRunID string) (*Operation, *agentRun) {
	t.Helper()
	ctx, cancel := context.WithCancel(instance.ctx)
	op := h.NewOperation(ctx)
	run := &agentRun{
		id: runID, operation: op, ctx: ctx, cancel: cancel, started: time.Now(),
		config:      runConfig{Provider: "openrouter", Model: "fixture/model", Name: "managed", MaxTokens: 128, Effort: "auto", MaxSteps: 1},
		parentRunID: parentRunID, group: instance.group, activityID: activityID,
		instance: instance, childScopes: make(map[*Operation]struct{}), instances: make(map[string]*agentInstance),
		steerByID: make(map[string]*steerCommand), finalDone: make(chan struct{}),
	}
	h.mu.Lock()
	op.parentRunID = parentRunID
	op.group = instance.group
	op.task = &managedTask{instance: instance, command: &command{id: commandID, activityID: commandID + "-activity"}, run: run}
	instance.currentRunID = runID
	h.runs[runID] = run
	h.mu.Unlock()
	return op, run
}

func TestExecutionGroupPropagationAndQueueCountingWithoutObservation(t *testing.T) {
	h := NewHost(Options{
		Provider:   &fixtureProvider{},
		Executable: filepath.Join(t.TempDir(), "not-a-running-binary"),
		Cwd:        t.TempDir(),
		Observe:    nil,
	})
	defer h.Close()
	rootOp := h.NewOperation(context.Background())
	defer rootOp.Close()

	rootAID := spawnLifecycle(t, rootOp, nil, true)
	rootBID := spawnLifecycle(t, rootOp, nil, true)
	h.mu.Lock()
	rootA, rootB := h.instances[rootAID], h.instances[rootBID]
	if rootA == nil || rootB == nil {
		h.mu.Unlock()
		t.Fatal("root instances were not registered")
	}
	groupA, groupB := rootA.group, rootB.group
	h.mu.Unlock()
	if groupA == nil || groupB == nil || groupA.id != rootAID || groupB.id != rootBID || groupA == groupB {
		groupAID, groupBID := "", ""
		if groupA != nil {
			groupAID = groupA.id
		}
		if groupB != nil {
			groupBID = groupB.id
		}
		t.Fatalf("independent roots do not have distinct root-agent groups: A=%q B=%q", groupAID, groupBID)
	}

	rootTaskOp, rootRun := newManagedTestRun(t, h, rootA, "root-run", "root-run-activity", "root-command", "")
	defer rootTaskOp.Close()
	childID := spawnLifecycle(t, rootTaskOp, nil, true)
	h.mu.Lock()
	child := h.instances[childID]
	h.mu.Unlock()
	if child == nil {
		t.Fatal("child instance was not registered")
	}
	if child.group != groupA || child.parent != rootRun || len(child.causedBy) != 1 || child.causedBy[0] != rootRun.activityID {
		t.Fatalf("child ownership/cause = group %p parent %p cause %v", child.group, child.parent, child.causedBy)
	}

	childTaskOp, childRun := newManagedTestRun(t, h, child, "child-run", "child-run-activity", "child-command", rootRun.id)
	defer childTaskOp.Close()
	grandchildID := spawnLifecycle(t, childTaskOp, nil, true)
	h.mu.Lock()
	grandchild := h.instances[grandchildID]
	h.mu.Unlock()
	if grandchild == nil {
		t.Fatal("grandchild instance was not registered")
	}
	if grandchild.group != groupA || grandchild.parent != childRun || len(grandchild.causedBy) != 1 || grandchild.causedBy[0] != childRun.activityID {
		t.Fatalf("grandchild ownership/cause = group %p parent %p cause %v", grandchild.group, grandchild.parent, grandchild.causedBy)
	}
	if childRun.parentRunID != rootRun.id || childTaskOp.parentRunID != rootRun.id {
		t.Fatalf("bootstrap parent relationship changed: run=%q operation=%q", childRun.parentRunID, childTaskOp.parentRunID)
	}

	queued := queueRequest{AgentID: childID, Instruction: "next task", CommandID: "queue-on-child"}
	lifecycleCall(t, rootTaskOp, "queue_agent", queued)
	// Replaying the same command returns the original receipt and does not add
	// another task or consume another queue count.
	lifecycleCall(t, rootTaskOp, "queue_agent", queued)
	h.mu.Lock()
	counters := groupA.counters
	command := child.commands[queued.CommandID]
	snapshot := child.snapshotLocked()
	h.mu.Unlock()
	if counters.AgentStarts != 3 || counters.QueuedTasks != 1 {
		t.Fatalf("group counters without Observe = %+v, want three starts and one queued task", counters)
	}
	if command == nil || command.activityID == "" || len(command.causedBy) != 1 || command.causedBy[0] != rootRun.activityID || command.origin.Kind != "script" {
		t.Fatalf("queued command activity/cause/origin = %+v", command)
	}
	if snapshot.GroupID != rootAID || snapshot.Execution == nil || snapshot.Execution.Counters.QueuedTasks != 1 {
		t.Fatalf("observe_agent snapshot did not carry the root summary: %+v", snapshot)
	}
	if h.opts.Observe != nil {
		t.Fatal("test unexpectedly configured observation")
	}
}

func TestModelRequestIDAndUsageCountersWithoutObservation(t *testing.T) {
	providerCalled := false
	p := &fixtureProvider{gen: func(_ context.Context, request provider.Request) (provider.Response, error) {
		providerCalled = true
		if request.Observe != nil {
			t.Error("provider stream observation must remain disabled without Host.Observe")
		}
		return provider.Response{
			Message:      json.RawMessage(`{"role":"assistant","content":"answer"}`),
			FinishReason: "stop",
			Usage:        json.RawMessage(`{"input_tokens":3,"output_tokens":2,"total_tokens":5,"input_tokens_details":{"cached_tokens":1},"cost":0.0004}`),
		}, nil
	}}
	h := NewHost(Options{Provider: p})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	runID := startFixtureRun(t, op)
	group := &executionGroup{id: "root-agent"}
	h.mu.Lock()
	h.runs[runID].group = group
	h.groups[group.id] = group
	h.mu.Unlock()

	text, err := op.Call("model", fixtureModelPayload(runID, "work"))
	if err != nil {
		t.Fatal(err)
	}
	var response modelResponse
	if err := json.Unmarshal([]byte(text), &response); err != nil {
		t.Fatal(err)
	}
	if !providerCalled || response.RequestID == "" || len(response.Usage) == 0 {
		t.Fatalf("model response did not pair request ID and usage: %+v, providerCalled=%v", response, providerCalled)
	}
	h.mu.Lock()
	counters := group.counters
	usage := group.usage
	h.mu.Unlock()
	if counters.ProviderCalls != 1 || counters.AgentProviderCalls != 1 || counters.DecisionProviderCalls != 0 {
		t.Fatalf("provider counters = %+v", counters)
	}
	if usage.PromptTokens == nil || *usage.PromptTokens != 3 || usage.CompletionTokens == nil || *usage.CompletionTokens != 2 || usage.TotalTokens == nil || *usage.TotalTokens != 5 || usage.CachedTokens == nil || *usage.CachedTokens != 1 || usage.Cost == nil || *usage.Cost != 0.0004 {
		t.Fatalf("aggregated model usage = %+v", usage)
	}
}

func TestDecisionActivityUsesCurrentOwnerAndGroup(t *testing.T) {
	observed := make(chan events.Event, 4)
	h := NewHost(Options{
		DecisionProvider: decisionProviderFunc(func(context.Context, decision.Request) (decision.Response, error) {
			return decision.Response{JSON: json.RawMessage(`{"model":"m","answers":{"q":{"choice":"yes"}},"usage":{"input_tokens":7,"output_tokens":1,"total_tokens":8,"cost":0.01}}`)}, nil
		}),
		Observe: func(event events.Event) { observed <- event },
	})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	runID := startFixtureRun(t, op)
	group := &executionGroup{id: "root-agent"}
	h.mu.Lock()
	run := h.runs[runID]
	run.group = group
	run.activityID = "parent-run-activity"
	h.groups[group.id] = group
	h.mu.Unlock()
	decisionOp := h.NewOperation(run.ctx)
	defer decisionOp.Close()
	h.mu.Lock()
	decisionOp.parentRunID = runID
	decisionOp.group = group
	decisionOp.causeActivityIDs = []string{"run-js-activity"}
	decisionOp.activityOrigin = activityOrigin{Kind: "tool", RunID: runID, ToolCallID: "tool-call"}
	decisionOp.decisionOwner = decisionOwnerFields(runID, run, "tool-call")
	h.mu.Unlock()
	if _, err := decisionOp.Call("decide", validDecisionRequest); err != nil {
		t.Fatal(err)
	}
	var started, completed events.Event
	for started == nil || completed == nil {
		event := <-observed
		switch event["type"] {
		case "decision.started":
			started = event
		case "decision.completed":
			completed = event
		}
	}
	if started["type"] != "decision.started" || completed["type"] != "decision.completed" {
		t.Fatalf("decision event order: %v, %v", started["type"], completed["type"])
	}
	for _, event := range []events.Event{started, completed} {
		origin, ok := event["origin"].(activityOrigin)
		causes, _ := event["causedBy"].([]string)
		if event["runId"] != runID || event["groupId"] != group.id || event["activityId"] == "" || event["requestId"] == "" || len(causes) != 1 || causes[0] != "run-js-activity" || !ok || origin.Kind != "tool" || origin.ToolCallID != "tool-call" {
			t.Errorf("decision attribution fields are incomplete: %+v", event)
		}
	}
	if started["activityId"] != completed["activityId"] || started["requestId"] != completed["requestId"] {
		t.Fatalf("decision lifecycle lost request/activity identity: started=%+v completed=%+v", started, completed)
	}
	h.mu.Lock()
	counters, usage := group.counters, group.usage
	h.mu.Unlock()
	if counters.ProviderCalls != 1 || counters.DecisionProviderCalls != 1 || counters.AgentProviderCalls != 0 {
		t.Fatalf("decision was not counted in the owning group: %+v", counters)
	}
	if usage.TotalTokens == nil || *usage.TotalTokens != 8 || usage.Cost == nil || *usage.Cost != 0.01 {
		t.Fatalf("decision usage was not aggregated: %+v", usage)
	}
}

func TestIdleExitFinishesBeforeInterruptAdmission(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	publishing, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	h := NewHost(Options{Observe: func(event events.Event) {
		if event["type"] == "instance.stopped" {
			close(publishing)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}})
	defer h.Close()
	defer releaseOnce.Do(func() { close(release) })
	op := h.NewOperation(ctx)
	defer op.Close()
	instanceCtx, instanceCancel := context.WithCancel(ctx)
	group := &executionGroup{id: "root-agent", counters: executionCounters{ActiveAgents: 1}}
	instance := &agentInstance{
		host: h, id: "root-agent", group: group, owner: op,
		state: "idle", activeCounted: true, ctx: instanceCtx, cancel: instanceCancel,
		done: make(chan struct{}),
	}
	h.mu.Lock()
	h.instances[instance.id] = instance
	h.groups[group.id] = group
	h.mu.Unlock()
	go instance.serve()
	select {
	case <-publishing:
	case <-ctx.Done():
		t.Fatal("idle instance did not reach terminal publication")
	}
	// Exercise the real idle-exit branch, with terminal event publication
	// paused. Admission must already be closed with counters settled.
	response := httptest.NewRecorder()
	h.interruptInstance(response, op, instance.id)
	if response.Code != 409 {
		t.Fatalf("interrupt during terminal publication returned status %d, want conflict: %s", response.Code, response.Body.String())
	}
	h.mu.Lock()
	state, reason, active := instance.state, instance.reason, group.counters.ActiveAgents
	_, stillActive := h.groups[group.id]
	h.mu.Unlock()
	if state != "stopped" || reason != "task_completed" || active != 0 || stillActive {
		t.Fatalf("terminal state/count mismatch: state=%s reason=%s active=%d groupRetained=%v", state, reason, active, stillActive)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-instance.done:
	case <-ctx.Done():
		t.Fatal("terminal instance did not finish publication")
	}
}
