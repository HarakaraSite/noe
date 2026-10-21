package agent

import (
	"context"
	"encoding/json"
)

// RequestError is a safe request failure returned across the JS promise
// boundary. It deliberately omits provider diagnostics and response bodies.
type RequestError struct {
	Code    string
	Message string
	cause   error
}

func (e *RequestError) Error() string {
	if e == nil || e.Message == "" {
		return "agent request failed"
	}
	return e.Message
}

func (e *RequestError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Call dispatches one operation-scoped request through the shared Go
// acceptance layer. The JSON payload is only an adapter boundary; no network
// transport or child process is involved.
func (op *Operation) Call(method, payload string) (string, error) {
	if op == nil {
		return "", ErrOperationClosed
	}
	return op.CallContext(op.ctx, method, payload)
}

// CallContext adds a caller lifetime to a request without changing the target
// operation's lifetime.
func (op *Operation) CallContext(ctx context.Context, method, payload string) (string, error) {
	if op == nil {
		return "", ErrOperationClosed
	}
	if ctx == nil {
		return "", &RequestError{Code: "request", Message: "agent request context is required"}
	}
	return op.call(ctx, "", method, payload)
}

// CallWithScope invokes one request through a child hook scope while retaining
// the caller's operation lifetime.
func (op *Operation) CallWithScope(ctx context.Context, scope, method, payload string) (string, error) {
	if scope == "" || ctx == nil {
		return "", &RequestError{Code: "scope", Message: "agent operation scope is incomplete"}
	}
	return op.call(ctx, scope, method, payload)
}

func (op *Operation) call(ctx context.Context, scopeOverride, method, payload string) (string, error) {
	if op == nil || op.host == nil {
		return "", ErrOperationClosed
	}
	if ctx == nil {
		return "", &RequestError{Code: "request", Message: "agent request context is required"}
	}
	if err := op.ctx.Err(); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if op.initErr != nil {
		return "", op.initErr
	}
	if !validOperationMethod(method) {
		return "", &RequestError{Code: "method", Message: "unknown agent request method"}
	}
	if !json.Valid([]byte(payload)) {
		return "", &RequestError{Code: "request", Message: "invalid agent request JSON"}
	}
	return op.host.callDirect(ctx, op, scopeOverride, method, payload)
}
