// Package imageapi defines file-based image input and independent generation.
// Image bytes stay in Go; JavaScript receives only metadata.
package imageapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type Metadata struct {
	ImageID  string `json:"imageId,omitempty"`
	Path     string `json:"path"`
	MIMEType string `json:"mimeType"`
	Bytes    int    `json:"bytes"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Detail   string `json:"detail,omitempty"`
}

type Data struct {
	Metadata
	Content []byte `json:"-"`
	Ordinal int    `json:"-"`
}

type Request struct {
	Provider   string                     `json:"provider"`
	Model      string                     `json:"model"`
	Prompt     string                     `json:"prompt"`
	Output     string                     `json:"output"`
	Parameters map[string]json.RawMessage `json:"parameters,omitempty"`
	RunID      string                     `json:"-"`
}

func (r Request) Validate() error {
	if strings.TrimSpace(r.Provider) == "" || strings.TrimSpace(r.Model) == "" || strings.TrimSpace(r.Prompt) == "" || strings.TrimSpace(r.Output) == "" {
		return &Error{Code: "input", Message: "provider, model, prompt, and output must be nonempty strings"}
	}
	for key, value := range r.Parameters {
		switch strings.ToLower(key) {
		case "model", "prompt", "n", "stream", "provider", "baseurl", "path", "output", "api_key", "authorization", "headers", "session_id", "prompt_cache_key", "input_references", "image", "images":
			return &Error{Code: "input", Message: "reserved image parameter: " + key}
		}
		if strings.TrimSpace(key) == "" || !json.Valid(value) {
			return &Error{Code: "input", Message: "invalid image parameter"}
		}
	}
	return nil
}

type Generated struct {
	Content       []byte `json:"-"`
	ResponseModel string
	RequestID     string
	Usage         json.RawMessage
}

type Response struct {
	Metadata
	Provider      string          `json:"provider"`
	Model         string          `json:"model"`
	ResponseModel string          `json:"responseModel,omitempty"`
	RequestID     string          `json:"requestId,omitempty"`
	Usage         json.RawMessage `json:"usage,omitempty"`
}

type Provider interface {
	GenerateImage(context.Context, Request) (Generated, error)
}

type Error struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	ProviderCode    string `json:"providerCode,omitempty"`
	ProviderMessage string `json:"providerMessage,omitempty"`
	StatusCode      int    `json:"statusCode,omitempty"`
	RequestID       string `json:"requestId,omitempty"`
}

func (e *Error) Error() string {
	if e == nil {
		return "image: unknown error"
	}
	return "image: " + e.Message
}
func AsError(err error) *Error {
	var imageErr *Error
	if errors.As(err, &imageErr) {
		return imageErr
	}
	code := "response"
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = "cancelled"
	}
	return &Error{Code: code, Message: err.Error()}
}
