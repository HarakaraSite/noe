// Package agent owns the Go side of the built-in agent module. Its HTTP
// broker is created on the first internal RPC, so importing the public module
// does not require credentials or open a listener.
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

// Options configures an agent host. Provider and ProviderOptions are used only
// by root hosts. Connection makes this host a scoped client of another host.
type Options struct {
	Provider        provider.Provider
	ProviderOptions provider.Options
	Cwd             string
	Executable      string
	Connection      *Connection
	Trace           io.Writer
	// Observe receives structured agent events. Calls are serialized and never
	// made while Host.mu is held.
	Observe func(events.Event)
}

// Config contains inheritable public agent defaults. Empty fields are omitted
// so a root host exposes an empty object.
type Config struct {
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	Name      string `json:"name,omitempty"`
	System    string `json:"system,omitempty"`
	MaxTokens int    `json:"maxTokens,omitempty"`
	Effort    string `json:"effort,omitempty"`
	Cwd       string `json:"cwd,omitempty"`
}

// Connection is the capability a child process receives from its parent.
// JSON tags are intentionally stable because the CLI passes this object via an
// environment variable to a future runner.
type Connection struct {
	URL      string `json:"url"`
	Token    string `json:"token"`
	Scope    string `json:"scope"`
	Defaults Config `json:"defaults"`
}

// Host owns the provider and broker. It is safe to close concurrently with
// operations and safe to close more than once.
type Host struct {
	mu         sync.Mutex
	opts       Options
	provider   provider.Provider
	ctx        context.Context
	cancel     context.CancelFunc
	closed     bool
	closeDone  chan struct{}
	broker     *broker
	operations map[*Operation]struct{}
	runs       map[string]*agentRun
	traceMu    sync.Mutex
	observeMu  sync.Mutex
}

// Operation is the cancellation and scope boundary for one runtime evaluation.
// It contains no QuickJS values and may be used by Go worker goroutines.
type Operation struct {
	host        *Host
	ctx         context.Context
	cancel      context.CancelFunc
	stopHost    func() bool
	scope       string
	initErr     error
	closed      bool // protected by host.mu
	remoteBase  string
	remoteAuth  string
	client      *http.Client
	parentRunID string

	requestMu      sync.Mutex
	requestClosing bool
	requests       sync.WaitGroup
	closeDone      chan struct{}
	closeDoneOnce  sync.Once
}

type endedRun struct {
	id       string
	parentID string
	name     string
	model    string
	started  time.Time
}

// NewHost allocates Go state only. In particular it does not read a key file,
// create a listener, or make a request.
func NewHost(opts Options) *Host {
	ctx, cancel := context.WithCancel(context.Background())
	p := opts.Provider
	if p == nil && opts.Connection == nil {
		// The provider itself performs credential lookup lazily on Generate.
		p = provider.NewOpenRouter(opts.ProviderOptions)
	}
	return &Host{
		opts:       opts,
		provider:   p,
		ctx:        ctx,
		cancel:     cancel,
		closeDone:  make(chan struct{}),
		operations: make(map[*Operation]struct{}),
		runs:       make(map[string]*agentRun),
	}
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

// Defaults returns a JSON object containing inherited defaults. Root hosts
// return {} and connected hosts return the configuration in their capability.
func (h *Host) Defaults() string {
	defaults := Config{}
	if h.opts.Connection != nil {
		defaults = h.opts.Connection.Defaults
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
	for op := range h.operations {
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
	for id, run := range h.runs {
		run.cancel()
		ended = append(ended, endedRun{
			id: id, parentID: run.parentRunID, name: run.config.Name,
			model: run.config.Model, started: run.started,
		})
		delete(h.runs, id)
	}
	b := h.broker
	h.mu.Unlock()
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
	var childScopes []*Operation
	for id, run := range h.runs {
		if run.operation == op {
			run.cancel()
			ended = append(ended, endedRun{
				id: id, parentID: run.parentRunID, name: run.config.Name,
				model: run.config.Model, started: run.started,
			})
			for childScope := range run.childScopes {
				childScopes = append(childScopes, childScope)
			}
			delete(h.runs, id)
		}
	}
	h.mu.Unlock()
	for _, childScope := range childScopes {
		childScope.Close()
	}
	op.requests.Wait()
	for _, run := range ended {
		h.traceEvent(traceEvent{
			RunID: run.id, ParentID: run.parentID, Name: run.name, Model: run.model,
			Status: "cancelled", Duration: time.Since(run.started),
		})
		h.observeEndedRun("agent.cancelled", run, events.Event{
			"status": "cancelled", "duration": time.Since(run.started),
		})
	}
	op.closeDoneOnce.Do(func() { close(op.closeDone) })
}

func (h *Host) observe(event events.Event) {
	if h.opts.Observe == nil {
		return
	}
	h.observeMu.Lock()
	defer h.observeMu.Unlock()
	h.opts.Observe(event)
}

func (h *Host) observeRun(eventType, runID string, run *agentRun, fields events.Event) {
	event := events.Event{
		"type": eventType, "runId": runID,
		"parentRunId": run.parentRunID, "name": run.config.Name, "model": run.config.Model,
	}
	for key, value := range fields {
		event[key] = value
	}
	h.observe(event)
}

func (h *Host) observeEndedRun(eventType string, run endedRun, fields events.Event) {
	event := events.Event{
		"type": eventType, "runId": run.id,
		"parentRunId": run.parentID, "name": run.name, "model": run.model,
	}
	for key, value := range fields {
		event[key] = value
	}
	h.observe(event)
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

// randomID returns an unpredictable lowercase hexadecimal identifier.
func randomID() (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

var (
	ErrHostClosed      = errors.New("agent host is closed")
	ErrOperationClosed = errors.New("agent operation is closed")
)
