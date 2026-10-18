package agent

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"noe/internal/events"
)

type runJSRequest struct {
	RunID       string  `json:"runId"`
	ModelStepID string  `json:"modelStepId,omitempty"`
	ToolCallID  string  `json:"toolCallId,omitempty"`
	Code        *string `json:"code"`
}

func (b *broker) handleRunJS(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request runJSRequest
	if err := decodeRequest(r, &request); err != nil || request.RunID == "" || request.Code == nil {
		writeRPCError(w, http.StatusBadRequest, "request", "invalid run_js request")
		return
	}
	h := b.host
	h.mu.Lock()
	run := h.runs[request.RunID]
	valid := run != nil && run.operation == op && run.ctx.Err() == nil && !run.interrupting && !run.finalClosed && !op.closed && !h.closed
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
	var toolState *toolCallState
	requestID := ""
	causedBy := []string(nil)
	if request.ModelStepID != "" {
		if run.modelStep == nil || run.modelStep.id != request.ModelStepID || !run.modelStep.responseComplete || run.modelStep.finished || request.ToolCallID == "" {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "model_step", "run_js does not match an active model tool call")
			return
		}
		toolState = run.modelStep.toolCalls[request.ToolCallID]
		if toolState == nil || toolState.name != "run_js" || toolState.resultRecorded || toolState.started {
			h.mu.Unlock()
			writeRPCError(w, http.StatusConflict, "tool_call", "unknown or completed run_js tool call")
			return
		}
		requestID = run.modelStep.requestID
		if run.modelStep.requestActivityID != "" {
			causedBy = []string{run.modelStep.requestActivityID}
		}
	}
	activityID := ""
	var err error
	if toolState != nil {
		activityID = toolState.activityID
	} else {
		activityID, err = randomID()
		if err != nil {
			h.mu.Unlock()
			writeRPCError(w, http.StatusInternalServerError, "broker", "could not allocate tool activity")
			return
		}
	}
	if activityID == "" {
		h.mu.Unlock()
		writeRPCError(w, http.StatusInternalServerError, "broker", "tool activity ID is unavailable")
		return
	}
	if len(causedBy) == 0 && run.activityID != "" {
		causedBy = []string{run.activityID}
	}
	if toolState != nil {
		toolState.started = true
	}
	h.mu.Unlock()
	origin := activityOrigin{Kind: "tool", RunID: run.id, AgentID: run.agentID(), ToolCallID: request.ToolCallID, ToolName: "run_js", ModelStepID: request.ModelStepID}
	started := time.Now()
	startFields := events.Event{"toolCallId": request.ToolCallID, "source": "run_js", "code": *request.Code}
	if request.ModelStepID != "" {
		startFields["modelStepId"] = request.ModelStepID
		startFields["requestId"] = requestID
	}
	addActivityFields(startFields, activityID, causedBy, origin)
	if comment := firstLineComment(*request.Code); comment != "" {
		startFields["text"] = comment
	}
	h.observeRun("run_js.started", request.RunID, run, startFields)

	ctx, cancel := context.WithCancel(run.ctx)
	stopRequest := context.AfterFunc(r.Context(), cancel)
	defer func() {
		stopRequest()
		cancel()
	}()
	if ctx.Err() != nil {
		result := Result{ExitCode: -1, Reason: reasonForContext(ctx.Err())}
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, activityID, causedBy, origin, result)
		status, code, message := classifyProviderError(ctx.Err(), ctx)
		writeRPCError(w, status, code, message)
		return
	}

	// Give this generated NOE process an independent broker scope. Register it
	// under the run before starting the process so finish can cancel descendants.
	childOperation := h.NewOperation(ctx)
	h.mu.Lock()
	groupOpen := h.executionGroupOpenLocked(run.group)
	registered := h.runs[request.RunID] == run && run.ctx.Err() == nil && !run.interrupting && !op.closed && !h.closed && !childOperation.closed && groupOpen
	if registered {
		childOperation.parentRunID = request.RunID
		childOperation.group = run.group
		childOperation.causeActivityIDs = []string{activityID}
		childOperation.activityOrigin = origin
		childOperation.decisionOwner = decisionOwnerFields(request.RunID, run, request.ToolCallID)
		run.childScopes[childOperation] = struct{}{}
	}
	h.mu.Unlock()
	if !registered {
		childOperation.Close()
		result := Result{ExitCode: -1, Reason: reasonForContext(run.ctx.Err())}
		if result.Reason == "" {
			result.Reason = "cancelled"
		}
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, activityID, causedBy, origin, result)
		if !groupOpen {
			code, message := executionLimitError(run.group)
			writeRPCError(w, http.StatusConflict, code, message)
			return
		}
		status, code, message := classifyProviderError(run.ctx.Err(), run.ctx)
		writeRPCError(w, status, code, message)
		return
	}
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			childOperation.Close()
			h.mu.Lock()
			delete(run.childScopes, childOperation)
			h.mu.Unlock()
		})
	}
	defer release()
	if opErr := childOperation.ctx.Err(); opErr != nil {
		result := Result{ExitCode: -1, Reason: reasonForContext(opErr)}
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, activityID, causedBy, origin, result)
		status, code, message := classifyProviderError(opErr, childOperation.ctx)
		writeRPCError(w, status, code, message)
		return
	}
	if childOperation.initErr != nil {
		result := Result{Stderr: childOperation.initErr.Error(), ExitCode: -1, Reason: "failed"}
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, activityID, causedBy, origin, result)
		writeJSON(w, result)
		return
	}
	connection := &Connection{
		URL: b.url, Token: b.token, Scope: childOperation.scope,
		Defaults: Config{
			Provider: run.config.Provider, Model: run.config.Model,
			MaxTokens: run.config.MaxTokens, Effort: run.config.Effort,
			Hooks:                cloneHookModules(run.config.Hooks),
			ContextBudget:        cloneContextBudget(run.config.ContextBudget),
			contextBudgetPresent: run.config.ContextBudgetPresent || run.config.ContextBudget != nil,
		},
	}
	result, err := (Runner{
		Executable: h.opts.Executable,
		Cwd:        h.opts.Cwd,
		Connection: connection,
		ImportMap:  h.opts.ImportMap,
	}).Run(childOperation.ctx, *request.Code)
	contextErr := ctx.Err()
	release()
	if contextErr != nil {
		result.ExitCode = -1
		result.Reason = reasonForContext(contextErr)
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, activityID, causedBy, origin, result)
		status, code, message := classifyProviderError(contextErr, ctx)
		writeRPCError(w, status, code, message)
		return
	}
	if err != nil {
		result.Reason = "failed"
		if result.ExitCode == 0 {
			result.ExitCode = -1
		}
		if result.Stderr != "" {
			result.Stderr += "\n"
		}
		result.Stderr += err.Error()
	}
	if result.Reason == "" {
		if result.ExitCode == 0 {
			result.Reason = "completed"
		} else {
			result.Reason = "failed"
		}
	}
	b.finishJSResult(request.RunID, run, started, request.ToolCallID, activityID, causedBy, origin, result)
	writeJSON(w, result)
}

func (b *broker) finishJSResult(runID string, run *agentRun, started time.Time, toolCallID, activityID string, causedBy []string, origin activityOrigin, result Result) {
	b.traceJSResult(runID, run, started, result)
	fields := events.Event{"toolCallId": toolCallID, "source": "run_js"}
	addActivityFields(fields, activityID, causedBy, origin)
	if result.Stdout != "" {
		stdout := fieldsWith(fields, events.Event{"text": result.Stdout})
		b.host.observeRun("stdout", runID, run, stdout)
	}
	if result.Stderr != "" {
		stderr := fieldsWith(fields, events.Event{"text": result.Stderr})
		b.host.observeRun("stderr", runID, run, stderr)
	}
	finishFields := events.Event{
		"success":  result.Reason == "completed" && result.ExitCode == 0,
		"exitCode": result.ExitCode, "reason": result.Reason,
		"duration": time.Since(started),
	}
	if origin.ModelStepID != "" {
		finishFields["modelStepId"] = origin.ModelStepID
		if requestID := originRequestID(b.host, run, origin.ModelStepID); requestID != "" {
			finishFields["requestId"] = requestID
		}
	}
	b.host.observeRun("run_js.finished", runID, run, fieldsWith(fields, finishFields))
}

func originRequestID(h *Host, run *agentRun, modelStepID string) string {
	if h == nil || run == nil || modelStepID == "" {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if run.modelStep == nil || run.modelStep.id != modelStepID {
		return ""
	}
	return run.modelStep.requestID
}

func firstLineComment(code string) string {
	line, _, _ := strings.Cut(code, "\n")
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "//") {
		return ""
	}
	return line
}
