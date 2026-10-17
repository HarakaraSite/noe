package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/decision"
	"noe/internal/events"
)

func (b *broker) handleDecide(w http.ResponseWriter, r *http.Request, op *Operation) {
	var raw json.RawMessage
	if err := decodeRequest(r, &raw); err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid decision request")
		return
	}
	request, err := decision.ParseRequest(raw)
	if err != nil {
		writeRPCError(w, http.StatusBadRequest, "request", err.Error())
		return
	}
	if b.host.decider == nil {
		writeRPCError(w, http.StatusBadGateway, "provider", "decision provider is unavailable")
		return
	}
	ctx, cancel := context.WithCancel(op.ctx)
	stopRequest := context.AfterFunc(r.Context(), cancel)
	defer func() { stopRequest(); cancel() }()
	if ctx.Err() != nil {
		status, code, message := classifyProviderError(ctx.Err(), ctx)
		writeRPCError(w, status, code, message)
		return
	}
	requestID, err := randomID()
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, "request", "cannot identify decision request")
		return
	}
	fields := b.host.decisionFields(op, requestID, request)
	started := time.Now()
	b.host.decisionEvent("decision.started", fields, nil)
	response, err := b.host.decider.Decide(ctx, request)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil && decision.ValidateResponse(response.JSON) != nil {
		err = &provider.ProviderError{Kind: provider.ErrorResponse, StatusCode: response.StatusCode}
	}
	if err != nil {
		status, code, message := classifyProviderError(err, ctx)
		kind := "decision.failed"
		if code == "cancelled" {
			kind = "decision.cancelled"
		}
		end := events.Event{"duration": time.Since(started), "reason": code, "error": message}
		var providerErr *provider.ProviderError
		if errors.As(err, &providerErr) && providerErr.StatusCode != 0 {
			end["statusCode"] = providerErr.StatusCode
		}
		b.host.decisionEvent(kind, fields, end)
		writeRPCError(w, status, code, message)
		return
	}
	var meta struct {
		Model string          `json:"model"`
		Usage json.RawMessage `json:"usage"`
	}
	_ = json.Unmarshal(response.JSON, &meta)
	end := events.Event{"duration": time.Since(started)}
	if response.StatusCode != 0 {
		end["statusCode"] = response.StatusCode
	}
	if meta.Model != "" {
		end["responseModel"] = meta.Model
	}
	if len(meta.Usage) != 0 {
		end["usage"] = meta.Usage
	}
	b.host.decisionEvent("decision.completed", fields, end)
	writeJSON(w, response.JSON)
}

func (h *Host) decisionFields(op *Operation, requestID string, request decision.Request) events.Event {
	fields := events.Event{"requestId": requestID, "provider": request.Provider, "decisionModel": request.Model}
	h.mu.Lock()
	defer h.mu.Unlock()
	for key, value := range op.decisionOwner {
		fields[key] = value
	}
	return fields
}

// Capture ownership before the child can make requests. Parent shutdown removes
// its run before joining child work, so event attribution must outlive that map.
func decisionOwnerFields(runID string, run *agentRun, toolCallID string) events.Event {
	fields := events.Event{"runId": runID, "parentRunId": run.parentRunID, "name": run.config.Name, "model": run.config.Model}
	if id := run.agentID(); id != "" {
		fields["agentId"] = id
	}
	if toolCallID != "" {
		fields["toolCallId"] = toolCallID
	}
	return fields
}

func (h *Host) decisionEvent(kind string, fields, extra events.Event) {
	event := fieldsWith(fields, extra)
	event["type"] = kind
	h.observe(event)
}
