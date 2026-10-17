package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

type modelRequest struct {
	RunID     string            `json:"runId"`
	Model     string            `json:"model"`
	Messages  []json.RawMessage `json:"messages"`
	Tools     []json.RawMessage `json:"tools,omitempty"`
	CacheKey  string            `json:"cacheKey,omitempty"`
	MaxTokens int               `json:"maxTokens,omitempty"`
	Effort    string            `json:"effort,omitempty"`
}

type modelResponse struct {
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
	h.mu.Lock()
	run := h.runs[request.RunID]
	valid := run != nil && run.operation == op && run.ctx.Err() == nil && !run.interrupting && !op.closed && !h.closed && !run.finalClosed
	if !valid {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "run", "unknown agent run")
		return
	}
	if request.Model != run.config.Model || request.MaxTokens != run.config.MaxTokens || effort != run.config.Effort {
		h.mu.Unlock()
		writeRPCError(w, http.StatusBadRequest, "request", "model request does not match agent run configuration")
		return
	}
	steers := h.deliverPendingSteersLocked(run)
	if len(steers) == 0 {
		if run.config.MaxSteps > 0 && run.providerCalls >= run.config.MaxSteps {
			limit := run.config.MaxSteps
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "max_steps", fmt.Sprintf("agent reached maxSteps=%d without a final answer", limit))
			return
		}
		// Count only requests that cross the provider boundary. Returning a
		// queued steer batch does not consume the task's model-call budget.
		run.providerCalls++
	}
	h.mu.Unlock()
	if len(steers) != 0 {
		writeJSON(w, modelResponse{Steers: steers})
		return
	}
	requestID := ""
	if h.opts.Observe != nil {
		var err error
		requestID, err = randomID()
		if err != nil {
			writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate model request ID")
			return
		}
		h.observeRun("model.started", request.RunID, run, events.Event{
			"requestId": requestID, "status": "started",
		})
	}
	ctx, cancel := context.WithCancel(run.ctx)
	stopRequest := context.AfterFunc(r.Context(), cancel)
	defer func() { stopRequest(); cancel() }()
	started := time.Now()
	var observeStream func(provider.StreamEvent)
	if requestID != "" {
		observeStream = func(streamEvent provider.StreamEvent) {
			switch streamEvent.Type {
			case "thinking", "thinking_summary", "message_delta":
				h.observeRun(streamEvent.Type, request.RunID, run, events.Event{
					"requestId": requestID, "text": streamEvent.Text,
				})
			}
		}
	}
	response, err := h.provider.Generate(ctx, provider.Request{
		Provider: run.config.Provider, RunID: request.RunID,
		Model: run.config.Model, Messages: request.Messages, Tools: request.Tools,
		CacheKey: request.CacheKey, MaxTokens: run.config.MaxTokens, Effort: run.config.Effort,
		Observe: observeStream,
	})
	if err != nil {
		status, code, message := classifyProviderError(err, ctx)
		h.traceEvent(traceEvent{
			RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
			Status: "provider_error", Duration: time.Since(started),
		})
		if requestID != "" {
			modelStatus := "failed"
			if code == "cancelled" {
				modelStatus = "cancelled"
			}
			h.observeRun("model.failed", request.RunID, run, events.Event{
				"requestId": requestID, "status": modelStatus, "reason": code,
				"duration": time.Since(started),
			})
		}
		writeRPCError(w, status, code, message)
		return
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		status, code, message := classifyProviderError(ctxErr, ctx)
		h.traceEvent(traceEvent{
			RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
			Status: "cancelled", Duration: time.Since(started),
		})
		if requestID != "" {
			h.observeRun("model.failed", request.RunID, run, events.Event{
				"requestId": requestID, "status": "cancelled", "reason": "cancelled",
				"duration": time.Since(started),
			})
		}
		writeRPCError(w, status, code, message)
		return
	}
	if !json.Valid(response.Message) || len(response.Message) == 0 || (len(response.Usage) != 0 && !json.Valid(response.Usage)) {
		h.traceEvent(traceEvent{
			RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
			Status: "invalid_response", Duration: time.Since(started),
		})
		if requestID != "" {
			h.observeRun("model.failed", request.RunID, run, events.Event{
				"requestId": requestID, "status": "invalid_response", "reason": "provider_response",
				"duration": time.Since(started),
			})
		}
		writeRPCError(w, http.StatusBadGateway, "provider_response", "provider returned an invalid response")
		return
	}
	h.traceEvent(traceEvent{
		RunID: request.RunID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
		Status: "model_response", Duration: time.Since(started), Usage: summarizeUsage(response.Usage),
	})
	if requestID != "" {
		h.observeRun("model.finished", request.RunID, run, events.Event{
			"requestId": requestID, "status": "finished", "finishReason": response.FinishReason,
			"duration": time.Since(started), "usage": summarizeUsage(response.Usage),
		})
		if text, toolCallIDs, ok := intermediateMessage(response.Message); ok && strings.TrimSpace(text) != "" {
			h.observeRun("agent.message", request.RunID, run, events.Event{
				"requestId": requestID, "status": "intermediate", "text": text, "toolCallIds": toolCallIDs,
			})
		}
	}
	writeJSON(w, modelResponse{Message: response.Message, FinishReason: response.FinishReason, Usage: response.Usage})
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
