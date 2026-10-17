package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type streamAccumulator struct {
	reasoningField string
	contentSeen    bool
	contentText    bool
	content        strings.Builder
	reasoning      strings.Builder
	reasoningSet   bool
	refusal        strings.Builder
	refusalSet     bool
	role           json.RawMessage
	other          map[string]json.RawMessage
	reasonDetails  []*reasoningDetailAccumulator
	reasonByKey    map[string]*reasoningDetailAccumulator
	toolCalls      map[int]*toolCallAccumulator
	finishReason   string
	usage          json.RawMessage
}

type toolCallAccumulator struct {
	fields       map[string]json.RawMessage
	function     map[string]json.RawMessage
	arguments    strings.Builder
	argumentsSet bool
	name         strings.Builder
	nameSet      bool
}

type reasoningDetailAccumulator struct {
	fields    map[string]json.RawMessage
	fragments map[string]*strings.Builder
}

func newStreamAccumulator() *streamAccumulator {
	return &streamAccumulator{
		other:       make(map[string]json.RawMessage),
		reasonByKey: make(map[string]*reasoningDetailAccumulator),
		toolCalls:   make(map[int]*toolCallAccumulator),
	}
}

func (a *streamAccumulator) consume(events []json.RawMessage) error {
	for _, raw := range events {
		var chunk struct {
			Choices []struct {
				Index        int             `json:"index"`
				Delta        json.RawMessage `json:"delta"`
				FinishReason json.RawMessage `json:"finish_reason"`
			} `json:"choices"`
			Usage json.RawMessage `json:"usage"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(raw, &chunk); err != nil {
			return fmt.Errorf("invalid SSE event")
		}
		if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
			return errors.New("provider returned an SSE error event")
		}
		if len(chunk.Usage) > 0 && string(chunk.Usage) != "null" {
			a.usage = append(a.usage[:0], chunk.Usage...)
		}
		for _, choice := range chunk.Choices {
			// This adapter requests one completion and therefore retains choice 0.
			if choice.Index != 0 {
				continue
			}
			if len(choice.FinishReason) > 0 && string(choice.FinishReason) != "null" {
				if err := json.Unmarshal(choice.FinishReason, &a.finishReason); err != nil {
					return fmt.Errorf("invalid finish reason")
				}
			}
			if len(choice.Delta) == 0 || string(choice.Delta) == "null" {
				continue
			}
			var delta map[string]json.RawMessage
			if err := json.Unmarshal(choice.Delta, &delta); err != nil {
				return fmt.Errorf("invalid assistant delta")
			}
			for key, value := range delta {
				if a.reasoningField != "" && key == a.reasoningField {
					key = "reasoning"
				} else if a.reasoningField != "" && (key == "reasoning" || key == "reasoning_content") {
					a.other[key] = append(json.RawMessage(nil), value...)
					continue
				}
				switch key {
				case "role":
					a.role = append(a.role[:0], value...)
				case "content":
					a.contentSeen = true
					if string(value) == "null" {
						continue
					}
					var part string
					if err := json.Unmarshal(value, &part); err != nil {
						return fmt.Errorf("invalid assistant content")
					}
					a.contentText = true
					a.content.WriteString(part)
				case "reasoning", "reasoning_content":
					a.reasoningSet = true
					var part string
					if string(value) != "null" {
						if err := json.Unmarshal(value, &part); err != nil {
							return fmt.Errorf("invalid reasoning content")
						}
					}
					a.reasoning.WriteString(part)
				case "refusal":
					a.refusalSet = true
					var part string
					if string(value) != "null" {
						if err := json.Unmarshal(value, &part); err != nil {
							return fmt.Errorf("invalid refusal content")
						}
					}
					a.refusal.WriteString(part)
				case "reasoning_details":
					if err := a.consumeReasoningDetails(value); err != nil {
						return err
					}
				case "tool_calls":
					if err := a.consumeToolCalls(value); err != nil {
						return err
					}
				default:
					a.other[key] = append(json.RawMessage(nil), value...)
				}
			}
		}
	}
	return nil
}

func (a *streamAccumulator) consumeReasoningDetails(raw json.RawMessage) error {
	var details []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &details); err != nil {
		return fmt.Errorf("invalid reasoning details")
	}
	for _, fields := range details {
		key := reasoningDetailKey(fields)
		detail := a.reasonByKey[key]
		if key == "" || detail == nil {
			detail = &reasoningDetailAccumulator{fields: make(map[string]json.RawMessage), fragments: make(map[string]*strings.Builder)}
			a.reasonDetails = append(a.reasonDetails, detail)
			if key != "" {
				a.reasonByKey[key] = detail
			}
		}
		for field, value := range fields {
			switch field {
			case "text", "summary", "data", "signature":
				var fragment string
				if string(value) == "null" {
					detail.fields[field] = append(json.RawMessage(nil), value...)
					continue
				}
				if err := json.Unmarshal(value, &fragment); err == nil {
					builder := detail.fragments[field]
					if builder == nil {
						builder = &strings.Builder{}
						detail.fragments[field] = builder
					}
					builder.WriteString(fragment)
				} else {
					detail.fields[field] = append(json.RawMessage(nil), value...)
				}
			default:
				detail.fields[field] = append(json.RawMessage(nil), value...)
			}
		}
	}
	return nil
}

func reasoningDetailKey(fields map[string]json.RawMessage) string {
	if rawIndex := fields["index"]; len(rawIndex) > 0 {
		var index int64
		if json.Unmarshal(rawIndex, &index) == nil {
			return "index:" + strconv.FormatInt(index, 10)
		}
	}
	if rawID := fields["id"]; len(rawID) > 0 {
		var id string
		if json.Unmarshal(rawID, &id) == nil && id != "" {
			return "id:" + id
		}
	}
	return ""
}

func (a *streamAccumulator) consumeToolCalls(raw json.RawMessage) error {
	var calls []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &calls); err != nil {
		return fmt.Errorf("invalid tool call delta")
	}
	for _, part := range calls {
		index := 0
		if value := part["index"]; len(value) > 0 {
			if err := json.Unmarshal(value, &index); err != nil {
				return fmt.Errorf("invalid tool call index")
			}
		}
		call := a.toolCalls[index]
		if call == nil {
			call = &toolCallAccumulator{fields: make(map[string]json.RawMessage), function: make(map[string]json.RawMessage)}
			a.toolCalls[index] = call
		}
		for key, value := range part {
			switch key {
			case "index":
			case "function":
				var function map[string]json.RawMessage
				if err := json.Unmarshal(value, &function); err != nil {
					return fmt.Errorf("invalid tool function delta")
				}
				for functionKey, functionValue := range function {
					switch functionKey {
					case "arguments":
						var fragment string
						if err := json.Unmarshal(functionValue, &fragment); err != nil {
							return fmt.Errorf("invalid tool arguments delta")
						}
						call.arguments.WriteString(fragment)
						call.argumentsSet = true
					case "name":
						var fragment string
						if err := json.Unmarshal(functionValue, &fragment); err != nil {
							return fmt.Errorf("invalid tool name delta")
						}
						call.name.WriteString(fragment)
						call.nameSet = true
					default:
						call.function[functionKey] = append(json.RawMessage(nil), functionValue...)
					}
				}
			default:
				call.fields[key] = append(json.RawMessage(nil), value...)
			}
		}
	}
	return nil
}

func (a *streamAccumulator) response() (Response, error) {
	if a.finishReason == "" {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	if a.finishReason != "stop" && a.finishReason != "tool_calls" {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	if a.refusal.Len() > 0 {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	if a.finishReason == "tool_calls" && len(a.toolCalls) == 0 {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	if a.finishReason == "stop" && !a.contentSeen && len(a.toolCalls) == 0 {
		return Response{}, providerError(ErrorStream, 0, nil)
	}

	message := make(map[string]json.RawMessage, len(a.other)+6)
	message["role"] = json.RawMessage(`"assistant"`)
	if len(a.role) > 0 {
		message["role"] = a.role
	}
	if a.contentSeen {
		if !a.contentText {
			message["content"] = json.RawMessage("null")
		} else {
			encoded, _ := json.Marshal(a.content.String())
			message["content"] = encoded
		}
	} else if len(a.toolCalls) > 0 {
		message["content"] = json.RawMessage("null")
	}
	if a.reasoningSet {
		encoded, _ := json.Marshal(a.reasoning.String())
		field := a.reasoningField
		if field == "" {
			field = "reasoning"
		}
		message[field] = encoded
	}
	if a.refusalSet {
		encoded, _ := json.Marshal(a.refusal.String())
		message["refusal"] = encoded
	}
	if len(a.reasonDetails) > 0 {
		details := make([]json.RawMessage, 0, len(a.reasonDetails))
		for _, detail := range a.reasonDetails {
			fields := make(map[string]json.RawMessage, len(detail.fields)+len(detail.fragments))
			for key, value := range detail.fields {
				fields[key] = value
			}
			for key, fragment := range detail.fragments {
				value, _ := json.Marshal(fragment.String())
				fields[key] = value
			}
			encodedDetail, err := json.Marshal(fields)
			if err != nil {
				return Response{}, providerError(ErrorResponse, 0, nil)
			}
			details = append(details, encodedDetail)
		}
		encoded, err := json.Marshal(details)
		if err != nil {
			return Response{}, providerError(ErrorResponse, 0, nil)
		}
		message["reasoning_details"] = encoded
	}
	if len(a.toolCalls) > 0 {
		indices := make([]int, 0, len(a.toolCalls))
		for index := range a.toolCalls {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		calls := make([]json.RawMessage, 0, len(indices))
		for _, index := range indices {
			call := a.toolCalls[index]
			fields := make(map[string]json.RawMessage, len(call.fields)+1)
			for key, value := range call.fields {
				fields[key] = value
			}
			function := make(map[string]json.RawMessage, len(call.function)+2)
			for key, value := range call.function {
				function[key] = value
			}
			if call.nameSet {
				value, _ := json.Marshal(call.name.String())
				function["name"] = value
			}
			if call.argumentsSet {
				value, _ := json.Marshal(call.arguments.String())
				function["arguments"] = value
			}
			functionJSON, err := json.Marshal(function)
			if err != nil {
				return Response{}, providerError(ErrorResponse, 0, nil)
			}
			fields["function"] = functionJSON
			value, err := json.Marshal(fields)
			if err != nil {
				return Response{}, providerError(ErrorResponse, 0, nil)
			}
			calls = append(calls, value)
		}
		value, err := json.Marshal(calls)
		if err != nil {
			return Response{}, providerError(ErrorResponse, 0, nil)
		}
		message["tool_calls"] = value
	}
	for key, value := range a.other {
		message[key] = value
	}
	messageJSON, err := json.Marshal(message)
	if err != nil {
		return Response{}, providerError(ErrorResponse, 0, nil)
	}
	return Response{Message: messageJSON, FinishReason: a.finishReason, Usage: append(json.RawMessage(nil), a.usage...)}, nil
}
