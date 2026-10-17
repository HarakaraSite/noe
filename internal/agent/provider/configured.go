package provider

import (
	"encoding/json"
	"errors"
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
			if state.runID == "" {
				return nil, errors.New("provider request requires a run ID")
			}
			request.Header.Set(c.profile.SessionHeader, state.runID)
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

func parameterObject(parameters map[string]json.RawMessage) (map[string]any, error) {
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
		next, ok := object[key].(map[string]any)
		if !ok {
			next = make(map[string]any)
			object[key] = next
		}
		object = next
	}
	object[keys[len(keys)-1]] = value
}

func (p *OpenRouter) chatRequestBody(request Request) ([]byte, error) {
	return configuredChatRequestBody(p.profile, request)
}

func configuredChatRequestBody(profile Definition, request Request) ([]byte, error) {
	raw, err := makeChatRequestBody(request)
	if err != nil {
		return nil, err
	}
	var dynamic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &dynamic); err != nil {
		return nil, err
	}
	delete(dynamic, "session_id")
	delete(dynamic, "reasoning")
	body := make(map[string]json.RawMessage)
	for key, value := range profile.Parameters {
		body[key] = value
	}
	for key, value := range dynamic {
		if key == "stream_options" && body[key] != nil {
			var configured, required map[string]json.RawMessage
			if json.Unmarshal(body[key], &configured) == nil && json.Unmarshal(value, &required) == nil && configured != nil {
				for key, value := range required {
					configured[key] = value
				}
				value, _ = json.Marshal(configured)
			}
		}
		body[key] = value
	}
	if profile.SessionField != "" && request.CacheKey != "" {
		putRawField(body, profile.SessionField, request.CacheKey)
	}
	effort, _ := NormalizeEffort(request.Effort)
	if effort != "auto" {
		if profile.EffortField == "" {
			return nil, errors.New("provider has no effort mapping")
		}
		putRawField(body, profile.EffortField, effort)
	}
	return json.Marshal(body)
}

func putRawField(object map[string]json.RawMessage, path string, value any) {
	key, rest, nested := strings.Cut(path, ".")
	if !nested {
		object[key], _ = json.Marshal(value)
		return
	}
	next := make(map[string]json.RawMessage)
	_ = json.Unmarshal(object[key], &next)
	if next == nil {
		next = make(map[string]json.RawMessage)
	}
	putRawField(next, rest, value)
	object[key], _ = json.Marshal(next)
}
