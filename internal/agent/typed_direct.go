package agent

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"unicode/utf8"
)

// CallTypedContext invokes the typed Go-client operations without crossing the
// JSON adapter used by JavaScript. Request fields are copied before admission
// so the accepted operation owns its input snapshot just as the JSON path did.
func (op *Operation) CallTypedContext(ctx context.Context, method string, request any) (any, error) {
	if op == nil || op.host == nil {
		return nil, ErrOperationClosed
	}
	snapshot, err := snapshotTypedRequest(method, request)
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		return nil, &RequestError{Code: "request", Message: "agent request context is required"}
	}
	if err := op.ctx.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if op.initErr != nil {
		return nil, op.initErr
	}
	if !validOperationMethod(method) {
		return nil, &RequestError{Code: "method", Message: "unknown agent request method"}
	}
	return op.host.callTypedDirect(ctx, op, method, snapshot)
}

func (h *Host) callTypedDirect(ctx context.Context, caller *Operation, method string, request any) (any, error) {
	return h.withDirectRequest(ctx, caller, "", func(merged context.Context, target *Operation) (any, error) {
		return h.dispatchTypedDirect(merged, target, method, request)
	})
}

func (h *Host) dispatchTypedDirect(ctx context.Context, op *Operation, method string, request any) (any, error) {
	switch method {
	case "list_agents":
		if !op.control {
			return nil, newOperationError(403, "scope", "host management is only available to a trusted Go operation")
		}
		snapshots, err := h.listAgents(op)
		if err != nil {
			return nil, err
		}
		return cloneInstanceSnapshots(snapshots), nil
	case "spawn_agent":
		req, ok := request.(SpawnRequest)
		if !ok {
			return nil, newOperationError(400, "request", "invalid agent configuration")
		}
		if err := validateTypedTaskConfig(req.Config); err != nil {
			return nil, newOperationError(400, "request", "invalid agent configuration")
		}
		return h.spawn(ctx, op, req)
	case "queue_agent":
		req, ok := request.(QueueRequest)
		if !ok {
			return nil, newOperationError(400, "request", "agentId and instruction are required")
		}
		return h.queue(op, req)
	case "observe_agent", "wait_agent":
		req, ok := request.(InstanceRequest)
		if !ok {
			return nil, newOperationError(400, "request", "agentId is required")
		}
		snapshot, err := h.observeInstance(op, req, method == "wait_agent", ctx)
		if err != nil {
			return nil, err
		}
		return cloneInstanceSnapshot(snapshot), nil
	case "wait_task":
		req, ok := request.(TaskRequest)
		if !ok {
			return nil, newOperationError(400, "request", "agentId and commandId are required")
		}
		outcome, err := h.waitTask(op, req, ctx)
		if err != nil {
			return nil, err
		}
		return cloneTaskOutcome(outcome), nil
	case "wait_tasks":
		req, ok := request.(WaitTasksRequest)
		if !ok {
			return nil, newOperationError(400, "request", "invalid tasks wait request")
		}
		result, err := h.waitTasks(op, req, ctx)
		if err != nil {
			return nil, err
		}
		return cloneWaitTasksResult(result), nil
	case "read_task_result":
		req, ok := request.(TaskResultRequest)
		if !ok {
			return nil, newOperationError(400, "request", "invalid task result query")
		}
		result, err := h.readTaskResult(op, req)
		if err != nil {
			return nil, err
		}
		result.RunID = normalizeJSONText(result.RunID)
		result.CommandID = normalizeJSONText(result.CommandID)
		result.State = normalizeJSONText(result.State)
		result.Text = normalizeJSONText(result.Text)
		return result, nil
	case "steer_agent":
		req, ok := request.(SteerRequest)
		if !ok {
			return nil, newOperationError(400, "request", "runId and instruction are required")
		}
		return h.steer(op, req)
	case "cancel_task":
		req, ok := request.(CancelTaskRequest)
		if !ok {
			return nil, newOperationError(400, "request", "agentId and commandId are required")
		}
		return h.cancelTask(op, req)
	case "stop_agent":
		req, ok := request.(StopAgentRequest)
		if !ok {
			return nil, newOperationError(400, "request", "agentId is required")
		}
		return h.stopAgent(op, req)
	default:
		return nil, &RequestError{Code: "method", Message: "agent operation is not available: " + method}
	}
}

func snapshotTypedRequest(method string, request any) (any, error) {
	switch method {
	case "list_agents":
		return struct{}{}, nil
	case "spawn_agent":
		req, ok := request.(SpawnRequest)
		if !ok {
			return nil, errors.New("agent: invalid typed spawn request")
		}
		req.Config = cloneTaskConfig(req.Config)
		req.Instruction = cloneString(req.Instruction)
		req.AllowDelegation = cloneBool(req.AllowDelegation)
		if policy := req.Config.ContextPolicy; policy != nil && policy.RetainRatio != nil && (math.IsNaN(*policy.RetainRatio) || math.IsInf(*policy.RetainRatio, 0)) {
			_, err := json.Marshal(*policy.RetainRatio)
			return nil, err
		}
		return req, nil
	case "queue_agent":
		req, ok := request.(QueueRequest)
		if !ok {
			return nil, errors.New("agent: invalid typed queue request")
		}
		req.AgentID = normalizeJSONText(req.AgentID)
		req.Instruction = normalizeJSONText(req.Instruction)
		req.CommandID = normalizeJSONText(req.CommandID)
		return req, nil
	case "observe_agent", "wait_agent":
		req, ok := request.(InstanceRequest)
		if !ok {
			return nil, errors.New("agent: invalid typed instance request")
		}
		req.AgentID = normalizeJSONText(req.AgentID)
		req.IncludeAnswer = cloneBool(req.IncludeAnswer)
		return req, nil
	case "wait_task":
		req, ok := request.(TaskRequest)
		if !ok {
			return nil, errors.New("agent: invalid typed task request")
		}
		req.AgentID = normalizeJSONText(req.AgentID)
		req.CommandID = normalizeJSONText(req.CommandID)
		req.IncludeAnswer = cloneBool(req.IncludeAnswer)
		return req, nil
	case "wait_tasks":
		req, ok := request.(WaitTasksRequest)
		if !ok {
			return nil, errors.New("agent: invalid typed tasks-wait request")
		}
		req.Tasks = append([]TaskReference(nil), req.Tasks...)
		for index := range req.Tasks {
			req.Tasks[index].AgentID = normalizeJSONText(req.Tasks[index].AgentID)
			req.Tasks[index].CommandID = normalizeJSONText(req.Tasks[index].CommandID)
		}
		req.IncludeAnswer = cloneBool(req.IncludeAnswer)
		return req, nil
	case "read_task_result":
		req, ok := request.(TaskResultRequest)
		if !ok {
			return nil, errors.New("agent: invalid typed task-result request")
		}
		req.AgentID = normalizeJSONText(req.AgentID)
		req.CommandID = normalizeJSONText(req.CommandID)
		req.MaxBytes = cloneInt64(req.MaxBytes)
		return req, nil
	case "steer_agent":
		req, ok := request.(SteerRequest)
		if !ok {
			return nil, errors.New("agent: invalid typed steer request")
		}
		req.RunID = normalizeJSONText(req.RunID)
		req.Instruction = normalizeJSONText(req.Instruction)
		req.CommandID = normalizeJSONText(req.CommandID)
		return req, nil
	case "cancel_task":
		req, ok := request.(CancelTaskRequest)
		if !ok {
			return nil, errors.New("agent: invalid typed cancellation request")
		}
		req.AgentID = normalizeJSONText(req.AgentID)
		req.CommandID = normalizeJSONText(req.CommandID)
		return req, nil
	case "stop_agent":
		req, ok := request.(StopAgentRequest)
		if !ok {
			return nil, errors.New("agent: invalid typed stop request")
		}
		req.AgentID = normalizeJSONText(req.AgentID)
		return req, nil
	default:
		return request, nil
	}
}

func cloneTaskConfig(config TaskConfig) TaskConfig {
	copy := config
	copy.Config = cloneConfig(config.Config)
	copy.CacheKey = cloneString(config.CacheKey)
	return copy
}

func cloneConfig(config Config) Config {
	copy := config
	copy.Provider = normalizeJSONText(copy.Provider)
	copy.Model = normalizeJSONText(copy.Model)
	copy.Name = normalizeJSONText(copy.Name)
	copy.System = normalizeJSONText(copy.System)
	copy.Effort = normalizeJSONText(copy.Effort)
	copy.Cwd = normalizeJSONText(copy.Cwd)
	if config.Hooks != nil {
		if *config.Hooks == nil {
			copy.Hooks = nil
		} else {
			hooks := append(HookModules{}, (*config.Hooks)...)
			for index := range hooks {
				hooks[index] = normalizeJSONText(hooks[index])
			}
			copy.Hooks = &hooks
		}
	}
	if config.ContextBudget != nil {
		budget := *config.ContextBudget
		copy.ContextBudget = &budget
	}
	copy.ContextPolicy = cloneContextPolicy(config.ContextPolicy)
	if config.ExecutionLimits != nil {
		limits := cloneExecutionLimits(config.ExecutionLimits)
		copy.ExecutionLimits = &limits
	}
	// MarshalJSON writes any non-nil field, and UnmarshalJSON restores these
	// presence bits from the resulting object just as the former adapter did.
	copy.contextBudgetPresent = config.contextBudgetPresent || config.ContextBudget != nil
	copy.contextPolicyPresent = config.contextPolicyPresent || config.ContextPolicy != nil
	copy.executionLimitsPresent = config.executionLimitsPresent || config.ExecutionLimits != nil
	return copy
}

func validateTypedTaskConfig(config TaskConfig) error {
	if budget := config.ContextBudget; budget != nil && (budget.InputTokens <= 0 || budget.InputTokens > maxSafeContextBudget) {
		return errors.New("invalid context budget")
	}
	if policy := config.ContextPolicy; policy != nil {
		if ratio := policy.RetainRatio; ratio != nil && (*ratio <= 0 || *ratio >= 1 || math.IsNaN(*ratio) || math.IsInf(*ratio, 0)) {
			return errors.New("invalid context policy retain ratio")
		}
		if reserve := policy.ResponseReserveTokens; reserve != nil && (*reserve <= 0 || *reserve > maxSafeContextBudget) {
			return errors.New("invalid context policy response reserve")
		}
	}
	if limits := config.ExecutionLimits; limits != nil {
		for _, value := range []*int64{limits.ProviderCalls, limits.AgentStarts, limits.QueuedTasks, limits.HookCalls} {
			if value != nil && (*value < 0 || *value > maxSafeContextBudget) {
				return errors.New("invalid execution limit")
			}
		}
	} else if config.executionLimitsPresent {
		return errors.New("execution limits must be an object")
	}
	return nil
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := normalizeJSONText(*value)
	return &copy
}

// encoding/json replaces each malformed UTF-8 byte with its own U+FFFD when
// decoding the old client adapter payload. Keep valid strings allocation-free.
func normalizeJSONText(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	var normalized []byte
	for index := 0; index < len(value); {
		_, size := utf8.DecodeRuneInString(value[index:])
		if size == 1 && value[index] >= utf8.RuneSelf {
			normalized = append(normalized, '\xef', '\xbf', '\xbd')
			index++
			continue
		}
		normalized = append(normalized, value[index:index+size]...)
		index += size
	}
	return string(normalized)
}

func cloneInstanceSnapshots(snapshots []InstanceSnapshot) []InstanceSnapshot {
	if snapshots == nil {
		return nil
	}
	copy := make([]InstanceSnapshot, len(snapshots))
	for index, snapshot := range snapshots {
		copy[index] = cloneInstanceSnapshot(snapshot)
	}
	return copy
}

func cloneInstanceSnapshot(snapshot InstanceSnapshot) InstanceSnapshot {
	copy := snapshot
	copy.AgentID = normalizeJSONText(copy.AgentID)
	copy.GroupID = normalizeJSONText(copy.GroupID)
	copy.ActivityID = normalizeJSONText(copy.ActivityID)
	copy.ParentRunID = normalizeJSONText(copy.ParentRunID)
	copy.Name = normalizeJSONText(copy.Name)
	copy.State = normalizeJSONText(copy.State)
	copy.Reason = normalizeJSONText(copy.Reason)
	copy.CurrentRunID = normalizeJSONText(copy.CurrentRunID)
	copy.CurrentCommandID = normalizeJSONText(copy.CurrentCommandID)
	copy.CausedBy = append([]string(nil), snapshot.CausedBy...)
	for index := range copy.CausedBy {
		copy.CausedBy[index] = normalizeJSONText(copy.CausedBy[index])
	}
	if snapshot.Origin != nil {
		origin := *snapshot.Origin
		normalizeActivityOrigin(&origin)
		copy.Origin = &origin
	}
	if snapshot.Execution != nil {
		execution := *snapshot.Execution
		execution.GroupID = normalizeJSONText(execution.GroupID)
		execution.Limits = cloneExecutionLimits(&snapshot.Execution.Limits)
		execution.Stop = cloneExecutionStop(snapshot.Execution.Stop)
		if execution.Stop != nil {
			execution.Stop.Reason = normalizeJSONText(execution.Stop.Reason)
			execution.Stop.LimitName = normalizeJSONText(execution.Stop.LimitName)
			if execution.Stop.Source != nil {
				execution.Stop.Source.ActivityID = normalizeJSONText(execution.Stop.Source.ActivityID)
				if execution.Stop.Source.Origin != nil {
					normalizeActivityOrigin(execution.Stop.Source.Origin)
				}
			}
		}
		if snapshot.Execution.Usage != nil {
			usage := *snapshot.Execution.Usage
			usage.PromptTokens = cloneInt64(usage.PromptTokens)
			usage.CompletionTokens = cloneInt64(usage.CompletionTokens)
			usage.TotalTokens = cloneInt64(usage.TotalTokens)
			usage.CachedTokens = cloneInt64(usage.CachedTokens)
			usage.Cost = cloneFloat64(usage.Cost)
			execution.Usage = &usage
		}
		copy.Execution = &execution
	}
	copy.LastResult = cloneTaskOutcomePointer(snapshot.LastResult)
	if snapshot.Context != nil {
		contextCopy := *snapshot.Context
		contextCopy.RunID = normalizeJSONText(contextCopy.RunID)
		contextCopy.ModelStepID = normalizeJSONText(contextCopy.ModelStepID)
		if snapshot.Context.ExecutionContext != nil {
			execution := *snapshot.Context.ExecutionContext
			execution.Policy = normalizeJSONText(execution.Policy)
			execution.BudgetSource = normalizeJSONText(execution.BudgetSource)
			execution.ResponseReserveTokens = cloneInt64(execution.ResponseReserveTokens)
			execution.UsedPercent = cloneFloat64(execution.UsedPercent)
			execution.LastTrim = cloneContextTrim(snapshot.Context.ExecutionContext.LastTrim)
			execution.PendingTrim = cloneContextTrim(snapshot.Context.ExecutionContext.PendingTrim)
			contextCopy.ExecutionContext = &execution
		}
		contextCopy.ModelWindow.Tokens = cloneInt64(contextCopy.ModelWindow.Tokens)
		contextCopy.ModelWindow.Source = cloneString(contextCopy.ModelWindow.Source)
		contextCopy.Previous = cloneContextPrevious(contextCopy.Previous)
		if contextCopy.Previous != nil {
			contextCopy.Previous.RequestID = normalizeJSONText(contextCopy.Previous.RequestID)
			contextCopy.Previous.Status = normalizeJSONText(contextCopy.Previous.Status)
		}
		contextCopy.Estimate = cloneContextEstimate(contextCopy.Estimate)
		if contextCopy.Estimate != nil {
			contextCopy.Estimate.Method = normalizeJSONText(contextCopy.Estimate.Method)
			contextCopy.Estimate.Stage = normalizeJSONText(contextCopy.Estimate.Stage)
			contextCopy.Estimate.ModelStepID = normalizeJSONText(contextCopy.Estimate.ModelStepID)
			contextCopy.Estimate.RequestID = cloneString(contextCopy.Estimate.RequestID)
		}
		contextCopy.Budget.InputTokens = cloneInt64(contextCopy.Budget.InputTokens)
		contextCopy.RemainingTokens = cloneInt64(contextCopy.RemainingTokens)
		contextCopy.UsedPercent = cloneFloat64(contextCopy.UsedPercent)
		copy.Context = &contextCopy
	}
	return copy
}

func cloneWaitTasksResult(result WaitTasksResult) WaitTasksResult {
	copy := result
	copy.Reason = normalizeJSONText(copy.Reason)
	copy.RunID = normalizeJSONText(copy.RunID)
	copy.Completed = make([]CompletedTask, len(result.Completed))
	for index, completed := range result.Completed {
		copy.Completed[index] = CompletedTask{
			AgentID: normalizeJSONText(completed.AgentID),
			Outcome: cloneTaskOutcome(completed.Outcome),
		}
	}
	copy.Pending = make([]TaskReference, len(result.Pending))
	for index, pending := range result.Pending {
		copy.Pending[index] = TaskReference{
			AgentID:   normalizeJSONText(pending.AgentID),
			CommandID: normalizeJSONText(pending.CommandID),
		}
	}
	return copy
}

func cloneTaskOutcome(outcome TaskOutcome) TaskOutcome {
	copy := outcome
	copy.RunID = normalizeJSONText(copy.RunID)
	copy.CommandID = normalizeJSONText(copy.CommandID)
	copy.State = normalizeJSONText(copy.State)
	copy.Answer = normalizeJSONText(copy.Answer)
	copy.Error = normalizeJSONText(copy.Error)
	copy.AnswerBytes = cloneInt(outcome.AnswerBytes)
	if outcome.ErrorDetails != nil {
		details := *outcome.ErrorDetails
		details.Code = normalizeJSONText(details.Code)
		details.Message = normalizeJSONText(details.Message)
		details.ProviderCode = normalizeJSONText(details.ProviderCode)
		details.ProviderMessage = normalizeJSONText(details.ProviderMessage)
		details.RequestID = normalizeJSONText(details.RequestID)
		details.Usage = append(json.RawMessage(nil), outcome.ErrorDetails.Usage...)
		copy.ErrorDetails = &details
	}
	if outcome.omitAnswer {
		answerBytes := len(outcome.Answer)
		copy.Answer = ""
		copy.AnswerBytes = &answerBytes
		copy.omitAnswer = false
	}
	return copy
}

func cloneTaskOutcomePointer(outcome *TaskOutcome) *TaskOutcome {
	if outcome == nil {
		return nil
	}
	copy := cloneTaskOutcome(*outcome)
	return &copy
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func normalizeActivityOrigin(origin *activityOrigin) {
	if origin == nil {
		return
	}
	origin.Kind = normalizeJSONText(origin.Kind)
	origin.RunID = normalizeJSONText(origin.RunID)
	origin.AgentID = normalizeJSONText(origin.AgentID)
	origin.ToolCallID = normalizeJSONText(origin.ToolCallID)
	origin.ToolName = normalizeJSONText(origin.ToolName)
	origin.CommandID = normalizeJSONText(origin.CommandID)
	origin.HookID = normalizeJSONText(origin.HookID)
	origin.Module = normalizeJSONText(origin.Module)
	origin.Phase = normalizeJSONText(origin.Phase)
	origin.ModelStepID = normalizeJSONText(origin.ModelStepID)
}

func cloneFloat64(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneContextTrim(trim *contextTrimSnapshot) *contextTrimSnapshot {
	if trim == nil {
		return nil
	}
	copy := *trim
	copy.ID = normalizeJSONText(copy.ID)
	return &copy
}
