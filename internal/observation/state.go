package observation

import (
	"encoding/json"
	"strconv"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

type activeTool struct {
	seq       uint64
	toolCall  string
	action    string
	hasAction bool
}

type toolOutput struct {
	text strings.Builder
}

type runState struct {
	snapshot    Snapshot
	phase       string
	models      map[string]uint64
	tools       map[string]activeTool
	hooks       map[string]activeTool
	outputs     map[string]*toolOutput
	lastTool    activeTool
	lastToolSet bool
	usageSet    bool
}

func newRunState(runID string) *runState {
	return &runState{
		snapshot: Snapshot{
			RunID: runID, Status: "unknown", Phase: "unknown", Events: make([]events.Event, 0),
		},
		models: make(map[string]uint64), tools: make(map[string]activeTool),
		hooks:   make(map[string]activeTool),
		outputs: make(map[string]*toolOutput),
	}
}

func (s *runState) apply(rec record, includeOutput bool) {
	event := rec.event
	typeName := rec.typeName
	if value := stringField(event, "groupId"); value != "" {
		s.snapshot.GroupID = value
	}
	for key, target := range map[string]*json.RawMessage{"execution": &s.snapshot.Execution, "context": &s.snapshot.Context} {
		if value, ok := event[key]; ok {
			if encoded, err := json.Marshal(value); err == nil {
				*target = encoded
			}
		}
	}
	if value := stringField(event, "agentId"); value != "" {
		s.snapshot.AgentID = value
	}
	if timeText := stringField(event, "time"); timeText != "" {
		s.snapshot.UpdatedAt = timeText
	}
	if value := stringField(event, "parentRunId"); value != "" || typeName == "agent.started" {
		s.snapshot.ParentRunID = value
	}
	if value := stringField(event, "name"); value != "" {
		s.snapshot.Name = value
	}
	if value := stringField(event, "model"); value != "" {
		s.snapshot.Model = value
	}

	switch typeName {
	case "hook.started":
		s.hooks[stringField(event, "invocationId")] = activeTool{seq: rec.seq, action: stringField(event, "phase") + " " + stringField(event, "module"), hasAction: true}
	case "hook.finished", "hook.failed", "hook.cancelled":
		delete(s.hooks, stringField(event, "invocationId"))
		s.phase = "between_steps"
	case "agent.started":
		s.snapshot.Status = "running"
		s.phase = "starting"
	case "agent.completed":
		s.snapshot.Status = "completed"
	case "agent.failed":
		s.snapshot.Status = "failed"
		s.snapshot.Error = stringField(event, "error")
	case "agent.cancelled":
		s.snapshot.Status = "cancelled"
	case "agent.message":
		s.snapshot.LastMessage = stringField(event, "text")
	case "agent.final":
		s.snapshot.FinalText = stringField(event, "text")
	case "model.started":
		requestID := stringField(event, "requestId")
		s.models[requestID] = rec.seq
		s.phase = "between_steps"
	case "model.finished", "model.failed":
		delete(s.models, stringField(event, "requestId"))
		s.phase = "between_steps"
		if typeName == "model.finished" {
			s.applyUsage(event["usage"])
		}
	case "run_js.started":
		toolCallID := stringField(event, "toolCallId")
		key := toolKey(toolCallID, rec.seq)
		action, hasAction := event["text"].(string)
		active := activeTool{seq: rec.seq, toolCall: toolCallID, action: action, hasAction: hasAction}
		s.tools[key] = active
		s.lastTool, s.lastToolSet = active, true
		s.phase = "between_steps"
	case "stdout", "stderr":
		if includeOutput && stringField(event, "source") == "run_js" {
			toolCallID := stringField(event, "toolCallId")
			if toolCallID != "" {
				out := s.outputs[toolCallID]
				if out == nil {
					out = &toolOutput{}
					s.outputs[toolCallID] = out
				}
				out.text.WriteString(stringField(event, "text"))
			}
		}
	case "run_js.finished":
		toolCallID := stringField(event, "toolCallId")
		delete(s.tools, toolKey(toolCallID, 0))
		// A finished event may be the only record for a tool if its start was
		// outside the observed file prefix; remove any matching active entry.
		for key, active := range s.tools {
			if active.toolCall == toolCallID {
				delete(s.tools, key)
			}
		}
		result := &ToolResult{
			ToolCallID: toolCallID,
			Success:    boolField(event, "success"),
			ExitCode:   intField(event["exitCode"]),
			Reason:     stringField(event, "reason"),
			Duration:   intField64(event["duration"]),
		}
		if includeOutput {
			if output := s.outputs[toolCallID]; output != nil {
				result.Text = output.text.String()
			}
		}
		s.snapshot.LastToolResult = result
		s.phase = "between_steps"
	case "thinking", "thinking_summary", "message_delta":
		// Stream fragments do not change the run's phase.
	}
}

func (s *runState) applyUsage(raw any) {
	values, ok := raw.(map[string]any)
	if !ok {
		return
	}
	if s.snapshot.Usage == nil {
		s.snapshot.Usage = &Usage{}
	}
	usage := s.snapshot.Usage
	addInt := func(key string, target *int64) {
		if value, ok := int64Field(values[key]); ok {
			*target += value
			s.usageSet = true
		}
	}
	addInt("promptTokens", &usage.PromptTokens)
	addInt("completionTokens", &usage.CompletionTokens)
	addInt("totalTokens", &usage.TotalTokens)
	addInt("cachedTokens", &usage.CachedTokens)
	if value, ok := float64Field(values["cost"]); ok {
		usage.Cost += value
		s.usageSet = true
	}
	if !s.usageSet {
		s.snapshot.Usage = nil
	}
}

func (s *runState) currentPhase() string {
	if isTerminalStatus(s.snapshot.Status) {
		return "settled"
	}
	if len(s.hooks) > 0 {
		return "hook"
	}
	if len(s.tools) > 0 {
		return "run_js"
	}
	if len(s.models) > 0 {
		return "model"
	}
	if s.phase != "" {
		return s.phase
	}
	return "unknown"
}

func (s *runState) currentAction() string {
	if len(s.hooks) > 0 {
		var newest activeTool
		for _, active := range s.hooks {
			if active.seq > newest.seq {
				newest = active
			}
		}
		return newest.action
	}
	if len(s.tools) > 0 {
		var current activeTool
		found := false
		for _, active := range s.tools {
			if !found || active.seq > current.seq {
				current, found = active, true
			}
		}
		if found && current.hasAction {
			return current.action
		}
		return ""
	}
	if s.lastToolSet && s.lastTool.hasAction {
		return s.lastTool.action
	}
	return ""
}

func isTerminalStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}

func toolKey(toolCallID string, seq uint64) string {
	if toolCallID != "" {
		return toolCallID
	}
	return "#" + strconv.FormatUint(seq, 10)
}
