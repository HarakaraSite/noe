package agent

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestCancelTaskAllowsDifferentPendingCommandAfterFinalCommit(t *testing.T) {
	h := NewHost(Options{})
	owner := h.NewOperation(context.Background())
	defer h.Close()

	closed := make(chan struct{})
	close(closed)
	active := &command{id: "active", active: true, accepted: closed, done: closed}
	pending := &command{id: "pending", accepted: closed, done: make(chan struct{})}
	instance := &agentInstance{
		host: h, id: "agent", owner: owner, keepAlive: true, state: "running",
		commands: map[string]*command{active.id: active, pending.id: pending},
		queue:    []*command{pending}, done: closed,
	}
	owner.instances[instance.id] = instance
	h.mu.Lock()
	h.instances[instance.id] = instance
	instance.currentRunID = "run"
	runCtx, cancel := context.WithCancel(context.Background())
	h.runs["run"] = &agentRun{id: "run", operation: owner, ctx: runCtx, cancel: cancel, instance: instance, finalClosed: true}
	h.mu.Unlock()
	defer cancel()

	response, err := h.cancelTask(owner, CancelTaskRequest{AgentID: instance.id, CommandID: pending.id})
	if err != nil || response.Status != "accepted" {
		t.Fatalf("pending cancel=%+v err=%v", response, err)
	}
	select {
	case <-pending.done:
	case <-time.After(time.Second):
		t.Fatal("cancelled command terminal event was not published")
	}
	if pending.outcome == nil || pending.outcome.State != "cancelled" {
		t.Fatalf("pending outcome=%+v", pending.outcome)
	}
	if _, err := h.cancelTask(owner, CancelTaskRequest{AgentID: instance.id, CommandID: active.id}); err == nil {
		t.Fatal("active command was cancelled after its final answer was committed")
	}
}

func TestClientAndHostCloseJoinQueuedCancellationPublication(t *testing.T) {
	for _, closeHost := range []bool{false, true} {
		name := "client"
		if closeHost {
			name = "host"
		}
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			var publishOnce sync.Once
			h := NewHost(Options{Observe: func(event events.Event) {
				if event["type"] == "command.cancelled" {
					publishOnce.Do(func() {
						entered <- struct{}{}
						<-release
					})
				}
			}})
			owner := h.NewOperation(context.Background())
			instanceCtx, instanceCancel := context.WithCancel(owner.ctx)
			instance := &agentInstance{
				host: h, id: "agent", owner: owner, keepAlive: true, state: "starting",
				ctx: instanceCtx, cancel: instanceCancel, done: make(chan struct{}), wake: make(chan struct{}, 1),
				commands: make(map[string]*command),
			}
			owner.instances[instance.id] = instance
			h.mu.Lock()
			h.instances[instance.id] = instance
			h.mu.Unlock()
			go instance.serve()
			select {
			case <-time.After(time.Second):
				t.Fatal("instance did not enter idle state")
			case <-waitForInstanceIdle(h, instance):
			}

			accepted := make(chan struct{})
			close(accepted)
			command := &command{id: "pending", accepted: accepted, done: make(chan struct{})}
			h.mu.Lock()
			instance.commands[command.id] = command
			instance.queue = append(instance.queue, command)
			h.mu.Unlock()
			if _, err := h.cancelTask(owner, CancelTaskRequest{AgentID: instance.id, CommandID: command.id}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("cancellation publisher did not enter observer")
			}

			closed := make(chan struct{})
			if closeHost {
				go func() { h.Close(); close(closed) }()
			} else {
				go func() { owner.Close(); close(closed) }()
			}
			select {
			case <-closed:
				t.Fatal("close returned before the accepted terminal event finished publishing")
			case <-time.After(40 * time.Millisecond):
			}
			close(release)
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("close did not join the cancellation publisher")
			}
			if !closeHost {
				h.Close()
			}
		})
	}
}

func waitForInstanceIdle(h *Host, instance *agentInstance) <-chan struct{} {
	ready := make(chan struct{})
	go func() {
		for {
			h.mu.Lock()
			idle := instance.state == "idle"
			h.mu.Unlock()
			if idle {
				close(ready)
				return
			}
			select {
			case <-instance.done:
				return
			default:
			}
			time.Sleep(time.Millisecond)
		}
	}()
	return ready
}

// The task runtime can close its operation before task outcome commitment.
// Cancellation in that window must still precede both terminal events.
func TestCancelAfterRunRemovalPublishesAcceptanceBeforeTerminals(t *testing.T) {
	previous := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previous)
	for iteration := 0; iteration < 20; iteration++ {
		func() {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			removed, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			var mu sync.Mutex
			var order []string
			h := NewHost(Options{Context: ctx, Models: reviewModelsWithProviderCheck{}, Observe: func(event events.Event) {
				kind, _ := event["type"].(string)
				if kind == "interrupt.accepted" || kind == "agent.cancelled" || kind == "command.cancelled" {
					mu.Lock()
					order = append(order, kind)
					mu.Unlock()
				}
			}, Execute: func(ctx context.Context, execution Execution) (Result, error) {
				if _, err := execution.Operation.Call("start", `{"provider":"fixture","model":"fixture","name":"late-cancel","effort":"none"}`); err != nil {
					return Result{}, err
				}
				execution.Operation.Close()
				close(removed)
				select {
				case <-release:
				case <-ctx.Done():
				}
				return Result{ExitCode: -1, Reason: "cancelled"}, nil
			}})
			defer h.Close()
			defer releaseOnce.Do(func() { close(release) })
			owner := h.NewOperation(ctx)
			instruction := "cancel during runtime cleanup"
			accepted, err := h.spawn(ctx, owner, spawnRequest{Config: taskConfig{Config: Config{Provider: "fixture", Model: "fixture", Name: "late-cancel", Effort: "none"}}, Instruction: &instruction})
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-removed:
			case <-ctx.Done():
				t.Fatal("runtime did not remove its run")
			}
			h.mu.Lock()
			instance := h.instances[accepted.AgentID]
			h.mu.Unlock()
			if _, err := h.cancelTask(owner, CancelTaskRequest(accepted)); err != nil {
				t.Fatal(err)
			}
			releaseOnce.Do(func() { close(release) })
			select {
			case <-instance.done:
			case <-ctx.Done():
				t.Fatal("task did not finish after cancellation")
			}
			h.Close()
			mu.Lock()
			defer mu.Unlock()
			if strings.Join(order, ",") != "interrupt.accepted,agent.cancelled,command.cancelled" {
				t.Fatalf("iteration %d event order=%v", iteration, order)
			}
		}()
	}
}

func TestCancelCurrentCommandPreservesQueuedCommands(t *testing.T) {
	for _, active := range []bool{false, true} {
		name := "selected"
		if active {
			name = "active"
		}
		t.Run(name, func(t *testing.T) {
			var eventMu sync.Mutex
			var currentAcceptances []events.Event
			h := NewHost(Options{Observe: func(event events.Event) {
				if event["type"] == "interrupt.accepted" && event["commandId"] == "current" {
					eventMu.Lock()
					currentAcceptances = append(currentAcceptances, event)
					eventMu.Unlock()
				}
			}})
			defer h.Close()
			owner := h.NewOperation(t.Context())
			instanceCtx, instanceCancel := context.WithCancel(owner.ctx)
			defer instanceCancel()
			done := make(chan struct{})
			close(done) // This fixture drives transitions directly, without serve.
			instance := &agentInstance{
				host: h, id: "agent", owner: owner, state: "starting", ctx: instanceCtx,
				cancel: instanceCancel, done: done, commands: make(map[string]*command),
				runInterruptAccepted: make(map[string]chan struct{}),
			}
			current := newTaskCommand("current", "current-activity", "")
			first := newTaskCommand("first", "first-activity", "")
			middle := newTaskCommand("middle", "middle-activity", "")
			last := newTaskCommand("last", "last-activity", "")
			for _, cmd := range []*command{current, first, middle, last} {
				close(cmd.accepted)
			}
			h.mu.Lock()
			h.instances[instance.id] = instance
			owner.instances[instance.id] = instance
			for _, cmd := range []*command{current, first, middle, last} {
				instance.enqueueCommandLocked(cmd)
			}
			selected := instance.selectCommandLocked()
			var run *agentRun
			var taskCtx context.Context
			if active {
				if instance.startSelectedCommandLocked(current) != commandStarted {
					h.mu.Unlock()
					t.Fatal("selected command did not start")
				}
				var taskCancel context.CancelFunc
				taskCtx, taskCancel = context.WithCancel(instanceCtx)
				instance.taskCancel = taskCancel
				instance.currentRunID = "run"
				run = &agentRun{id: "run", operation: owner, ctx: taskCtx, cancel: taskCancel, instance: instance}
				h.runs[run.id] = run
			}
			h.mu.Unlock()
			if selected != current {
				t.Fatal("selection did not reserve the current command")
			}

			request := CancelTaskRequest{AgentID: instance.id, CommandID: current.id}
			var accepted chan struct{}
			for attempt := 0; attempt < 2; attempt++ {
				response, err := h.cancelTask(owner, request)
				if err != nil || response.Status != "accepted" {
					t.Fatalf("current cancellation attempt %d: response=%+v err=%v", attempt, response, err)
				}
				h.mu.Lock()
				gate := current.cancelAccepted
				h.mu.Unlock()
				if attempt == 0 {
					accepted = gate
				} else if gate != accepted {
					t.Fatal("retry replaced the cancellation acceptance gate")
				}
			}
			h.mu.Lock()
			if !current.cancelRequested || accepted == nil || current.outcome != nil ||
				len(instance.queue) != 3 || instance.queue[0] != first ||
				instance.queue[1] != middle || instance.queue[2] != last {
				h.mu.Unlock()
				t.Fatal("current cancellation changed queued commands or settled current prematurely")
			}
			if active && (!run.interrupting || instance.interruptedRunID != run.id || instance.runInterruptAccepted[run.id] != accepted) {
				h.mu.Unlock()
				t.Fatal("active cancellation did not register the current run interruption")
			}
			h.mu.Unlock()
			if active {
				select {
				case <-taskCtx.Done():
				default:
					t.Fatal("current task cancellation was not invoked")
				}
			}
			<-accepted
			instance.publishWG.Wait()
			eventMu.Lock()
			acceptances := append([]events.Event(nil), currentAcceptances...)
			eventMu.Unlock()
			if len(acceptances) != 1 {
				t.Fatalf("current cancellation acceptance events=%d, want 1 after retry", len(acceptances))
			}
			if active && acceptances[0]["runId"] != run.id {
				t.Fatalf("active cancellation acceptance runId=%v, want %s", acceptances[0]["runId"], run.id)
			}

			if _, err := h.cancelTask(owner, CancelTaskRequest{AgentID: instance.id, CommandID: middle.id}); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				outcome, err := h.waitTask(owner, taskRequest{AgentID: instance.id, CommandID: middle.id}, t.Context())
				if err != nil || outcome.CommandID != middle.id || outcome.State != "cancelled" || outcome.Error != "agent task cancelled" {
					t.Fatalf("retained queued outcome attempt %d: outcome=%+v err=%v", attempt, outcome, err)
				}
			}
			h.mu.Lock()
			if len(instance.queue) != 2 || instance.queue[0] != first || instance.queue[1] != last ||
				instance.commands[middle.id] != middle {
				h.mu.Unlock()
				t.Fatal("queued cancellation changed sibling order or removed the retained command")
			}
			h.mu.Unlock()
		})
	}
}
