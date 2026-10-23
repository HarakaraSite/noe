package loganalysis

import (
	"encoding/json"
	"math"
	"strconv"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func projectEvent(event events.Event, q Query) events.Event {
	typeName := stringField(event, "type")
	switch typeName {
	case "agent.started", "agent.completed", "agent.failed", "agent.cancelled",
		"agent.message", "agent.final", "model.started", "model.finished", "model.failed",
		"run_js.started", "decision.started", "decision.completed", "decision.failed", "decision.cancelled", "image.read", "image.started", "image.completed", "image.failed", "image.cancelled", "image.saved", "image.save_failed":
		return cloneEvent(event)
	case "run_js.finished":
		projected := cloneEvent(event)
		if !q.IncludeOutput {
			delete(projected, "data")
		}
		return projected
	case "tool.finished":
		projected := cloneEvent(event)
		if !q.IncludeOutput {
			// Results may be arbitrary JSON after an afterTool hook. Preserve
			// execution facts and IDs, and opt in to full raw/effective payloads.
			delete(projected, "rawResult")
			delete(projected, "result")
		}
		return projected
	case "model.step.started", "context.updated", "context.trimmed", "hook.started", "hook.finished", "hook.failed", "hook.cancelled", "execution.limit_reached", "execution.diagnostic":
		return cloneEvent(event)
	case "thinking", "thinking_summary":
		if q.IncludeThinking {
			return cloneEvent(event)
		}
	case "stdout", "stderr":
		if q.IncludeOutput {
			return cloneEvent(event)
		}
	}
	return nil
}

func appendProjected(eventsOut *[]events.Event, event events.Event, seq uint64, previousSeq *uint64) {
	if len(*eventsOut) > 0 && seq == *previousSeq+1 && isThinkingType(stringField(event, "type")) {
		previous := (*eventsOut)[len(*eventsOut)-1]
		if stringField(previous, "type") == stringField(event, "type") &&
			stringField(previous, "requestId") == stringField(event, "requestId") {
			previous["text"] = stringField(previous, "text") + stringField(event, "text")
			*previousSeq = seq
			return
		}
	}
	*eventsOut = append(*eventsOut, event)
	*previousSeq = seq
}

func cloneEvent(event events.Event) events.Event {
	copy := make(events.Event, len(event))
	for key, value := range event {
		copy[key] = value
	}
	return copy
}

func isThinkingType(typeName string) bool {
	return typeName == "thinking" || typeName == "thinking_summary"
}

func stringField(event events.Event, key string) string {
	value, _ := event[key].(string)
	return value
}

func boolField(event events.Event, key string) bool {
	value, _ := event[key].(bool)
	return value
}

func uintField(value any) (uint64, bool) {
	if number, ok := value.(json.Number); ok {
		parsed, err := strconv.ParseUint(number.String(), 10, 64)
		return parsed, err == nil
	}
	switch number := value.(type) {
	case uint64:
		return number, true
	case uint:
		return uint64(number), true
	case int64:
		if number >= 0 {
			return uint64(number), true
		}
	case int:
		if number >= 0 {
			return uint64(number), true
		}
	case float64:
		if number >= 0 && number <= math.MaxUint64 && math.Trunc(number) == number {
			return uint64(number), true
		}
	}
	return 0, false
}

func int64Field(value any) (int64, bool) {
	if number, ok := value.(json.Number); ok {
		parsed, err := strconv.ParseInt(number.String(), 10, 64)
		return parsed, err == nil
	}
	switch number := value.(type) {
	case int64:
		return number, true
	case int:
		return int64(number), true
	case uint64:
		if number <= math.MaxInt64 {
			return int64(number), true
		}
	case float64:
		if number >= math.MinInt64 && number <= math.MaxInt64 && math.Trunc(number) == number {
			return int64(number), true
		}
	}
	return 0, false
}

func intField(value any) int {
	number, ok := int64Field(value)
	if !ok || int64(int(number)) != number {
		return 0
	}
	return int(number)
}

func intField64(value any) int64 {
	number, _ := int64Field(value)
	return number
}

func float64Field(value any) (float64, bool) {
	if number, ok := value.(json.Number); ok {
		parsed, err := strconv.ParseFloat(number.String(), 64)
		return parsed, err == nil
	}
	if number, ok := value.(float64); ok {
		return number, true
	}
	if number, ok := value.(int64); ok {
		return float64(number), true
	}
	if number, ok := value.(int); ok {
		return float64(number), true
	}
	return 0, false
}
