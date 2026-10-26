package agent

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

// Options configures an agent host. The host borrows its model service and
// executor and never discovers process-global settings or starts a child host.
type Options struct {
	Models       model.Service
	Context      context.Context
	Execute      Executor
	Instructions func(context.Context) (string, error)
	Policy       string
	// RuntimeFacts describes the execution profile, not application policy.
	RuntimeFacts string
	Trace        io.Writer
	// Observe receives structured agent events. Calls are serialized and never
	// made while Host.mu is held.
	Observe func(events.Event)
}

func (h *Host) loadInstructions(ctx context.Context) (string, error) {
	var instructions string
	var err error
	if h.opts.Instructions != nil {
		instructions, err = h.opts.Instructions(ctx)
	}
	if err != nil {
		return "", err
	}
	if h.opts.Policy == "" {
		return instructions, nil
	}
	if instructions == "" {
		return h.opts.Policy, nil
	}
	return h.opts.Policy + "\n\n" + instructions, nil
}

// loadInstanceInstructions snapshots the host-supplied source once for an
// agent instance. The callback runs without Host.mu held and its error is
// retained just like a successful body so queued tasks observe the same setup.
func (h *Host) loadInstanceInstructions(ctx context.Context, instance *agentInstance) (string, error) {
	if instance == nil {
		return h.loadInstructions(ctx)
	}
	for {
		h.mu.Lock()
		if instance.instructionLoaded {
			text, err := instance.instructionText, instance.instructionErr
			h.mu.Unlock()
			return text, err
		}
		if waiting := instance.instructionLoading; waiting != nil {
			h.mu.Unlock()
			select {
			case <-waiting:
				continue
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		waiting := make(chan struct{})
		instance.instructionLoading = waiting
		h.mu.Unlock()

		text, err := h.loadInstructions(ctx)
		if err == nil && ctx.Err() != nil {
			text, err = "", ctx.Err()
		}

		h.mu.Lock()
		instance.instructionText = text
		instance.instructionErr = err
		instance.instructionLoaded = true
		instance.instructionLoading = nil
		close(waiting)
		h.mu.Unlock()
		return text, err
	}
}

func (h *Host) instructionFacts() string {
	facts := Facts()
	if h.opts.RuntimeFacts != "" {
		facts += "\n\n" + h.opts.RuntimeFacts
	}
	return facts
}

// Host owns agent state. It is safe to close concurrently with
// operations and safe to close more than once.
type Host struct {
	mu                sync.Mutex
	opts              Options
	provider          provider.Provider
	decider           decision.Provider
	imager            imageapi.Provider
	ctx               context.Context
	cancel            context.CancelFunc
	closed            bool
	closeDone         chan struct{}
	operations        map[*Operation]struct{}
	closingOperations map[*Operation]struct{}
	runs              map[string]*agentRun
	instances         map[string]*agentInstance
	groups            map[string]*executionGroup
	traceMu           sync.Mutex
	observeMu         sync.Mutex
}

// NewHost allocates Go state only. In particular it does not read a key file,
// create a listener, or make a request.
func NewHost(opts Options) *Host {
	parent := opts.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	var p provider.Provider
	var imager imageapi.Provider
	var decider decision.Provider
	if opts.Models != nil {
		adapter := directModelProvider{service: opts.Models}
		p = adapter
		imager = adapter
		decider = adapter
	}
	return &Host{
		imager:            imager,
		opts:              opts,
		provider:          p,
		decider:           decider,
		ctx:               ctx,
		cancel:            cancel,
		closeDone:         make(chan struct{}),
		operations:        make(map[*Operation]struct{}),
		closingOperations: make(map[*Operation]struct{}),
		runs:              make(map[string]*agentRun),
		instances:         make(map[string]*agentInstance),
		groups:            make(map[string]*executionGroup),
	}
}

func (h *Host) checkProvider(ctx context.Context, name string) error {
	if h.opts.Models == nil {
		return fmt.Errorf("agent host has no model service")
	}
	if ctx == nil {
		return fmt.Errorf("agent provider check requires a context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	models, err := h.opts.Models.ListModels(ctx, model.ModelQuery{})
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		if allowed, checked, checkErr := checkModelProvider(h.opts.Models, name); checked {
			if checkErr != nil {
				return checkErr
			}
			if allowed {
				return nil
			}
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, entry := range models {
		if entry.Provider == name {
			return nil
		}
	}
	// A model service may choose a provider that is not represented by its
	// catalog (or may expose an empty catalog). When it offers an explicit
	// provider check, let that service classify the name after the
	// context-aware catalog query has completed.
	if exists, checked, checkErr := checkModelProvider(h.opts.Models, name); checked {
		if checkErr != nil {
			return checkErr
		}
		if exists {
			return nil
		}
		return fmt.Errorf("unknown provider %q", name)
	}
	if len(models) == 0 {
		// Services without a catalog own provider validation in Generate.
		return nil
	}
	return fmt.Errorf("unknown provider %q", name)
}

func checkModelProvider(service model.Service, name string) (exists, checked bool, err error) {
	checker, ok := service.(interface {
		HasProvider(string) (bool, error)
	})
	if !ok {
		return false, false, nil
	}
	exists, err = checker.HasProvider(name)
	return exists, true, err
}

// requestOperationContext combines one request's lifetime with its owning
// operation. Direct provider catalog calls must stop when either ends.
func requestOperationContext(requestCtx context.Context, op *Operation) (context.Context, context.CancelFunc) {
	if requestCtx == nil {
		requestCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(requestCtx)
	if op == nil || op.ctx == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(op.ctx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// Close cancels and joins all Go-owned operations and runs. It never touches a
// JavaScript runtime or borrowed resources.
func (h *Host) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		<-h.closeDone
		return
	}
	h.closed = true
	defer close(h.closeDone)
	h.cancel()
	type operationJoin struct {
		op        *Operation
		waitClose bool
	}
	var closedOps []operationJoin
	type hookTerminal struct {
		event     events.Event
		startDone <-chan struct{}
	}
	var hookTerminals []hookTerminal
	joinOperation := func(op *Operation, waitClose bool) {
		if op.hookInvocation != nil && op.hookInvocation.state == "active" {
			op.hookInvocation.state = "cancelled"
			hookTerminals = append(hookTerminals, hookTerminal{
				event:     terminalHookEventLocked(op.hookInvocation, "cancelled", "", op.group),
				startDone: op.hookInvocation.startEventDone,
			})
		}
		op.closed = true
		op.cancel()
		op.closeRequestGate()
		if op.stopHost != nil {
			op.stopHost()
		}
		closedOps = append(closedOps, operationJoin{op: op, waitClose: waitClose})
	}
	for op := range h.operations {
		joinOperation(op, false)
	}
	for op := range h.closingOperations {
		joinOperation(op, true)
	}
	clear(h.operations)
	clear(h.closingOperations)
	var ended []endedRun
	var pendingSteers []steerLifecycleEvent
	for id, run := range h.runs {
		discardStepImages(run.modelStep)
		run.images = nil
		run.cancel()
		pendingSteers = append(pendingSteers, h.cancelPendingSteersLocked(run)...)
		ended = append(ended, endedRun{
			id: id, agentID: run.agentID(), parentID: run.parentRunID, name: run.config.Name,
			model: run.config.Model, started: run.started, group: run.group,
			activityID: run.activityID, causedBy: append([]string(nil), run.causedBy...), origin: run.origin,
		})
		if run.instance == nil {
			delete(h.runs, id)
		} else {
			ended = ended[:len(ended)-1] // managed task emits its terminal after process reap
		}
	}
	instances := make([]*agentInstance, 0, len(h.instances))
	for _, instance := range h.instances {
		instances = append(instances, instance)
	}
	h.mu.Unlock()
	for _, terminal := range hookTerminals {
		if terminal.startDone != nil {
			<-terminal.startDone
		}
		h.observe(terminal.event)
	}
	h.emitSteerEvents(pendingSteers)
	for _, run := range ended {
		h.traceEvent(traceEvent{
			RunID: run.id, ParentID: run.parentID, Name: run.name, Model: run.model,
			Status: "cancelled", Duration: time.Since(run.started),
		})
		h.observeEndedRun("agent.cancelled", run, events.Event{
			"status": "cancelled", "duration": time.Since(run.started),
		})
	}
	for _, joined := range closedOps {
		joined.op.requests.Wait()
		if !joined.waitClose {
			joined.op.closeDoneOnce.Do(func() { close(joined.op.closeDone) })
		}
	}
	for _, instance := range instances {
		<-instance.done
		instance.publishWG.Wait()
	}
	for _, joined := range closedOps {
		if joined.waitClose {
			<-joined.op.closeDone
		}
	}
	h.mu.Lock()
	clear(h.instances)
	clear(h.runs)
	clear(h.groups)
	h.mu.Unlock()
}
