package agent

import (
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

type interruptRequest struct {
	RunID   string `json:"runId,omitempty"`
	AgentID string `json:"agentId,omitempty"`
}

type interruptResponse struct {
	Target  string `json:"target"`
	Status  string `json:"status"`
	RunID   string `json:"runId,omitempty"`
	AgentID string `json:"agentId,omitempty"`
}

func (h *Host) interrupt(op *Operation, request interruptRequest) (interruptResponse, error) {
	hasRun := strings.TrimSpace(request.RunID) != ""
	hasAgent := strings.TrimSpace(request.AgentID) != ""
	if hasRun == hasAgent {
		return interruptResponse{}, newOperationError(400, "request", "specify exactly one of runId or agentId")
	}
	if hasRun {
		return h.interruptRun(op, request.RunID)
	}
	return h.interruptInstance(op, request.AgentID)
}

// interruptRun records a managed task cancellation under Host.mu, cancels
// immediately, and leaves event publication and process cleanup independent.
func (h *Host) interruptRun(op *Operation, runID string) (interruptResponse, error) {
	h.mu.Lock()
	run := h.runs[runID]
	if run == nil || !h.canManageRunLocked(op, run) {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(409, "run", "unknown or unowned agent run")
	}
	if op == nil || op.closed || op.ctx.Err() != nil || h.closed {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(409, "scope", "agent operation has ended")
	}
	if run.finalClosed {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(409, "run", "agent run is already finished")
	}
	instance := run.instance
	if instance == nil {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(409, "run", "only managed task runs can be interrupted by runId")
	}
	if _, accepted := instance.runInterruptAccepted[run.id]; accepted {
		// Retrying an accepted interrupt is idempotent while the task is being
		// reaped. It does not publish a second acceptance event.
		h.mu.Unlock()
		return interruptResponse{Target: "run", Status: "accepted", RunID: run.id}, nil
	}
	if run.interrupting {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(409, "run", "managed task is already stopping")
	}
	if run.ctx.Err() != nil {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(409, "run", "agent run is already finished")
	}
	if instance.taskCancel == nil || instance.state == "stopping" || instance.state == "stopped" || instance.state == "failed" {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(409, "run", "managed task is no longer interruptible")
	}
	fields, err := h.interruptActivityLocked(op, "run")
	if err != nil {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(500, "broker", "could not allocate interrupt activity")
	}
	accepted := make(chan struct{})
	run.interrupting = true
	instance.interruptedRunID = run.id
	instance.runInterruptAccepted[run.id] = accepted
	cancel := instance.taskCancel
	h.mu.Unlock()

	// Cancel before observer callbacks can run. The task process, active model
	// request, generated JS, and descendants all inherit this controller.
	cancel()
	go func() {
		h.observeRun("interrupt.accepted", run.id, run, fields)
		close(accepted)
	}()
	return interruptResponse{Target: "run", Status: "accepted", RunID: run.id}, nil
}

func (h *Host) interruptInstance(op *Operation, agentID string) (interruptResponse, error) {
	if op == nil {
		return interruptResponse{}, newOperationError(409, "scope", "agent operation has ended")
	}
	h.mu.Lock()
	instance := h.instances[agentID]
	if instance == nil || !h.canManageLocked(op, instance) {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(409, "agent", "unknown or unowned agent")
	}
	if op.closed || op.ctx.Err() != nil || h.closed {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(409, "scope", "agent operation has ended")
	}
	if instance.state == "stopping" && instance.interruptAccepted != nil && instance.reason == "interrupted" {
		// Repeated whole-agent interrupt calls report the original acceptance.
		h.mu.Unlock()
		return interruptResponse{Target: "agent", Status: "accepted", AgentID: instance.id}, nil
	}
	if instance.state == "stopping" || instance.state == "stopped" || instance.state == "failed" {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(409, "agent", "agent is already stopping or finished")
	}
	fields, err := h.interruptActivityLocked(op, "agent")
	if err != nil {
		h.mu.Unlock()
		return interruptResponse{}, newOperationError(500, "broker", "could not allocate interrupt activity")
	}

	accepted := make(chan struct{})
	settled := make(chan struct{})
	queued := instance.queue
	instance.queue = nil
	for _, command := range queued {
		command.status = "cancelled"
	}
	currentRunID := instance.currentRunID
	if run := h.runs[currentRunID]; run != nil {
		// Serialize whole-instance interruption with final-answer commitment.
		run.interrupting = true
	}
	instance.state = "stopping"
	instance.reason = "interrupted"
	instance.interruptAccepted = accepted
	instance.interruptSettled = settled
	cancel := instance.cancel
	h.mu.Unlock()

	cancel()
	go func() {
		if currentRunID != "" {
			fields["runId"] = currentRunID
		}
		instance.event("interrupt.accepted", fields)
		close(accepted)
		for _, command := range queued {
			<-command.accepted
			instance.commandEvent(command, "cancelled", "")
		}
		close(settled)
	}()
	return interruptResponse{Target: "agent", Status: "accepted", AgentID: instance.id}, nil
}

// interruptActivityLocked describes the requesting operation independently of
// the target's creation. Accepted retries are handled before allocating IDs.
func (h *Host) interruptActivityLocked(op *Operation, target string) (events.Event, error) {
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	origin := op.activityOrigin
	if origin.Kind == "" {
		origin.Kind = "script"
		if op.control {
			origin.Kind = "human"
		}
	}
	causes := append([]string(nil), op.causeActivityIDs...)
	if sender := h.currentOwnerRunLocked(op); sender != nil {
		if origin.RunID == "" {
			origin.RunID = sender.id
		}
		if origin.AgentID == "" {
			origin.AgentID = sender.agentID()
		}
		if len(causes) == 0 && !op.control && sender.activityID != "" {
			causes = []string{sender.activityID}
		}
	}
	fields := events.Event{"target": target, "status": "accepted"}
	addActivityFields(fields, id, causes, origin)
	// An empty caller cause must override, rather than inherit, target causes.
	if len(causes) == 0 {
		fields["causedBy"] = []string{}
	}
	return fields, nil
}
