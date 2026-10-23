package agent

import (
	"encoding/json"
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
	RawArguments         json.RawMessage     `json:"rawArguments"`
	Arguments            json.RawMessage     `json:"arguments"`
	RawResult            json.RawMessage     `json:"rawResult"`
	Result               json.RawMessage     `json:"result"`
	ModelResultAvailable *bool               `json:"modelResultAvailable,omitempty"`
	RawImages            []imageapi.Metadata `json:"rawImages,omitempty"`
	Images               []imageapi.Metadata `json:"images,omitempty"`
	HookError            string              `json:"hookError,omitempty"`
}

func (h *Host) recordToolResult(op *Operation, request toolResultRequest) (map[string]any, error) {
	if request.RunID == "" || request.ModelStepID == "" || request.ToolCallID == "" || strings.TrimSpace(request.ToolName) == "" ||
		len(request.RawArguments) == 0 || len(request.Arguments) == 0 || !json.Valid(request.RawArguments) || !json.Valid(request.Arguments) ||
		len(request.RawResult) == 0 || !json.Valid(request.RawResult) || len(request.Result) == 0 || !json.Valid(request.Result) {
		return nil, newOperationError(http.StatusBadRequest, "request", "invalid tool result report")
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
	modelResultAvailable := request.ModelResultAvailable == nil || *request.ModelResultAvailable
	modelResult := json.RawMessage("null")
	var hookResult json.RawMessage
	if modelResultAvailable {
		projected, truncated, err := projectToolOutput(request.Result, request.RawResult, toolState.outputBytes)
		if err != nil {
			h.mu.Unlock()
			return nil, newOperationError(http.StatusInternalServerError, "tool_output", err.Error())
		}
		modelResult = projected
		if truncated && !sameToolOutput(request.Result, request.RawResult) {
			hookResult = cloneRawJSON(request.Result)
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
	if request.HookError != "" || (request.ModelResultAvailable != nil && !*request.ModelResultAvailable) {
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
	return map[string]any{"recorded": true, "result": modelResult}, nil
}

func cloneRawJSON(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}

func rawToolExecutionFacts(raw json.RawMessage) events.Event {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil
	}
	facts := events.Event{}
	var reason string
	if json.Unmarshal(object["reason"], &reason) == nil && reason != "" {
		facts["reason"] = reason
		facts["rawStatus"] = reason
	}
	var exitCode int
	if json.Unmarshal(object["exitCode"], &exitCode) == nil {
		facts["exitCode"] = exitCode
	}
	var success bool
	if json.Unmarshal(object["success"], &success) == nil {
		facts["success"] = success
	} else if len(object["exitCode"]) != 0 && reason != "" {
		facts["success"] = reason == "completed" && exitCode == 0
	}
	if len(facts) == 0 {
		return nil
	}
	return facts
}
