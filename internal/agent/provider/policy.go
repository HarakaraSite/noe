package provider

import (
	"context"
	"encoding/json"

	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

// classifyFailure runs only on failed requests, before the retry decision. It
// uses raw received fields because SDKs may reject new error codes or discard
// the reason when turning an SSE error into a transport/decoding failure.
func (s *callState) classifyFailure(ctx context.Context, err *error) {
	if *err == nil || ctx.Err() != nil {
		return
	}
	status, body := s.response()
	if body == nil {
		return
	}
	body.mu.Lock()
	defer body.mu.Unlock()
	if failure := cyberPolicyError(body.errorBody, status, body.requestID, body.credential); failure != nil {
		*err = failure
		return
	}
	for _, event := range body.events {
		if failure := cyberPolicyError(event, status, body.requestID, body.credential); failure != nil {
			*err = failure
			return
		}
	}
}

func cyberPolicyError(raw []byte, status int, requestID, credential string) *ProviderError {
	var envelope struct {
		Type     string          `json:"type"`
		Code     string          `json:"code"`
		Message  string          `json:"message"`
		Error    json.RawMessage `json:"error"`
		Response struct {
			Error json.RawMessage `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return nil
	}
	var upstream struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	var rawError json.RawMessage
	switch {
	case envelope.Type == "error":
		upstream.Code, upstream.Message = envelope.Code, envelope.Message
		if upstream.Code == "" {
			rawError = envelope.Error
		}
	case envelope.Type == "response.failed" || envelope.Type == "response.incomplete":
		rawError = envelope.Response.Error
	default:
		// HTTP and Chat-compatible streaming error envelopes.
		rawError = envelope.Error
	}
	if len(rawError) > 0 && json.Unmarshal(rawError, &upstream) != nil {
		return nil
	}
	if upstream.Code != "cyber_policy" {
		return nil
	}
	failure := providerError(ErrorPolicy, status, nil)
	failure.Details = &imageapi.Error{
		Code: "cyber_policy", Message: "request blocked by cybersecurity policy",
		ProviderCode: upstream.Code, ProviderMessage: sanitizeImageMetadata(upstream.Message, credential),
		StatusCode: status, RequestID: sanitizeImageMetadata(requestID, credential),
	}
	return failure
}
