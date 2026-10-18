package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type rpcErrorBody struct {
	Error rpcError `json:"error"`
}

type rpcError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RPCError is a safe broker error returned to the JS promise boundary.
// It intentionally contains no raw provider diagnostics or response body.
type RPCError struct {
	Code    string
	Message string
	cause   error
}

func (e *RPCError) Error() string {
	if e == nil || e.Message == "" {
		return "agent request failed"
	}
	return e.Message
}

func (e *RPCError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Call invokes one operation-scoped broker RPC.
func (op *Operation) Call(method, payload string) (string, error) {
	if op == nil {
		return "", ErrOperationClosed
	}
	return op.call(op.ctx, "", method, payload)
}

// CallWithScope invokes an RPC using a child broker scope while retaining the
// operation's connection capability. ctx lets the caller end one hook's
// in-flight requests without closing the enclosing runtime operation.
func (op *Operation) CallWithScope(ctx context.Context, scope, method, payload string) (string, error) {
	if scope == "" || ctx == nil {
		return "", &RPCError{Code: "scope", Message: "agent operation scope is incomplete"}
	}
	return op.call(ctx, scope, method, payload)
}

func (op *Operation) call(ctx context.Context, scopeOverride, method, payload string) (string, error) {
	if op == nil || op.host == nil {
		return "", ErrOperationClosed
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
	if !validRPCMethod(method) {
		return "", &RPCError{Code: "method", Message: "unknown agent request method"}
	}
	if !json.Valid([]byte(payload)) {
		return "", &RPCError{Code: "request", Message: "invalid agent request JSON"}
	}
	urlBase, token := op.remoteBase, op.remoteAuth
	if urlBase == "" {
		b, err := op.host.ensureBroker()
		if err != nil {
			return "", err
		}
		urlBase, token = b.url, b.token
	}
	scope := op.scope
	if scopeOverride != "" {
		scope = scopeOverride
	}
	if scope == "" || token == "" {
		return "", &RPCError{Code: "connection", Message: "agent connection is incomplete"}
	}
	if err := validateConnectionURL(urlBase); err != nil {
		return "", err
	}
	requestURL := strings.TrimRight(urlBase, "/") + "/rpc/" + method
	requestCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(op.ctx, cancel)
	defer func() {
		stop()
		cancel()
	}()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, requestURL, strings.NewReader(payload))
	if err != nil {
		if ctxErr := op.ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", &RPCError{Code: "transport", Message: "agent broker request failed"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Noe-Scope", scope)
	response, err := op.client.Do(req)
	if err != nil {
		if ctxErr := op.ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return "", err
		}
		return "", &RPCError{Code: "transport", Message: "agent broker request failed"}
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var body rpcErrorBody
		if json.NewDecoder(response.Body).Decode(&body) == nil && body.Error.Code != "" {
			err := &RPCError{Code: body.Error.Code, Message: body.Error.Message}
			if body.Error.Code == "cancelled" {
				if ctxErr := op.ctx.Err(); ctxErr != nil {
					return "", ctxErr
				}
				if ctxErr := ctx.Err(); ctxErr != nil {
					return "", ctxErr
				}
				err.cause = context.Canceled
			}
			return "", err
		}
		return "", &RPCError{Code: "broker", Message: "agent broker rejected the request"}
	}
	var result json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || !json.Valid(result) {
		return "", &RPCError{Code: "broker", Message: "agent broker returned an invalid response"}
	}
	return string(result), nil
}

func validateConnectionURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return &RPCError{Code: "connection", Message: "agent connection URL is invalid"}
	}
	host := parsed.Hostname()
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return &RPCError{Code: "connection", Message: "agent connection URL must use a loopback address"}
		}
	}
	return nil
}

func decodeRequest(r *http.Request, target any) error {
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return errors.New("invalid trailing JSON data")
	}
	return nil
}

func writeRPCError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(rpcErrorBody{Error: rpcError{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, value any) {
	_ = json.NewEncoder(w).Encode(value)
}
