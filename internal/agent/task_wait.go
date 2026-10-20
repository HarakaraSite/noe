package agent

import (
	"context"
	"strings"
)

func (h *Host) waitTask(op *Operation, req taskRequest, requestContext context.Context) (taskOutcome, error) {
	if strings.TrimSpace(req.AgentID) == "" || strings.TrimSpace(req.CommandID) == "" {
		return taskOutcome{}, newOperationError(400, "request", "agentId and commandId are required")
	}
	h.mu.Lock()
	instance := h.instances[req.AgentID]
	if instance == nil || !h.canManageLocked(op, instance) {
		h.mu.Unlock()
		return taskOutcome{}, newOperationError(409, "agent", "unknown or unowned agent")
	}
	cmd := instance.commands[req.CommandID]
	if cmd == nil {
		h.mu.Unlock()
		return taskOutcome{}, newOperationError(409, "command", "unknown task command")
	}
	h.mu.Unlock()

	select {
	case <-cmd.done:
	case <-op.ctx.Done():
		return taskOutcome{}, newOperationError(409, "cancelled", "agent operation cancelled")
	case <-requestContext.Done():
		return taskOutcome{}, errRequestCancelled
	}
	// Completion publishes an immutable result before closing done. Waiting is
	// observational: it neither consumes the result nor cancels the target task.
	return *cmd.outcome, nil
}
