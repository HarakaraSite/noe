package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type contextPrepareRequest struct {
	RunID         string                `json:"runId"`
	ModelStepID   string                `json:"modelStepId"`
	Messages      []json.RawMessage     `json:"messages"`
	Tools         []json.RawMessage     `json:"tools,omitempty"`
	History       *contextHistoryLayout `json:"history,omitempty"`
	Snapshot      *conversationSnapshot `json:"snapshot,omitempty"`
	Final         bool                  `json:"final,omitempty"`
	AppliedPlanID string                `json:"appliedPlanId,omitempty"`
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
	if _, err := h.expandImagesLocked(run, request.Messages); err != nil {
		h.mu.Unlock()
		writeRPCError(w, 400, "image", err.Error())
		return
	}
	wasPrepared := run.context.prepared
	run.context.imageEstimates = imageEstimatesLocked(run)
	fixed, execution, _, partsErr := historyParts(request.Messages, request.Tools, request.History, run.context.imageEstimates)
	if partsErr != nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusBadRequest, "request", partsErr.Error())
		return
	}
	if run.context.pendingPlan != nil && request.AppliedPlanID == "" {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "context_plan", "context trim plan must be acknowledged before preparing again")
		return
	}
	if request.AppliedPlanID != "" && run.instance != nil {
		if run.context.pendingPlan == nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "context_plan", "no context trim plan is pending")
			return
		}
		if err := validateConversationSnapshotMatches(request.Snapshot, request.Messages, request.History); err != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusBadRequest, "conversation", err.Error())
			return
		}
		if err := validateConversationTrim(run.instance.conversation, request.Snapshot, run.context.pendingPlan.plan.RemoveIDs); err != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "conversation", err.Error())
			return
		}
	}
	run.context.fixedTokens, run.context.executionTokens = fixed, execution
	var trimmed *contextTrimSnapshot
	if request.AppliedPlanID != "" {
		var ackErr error
		trimmed, ackErr = run.context.acknowledgeTrim(request.AppliedPlanID, request.History, request.Messages, request.Tools)
		if ackErr != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "context_plan", ackErr.Error())
			return
		}
		if run.instance != nil {
			run.instance.conversation = cloneConversationSnapshot(request.Snapshot)
		}
	}
	if request.AppliedPlanID != "" {
		reconcileImagesLocked(run, request.Messages)
	}
	snapshot, _, err := prepareContextEstimate(&run.context, run.id, request.Messages, request.Tools)
	if err != nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusBadRequest, "request", "could not estimate model context")
		return
	}
	var plan *contextTrimPlan
	if request.Final && request.AppliedPlanID == "" {
		plan, err = run.context.planTrim(request.Messages, request.Tools, request.History)
		if err != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusBadRequest, "context_plan", err.Error())
			return
		}
	}
	run.context.finalPrepared = request.Final && plan == nil
	if run.context.finalPrepared {
		run.context.preparedDigest = contextHistoryDigest(request.Messages, request.Tools)
	}
	snapshot = h.storeContextSnapshotLocked(run, time.Now())
	stepActivityID := run.modelStep.activityID
	causes := append([]string(nil), run.modelStep.predecessorActivityIDs...)
	h.mu.Unlock()
	if trimmed != nil {
		h.emitContextTrimmed(run, snapshot, *trimmed, request.ModelStepID, appendUniqueActivityIDs(causes, stepActivityID))
	}
	if !wasPrepared {
		h.emitContextUpdated(run, snapshot, "preparing", stepActivityID, causes)
	}
	writeJSON(w, struct {
		Prepared bool             `json:"prepared"`
		Plan     *contextTrimPlan `json:"plan,omitempty"`
	}{true, plan})
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
	imageTokens := historyImageTokens(request.Messages, run.context.imageEstimates)
	tokens := estimateContextTokens(len(messageData), len(toolData)) + imageTokens
	requestIDCopy := requestID
	run.context.modelStepID = request.ModelStepID
	run.context.estimate = &contextEstimate{
		Tokens: tokens, ImageTokens: imageTokens, Method: imageEstimateMethod(imageTokens), Stage: "sent",
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
