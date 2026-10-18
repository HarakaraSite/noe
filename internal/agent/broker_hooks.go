package agent

import (
	"fmt"
	"net/http"
	"time"

	"noe/internal/events"
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
}

type hookScopeResponse struct {
	ScopeID  string       `json:"scopeId"`
	Defaults Config       `json:"defaults"`
	Event    events.Event `json:"event,omitempty"`
}

func (b *broker) handleBeginHookScope(w http.ResponseWriter, r *http.Request, parent *Operation) {
	var request hookScopeRequest
	if decodeRequest(r, &request) != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid hook scope request")
		return
	}
	registered := hasHookMetadata(request)
	canonicalModule := ""
	if registered {
		if request.RunID == "" || request.Phase == "" || request.HookIndex == nil || request.Module == "" || request.ModelStepID == "" {
			writeRPCError(w, http.StatusBadRequest, "request", "incomplete registered hook metadata")
			return
		}
		if !validHookPhase(request.Phase) {
			writeRPCError(w, http.StatusBadRequest, "request", "unsupported hook phase")
			return
		}
		resolved, err := normalizeHookModules([]string{request.Module}, b.host.opts.Cwd, b.host.opts.ImportMap)
		if err != nil || len(resolved) != 1 {
			writeRPCError(w, http.StatusBadRequest, "request", "invalid hook module")
			return
		}
		canonicalModule = resolved[0]
	}

	h := b.host
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
		writeRPCError(w, http.StatusConflict, "scope", "hook owner run has ended")
		return
	}
	if !h.executionGroupOpenLocked(owner.group) {
		group := owner.group
		h.mu.Unlock()
		code, message := executionLimitError(group)
		writeRPCError(w, http.StatusConflict, code, message)
		return
	}
	if registered {
		if _, _, _, _, err := validateHookInvocationLocked(request, canonicalModule, owner); err != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "hook", err.Error())
			return
		}
	}
	ownerCtx := owner.ctx
	defaults := hookDefaults(owner)
	h.mu.Unlock()

	child := h.NewOperation(ownerCtx)
	var activityID string
	var err error
	if registered {
		activityID, err = randomID()
		if err != nil {
			child.Close()
			writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate hook invocation")
			return
		}
	}

	h.mu.Lock()
	if child.closed || h.runs[owner.id] != owner || owner.ctx.Err() != nil || parent.closed || h.closed {
		h.mu.Unlock()
		child.Close()
		writeRPCError(w, http.StatusConflict, "scope", "hook owner run has ended")
		return
	}
	var hookEvent events.Event
	var causeActivityIDs []string
	var origin activityOrigin
	if registered {
		_, step, toolCall, causes, validateErr := validateHookInvocationLocked(request, canonicalModule, owner)
		if validateErr != nil {
			h.mu.Unlock()
			child.Close()
			writeRPCError(w, http.StatusConflict, "hook", validateErr.Error())
			return
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
			writeRPCError(w, http.StatusConflict, code, message)
			return
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
	writeJSON(w, hookScopeResponse{ScopeID: child.scope, Defaults: defaults, Event: hookEvent})
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
	}
}

func hookID(module string, index int, phase string) string {
	return fmt.Sprintf("%s#%d:%s", module, index, phase)
}

func validateHookInvocationLocked(request hookScopeRequest, canonicalModule string, owner *agentRun) (string, *modelStepState, *toolCallState, []string, error) {
	if request.RunID != owner.id {
		return "", nil, nil, nil, fmt.Errorf("hook run does not match the current run")
	}
	if *request.HookIndex < 0 || *request.HookIndex >= len(owner.config.Hooks) || owner.config.Hooks[*request.HookIndex] != canonicalModule {
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

func (b *broker) handleEndHookScope(w http.ResponseWriter, r *http.Request, parent *Operation) {
	var request hookScopeRequest
	if decodeRequest(r, &request) != nil || request.ScopeID == "" || (request.Status != "completed" && request.Status != "failed" && request.Status != "cancelled") {
		writeRPCError(w, http.StatusBadRequest, "request", "scopeId and a completed, failed, or cancelled status are required")
		return
	}
	h := b.host
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
	if child != nil && child.hookInvocation != nil && child.hookInvocation.state == "active" {
		child.hookInvocation.state = request.Status
		terminalEvent = terminalHookEventLocked(child.hookInvocation, request.Status, request.Error, child.group)
		startEventDone = child.hookInvocation.startEventDone
	}
	h.mu.Unlock()
	if child == nil {
		writeRPCError(w, http.StatusConflict, "scope", "unknown or unowned hook scope")
		return
	}
	child.Close()
	if terminalEvent != nil {
		if startEventDone != nil {
			<-startEventDone
		}
		h.observe(terminalEvent)
	}
	writeJSON(w, map[string]bool{"closed": true})
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

func (b *broker) handleExecutionGet(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request struct{}
	if decodeRequest(r, &request) != nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid execution request")
		return
	}
	h := b.host
	h.mu.Lock()
	group := h.executionForOperationLocked(op)
	if group == nil {
		h.mu.Unlock()
		writeRPCError(w, http.StatusConflict, "scope", "execution group is unavailable")
		return
	}
	view := struct {
		executionSnapshot
		ActivityID string          `json:"activityId,omitempty"`
		CausedBy   []string        `json:"causedBy,omitempty"`
		Origin     *activityOrigin `json:"origin,omitempty"`
	}{executionSnapshot: group.snapshotLocked()}
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
	h.mu.Unlock()
	writeJSON(w, view)
}
