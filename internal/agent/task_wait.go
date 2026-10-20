package agent

import (
	"context"
	"strings"
	"unicode/utf8"
)

func (h *Host) waitTask(op *Operation, req taskRequest, requestContext context.Context) (taskOutcome, error) {
	cmd, err := h.taskCommand(op, req.AgentID, req.CommandID)
	if err != nil {
		return taskOutcome{}, err
	}

	select {
	case <-cmd.done:
	case <-op.ctx.Done():
		return taskOutcome{}, newOperationError(409, "cancelled", "agent operation cancelled")
	case <-requestContext.Done():
		return taskOutcome{}, errRequestCancelled
	}
	// Completion publishes an immutable result before closing done. Waiting is
	// observational: it neither consumes the result nor cancels the target task.
	outcome := *cmd.outcome
	outcome.omitAnswer = req.IncludeAnswer != nil && !*req.IncludeAnswer
	return outcome, nil
}

func (h *Host) taskCommand(op *Operation, agentID, commandID string) (*command, error) {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(commandID) == "" {
		return nil, newOperationError(400, "request", "agentId and commandId are required")
	}
	h.mu.Lock()
	instance := h.instances[agentID]
	if instance == nil || !h.canManageLocked(op, instance) {
		h.mu.Unlock()
		return nil, newOperationError(409, "agent", "unknown or unowned agent")
	}
	cmd := instance.commands[commandID]
	if cmd == nil {
		h.mu.Unlock()
		return nil, newOperationError(409, "command", "unknown task command")
	}
	h.mu.Unlock()

	return cmd, nil
}

func (h *Host) readTaskResult(op *Operation, req taskResultRequest) (taskResultSlice, error) {
	const maxSafeInteger = int64(9007199254740991)
	limit := int64(4096)
	if req.MaxBytes != nil {
		limit = *req.MaxBytes
	}
	if req.Offset < 0 || req.Offset > maxSafeInteger || limit <= 0 || limit > maxSafeInteger {
		return taskResultSlice{}, newOperationError(400, "request", "offset must be a nonnegative safe integer and maxBytes a positive safe integer")
	}
	cmd, err := h.taskCommand(op, req.AgentID, req.CommandID)
	if err != nil {
		return taskResultSlice{}, err
	}
	select {
	case <-cmd.done:
	default:
		return taskResultSlice{}, newOperationError(409, "state", "task result is not ready; wait_task first")
	}
	result := cmd.outcome
	answer := result.Answer
	total := int64(len(answer))
	if req.Offset > total || (req.Offset < total && !utf8.RuneStart(answer[int(req.Offset)])) {
		return taskResultSlice{}, newOperationError(400, "request", "offset must be a UTF-8 boundary within the answer")
	}
	end := total
	if limit < total-req.Offset {
		end = req.Offset + limit
		for end > req.Offset && !utf8.RuneStart(answer[int(end)]) {
			end--
		}
		// Always make progress, even when the next character exceeds maxBytes.
		if end == req.Offset {
			_, size := utf8.DecodeRuneInString(answer[int(req.Offset):])
			end += int64(size)
		}
	}
	return taskResultSlice{RunID: result.RunID, CommandID: result.CommandID, State: result.State,
		Text: answer[int(req.Offset):int(end)], Offset: req.Offset, NextOffset: end, TotalBytes: total, EOF: end == total}, nil
}
