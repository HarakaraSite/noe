package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type contextPrepareRequest struct {
	RunID       string            `json:"runId"`
	ModelStepID string            `json:"modelStepId"`
	Messages    []json.RawMessage `json:"messages"`
	Tools       []json.RawMessage `json:"tools,omitempty"`
}

type contextAccessRequest struct {
	RunID       string `json:"runId,omitempty"`
	ModelStepID string `json:"modelStepId,omitempty"`
}

type contextSetRequest struct {
	RunID       string          `json:"runId,omitempty"`
	ModelStepID string          `json:"modelStepId,omitempty"`
	Key         json.RawMessage `json:"key"`
	Text        json.RawMessage `json:"text"`
}

type contextGetResponse struct {
	Context      contextSnapshot `json:"context"`
	Notification string          `json:"notification"`
}

func (b *broker) handleContextPrepare(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request contextPrepareRequest
	if decodeRequest(r, &request) != nil || request.RunID == "" || request.ModelStepID == "" || len(request.Messages) == 0 {
		writeRPCError(w, http.StatusBadRequest, "request", "runId, modelStepId, and nonempty messages are required")
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
	if run == nil || run.operation != op || run.ctx.Err() != nil || run.interrupting || run.finalClosed || op.closed || h.closed || !h.executionGroupOpenLocked(run.group) ||
		run.modelStep == nil || run.modelStep.id != request.ModelStepID || run.modelStep.finished || run.context.modelStepID != request.ModelStepID {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "scope", "context preparation is outside the current model step")
		return
	}
	wasPrepared := run.context.prepared
	snapshot, _, err := prepareContextEstimate(&run.context, run.id, request.Messages, request.Tools)
	if err != nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusBadRequest, "request", "could not estimate model context")
		return
	}
	snapshot = h.storeContextSnapshotLocked(run, time.Now())
	stepActivityID := run.modelStep.activityID
	causes := append([]string(nil), run.modelStep.predecessorActivityIDs...)
	h.mu.Unlock()
	if !wasPrepared {
		h.emitContextUpdated(run, snapshot, "preparing", stepActivityID, causes)
	}
	writeJSON(w, map[string]bool{"prepared": true})
}

func (b *broker) handleContextGet(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request contextAccessRequest
	if decodeRequest(r, &request) != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid context request")
		return
	}
	h := b.host
	h.mu.Lock()
	run, err := h.contextRunForScopeLocked(op, request.RunID, request.ModelStepID)
	if err != nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "scope", err.Error())
		return
	}
	snapshot := run.context.snapshot(run.id, time.Now())
	notification := contextMessage(snapshot, run.context.runtimeInfo)
	h.mu.Unlock()
	writeJSON(w, contextGetResponse{Context: snapshot, Notification: notification})
}

func (b *broker) handleContextSetRuntimeInfo(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request contextSetRequest
	if decodeRequest(r, &request) != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid runtime information")
		return
	}
	var key, text string
	if json.Unmarshal(request.Key, &key) != nil || strings.TrimSpace(key) == "" || json.Unmarshal(request.Text, &text) != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "runtime information requires a nonempty key and string text")
		return
	}
	h := b.host
	h.mu.Lock()
	if op == nil || !op.hookScope || op.hookInvocation == nil || op.hookInvocation.state != "active" || op.ownerRun == nil ||
		op.hookInvocation.event["phase"] != "beforeModelRequest" {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "scope", "runtime information is only available during beforeModelRequest")
		return
	}
	run, err := h.contextRunForScopeLocked(op, request.RunID, request.ModelStepID)
	if err != nil || run.modelStep == nil || run.modelStep.id != request.ModelStepID || run.context.modelStepID != request.ModelStepID || run.modelStep.requestID != "" {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "scope", "runtime information is outside the current beforeModelRequest")
		return
	}
	if run.context.runtimeInfo == nil {
		run.context.runtimeInfo = make(map[string]string)
	}
	run.context.runtimeInfo[key] = text
	var snapshot contextSnapshot
	if run.context.prepared {
		snapshot = recalculatePreparedContext(&run.context, run.id, time.Now())
	} else {
		snapshot = run.context.snapshot(run.id, time.Now())
	}
	snapshot = h.storeContextSnapshotLocked(run, time.Now())
	h.mu.Unlock()
	writeJSON(w, map[string]any{"context": snapshot})
}

func (h *Host) contextRunForScopeLocked(op *Operation, runID, modelStepID string) (*agentRun, error) {
	if op == nil || op.closed || h.closed || op.ctx.Err() != nil {
		return nil, contextScopeError()
	}
	var run *agentRun
	if op.hookScope {
		if op.hookInvocation == nil || op.hookInvocation.state != "active" || op.ownerRun == nil {
			return nil, contextScopeError()
		}
		run = op.ownerRun
	} else if op.task != nil && op.task.run != nil {
		run = op.task.run
	} else if owner := h.currentOwnerRunLocked(op); owner != nil {
		run = owner
	} else if runID != "" {
		candidate := h.runs[runID]
		if candidate != nil && candidate.operation == op {
			run = candidate
		}
	}
	if run == nil || h.runs[run.id] != run || run.ctx.Err() != nil || run.interrupting || run.finalClosed || !h.executionGroupOpenLocked(run.group) {
		return nil, contextScopeError()
	}
	if runID != "" && runID != run.id {
		return nil, contextScopeError()
	}
	if modelStepID != "" && (run.modelStep == nil || run.modelStep.id != modelStepID || run.modelStep.finished) {
		return nil, contextScopeError()
	}
	if op.hookScope {
		if op.hookInvocation.event["runId"] != run.id || (modelStepID != "" && op.hookInvocation.event["modelStepId"] != modelStepID) {
			return nil, contextScopeError()
		}
	}
	return run, nil
}

func contextScopeError() error {
	return &RPCError{Code: "scope", Message: "context is unavailable outside the active run scope"}
}

func (h *Host) markContextSentLocked(run *agentRun, request modelRequest, requestID string, now time.Time) (contextSnapshot, string, []string) {
	messageData, _ := json.Marshal(request.Messages)
	toolData, _ := json.Marshal(request.Tools)
	tokens := estimateContextTokens(len(messageData), len(toolData))
	requestIDCopy := requestID
	run.context.modelStepID = request.ModelStepID
	run.context.estimate = &contextEstimate{
		Tokens: tokens, Method: "json_utf8_bytes/4", Stage: "sent",
		ModelStepID: request.ModelStepID, RequestID: &requestIDCopy, EstimatedAt: now,
	}
	snapshot := h.storeContextSnapshotLocked(run, now)
	activityID := run.activityID
	causes := append([]string(nil), run.causedBy...)
	if request.ModelStepID != "" && run.modelStep != nil && run.modelStep.id == request.ModelStepID {
		activityID = run.modelStep.requestActivityID
		causes = append([]string(nil), run.modelStep.requestCauseActivityIDs...)
	}
	return snapshot, activityID, causes
}

func (h *Host) completeContextRequestLocked(run *agentRun, requestID, status string, usage *usageSummary, now time.Time) (contextSnapshot, string, []string) {
	previous := &contextPrevious{RequestID: requestID, Status: status, MeasuredAt: now}
	if usage != nil {
		previous.PromptTokens = cloneInt64(usage.PromptTokens)
		previous.CachedTokens = cloneInt64(usage.CachedTokens)
	}
	run.context.previous = previous
	snapshot := h.storeContextSnapshotLocked(run, now)
	activityID := run.activityID
	causes := []string(nil)
	if run.modelStep != nil && run.modelStep.requestID == requestID {
		activityID = run.modelStep.requestActivityID
		causes = append([]string(nil), run.modelStep.requestCauseActivityIDs...)
	}
	return snapshot, activityID, causes
}
