package agent

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

type runJSRequest struct {
	RunID       string  `json:"runId"`
	ModelStepID string  `json:"modelStepId,omitempty"`
	ToolCallID  string  `json:"toolCallId,omitempty"`
	Code        *string `json:"code"`
}

type runJSResponse struct {
	Result
	Images []imageapi.Metadata `json:"images,omitempty"`
}

// runJS owns admission, activity accounting, and child-operation lifetime for
// each caller-supplied in-process Executor invocation.
func (h *Host) runJS(requestContext context.Context, op *Operation, request runJSRequest) (runJSResponse, error) {
	if requestContext == nil || op == nil || request.RunID == "" || request.Code == nil {
		return runJSResponse{}, newOperationError(http.StatusBadRequest, "request", "invalid run_js request")
	}
	options, pragmaErr := parseRunJSOptions(*request.Code)
	h.mu.Lock()
	run := h.runs[request.RunID]
	valid := run != nil && run.operation == op && run.ctx.Err() == nil && !run.interrupting && !run.finalClosed && !op.closed && !h.closed
	if !valid {
		h.mu.Unlock()
		return runJSResponse{}, newOperationError(http.StatusConflict, "run", "unknown agent run")
	}
	if !h.executionGroupOpenLocked(run.group) {
		group := run.group
		h.mu.Unlock()
		code, message := executionLimitError(group)
		return runJSResponse{}, newOperationError(http.StatusConflict, code, message)
	}
	var toolState *toolCallState
	requestID := ""
	causedBy := []string(nil)
	if request.ModelStepID != "" {
		if run.modelStep == nil || run.modelStep.id != request.ModelStepID || !run.modelStep.responseComplete || run.modelStep.finished || request.ToolCallID == "" {
			h.mu.Unlock()
			return runJSResponse{}, newOperationError(http.StatusConflict, "model_step", "run_js does not match an active model tool call")
		}
		toolState = run.modelStep.toolCalls[request.ToolCallID]
		if toolState == nil || toolState.name != "run_js" || toolState.resultRecorded || toolState.started {
			h.mu.Unlock()
			return runJSResponse{}, newOperationError(http.StatusConflict, "tool_call", "unknown or completed run_js tool call")
		}
		requestID = run.modelStep.requestID
		if run.modelStep.requestActivityID != "" {
			causedBy = []string{run.modelStep.requestActivityID}
		}
	}
	activityID := ""
	if toolState != nil {
		activityID = toolState.activityID
	} else {
		var err error
		activityID, err = randomID()
		if err != nil {
			h.mu.Unlock()
			return runJSResponse{}, newOperationError(http.StatusInternalServerError, "broker", "could not allocate tool activity")
		}
	}
	if activityID == "" {
		h.mu.Unlock()
		return runJSResponse{}, newOperationError(http.StatusInternalServerError, "broker", "tool activity ID is unavailable")
	}
	if len(causedBy) == 0 && run.activityID != "" {
		causedBy = []string{run.activityID}
	}
	if toolState != nil {
		toolState.started = true
		toolState.outputBytes = options.outputBytes
	}
	h.mu.Unlock()

	started := time.Now()
	origin := activityOrigin{Kind: "tool", RunID: run.id, AgentID: run.agentID(), ToolCallID: request.ToolCallID, ToolName: "run_js", ModelStepID: request.ModelStepID}
	finish := func(result Result) runJSResponse {
		h.finishJSResult(request.RunID, run, started, request.ToolCallID, activityID, causedBy, origin, result)
		images := h.finishToolImages(run, request.ModelStepID, request.ToolCallID, result.ExitCode == 0 && result.Reason == "completed")
		return runJSResponse{Result: result, Images: images}
	}
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
	if pragmaErr != nil {
		result := failedResult()
		appendExecutionDiagnostic(&result, pragmaErr, "setup")
		return finish(result), nil
	}

	parentCtx, cancel := context.WithCancel(run.ctx)
	stopRequest := context.AfterFunc(requestContext, cancel)
	defer func() {
		stopRequest()
		cancel()
	}()
	ctx := parentCtx
	if options.timeout > 0 {
		var stopDeadline context.CancelFunc
		ctx, stopDeadline = context.WithDeadline(parentCtx, started.Add(options.timeout))
		defer stopDeadline()
	}
	contextFailure := func() (err error, localTimeout bool) {
		// The request cancellation callback may not have run yet.
		if requestContext.Err() != nil {
			cancel()
		}
		err = ctx.Err()
		if parentErr := parentCtx.Err(); parentErr != nil {
			return parentErr, false
		}
		return err, err == context.DeadlineExceeded
	}
	finishInterrupted := func(result Result, err error, stage string, localTimeout bool) (runJSResponse, error) {
		result.ExitCode = -1
		result.Reason = reasonForContext(err)
		appendExecutionDiagnostic(&result, err, stage)
		response := finish(result)
		if localTimeout {
			return response, nil
		}
		return runJSResponse{}, err
	}
	if err, localTimeout := contextFailure(); err != nil {
		return finishInterrupted(Result{}, err, "setup", localTimeout)
	}

	// The tool receives its own scope so completion can cancel and join all
	// descendants before the result is committed to the model step.
	childOperation := h.NewOperation(ctx)
	h.mu.Lock()
	groupOpen := h.executionGroupOpenLocked(run.group)
	registered := h.runs[request.RunID] == run && run.ctx.Err() == nil && !run.interrupting && !op.closed && !h.closed && !childOperation.closed && groupOpen
	if registered {
		childOperation.parentRunID = request.RunID
		childOperation.imageOwner = &imageOwner{runID: request.RunID, stepID: request.ModelStepID, toolID: request.ToolCallID}
		childOperation.group = run.group
		childOperation.causeActivityIDs = []string{activityID}
		childOperation.activityOrigin = origin
		childOperation.decisionOwner = decisionOwnerFields(request.RunID, run, request.ToolCallID)
		run.childScopes[childOperation] = struct{}{}
	}
	h.mu.Unlock()
	if !registered {
		childOperation.Close()
		scopeErr := run.ctx.Err()
		if scopeErr == nil {
			scopeErr = errors.New("run_js scope has ended")
		}
		result := Result{ExitCode: -1, Reason: reasonForContext(scopeErr)}
		if result.Reason == "" {
			result.Reason = "cancelled"
		}
		appendExecutionDiagnostic(&result, scopeErr, "setup")
		finish(result)
		if !groupOpen {
			code, message := executionLimitError(run.group)
			return runJSResponse{}, newOperationError(http.StatusConflict, code, message)
		}
		if run.ctx.Err() != nil {
			return runJSResponse{}, run.ctx.Err()
		}
		return runJSResponse{}, newOperationError(http.StatusConflict, "scope", "run_js scope has ended")
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
	stage := "setup"
	var result Result
	var err error
	var waitProgress func()
	scopeErr := childOperation.ctx.Err()
	if scopeErr == nil {
		switch {
		case childOperation.initErr != nil:
			err = childOperation.initErr
		case h.opts.Execute == nil:
			err = errors.New("agent execution runtime is unavailable")
		default:
			config, _ := json.Marshal(hookDefaults(run))
			if requestContext.Err() != nil {
				cancel()
			}
			scopeErr = childOperation.ctx.Err()
			if scopeErr == nil {
				stage = "execute"
				execution := Execution{
					Operation: childOperation, Kind: ExecutionTool,
					Config: config, Code: *request.Code, OutputBytes: options.outputBytes,
					SaveData: options.saveData,
				}
				if h.opts.Observe != nil {
					execution.Progress, waitProgress = startToolProgress(childOperation.ctx, func(stream, text string) {
						fields := events.Event{
							"toolCallId": request.ToolCallID, "source": "run_js",
							"stream": stream, "text": text,
						}
						if request.ModelStepID != "" {
							fields["modelStepId"] = request.ModelStepID
							fields["requestId"] = requestID
						}
						addActivityFields(fields, activityID, causedBy, origin)
						h.observeRun("run_js.progress", request.RunID, run, fields)
					})
				}
				result, err = h.opts.Execute(childOperation.ctx, execution)
			}
		}
	}
	// Closing the child cancels its context, so read the enclosing contexts
	// after joining cleanup to distinguish a deadline from normal scope release.
	release()
	contextErr, localTimeout := contextFailure()
	if waitProgress != nil {
		waitProgress()
	}
	if contextErr != nil {
		return finishInterrupted(result, contextErr, stage, localTimeout)
	}
	if scopeErr != nil {
		return finishInterrupted(result, scopeErr, stage, false)
	}
	if err != nil {
		result.Reason = "failed"
		if contextReason := reasonForContext(err); contextReason == "cancelled" || contextReason == "timeout" {
			result.Reason = contextReason
		}
		if result.ExitCode == 0 {
			result.ExitCode = -1
		}
		appendExecutionDiagnostic(&result, err, stage)
	}
	appendReasonDiagnostic(&result, stage)
	if result.Reason == "" {
		if result.ExitCode == 0 {
			result.Reason = "completed"
		} else {
			result.Reason = "failed"
		}
	}
	return finish(result), nil
}

func (h *Host) finishJSResult(runID string, run *agentRun, started time.Time, toolCallID, activityID string, causedBy []string, origin activityOrigin, result Result) {
	exitCode := result.ExitCode
	h.traceEvent(traceEvent{
		RunID: runID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
		Status: "js_result", Duration: int64(time.Since(started)), ExitCode: &exitCode, Reason: result.Reason,
	})
	fields := events.Event{"toolCallId": toolCallID, "source": "run_js"}
	addActivityFields(fields, activityID, causedBy, origin)
	if result.Stdout != "" {
		stdout := fieldsWith(fields, events.Event{"text": result.Stdout})
		if result.StdoutOmittedBytes > 0 {
			stdout["omittedBytes"] = result.StdoutOmittedBytes
		}
		h.observeRun("stdout", runID, run, stdout)
	}
	if result.Stderr != "" {
		stderr := fieldsWith(fields, events.Event{"text": result.Stderr})
		if result.StderrOmittedBytes > 0 {
			stderr["omittedBytes"] = result.StderrOmittedBytes
		}
		h.observeRun("stderr", runID, run, stderr)
	}
	finishFields := events.Event{
		"success":  result.Reason == "completed" && result.ExitCode == 0,
		"exitCode": result.ExitCode, "reason": result.Reason,
		"duration": time.Since(started),
	}
	if result.StdoutOmittedBytes > 0 {
		finishFields["stdoutOmittedBytes"] = result.StdoutOmittedBytes
	}
	if result.StderrOmittedBytes > 0 {
		finishFields["stderrOmittedBytes"] = result.StderrOmittedBytes
	}
	if result.StdoutFile != nil {
		finishFields["stdoutFile"] = *result.StdoutFile
	}
	if result.StderrFile != nil {
		finishFields["stderrFile"] = *result.StderrFile
	}
	if result.DataFile != nil {
		finishFields["dataFile"] = *result.DataFile
	}
	if len(result.Data) > 0 {
		finishFields["data"] = cloneRawJSON(result.Data)
	}
	if len(result.Diagnostics) > 0 {
		finishFields["diagnostics"] = result.Diagnostics
	}
	if origin.ModelStepID != "" {
		finishFields["modelStepId"] = origin.ModelStepID
		if requestID := originRequestID(h, run, origin.ModelStepID); requestID != "" {
			finishFields["requestId"] = requestID
		}
	}
	h.observeRun("run_js.finished", runID, run, fieldsWith(fields, finishFields))
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
