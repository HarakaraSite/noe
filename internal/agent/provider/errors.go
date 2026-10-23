package provider

import (
	"context"
	"errors"
	"fmt"

	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

// ErrorKind classifies provider failures without exposing SDK diagnostics or
// response bodies, which may contain arbitrary server-supplied text.
type ErrorKind string

const (
	ErrorAuthentication ErrorKind = "authentication"
	ErrorQuota          ErrorKind = "quota"
	ErrorRateLimit      ErrorKind = "rate_limit"
	ErrorTransport      ErrorKind = "transport"
	ErrorStream         ErrorKind = "stream"
	ErrorResponse       ErrorKind = "response"
	ErrorCancelled      ErrorKind = "cancelled"
	ErrorPolicy         ErrorKind = "policy"
)

// ProviderError is a safe, classified error from a provider adapter.
type ProviderError struct {
	Kind       ErrorKind
	StatusCode int
	// Details is populated only for explicitly recognized policy errors, with
	// credentials removed before leaving the adapter.
	Details *imageapi.Error
	cause   error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "provider: unknown error"
	}
	message := map[ErrorKind]string{
		ErrorAuthentication: "authentication failed",
		ErrorQuota:          "usage limit or credits exhausted",
		ErrorRateLimit:      "rate limited",
		ErrorTransport:      "transport failed",
		ErrorStream:         "invalid or incomplete response stream",
		ErrorResponse:       "invalid provider response",
		ErrorCancelled:      "request cancelled",
		ErrorPolicy:         "request blocked by cybersecurity policy",
	}[e.Kind]
	if message == "" {
		message = "request failed"
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("provider: %s (HTTP %d)", message, e.StatusCode)
	}
	return "provider: " + message
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func providerError(kind ErrorKind, status int, cause error) *ProviderError {
	return &ProviderError{Kind: kind, StatusCode: status, cause: cause}
}

func cancellationError(ctx context.Context) error {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}
	return providerError(ErrorCancelled, 0, ctx.Err())
}

func classifyHTTPError(status int, cause error) error {
	var existing *ProviderError
	if errors.As(cause, &existing) {
		return existing
	}
	switch {
	case status == 401 || status == 403:
		return providerError(ErrorAuthentication, status, nil)
	case status == 402:
		return providerError(ErrorQuota, status, nil)
	case status == 429:
		return providerError(ErrorRateLimit, status, nil)
	case status >= 500:
		return providerError(ErrorTransport, status, nil)
	case status != 0:
		return providerError(ErrorResponse, status, nil)
	case errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded):
		return providerError(ErrorCancelled, 0, cause)
	default:
		return providerError(ErrorTransport, 0, nil)
	}
}
