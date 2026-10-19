package provider

import (
	"context"
	"errors"
	"time"
)

// Retries stay inside one Generate call: hooks, tools, admission counters and
// terminal events belong to the logical request, not its HTTP attempts. Each
// attempt builds fresh response state; already observed fragments remain visible.
func generateWithRetry(ctx context.Context, request Request, generate func(context.Context, Request) (Response, error)) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	delays := [...]time.Duration{2 * time.Second, 4 * time.Second}
	for attempt := 0; ; attempt++ {
		response, err := generate(ctx, request)
		if err == nil {
			return response, nil
		}
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return Response{}, cancelErr
		}
		if attempt == len(delays) || !retryableGenerationError(err) {
			return Response{}, err
		}
		timer := time.NewTimer(delays[attempt])
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
	if !errors.As(err, &failure) {
		return false
	}
	switch failure.Kind {
	case ErrorTransport, ErrorRateLimit, ErrorStream:
		return true
	default:
		return false
	}
}
