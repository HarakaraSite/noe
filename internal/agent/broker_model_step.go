package agent

import (
	"encoding/json"
	"net/http"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

type beginModelStepRequest struct {
	HistoryRevision int64             `json:"historyRevision,omitempty"`
	RunID           string            `json:"runId"`
	Messages        []json.RawMessage `json:"messages"`
	Tools           []json.RawMessage `json:"tools,omitempty"`
}

type beginModelStepResponse struct {
	ModelStepID string             `json:"modelStepId"`
	Steers      []steerInstruction `json:"steers,omitempty"`
}

func (h *Host) beginModelStep(op *Operation, request beginModelStepRequest) (beginModelStepResponse, error) {
	if request.RunID == "" || (len(request.Messages) == 0 && request.HistoryRevision == 0) || request.HistoryRevision < 0 || (request.HistoryRevision != 0 && request.Messages != nil) {
		return beginModelStepResponse{}, newOperationError(http.StatusBadRequest, "request", "runId and nonempty messages are required")
	}
	for _, message := range request.Messages {
		if !isJSONObject(message) {
			return beginModelStepResponse{}, newOperationError(http.StatusBadRequest, "request", "invalid model message")
		}
	}
	for _, tool := range request.Tools {
		if !isJSONObject(tool) {
			return beginModelStepResponse{}, newOperationError(http.StatusBadRequest, "request", "invalid model tool")
		}
	}

	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || run.operation != op || run.ctx.Err() != nil || run.interrupting || run.finalClosed || op.closed || h.closed {
		h.mu.Unlock()
		return beginModelStepResponse{}, newOperationError(http.StatusConflict, "run", "unknown or finished agent run")
	}
	if request.HistoryRevision != 0 {
		if _, err := conversationAtRevisionLocked(run, request.HistoryRevision); err != nil {
			h.mu.Unlock()
			return beginModelStepResponse{}, err
		}
	}
	if !h.executionGroupOpenLocked(run.group) {
		group := run.group
		h.mu.Unlock()
		code, message := executionLimitError(group)
		return beginModelStepResponse{}, newOperationError(http.StatusConflict, code, message)
	}
	if run.modelStep != nil && !run.modelStep.finished {
		h.mu.Unlock()
		return beginModelStepResponse{}, newOperationError(http.StatusConflict, "model_step", "the previous logical step has not finished")
	}
	stepID, err := randomID()
	if err != nil {
		h.mu.Unlock()
		return beginModelStepResponse{}, newOperationError(http.StatusInternalServerError, "broker", "could not allocate model step")
	}
	activityID, err := randomID()
	if err != nil {
		h.mu.Unlock()
		return beginModelStepResponse{}, newOperationError(http.StatusInternalServerError, "broker", "could not allocate model step activity")
	}
	causedBy := []string(nil)
	predecessors := []string(nil)
	if previous := run.modelStep; previous != nil {
		if !previous.imagesCommitted {
			for _, call := range previous.toolCalls {
				if len(call.images) > 0 {
					h.mu.Unlock()
					return beginModelStepResponse{}, newOperationError(http.StatusConflict, "image", "image tool exchange has not been checkpointed")
				}
			}
			discardStepImages(previous)
		}
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
	return beginModelStepResponse{ModelStepID: stepID, Steers: steers}, nil
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
