package provider

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	maxGenerationRetries = 2
	maxServerRetryDelay  = 60 * time.Second
)

type retryDirective int8

const (
	retryDirectiveDefault retryDirective = iota
	retryDirectiveDeny
	retryDirectiveAllow
)

// Retries stay inside one Generate call: hooks, tools, admission counters and
// terminal events belong to the logical request, not its HTTP attempts. Each
// attempt builds fresh response state; already observed fragments remain visible.
func generateWithRetry(ctx context.Context, request Request, generate func(context.Context, Request) (Response, error)) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for retry := 0; ; retry++ {
		response, err := generate(ctx, request)
		if err == nil {
			return response, nil
		}
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return Response{}, cancelErr
		}
		if retry == maxGenerationRetries || !retryableGenerationError(err) {
			return Response{}, err
		}
		delay := generationRetryDelay(err, retry)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Response{}, cancellationError(ctx)
		case <-timer.C:
		}
	}
}

func retryableGenerationError(err error) bool {
	var failure *ProviderError
	if !errors.As(err, &failure) || failure == nil {
		return false
	}
	// An explicit retry directive never turns a deterministic account or
	// authentication failure into a retry.
	switch failure.Kind {
	case ErrorAuthentication, ErrorQuota, ErrorCancelled:
		return false
	}
	if failure.retryDelayTooLong || failure.retryDirective == retryDirectiveDeny {
		return false
	}
	if failure.retryDirective == retryDirectiveAllow {
		return true
	}
	switch failure.Kind {
	case ErrorTransport, ErrorRateLimit, ErrorStream:
		return true
	default:
		return false
	}
}

func generationRetryDelay(err error, retry int) time.Duration {
	var failure *ProviderError
	if errors.As(err, &failure) && failure != nil && failure.hasRetryAfter {
		return failure.retryAfter
	}
	base := 2 * time.Second
	if retry > 0 {
		base = 4 * time.Second
	}
	// Add downward jitter so concurrent clients do not all retry together.
	return time.Duration(float64(base) * (1 - rand.Float64()*0.25))
}

// classifyHTTPResponseError adds machine-readable response classification and
// retry metadata without retaining or exposing provider diagnostic bodies.
func classifyHTTPResponseError(status int, cause error, code string, headers http.Header) error {
	err := classifyHTTPError(status, cause)
	var failure *ProviderError
	if !errors.As(err, &failure) || failure == nil {
		return err
	}
	if failure.Kind != ErrorAuthentication {
		if kind, known := providerCodeKind(code); known {
			failure.Kind = kind
		}
	}
	applyRetryMetadata(failure, headers)
	return failure
}

func applyRetryMetadata(failure *ProviderError, headers http.Header) {
	if failure == nil {
		return
	}
	switch strings.ToLower(strings.TrimSpace(headers.Get("x-should-retry"))) {
	case "false":
		failure.retryDirective = retryDirectiveDeny
	case "true":
		failure.retryDirective = retryDirectiveAllow
	}
	if delay, ok, tooLong := serverRetryDelay(headers); ok {
		failure.retryAfter = delay
		failure.hasRetryAfter = true
		failure.retryDelayTooLong = tooLong
	}
}

// serverRetryDelay honors the millisecond header before Retry-After. Invalid
// values are ignored, allowing the lower-precedence hint or local backoff to
// apply. A valid delay above the local cap is reported separately so callers
// can return the classified error instead of retrying earlier than requested.
func serverRetryDelay(headers http.Header) (delay time.Duration, valid bool, tooLong bool) {
	if raw := strings.TrimSpace(headers.Get("retry-after-ms")); raw != "" {
		if value, err := strconv.ParseFloat(raw, 64); err == nil {
			if delay, valid, tooLong := retryDelayFromNumber(value, float64(time.Millisecond)); valid {
				return delay, valid, tooLong
			}
		}
	}
	if raw := strings.TrimSpace(headers.Get("retry-after")); raw != "" {
		if seconds, err := strconv.ParseFloat(raw, 64); err == nil {
			if delay, valid, tooLong := retryDelayFromNumber(seconds, float64(time.Second)); valid {
				return delay, valid, tooLong
			}
		}
		if date, err := http.ParseTime(raw); err == nil {
			until := time.Until(date)
			if until >= 0 {
				if until > maxServerRetryDelay {
					return 0, true, true
				}
				return until, true, false
			}
		}
	}
	return 0, false, false
}

func retryDelayFromNumber(value, unit float64) (time.Duration, bool, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false, false
	}
	maxUnits := float64(maxServerRetryDelay) / unit
	if value > maxUnits {
		return 0, true, true
	}
	return time.Duration(value * unit), true, false
}

// providerCodeKind classifies only recognized machine codes. Free-form error
// messages are intentionally excluded because providers can return arbitrary
// user-visible or secret-bearing diagnostics.
func providerCodeKind(code string) (ErrorKind, bool) {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "insufficient_quota", "quota_exceeded", "billing_hard_limit_reached",
		"usage_limit_exceeded", "usage_limit_reached",
		"subscription_sharing_usage_limit_exceeded",
		"subscription_sharing_workplace_individual_usage_limit_exceeded":
		return ErrorQuota, true
	case "rate_limit_exceeded", "rate_limit_error":
		return ErrorRateLimit, true
	case "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable", "user_unavailable":
		return ErrorTransport, true
	default:
		return "", false
	}
}
