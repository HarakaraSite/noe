package provider

import (
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type configuredHTTPClient struct {
	client  HTTPClient
	profile Definition
}

func (c *configuredHTTPClient) Do(request *http.Request) (*http.Response, error) {
	request = request.Clone(request.Context())
	for key, value := range c.profile.Headers {
		request.Header.Set(key, value)
	}
	if state, _ := request.Context().Value(callContextKey{}).(*callState); state != nil {
		if c.profile.SessionHeader != "" {
			if state.sessionID == "" {
				return nil, errors.New("provider request requires a session ID")
			}
			request.Header.Set(c.profile.SessionHeader, state.sessionID)
		}
		endpoint, err := url.JoinPath(c.profile.BaseURL, c.profile.Path)
		if err != nil {
			return nil, err
		}
		request.URL, err = url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
	}
	return c.client.Do(request)
}

func parameterObject(parameters map[string]jsonv1.RawMessage) (map[string]any, error) {
	if len(parameters) == 0 {
		return make(map[string]any), nil
	}
	raw, err := json.Marshal(parameters)
	if err != nil {
		return nil, err
	}
	return decodeObject(raw)
}

func putField(object map[string]any, path string, value any) {
	keys := strings.Split(path, ".")
	for _, key := range keys[:len(keys)-1] {
		next, _ := object[key].(map[string]any)
		if raw, ok := object[key].(jsonv1.RawMessage); ok {
			next, _ = rawParameterObject(raw)
		}
		if next == nil {
			next = make(map[string]any)
		}
		object[key] = next
		object = next
	}
	object[keys[len(keys)-1]] = value
}

func (p *OpenRouter) chatRequestBody(request Request) ([]byte, error) {
	return configuredChatRequestBody(p.profile, request)
}

func configuredChatRequestBody(profile Definition, request Request) ([]byte, error) {
	effort, err := validateGenerationRequest(request)
	if err != nil {
		return nil, err
	}
	body := make(map[string]any, len(profile.Parameters)+6)
	for key, value := range profile.Parameters {
		body[key] = value
	}
	body["model"], body["messages"], body["stream"] = request.Model, request.Messages, true
	if len(request.Tools) != 0 {
		body["tools"] = request.Tools
	}
	if request.MaxTokens > 0 {
		body["max_tokens"] = request.MaxTokens
	}
	var streamOptions map[string]any
	if raw := profile.Parameters["stream_options"]; raw != nil {
		streamOptions, err = rawParameterObject(raw)
	}
	if err != nil || streamOptions == nil {
		streamOptions = make(map[string]any, 1)
	}
	streamOptions["include_usage"] = true
	body["stream_options"] = streamOptions
	if profile.SessionField != "" && request.CacheKey != "" {
		putField(body, profile.SessionField, request.CacheKey)
	}
	if effort != "auto" {
		if profile.EffortField == "" {
			return nil, chatRequestEncodingError(request, errors.New("provider has no effort mapping"))
		}
		putField(body, profile.EffortField, effort)
	}
	payload, err := json.Marshal(body)
	_, keepsMessages := body["messages"].([]jsonv1.RawMessage)
	_, keepsTools := body["tools"].([]jsonv1.RawMessage)
	if err != nil || !keepsMessages || len(request.Tools) != 0 && !keepsTools {
		if err := chatRequestEncodingError(request, err); err != nil {
			return nil, err
		}
	}
	return payload, nil
}

// An object that is valid on its own can exceed the JSON depth limit inside
// the request arrays. Retain that error's precedence if a profile displaced
// an array or the final payload failed to encode.
func chatRequestEncodingError(request Request, cause error) error {
	history := struct {
		Messages []jsonv1.RawMessage `json:"messages"`
		Tools    []jsonv1.RawMessage `json:"tools,omitempty"`
	}{Messages: request.Messages, Tools: request.Tools}
	if err := json.MarshalWrite(io.Discard, history); err != nil {
		return providerError(ErrorResponse, 0, nil)
	}
	return cause
}

// rawParameterObject opens only the level being merged and keeps each member
// as raw JSON so its number spelling and nested values are preserved.
func rawParameterObject(raw jsonv1.RawMessage) (map[string]any, error) {
	var fields map[string]jsonv1.RawMessage
	err := json.Unmarshal(raw, &fields)
	if fields == nil {
		return nil, err
	}
	object := make(map[string]any, len(fields))
	for key, value := range fields {
		object[key] = value
	}
	return object, err
}
