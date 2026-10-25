package agent

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

type hookScopeRequest struct {
	ScopeID     string `json:"scopeId"`
	RunID       string `json:"runId,omitempty"`
	Phase       string `json:"phase,omitempty"`
	HookIndex   *int   `json:"hookIndex,omitempty"`
	Module      string `json:"module,omitempty"`
	ModelStepID string `json:"modelStepId,omitempty"`
	RequestID   string `json:"requestId,omitempty"`
	ToolCallID  string `json:"toolCallId,omitempty"`
	ToolName    string `json:"toolName,omitempty"`
	Status      string `json:"status,omitempty"`
	Error       string `json:"error,omitempty"`
	Message     string `json:"message,omitempty"`
}

type hookScopeResponse struct {
	ScopeID  string       `json:"scopeId"`
	Defaults Config       `json:"defaults"`
	Event    events.Event `json:"event,omitempty"`
}

func (h *Host) beginHookScope(parent *Operation, request hookScopeRequest) (hookScopeResponse, error) {
	registered := hasHookMetadata(request)
	canonicalModule := ""
	if registered {
		if request.RunID == "" || request.Phase == "" || request.HookIndex == nil || request.Module == "" || request.ModelStepID == "" {
			return hookScopeResponse{}, newOperationError(http.StatusBadRequest, "request", "incomplete registered hook metadata")
		}
		if !validHookPhase(request.Phase) {
			return hookScopeResponse{}, newOperationError(http.StatusBadRequest, "request", "unsupported hook phase")
		}
		h.mu.Lock()
		owner := h.currentOwnerRunLocked(parent)
		if owner == nil && request.RunID != "" {
			candidate := h.runs[request.RunID]
			if candidate != nil && candidate.operation == parent {
				owner = candidate
			}
		}
		resolved, ok := resolvedHook{}, false
		if owner != nil && request.HookIndex != nil {
			resolved, ok = owner.resolvedHooks[*request.HookIndex]
		}
		h.mu.Unlock()
		if !ok || resolved.Canonical != request.Module {
			return hookScopeResponse{}, newOperationError(http.StatusBadRequest, "request", "hook module was not resolved by this task loader")
		}
		canonicalModule = resolved.Canonical
	}

	h.mu.Lock()
	owner := h.currentOwnerRunLocked(parent)
	if owner == nil && registered {
		candidate := h.runs[request.RunID]
		if candidate != nil && candidate.operation == parent {
			owner = candidate
		}
	}
	if owner == nil || h.runs[owner.id] != owner || owner.ctx.Err() != nil || parent.closed || h.closed {
		h.mu.Unlock()
		return hookScopeResponse{}, newOperationError(http.StatusConflict, "scope", "hook owner run has ended")
	}
	if !h.executionGroupOpenLocked(owner.group) {
		group := owner.group
		h.mu.Unlock()
		code, message := executionLimitError(group)
		return hookScopeResponse{}, newOperationError(http.StatusConflict, code, message)
	}
	if registered {
		if _, _, _, _, err := validateHookInvocationLocked(request, canonicalModule, owner); err != nil {
			h.mu.Unlock()
			return hookScopeResponse{}, newOperationError(http.StatusConflict, "hook", err.Error())
		}
	}
	ownerCtx := owner.ctx
	defaults := hookDefaults(owner)
	h.mu.Unlock()

	child := h.NewOperation(ownerCtx)
	if child.initErr != nil {
		child.Close()
		return hookScopeResponse{}, newOperationError(http.StatusInternalServerError, "broker", "could not allocate hook scope")
	}
	var activityID string
	var err error
	if registered {
		activityID, err = randomID()
		if err != nil {
			child.Close()
			return hookScopeResponse{}, newOperationError(http.StatusInternalServerError, "broker", "could not allocate hook invocation")
		}
	}

	h.mu.Lock()
	if child.closed || h.runs[owner.id] != owner || owner.ctx.Err() != nil || parent.closed || h.closed {
		h.mu.Unlock()
		child.Close()
		return hookScopeResponse{}, newOperationError(http.StatusConflict, "scope", "hook owner run has ended")
	}
	var hookEvent events.Event
	var causeActivityIDs []string
	var origin activityOrigin
	if registered {
		_, step, toolCall, causes, validateErr := validateHookInvocationLocked(request, canonicalModule, owner)
		if validateErr != nil {
			h.mu.Unlock()
			child.Close()
			return hookScopeResponse{}, newOperationError(http.StatusConflict, "hook", validateErr.Error())
		}
		causeActivityIDs = causes
		origin = activityOrigin{
			Kind: "hook", RunID: owner.id, AgentID: owner.agentID(),
			ToolCallID: request.ToolCallID, ToolName: request.ToolName,
			HookID: hookID(canonicalModule, *request.HookIndex, request.Phase),
			Module: canonicalModule, Phase: request.Phase, ModelStepID: step.id,
		}
		if toolCall != nil {
			origin.ToolName = request.ToolName
		}
		limitAction, admitted := h.reserveExecutionLocked(owner.group, "hookCalls", "", activityID, origin)
		if !admitted {
			group := owner.group
			h.mu.Unlock()
			child.Close()
			h.finishExecutionLimit(limitAction)
			code, message := executionLimitError(group)
			return hookScopeResponse{}, newOperationError(http.StatusConflict, code, message)
		}
		hookEvent = hookEventLocked(owner, request, canonicalModule, activityID, causeActivityIDs, origin)
		if request.Phase == "beforeModelRequest" {
			step.beforeHookActivityIDs = appendUniqueActivityIDs(step.beforeHookActivityIDs, activityID)
		}
		if owner.group != nil {
			hookEvent["execution"] = owner.group.snapshotLocked()
		}
		child.hookInvocation = &hookInvocation{
			activityID: activityID, event: hookEvent, state: "active", started: time.Now(),
			startEventDone: make(chan struct{}),
		}
		// The callback event records the invocation's parent causes. Work
		// created by the callback is caused by the invocation itself.
		causeActivityIDs = []string{activityID}
	} else {
		origin = parent.activityOrigin
		causeActivityIDs = append([]string(nil), parent.causeActivityIDs...)
		if origin.Kind == "" {
			origin = activityOrigin{Kind: "hook", RunID: owner.id, AgentID: owner.agentID()}
		}
		if len(causeActivityIDs) == 0 && owner.activityID != "" {
			causeActivityIDs = []string{owner.activityID}
		}
	}
	child.parentRunID = owner.id
	child.ownerRun = owner
	child.hookScope = true
	child.group = owner.group
	child.decisionOwner = decisionOwnerFields(owner.id, owner, request.ToolCallID)
	child.activityOrigin = origin
	child.causeActivityIDs = causeActivityIDs
	owner.childScopes[child] = struct{}{}
	h.mu.Unlock()

	if registered {
		startEvent := hookObservationEvent(hookEvent, "hook.started", "started")
		func() {
			defer close(child.hookInvocation.startEventDone)
			h.observe(startEvent)
		}()
	}
	return hookScopeResponse{ScopeID: child.scope, Defaults: defaults, Event: hookEvent}, nil
}

func hasHookMetadata(request hookScopeRequest) bool {
	return request.RunID != "" || request.Phase != "" || request.HookIndex != nil || request.Module != "" ||
		request.ModelStepID != "" || request.RequestID != "" || request.ToolCallID != "" || request.ToolName != ""
}

func validHookPhase(phase string) bool {
	switch phase {
	case "beforeModelRequest", "afterModelResponse", "beforeTool", "afterTool":
		return true
	default:
		return false
	}
}

func hookDefaults(owner *agentRun) Config {
	return Config{
		Provider: owner.config.Provider, Model: owner.config.Model,
		MaxTokens: owner.config.MaxTokens, Effort: owner.config.Effort,
		Hooks:                cloneHookModules(owner.config.Hooks),
		ContextBudget:        cloneContextBudget(owner.config.ContextBudget),
		contextBudgetPresent: owner.config.ContextBudgetPresent || owner.config.ContextBudget != nil,
		ContextPolicy:        cloneContextPolicy(owner.config.ContextPolicy),
		contextPolicyPresent: owner.config.ContextPolicyPresent || owner.config.ContextPolicy != nil,
	}
}

func hookID(module string, index int, phase string) string {
	return fmt.Sprintf("%s#%d:%s", module, index, phase)
}

func validateHookInvocationLocked(request hookScopeRequest, canonicalModule string, owner *agentRun) (string, *modelStepState, *toolCallState, []string, error) {
	if request.RunID != owner.id {
		return "", nil, nil, nil, fmt.Errorf("hook run does not match the current run")
	}
	if *request.HookIndex < 0 || *request.HookIndex >= len(owner.config.Hooks) {
		return "", nil, nil, nil, fmt.Errorf("hook module is not registered for this run")
	}
	resolved, ok := owner.resolvedHooks[*request.HookIndex]
	if !ok || resolved.Specifier != owner.config.Hooks[*request.HookIndex] || resolved.Canonical != canonicalModule {
		return "", nil, nil, nil, fmt.Errorf("hook module is not registered for this run")
	}
	step := owner.modelStep
	if step == nil || step.id != request.ModelStepID || step.finished {
		return "", nil, nil, nil, fmt.Errorf("hook does not match the active model step")
	}
	var causes []string
	var toolCall *toolCallState
	switch request.Phase {
	case "beforeModelRequest":
		if step.requestID != "" || step.activityID == "" {
			return "", nil, nil, nil, fmt.Errorf("beforeModelRequest is outside model preparation")
		}
		causes = []string{step.activityID}
	case "afterModelResponse":
		if !step.responseComplete || request.RequestID == "" || request.RequestID != step.requestID || step.requestActivityID == "" {
			return "", nil, nil, nil, fmt.Errorf("afterModelResponse does not match the completed provider request")
		}
		causes = []string{step.requestActivityID}
	case "beforeTool", "afterTool":
		if !step.responseComplete || request.RequestID == "" || request.RequestID != step.requestID || request.ToolCallID == "" || request.ToolName == "" {
			return "", nil, nil, nil, fmt.Errorf("tool hook does not match a completed provider request")
		}
		toolCall = step.toolCalls[request.ToolCallID]
		if toolCall == nil || toolCall.name != request.ToolName || toolCall.resultRecorded {
			return "", nil, nil, nil, fmt.Errorf("tool hook does not match an active tool call")
		}
		if request.Phase == "beforeTool" {
			if toolCall.started {
				return "", nil, nil, nil, fmt.Errorf("beforeTool ran after tool execution started")
			}
			causes = []string{step.requestActivityID}
		} else {
			if !toolCall.started && toolCall.activityID == "" {
				return "", nil, nil, nil, fmt.Errorf("afterTool has no completed tool activity")
			}
			causes = []string{toolCall.activityID}
		}
	}
	return canonicalModule, step, toolCall, causes, nil
}

func hookEventLocked(owner *agentRun, request hookScopeRequest, module, activityID string, causedBy []string, origin activityOrigin) events.Event {
	depth := 0
	for instance := owner.instance; instance != nil && instance.parent != nil; {
		parent := instance.parent
		if parent.group != owner.group || parent.instance == nil {
			break
		}
		depth++
		instance = parent.instance
	}
	role := "root"
	if depth > 0 {
		role = "child"
	}
	event := events.Event{
		"name":         owner.config.Name,
		"model":        owner.config.Model,
		"phase":        request.Phase,
		"hookId":       origin.HookID,
		"hookIndex":    *request.HookIndex,
		"module":       module,
		"invocationId": activityID,
		"activityId":   activityID,
		"groupId":      groupID(owner.group),
		"runId":        owner.id,
		"parentRunId":  owner.parentRunID,
		"modelStepId":  request.ModelStepID,
		"role":         role,
		"agentDepth":   depth,
		"causedBy":     append([]string(nil), causedBy...),
		"origin":       origin,
	}
	if request.RequestID != "" {
		event["requestId"] = request.RequestID
	}
	if request.ToolCallID != "" {
		event["toolCallId"] = request.ToolCallID
	}
	if request.ToolName != "" {
		event["toolName"] = request.ToolName
	}
	if agentID := owner.agentID(); agentID != "" {
		event["agentId"] = agentID
	}
	if owner.origin.CommandID != "" {
		event["commandId"] = owner.origin.CommandID
	}
	if owner.group != nil {
		event["execution"] = owner.group.snapshotLocked()
	}
	if owner.context.modelStepID != "" {
		event["context"] = owner.context.snapshot(owner.id, time.Now())
	}
	return event
}

func groupID(group *executionGroup) string {
	if group == nil {
		return ""
	}
	return group.id
}

func (h *Host) endHookScope(parent *Operation, request hookScopeRequest) (map[string]bool, error) {
	if request.ScopeID == "" || (request.Status != "completed" && request.Status != "failed" && request.Status != "cancelled") {
		return nil, newOperationError(http.StatusBadRequest, "request", "scopeId and a completed, failed, or cancelled status are required")
	}
	h.mu.Lock()
	owner := h.currentOwnerRunLocked(parent)
	var child *Operation
	var terminalEvent events.Event
	var startEventDone <-chan struct{}
	for candidate := range h.operations {
		if candidate.scope != request.ScopeID || !candidate.hookScope {
			continue
		}
		if owner != nil && candidate.ownerRun == owner {
			child = candidate
			break
		}
		if owner == nil && candidate.ownerRun != nil && candidate.ownerRun.operation == parent {
			child = candidate
			owner = candidate.ownerRun
			break
		}
	}
	if child == nil || (child.hookInvocation != nil && child.hookInvocation.state != "active") {
		h.mu.Unlock()
		return nil, newOperationError(http.StatusConflict, "scope", "unknown or unowned hook scope")
	}
	if child.hookInvocation != nil {
		// Claim finalization before Close; Close must not emit a second terminal.
		child.hookInvocation.state = "ending"
		startEventDone = child.hookInvocation.startEventDone
	}
	h.mu.Unlock()

	// Cancel unawaited scoped work and join it before deciding recoverability.
	// A request can fail internally while the callback itself is returning.
	child.Close()
	h.mu.Lock()
	closeErr := child.hookFatalError
	status, errorText := request.Status, request.Error
	if h.closed || parent.closed || h.runs[owner.id] != owner || owner.ctx.Err() != nil || owner.interrupting {
		closeErr = newOperationError(http.StatusConflict, "scope", "hook owner run has ended")
		status = "cancelled"
	} else if !h.executionGroupOpenLocked(owner.group) {
		code, message := executionLimitError(owner.group)
		closeErr = newOperationError(http.StatusConflict, code, message)
		status = "cancelled"
	} else if closeErr != nil {
		status = "failed"
		errorText = closeErr.Error()
	}
	if invocation := child.hookInvocation; invocation != nil {
		invocation.state = status
		terminalEvent = terminalHookEventLocked(invocation, status, errorText, child.group)
		if closeErr == nil && status == "failed" {
			message := request.Message
			if message == "" {
				message = request.Error
			}
			// Only the active -> ending winner records this invocation.
			owner.hookFailures = append(owner.hookFailures, hookFailureFromEvent(invocation.event, message))
		}
	}
	h.mu.Unlock()
	if terminalEvent != nil {
		if startEventDone != nil {
			<-startEventDone
		}
		h.observe(terminalEvent)
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return map[string]bool{"closed": true}, nil
}

// recordHookRequestError preserves infrastructure failures even if JS catches
// the RPC rejection and the owner run remains alive. Ordinary API misuse and
// service/IO errors are callback failures, not Host failures.
func (h *Host) recordHookRequestError(op *Operation, err error) {
	if op == nil || !op.hookScope || err == nil {
		return
	}
	var operationErr *operationError
	var encodingErr *directResponseEncodingError
	fatal := errors.As(err, &encodingErr) ||
		(errors.As(err, &operationErr) && operationErr.status >= 500 && operationErr.code == "broker")
	if !fatal {
		return
	}
	h.mu.Lock()
	if op.hookFatalError == nil {
		op.hookFatalError = err
	}
	h.mu.Unlock()
}

func hookFailureFromEvent(event events.Event, message string) HookFailure {
	text := func(key string) string { value, _ := event[key].(string); return value }
	index, _ := event["hookIndex"].(int)
	return HookFailure{
		InvocationID: text("invocationId"), HookIndex: index,
		Module: text("module"), Phase: text("phase"), ModelStepID: text("modelStepId"),
		RequestID: text("requestId"), ToolCallID: text("toolCallId"), ToolName: text("toolName"), Error: message,
	}
}

func cloneHookFailures(failures []HookFailure) []HookFailure {
	if failures == nil {
		return nil
	}
	result := append([]HookFailure{}, failures...)
	for index := range result {
		failure := &result[index]
		failure.InvocationID = normalizeJSONText(failure.InvocationID)
		failure.Module = normalizeJSONText(failure.Module)
		failure.Phase = normalizeJSONText(failure.Phase)
		failure.ModelStepID = normalizeJSONText(failure.ModelStepID)
		failure.RequestID = normalizeJSONText(failure.RequestID)
		failure.ToolCallID = normalizeJSONText(failure.ToolCallID)
		failure.ToolName = normalizeJSONText(failure.ToolName)
		failure.Error = normalizeJSONText(failure.Error)
		failure.NotifiedRequestID = normalizeJSONText(failure.NotifiedRequestID)
	}
	return result
}

func terminalHookEventLocked(invocation *hookInvocation, status, errorText string, group *executionGroup) events.Event {
	if invocation == nil {
		return nil
	}
	event := hookObservationEvent(invocation.event, "", status)
	if status == "completed" {
		event["type"] = "hook.finished"
	} else if status == "failed" {
		event["type"] = "hook.failed"
	} else {
		event["type"] = "hook.cancelled"
	}
	event["status"] = status
	event["duration"] = time.Since(invocation.started)
	if errorText != "" {
		event["error"] = errorText
	}
	if group != nil {
		event["execution"] = group.snapshotLocked()
	}
	return event
}

func hookObservationEvent(base events.Event, eventType, status string) events.Event {
	event := make(events.Event, len(base)+4)
	for key, value := range base {
		event[key] = value
	}
	if causes, ok := event["causedBy"].([]string); ok {
		event["causedBy"] = append([]string(nil), causes...)
	}
	if eventType != "" {
		event["type"] = eventType
	}
	if status != "" {
		event["status"] = status
	}
	return event
}

type executionView struct {
	executionSnapshot
	ActivityID string          `json:"activityId,omitempty"`
	CausedBy   []string        `json:"causedBy,omitempty"`
	Origin     *activityOrigin `json:"origin,omitempty"`
}

func (h *Host) executionGet(op *Operation) (executionView, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	group := h.executionForOperationLocked(op)
	if group == nil {
		return executionView{}, newOperationError(http.StatusConflict, "scope", "execution group is unavailable")
	}
	view := executionView{executionSnapshot: group.snapshotLocked()}
	if op != nil {
		if invocation := op.hookInvocation; invocation != nil {
			view.ActivityID = invocation.activityID
			if causes, ok := invocation.event["causedBy"].([]string); ok {
				view.CausedBy = append([]string(nil), causes...)
			}
		} else {
			view.CausedBy = append([]string(nil), op.causeActivityIDs...)
		}
		if op.activityOrigin.Kind != "" {
			origin := op.activityOrigin
			view.Origin = &origin
		}
	}
	return view, nil
}
