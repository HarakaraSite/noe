package agent

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
	cancellation, err := instance.acceptTaskCancellationLocked(op, command)
	h.mu.Unlock()
	if err != nil {
		return CancelTaskResponse{}, err
	}
	if cancellation != nil {
		cancellation.publish()
	}
	return CancelTaskResponse{AgentID: instance.id, CommandID: command.id, Status: "accepted"}, nil
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
