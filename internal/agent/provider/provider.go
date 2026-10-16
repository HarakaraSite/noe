// Package provider defines the small boundary between the agent loop and model
// providers. Provider values deliberately use JSON so SDK-specific types stay
// inside each adapter.
package provider

import (
	"context"
	"encoding/json"
)

// Request is one chat completion request. Messages and Tools contain complete
// provider-compatible JSON objects and are forwarded without SDK conversion.
type Request struct {
	Model     string            `json:"model"`
	Messages  []json.RawMessage `json:"messages"`
	Tools     []json.RawMessage `json:"tools,omitempty"`
	CacheKey  string            `json:"cacheKey,omitempty"`
	MaxTokens int               `json:"maxTokens,omitempty"`
}

// Response contains the assistant message, completion reason and raw usage
// object returned by a provider.
type Response struct {
	Message      json.RawMessage `json:"message"`
	FinishReason string          `json:"finishReason"`
	Usage        json.RawMessage `json:"usage,omitempty"`
}

// Provider generates one assistant response for a chat history.
type Provider interface {
	Generate(context.Context, Request) (Response, error)
}
