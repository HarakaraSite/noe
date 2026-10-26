package agent

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type contextPrepareRequest struct {
	HistoryRevision int64                 `json:"historyRevision,omitempty"`
	RunID           string                `json:"runId"`
	ModelStepID     string                `json:"modelStepId"`
	Messages        []json.RawMessage     `json:"messages"`
	Tools           []json.RawMessage     `json:"tools,omitempty"`
	History         *contextHistoryLayout `json:"history,omitempty"`
	Snapshot        *conversationSnapshot `json:"snapshot,omitempty"`
	Final           bool                  `json:"final,omitempty"`
	AppliedPlanID   string                `json:"appliedPlanId,omitempty"`
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

type contextPrepareResponse struct {
	Prepared bool             `json:"prepared"`
	Plan     *contextTrimPlan `json:"plan,omitempty"`
}

func (h *Host) contextPrepare(op *Operation, request contextPrepareRequest) (contextPrepareResponse, error) {
	if request.RunID == "" || request.ModelStepID == "" || (len(request.Messages) == 0 && request.HistoryRevision == 0) || request.HistoryRevision < 0 || (request.HistoryRevision != 0 && (request.Messages != nil || request.History != nil || request.Snapshot != nil || request.AppliedPlanID != "")) {
		return contextPrepareResponse{}, newOperationError(http.StatusBadRequest, "request", "runId, modelStepId, and nonempty messages are required")
	}
	for _, message := range request.Messages {
		if !isJSONObject(message) {
			return contextPrepareResponse{}, newOperationError(http.StatusBadRequest, "request", "invalid model message")
		}
	}
	for _, tool := range request.Tools {
		if !isJSONObject(tool) {
			return contextPrepareResponse{}, newOperationError(http.StatusBadRequest, "request", "invalid model tool")
		}
	}
	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || run.operation != op || run.ctx.Err() != nil || run.interrupting || run.finalClosed || op.closed || h.closed || !h.executionGroupOpenLocked(run.group) ||
		run.modelStep == nil || run.modelStep.id != request.ModelStepID || run.modelStep.finished || run.context.modelStepID != request.ModelStepID {
		h.mu.Unlock()
		return contextPrepareResponse{}, newOperationError(http.StatusConflict, "scope", "context preparation is outside the current model step")
	}
	if request.HistoryRevision != 0 {
		conversation, err := conversationAtRevisionLocked(run, request.HistoryRevision)
		if err != nil {
			h.mu.Unlock()
			return contextPrepareResponse{}, err
		}
		request.Messages, request.History = conversation.messageView(), conversation.layout()
	}
	if err := h.validateImagesLocked(run, request.Messages); err != nil {
		h.mu.Unlock()
		return contextPrepareResponse{}, newOperationError(http.StatusBadRequest, "image", err.Error())
	}
	run.context.imageEstimates = imageEstimatesLocked(run)
	stats, statsErr := measureContextHistory(request.Messages, request.Tools, run.context.imageEstimates)
	if statsErr != nil {
		h.mu.Unlock()
		return contextPrepareResponse{}, newOperationError(http.StatusBadRequest, "request", "could not estimate model context")
	}
	fixed, execution, weights, partsErr := historyPartsFromStats(request.Messages, request.History, run.context.imageEstimates, stats)
	if partsErr != nil {
		h.mu.Unlock()
		return contextPrepareResponse{}, newOperationError(http.StatusBadRequest, "request", partsErr.Error())
	}
	if run.context.pendingPlan != nil && request.AppliedPlanID == "" {
		h.mu.Unlock()
		return contextPrepareResponse{}, newOperationError(http.StatusConflict, "context_plan", "context trim plan must be acknowledged before preparing again")
	}
	if request.AppliedPlanID != "" && run.instance != nil {
		if run.context.pendingPlan == nil {
			h.mu.Unlock()
			return contextPrepareResponse{}, newOperationError(http.StatusConflict, "context_plan", "no context trim plan is pending")
		}
		if err := validateConversationSnapshotMatches(request.Snapshot, request.Messages, request.History); err != nil {
			h.mu.Unlock()
			return contextPrepareResponse{}, newOperationError(http.StatusBadRequest, "conversation", err.Error())
		}
		if err := validateConversationTrim(run.instance.conversation, request.Snapshot, run.context.pendingPlan.plan.RemoveIDs); err != nil {
			h.mu.Unlock()
			return contextPrepareResponse{}, newOperationError(http.StatusConflict, "conversation", err.Error())
		}
	}
	run.context.fixedTokens, run.context.executionTokens = fixed, execution
	var trimmed *contextTrimSnapshot
	if request.AppliedPlanID != "" {
		var ackErr error
		trimmed, ackErr = run.context.acknowledgeTrim(request.AppliedPlanID, request.History, request.Messages, request.Tools)
		if ackErr != nil {
			h.mu.Unlock()
			return contextPrepareResponse{}, newOperationError(http.StatusConflict, "context_plan", ackErr.Error())
		}
		if run.instance != nil {
			run.instance.conversation = cloneConversationSnapshot(request.Snapshot)
		}
	}
	if request.AppliedPlanID != "" {
		reconcileImagesLocked(run, request.Messages)
	}
	failures := pendingHookFailures(run)
	run.context.hookFailureNotice = hookFailureNotice(failures)
	run.context.preparedNotification = nil
	prepareContextEstimateFromStats(&run.context, run.id, stats)
	var err error
	var plan *contextTrimPlan
	if request.Final && request.AppliedPlanID == "" {
		plan, err = run.context.planTrimWithWeights(request.Messages, request.Tools, request.History, weights)
		if err != nil {
			h.mu.Unlock()
			return contextPrepareResponse{}, newOperationError(http.StatusBadRequest, "context_plan", err.Error())
		}
	}
	run.context.finalPrepared = request.Final && plan == nil
	if run.context.finalPrepared {
		run.context.preparedDigest = contextHistoryDigest(request.Messages, request.Tools)
		// Planning can change the diagnostic budget flags. Freeze the final
		// text only after planning/acknowledgement, and estimate that same text.
		snapshot := recalculatePreparedContext(&run.context, run.id, time.Now())
		notice := &preparedContextNotification{
			text: contextMessage(snapshot, run.context.runtimeInfo, run.context.hookFailureNotice),
		}
		for _, failure := range failures {
			notice.hookFailureIDs = append(notice.hookFailureIDs, failure.InvocationID)
		}
		run.context.preparedNotification = notice
	}
	snapshot := h.storeContextSnapshotLocked(run, time.Now())
	stepActivityID := run.modelStep.activityID
	causes := append([]string(nil), run.modelStep.predecessorActivityIDs...)
	h.mu.Unlock()
	if trimmed != nil {
		h.emitContextTrimmed(run, snapshot, *trimmed, request.ModelStepID, appendUniqueActivityIDs(causes, stepActivityID))
	}
	return contextPrepareResponse{Prepared: true, Plan: plan}, nil
}

func (h *Host) contextGet(op *Operation, request contextAccessRequest) (contextGetResponse, error) {
	h.mu.Lock()
	run, err := h.contextRunForScopeLocked(op, request.RunID, request.ModelStepID)
	if err != nil {
		h.mu.Unlock()
		return contextGetResponse{}, err
	}
	snapshot := run.context.snapshot(run.id, time.Now())
	notification := ""
	if notice := run.context.preparedNotification; notice != nil {
		notification = notice.text
	} else {
		notification = contextMessage(snapshot, run.context.runtimeInfo, hookFailureNotice(pendingHookFailures(run)))
	}
	h.mu.Unlock()
	return contextGetResponse{Context: snapshot, Notification: notification}, nil
}

func (h *Host) contextSetRuntimeInfo(op *Operation, request contextSetRequest) (map[string]any, error) {
	var key, text string
	if json.Unmarshal(request.Key, &key) != nil || strings.TrimSpace(key) == "" || json.Unmarshal(request.Text, &text) != nil {
		return nil, newOperationError(http.StatusBadRequest, "request", "runtime information requires a nonempty key and string text")
	}
	h.mu.Lock()
	if op == nil || !op.hookScope || op.hookInvocation == nil || op.hookInvocation.state != "active" || op.ownerRun == nil ||
		op.hookInvocation.event["phase"] != "beforeModelRequest" {
		h.mu.Unlock()
		return nil, newOperationError(http.StatusConflict, "scope", "runtime information is only available during beforeModelRequest")
	}
	run, err := h.contextRunForScopeLocked(op, request.RunID, request.ModelStepID)
	if err != nil || run.modelStep == nil || run.modelStep.id != request.ModelStepID || run.context.modelStepID != request.ModelStepID || run.modelStep.requestID != "" {
		h.mu.Unlock()
		return nil, newOperationError(http.StatusConflict, "scope", "runtime information is outside the current beforeModelRequest")
	}
	if run.context.runtimeInfo == nil {
		run.context.runtimeInfo = make(map[string]string)
	}
	run.context.runtimeInfo[key] = text
	run.context.finalPrepared = false
	run.context.preparedNotification = nil
	run.context.hookFailureNotice = hookFailureNotice(pendingHookFailures(run))
	if run.context.prepared {
		recalculatePreparedContext(&run.context, run.id, time.Now())
	}
	snapshot := h.storeContextSnapshotLocked(run, time.Now())
	h.mu.Unlock()
	return map[string]any{"context": snapshot}, nil
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
	return &RequestError{Code: "scope", Message: "context is unavailable outside the active run scope"}
}

func (h *Host) markContextSentLocked(run *agentRun, request modelRequest, requestID string, now time.Time) {
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
	h.storeContextSnapshotLocked(run, now)
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
