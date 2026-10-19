package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
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
	activityID, err := randomID()
	if err != nil {
		writeRPCError(w, http.StatusInternalServerError, "request", "cannot identify decision activity")
		return
	}
	fields, group, limitAction, admitted := b.host.beginDecisionActivity(op, requestID, activityID, request)
	if !admitted {
		b.host.finishExecutionLimit(limitAction)
		code, message := executionLimitError(group)
		writeRPCError(w, http.StatusConflict, code, message)
		return
	}
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
		b.host.decorateExecution(end, group)
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
		b.host.mu.Lock()
		if group != nil {
			group.usage.add(summarizeUsage(meta.Usage))
		}
		b.host.mu.Unlock()
	}
	b.host.decorateExecution(end, group)
	b.host.decisionEvent("decision.completed", fields, end)
	writeJSON(w, response.JSON)
}

func (h *Host) decisionFields(op *Operation, requestID string, request decision.Request) events.Event {
	activityID, _ := randomID()
	fields, _, _, _ := h.prepareDecisionFields(op, requestID, activityID, request, false)
	return fields
}

func (h *Host) beginDecisionActivity(op *Operation, requestID, activityID string, request decision.Request) (events.Event, *executionGroup, *executionLimitAction, bool) {
	return h.prepareDecisionFields(op, requestID, activityID, request, true)
}

func (h *Host) prepareDecisionFields(op *Operation, requestID, activityID string, request decision.Request, count bool) (events.Event, *executionGroup, *executionLimitAction, bool) {
	fields := events.Event{"requestId": requestID, "provider": request.Provider, "decisionModel": request.Model}
	group := (*executionGroup)(nil)
	h.mu.Lock()
	run := h.currentOwnerRunLocked(op)
	group = h.executionForOperationLocked(op)
	var causedBy []string
	origin := activityOrigin{Kind: "script"}
	if run != nil {
		for key, value := range decisionOwnerFields(run.id, run, "") {
			fields[key] = value
		}
		if run.activityID != "" {
			causedBy = []string{run.activityID}
		}
		origin.RunID, origin.AgentID = run.id, run.agentID()
	}
	if op != nil {
		for key, value := range op.decisionOwner {
			fields[key] = value
		}
		if len(op.causeActivityIDs) != 0 {
			causedBy = append([]string(nil), op.causeActivityIDs...)
		}
		if op.activityOrigin.Kind != "" {
			origin = op.activityOrigin
		}
	}
	if len(causedBy) == 0 {
		if ownerActivity, _ := fields["ownerActivityId"].(string); ownerActivity != "" {
			causedBy = []string{ownerActivity}
		}
	}
	if origin.RunID == "" {
		origin.RunID, _ = fields["runId"].(string)
	}
	if origin.AgentID == "" {
		origin.AgentID, _ = fields["agentId"].(string)
	}
	toolCallID, _ := fields["toolCallId"].(string)
	origin.ToolCallID = toolCallID
	addActivityFields(fields, activityID, causedBy, origin)
	var limitAction *executionLimitAction
	admitted := true
	if group != nil {
		if count {
			limitAction, admitted = h.reserveExecutionLocked(group, "providerCalls", "decision", activityID, origin)
		} else {
			admitted = h.executionGroupOpenLocked(group)
		}
		if admitted {
			fields["groupId"] = group.id
			fields["execution"] = group.snapshotLocked()
		}
	}
	h.mu.Unlock()
	return fields, group, limitAction, admitted
}

// Capture ownership before the child can make requests. Parent shutdown removes
// its run before joining child work, so event attribution must outlive that map.
func decisionOwnerFields(runID string, run *agentRun, toolCallID string) events.Event {
	fields := events.Event{"runId": runID, "parentRunId": run.parentRunID, "name": run.config.Name, "model": run.config.Model}
	if run.activityID != "" {
		fields["ownerActivityId"] = run.activityID
	}
	if run.group != nil {
		fields["groupId"] = run.group.id
	}
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
