package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

// Models returns a capability scoped to this operation. It never exposes the
// borrowed Host service directly, so calls retain operation lifetime,
// ownership, group admission, and usage accounting.
func (op *Operation) Models() model.Service {
	if op == nil || op.host == nil {
		return unavailableModels{}
	}
	return operationModels{operation: op}
}

type operationModels struct {
	operation *Operation
}

type unavailableModels struct{}

func (unavailableModels) ListModels(context.Context, model.ModelQuery) ([]model.Model, error) {
	return nil, ErrOperationClosed
}
func (unavailableModels) Generate(context.Context, model.Request) (model.Response, error) {
	return model.Response{}, ErrOperationClosed
}
func (unavailableModels) Decide(context.Context, model.DecisionRequest) (model.DecisionResponse, error) {
	return model.DecisionResponse{}, ErrOperationClosed
}
func (unavailableModels) GenerateImage(context.Context, model.ImageRequest) (model.GeneratedImage, error) {
	return model.GeneratedImage{}, ErrOperationClosed
}

func (s operationModels) checkedContext(ctx context.Context) (context.Context, context.CancelFunc, *agentRun, error) {
	op := s.operation
	if ctx == nil {
		return nil, nil, nil, errors.New("agent model operation requires a context")
	}
	if op == nil || op.host == nil {
		return nil, nil, nil, ErrOperationClosed
	}
	if !op.beginRequest() {
		return nil, nil, nil, ErrOperationClosed
	}
	h := op.host
	h.mu.Lock()
	if op.closed || h.closed || op.ctx.Err() != nil {
		h.mu.Unlock()
		op.endRequest()
		return nil, nil, nil, ErrOperationClosed
	}
	run := h.currentOwnerRunLocked(op)
	if (op.task != nil || op.parentRunID != "") && run == nil {
		h.mu.Unlock()
		op.endRequest()
		return nil, nil, nil, newOperationError(409, "scope", "agent model scope has no active owner run")
	}
	if run != nil && (h.runs[run.id] != run || run.ctx.Err() != nil || run.interrupting || run.finalClosed || !h.executionGroupOpenLocked(run.group)) {
		h.mu.Unlock()
		op.endRequest()
		return nil, nil, nil, newOperationError(409, "scope", "agent model scope has ended")
	}
	h.mu.Unlock()
	if h.opts.Models == nil {
		op.endRequest()
		return nil, nil, nil, errors.New("agent host has no model service")
	}
	merged, cancel := context.WithCancel(ctx)
	stopOperation := context.AfterFunc(op.ctx, cancel)
	if run != nil {
		stopRun := context.AfterFunc(run.ctx, cancel)
		return merged, func() { stopRun(); stopOperation(); cancel(); op.endRequest() }, run, nil
	}
	return merged, func() { stopOperation(); cancel(); op.endRequest() }, nil, nil
}

func (s operationModels) ListModels(ctx context.Context, query model.ModelQuery) ([]model.Model, error) {
	merged, cancel, _, err := s.checkedContext(ctx)
	if err != nil {
		return nil, err
	}
	defer cancel()
	return s.operation.host.opts.Models.ListModels(merged, query)
}

func (s operationModels) Generate(ctx context.Context, request model.Request) (model.Response, error) {
	merged, cancel, run, err := s.checkedContext(ctx)
	if err != nil {
		return model.Response{}, err
	}
	defer cancel()
	h := s.operation.host
	if request.Provider == "" && run != nil {
		request.Provider = run.config.Provider
	}
	if request.Model == "" && run != nil {
		request.Model = run.config.Model
	}
	if err = h.checkProvider(merged, request.Provider); err != nil {
		return model.Response{}, err
	}
	requestID, err := randomID()
	if err != nil {
		return model.Response{}, err
	}
	activityID, err := randomID()
	if err != nil {
		return model.Response{}, err
	}
	var group *executionGroup
	var causes []string
	origin := activityOrigin{Kind: "model"}
	if run != nil {
		if request.Provider != run.config.Provider || request.Model != run.config.Model {
			return model.Response{}, newOperationError(403, "model", "model operation does not match the owning agent")
		}
		request.RunID = run.id
		request.SessionID = run.sessionID()
		if run.activityID != "" {
			causes = []string{run.activityID}
		}
		origin.RunID, origin.AgentID = run.id, run.agentID()
	} else {
		// Calls without an explicit session are isolated from unrelated requests
		// made through the same long-lived Client.
		if request.SessionID == "" {
			request.SessionID = request.RunID
			if request.SessionID == "" {
				request.SessionID = requestID
			}
		}
		if request.RunID == "" {
			request.RunID = request.SessionID
		}
	}
	h.mu.Lock()
	if s.operation.closed || h.closed || s.operation.ctx.Err() != nil ||
		(run != nil && (h.runs[run.id] != run || run.ctx.Err() != nil || run.interrupting || run.finalClosed || !h.executionGroupOpenLocked(run.group))) {
		h.mu.Unlock()
		return model.Response{}, newOperationError(409, "scope", "agent model scope has ended")
	}
	group = h.executionForOperationLocked(s.operation)
	if run != nil {
		if run.config.MaxSteps > 0 && run.providerCalls >= run.config.MaxSteps {
			h.mu.Unlock()
			return model.Response{}, newOperationError(409, "max_steps", fmt.Sprintf("agent reached maxSteps=%d", run.config.MaxSteps))
		}
	}
	limitAction, admitted := h.reserveExecutionLocked(group, "providerCalls", "agent", activityID, origin)
	if admitted && run != nil {
		run.providerCalls++
	}
	h.mu.Unlock()
	if !admitted {
		h.finishExecutionLimit(limitAction)
		code, message := executionLimitError(group)
		return model.Response{}, newOperationError(409, code, message)
	}
	fields := eventsForDirectModel(requestID, activityID, causes, origin, group, h)
	if run != nil {
		h.observeRun("model.started", request.RunID, run, eventsWithStatus(fields, "started"))
	} else {
		fields["type"], fields["status"] = "model.started", "started"
		h.observe(fields)
	}
	started := time.Now()
	userObserve := request.Observe
	request.Observe = func(event provider.StreamEvent) {
		if userObserve != nil {
			userObserve(event)
		}
		if run != nil {
			h.observeRun(event.Type, run.id, run, events.Event{"requestId": requestID, "text": event.Text})
		}
	}
	response, err := h.opts.Models.Generate(merged, request)
	if merged.Err() != nil {
		err = merged.Err()
	}
	if err != nil {
		if run != nil {
			h.observeRun("model.failed", run.id, run, events.Event{"requestId": requestID, "reason": "provider"})
		} else {
			h.observe(events.Event{"type": "model.failed", "requestId": requestID, "reason": "provider"})
		}
		return model.Response{}, err
	}
	if !json.Valid(response.Message) || len(response.Message) == 0 || (len(response.Usage) != 0 && !json.Valid(response.Usage)) {
		return model.Response{}, errors.New("provider returned an invalid response")
	}
	usage := summarizeUsage(response.Usage)
	h.mu.Lock()
	if group != nil {
		group.usage.add(usage)
	}
	h.mu.Unlock()
	if run != nil {
		h.observeRun("model.finished", run.id, run, events.Event{
			"requestId": requestID, "duration": time.Since(started),
			"finishReason": response.FinishReason, "usage": usage,
		})
	} else {
		h.observe(events.Event{"type": "model.finished", "requestId": requestID, "duration": time.Since(started), "finishReason": response.FinishReason, "usage": usage})
	}
	return response, nil
}

func (s operationModels) Decide(ctx context.Context, request model.DecisionRequest) (model.DecisionResponse, error) {
	merged, cancel, _, err := s.checkedContext(ctx)
	if err != nil {
		return model.DecisionResponse{}, err
	}
	defer cancel()
	if err := request.Validate(); err != nil {
		return model.DecisionResponse{}, err
	}
	h := s.operation.host
	if err := h.checkProvider(merged, request.Provider); err != nil {
		return model.DecisionResponse{}, err
	}
	if h.decider == nil {
		return model.DecisionResponse{}, errors.New("decision provider is unavailable")
	}
	requestID, err := randomID()
	if err != nil {
		return model.DecisionResponse{}, err
	}
	activityID, err := randomID()
	if err != nil {
		return model.DecisionResponse{}, err
	}
	fields, group, action, admitted := h.beginDecisionActivity(s.operation, requestID, activityID, request)
	if !admitted {
		h.finishExecutionLimit(action)
		code, message := executionLimitError(group)
		return model.DecisionResponse{}, newOperationError(409, code, message)
	}
	h.decisionEvent("decision.started", fields, nil)
	started := time.Now()
	response, err := h.decider.Decide(merged, request)
	if merged.Err() != nil {
		err = merged.Err()
	}
	if err != nil {
		h.decisionEvent("decision.failed", fields, events.Event{"duration": time.Since(started), "reason": "provider"})
		return model.DecisionResponse{}, err
	}
	if err := decision.ValidateResponse(response.JSON); err != nil {
		return model.DecisionResponse{}, err
	}
	var metadata struct {
		Model string          `json:"model"`
		Usage json.RawMessage `json:"usage"`
	}
	_ = json.Unmarshal(response.JSON, &metadata)
	h.mu.Lock()
	if group != nil {
		group.usage.add(summarizeUsage(metadata.Usage))
	}
	h.mu.Unlock()
	completed := events.Event{"duration": time.Since(started)}
	if response.StatusCode != 0 {
		completed["statusCode"] = response.StatusCode
	}
	if metadata.Model != "" {
		completed["responseModel"] = metadata.Model
	}
	if len(metadata.Usage) != 0 {
		completed["usage"] = metadata.Usage
	}
	h.decorateExecution(completed, group)
	h.decisionEvent("decision.completed", fields, completed)
	return response, nil
}

func (s operationModels) GenerateImage(ctx context.Context, request model.ImageRequest) (model.GeneratedImage, error) {
	merged, cancel, run, err := s.checkedContext(ctx)
	if err != nil {
		return model.GeneratedImage{}, err
	}
	defer cancel()
	if err := (imageapi.Request{Provider: request.Provider, Model: request.Model, Prompt: request.Prompt, Parameters: request.Parameters}).ValidateGeneration(); err != nil {
		return model.GeneratedImage{}, err
	}
	h := s.operation.host
	if err := h.checkProvider(merged, request.Provider); err != nil {
		return model.GeneratedImage{}, err
	}
	if run != nil {
		request.RunID = run.id
	}
	if h.imager == nil {
		return model.GeneratedImage{}, errors.New("image generation provider is unavailable")
	}
	requestID, err := randomID()
	if err != nil {
		return model.GeneratedImage{}, err
	}
	activityID, err := randomID()
	if err != nil {
		return model.GeneratedImage{}, err
	}
	fields, group, _, admitted := h.prepareDecisionFields(s.operation, requestID, activityID, decision.Request{Provider: request.Provider, Model: request.Model}, false)
	limitOrigin := activityOrigin{Kind: "image"}
	limitOrigin.RunID, _ = fields["runId"].(string)
	limitOrigin.AgentID, _ = fields["agentId"].(string)
	limitOrigin.ToolCallID, _ = fields["toolCallId"].(string)
	h.mu.Lock()
	var action *executionLimitAction
	if admitted {
		action, admitted = h.reserveExecutionLocked(group, "providerCalls", "image", activityID, limitOrigin)
	}
	h.mu.Unlock()
	if !admitted {
		h.finishExecutionLimit(action)
		return model.GeneratedImage{}, newOperationError(409, "execution_limit", "image generation execution scope has ended")
	}
	h.decisionEvent("image.started", fields, nil)
	started := time.Now()
	generated, err := h.opts.Models.GenerateImage(merged, request)
	if merged.Err() != nil {
		err = merged.Err()
	}
	if err != nil {
		h.decisionEvent("image.failed", fields, events.Event{"duration": time.Since(started), "reason": "provider"})
		return model.GeneratedImage{}, err
	}
	h.mu.Lock()
	if group != nil {
		group.usage.add(summarizeUsage(generated.Usage))
	}
	h.mu.Unlock()
	completed := events.Event{
		"duration": time.Since(started), "providerRequestId": generated.RequestID,
		"responseModel": generated.ResponseModel, "usage": generated.Usage,
	}
	h.decorateExecution(completed, group)
	h.decisionEvent("image.completed", fields, completed)
	return generated, nil
}

func eventsForDirectModel(requestID, activityID string, causes []string, origin activityOrigin, group *executionGroup, h *Host) events.Event {
	fields := events.Event{"requestId": requestID}
	addActivityFields(fields, activityID, causes, origin)
	h.decorateExecution(fields, group)
	return fields
}

func eventsWithStatus(fields events.Event, status string) events.Event {
	copy := make(events.Event, len(fields)+1)
	for key, value := range fields {
		copy[key] = value
	}
	copy["status"] = status
	return copy
}
