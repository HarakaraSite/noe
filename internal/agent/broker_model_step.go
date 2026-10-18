package agent

import (
	"encoding/json"
	"net/http"

	"noe/internal/events"
)

type beginModelStepRequest struct {
	RunID    string            `json:"runId"`
	Messages []json.RawMessage `json:"messages"`
	Tools    []json.RawMessage `json:"tools,omitempty"`
}

type beginModelStepResponse struct {
	ModelStepID string             `json:"modelStepId"`
	Steers      []steerInstruction `json:"steers,omitempty"`
}

func (b *broker) handleBeginModelStep(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request beginModelStepRequest
	if decodeRequest(r, &request) != nil || request.RunID == "" || len(request.Messages) == 0 {
		writeRPCError(w, http.StatusBadRequest, "request", "runId and nonempty messages are required")
		return
	}
	for _, message := range request.Messages {
		if !isJSONObject(message) {
			writeRPCError(w, http.StatusBadRequest, "request", "invalid model message")
			return
		}
	}
	for _, tool := range request.Tools {
		if !isJSONObject(tool) {
			writeRPCError(w, http.StatusBadRequest, "request", "invalid model tool")
			return
		}
	}

	h := b.host
	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || run.operation != op || run.ctx.Err() != nil || run.interrupting || run.finalClosed || op.closed || h.closed {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "unknown or finished agent run")
		return
	}
	if !h.executionGroupOpenLocked(run.group) {
		group := run.group
		h.mu.Unlock()
		code, message := executionLimitError(group)
		writeRPCError(w, http.StatusConflict, code, message)
		return
	}
	if run.modelStep != nil && !run.modelStep.finished {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "model_step", "the previous logical step has not finished")
		return
	}
	stepID, err := randomID()
	if err != nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate model step")
		return
	}
	activityID, err := randomID()
	if err != nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate model step activity")
		return
	}
	causedBy := []string(nil)
	predecessors := []string(nil)
	if previous := run.modelStep; previous != nil {
		predecessors = completedStepActivityIDs(previous)
	} else if run.activityID != "" {
		predecessors = append(predecessors, run.activityID)
	}
	predecessors = appendUniqueActivityIDs(predecessors, run.pendingSteerActivityIDs...)
	causedBy = append(causedBy, predecessors...)
	run.modelStep = &modelStepState{
		id: stepID, activityID: activityID,
		predecessorActivityIDs: append([]string(nil), predecessors...),
	}
	run.context.resetStep(stepID)
	steers := h.deliverPendingSteersLocked(run)
	stepEvent := events.Event{
		"type": "model.step.started", "runId": run.id,
		"parentRunId": run.parentRunID, "modelStepId": stepID,
	}
	addActivityFields(stepEvent, activityID, causedBy, activityOrigin{
		Kind: "model", RunID: run.id, AgentID: run.agentID(), ModelStepID: stepID,
	})
	if run.group != nil {
		stepEvent["groupId"] = run.group.id
		stepEvent["execution"] = run.group.snapshotLocked()
	}
	if agentID := run.agentID(); agentID != "" {
		stepEvent["agentId"] = agentID
	}
	h.mu.Unlock()

	h.observe(stepEvent)
	writeJSON(w, beginModelStepResponse{ModelStepID: stepID, Steers: steers})
}

func completedStepActivityIDs(step *modelStepState) []string {
	if step == nil {
		return nil
	}
	causes := []string(nil)
	if step.requestActivityID != "" {
		causes = appendUniqueActivityIDs(causes, step.requestActivityID)
	}
	for _, toolCallID := range step.toolCallOrder {
		call := step.toolCalls[toolCallID]
		if call != nil && call.resultRecorded {
			causes = appendUniqueActivityIDs(causes, call.activityID)
		}
	}
	return causes
}
