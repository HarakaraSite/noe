// Package main is an isolated Responses transport experiment, not a provider implementation.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type item = json.RawMessage
type body = map[string]any

type turn struct {
	Transport      string          `json:"transport"`
	RequestBytes   int             `json:"requestJsonBytes"`
	InputItems     int             `json:"inputItems"`
	Incremental    bool            `json:"incremental"`
	ChainDecision  string          `json:"chainDecision,omitempty"`
	FailureCode    string          `json:"failureCode,omitempty"`
	ErrorEvent     json.RawMessage `json:"errorEvent,omitempty"`
	DurationMS     float64         `json:"durationMs"`
	FirstEventMS   *float64        `json:"firstEventMs,omitempty"`
	FirstTextMS    *float64        `json:"firstTextMs,omitempty"`
	ResponseID     string          `json:"responseId,omitempty"`
	Model          string          `json:"model,omitempty"`
	ServiceTier    string          `json:"returnedServiceTier,omitempty"`
	Usage          json.RawMessage `json:"usage,omitempty"`
	EventCount     int             `json:"events"`
	OutputItems    int             `json:"outputItems"`
	ReasoningItems int             `json:"reasoningItems"`
	Text           string          `json:"text,omitempty"`
	Output         []item          `json:"-"`
}

type protocolError struct {
	Code    string `json:"code,omitempty"`
	Type    string `json:"type,omitempty"`
	Message string `json:"message,omitempty"`
	Status  int    `json:"status,omitempty"`
}

func (e *protocolError) Error() string { return fmt.Sprintf("Responses %s: %s", e.Code, e.Message) }

// collect uses the same raw-event assembly for SSE and WebSocket. A closed
// transport is not a completed response. Keep output_item.done as the fallback
// used by NOE when a terminal snapshot contains an empty output array.
func collect(start time.Time, result turn, next func() (item, error)) (assembled turn, err error) {
	defer func() { assembled.DurationMS = milliseconds(start) }()
	var completed []item
	for {
		data, err := next()
		if err != nil {
			result.DurationMS = milliseconds(start)
			return result, err
		}
		result.EventCount++
		if result.FirstEventMS == nil {
			elapsed := milliseconds(start)
			result.FirstEventMS = &elapsed
		}
		var event struct {
			Type     string         `json:"type"`
			Item     item           `json:"item"`
			Error    *protocolError `json:"error"`
			Status   int            `json:"status"`
			Response struct {
				ID          string         `json:"id"`
				Model       string         `json:"model"`
				Status      string         `json:"status"`
				ServiceTier string         `json:"service_tier"`
				Usage       item           `json:"usage"`
				Output      []item         `json:"output"`
				Error       *protocolError `json:"error"`
			} `json:"response"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			return result, err
		}
		switch event.Type {
		case "response.output_text.delta":
			if result.FirstTextMS == nil {
				elapsed := milliseconds(start)
				result.FirstTextMS = &elapsed
			}
		case "response.output_item.done":
			completed = append(completed, event.Item)
		case "error":
			result.ErrorEvent = append(item(nil), data...)
			if event.Error == nil {
				return result, errors.New("error event has no error")
			}
			event.Error.Status = event.Status
			return result, event.Error
		case "response.failed", "response.incomplete":
			if event.Response.Error != nil {
				return result, event.Response.Error
			}
			return result, fmt.Errorf("terminal event: %s", event.Type)
		case "response.completed":
			if event.Response.ID == "" {
				return result, errors.New("completed response has no ID")
			}
			result.ResponseID, result.Model = event.Response.ID, event.Response.Model
			result.ServiceTier, result.Usage = event.Response.ServiceTier, event.Response.Usage
			result.Output = event.Response.Output
			if len(result.Output) == 0 {
				result.Output = completed
			}
			result.OutputItems = len(result.Output)
			for _, raw := range result.Output {
				var value struct {
					Type    string `json:"type"`
					Content []struct {
						Text string `json:"text"`
					} `json:"content"`
				}
				if err := json.Unmarshal(raw, &value); err != nil {
					return result, err
				}
				if value.Type == "reasoning" {
					result.ReasoningItems++
				}
				if value.Type == "message" {
					for _, part := range value.Content {
						result.Text += part.Text
					}
				}
			}
			result.DurationMS = milliseconds(start)
			return result, nil
		}
	}
}

func milliseconds(start time.Time) float64 { return float64(time.Since(start).Microseconds()) / 1000 }

func encodedRequest(request body, websocket bool) ([]byte, turn, error) {
	copy := make(body, len(request)+1)
	for key, value := range request {
		copy[key] = value
	}
	if websocket {
		delete(copy, "stream")
		delete(copy, "background")
		copy["type"] = "response.create"
	} else {
		copy["stream"] = true
	}
	data, err := json.Marshal(copy)
	result := turn{Transport: "sse", RequestBytes: len(data)}
	if websocket {
		result.Transport = "websocket"
	}
	_, result.Incremental = copy["previous_response_id"]
	if input, ok := copy["input"].([]item); ok {
		result.InputItems = len(input)
	}
	return data, result, err
}

func sseTurn(ctx context.Context, client openai.Client, request body) (turn, error) {
	data, result, err := encodedRequest(request, false)
	if err != nil {
		return result, err
	}
	start := time.Now()
	stream := client.Responses.NewStreaming(ctx, responses.ResponseNewParams{}, option.WithRequestBody("application/json", data))
	defer stream.Close()
	return collect(start, result, func() (item, error) {
		if stream.Next() {
			return item(stream.Current().RawJSON()), nil
		}
		if err := stream.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("SSE ended before response.completed")
	})
}

func websocketTurn(ctx context.Context, connection *responses.ResponseConnection, request body) (turn, error) {
	data, result, err := encodedRequest(request, true)
	if err != nil {
		return result, err
	}
	start := time.Now()
	if err := connection.SendRaw(ctx, data); err != nil {
		result.DurationMS = milliseconds(start)
		return result, err
	}
	result, err = collect(start, result, func() (item, error) { return connection.RecvRaw(ctx) })
	if err != nil && ctx.Err() != nil {
		connection.Abort()
	}
	return result, err
}

func newClient(endpoint, session string, client *http.Client) openai.Client {
	opts := []option.RequestOption{option.WithBaseURL(endpoint), option.WithHTTPClient(client),
		option.WithAPIKey(""), option.WithAdminAPIKey(""), option.WithOrganization(""), option.WithProject(""),
		option.WithHeaderDel("OpenAI-Organization"), option.WithHeaderDel("OpenAI-Project"),
		option.WithHeader("User-Agent", "noe/websocket-spike"), option.WithMaxRetries(0)}
	if session != "" {
		opts = append(opts, option.WithHeader("session_id", session))
	}
	return openai.NewClient(opts...)
}

func openSocket(ctx context.Context, client openai.Client) (*responses.ResponseConnection, float64, error) {
	start := time.Now()
	connection, err := client.Responses.Connect(ctx, responses.ResponseConnectionOptions{})
	return connection, milliseconds(start), err
}

func inputMessage(role, text string) item {
	encoded, _ := json.Marshal(body{"role": role, "content": text})
	return encoded
}

func toolDefinition(kind string) body {
	if kind == "custom" {
		return body{"type": "custom", "name": "run_js", "description": "Return a synthetic probe value. Submit console.log(probe_value()) as JavaScript text.", "format": body{"type": "text"}}
	}
	return body{"type": "function", "name": "probe_value", "description": "Return a synthetic probe value.", "parameters": body{"type": "object", "properties": body{}, "required": []string{}, "additionalProperties": false}, "strict": true}
}

func requestBody(model, effort, tier, cacheKey, kind string, input []item, choice any) body {
	request := body{"model": model, "store": false, "input": input, "reasoning": body{"effort": effort, "summary": "auto"},
		"include": []string{"reasoning.encrypted_content"}, "tools": []body{toolDefinition(kind)}, "tool_choice": choice,
		"parallel_tool_calls": false, "prompt_cache_key": cacheKey, "max_output_tokens": 1024}
	if tier != "" {
		request["service_tier"] = tier
	}
	return request
}

func forcedTool(kind string) body {
	if kind == "custom" {
		return body{"type": "custom", "name": "run_js"}
	}
	return body{"type": "function", "name": "probe_value"}
}

// The fixture tool has no external side effect; never execute model-generated
// JavaScript in this transport-only spike. Its nonce becomes known to the model
// only in the tool output, making the final answer a continuation check.
func toolOutput(first turn, kind, value string) (item, error) {
	wanted, outputType := "function_call", "function_call_output"
	if kind == "custom" {
		wanted, outputType = "custom_tool_call", "custom_tool_call_output"
	}
	var callID string
	for _, raw := range first.Output {
		var call struct {
			Type   string `json:"type"`
			Name   string `json:"name"`
			CallID string `json:"call_id"`
		}
		if err := json.Unmarshal(raw, &call); err != nil {
			return nil, err
		}
		if call.Type != wanted {
			continue
		}
		if callID != "" || call.CallID == "" {
			return nil, errors.New("expected one tool call")
		}
		if (kind == "custom" && call.Name != "run_js") || (kind != "custom" && call.Name != "probe_value") {
			return nil, errors.New("unexpected tool name")
		}
		callID = call.CallID
	}
	if callID == "" {
		return nil, errors.New("response did not contain the forced tool call")
	}
	result, err := json.Marshal(body{"type": outputType, "call_id": callID, "output": value})
	return result, err
}

func verifiedAnswer(result turn, value string) error {
	if strings.TrimSpace(result.Text) != value {
		return errors.New("final answer does not match the tool output")
	}
	return nil
}
