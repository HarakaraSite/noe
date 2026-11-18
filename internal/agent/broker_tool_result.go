package agent

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"net/http"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

type toolResultRequest struct {
	RunID                string              `json:"runId"`
	ModelStepID          string              `json:"modelStepId"`
	ToolCallID           string              `json:"toolCallId"`
	ToolName             string              `json:"toolName"`
	RawArguments         jsonv1.RawMessage   `json:"rawArguments"`
	Arguments            jsonv1.RawMessage   `json:"arguments"`
	RawResult            jsonv1.RawMessage   `json:"rawResult"`
	Result               jsonv1.RawMessage   `json:"result,omitzero"`
	UseRawResult         bool                `json:"useRawResult,omitzero"`
	ModelResultAvailable *bool               `json:"modelResultAvailable,omitempty"`
	RawImages            []imageapi.Metadata `json:"rawImages,omitempty"`
	Images               []imageapi.Metadata `json:"images,omitempty"`
	HookError            string              `json:"hookError,omitempty"`
}

func (h *Host) recordToolResult(op *Operation, request toolResultRequest) (map[string]any, error) {
	modelResultAvailable := request.ModelResultAvailable == nil || *request.ModelResultAvailable
	if request.RunID == "" || request.ModelStepID == "" || request.ToolCallID == "" || strings.TrimSpace(request.ToolName) == "" ||
		len(request.RawArguments) == 0 || len(request.Arguments) == 0 || !validJSON(request.RawArguments) || !validJSON(request.Arguments) ||
		!validJSON(request.RawResult) ||
		(request.UseRawResult && (len(request.Result) != 0 || !modelResultAvailable)) ||
		(!request.UseRawResult && !validJSON(request.Result)) {
		return nil, newOperationError(http.StatusBadRequest, "request", "invalid tool result report")
	}
	result := request.Result
	if request.UseRawResult {
		result = request.RawResult
	}

	h.mu.Lock()
	run := h.runs[request.RunID]
	if run == nil || run.operation != op || run.ctx.Err() != nil || run.interrupting || op.closed || h.closed ||
		run.modelStep == nil || run.modelStep.id != request.ModelStepID || !run.modelStep.responseComplete || run.modelStep.finished {
		h.mu.Unlock()
		return nil, newOperationError(http.StatusConflict, "model_step", "tool result does not match an active model step")
	}
	toolState := run.modelStep.toolCalls[request.ToolCallID]
	if toolState == nil || toolState.name != request.ToolName || toolState.resultRecorded {
		h.mu.Unlock()
		return nil, newOperationError(http.StatusConflict, "tool_call", "unknown or completed tool call")
	}
	modelResult := jsonv1.RawMessage("null")
	var hookResult jsonv1.RawMessage
	if modelResultAvailable {
		projected, truncated, err := projectToolOutput(result, request.RawResult, toolState.outputBytes)
		if err != nil {
			h.mu.Unlock()
			return nil, newOperationError(http.StatusInternalServerError, "tool_output", err.Error())
		}
		modelResult = projected
		if truncated && h.opts.Observe != nil && !request.UseRawResult && !sameToolOutput(result, request.RawResult) {
			hookResult = cloneRawJSON(result)
		}
	}
	if toolState.activityID == "" {
		activityID, err := randomID()
		if err != nil {
			h.mu.Unlock()
			return nil, newOperationError(http.StatusInternalServerError, "broker", "could not allocate tool activity")
		}
		toolState.activityID = activityID
	}
	selected, imageErr := selectedToolImages(toolState, request.RawImages, request.Images)
	if imageErr != nil {
		h.mu.Unlock()
		return nil, newOperationError(400, "image", imageErr.Error())
	}
	if request.HookError != "" || !modelResultAvailable {
		discardStepImages(run.modelStep)
	} else {
		toolState.images = selected
	}
	toolState.resultRecorded = true
	allRecorded := len(run.modelStep.toolCalls) != 0
	for _, call := range run.modelStep.toolCalls {
		if !call.resultRecorded {
			allRecorded = false
			break
		}
	}
	if allRecorded {
		run.modelStep.finished = true
	}
	activityID := toolState.activityID
	requestID := run.modelStep.requestID
	requestActivityID := run.modelStep.requestActivityID
	h.mu.Unlock()

	reply := map[string]any{"recorded": true, "result": modelResult}
	if h.opts.Observe == nil {
		return reply, nil
	}
	fields := events.Event{
		"modelStepId":  request.ModelStepID,
		"requestId":    requestID,
		"toolCallId":   request.ToolCallID,
		"toolName":     request.ToolName,
		"rawArguments": cloneRawJSON(request.RawArguments),
		"arguments":    cloneRawJSON(request.Arguments),
		"rawResult":    cloneRawJSON(request.RawResult),
		"rawImages":    request.RawImages, "images": imageMetadata(selected),
		"result":               cloneRawJSON(modelResult),
		"modelResultAvailable": modelResultAvailable,
	}
	if request.HookError != "" {
		fields["hookError"] = request.HookError
	}
	if hookResult != nil {
		fields["hookResult"] = hookResult
	}
	if requestID != "" {
		fields["requestId"] = requestID
	}
	causedBy := []string(nil)
	if requestActivityID != "" {
		causedBy = []string{requestActivityID}
	}
	origin := activityOrigin{
		Kind: "tool", RunID: run.id, AgentID: run.agentID(),
		ToolCallID: request.ToolCallID, ToolName: request.ToolName, ModelStepID: request.ModelStepID,
	}
	addActivityFields(fields, activityID, causedBy, origin)
	if facts := rawToolExecutionFacts(request.RawResult); facts != nil {
		fields["toolExecution"] = facts
	}
	h.observeRun("tool.finished", run.id, run, fields)
	return reply, nil
}

func cloneRawJSON(raw jsonv1.RawMessage) jsonv1.RawMessage {
	return append(jsonv1.RawMessage(nil), raw...)
}

func rawToolExecutionFacts(raw jsonv1.RawMessage) events.Event {
	var object struct {
		Reason   jsonv1.RawMessage `json:"reason"`
		ExitCode jsonv1.RawMessage `json:"exitCode"`
		Success  jsonv1.RawMessage `json:"success"`
	}
	if json.Unmarshal(raw, &object) != nil {
		return nil
	}
	facts := events.Event{}
	var reason string
	if json.Unmarshal(object.Reason, &reason) == nil && reason != "" {
		facts["reason"] = reason
		facts["rawStatus"] = reason
	}
	var exitCode int
	if json.Unmarshal(object.ExitCode, &exitCode) == nil {
		facts["exitCode"] = exitCode
	}
	var success bool
	if json.Unmarshal(object.Success, &success) == nil {
		facts["success"] = success
	} else if len(object.ExitCode) != 0 && reason != "" {
		facts["success"] = reason == "completed" && exitCode == 0
	}
	if len(facts) == 0 {
		return nil
	}
	return facts
}
