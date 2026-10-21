package agent

import (
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

type SteerRequest struct {
	RunID       string `json:"runId"`
	Instruction string `json:"instruction"`
	CommandID   string `json:"commandId,omitempty"`
}

type steerRequest = SteerRequest

type steerInstruction struct {
	CommandID   string `json:"commandId"`
	Instruction string `json:"instruction"`
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

type steerLifecycleEvent struct {
	run     *agentRun
	command *steerCommand
	status  string
}

func (h *Host) steer(op *Operation, request steerRequest) (commandReceipt, error) {
	if request.RunID == "" || strings.TrimSpace(request.Instruction) == "" {
		return commandReceipt{}, newOperationError(400, "request", "runId and instruction are required")
	}
	if request.CommandID == "" {
		id, err := randomID()
		if err != nil {
			return commandReceipt{}, newOperationError(500, "broker", "could not allocate command ID")
		}
		request.CommandID = id
	} else if strings.TrimSpace(request.CommandID) == "" {
		return commandReceipt{}, newOperationError(400, "request", "commandId must be nonempty")
	}

	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || !h.canManageRunLocked(op, run) || run.ctx.Err() != nil || run.interrupting || op.closed || h.closed || run.finalClosed {
		h.mu.Unlock()
		return commandReceipt{}, newOperationError(409, "run", "unknown, unowned, or finished agent run")
	}
	if !h.executionGroupOpenLocked(run.group) {
		group := run.group
		h.mu.Unlock()
		code, message := executionLimitError(group)
		return commandReceipt{}, newOperationError(409, code, message)
	}
	if previous := run.steerByID[request.CommandID]; previous != nil {
		if previous.instruction != request.Instruction {
			h.mu.Unlock()
			return commandReceipt{}, newOperationError(409, "command", "commandId already used for a different instruction")
		}
		response := commandReceipt{CommandID: request.CommandID, Status: previous.status}
		h.mu.Unlock()
		return response, nil
	}
	activityID, err := randomID()
	if err != nil {
		h.mu.Unlock()
		return commandReceipt{}, newOperationError(500, "broker", "could not allocate steer activity")
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
	return commandReceipt{CommandID: command.id, Status: "accepted"}, nil
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
	if status == "accepted" {
		fields["instruction"] = command.instruction
	}
	addActivityFields(fields, command.activityID, command.causedBy, command.origin)
	return fields
}
