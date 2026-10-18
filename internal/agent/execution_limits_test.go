package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/decision"
	"noe/internal/events"
)

func limitPtr(value int64) *int64 { return &value }

func TestExecutionLimitsConfigValidationAndEncoding(t *testing.T) {
	var request spawnRequest
	if err := json.Unmarshal([]byte(`{"config":{"provider":"openrouter","model":"fixture/model","name":"root","effort":"auto","executionLimits":{"providerCalls":0,"agentStarts":3,"queuedTasks":2,"hookCalls":9007199254740991}},"keepAlive":true}`), &request); err != nil {
		t.Fatal(err)
	}
	got := request.Config.ExecutionLimits
	if got == nil || got.ProviderCalls == nil || *got.ProviderCalls != 0 || got.AgentStarts == nil || *got.AgentStarts != 3 || got.QueuedTasks == nil || *got.QueuedTasks != 2 || got.HookCalls == nil || *got.HookCalls != maxSafeContextBudget {
		t.Fatalf("decoded limits = %+v", got)
	}

	for _, limits := range []string{
		`null`, `[]`, `{"unknown":1}`, `{"providerCalls":-1}`, `{"agentStarts":1.5}`,
		`{"queuedTasks":9007199254740992}`, `{"hookCalls":null}`,
	} {
		t.Run(limits, func(t *testing.T) {
			data := `{"provider":"openrouter","model":"fixture/model","name":"root","effort":"auto","executionLimits":` + limits + `}`
			var config Config
			if err := json.Unmarshal([]byte(data), &config); err == nil {
				t.Fatalf("invalid executionLimits %s was accepted", limits)
			}
		})
	}

	encoded, err := json.Marshal(Config{ExecutionLimits: &ExecutionLimits{AgentStarts: limitPtr(4)}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded Config
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.ExecutionLimits == nil || decoded.ExecutionLimits.AgentStarts == nil || *decoded.ExecutionLimits.AgentStarts != 4 {
		t.Fatalf("round trip %s: limits=%+v err=%v", encoded, decoded.ExecutionLimits, err)
	}
}

func TestExecutionAgentStartsZeroRejectsRootBeforeRegistration(t *testing.T) {
	observed := make(chan events.Event, 8)
	provider := &fixtureProvider{}
	h := NewHost(Options{
		Provider:   provider,
		Executable: "/no/such/noe-executable",
		Observe:    func(event events.Event) { observed <- event },
	})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()

	_, err := op.Call("spawn_agent", `{"config":{"provider":"openrouter","model":"fixture/model","name":"zero-starts","effort":"auto","executionLimits":{"agentStarts":0}},"instruction":"must not start"}`)
	rpcErr, ok := err.(*RPCError)
	if !ok || rpcErr.Code != "execution_limit" {
		t.Fatalf("spawn error = %#v, want execution_limit", err)
	}
	if provider.callCount() != 0 {
		t.Fatalf("provider calls = %d, want no runtime start", provider.callCount())
	}

	h.mu.Lock()
	instances, groups := len(h.instances), len(h.groups)
	h.mu.Unlock()
	if instances != 0 || groups != 0 {
		t.Fatalf("zero-capacity root registered runtime state: instances=%d groups=%d", instances, groups)
	}

	var limitEvent events.Event
	for len(observed) > 0 {
		event := <-observed
		if event["type"] == "execution.limit_reached" {
			limitEvent = event
		} else if event["type"] == "instance.started" {
			t.Fatalf("zero-capacity root emitted instance.started: %+v", event)
		}
	}
	if limitEvent == nil {
		t.Fatal("zero-capacity root did not emit execution.limit_reached")
	}
	if limitEvent["limitName"] != "agentStarts" || limitEvent["limit"] != int64(0) || limitEvent["used"] != int64(0) {
		t.Fatalf("zero-capacity limit event = %+v", limitEvent)
	}
	if _, started := limitEvent["agentId"]; started {
		t.Fatalf("root was attributed an agentId before admission: %+v", limitEvent)
	}
}

func TestExecutionAdmissionLimitsAreAtomicAndCloseOnlyTheirGroup(t *testing.T) {
	tests := []struct {
		name      string
		limitName string
		limit     int64
		initial   int64
	}{
		{name: "provider calls", limitName: "providerCalls", limit: 2},
		{name: "agent starts including root", limitName: "agentStarts", limit: 3, initial: 1},
		{name: "queued tasks", limitName: "queuedTasks", limit: 2},
		{name: "hook calls", limitName: "hookCalls", limit: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observed := make(chan events.Event, 32)
			h := NewHost(Options{Observe: func(event events.Event) { observed <- event }})
			defer h.Close()
			rootContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			limits := ExecutionLimits{}
			switch test.limitName {
			case "providerCalls":
				limits.ProviderCalls = limitPtr(test.limit)
			case "agentStarts":
				limits.AgentStarts = limitPtr(test.limit)
			case "queuedTasks":
				limits.QueuedTasks = limitPtr(test.limit)
			case "hookCalls":
				limits.HookCalls = limitPtr(test.limit)
			}
			group := &executionGroup{
				id: "root-group", limits: limits, startedAt: time.Now().Add(-time.Second),
				counters: executionCounters{AgentStarts: test.initial, ActiveAgents: 1},
			}
			root := &agentInstance{
				host: h, id: group.id, group: group, config: taskConfig{Config: Config{Name: "root", Model: "fixture/model"}},
				ctx: rootContext, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1),
				state: "idle", keepAlive: true, activeCounted: true,
			}
			h.mu.Lock()
			h.instances[root.id] = root
			h.groups[group.id] = group
			h.mu.Unlock()
			go root.serve()

			remaining := test.limit - test.initial
			attempts := int(remaining + 8)
			var wg sync.WaitGroup
			var accepted int64
			var actionMu sync.Mutex
			var action *executionLimitAction
			for index := 0; index < attempts; index++ {
				wg.Add(1)
				go func(index int) {
					defer wg.Done()
					origin := activityOrigin{Kind: "script", RunID: "source-run"}
					h.mu.Lock()
					nextAction, ok := h.reserveExecutionLocked(group, test.limitName, "agent", "attempt-activity", origin)
					h.mu.Unlock()
					if ok {
						atomic.AddInt64(&accepted, 1)
					}
					if nextAction != nil {
						actionMu.Lock()
						if action == nil {
							action = nextAction
						}
						actionMu.Unlock()
					}
				}(index)
			}
			wg.Wait()
			if accepted != remaining {
				t.Fatalf("admitted %d attempts, want exact remaining capacity %d", accepted, remaining)
			}
			if action == nil {
				t.Fatal("limit exhaustion did not produce a stop action")
			}
			h.finishExecutionLimit(action)
			select {
			case <-root.done:
			case <-time.After(3 * time.Second):
				t.Fatal("root group did not settle after limit stop")
			}

			h.mu.Lock()
			counters := group.counters
			snapshot := group.snapshotLocked()
			rootState, rootReason := root.state, root.reason
			h.mu.Unlock()
			if counters.value(test.limitName) != test.limit || counters.ActiveAgents != 0 || counters.ActiveTasks != 0 {
				t.Fatalf("counters after stop = %+v", counters)
			}
			if snapshot.Stop == nil || snapshot.Stop.LimitName != test.limitName || snapshot.Stop.Limit != test.limit || snapshot.Stop.Used != test.limit || snapshot.Limits.value(test.limitName) == nil || *snapshot.Limits.value(test.limitName) != test.limit {
				t.Fatalf("execution snapshot = %+v", snapshot)
			}
			if rootState != "stopped" || rootReason != "execution_limit" || snapshot.ElapsedMS <= 0 {
				t.Fatalf("root state/reason/elapsed = %s/%s/%d", rootState, rootReason, snapshot.ElapsedMS)
			}
			frozenElapsed := snapshot.ElapsedMS
			h.mu.Lock()
			secondSnapshot := group.snapshotLocked()
			h.mu.Unlock()
			if secondSnapshot.ElapsedMS != frozenElapsed {
				t.Fatalf("terminal elapsed time moved: %d -> %d", frozenElapsed, secondSnapshot.ElapsedMS)
			}

			limitEvents := 0
			for {
				select {
				case event := <-observed:
					if event["type"] == "execution.limit_reached" {
						limitEvents++
						if event["limitName"] != test.limitName || event["used"] != test.limit || event["limit"] != test.limit || event["groupId"] != group.id || event["activityId"] == "" || event["causedBy"] == nil {
							t.Errorf("limit event lacks stable source/limit fields: %+v", event)
						}
					}
				default:
					if limitEvents != 1 {
						t.Fatalf("got %d limit events, want one", limitEvents)
					}
					return
				}
			}
		})
	}
}

func TestExecutionProviderLimitIsSharedByAgentAndDecision(t *testing.T) {
	h := NewHost(Options{})
	group := &executionGroup{id: "root-group", limits: ExecutionLimits{ProviderCalls: limitPtr(2)}}
	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := &agentInstance{host: h, id: group.id, group: group, state: "idle", ctx: rootCtx, cancel: cancel, config: taskConfig{Config: Config{Name: "root"}}}
	h.mu.Lock()
	h.instances[root.id] = root
	_, first := h.reserveExecutionLocked(group, "providerCalls", "agent", "model-activity", activityOrigin{Kind: "model"})
	_, second := h.reserveExecutionLocked(group, "providerCalls", "decision", "decision-activity", activityOrigin{Kind: "tool"})
	_, third := h.reserveExecutionLocked(group, "providerCalls", "agent", "next-model-activity", activityOrigin{Kind: "model"})
	counters := group.counters
	stop := cloneExecutionStop(group.stop)
	h.mu.Unlock()
	if !first || !second || third || counters.ProviderCalls != 2 || counters.AgentProviderCalls != 1 || counters.DecisionProviderCalls != 1 {
		t.Fatalf("shared provider admission: first=%v second=%v third=%v counters=%+v", first, second, third, counters)
	}
	if stop == nil || stop.LimitName != "providerCalls" || stop.Used != 2 {
		t.Fatalf("provider stop receipt = %+v", stop)
	}
}

func TestAdmittedModelCallEntersProviderAfterStartedObserverCancellation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	entered := make(chan error, 1)
	terminal := make(chan events.Event, 4)
	p := &fixtureProvider{gen: func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		entered <- ctx.Err()
		return provider.Response{}, ctx.Err()
	}}
	h := NewHost(Options{Provider: p, Observe: func(event events.Event) {
		switch event["type"] {
		case "model.started":
			close(started)
			<-release
		case "model.finished", "model.failed", "model.cancelled":
			terminal <- event
		}
	}})
	op := h.NewOperation(context.Background())
	defer func() {
		releaseOnce.Do(func() { close(release) })
		op.Close()
		h.Close()
	}()
	runID := startFixtureRun(t, op)
	group := &executionGroup{id: "root-agent", limits: ExecutionLimits{ProviderCalls: limitPtr(1)}}
	h.mu.Lock()
	h.runs[runID].group = group
	h.groups[group.id] = group
	h.mu.Unlock()

	callDone := make(chan error, 1)
	go func() {
		_, err := op.Call("model", fixtureModelPayload(runID, "cancel after admission"))
		callDone <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("model.started observer was not reached")
	}
	op.cancel()
	releaseOnce.Do(func() { close(release) })
	select {
	case ctxErr := <-entered:
		if ctxErr != context.Canceled {
			t.Fatalf("provider entered with context error %v, want canceled", ctxErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("admitted model attempt did not enter the provider adapter")
	}
	select {
	case event := <-terminal:
		if event["type"] != "model.failed" || event["status"] != "cancelled" {
			t.Fatalf("terminal model event=%+v, want model.failed status=cancelled", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled provider attempt emitted no terminal model event")
	}
	select {
	case err := <-callDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("model RPC error=%v, want canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled model RPC did not return")
	}
	if got := p.callCount(); got != 1 {
		t.Fatalf("provider adapter entries=%d, want exactly one admitted attempt", got)
	}
	select {
	case event := <-terminal:
		t.Fatalf("provider attempt emitted multiple terminal model events; extra=%+v", event)
	default:
	}
	h.mu.Lock()
	counters := group.counters
	h.mu.Unlock()
	if counters.ProviderCalls != 1 || counters.AgentProviderCalls != 1 {
		t.Fatalf("admitted model attempt counters=%+v", counters)
	}
}

func TestAdmittedDecisionCallEntersProviderAfterStartedObserverCancellation(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	var entries atomic.Int32
	entered := make(chan error, 1)
	terminal := make(chan events.Event, 4)
	h := NewHost(Options{
		DecisionProvider: decisionProviderFunc(func(ctx context.Context, _ decision.Request) (decision.Response, error) {
			entries.Add(1)
			entered <- ctx.Err()
			return decision.Response{}, ctx.Err()
		}),
		Observe: func(event events.Event) {
			switch event["type"] {
			case "decision.started":
				close(started)
				<-release
			case "decision.completed", "decision.failed", "decision.cancelled":
				terminal <- event
			}
		},
	})
	op := h.NewOperation(context.Background())
	var decisionOp *Operation
	defer func() {
		releaseOnce.Do(func() { close(release) })
		if decisionOp != nil {
			decisionOp.Close()
		}
		op.Close()
		h.Close()
	}()
	runID := startFixtureRun(t, op)
	group := &executionGroup{id: "root-agent", limits: ExecutionLimits{ProviderCalls: limitPtr(1)}}
	h.mu.Lock()
	run := h.runs[runID]
	run.group = group
	h.groups[group.id] = group
	h.mu.Unlock()
	decisionOp = h.NewOperation(run.ctx)
	h.mu.Lock()
	decisionOp.parentRunID = runID
	decisionOp.group = group
	decisionOp.decisionOwner = decisionOwnerFields(runID, run, "")
	h.mu.Unlock()

	callDone := make(chan error, 1)
	go func() {
		_, err := decisionOp.Call("decide", validDecisionRequest)
		callDone <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("decision.started observer was not reached")
	}
	decisionOp.cancel()
	releaseOnce.Do(func() { close(release) })
	select {
	case ctxErr := <-entered:
		if ctxErr != context.Canceled {
			t.Fatalf("decision provider entered with context error %v, want canceled", ctxErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("admitted decision attempt did not enter the provider adapter")
	}
	select {
	case event := <-terminal:
		if event["type"] != "decision.cancelled" {
			t.Fatalf("terminal decision event=%+v, want decision.cancelled", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled provider attempt emitted no terminal decision event")
	}
	select {
	case err := <-callDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("decision RPC error=%v, want canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled decision RPC did not return")
	}
	if got := entries.Load(); got != 1 {
		t.Fatalf("decision provider adapter entries=%d, want exactly one admitted attempt", got)
	}
	select {
	case event := <-terminal:
		t.Fatalf("provider attempt emitted multiple terminal decision events; extra=%+v", event)
	default:
	}
	h.mu.Lock()
	counters := group.counters
	h.mu.Unlock()
	if counters.ProviderCalls != 1 || counters.DecisionProviderCalls != 1 {
		t.Fatalf("admitted decision attempt counters=%+v", counters)
	}
}
