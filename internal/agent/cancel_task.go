package agent

import (
	"fmt"
)

// cancelTask cancels one queued or active command while leaving its managed
// instance and sibling commands intact. The Host mutex serializes cancellation
// with the selected-to-running transition in agentInstance.serve.
func (h *Host) cancelTask(op *Operation, request CancelTaskRequest) (CancelTaskResponse, error) {
	if request.AgentID == "" || request.CommandID == "" {
		return CancelTaskResponse{}, newOperationError(400, "request", "agentId and commandId are required")
	}
	h.mu.Lock()
	instance := h.instances[request.AgentID]
	if instance == nil || !h.canManageLocked(op, instance) {
		h.mu.Unlock()
		return CancelTaskResponse{}, newOperationError(409, "agent", "unknown or unowned agent")
	}
	if op == nil || op.closed || op.ctx.Err() != nil || h.closed {
		h.mu.Unlock()
		return CancelTaskResponse{}, newOperationError(409, "scope", "agent operation has ended")
	}
	command := instance.commands[request.CommandID]
	if command == nil {
		h.mu.Unlock()
		return CancelTaskResponse{}, newOperationError(409, "command", "unknown task command")
	}
	if command.outcome != nil {
		h.mu.Unlock()
		return CancelTaskResponse{}, newOperationError(409, "command", "task command has already finished")
	}
	if command.cancelRequested {
		h.mu.Unlock()
		return CancelTaskResponse{AgentID: instance.id, CommandID: command.id, Status: "accepted"}, nil
	}
	if command.finalCommitted && (instance.selected == command || command.active) {
		h.mu.Unlock()
		return CancelTaskResponse{}, newOperationError(409, "command", "task final answer is already committed")
	}
	if instance.currentRunID != "" {
		if run := h.runs[instance.currentRunID]; run != nil && run.finalClosed && (instance.selected == command || command.active) {
			h.mu.Unlock()
			return CancelTaskResponse{}, newOperationError(409, "command", "task final answer is already committed")
		}
	}
	fields, err := h.controlActivityLocked(op, "task")
	if err != nil {
		h.mu.Unlock()
		return CancelTaskResponse{}, newOperationError(500, "broker", "could not allocate cancellation activity")
	}
	var queueIndex = -1
	for index, candidate := range instance.queue {
		if candidate == command {
			queueIndex = index
			break
		}
	}
	if queueIndex < 0 && instance.selected != command && !command.active {
		h.mu.Unlock()
		return CancelTaskResponse{}, newOperationError(409, "command", fmt.Sprintf("task command is not cancellable in its current state"))
	}
	command.cancelRequested = true
	accepted := make(chan struct{})
	command.cancelAccepted = accepted
	fields["commandId"] = command.id
	cancel := instance.taskCancel
	if queueIndex >= 0 {
		instance.queue = append(instance.queue[:queueIndex], instance.queue[queueIndex+1:]...)
		h.settleTaskCancellationLocked(instance, command)
		instance.publishCommandOutcomeAsync(command)
	} else if run := h.runs[instance.currentRunID]; run != nil {
		run.interrupting = true
		instance.interruptedRunID = run.id
		instance.runInterruptAccepted[run.id] = accepted
		fields["runId"] = run.id
	}
	// Join accepted-event publication with the instance even if cancellation
	// precedes run creation. Terminal command events wait on this admission gate.
	instance.publishWG.Add(1)
	h.mu.Unlock()
	if queueIndex < 0 && cancel != nil {
		cancel()
	}
	go func() { defer instance.publishWG.Done(); instance.event("interrupt.accepted", fields); close(accepted) }()
	return CancelTaskResponse{AgentID: instance.id, CommandID: command.id, Status: "accepted"}, nil
}

func (h *Host) settleTaskCancellationLocked(instance *agentInstance, command *command) {
	h.maybeCloseGroupLocked(instance.group)
	instance.settleCommandLocked(command, taskOutcome{CommandID: command.id, State: "cancelled", Error: "agent task cancelled"})
}

func (h *Host) stopAgent(op *Operation, request StopAgentRequest) (StopAgentResponse, error) {
	if request.AgentID == "" {
		return StopAgentResponse{}, newOperationError(400, "request", "agentId is required")
	}
	response, err := h.interruptInstance(op, request.AgentID)
	if err != nil {
		return StopAgentResponse{}, err
	}
	return StopAgentResponse{AgentID: response.AgentID, Status: response.Status}, nil
}
