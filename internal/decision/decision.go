// Package decision defines one-shot structured model decisions, independently
// of the agent loop and provider SDK types.
package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type Request struct {
	Provider  string                     `json:"provider"`
	Model     string                     `json:"model"`
	State     json.RawMessage            `json:"state"`
	Questions map[string]json.RawMessage `json:"questions"`
}

type Response struct {
	JSON       json.RawMessage
	StatusCode int
}

type Provider interface {
	Decide(context.Context, Request) (Response, error)
}

func ParseRequest(raw []byte) (Request, error) {
	var request Request
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return Request{}, errors.New("invalid decision request")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Request{}, errors.New("invalid trailing decision data")
	}
	return request, request.Validate()
}

func (r Request) Validate() error {
	if r.Provider != "openrouter" || strings.TrimSpace(r.Model) == "" {
		return errors.New("decision requires provider openrouter and a nonempty model")
	}
	state := bytes.TrimSpace(r.State)
	if !json.Valid(state) || len(state) == 0 {
		return errors.New("decision state must be a nonempty string, object, or array")
	}
	switch state[0] {
	case '"':
		var text string
		if json.Unmarshal(state, &text) != nil || strings.TrimSpace(text) == "" {
			return errors.New("decision state must be a nonempty string, object, or array")
		}
	case '{', '[':
	default:
		return errors.New("decision state must be a nonempty string, object, or array")
	}
	if len(r.Questions) == 0 {
		return errors.New("decision questions must be a nonempty object")
	}
	for _, question := range r.Questions {
		if !IsObject(question) {
			return errors.New("each decision question must be an object")
		}
	}
	return nil
}

func IsObject(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) != 0 && raw[0] == '{' && json.Valid(raw)
}

func ValidateResponse(raw json.RawMessage) error {
	var response map[string]json.RawMessage
	if !IsObject(raw) || json.Unmarshal(raw, &response) != nil || !IsObject(response["answers"]) {
		return errors.New("invalid decision response")
	}
	if value, exists := response["error"]; exists && string(bytes.TrimSpace(value)) != "null" {
		return errors.New("decision API returned an error")
	}
	return nil
}
