// Package decision defines one-shot structured model decisions, independently
// of the agent loop and provider SDK types.
package decision

import (
	"bytes"
	"context"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"io"
	"strings"
)

type Request struct {
	Provider  string                       `json:"provider"`
	Model     string                       `json:"model"`
	State     jsonv1.RawMessage            `json:"state"`
	Questions map[string]jsonv1.RawMessage `json:"questions"`
}

type Response struct {
	JSON       jsonv1.RawMessage
	StatusCode int
}

type Provider interface {
	Decide(context.Context, Request) (Response, error)
}

func ParseRequest(raw []byte) (Request, error) {
	var request Request
	decoder := jsontext.NewDecoder(bytes.NewReader(raw))
	if err := json.UnmarshalDecode(decoder, &request, json.RejectUnknownMembers(true)); err != nil {
		return Request{}, errors.New("invalid decision request")
	}
	if err := json.UnmarshalDecode(decoder, new(any)); err != io.EOF {
		return Request{}, errors.New("invalid trailing decision data")
	}
	return request, request.Validate()
}

func (r Request) Validate() error {
	if r.Provider != "openrouter" || strings.TrimSpace(r.Model) == "" {
		return errors.New("decision requires provider openrouter and a nonempty model")
	}
	state := bytes.TrimSpace(r.State)
	if !jsontext.Value(state).IsValid() || len(state) == 0 {
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

func IsObject(raw jsonv1.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) != 0 && raw[0] == '{' && jsontext.Value(raw).IsValid()
}

func ValidateResponse(raw jsonv1.RawMessage) error {
	var response map[string]jsonv1.RawMessage
	if !IsObject(raw) || json.Unmarshal(raw, &response) != nil || !IsObject(response["answers"]) {
		return errors.New("invalid decision response")
	}
	if value, exists := response["error"]; exists && string(bytes.TrimSpace(value)) != "null" {
		return errors.New("decision API returned an error")
	}
	return nil
}
