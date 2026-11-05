// Package provider defines the small boundary between the agent loop and model
// providers. Provider values deliberately use JSON so SDK-specific types stay
// inside each adapter.
package provider

import (
	"context"
	jsonv1 "encoding/json"
)

// Request is one chat completion request. Messages and Tools contain complete
// provider-compatible JSON objects and are forwarded without SDK conversion.
type Request struct {
	Provider  string              `json:"provider,omitempty"`
	RunID     string              `json:"runId,omitempty"`
	SessionID string              `json:"sessionId,omitempty"` // Stable for a managed agent instance.
	Model     string              `json:"model"`
	Messages  []jsonv1.RawMessage `json:"messages"`
	Tools     []jsonv1.RawMessage `json:"tools,omitempty"`
	CacheKey  string              `json:"cacheKey,omitempty"`
	MaxTokens int                 `json:"maxTokens,omitzero"`
	Effort    string              `json:"effort,omitempty"`
	Observe   func(StreamEvent)   `json:"-"`
}

func (r Request) sessionID() string {
	if r.SessionID != "" {
		return r.SessionID
	}
	// Direct provider callers without a managed instance use a standalone run.
	return r.RunID
}

// StreamEvent is a text fragment observed while a provider response is
// streaming. Type is one of "thinking", "thinking_summary", or
// "message_delta". Observe callbacks run synchronously on the provider's Go
// stream-reading goroutine.
type StreamEvent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Response contains the assistant message, completion reason and raw usage
// object returned by a provider.
type Response struct {
	Message      jsonv1.RawMessage `json:"message"`
	FinishReason string            `json:"finishReason"`
	Usage        jsonv1.RawMessage `json:"usage,omitzero"`
}

// Provider generates one assistant response for a chat history.
type Provider interface {
	Generate(context.Context, Request) (Response, error)
}
