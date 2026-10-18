package agent

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

func (h *Host) spawn(op *Operation, req spawnRequest) (string, error) {
	effort, valid := provider.NormalizeEffort(req.Config.Effort)
	if strings.TrimSpace(req.Config.Provider) == "" || strings.TrimSpace(req.Config.Model) == "" || strings.TrimSpace(req.Config.Name) == "" || req.Config.Effort == "" || !valid || req.Config.MaxTokens < 0 || req.Config.MaxSteps < 0 || (!req.KeepAlive && req.Instruction == nil) || (req.Instruction != nil && strings.TrimSpace(*req.Instruction) == "") {
		return "", newOperationError(400, "request", "invalid agent configuration or instruction")
	}
	if err := h.checkProvider(req.Config.Provider); err != nil {
		return "", newOperationError(400, "provider", err.Error())
	}
	req.Config.Effort = effort
	resolvedHooks, hookErr := normalizeHookModules(req.Config.hookModules(), h.opts.Cwd, h.opts.ImportMap)
	if hookErr != nil {
		return "", newOperationError(400, "request", hookErr.Error())
	}
	req.Config.Hooks = cloneHookModules(resolvedHooks)
	encoded, err := json.Marshal(req.Config)
	if err != nil {
		return "", newOperationError(500, "broker", "could not encode agent configuration")
	}
	id, err := randomID()
	if err != nil {
		return "", newOperationError(500, "broker", "could not allocate agent ID")
	}
	activityID, err := randomID()
	if err != nil {
		return "", newOperationError(500, "broker", "could not allocate agent activity")
	}
	var initial *command
	if req.Instruction != nil {
		commandID, err := randomID()
		if err != nil {
			return "", newOperationError(500, "broker", "could not allocate task ID")
		}
		commandActivityID, err := randomID()
		if err != nil {
			return "", newOperationError(500, "broker", "could not allocate task activity")
		}
		initial = &command{id: commandID, activityID: commandActivityID, instruction: *req.Instruction, operation: "queue", status: "accepted", initial: true, accepted: make(chan struct{})}
	}
	modelWindow, modelMaxOutput := h.contextModelLimits(req.Config.Provider, req.Config.Model)
	h.mu.Lock()
	if op.closed || op.ctx.Err() != nil || h.closed {
		h.mu.Unlock()
		return "", newOperationError(409, "scope", "agent operation has ended")
	}
	ownerCtx := op.ctx
	parent := h.currentOwnerRunLocked(op)
	if parent == nil && op.parentRunID != "" {
		h.mu.Unlock()
		return "", newOperationError(409, "scope", "parent agent run has ended")
	}
	if parent != nil {
		if parent.ctx.Err() != nil {
			h.mu.Unlock()
			return "", newOperationError(409, "scope", "parent agent run has ended")
		}
		ownerCtx = parent.ctx
	}
	group := h.executionForOperationLocked(op)
	rootGroup := group == nil
	if !rootGroup && (req.Config.executionLimitsPresent || req.Config.ExecutionLimits != nil) {
		h.mu.Unlock()
		return "", newOperationError(400, "request", "executionLimits may only be set on a root agent")
	}
	if group == nil {
		group = &executionGroup{id: id, limits: cloneExecutionLimits(req.Config.ExecutionLimits), startedAt: time.Now()}
	}
	origin := op.activityOrigin
	if origin.Kind == "" {
		origin.Kind = "script"
		if parent != nil {
			origin.RunID, origin.AgentID = parent.id, parent.agentID()
		}
	}
	causedBy := append([]string(nil), op.causeActivityIDs...)
	if len(causedBy) == 0 && parent != nil && parent.activityID != "" {
		causedBy = []string{parent.activityID}
	}
	limitAction, admitted := h.reserveExecutionLocked(group, "agentStarts", "", activityID, origin)
	if !admitted {
		h.mu.Unlock()
		h.finishExecutionLimit(limitAction)
		code, message := executionLimitError(group)
		return "", newOperationError(409, code, message)
	}
	if rootGroup {
		h.groups[id] = group
	}
	ctx, cancel := context.WithCancel(ownerCtx)
	instance := &agentInstance{host: h, id: id, config: req.Config, configJSON: encoded, keepAlive: req.KeepAlive, owner: op, parent: parent, group: group, modelWindow: modelWindow, modelMaxOutputTokens: modelMaxOutput, activityID: activityID, causedBy: causedBy, origin: origin, activeCounted: true, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), state: "starting", commands: make(map[string]*command), runInterruptAccepted: make(map[string]chan struct{})}
	group.counters.ActiveAgents++
	if initial != nil {
		initial.causedBy = []string{activityID}
		initial.origin = activityOrigin{Kind: "script", RunID: origin.RunID, AgentID: id, CommandID: initial.id}
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
	return id, nil
}

func (h *Host) observeInstance(op *Operation, req instanceRequest, wait bool, requestContext context.Context) (instanceSnapshot, error) {
	if req.AgentID == "" {
		return instanceSnapshot{}, newOperationError(400, "request", "agentId is required")
	}
	h.mu.Lock()
	instance := h.instances[req.AgentID]
	allowed := instance != nil && h.canManageLocked(op, instance)
	h.mu.Unlock()
	if !allowed {
		return instanceSnapshot{}, newOperationError(409, "agent", "unknown or unowned agent")
	}
	if wait {
		select {
		case <-instance.done:
		case <-op.ctx.Done():
			return instanceSnapshot{}, newOperationError(409, "cancelled", "agent operation cancelled")
		case <-requestContext.Done():
			return instanceSnapshot{}, errRequestCancelled
		}
	}
	h.mu.Lock()
	snapshot := instance.snapshotLocked()
	h.mu.Unlock()
	return snapshot, nil
}

func (h *Host) queue(op *Operation, req queueRequest) (commandReceipt, error) {
	if req.AgentID == "" || strings.TrimSpace(req.Instruction) == "" {
		return commandReceipt{}, newOperationError(400, "request", "agentId and instruction are required")
	}
	if req.CommandID == "" {
		id, err := randomID()
		if err != nil {
			return commandReceipt{}, newOperationError(500, "broker", "could not allocate command ID")
		}
		req.CommandID = id
	} else if strings.TrimSpace(req.CommandID) == "" {
		return commandReceipt{}, newOperationError(400, "request", "commandId must be nonempty")
	}
	h.mu.Lock()
	instance := h.instances[req.AgentID]
	if instance == nil || !h.canManageLocked(op, instance) {
		h.mu.Unlock()
		return commandReceipt{}, newOperationError(409, "agent", "unknown or unowned agent")
	}
	if previous := instance.commands[req.CommandID]; previous != nil {
		if previous.instruction != req.Instruction {
			h.mu.Unlock()
			return commandReceipt{}, newOperationError(409, "command", "commandId already used for a different instruction")
		}
		response := commandReceipt{CommandID: req.CommandID, Status: previous.status}
		h.mu.Unlock()
		return response, nil
	}
	if !instance.keepAlive || instance.ctx.Err() != nil || (instance.state != "starting" && instance.state != "running" && instance.state != "idle") {
		h.mu.Unlock()
		return commandReceipt{}, newOperationError(409, "state", "agent does not accept queued tasks")
	}
	activityID, err := randomID()
	if err != nil {
		h.mu.Unlock()
		return commandReceipt{}, newOperationError(500, "broker", "could not allocate task activity")
	}
	origin := op.activityOrigin
	if origin.Kind == "" {
		origin.Kind = "script"
		if op.control {
			origin.Kind = "human"
		}
		if owner := h.currentOwnerRunLocked(op); owner != nil {
			origin.RunID, origin.AgentID = owner.id, owner.agentID()
		}
	}
	origin.CommandID = req.CommandID
	causedBy := append([]string(nil), op.causeActivityIDs...)
	if len(causedBy) == 0 {
		if owner := h.currentOwnerRunLocked(op); owner != nil && owner.activityID != "" {
			causedBy = []string{owner.activityID}
		}
	}
	limitAction, admitted := h.reserveExecutionLocked(instance.group, "queuedTasks", "", activityID, origin)
	if !admitted {
		h.mu.Unlock()
		h.finishExecutionLimit(limitAction)
		code, message := executionLimitError(instance.group)
		return commandReceipt{}, newOperationError(409, code, message)
	}
	cmd := &command{id: req.CommandID, activityID: activityID, causedBy: causedBy, origin: origin, instruction: req.Instruction, operation: "queue", status: "accepted", accepted: make(chan struct{})}
	instance.commands[cmd.id] = cmd
	instance.queue = append(instance.queue, cmd)
	h.mu.Unlock()
	instance.commandEvent(cmd, "accepted", "")
	close(cmd.accepted)
	select {
	case instance.wake <- struct{}{}:
	default:
	}
	return commandReceipt{CommandID: cmd.id, Status: "accepted"}, nil
}
