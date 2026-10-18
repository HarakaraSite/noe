package agent

import (
	"net/http"
	"strings"

	"noe/internal/events"
)

type steerRequest struct {
	RunID       string `json:"runId"`
	Instruction string `json:"instruction"`
	CommandID   string `json:"commandId,omitempty"`
}

type steerCommand struct {
	id          string
	instruction string
	status      string
	activityID  string
	causedBy    []string
	origin      activityOrigin
	accepted    chan struct{}
	settled     chan struct{}
}

type steerInstruction struct {
	CommandID   string `json:"commandId"`
	Instruction string `json:"instruction"`
}

type applySteersRequest struct {
	RunID      string   `json:"runId"`
	CommandIDs []string `json:"commandIds"`
}

type commitFinalRequest struct {
	RunID  string `json:"runId"`
	Answer string `json:"answer"`
}

type commitFinalResponse struct {
	Committed    bool               `json:"committed"`
	Instructions []steerInstruction `json:"instructions,omitempty"`
}

type steerLifecycleEvent struct {
	run     *agentRun
	command *steerCommand
	status  string
}

func (b *broker) handleSteer(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request steerRequest
	if err := decodeRequest(r, &request); err != nil || request.RunID == "" || strings.TrimSpace(request.Instruction) == "" {
		writeRPCError(w, http.StatusBadRequest, "request", "runId and instruction are required")
		return
	}
	if request.CommandID == "" {
		id, err := randomID()
		if err != nil {
			writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate command ID")
			return
		}
		request.CommandID = id
	} else if strings.TrimSpace(request.CommandID) == "" {
		writeRPCError(w, http.StatusBadRequest, "request", "commandId must be nonempty")
		return
	}

	h := b.host
	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || !h.canManageRunLocked(op, run) || run.ctx.Err() != nil || run.interrupting || op.closed || h.closed || run.finalClosed {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "unknown, unowned, or finished agent run")
		return
	}
	if !h.executionGroupOpenLocked(run.group) {
		group := run.group
		h.mu.Unlock()
		code, message := executionLimitError(group)
		writeRPCError(w, http.StatusConflict, code, message)
		return
	}
	if previous := run.steerByID[request.CommandID]; previous != nil {
		if previous.instruction != request.Instruction {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "command", "commandId already used for a different instruction")
			return
		}
		status := previous.status
		h.mu.Unlock()
		writeJSON(w, map[string]string{"commandId": request.CommandID, "status": status})
		return
	}
	activityID, err := randomID()
	if err != nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate steer activity")
		return
	}
	senderRun := h.currentOwnerRunLocked(op)
	origin := op.activityOrigin
	if origin.Kind == "" {
		origin.Kind = "script"
		if op.control {
			origin.Kind = "human"
		}
	}
	if origin.RunID == "" && senderRun != nil {
		origin.RunID = senderRun.id
	}
	if origin.AgentID == "" && senderRun != nil {
		origin.AgentID = senderRun.agentID()
	}
	if origin.RunID == "" && run.operation == op {
		origin.RunID = run.id
		origin.AgentID = run.agentID()
	}
	if origin.RunID == "" && op.control {
		origin.RunID = run.id
		origin.AgentID = run.agentID()
	}
	origin.CommandID = request.CommandID
	causedBy := append([]string(nil), op.causeActivityIDs...)
	if len(causedBy) == 0 && !op.control && senderRun != nil && senderRun.activityID != "" {
		causedBy = []string{senderRun.activityID}
	}
	if len(causedBy) == 0 && !op.control && run.operation == op && run.activityID != "" {
		causedBy = []string{run.activityID}
	}
	command := &steerCommand{
		id: request.CommandID, instruction: request.Instruction, status: "accepted",
		activityID: activityID, causedBy: causedBy, origin: origin,
		accepted: make(chan struct{}), settled: make(chan struct{}),
	}
	run.steerByID[command.id] = command
	run.steerOrder = append(run.steerOrder, command)
	h.mu.Unlock()

	h.observeRun("command.accepted", run.id, run, steerEventFields(command, "accepted"))
	close(command.accepted)
	writeJSON(w, map[string]string{"commandId": command.id, "status": "accepted"})
}

// deliverPendingSteersLocked is called at the provider-call and final-answer
// linearization points. Both accepted and previously delivered commands stay
// pending until apply_steers acknowledges them.
func (h *Host) deliverPendingSteersLocked(run *agentRun) []steerInstruction {
	var instructions []steerInstruction
	for _, command := range run.steerOrder {
		if command.status != "accepted" && command.status != "delivered" {
			continue
		}
		command.status = "delivered"
		instructions = append(instructions, steerInstruction{CommandID: command.id, Instruction: command.instruction})
	}
	return instructions
}

func (h *Host) cancelPendingSteersLocked(run *agentRun) []steerLifecycleEvent {
	var pending []steerLifecycleEvent
	for _, command := range run.steerOrder {
		if command.status != "accepted" && command.status != "delivered" {
			continue
		}
		command.status = "cancelled"
		pending = append(pending, steerLifecycleEvent{run: run, command: command, status: "cancelled"})
	}
	return pending
}

// emitSteerEvents preserves accepted-before-settled event ordering without
// holding Host.mu while invoking the user observer.
func (h *Host) emitSteerEvents(pending []steerLifecycleEvent) {
	for _, item := range pending {
		<-item.command.accepted
		h.observeRun("command."+item.status, item.run.id, item.run, steerEventFields(item.command, item.status))
		close(item.command.settled)
	}
}

func steerEventFields(command *steerCommand, status string) events.Event {
	fields := events.Event{"commandId": command.id, "status": status, "operation": "steer"}
	addActivityFields(fields, command.activityID, command.causedBy, command.origin)
	return fields
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
	if !allApplied {
		for _, command := range commands {
			command.status = "applied"
			run.pendingSteerActivityIDs = appendUniqueActivityIDs(run.pendingSteerActivityIDs, command.activityID)
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
