package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

type applySteersRequest struct {
	RunID      string                `json:"runId"`
	CommandIDs []string              `json:"commandIds"`
	Snapshot   *conversationSnapshot `json:"snapshot,omitempty"`
}

type commitFinalRequest struct {
	RunID    string                `json:"runId"`
	Answer   string                `json:"answer"`
	Snapshot *conversationSnapshot `json:"snapshot,omitempty"`
}

type commitFinalResponse struct {
	Committed    bool               `json:"committed"`
	Instructions []steerInstruction `json:"instructions,omitempty"`
}

func (h *Host) applySteers(op *Operation, request applySteersRequest) (any, error) {
	if request.RunID == "" || len(request.CommandIDs) == 0 {
		return nil, newOperationError(http.StatusBadRequest, "request", "runId and commandIds are required")
	}
	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || run.operation != op || run.ctx.Err() != nil || run.interrupting || op.closed || h.closed {
		h.mu.Unlock()
		return nil, newOperationError(http.StatusConflict, "run", "unknown agent run")
	}
	if !h.executionGroupOpenLocked(run.group) {
		group := run.group
		h.mu.Unlock()
		code, message := executionLimitError(group)
		return nil, newOperationError(http.StatusConflict, code, message)
	}
	commands := make([]*steerCommand, 0, len(request.CommandIDs))
	seen := make(map[string]struct{}, len(request.CommandIDs))
	lastIndex := -1
	allApplied := true
	for _, id := range request.CommandIDs {
		if strings.TrimSpace(id) == "" {
			h.mu.Unlock()
			return nil, newOperationError(http.StatusBadRequest, "request", "commandIds must be nonempty")
		}
		if _, duplicate := seen[id]; duplicate {
			h.mu.Unlock()
			return nil, newOperationError(http.StatusBadRequest, "request", "commandIds must be unique")
		}
		seen[id] = struct{}{}
		command := run.steerByID[id]
		if command == nil {
			h.mu.Unlock()
			return nil, newOperationError(http.StatusConflict, "command", "unknown steer command")
		}
		index := -1
		for i, candidate := range run.steerOrder {
			if candidate == command {
				index = i
				break
			}
		}
		if index <= lastIndex {
			h.mu.Unlock()
			return nil, newOperationError(http.StatusConflict, "command", "steer commands must be applied in acceptance order")
		}
		lastIndex = index
		switch command.status {
		case "applied":
		case "delivered":
			allApplied = false
		default:
			h.mu.Unlock()
			return nil, newOperationError(http.StatusConflict, "command", "steer command has not been delivered")
		}
		commands = append(commands, command)
	}
	if !allApplied {
		for _, command := range commands {
			if command.status != "delivered" {
				h.mu.Unlock()
				return nil, newOperationError(http.StatusConflict, "command", "steer acknowledgement must cover one delivered batch")
			}
		}
		for _, command := range run.steerOrder {
			if command.status == "delivered" {
				if _, included := seen[command.id]; !included {
					h.mu.Unlock()
					return nil, newOperationError(http.StatusConflict, "command", "steer acknowledgement must cover one delivered batch")
				}
			}
		}
	}
	if run.instance != nil {
		if request.Snapshot == nil {
			h.mu.Unlock()
			return nil, newOperationError(http.StatusBadRequest, "conversation", "managed task steer acknowledgement requires a snapshot")
		}
		if err := validateConversationSnapshot(request.Snapshot); err != nil {
			h.mu.Unlock()
			return nil, newOperationError(http.StatusBadRequest, "conversation", err.Error())
		}
		if allApplied {
			if !conversationSnapshotEqual(run.instance.conversation, request.Snapshot) {
				h.mu.Unlock()
				return nil, newOperationError(http.StatusConflict, "conversation", "applied steer snapshot is stale")
			}
		} else {
			if err := validateConversationAppend(run.instance.conversation, request.Snapshot, len(commands), "protected"); err != nil {
				h.mu.Unlock()
				return nil, newOperationError(http.StatusConflict, "conversation", err.Error())
			}
			entries := request.Snapshot.Entries[len(run.instance.conversation.Entries):]
			for index, command := range commands {
				var message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}
				if len(entries[index].Messages) != 1 || json.Unmarshal(entries[index].Messages[0], &message) != nil || message.Role != "user" || message.Content != command.instruction {
					h.mu.Unlock()
					return nil, newOperationError(http.StatusConflict, "conversation", "steer snapshot does not match the delivered commands")
				}
			}
		}
	}
	if !allApplied {
		for _, command := range commands {
			command.status = "applied"
			run.pendingSteerActivityIDs = appendUniqueActivityIDs(run.pendingSteerActivityIDs, command.activityID)
		}
		if run.instance != nil {
			run.instance.conversation = cloneConversationSnapshot(request.Snapshot)
		}
	}
	h.mu.Unlock()
	if !allApplied {
		pending := make([]steerLifecycleEvent, 0, len(commands))
		for _, command := range commands {
			pending = append(pending, steerLifecycleEvent{run: run, command: command, status: "applied"})
		}
		h.emitSteerEvents(pending)
	}
	return map[string]bool{"applied": true}, nil
}

func (h *Host) commitFinal(ctx context.Context, op *Operation, request commitFinalRequest) (commitFinalResponse, error) {
	if request.RunID == "" || strings.TrimSpace(request.Answer) == "" {
		return commitFinalResponse{}, newOperationError(http.StatusBadRequest, "request", "runId and a nonempty answer are required")
	}
	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || run.operation != op || run.ctx.Err() != nil || run.interrupting || op.closed || h.closed {
		h.mu.Unlock()
		return commitFinalResponse{}, newOperationError(http.StatusConflict, "run", "unknown agent run")
	}
	if !h.executionGroupOpenLocked(run.group) {
		group := run.group
		h.mu.Unlock()
		code, message := executionLimitError(group)
		return commitFinalResponse{}, newOperationError(http.StatusConflict, code, message)
	}
	if run.instance != nil {
		if request.Snapshot == nil {
			h.mu.Unlock()
			return commitFinalResponse{}, newOperationError(http.StatusBadRequest, "conversation", "managed task final commit requires a snapshot")
		}
		if run.modelStep == nil || !run.modelStep.responseComplete || len(run.modelStep.toolCalls) != 0 {
			h.mu.Unlock()
			return commitFinalResponse{}, newOperationError(http.StatusConflict, "final", "managed task has no complete final assistant response")
		}
		alreadyStored := conversationSnapshotEqual(run.instance.conversation, request.Snapshot)
		if err := validateFinalConversationCandidate(run.instance.conversation, request.Snapshot, request.Answer, alreadyStored); err != nil {
			h.mu.Unlock()
			return commitFinalResponse{}, newOperationError(http.StatusConflict, "conversation", err.Error())
		}
	}
	if run.finalClosed {
		if run.finalAnswer != request.Answer {
			h.mu.Unlock()
			return commitFinalResponse{}, newOperationError(http.StatusConflict, "final", "a different final answer is already committed")
		}
		done := run.finalDone
		h.mu.Unlock()
		select {
		case <-done:
		case <-op.ctx.Done():
			return commitFinalResponse{}, op.ctx.Err()
		case <-ctx.Done():
			return commitFinalResponse{}, errRequestCancelled
		}
		return commitFinalResponse{Committed: true}, nil
	}
	instructions := h.deliverPendingSteersLocked(run)
	if run.modelStep != nil && run.modelStep.responseComplete && len(run.modelStep.toolCalls) == 0 {
		// A rejected final candidate starts a new logical judgment, but only
		// after its afterModelResponse hooks and this finalization gate finish.
		run.modelStep.finished = true
	}
	if run.instance != nil {
		// The full provider assistant object is committed together with the
		// finalization gate, including candidates rejected by a pending steer.
		run.instance.conversation = cloneConversationSnapshot(request.Snapshot)
	}
	if len(instructions) != 0 {
		h.mu.Unlock()
		return commitFinalResponse{Committed: false, Instructions: instructions}, nil
	}
	run.finalClosed = true
	run.finalAnswer = request.Answer
	if run.instance != nil && op.task != nil && op.task.run == run && op.task.command != nil {
		// finish removes the run from h.runs before the task command is reaped.
		// Preserve the final-answer boundary on the command so CancelTask cannot
		// reopen cancellation during that cleanup window.
		op.task.command.finalCommitted = true
	}
	settled := make([]<-chan struct{}, 0, len(run.steerOrder))
	for _, command := range run.steerOrder {
		if command.status == "applied" {
			settled = append(settled, command.settled)
		}
	}
	h.mu.Unlock()

	for _, eventDone := range settled {
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
	return commitFinalResponse{Committed: true}, nil
}
