package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

type modelRequest struct {
	RunID       string            `json:"runId"`
	ModelStepID string            `json:"modelStepId,omitempty"`
	Model       string            `json:"model"`
	Messages    []json.RawMessage `json:"messages"`
	Tools       []json.RawMessage `json:"tools,omitempty"`
	CacheKey    string            `json:"cacheKey,omitempty"`
	MaxTokens   int               `json:"maxTokens,omitempty"`
	Effort      string            `json:"effort,omitempty"`
}

type modelResponse struct {
	RequestID    string             `json:"requestId,omitempty"`
	Message      json.RawMessage    `json:"message,omitempty"`
	FinishReason string             `json:"finishReason,omitempty"`
	Usage        json.RawMessage    `json:"usage,omitempty"`
	Steers       []steerInstruction `json:"steers,omitempty"`
}

func (b *broker) handleModel(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request modelRequest
	decodeErr := decodeRequest(r, &request)
	effort, validEffort := provider.NormalizeEffort(request.Effort)
	if decodeErr != nil || request.RunID == "" || strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 || request.MaxTokens < 0 || !validEffort {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid model request")
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
	requestID, activityID := "", ""
	causedBy := []string(nil)
	h.mu.Lock()
	run := h.runs[request.RunID]
	valid := run != nil && run.operation == op && run.ctx.Err() == nil && !run.interrupting && !op.closed && !h.closed && !run.finalClosed
	if !valid {
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
	if request.Model != run.config.Model || request.MaxTokens != run.config.MaxTokens || effort != run.config.Effort {
		h.mu.Unlock()
		writeRPCError(w, http.StatusBadRequest, "request", "model request does not match agent run configuration")
		return
	}
	if request.ModelStepID != "" && (run.modelStep == nil || run.modelStep.id != request.ModelStepID || run.modelStep.finished) {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "model_step", "model request does not match the active logical step")
		return
	}
	expandedMessages, imageErr := h.expandImagesLocked(run, request.Messages)
	if imageErr != nil {
		h.mu.Unlock()
		writeRPCError(w, 400, "image", imageErr.Error())
		return
	}
	steers := h.deliverPendingSteersLocked(run)
	if len(steers) == 0 {
		if run.context.pendingPlan != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "context_plan", "context trim has not been acknowledged")
			return
		}
		if run.context.finalPrepared && (len(request.Messages) < 2 || contextHistoryDigest(request.Messages[:len(request.Messages)-1], request.Tools) != run.context.preparedDigest) {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "context_plan", "model transcript differs from prepared context")
			return
		}
		if run.config.MaxSteps > 0 && run.providerCalls >= run.config.MaxSteps {
			limit := run.config.MaxSteps
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "max_steps", fmt.Sprintf("agent reached maxSteps=%d without a final answer", limit))
			return
		}
		var err error
		requestID, err = randomID()
		if err == nil {
			activityID, err = randomID()
		}
		if err != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate model request activity")
			return
		}
		origin := activityOrigin{Kind: "model", RunID: run.id, AgentID: run.agentID(), ModelStepID: request.ModelStepID}
		limitAction, admitted := h.reserveExecutionLocked(run.group, "providerCalls", "agent", activityID, origin)
		if !admitted {
			group := run.group
			h.mu.Unlock()
			h.finishExecutionLimit(limitAction)
			code, message := executionLimitError(group)
			writeRPCError(w, http.StatusConflict, code, message)
			return
		}
		if request.ModelStepID != "" {
			step := run.modelStep
			step.requestID = requestID
			step.requestActivityID = activityID
			causedBy = appendUniqueActivityIDs(causedBy, step.activityID)
			causedBy = appendUniqueActivityIDs(causedBy, step.beforeHookActivityIDs...)
			causedBy = appendUniqueActivityIDs(causedBy, run.pendingSteerActivityIDs...)
			causedBy = appendUniqueActivityIDs(causedBy, run.context.pendingTrimActivityIDs...)
			step.requestCauseActivityIDs = append([]string(nil), causedBy...)
		} else if run.activityID != "" {
			causedBy = append(causedBy, run.activityID)
		}
		// Applied steering instructions are consumed by the next actual
		// provider request, after any steer-only model RPCs have returned.
		run.pendingSteerActivityIDs = nil
		// Count only requests that cross the provider boundary. Returning a
		// queued steer batch does not consume the task's model-call budget.
		run.providerCalls++
		h.markContextSentLocked(run, request, requestID, time.Now())
		run.context.pendingNotice = nil
		run.context.pendingTrimActivityIDs = nil
		// markContextSentLocked stores the request snapshot before the notice is
		// consumed. Persist the post-admission state too so a later queue task
		// does not repeat an already delivered trim notice or its causes.
		if run.instance != nil {
			h.storeContextSnapshotLocked(run, time.Now())
		}
	}
	h.mu.Unlock()
	if len(steers) != 0 {
		writeJSON(w, modelResponse{Steers: steers})
		return
	}
	origin := activityOrigin{Kind: "model", RunID: run.id, AgentID: run.agentID(), ModelStepID: request.ModelStepID}
	if h.opts.Observe != nil {
		fields := events.Event{"requestId": requestID, "status": "started"}
		if request.ModelStepID != "" {
			fields["modelStepId"] = request.ModelStepID
		}
		addActivityFields(fields, activityID, causedBy, origin)
		h.observeRun("model.started", request.RunID, run, fields)
	}
	ctx, cancel := context.WithCancel(run.ctx)
	stopRequest := context.AfterFunc(r.Context(), cancel)
	defer func() { stopRequest(); cancel() }()
	started := time.Now()
	var observeStream func(provider.StreamEvent)
	if h.opts.Observe != nil {
		observeStream = func(streamEvent provider.StreamEvent) {
			switch streamEvent.Type {
			case "thinking", "thinking_summary", "message_delta":
				fields := events.Event{"requestId": requestID, "text": streamEvent.Text}
				if request.ModelStepID != "" {
					fields["modelStepId"] = request.ModelStepID
				}
				addActivityFields(fields, activityID, causedBy, origin)
				h.observeRun(streamEvent.Type, request.RunID, run, fields)
			}
		}
	}
	providerRequest := provider.Request{
		Provider: run.config.Provider, RunID: request.RunID,
		Model: run.config.Model, Messages: expandedMessages, Tools: request.Tools,
		CacheKey: request.CacheKey, MaxTokens: run.config.MaxTokens, Effort: run.config.Effort,
		Observe: observeStream,
	}
	// Admission above is the provider-call boundary. Even if cancellation
	// arrives after admission (for example while the started observer runs),
	// enter the adapter with the cancelled context so accounting and terminal
	// events describe the same attempt.
	response, err := h.provider.Generate(ctx, providerRequest)
	if err != nil {
		status, code, message := classifyProviderError(err, ctx)
		modelStatus := "failed"
		if code == "cancelled" {
			modelStatus = "cancelled"
		}
		h.mu.Lock()
		contextSnapshot, contextActivityID, contextCauses := h.completeContextRequestLocked(run, requestID, modelStatus, nil, time.Now())
		h.mu.Unlock()
		h.emitContextUpdated(run, contextSnapshot, modelStatus, contextActivityID, contextCauses)
		h.traceEvent(traceEvent{
			RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
			Status: "provider_error", Duration: time.Since(started),
		})
		if h.opts.Observe != nil {
			modelStatus := "failed"
			if code == "cancelled" {
				modelStatus = "cancelled"
			}
			fields := events.Event{"requestId": requestID, "status": modelStatus, "reason": code, "duration": time.Since(started)}
			if request.ModelStepID != "" {
				fields["modelStepId"] = request.ModelStepID
			}
			addActivityFields(fields, activityID, causedBy, origin)
			h.observeRun("model.failed", request.RunID, run, fields)
		}
		writeRPCError(w, status, code, message)
		return
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		status, code, message := classifyProviderError(ctxErr, ctx)
		h.mu.Lock()
		contextSnapshot, contextActivityID, contextCauses := h.completeContextRequestLocked(run, requestID, "cancelled", nil, time.Now())
		h.mu.Unlock()
		h.emitContextUpdated(run, contextSnapshot, "cancelled", contextActivityID, contextCauses)
		h.traceEvent(traceEvent{
			RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
			Status: "cancelled", Duration: time.Since(started),
		})
		if h.opts.Observe != nil {
			fields := events.Event{"requestId": requestID, "status": "cancelled", "reason": "cancelled", "duration": time.Since(started)}
			if request.ModelStepID != "" {
				fields["modelStepId"] = request.ModelStepID
			}
			addActivityFields(fields, activityID, causedBy, origin)
			h.observeRun("model.failed", request.RunID, run, fields)
		}
		writeRPCError(w, status, code, message)
		return
	}
	if !json.Valid(response.Message) || len(response.Message) == 0 || (len(response.Usage) != 0 && !json.Valid(response.Usage)) {
		h.mu.Lock()
		contextSnapshot, contextActivityID, contextCauses := h.completeContextRequestLocked(run, requestID, "failed", nil, time.Now())
		h.mu.Unlock()
		h.emitContextUpdated(run, contextSnapshot, "failed", contextActivityID, contextCauses)
		h.traceEvent(traceEvent{
			RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
			Status: "invalid_response", Duration: time.Since(started),
		})
		if h.opts.Observe != nil {
			fields := events.Event{"requestId": requestID, "status": "invalid_response", "reason": "provider_response", "duration": time.Since(started)}
			if request.ModelStepID != "" {
				fields["modelStepId"] = request.ModelStepID
			}
			addActivityFields(fields, activityID, causedBy, origin)
			h.observeRun("model.failed", request.RunID, run, fields)
		}
		writeRPCError(w, http.StatusBadGateway, "provider_response", "provider returned an invalid response")
		return
	}
	h.traceEvent(traceEvent{
		RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
		Status: "model_response", Duration: time.Since(started), Usage: summarizeUsage(response.Usage),
	})
	usage := summarizeUsage(response.Usage)
	var explicitRefusal struct {
		Refusal string `json:"refusal"`
	}
	_ = json.Unmarshal(response.Message, &explicitRefusal)
	h.mu.Lock()
	if strings.TrimSpace(explicitRefusal.Refusal) != "" {
		run.refusal = &imageapi.Error{Code: "refusal", Message: "model refused the request", ProviderMessage: explicitRefusal.Refusal, RequestID: requestID}
	}
	if run.group != nil {
		run.group.usage.add(usage)
	}
	if request.ModelStepID != "" && run.modelStep != nil && run.modelStep.id == request.ModelStepID {
		toolCalls, toolCallOrder, toolErr := modelToolCallStates(response.Message)
		if toolErr != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate tool activity IDs")
			return
		}
		run.modelStep.responseComplete = true
		run.modelStep.toolCalls = toolCalls
		run.modelStep.toolCallOrder = toolCallOrder
	}
	contextSnapshot, contextActivityID, contextCauses := h.completeContextRequestLocked(run, requestID, "completed", usage, time.Now())
	h.mu.Unlock()
	h.emitContextUpdated(run, contextSnapshot, "completed", contextActivityID, contextCauses)
	if h.opts.Observe != nil {
		fields := events.Event{"requestId": requestID, "status": "finished", "finishReason": response.FinishReason, "duration": time.Since(started), "usage": usage}
		if request.ModelStepID != "" {
			fields["modelStepId"] = request.ModelStepID
		}
		if explicitRefusal.Refusal != "" {
			fields["errorDetails"] = run.refusal
		}
		addActivityFields(fields, activityID, causedBy, origin)
		h.observeRun("model.finished", request.RunID, run, fields)
		if text, toolCallIDs, ok := intermediateMessage(response.Message); ok && strings.TrimSpace(text) != "" {
			fields := events.Event{"requestId": requestID, "status": "intermediate", "text": text, "toolCallIds": toolCallIDs}
			if request.ModelStepID != "" {
				fields["modelStepId"] = request.ModelStepID
			}
			addActivityFields(fields, activityID, causedBy, origin)
			h.observeRun("agent.message", request.RunID, run, fields)
		}
	}
	writeJSON(w, modelResponse{RequestID: requestID, Message: response.Message, FinishReason: response.FinishReason, Usage: response.Usage})
}

func modelToolCallStates(raw json.RawMessage) (map[string]*toolCallState, []string, error) {
	var message struct {
		ToolCalls []struct {
			ID       string `json:"id"`
			Function struct {
				Name json.RawMessage `json:"name"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if json.Unmarshal(raw, &message) != nil || len(message.ToolCalls) == 0 {
		return nil, nil, nil
	}
	toolCalls := make(map[string]*toolCallState, len(message.ToolCalls))
	var toolCallOrder []string
	for _, call := range message.ToolCalls {
		if call.ID == "" {
			continue
		}
		if _, exists := toolCalls[call.ID]; exists {
			continue
		}
		activityID, err := randomID()
		if err != nil {
			return nil, nil, err
		}
		toolName := ""
		if json.Unmarshal(call.Function.Name, &toolName) != nil || strings.TrimSpace(toolName) == "" {
			toolName = "<invalid>"
		}
		toolCalls[call.ID] = &toolCallState{name: toolName, activityID: activityID}
		toolCallOrder = append(toolCallOrder, call.ID)
	}
	if len(toolCalls) == 0 {
		return nil, nil, nil
	}
	return toolCalls, toolCallOrder, nil
}

func intermediateMessage(raw json.RawMessage) (string, []string, bool) {
	var message struct {
		Content   json.RawMessage   `json:"content"`
		ToolCalls []json.RawMessage `json:"tool_calls"`
	}
	if json.Unmarshal(raw, &message) != nil || len(message.ToolCalls) == 0 {
		return "", nil, false
	}
	ids := make([]string, 0, len(message.ToolCalls))
	for _, rawCall := range message.ToolCalls {
		var call struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(rawCall, &call) == nil && call.ID != "" {
			ids = append(ids, call.ID)
		}
	}
	return messageText(message.Content), ids, true
}

func messageText(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var builder strings.Builder
	for _, part := range parts {
		if part.Type != "text" || part.Text == "" {
			continue
		}
		if builder.Len() != 0 {
			builder.WriteByte('\n')
		}
		builder.WriteString(part.Text)
	}
	return builder.String()
}

func isJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

func classifyProviderError(err error, ctx context.Context) (int, string, string) {
	if ctx != nil && ctx.Err() != nil {
		return http.StatusRequestTimeout, "cancelled", "provider request cancelled"
	}
	var providerErr *provider.ProviderError
	if errors.As(err, &providerErr) {
		status := http.StatusBadGateway
		switch providerErr.Kind {
		case provider.ErrorAuthentication:
			status = http.StatusUnauthorized
		case provider.ErrorQuota:
			status = http.StatusPaymentRequired
		case provider.ErrorRateLimit:
			status = http.StatusTooManyRequests
		case provider.ErrorCancelled:
			return http.StatusRequestTimeout, "cancelled", "provider request cancelled"
		}
		return status, string(providerErr.Kind), providerErr.Error()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return http.StatusRequestTimeout, "cancelled", "provider request cancelled"
	}
	return http.StatusBadGateway, "provider", "provider request failed"
}
