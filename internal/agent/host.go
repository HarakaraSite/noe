package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"
)

// Options configures an agent host. Provider and ProviderOptions are used only
// by root hosts. Connection makes this host a scoped client of another host.
type Options struct {
	Provider         provider.Provider
	DecisionProvider decision.Provider
	ImageProvider    imageapi.Provider
	ProviderOptions  provider.Options
	Cwd              string
	Executable       string
	ImportMap        *modulemap.Snapshot // host module configuration; never provider config
	Connection       *Connection
	ControlDir       string // explicit human control registry directory
	EnableControl    bool   // resolve the default directory lazily at first broker use
	Trace            io.Writer
	// Observe receives structured agent events. Calls are serialized and never
	// made while Host.mu is held.
	Observe func(events.Event)
}

// Host owns the provider and broker. It is safe to close concurrently with
// operations and safe to close more than once.
type Host struct {
	mu         sync.Mutex
	opts       Options
	provider   provider.Provider
	catalog    *provider.Registry
	decider    decision.Provider
	imager     imageapi.Provider
	ctx        context.Context
	cancel     context.CancelFunc
	closed     bool
	closeDone  chan struct{}
	broker     *broker
	operations map[*Operation]struct{}
	runs       map[string]*agentRun
	instances  map[string]*agentInstance
	groups     map[string]*executionGroup
	traceMu    sync.Mutex
	observeMu  sync.Mutex
}

// NewHost allocates Go state only. In particular it does not read a key file,
// create a listener, or make a request.
func NewHost(opts Options) *Host {
	ctx, cancel := context.WithCancel(context.Background())
	p := opts.Provider
	var catalog *provider.Registry
	if opts.Connection == nil {
		catalog, _ = p.(*provider.Registry)
		if catalog == nil {
			catalog = provider.NewRegistry(opts.ProviderOptions)
		}
	}
	if p == nil && opts.Connection == nil {
		// The provider itself performs credential lookup lazily on Generate.
		p = catalog
	}
	imager := opts.ImageProvider
	if imager == nil {
		imager, _ = p.(imageapi.Provider)
	}
	return &Host{
		imager:     imager,
		opts:       opts,
		provider:   p,
		catalog:    catalog,
		decider:    decisionProvider(opts.DecisionProvider, p),
		ctx:        ctx,
		cancel:     cancel,
		closeDone:  make(chan struct{}),
		operations: make(map[*Operation]struct{}),
		runs:       make(map[string]*agentRun),
		instances:  make(map[string]*agentInstance),
		groups:     make(map[string]*executionGroup),
	}
}

func (h *Host) checkProvider(name string) error {
	if registry, ok := h.provider.(interface{ HasProvider(string) (bool, error) }); ok {
		exists, err := registry.HasProvider(name)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	} else if name == "openrouter" {
		return nil
	}
	return fmt.Errorf("unknown provider %q", name)
}

func decisionProvider(explicit decision.Provider, chat provider.Provider) decision.Provider {
	if explicit != nil {
		return explicit
	}
	shared, _ := chat.(decision.Provider)
	return shared
}

// Defaults returns a JSON object containing inherited defaults. Root hosts
// return {} and connected hosts return the configuration in their capability.
func (h *Host) Defaults() string {
	defaults := Config{}
	if h.opts.Connection != nil {
		defaults = h.opts.Connection.Defaults
		// Shared group limits are owned by the root group and never become
		// child defaults through a connected runtime.
		defaults.ExecutionLimits = nil
		defaults.executionLimitsPresent = false
	}
	encoded, err := json.Marshal(defaults)
	if err != nil { // Config has no fields that can fail to marshal.
		return "{}"
	}
	return string(encoded)
}

// Close cancels all Go-owned operations, runs and in-flight HTTP work, then
// closes the lazy listener. It never touches a JavaScript runtime.
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
	var closedOps []*Operation
	type hookTerminal struct {
		event     events.Event
		startDone <-chan struct{}
	}
	var hookTerminals []hookTerminal
	for op := range h.operations {
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
		closedOps = append(closedOps, op)
	}
	clear(h.operations)
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
	b := h.broker
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
	if b != nil {
		b.close()
	}
	for _, op := range closedOps {
		op.requests.Wait()
		op.closeDoneOnce.Do(func() { close(op.closeDone) })
	}
	for _, instance := range instances {
		<-instance.done
	}
	h.mu.Lock()
	clear(h.instances)
	clear(h.runs)
	clear(h.groups)
	h.mu.Unlock()
}
