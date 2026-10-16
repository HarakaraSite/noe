package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type modelRequest struct {
	Messages       []json.RawMessage
	Tools          []json.RawMessage
	CacheKey, Name string
}
type modelResponse struct {
	Message      map[string]any  `json:"message"`
	FinishReason string          `json:"finishReason"`
	Usage        json.RawMessage `json:"usage"`
}
type provider interface {
	generate(context.Context, modelRequest) (modelResponse, error)
}
type providerError struct{ Code, Message string }

func (e *providerError) Error() string { return e.Code + ": " + e.Message }

type openRouter struct {
	client     openai.Client
	model, key string
	maxTokens  int
}

func newOpenRouter(key, model string, maxTokens int) *openRouter {
	return &openRouter{client: openai.NewClient(option.WithAPIKey(key), option.WithBaseURL("https://openrouter.ai/api/v1/"), option.WithMaxRetries(0)), model: model, key: key, maxTokens: maxTokens}
}

func (p *openRouter) generate(ctx context.Context, q modelRequest) (response modelResponse, resultErr error) {
	defer func() {
		if resultErr == nil {
			return
		}
		var pe *providerError
		if errors.As(resultErr, &pe) {
			resultErr = &providerError{Code: pe.Code, Message: strings.ReplaceAll(pe.Message, p.key, "[redacted]")}
		}
	}()
	params := openai.ChatCompletionNewParams{Model: p.model, StreamOptions: openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}}
	if p.maxTokens > 0 {
		params.MaxTokens = openai.Int(int64(p.maxTokens))
	}
	// Raw messages preserve provider extensions such as reasoning_details across
	// tool turns. Typed SDK conversion would otherwise discard unknown fields.
	opts := []option.RequestOption{option.WithJSONSet("messages", q.Messages), option.WithJSONSet("tools", q.Tools)}
	if q.CacheKey != "" {
		opts = append(opts, option.WithJSONSet("session_id", q.CacheKey))
	}
	stream := p.client.Chat.Completions.NewStreaming(ctx, params, opts...)
	defer stream.Close()
	acc := completionStream{message: map[string]any{"role": "assistant", "content": nil}}
	for stream.Next() {
		if err := acc.add([]byte(stream.Current().RawJSON())); err != nil {
			return modelResponse{}, err
		}
	}
	if err := stream.Err(); err != nil {
		code := "provider"
		var api *openai.Error
		if errors.As(err, &api) {
			switch api.StatusCode {
			case 401, 403:
				code = "authentication"
			case 429:
				code = "rate_limit"
			}
		}
		if ctx.Err() != nil {
			return modelResponse{}, ctx.Err()
		}
		return modelResponse{}, &providerError{Code: code, Message: strings.ReplaceAll(err.Error(), p.key, "[redacted]")}
	}
	if acc.finish == "" {
		return modelResponse{}, &providerError{Code: "incomplete_stream", Message: "model stream ended without a finish reason"}
	}
	return modelResponse{Message: acc.message, FinishReason: acc.finish, Usage: acc.usage}, nil
}

// completionStream also accumulates OpenRouter's extension fields, retaining
// reasoning block order and fragmented function arguments.
type completionStream struct {
	message map[string]any
	finish  string
	usage   json.RawMessage
}

func (a *completionStream) add(raw []byte) error {
	var chunk struct {
		Choices []struct {
			Index  int            `json:"index"`
			Delta  map[string]any `json:"delta"`
			Finish string         `json:"finish_reason"`
		} `json:"choices"`
		Usage json.RawMessage `json:"usage"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return fmt.Errorf("decode model stream: %w", err)
	}
	if chunk.Error != nil {
		return &providerError{Code: "provider_stream", Message: chunk.Error.Message}
	}
	for _, choice := range chunk.Choices {
		if choice.Index != 0 {
			continue
		}
		mergeDelta(a.message, choice.Delta)
		if choice.Finish != "" {
			a.finish = choice.Finish
		}
	}
	if len(chunk.Usage) > 0 && string(chunk.Usage) != "null" {
		a.usage = chunk.Usage
	}
	return nil
}

func mergeDelta(dst, src map[string]any) {
	for key, value := range src {
		if value == nil || key == "index" {
			continue
		}
		switch v := value.(type) {
		case string:
			switch key {
			case "content", "arguments", "name", "reasoning", "reasoning_content", "refusal", "text", "summary", "data", "signature":
				old, _ := dst[key].(string)
				dst[key] = old + v
			default:
				dst[key] = v
			}
		case map[string]any:
			old, _ := dst[key].(map[string]any)
			if old == nil {
				old = map[string]any{}
			}
			mergeDelta(old, v)
			dst[key] = old
		case []any:
			old, _ := dst[key].([]any)
			if key == "tool_calls" || key == "reasoning_details" {
				for _, part := range v {
					item, ok := part.(map[string]any)
					if !ok {
						continue
					}
					index := len(old)
					if n, ok := item["index"].(float64); ok {
						index = int(n)
					}
					if index < 0 {
						continue
					}
					for len(old) <= index {
						old = append(old, map[string]any{})
					}
					target, _ := old[index].(map[string]any)
					mergeDelta(target, item)
					if key == "reasoning_details" {
						target["index"] = index
					}
				}
				dst[key] = old
			} else {
				dst[key] = append(old, v...)
			}
		default:
			dst[key] = value
		}
	}
}
