package provider

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"strings"
)

type streamObservationDelta struct {
	Reasoning        jsonv1.RawMessage `json:"reasoning"`
	ReasoningContent jsonv1.RawMessage `json:"reasoning_content"`
	ReasoningDetails jsonv1.RawMessage `json:"reasoning_details"`
	Content          jsonv1.RawMessage `json:"content"`
}

type streamObservationChoice struct {
	Index int                    `json:"index"`
	Delta streamObservationDelta `json:"delta"`
}

// streamObservations extracts only user-readable text from choice zero. In a
// chunk that contains both top-level reasoning and reasoning.text details, an
// exact match of the concatenated detail text is treated as a mirrored copy
// and emitted once; differing detail text is retained as its own fragment.
// Encrypted reasoning data and signatures are never surfaced here.
func streamObservations(raw jsonv1.RawMessage) []StreamEvent {
	return streamObservationsForField(raw, "")
}

// An explicit field selects the same reasoning text as response assembly.
// Only callers without a configured field use the historical alias fallback.
func streamObservationsForField(raw jsonv1.RawMessage, field string) []StreamEvent {
	var chunk struct {
		Choices []streamObservationChoice `json:"choices"`
	}
	if field == "" || field == "reasoning" || field == "reasoning_content" {
		if err := json.Unmarshal(raw, &chunk); err != nil {
			return nil
		}
	} else {
		var custom struct {
			Choices []struct {
				Index int                          `json:"index"`
				Delta map[string]jsonv1.RawMessage `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(raw, &custom); err != nil {
			return nil
		}
		for _, choice := range custom.Choices {
			chunk.Choices = append(chunk.Choices, streamObservationChoice{Index: choice.Index, Delta: streamObservationDelta{
				Reasoning: choice.Delta[field], ReasoningDetails: choice.Delta["reasoning_details"], Content: choice.Delta["content"],
			}})
		}
		field = "reasoning"
	}

	var observations []StreamEvent
	for _, choice := range chunk.Choices {
		if choice.Index != 0 {
			continue
		}
		reasoning, hasReasoning := streamText(choice.Delta.Reasoning)
		if field == "reasoning_content" || (field == "" && (!hasReasoning || reasoning == "")) {
			reasoning, hasReasoning = streamText(choice.Delta.ReasoningContent)
		}
		type detailObservation struct {
			event StreamEvent
			text  bool
		}
		var details []detailObservation
		var detailText strings.Builder
		var rawDetails []map[string]jsonv1.RawMessage
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

func streamText(raw jsonv1.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return "", false
	}
	return text, true
}
