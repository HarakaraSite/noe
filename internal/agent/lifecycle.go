package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

// taskConfig is frozen at registration and copied into each fresh task runner.
type taskConfig struct {
	Config
	MaxSteps int     `json:"maxSteps"`
	CacheKey *string `json:"cacheKey,omitempty"`
}

type spawnRequest struct {
	Config      taskConfig `json:"config"`
	Instruction *string    `json:"instruction"`
	KeepAlive   bool       `json:"keepAlive"`
}

type instanceRequest struct {
	AgentID string `json:"agentId"`
}
type queueRequest struct {
	AgentID     string `json:"agentId"`
	Instruction string `json:"instruction"`
	CommandID   string `json:"commandId,omitempty"`
}

type command struct {
	id          string
	instruction string
	operation   string
	status      string
	accepted    chan struct{}
}

type taskOutcome struct {
	RunID     string `json:"runId"`
	CommandID string `json:"commandId"`
	State     string `json:"state"`
	Answer    string `json:"answer"`
	Error     string `json:"error,omitempty"`
}

type instanceSnapshot struct {
	AgentID      string       `json:"agentId"`
	ParentRunID  string       `json:"parentRunId"`
	Name         string       `json:"name"`
	State        string       `json:"state"`
	Reason       string       `json:"reason"`
	KeepAlive    bool         `json:"keepAlive"`
	CurrentRunID string       `json:"currentRunId"`
	QueueLength  int          `json:"queueLength"`
	LastResult   *taskOutcome `json:"lastResult"`
}

type agentInstance struct {
	host       *Host
	id         string
	config     taskConfig
	configJSON json.RawMessage
	keepAlive  bool
	owner      *Operation
	parent     *agentRun
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	wake       chan struct{}
	// Mutable fields below are protected by Host.mu.
	state                string
	reason               string
	queue                []*command
	commands             map[string]*command
	currentRunID         string
	lastResult           *taskOutcome
	taskCancel           context.CancelFunc
	interruptedRunID     string
	runInterruptAccepted map[string]chan struct{}
	interruptAccepted    chan struct{}
	interruptSettled     chan struct{}
}

type managedTask struct {
	instance *agentInstance
	command  *command
	run      *agentRun
	report   *finishRequest
}

func (b *broker) handleSpawn(w http.ResponseWriter, r *http.Request, op *Operation) {
	var req spawnRequest
	if err := decodeRequest(r, &req); err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid agent configuration")
		return
	}
	effort, valid := provider.NormalizeEffort(req.Config.Effort)
	if strings.TrimSpace(req.Config.Provider) == "" || strings.TrimSpace(req.Config.Model) == "" || strings.TrimSpace(req.Config.Name) == "" || req.Config.Effort == "" || !valid || req.Config.MaxTokens < 0 || req.Config.MaxSteps < 0 || (!req.KeepAlive && req.Instruction == nil) || (req.Instruction != nil && strings.TrimSpace(*req.Instruction) == "") {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid agent configuration or instruction")
		return
	}
	if err := b.host.checkProvider(req.Config.Provider); err != nil {
		writeRPCError(w, http.StatusBadRequest, "provider", err.Error())
		return
	}
	req.Config.Effort = effort
	encoded, err := json.Marshal(req.Config)
	if err != nil {
		writeRPCError(w, 500, "broker", "could not encode agent configuration")
		return
	}
	id, err := randomID()
	if err != nil {
		writeRPCError(w, 500, "broker", "could not allocate agent ID")
		return
	}
	var initial *command
	if req.Instruction != nil {
		commandID, err := randomID()
		if err != nil {
			writeRPCError(w, 500, "broker", "could not allocate task ID")
			return
		}
		initial = &command{id: commandID, instruction: *req.Instruction, operation: "queue", status: "accepted", accepted: make(chan struct{})}
	}
	h := b.host
	h.mu.Lock()
	if op.closed || op.ctx.Err() != nil || h.closed {
		h.mu.Unlock()
		writeRPCError(w, 409, "scope", "agent operation has ended")
		return
	}
	ownerCtx := op.ctx
	var parent *agentRun
	if op.parentRunID != "" {
		parent = h.runs[op.parentRunID]
		if parent == nil || parent.ctx.Err() != nil {
			h.mu.Unlock()
			writeRPCError(w, 409, "scope", "parent agent run has ended")
			return
		}
		ownerCtx = parent.ctx
	}
	ctx, cancel := context.WithCancel(ownerCtx)
	instance := &agentInstance{host: h, id: id, config: req.Config, configJSON: encoded, keepAlive: req.KeepAlive, owner: op, parent: parent, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), state: "starting", commands: make(map[string]*command), runInterruptAccepted: make(map[string]chan struct{})}
	if initial != nil {
		instance.queue = append(instance.queue, initial)
		instance.commands[initial.id] = initial
	}
	h.instances[id] = instance
	if parent != nil {
		parent.instances[id] = instance
	} else {
		op.instances[id] = instance
	}
	h.mu.Unlock()
	instance.event("instance.started", events.Event{})
	if initial != nil {
		instance.commandEvent(initial, "accepted", "")
		close(initial.accepted)
	}
	go instance.serve()
	writeJSON(w, map[string]string{"agentId": id})
}

// Access follows ownership, not the short-lived process which sent spawn.
// An owning evaluation can also manage its nested descendants.
func (h *Host) canManageLocked(op *Operation, instance *agentInstance) bool {
	if op.control {
		return true
	}
	// A generated scope belongs to the run that launched it. Let that run
	// manage its own instance without granting sibling scopes access.
	if instance.currentRunID != "" && op.parentRunID == instance.currentRunID {
		return true
	}
	if instance.parent == nil {
		return instance.owner == op
	}
	for parent := instance.parent; parent != nil; {
		if parent.operation == op || parent.id == op.parentRunID {
			return true
		}
		if parent.instance == nil {
			break
		}
		if parent.instance.parent == nil && parent.instance.owner == op {
			return true
		}
		parent = parent.instance.parent
	}
	return false
}

// canManageRunLocked applies the same owner and parent-scope relationship used
// for managed instances to an active task run. The run's own operation and a
// generated scope created by that run may manage it directly.
func (h *Host) canManageRunLocked(op *Operation, run *agentRun) bool {
	if op == nil || run == nil {
		return false
	}
	if op.control {
		return true
	}
	if run.operation == op || op.parentRunID == run.id {
		return true
	}
	if run.instance != nil && h.canManageLocked(op, run.instance) {
		return true
	}
	for parentID := run.parentRunID; parentID != ""; {
		parent := h.runs[parentID]
		if parent == nil {
			break
		}
		if parent.operation == op || op.parentRunID == parent.id {
			return true
		}
		if parent.instance != nil && h.canManageLocked(op, parent.instance) {
			return true
		}
		parentID = parent.parentRunID
	}
	return false
}

func (b *broker) handleInstance(w http.ResponseWriter, r *http.Request, op *Operation, wait bool) {
	var req instanceRequest
	if decodeRequest(r, &req) != nil || req.AgentID == "" {
		writeRPCError(w, 400, "request", "agentId is required")
		return
	}
	h := b.host
	h.mu.Lock()
	instance := h.instances[req.AgentID]
	allowed := instance != nil && h.canManageLocked(op, instance)
	h.mu.Unlock()
	if !allowed {
		writeRPCError(w, 409, "agent", "unknown or unowned agent")
		return
	}
	if wait {
		select {
		case <-instance.done:
		case <-op.ctx.Done():
			writeRPCError(w, 409, "cancelled", "agent operation cancelled")
			return
		case <-r.Context().Done():
			return
		}
	}
	h.mu.Lock()
	snapshot := instance.snapshotLocked()
	h.mu.Unlock()
	writeJSON(w, snapshot)
}

func (b *broker) handleQueue(w http.ResponseWriter, r *http.Request, op *Operation) {
	var req queueRequest
	if decodeRequest(r, &req) != nil || req.AgentID == "" || strings.TrimSpace(req.Instruction) == "" {
		writeRPCError(w, 400, "request", "agentId and instruction are required")
		return
	}
	if req.CommandID == "" {
		id, err := randomID()
		if err != nil {
			writeRPCError(w, 500, "broker", "could not allocate command ID")
			return
		}
		req.CommandID = id
	} else if strings.TrimSpace(req.CommandID) == "" {
		writeRPCError(w, 400, "request", "commandId must be nonempty")
		return
	}
	h := b.host
	h.mu.Lock()
	instance := h.instances[req.AgentID]
	if instance == nil || !h.canManageLocked(op, instance) {
		h.mu.Unlock()
		writeRPCError(w, 409, "agent", "unknown or unowned agent")
		return
	}
	if previous := instance.commands[req.CommandID]; previous != nil {
		if previous.instruction != req.Instruction {
			h.mu.Unlock()
			writeRPCError(w, 409, "command", "commandId already used for a different instruction")
			return
		}
		status := previous.status
		h.mu.Unlock()
		writeJSON(w, map[string]string{"commandId": req.CommandID, "status": status})
		return
	}
	if !instance.keepAlive || instance.ctx.Err() != nil || (instance.state != "starting" && instance.state != "running" && instance.state != "idle") {
		h.mu.Unlock()
		writeRPCError(w, 409, "state", "agent does not accept queued tasks")
		return
	}
	cmd := &command{id: req.CommandID, instruction: req.Instruction, operation: "queue", status: "accepted", accepted: make(chan struct{})}
	instance.commands[cmd.id] = cmd
	instance.queue = append(instance.queue, cmd)
	h.mu.Unlock()
	instance.commandEvent(cmd, "accepted", "")
	close(cmd.accepted)
	select {
	case instance.wake <- struct{}{}:
	default:
	}
	writeJSON(w, map[string]string{"commandId": cmd.id, "status": "accepted"})
}

func (i *agentInstance) snapshotLocked() instanceSnapshot {
	parentID := ""
	if i.parent != nil {
		parentID = i.parent.id
	}
	return instanceSnapshot{AgentID: i.id, ParentRunID: parentID, Name: i.config.Name, State: i.state, Reason: i.reason, KeepAlive: i.keepAlive, CurrentRunID: i.currentRunID, QueueLength: len(i.queue), LastResult: i.lastResult}
}

func (i *agentInstance) event(kind string, fields events.Event) {
	parentID := ""
	if i.parent != nil {
		parentID = i.parent.id
	}
	event := events.Event{"type": kind, "agentId": i.id, "parentRunId": parentID, "name": i.config.Name, "model": i.config.Model}
	for key, value := range fields {
		event[key] = value
	}
	i.host.observe(event)
}
func (i *agentInstance) commandEvent(cmd *command, status, runID string) {
	fields := events.Event{"commandId": cmd.id, "status": status, "runId": runID}
	operation := cmd.operation
	if operation == "" {
		operation = "queue"
	}
	fields["operation"] = operation
	i.event("command."+status, fields)
}

func (i *agentInstance) serve() {
	defer close(i.done)
	defer i.cancel()
	h := i.host
	for {
		h.mu.Lock()
		if i.ctx.Err() != nil || i.state == "stopping" {
			reason := i.reason
			if reason == "" {
				reason = "owner_cancelled"
			}
			i.state = "stopping"
			queued := i.queue
			i.queue = nil
			for _, cmd := range queued {
				cmd.status = "cancelled"
			}
			accepted, settled := i.interruptAccepted, i.interruptSettled
			h.mu.Unlock()
			if accepted != nil {
				<-accepted
			}
			if reason == "interrupted" {
				if settled != nil {
					<-settled
				}
				for _, cmd := range queued {
					<-cmd.accepted
					i.commandEvent(cmd, "cancelled", "")
				}
				i.event("instance.stopping", events.Event{"reason": reason})
			} else {
				i.event("instance.stopping", events.Event{"reason": reason})
				for _, cmd := range queued {
					<-cmd.accepted
					i.commandEvent(cmd, "cancelled", "")
				}
				if settled != nil {
					<-settled
				}
			}
			i.finish("stopped", reason)
			return
		}
		if len(i.queue) == 0 {
			if !i.keepAlive {
				reason := "task_completed"
				if i.lastResult != nil && i.lastResult.State == "cancelled" && i.interruptedRunID == i.lastResult.RunID {
					reason = "task_interrupted"
				} else if i.lastResult != nil && i.lastResult.State != "completed" {
					reason = "task_failed"
				}
				i.state = "stopped"
				i.reason = reason
				i.currentRunID = ""
				h.mu.Unlock()
				i.event("instance.stopped", events.Event{"status": "stopped", "reason": reason})
				return
			}
			changed := i.state != "idle"
			i.state = "idle"
			h.mu.Unlock()
			if changed {
				i.event("instance.idle", events.Event{})
			}
			select {
			case <-i.ctx.Done():
			case <-i.wake:
			}
			continue
		}
		cmd := i.queue[0]
		i.queue = i.queue[1:]
		h.mu.Unlock()
		<-cmd.accepted // Publish command acceptance before any execution event.
		h.mu.Lock()
		if i.state == "stopping" || i.ctx.Err() != nil {
			// The command was already removed from queue when whole-instance
			// interruption took its snapshot. Put it back so the stopping branch
			// emits the accepted command's cancellation without starting a task.
			cmd.status = "cancelled"
			i.queue = append([]*command{cmd}, i.queue...)
			h.mu.Unlock()
			continue
		}
		cmd.status = "running"
		i.state = "running"
		h.mu.Unlock()
		i.event("instance.running", events.Event{"commandId": cmd.id})
		outcome, run, fatal := i.execute(cmd)
		h.mu.Lock()
		i.lastResult = &outcome
		i.currentRunID = ""
		cmd.status = outcome.State
		if fatal {
			if i.state == "stopping" || i.ctx.Err() != nil {
				// An accepted stop takes precedence over supervision cleanup.
				// The next iteration publishes that stop and cancels reservations.
				fatal = false
			} else {
				// Close admission before publishing the failed task. Observers
				// may block or immediately send a whole-instance interrupt.
				i.state = "stopping"
				i.reason = "supervision_failed"
			}
		}
		h.mu.Unlock()
		if run != nil {
			fields := events.Event{"status": outcome.State, "duration": time.Since(run.started)}
			if outcome.Error != "" {
				fields["error"] = outcome.Error
			}
			h.traceEvent(traceEvent{RunID: run.id, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model, Status: outcome.State, Duration: time.Since(run.started)})
			h.observeRun("agent."+outcome.State, run.id, run, fields)
		}
		i.commandEvent(cmd, outcome.State, outcome.RunID)
		if fatal {
			h.mu.Lock()
			i.state = "stopping"
			queued := i.queue
			i.queue = nil
			for _, cmd := range queued {
				cmd.status = "cancelled"
			}
			h.mu.Unlock()
			for _, cmd := range queued {
				<-cmd.accepted
				i.commandEvent(cmd, "cancelled", "")
			}
			i.finish("failed", "supervision_failed")
			return
		}
	}
}

func (i *agentInstance) finish(state, reason string) {
	h := i.host
	h.mu.Lock()
	i.state = state
	i.reason = reason
	i.currentRunID = ""
	h.mu.Unlock()
	i.event("instance."+state, events.Event{"status": state, "reason": reason})
}

func (i *agentInstance) execute(cmd *command) (taskOutcome, *agentRun, bool) {
	h := i.host
	ctx, cancel := context.WithCancel(i.ctx)
	h.mu.Lock()
	i.taskCancel = cancel
	h.mu.Unlock()
	defer func() {
		cancel()
		h.mu.Lock()
		i.taskCancel = nil
		h.mu.Unlock()
	}()
	op := h.NewOperation(ctx)
	task := &managedTask{instance: i, command: cmd}
	h.mu.Lock()
	op.task = task
	if i.parent != nil {
		op.parentRunID = i.parent.id
	}
	b := h.broker
	h.mu.Unlock()
	connection := &Connection{URL: b.url, Token: b.token, Scope: op.scope, Defaults: Config{Provider: i.config.Provider, Model: i.config.Model, MaxTokens: i.config.MaxTokens, Effort: i.config.Effort}}
	result, err := (Runner{Executable: h.opts.Executable, Cwd: h.opts.Cwd, Connection: connection, ImportMap: h.opts.ImportMap}).RunTask(ctx, i.configJSON, cmd.instruction)
	op.Close() // Joins finish/model handlers before reading the authenticated report.
	h.mu.Lock()
	run, report := task.run, task.report
	finalAnswer := ""
	finalCommitted := false
	taskInterrupted := false
	var interruptSettled []chan struct{}
	if run != nil {
		finalCommitted = run.finalClosed
		if finalCommitted {
			finalAnswer = run.finalAnswer
		}
		taskInterrupted = i.interruptedRunID == run.id
		if accepted := i.runInterruptAccepted[run.id]; accepted != nil {
			interruptSettled = append(interruptSettled, accepted)
			delete(i.runInterruptAccepted, run.id)
		}
	}
	instanceInterrupted := i.reason == "interrupted"
	if i.interruptSettled != nil {
		interruptSettled = append(interruptSettled, i.interruptSettled)
	}
	h.mu.Unlock()
	for _, accepted := range interruptSettled {
		<-accepted
	}
	outcome := taskOutcome{CommandID: cmd.id, State: "failed"}
	if run != nil {
		outcome.RunID = run.id
	}
	fatal := false
	switch {
	case instanceInterrupted:
		outcome.State = "cancelled"
		outcome.Error = "agent interrupted"
	case taskInterrupted:
		outcome.State = "cancelled"
		outcome.Error = "agent task interrupted"
	case i.ctx.Err() != nil:
		outcome.State = "cancelled"
		outcome.Error = "agent owner cancelled"
	case err != nil:
		fatal = true
		outcome.Error = err.Error()
	case report == nil:
		outcome.Error = "task process exited without an authenticated completion report"
	case result.ExitCode != 0 || result.Reason != "completed":
		outcome.Error = fmt.Sprintf("task process failed: %s (exit %d)", result.Reason, result.ExitCode)
	default:
		outcome.State = sanitizeRunStatus(report.Status)
		outcome.Error = report.Error
	}
	if report != nil {
		outcome.Answer = report.Answer
	}
	if finalCommitted {
		// Keep the committed answer observable even if the child process exits
		// before it can send its completion report.
		outcome.Answer = finalAnswer
	}
	if result.Stdout != "" {
		i.event("stdout", events.Event{"source": "agent_task", "runId": outcome.RunID, "text": result.Stdout})
	}
	if result.Stderr != "" {
		i.event("stderr", events.Event{"source": "agent_task", "runId": outcome.RunID, "text": result.Stderr})
	}
	return outcome, run, fatal
}

// OwnedLifetime lets a local runtime track a spawned instance as native work.
// Remote run_js clients only track their launch request; the parent host owns
// the instance and its process lifetime.
func (op *Operation) OwnedLifetime(id string) <-chan struct{} {
	h := op.host
	h.mu.Lock()
	defer h.mu.Unlock()
	if instance := op.instances[id]; instance != nil {
		return instance.done
	}
	return nil
}
