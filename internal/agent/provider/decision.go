package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"

	"noe/internal/decision"

	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/models/operations"
	"github.com/OpenRouterTeam/go-sdk/retry"
)

type decisionContextKey struct{}

type decisionCall struct {
	payload []byte
	status  int
	body    *decisionBody
}

type decisionBody struct {
	io.ReadCloser
	data     bytes.Buffer
	readErr  error
	once     sync.Once
	closeErr error
}

func (b *decisionBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.data.Write(p[:n])
	if err != nil && err != io.EOF {
		b.readErr = err
	}
	return n, err
}

func (b *decisionBody) Close() error {
	b.once.Do(func() { b.closeErr = b.ReadCloser.Close() })
	return b.closeErr
}

type decisionHTTPClient struct{ client HTTPClient }

func (c *decisionHTTPClient) Do(request *http.Request) (*http.Response, error) {
	call, _ := request.Context().Value(decisionContextKey{}).(*decisionCall)
	if call == nil {
		return c.client.Do(request)
	}
	if request.Body != nil {
		_ = request.Body.Close()
	}
	request.Body = io.NopCloser(bytes.NewReader(call.payload))
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(call.payload)), nil }
	request.ContentLength = int64(len(call.payload))
	request.TransferEncoding = nil
	request.Header.Del("Content-Length")
	response, err := c.client.Do(request)
	if response != nil {
		call.status = response.StatusCode
		source := response.Body
		if source == nil {
			source = http.NoBody
		}
		call.body = &decisionBody{ReadCloser: source}
		response.Body = call.body
	}
	return response, err
}

// Decide uses the official SDK for transport/authentication and retains the
// original JSON at the HTTP boundary instead of re-encoding SDK structs.
func (p *OpenRouter) Decide(ctx context.Context, request decision.Request) (decision.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := cancellationError(ctx); err != nil {
		return decision.Response{}, err
	}
	if err := request.Validate(); err != nil {
		return decision.Response{}, err
	}
	payload, err := json.Marshal(struct {
		Model     string                     `json:"model"`
		State     json.RawMessage            `json:"state"`
		Questions map[string]json.RawMessage `json:"questions"`
	}{request.Model, request.State, request.Questions})
	if err != nil {
		return decision.Response{}, providerError(ErrorResponse, 0, nil)
	}
	call := &decisionCall{payload: payload}
	ctx = context.WithValue(ctx, decisionContextKey{}, call)
	defer func() {
		if call.body != nil {
			_ = call.body.Close()
		}
	}()
	_, err = p.client.Alpha.Decisions.Create(ctx, components.DecisionsRequest{
		Model: request.Model, State: components.CreateStateStr(""), Questions: map[string]components.Questions{},
	}, operations.WithServerURL(p.decisionServerURL), operations.WithRetries(retry.Config{Strategy: "none"}))
	if cancelErr := cancellationError(ctx); cancelErr != nil {
		return decision.Response{}, cancelErr
	}
	if err != nil {
		return decision.Response{}, classifyHTTPError(call.status, err)
	}
	if call.status != http.StatusOK || call.body == nil || call.body.readErr != nil || decision.ValidateResponse(call.body.data.Bytes()) != nil {
		return decision.Response{}, providerError(ErrorResponse, call.status, nil)
	}
	return decision.Response{JSON: append(json.RawMessage(nil), call.body.data.Bytes()...), StatusCode: call.status}, nil
}
