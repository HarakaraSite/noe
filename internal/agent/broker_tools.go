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
	RunID      string  `json:"runId"`
	ToolCallID string  `json:"toolCallId,omitempty"`
	Code       *string `json:"code"`
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
	h.mu.Unlock()
	if !valid {
		writeRPCError(w, http.StatusConflict, "run", "unknown agent run")
		return
	}
	started := time.Now()
	startFields := events.Event{"toolCallId": request.ToolCallID, "source": "run_js", "code": *request.Code}
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
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
		status, code, message := classifyProviderError(ctx.Err(), ctx)
		writeRPCError(w, status, code, message)
		return
	}

	// Give this generated NOE process an independent broker scope. Register it
	// under the run before starting the process so finish can cancel descendants.
	childOperation := h.NewOperation(ctx)
	h.mu.Lock()
	registered := h.runs[request.RunID] == run && run.ctx.Err() == nil && !run.interrupting && !op.closed && !h.closed && !childOperation.closed
	if registered {
		childOperation.parentRunID = request.RunID
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
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
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
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
		status, code, message := classifyProviderError(opErr, childOperation.ctx)
		writeRPCError(w, status, code, message)
		return
	}
	if childOperation.initErr != nil {
		result := Result{Stderr: childOperation.initErr.Error(), ExitCode: -1, Reason: "failed"}
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
		writeJSON(w, result)
		return
	}
	connection := &Connection{
		URL: b.url, Token: b.token, Scope: childOperation.scope,
		Defaults: Config{
			Provider: run.config.Provider, Model: run.config.Model,
			MaxTokens: run.config.MaxTokens, Effort: run.config.Effort,
		},
	}
	result, err := (Runner{
		Executable: h.opts.Executable,
		Cwd:        h.opts.Cwd,
		Connection: connection,
	}).Run(childOperation.ctx, *request.Code)
	contextErr := ctx.Err()
	release()
	if contextErr != nil {
		result.ExitCode = -1
		result.Reason = reasonForContext(contextErr)
		b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
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
	b.finishJSResult(request.RunID, run, started, request.ToolCallID, result)
	writeJSON(w, result)
}

func (b *broker) finishJSResult(runID string, run *agentRun, started time.Time, toolCallID string, result Result) {
	b.traceJSResult(runID, run, started, result)
	fields := events.Event{"toolCallId": toolCallID, "source": "run_js"}
	if result.Stdout != "" {
		stdout := fieldsWith(fields, events.Event{"text": result.Stdout})
		b.host.observeRun("stdout", runID, run, stdout)
	}
	if result.Stderr != "" {
		stderr := fieldsWith(fields, events.Event{"text": result.Stderr})
		b.host.observeRun("stderr", runID, run, stderr)
	}
	b.host.observeRun("run_js.finished", runID, run, fieldsWith(fields, events.Event{
		"success":  result.Reason == "completed" && result.ExitCode == 0,
		"exitCode": result.ExitCode, "reason": result.Reason,
		"duration": time.Since(started),
	}))
}

func firstLineComment(code string) string {
	line, _, _ := strings.Cut(code, "\n")
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "//") {
		return ""
	}
	return line
}
