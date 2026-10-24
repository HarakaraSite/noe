package agent

import "forge.harakara.site/littleisland/noe/v2/internal/events"

func (h *Host) interruptInstance(op *Operation, agentID string) (StopAgentResponse, error) {
	if op == nil {
		return StopAgentResponse{}, newOperationError(409, "scope", "agent operation has ended")
	}
	h.mu.Lock()
	instance := h.instances[agentID]
	if instance == nil || !h.canManageLocked(op, instance) {
		h.mu.Unlock()
		return StopAgentResponse{}, newOperationError(409, "agent", "unknown or unowned agent")
	}
	if op.closed || op.ctx.Err() != nil || h.closed {
		h.mu.Unlock()
		return StopAgentResponse{}, newOperationError(409, "scope", "agent operation has ended")
	}
	if instance.state == "stopping" && instance.interruptAccepted != nil && instance.reason == "interrupted" {
		// Repeated stop calls report the original acceptance.
		h.mu.Unlock()
		return StopAgentResponse{Status: "accepted", AgentID: instance.id}, nil
	}
	if instance.state == "stopping" || instance.state == "stopped" || instance.state == "failed" {
		h.mu.Unlock()
		return StopAgentResponse{}, newOperationError(409, "agent", "agent is already stopping or finished")
	}
	fields, err := h.controlActivityLocked(op, "agent")
	if err != nil {
		h.mu.Unlock()
		return StopAgentResponse{}, newOperationError(500, "request", "could not allocate stop activity")
	}

	accepted := make(chan struct{})
	settled := make(chan struct{})
	queued := instance.settleQueuedCommandsLocked("interrupted")
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
			instance.publishCommandOutcome(command)
		}
		close(settled)
	}()
	return StopAgentResponse{Status: "accepted", AgentID: instance.id}, nil
}

// controlActivityLocked describes the requesting operation independently of
// the target's creation. Accepted retries are handled before allocating IDs.
func (h *Host) controlActivityLocked(op *Operation, target string) (events.Event, error) {
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
