package provider

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/models/operations"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/OpenRouterTeam/go-sdk/retry"
)

const nativeResponseItems = "_noe_response_items"

func (p *OpenRouter) responsesRequestBody(request Request) ([]byte, error) {
	return configuredResponsesRequestBody(p.profile, request)
}

func configuredResponsesRequestBody(profile Definition, request Request) ([]byte, error) {
	if _, err := makeChatRequestBody(request); err != nil {
		return nil, err
	}
	body, err := parameterObject(profile.Parameters)
	if err != nil {
		return nil, err
	}
	var input []json.RawMessage
	for _, raw := range request.Messages {
		var message map[string]json.RawMessage
		if err := json.Unmarshal(raw, &message); err != nil {
			return nil, err
		}
		var role string
		json.Unmarshal(message["role"], &role)
		switch role {
		case "assistant":
			if items, ok := message[nativeResponseItems]; ok {
				var native []json.RawMessage
				if err := json.Unmarshal(items, &native); err != nil {
					return nil, err
				}
				input = append(input, native...)
				continue
			}
		case "tool":
			item := map[string]any{"type": "function_call_output", "call_id": message["tool_call_id"], "output": message["content"]}
			encoded, err := json.Marshal(item)
			if err != nil {
				return nil, err
			}
			input = append(input, encoded)
			continue
		case "user", "system", "developer":
		default:
			return nil, errors.New("unsupported conversation role")
		}
		content := message["content"]
		if role == "user" {
			var parts []map[string]json.RawMessage
			if json.Unmarshal(content, &parts) == nil {
				for index, part := range parts {
					var kind string
					json.Unmarshal(part["type"], &kind)
					switch kind {
					case "text":
						part["type"] = json.RawMessage(`"input_text"`)
					case "image_url":
						var image struct {
							URL    string `json:"url"`
							Detail string `json:"detail"`
						}
						if json.Unmarshal(part["image_url"], &image) != nil || image.URL == "" {
							return nil, errors.New("invalid image content")
						}
						raw, _ := json.Marshal(map[string]string{"type": "input_image", "image_url": image.URL, "detail": image.Detail})
						parts[index] = nil
						json.Unmarshal(raw, &parts[index])
					}
				}
				content, _ = json.Marshal(parts)
			}
		}
		encoded, err := json.Marshal(map[string]any{"role": role, "content": content})
		if err != nil {
			return nil, err
		}
		input = append(input, encoded)
	}
	tools := make([]map[string]json.RawMessage, 0, len(request.Tools))
	for _, raw := range request.Tools {
		var tool struct {
			Type     string                     `json:"type"`
			Function map[string]json.RawMessage `json:"function"`
		}
		if json.Unmarshal(raw, &tool) != nil || tool.Type != "function" || tool.Function == nil {
			return nil, errors.New("unsupported Responses tool")
		}
		tool.Function["type"] = json.RawMessage(`"function"`)
		tools = append(tools, tool.Function)
	}
	body["model"], body["input"], body["tools"], body["stream"], body["store"] = request.Model, input, tools, true, false
	// Stateless continuation needs the opaque reasoning content, not just IDs.
	if _, configured := body["include"]; !configured {
		body["include"] = []string{"reasoning.encrypted_content"}
	}
	if request.MaxTokens > 0 {
		body["max_output_tokens"] = request.MaxTokens
	}
	if profile.SessionField != "" && request.CacheKey != "" {
		putField(body, profile.SessionField, request.CacheKey)
	}
	effort, _ := NormalizeEffort(request.Effort)
	if effort != "auto" {
		if profile.EffortField == "" {
			return nil, errors.New("provider has no effort mapping")
		}
		putField(body, profile.EffortField, effort)
	}
	return json.Marshal(body)
}

func (p *OpenRouter) generateResponses(ctx context.Context, request Request) (Response, error) {
	payload, err := p.responsesRequestBody(request)
	if err != nil {
		return Response{}, providerError(ErrorResponse, 0, nil)
	}
	state := &callState{requestBody: payload, observe: request.Observe, sessionID: request.sessionID(), responseAPI: "responses"}
	ctx = context.WithValue(ctx, callContextKey{}, state)
	defer state.closeBody()
	model := request.Model
	stream := true
	tier := components.ResponsesRequestServiceTierAuto
	result, err := p.client.Responses.Send(ctx, components.ResponsesRequest{Model: &model, Stream: &stream, ServiceTier: optionalnullable.From(&tier)}, nil,
		operations.WithRetries(retry.Config{Strategy: "none"}), operations.WithAcceptHeaderOverride(operations.AcceptHeaderEnumTextEventStream))
	if cancelErr := cancellationError(ctx); cancelErr != nil {
		return Response{}, cancelErr
	}
	if err != nil {
		status, _ := state.response()
		return Response{}, classifyHTTPError(status, err)
	}
	if result == nil || result.EventStream == nil {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	defer result.EventStream.Close()
	completed := false
	for result.EventStream.Next() {
		if event := result.EventStream.Value(); event != nil {
			if event.GetDataResponseFailed() != nil || event.GetDataResponseIncomplete() != nil {
				return Response{}, nativeStreamFailure(state)
			}
			if event.GetDataResponseCompleted() != nil {
				completed = true
				break
			}
		}
	}
	if cancelErr := cancellationError(ctx); cancelErr != nil {
		return Response{}, cancelErr
	}
	if result.EventStream.Err() != nil || !completed {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	_, body := state.response()
	if body == nil {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	events, _, readErr := body.snapshot()
	if readErr != nil {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	return responseFromEvents(events)
}

// Completed streams may put the full items only in output_item.done while the
// terminal response.output is empty (observed on ChatGPT plan Responses).
// Retain whole raw items, including encrypted reasoning and unknown fields.
func responseFromEvents(events []json.RawMessage) (Response, error) {
	items := make(map[int]json.RawMessage)
	for _, raw := range events {
		var event struct {
			Type        string          `json:"type"`
			Response    json.RawMessage `json:"response"`
			Item        json.RawMessage `json:"item"`
			OutputIndex int             `json:"output_index"`
		}
		if json.Unmarshal(raw, &event) != nil {
			return Response{}, providerError(ErrorResponse, 0, nil)
		}
		if event.Type == "response.output_item.done" {
			if !isJSONObject(event.Item) {
				return Response{}, providerError(ErrorResponse, 0, nil)
			}
			items[event.OutputIndex] = event.Item
		}
		if event.Type == "response.completed" {
			var envelope map[string]json.RawMessage
			if json.Unmarshal(event.Response, &envelope) != nil {
				return Response{}, providerError(ErrorResponse, 0, nil)
			}
			var output []json.RawMessage
			if raw := envelope["output"]; len(raw) > 0 && json.Unmarshal(raw, &output) != nil {
				return Response{}, providerError(ErrorResponse, 0, nil)
			}
			for index, item := range output {
				items[index] = item
			}
			indices := make([]int, 0, len(items))
			for index := range items {
				indices = append(indices, index)
			}
			sort.Ints(indices)
			output = make([]json.RawMessage, 0, len(items))
			for _, index := range indices {
				output = append(output, items[index])
			}
			envelope["output"], _ = json.Marshal(output)
			assembled, err := json.Marshal(envelope)
			if err != nil {
				return Response{}, err
			}
			return responseFromNative(assembled)
		}
	}
	return Response{}, providerError(ErrorStream, 0, nil)
}

func responseFromNative(raw json.RawMessage) (Response, error) {
	var response struct {
		Status string            `json:"status"`
		Output []json.RawMessage `json:"output"`
		Usage  json.RawMessage   `json:"usage"`
		Error  json.RawMessage   `json:"error"`
	}
	if json.Unmarshal(raw, &response) != nil || response.Status != "completed" || (len(response.Error) > 0 && string(response.Error) != "null") {
		return Response{}, providerError(ErrorResponse, 0, nil)
	}
	var content strings.Builder
	var calls []map[string]any
	var refusal strings.Builder
	for _, raw := range response.Output {
		var item struct {
			Type      string `json:"type"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Content   []struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Refusal string `json:"refusal"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &item) != nil {
			return Response{}, providerError(ErrorResponse, 0, nil)
		}
		if item.Type == "function_call" {
			if item.CallID == "" || item.Name == "" {
				return Response{}, providerError(ErrorResponse, 0, nil)
			}
			calls = append(calls, map[string]any{"id": item.CallID, "type": "function", "function": map[string]any{"name": item.Name, "arguments": item.Arguments}})
		}
		for _, part := range item.Content {
			if part.Type == "output_text" {
				content.WriteString(part.Text)
			}
			if part.Type == "refusal" {
				refusal.WriteString(part.Refusal)
			}
		}
	}
	message := map[string]any{"role": "assistant", "content": content.String(), nativeResponseItems: response.Output}
	finish := "stop"
	if len(calls) > 0 {
		message["tool_calls"] = calls
		finish = "tool_calls"
	}
	if refusal.Len() > 0 {
		message["refusal"] = refusal.String()
	}
	encoded, err := json.Marshal(message)
	if err != nil {
		return Response{}, err
	}
	return Response{Message: encoded, FinishReason: finish, Usage: response.Usage}, nil
}

func responsesObservations(raw json.RawMessage) []StreamEvent {
	var event struct {
		Type  string `json:"type"`
		Delta string `json:"delta"`
	}
	if json.Unmarshal(raw, &event) != nil || event.Delta == "" {
		return nil
	}
	kind := ""
	switch event.Type {
	case "response.output_text.delta":
		kind = "message_delta"
	case "response.reasoning_summary_text.delta":
		kind = "thinking_summary"
	case "response.reasoning_text.delta":
		kind = "thinking"
	}
	if kind == "" {
		return nil
	}
	return []StreamEvent{{Type: kind, Text: event.Delta}}
}
