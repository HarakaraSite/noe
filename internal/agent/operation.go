package agent

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

// Operation is the cancellation and scope boundary for one runtime evaluation.
// It contains no QuickJS values and may be used by Go worker goroutines.
type Operation struct {
	host             *Host
	ctx              context.Context
	cancel           context.CancelFunc
	stopHost         func() bool
	scope            string
	initErr          error
	closed           bool // protected by host.mu
	parentRunID      string
	ownerRun         *agentRun
	hookScope        bool
	hookInvocation   *hookInvocation
	hookFatalError   error // sticky hook-scoped infrastructure failure; host.mu
	group            *executionGroup
	causeActivityIDs []string
	activityOrigin   activityOrigin
	imageOwner       *imageOwner
	decisionOwner    events.Event              // immutable after the run_js scope is registered
	resolvedHooks    map[int]resolvedHook      // direct task-loader resolutions owned by this scope
	instances        map[string]*agentInstance // direct evaluation-owned instances; host.mu
	task             *managedTask              // task bootstrap scope; host.mu
	control          bool                      // trusted host control, never delegated to JS

	requestMu      sync.Mutex
	requestClosing bool
	requests       sync.WaitGroup
	closeDone      chan struct{}
	closeDoneOnce  sync.Once
}

// NewOperation starts an independent cancellation scope without performing
// network or file I/O.
func (h *Host) NewOperation(parent context.Context) *Operation {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	scope, scopeErr := randomID()
	op := &Operation{
		host: h, ctx: ctx, cancel: cancel, scope: scope,
		instances: make(map[string]*agentInstance),
		closeDone: make(chan struct{}),
	}
	op.initErr = scopeErr
	op.stopHost = context.AfterFunc(h.ctx, cancel)
	h.mu.Lock()
	if h.closed {
		op.closed = true
		cancel()
		op.stopHost()
		op.closeRequestGate()
		op.closeDoneOnce.Do(func() { close(op.closeDone) })
	} else {
		h.operations[op] = struct{}{}
	}
	h.mu.Unlock()
	return op
}

// NewControlOperation starts a trusted Go-management scope. It is intended for
// the public Host's administrative methods and must never be exposed to JS.
func (h *Host) NewControlOperation(parent context.Context) *Operation {
	op := h.NewOperation(parent)
	h.mu.Lock()
	if !op.closed {
		op.control = true
	}
	h.mu.Unlock()
	return op
}

// Close cancels this operation and all runs it owns. It is idempotent.
func (op *Operation) Close() {
	if op == nil || op.host == nil {
		return
	}
	h := op.host
	h.mu.Lock()
	if op.closed {
		done := op.closeDone
		hostClosed := h.closed
		h.mu.Unlock()
		if !hostClosed && done != nil {
			<-done
		}
		return
	}
	op.closed = true
	op.cancel()
	op.closeRequestGate()
	delete(h.operations, op)
	h.closingOperations[op] = struct{}{}
	if op.stopHost != nil {
		op.stopHost()
	}
	var ended []endedRun
	var pendingSteers []steerLifecycleEvent
	var childScopes []*Operation
	var instances []*agentInstance
	var terminalHookEvent events.Event
	var hookStartEventDone <-chan struct{}
	if op.hookInvocation != nil && op.hookInvocation.state == "active" {
		op.hookInvocation.state = "cancelled"
		terminalHookEvent = terminalHookEventLocked(op.hookInvocation, "cancelled", "", op.group)
		hookStartEventDone = op.hookInvocation.startEventDone
	}
	for _, instance := range op.instances {
		instances = append(instances, instance)
	}
	for id, run := range h.runs {
		if run.operation == op {
			discardStepImages(run.modelStep)
			run.images = nil
			run.cancel()
			pendingSteers = append(pendingSteers, h.cancelPendingSteersLocked(run)...)
			if run.instance == nil {
				ended = append(ended, endedRun{
					id: id, parentID: run.parentRunID, name: run.config.Name,
					model: run.config.Model, started: run.started, group: run.group,
					activityID: run.activityID, causedBy: append([]string(nil), run.causedBy...), origin: run.origin,
				})
			}
			for _, instance := range run.instances {
				instances = append(instances, instance)
			}
			for childScope := range run.childScopes {
				childScopes = append(childScopes, childScope)
			}
			delete(h.runs, id)
		}
	}
	if op.ownerRun != nil {
		delete(op.ownerRun.childScopes, op)
		op.ownerRun = nil
	}
	h.mu.Unlock()
	if terminalHookEvent != nil {
		if hookStartEventDone != nil {
			<-hookStartEventDone
		}
		h.observe(terminalHookEvent)
	}
	h.emitSteerEvents(pendingSteers)
	for _, childScope := range childScopes {
		childScope.Close()
	}
	op.requests.Wait()
	for _, instance := range instances {
		<-instance.done
		instance.publishWG.Wait()
	}
	for _, run := range ended {
		h.traceEvent(traceEvent{
			RunID: run.id, ParentID: run.parentID, Name: run.name, Model: run.model,
			Status: "cancelled", Duration: time.Since(run.started),
		})
		h.observeEndedRun("agent.cancelled", run, events.Event{
			"status": "cancelled", "duration": time.Since(run.started),
		})
	}
	// Results remain observable for the owning evaluation. After that scope
	// closes, no later operation is authorized to read them.
	if op.task == nil && op.parentRunID == "" {
		h.mu.Lock()
		for id, instance := range h.instances {
			if h.canManageLocked(op, instance) {
				delete(h.instances, id)
			}
		}
		h.mu.Unlock()
	}
	// Signal completion before dropping the join record. Host.Close either sees
	// this closing operation and waits for the signal, or sees it removed after
	// all Go-owned cleanup has finished.
	op.closeDoneOnce.Do(func() { close(op.closeDone) })
	h.mu.Lock()
	delete(h.closingOperations, op)
	h.mu.Unlock()
}

// CloseClient cancels and joins a client-owned scope. If Host.Close already
// owns shutdown, it waits for the Host's full join as well. Internal task and
// tool execution scopes must use Close so Host.Close can finish their owner
// instances without forming a shutdown cycle.
func (op *Operation) CloseClient() {
	if op == nil || op.host == nil {
		return
	}
	op.Close()

	h := op.host
	h.mu.Lock()
	hostClosing := h.closed
	hostDone := h.closeDone
	opDone := op.closeDone
	h.mu.Unlock()
	if hostClosing {
		<-hostDone
		return
	}
	if opDone != nil {
		<-opDone
	}
}

func (op *Operation) beginRequest() bool {
	op.requestMu.Lock()
	defer op.requestMu.Unlock()
	if op.requestClosing {
		return false
	}
	op.requests.Add(1)
	return true
}

func (op *Operation) endRequest() { op.requests.Done() }

func (op *Operation) closeRequestGate() {
	op.requestMu.Lock()
	op.requestClosing = true
	op.requestMu.Unlock()
}

// Context is the lifetime of the current runtime evaluation's native work.
func (op *Operation) Context() context.Context { return op.ctx }

// Defaults returns the model and run settings inherited by this execution.
// It deliberately excludes host administration and execution-group limits,
// which are Host-owned rather than task defaults.
func (op *Operation) Defaults() string {
	if op == nil || op.host == nil {
		return "{}"
	}
	h := op.host
	h.mu.Lock()
	var config Config
	if op.task != nil && op.task.instance != nil {
		config = publicConfig(op.task.instance.config.Config)
	} else if owner := h.currentOwnerRunLocked(op); owner != nil {
		config = configFromRun(owner.config)
	}
	h.mu.Unlock()
	encoded, err := json.Marshal(config)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

func publicConfig(config Config) Config {
	return Config{
		Provider: config.Provider, Model: config.Model,
		MaxTokens: config.MaxTokens, Effort: config.Effort,
		Hooks:                cloneHookModules(config.hookModules()),
		ContextBudget:        cloneContextBudget(config.ContextBudget),
		contextBudgetPresent: config.contextBudgetPresent || config.ContextBudget != nil,
		ContextPolicy:        cloneContextPolicy(config.ContextPolicy),
		contextPolicyPresent: config.contextPolicyPresent || config.ContextPolicy != nil,
	}
}

func configFromRun(run runConfig) Config {
	return Config{
		Provider: run.Provider, Model: run.Model, Name: run.Name,
		MaxTokens: run.MaxTokens, Effort: run.Effort,
		Hooks:                cloneHookModules(run.Hooks),
		ContextBudget:        cloneContextBudget(run.ContextBudget),
		contextBudgetPresent: run.ContextBudgetPresent || run.ContextBudget != nil,
		ContextPolicy:        cloneContextPolicy(run.ContextPolicy),
		contextPolicyPresent: run.ContextPolicyPresent || run.ContextPolicy != nil,
	}
}
