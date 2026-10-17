package agent

import (
	"context"
	"net/http"
	"strings"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

type agentRun struct {
	id             string
	operation      *Operation
	ctx            context.Context
	cancel         context.CancelFunc
	started        time.Time
	config         runConfig
	parentRunID    string
	childScopes    map[*Operation]struct{}
	instances      map[string]*agentInstance
	instance       *agentInstance
	steerByID      map[string]*steerCommand
	steerOrder     []*steerCommand
	finalAnswer    string
	finalDone      chan struct{}
	finalClosed    bool
	finalEventDone bool
	providerCalls  int
	interrupting   bool
}

func (r *agentRun) agentID() string {
	if r.instance != nil {
		return r.instance.id
	}
	return ""
}

type runConfig struct {
	Provider  string
	Model     string
	Name      string
	MaxTokens int
	Effort    string
	MaxSteps  int
}

type startRequest struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Name      string `json:"name"`
	MaxTokens int    `json:"maxTokens"`
	Effort    string `json:"effort,omitempty"`
	MaxSteps  int    `json:"maxSteps,omitempty"`
}

type finishRequest struct {
	RunID  string `json:"runId"`
	Status string `json:"status"`
	Answer string `json:"answer,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (b *broker) handleStart(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request startRequest
	err := decodeRequest(r, &request)
	effort, validEffort := provider.NormalizeEffort(request.Effort)
	if err != nil || strings.TrimSpace(request.Provider) == "" || strings.TrimSpace(request.Model) == "" || strings.TrimSpace(request.Name) == "" || request.MaxTokens < 0 || request.MaxSteps < 0 || !validEffort {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid start request")
		return
	}
	if err := b.host.checkProvider(request.Provider); err != nil {
		writeRPCError(w, http.StatusBadRequest, "provider", err.Error())
		return
	}
	id, err := randomID()
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate agent run")
		return
	}
	ctx, cancel := context.WithCancel(op.ctx)
	config := runConfig{
		Provider: request.Provider, Model: request.Model,
		Name: request.Name, MaxTokens: request.MaxTokens, Effort: effort, MaxSteps: request.MaxSteps,
	}
	run := &agentRun{
		id:        id,
		operation: op, ctx: ctx, cancel: cancel, started: time.Now(),
		config: config, parentRunID: op.parentRunID,
		childScopes: make(map[*Operation]struct{}),
		instances:   make(map[string]*agentInstance),
		steerByID:   make(map[string]*steerCommand),
		finalDone:   make(chan struct{}),
	}
	h := b.host
	h.mu.Lock()
	if op.closed || op.ctx.Err() != nil || h.closed {
		h.mu.Unlock()
		cancel()
		writeRPCError(w, http.StatusConflict, "scope", "agent operation has ended")
		return
	}
	if op.task != nil {
		instance := op.task.instance
		if instance.state == "stopping" || instance.state == "stopped" || instance.state == "failed" || instance.ctx.Err() != nil {
			h.mu.Unlock()
			cancel()
			writeRPCError(w, http.StatusConflict, "scope", "agent instance is stopping or finished")
			return
		}
	}
	h.runs[id] = run
	if op.task != nil {
		if op.task.run != nil {
			delete(h.runs, id)
			h.mu.Unlock()
			cancel()
			writeRPCError(w, http.StatusConflict, "run", "task scope already claimed a run")
			return
		}
		run.instance = op.task.instance
		op.task.run = run
		op.task.instance.currentRunID = id
	}
	h.mu.Unlock()
	h.traceEvent(traceEvent{
		RunID: id, ParentID: run.parentRunID, Name: config.Name, Model: config.Model,
		Status: "started",
	})
	h.observeRun("agent.started", id, run, events.Event{"status": "started"})
	writeJSON(w, map[string]string{"runId": id})
}

func (b *broker) handleFinish(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request finishRequest
	if err := decodeRequest(r, &request); err != nil || request.RunID == "" {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid finish request")
		return
	}
	status := sanitizeRunStatus(request.Status)
	h := b.host
	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || run.operation != op {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "unknown agent run")
		return
	}
	if run.finalClosed && status != "completed" {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "final", "a committed final answer cannot be replaced by a failed finish")
		return
	}
	var emitFinal bool
	var finalSettled []<-chan struct{}
	var waitFinal <-chan struct{}
	if status == "completed" {
		if run.finalClosed {
			if strings.TrimSpace(request.Answer) == "" || request.Answer != run.finalAnswer {
				h.mu.Unlock()
				writeRPCError(w, http.StatusConflict, "final", "finish answer does not match the committed final answer")
				return
			}
			waitFinal = run.finalDone
		} else if strings.TrimSpace(request.Answer) != "" {
			// Preserve direct-run finish behavior for existing internal callers.
			// Managed task runners must pass the final-answer gate first.
			if run.instance != nil {
				h.mu.Unlock()
				writeRPCError(w, http.StatusConflict, "final", "managed task must commit its final answer first")
				return
			}
			for _, command := range run.steerOrder {
				if command.status == "accepted" || command.status == "delivered" {
					h.mu.Unlock()
					writeRPCError(w, http.StatusConflict, "final", "pending steer commands must be applied before finishing")
					return
				}
			}
			run.finalClosed = true
			run.finalAnswer = request.Answer
			emitFinal = true
			for _, command := range run.steerOrder {
				if command.status == "applied" {
					finalSettled = append(finalSettled, command.settled)
				}
			}
		} else if run.instance != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "final", "managed task completion requires a committed answer")
			return
		}
	}
	pending := h.cancelPendingSteersLocked(run)
	delete(h.runs, request.RunID)
	if op.task != nil && op.task.run == run {
		report := request
		op.task.report = &report
	}
	run.cancel()
	childScopes := make([]*Operation, 0, len(run.childScopes))
	for childScope := range run.childScopes {
		childScopes = append(childScopes, childScope)
	}
	clear(run.childScopes)
	instances := make([]*agentInstance, 0, len(run.instances))
	for _, instance := range run.instances {
		instances = append(instances, instance)
	}
	h.mu.Unlock()
	h.emitSteerEvents(pending)
	if emitFinal {
		for _, eventDone := range finalSettled {
			<-eventDone
		}
		h.observeRun("agent.final", run.id, run, events.Event{
			"status": "completed", "text": request.Answer,
		})
		h.mu.Lock()
		if !run.finalEventDone {
			run.finalEventDone = true
			close(run.finalDone)
		}
		h.mu.Unlock()
	} else if waitFinal != nil {
		<-waitFinal
	}
	for _, childScope := range childScopes {
		childScope.Close()
	}
	for _, instance := range instances {
		<-instance.done
	}
	if run.instance != nil {
		writeJSON(w, map[string]bool{})
		return
	}
	h.traceEvent(traceEvent{
		RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
		Status: status, Duration: time.Since(run.started),
	})
	fields := events.Event{
		"status": status, "duration": time.Since(run.started),
	}
	if status == "failed" && request.Error != "" {
		fields["error"] = request.Error
	}
	h.observeRun("agent."+status, request.RunID, run, fields)
	writeJSON(w, map[string]bool{})
}

func sanitizeRunStatus(status string) string {
	switch status {
	case "completed", "failed", "cancelled":
		return status
	default:
		return "failed"
	}
}
