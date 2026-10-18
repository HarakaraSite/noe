package agent

import (
	"encoding/json"
	"net/http"
	"strings"

	"noe/internal/events"
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

func (b *broker) handleSteer(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request steerRequest
	if err := decodeRequest(r, &request); err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "runId and instruction are required")
		return
	}
	response, err := b.host.steer(op, request)
	if err != nil {
		writeOperationError(w, err)
		return
	}
	writeJSON(w, response)
}

func (b *broker) handleApplySteers(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request applySteersRequest
	if err := decodeRequest(r, &request); err != nil || request.RunID == "" || len(request.CommandIDs) == 0 {
		writeRPCError(w, http.StatusBadRequest, "request", "runId and commandIds are required")
		return
	}
	h := b.host
	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || run.operation != op || run.ctx.Err() != nil || run.interrupting || op.closed || h.closed {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "unknown agent run")
		return
	}
	if !h.executionGroupOpenLocked(run.group) {
		group := run.group
		h.mu.Unlock()
		code, message := executionLimitError(group)
		writeRPCError(w, http.StatusConflict, code, message)
		return
	}
	commands := make([]*steerCommand, 0, len(request.CommandIDs))
	seen := make(map[string]struct{}, len(request.CommandIDs))
	lastIndex := -1
	allApplied := true
	for _, id := range request.CommandIDs {
		if strings.TrimSpace(id) == "" {
			h.mu.Unlock()
			writeRPCError(w, http.StatusBadRequest, "request", "commandIds must be nonempty")
			return
		}
		if _, duplicate := seen[id]; duplicate {
			h.mu.Unlock()
			writeRPCError(w, http.StatusBadRequest, "request", "commandIds must be unique")
			return
		}
		seen[id] = struct{}{}
		command := run.steerByID[id]
		if command == nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "command", "unknown steer command")
			return
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
			writeRPCError(w, http.StatusConflict, "command", "steer commands must be applied in acceptance order")
			return
		}
		lastIndex = index
		switch command.status {
		case "applied":
		case "delivered":
			allApplied = false
		default:
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "command", "steer command has not been delivered")
			return
		}
		commands = append(commands, command)
	}
	if !allApplied {
		for _, command := range commands {
			if command.status != "delivered" {
				h.mu.Unlock()
				writeRPCError(w, http.StatusConflict, "command", "steer acknowledgement must cover one delivered batch")
				return
			}
		}
		for _, command := range run.steerOrder {
			if command.status == "delivered" {
				if _, included := seen[command.id]; !included {
					h.mu.Unlock()
					writeRPCError(w, http.StatusConflict, "command", "steer acknowledgement must cover one delivered batch")
					return
				}
			}
		}
	}
	if run.instance != nil {
		if request.Snapshot == nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusBadRequest, "conversation", "managed task steer acknowledgement requires a snapshot")
			return
		}
		if err := validateConversationSnapshot(request.Snapshot); err != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusBadRequest, "conversation", err.Error())
			return
		}
		if allApplied {
			if !conversationSnapshotEqual(run.instance.conversation, request.Snapshot) {
				h.mu.Unlock()
				writeRPCError(w, http.StatusConflict, "conversation", "applied steer snapshot is stale")
				return
			}
		} else {
			if err := validateConversationAppend(run.instance.conversation, request.Snapshot, len(commands), "protected"); err != nil {
				h.mu.Unlock()
				writeRPCError(w, http.StatusConflict, "conversation", err.Error())
				return
			}
			entries := request.Snapshot.Entries[len(run.instance.conversation.Entries):]
			for index, command := range commands {
				var message struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}
				if len(entries[index].Messages) != 1 || json.Unmarshal(entries[index].Messages[0], &message) != nil || message.Role != "user" || message.Content != command.instruction {
					h.mu.Unlock()
					writeRPCError(w, http.StatusConflict, "conversation", "steer snapshot does not match the delivered commands")
					return
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
	writeJSON(w, map[string]bool{"applied": true})
}

func (b *broker) handleCommitFinal(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request commitFinalRequest
	if err := decodeRequest(r, &request); err != nil || request.RunID == "" || strings.TrimSpace(request.Answer) == "" {
		writeRPCError(w, http.StatusBadRequest, "request", "runId and a nonempty answer are required")
		return
	}
	h := b.host
	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || run.operation != op || run.ctx.Err() != nil || run.interrupting || op.closed || h.closed {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "unknown agent run")
		return
	}
	if !h.executionGroupOpenLocked(run.group) {
		group := run.group
		h.mu.Unlock()
		code, message := executionLimitError(group)
		writeRPCError(w, http.StatusConflict, code, message)
		return
	}
	if run.instance != nil {
		if request.Snapshot == nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusBadRequest, "conversation", "managed task final commit requires a snapshot")
			return
		}
		if run.modelStep == nil || !run.modelStep.responseComplete || len(run.modelStep.toolCalls) != 0 {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "final", "managed task has no complete final assistant response")
			return
		}
		alreadyStored := conversationSnapshotEqual(run.instance.conversation, request.Snapshot)
		if err := validateFinalConversationCandidate(run.instance.conversation, request.Snapshot, request.Answer, alreadyStored); err != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "conversation", err.Error())
			return
		}
	}
	if run.finalClosed {
		if run.finalAnswer != request.Answer {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "final", "a different final answer is already committed")
			return
		}
		done := run.finalDone
		h.mu.Unlock()
		select {
		case <-done:
		case <-op.ctx.Done():
			writeRPCError(w, http.StatusConflict, "cancelled", "agent operation cancelled")
			return
		case <-r.Context().Done():
			return
		}
		writeJSON(w, commitFinalResponse{Committed: true})
		return
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
		writeJSON(w, commitFinalResponse{Committed: false, Instructions: instructions})
		return
	}
	run.finalClosed = true
	run.finalAnswer = request.Answer
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
	writeJSON(w, commitFinalResponse{Committed: true})
}
