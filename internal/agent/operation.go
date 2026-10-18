package agent

import (
	"context"
	"net/http"
	"sync"
	"time"

	"noe/internal/events"
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
	remoteBase       string
	remoteAuth       string
	client           *http.Client
	parentRunID      string
	ownerRun         *agentRun
	hookScope        bool
	hookInvocation   *hookInvocation
	group            *executionGroup
	causeActivityIDs []string
	activityOrigin   activityOrigin
	imageOwner       *imageOwner
	decisionOwner    events.Event              // immutable after the run_js scope is registered
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
// network or file I/O. A local root gets a fresh scope; a connected host uses
// the scope granted by its parent broker.
func (h *Host) NewOperation(parent context.Context) *Operation {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	scope := ""
	var scopeErr error
	remoteBase, remoteAuth := "", ""
	if h.opts.Connection != nil {
		scope = h.opts.Connection.Scope
		remoteBase = h.opts.Connection.URL
		remoteAuth = h.opts.Connection.Token
		if remoteBase == "" || remoteAuth == "" || scope == "" {
			scopeErr = &RPCError{Code: "connection", Message: "agent connection is incomplete"}
		} else {
			scopeErr = validateConnectionURL(remoteBase)
		}
	} else {
		scope, scopeErr = randomID()
	}
	op := &Operation{
		host: h, ctx: ctx, cancel: cancel, scope: scope,
		remoteBase: remoteBase, remoteAuth: remoteAuth,
		instances: make(map[string]*agentInstance),
		closeDone: make(chan struct{}),
		// A zero Timeout is deliberate: long model calls are governed solely by
		// the operation context and explicit runtime cancellation.
		client: &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			// Never forward a broker capability to a redirect target.
			return http.ErrUseLastResponse
		}},
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
	if op.stopHost != nil {
		op.stopHost()
	}
	delete(h.operations, op)
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
	// closes no future evaluation is authorized to read them (notably in REPL).
	if op.task == nil && op.parentRunID == "" {
		h.mu.Lock()
		for id, instance := range h.instances {
			if h.canManageLocked(op, instance) {
				delete(h.instances, id)
			}
		}
		h.mu.Unlock()
	}
	op.closeDoneOnce.Do(func() { close(op.closeDone) })
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
