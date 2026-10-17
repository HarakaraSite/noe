package agent

import (
	"net/http"
	"strings"

	"noe/internal/events"
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

// handleInterrupt accepts either a task-scoped cancellation or a whole-agent
// stop. It records the request under Host.mu, cancels immediately, and leaves
// event publication and process cleanup to independent goroutines.
func (b *broker) handleInterrupt(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request interruptRequest
	if err := decodeRequest(r, &request); err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid interrupt request")
		return
	}
	hasRun := strings.TrimSpace(request.RunID) != ""
	hasAgent := strings.TrimSpace(request.AgentID) != ""
	if hasRun == hasAgent {
		writeRPCError(w, http.StatusBadRequest, "request", "specify exactly one of runId or agentId")
		return
	}
	h := b.host
	if hasRun {
		b.interruptRun(w, op, request.RunID)
		return
	}
	h.interruptInstance(w, op, request.AgentID)
}

func (b *broker) interruptRun(w http.ResponseWriter, op *Operation, runID string) {
	h := b.host
	h.mu.Lock()
	run := h.runs[runID]
	if run == nil || !h.canManageRunLocked(op, run) {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "unknown or unowned agent run")
		return
	}
	if op == nil || op.closed || op.ctx.Err() != nil || h.closed {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "scope", "agent operation has ended")
		return
	}
	if run.finalClosed {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "agent run is already finished")
		return
	}
	instance := run.instance
	if instance == nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "only managed task runs can be interrupted by runId")
		return
	}
	if _, accepted := instance.runInterruptAccepted[run.id]; accepted {
		// Retrying an accepted interrupt is idempotent while the task is being
		// reaped. It does not publish a second acceptance event.
		h.mu.Unlock()
		writeJSON(w, interruptResponse{Target: "run", Status: "accepted", RunID: run.id})
		return
	}
	if run.interrupting {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "managed task is already stopping")
		return
	}
	if run.ctx.Err() != nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "agent run is already finished")
		return
	}
	if instance.taskCancel == nil || instance.state == "stopping" || instance.state == "stopped" || instance.state == "failed" {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "managed task is no longer interruptible")
		return
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
		h.observeRun("interrupt.accepted", run.id, run, events.Event{
			"target": "run", "status": "accepted",
		})
		close(accepted)
	}()
	writeJSON(w, interruptResponse{Target: "run", Status: "accepted", RunID: run.id})
}

func (h *Host) interruptInstance(w http.ResponseWriter, op *Operation, agentID string) {
	if op == nil {
		writeRPCError(w, http.StatusConflict, "scope", "agent operation has ended")
		return
	}
	h.mu.Lock()
	instance := h.instances[agentID]
	if instance == nil || !h.canManageLocked(op, instance) {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "agent", "unknown or unowned agent")
		return
	}
	if op == nil || op.closed || op.ctx.Err() != nil || h.closed {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "scope", "agent operation has ended")
		return
	}
	if instance.state == "stopping" && instance.interruptAccepted != nil && instance.reason == "interrupted" {
		// Repeated whole-agent interrupt calls report the original acceptance.
		h.mu.Unlock()
		writeJSON(w, interruptResponse{Target: "agent", Status: "accepted", AgentID: instance.id})
		return
	}
	if instance.state == "stopping" || instance.state == "stopped" || instance.state == "failed" {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "agent", "agent is already stopping or finished")
		return
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
		fields := events.Event{"target": "agent", "status": "accepted"}
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
	writeJSON(w, interruptResponse{Target: "agent", Status: "accepted", AgentID: instance.id})
}
