package provider

import (
	"encoding/json"
	"strings"
)

// streamObservations extracts only user-readable text from choice zero. In a
// chunk that contains both top-level reasoning and reasoning.text details, an
// exact match of the concatenated detail text is treated as a mirrored copy
// and emitted once; differing detail text is retained as its own fragment.
// Encrypted reasoning data and signatures are never surfaced here.
func streamObservations(raw json.RawMessage) []StreamEvent {
	var chunk struct {
		Choices []struct {
			Index int `json:"index"`
			Delta struct {
				Reasoning        json.RawMessage `json:"reasoning"`
				ReasoningContent json.RawMessage `json:"reasoning_content"`
				ReasoningDetails json.RawMessage `json:"reasoning_details"`
				Content          json.RawMessage `json:"content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return nil
	}

	var observations []StreamEvent
	for _, choice := range chunk.Choices {
		if choice.Index != 0 {
			continue
		}
		reasoning, hasReasoning := streamText(choice.Delta.Reasoning)
		if !hasReasoning || reasoning == "" {
			reasoning, hasReasoning = streamText(choice.Delta.ReasoningContent)
		}
		type detailObservation struct {
			event StreamEvent
			text  bool
		}
		var details []detailObservation
		var detailText strings.Builder
		var rawDetails []map[string]json.RawMessage
		if len(choice.Delta.ReasoningDetails) > 0 && json.Unmarshal(choice.Delta.ReasoningDetails, &rawDetails) == nil {
			for _, fields := range rawDetails {
				var kind string
				if json.Unmarshal(fields["type"], &kind) != nil {
					continue
				}
				switch kind {
				case "reasoning.text":
					if text, ok := streamText(fields["text"]); ok && text != "" {
						details = append(details, detailObservation{event: StreamEvent{Type: "thinking", Text: text}, text: true})
						detailText.WriteString(text)
					}
				case "reasoning.summary":
					if text, ok := streamText(fields["summary"]); ok && text != "" {
						details = append(details, detailObservation{event: StreamEvent{Type: "thinking_summary", Text: text}})
					}
				}
			}
		}

		if hasReasoning && reasoning != "" {
			observations = append(observations, StreamEvent{Type: "thinking", Text: reasoning})
		}
		mirrored := hasReasoning && detailText.Len() > 0 && detailText.String() == reasoning
		for _, detail := range details {
			if detail.text && mirrored {
				continue
			}
			observations = append(observations, detail.event)
		}
		if text, ok := streamText(choice.Delta.Content); ok && text != "" {
			observations = append(observations, StreamEvent{Type: "message_delta", Text: text})
		}
	}
	return observations
}

func streamObservationsForField(raw json.RawMessage, field string) []StreamEvent {
	if field == "reasoning" || field == "reasoning_content" {
		return streamObservations(raw)
	}
	var chunk map[string]json.RawMessage
	if json.Unmarshal(raw, &chunk) != nil {
		return nil
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(chunk["choices"], &choices) != nil {
		return nil
	}
	for _, choice := range choices {
		var delta map[string]json.RawMessage
		if json.Unmarshal(choice["delta"], &delta) != nil {
			continue
		}
		if value, ok := delta[field]; ok {
			delta["reasoning"] = value
			choice["delta"], _ = json.Marshal(delta)
		}
	}
	chunk["choices"], _ = json.Marshal(choices)
	encoded, _ := json.Marshal(chunk)
	return streamObservations(encoded)
}

func streamText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return "", false
	}
	return text, true
}
