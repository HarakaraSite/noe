package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type countingHTTPClient struct {
	base        HTTPClient
	calls       atomic.Int32
	closes      atomic.Int32
	readStarted chan struct{}
}

func (c *countingHTTPClient) Do(request *http.Request) (*http.Response, error) {
	c.calls.Add(1)
	response, err := c.base.Do(request)
	if response != nil && response.Body != nil {
		response.Body = &countingReadCloser{ReadCloser: response.Body, closes: &c.closes, readStarted: c.readStarted}
	}
	return response, err
}

type countingReadCloser struct {
	io.ReadCloser
	closes      *atomic.Int32
	readStarted chan struct{}
	readOnce    atomic.Bool
}

func (b *countingReadCloser) Read(p []byte) (int, error) {
	if b.readStarted != nil && b.readOnce.CompareAndSwap(false, true) {
		close(b.readStarted)
	}
	return b.ReadCloser.Read(p)
}

func (b *countingReadCloser) Close() error {
	b.closes.Add(1)
	return b.ReadCloser.Close()
}

func writeSSE(w http.ResponseWriter, data string) {
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func writeToolCallStream(w http.ResponseWriter, terminal bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	writeSSE(w, `{"id":"completion-1","model":"test/model","object":"chat.completion.chunk","created":1,"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"think","reasoning_details":[{"type":"reasoning.text","id":"reason-1","index":0,"text":"think","provider_extension":{"keep":true}}],"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"run_","arguments":"{\"code\":"}}]},"finish_reason":null}]}`)
	writeSSE(w, `{"id":"completion-1","model":"test/model","object":"chat.completion.chunk","created":1,"choices":[{"index":0,"delta":{"reasoning":"ing","reasoning_details":[{"type":"reasoning.text","id":"reason-1","index":0,"text":"ing"}],"tool_calls":[{"index":0,"function":{"name":"js","arguments":"\"answer()\"}"}}]},"finish_reason":null}]}`)
	writeSSE(w, `{"id":"completion-1","model":"test/model","object":"chat.completion.chunk","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
	writeSSE(w, `{"id":"completion-1","model":"test/model","object":"chat.completion.chunk","created":1,"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17,"cost":0.0003,"prompt_tokens_details":{"cached_tokens":8},"vendor_usage":{"route":"fixture"}}}`)
	if terminal {
		writeSSE(w, "[DONE]")
	}
}

func TestOpenRouterGeneratePreservesRequestAndStreamingFields(t *testing.T) {
	keyFile := t.TempDir() + "/openrouter-api-key"
	if err := osWriteFile(keyFile, []byte(" fixture-secret \n")); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/chat/completions" {
			t.Errorf("unexpected endpoint: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fixture-secret" {
			t.Errorf("Authorization = %q", got)
		}
		if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			t.Errorf("Accept does not request SSE: %q", r.Header.Get("Accept"))
		}
		var body struct {
			Model         string            `json:"model"`
			Messages      []json.RawMessage `json:"messages"`
			Tools         []json.RawMessage `json:"tools"`
			SessionID     string            `json:"session_id"`
			MaxTokens     int               `json:"max_tokens"`
			Stream        bool              `json:"stream"`
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.Model != "test/model" || body.SessionID != "conversation-1" || body.MaxTokens != 321 || !body.Stream || !body.StreamOptions.IncludeUsage {
			t.Errorf("request fields were not forwarded: %#v", body)
		}
		var message map[string]json.RawMessage
		if len(body.Messages) != 1 || json.Unmarshal(body.Messages[0], &message) != nil || len(message["reasoning_details"]) == 0 || len(message["provider_extension"]) == 0 {
			t.Errorf("raw message extensions were lost: %s", body.Messages)
		}
		var tool map[string]json.RawMessage
		if len(body.Tools) != 1 || json.Unmarshal(body.Tools[0], &tool) != nil || len(tool["provider_tool_extension"]) == 0 {
			t.Errorf("raw tool extensions were lost: %s", body.Tools)
		}
		writeToolCallStream(w, true)
	}))
	defer server.Close()

	httpClient := &countingHTTPClient{base: &http.Client{}}
	provider := NewOpenRouter(Options{KeyFile: keyFile, BaseURL: server.URL + "/api/v1", HTTPClient: httpClient})
	response, err := provider.Generate(context.Background(), Request{
		Model:     "test/model",
		Messages:  []json.RawMessage{json.RawMessage(`{"role":"assistant","content":null,"reasoning_details":[{"type":"reasoning.encrypted","data":"opaque"}],"provider_extension":{"preserve":true}}`)},
		Tools:     []json.RawMessage{json.RawMessage(`{"type":"function","function":{"name":"run_js","parameters":{"type":"object"}},"provider_tool_extension":"preserve"}`)},
		CacheKey:  "conversation-1",
		MaxTokens: 321,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if response.FinishReason != "tool_calls" {
		t.Fatalf("FinishReason = %q", response.FinishReason)
	}
	var message map[string]json.RawMessage
	if err := json.Unmarshal(response.Message, &message); err != nil {
		t.Fatalf("decode assistant message: %v", err)
	}
	var reasoning string
	if err := json.Unmarshal(message["reasoning"], &reasoning); err != nil || reasoning != "thinking" {
		t.Errorf("reasoning = %q, err = %v", reasoning, err)
	}
	var reasoningDetails []map[string]json.RawMessage
	if err := json.Unmarshal(message["reasoning_details"], &reasoningDetails); err != nil {
		t.Fatalf("decode reasoning_details: %v", err)
	}
	var detailText string
	if len(reasoningDetails) > 0 {
		_ = json.Unmarshal(reasoningDetails[0]["text"], &detailText)
	}
	if len(reasoningDetails) != 1 || detailText != "thinking" || string(reasoningDetails[0]["provider_extension"]) != `{"keep":true}` {
		t.Errorf("reasoning_details did not round trip: %s (err %v)", message["reasoning_details"], err)
	}
	var calls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	if err := json.Unmarshal(message["tool_calls"], &calls); err != nil || len(calls) != 1 || calls[0].ID != "call-1" || calls[0].Function.Name != "run_js" || calls[0].Function.Arguments != `{"code":"answer()"}` {
		t.Errorf("tool call fragments were not assembled: %s (err %v)", message["tool_calls"], err)
	}
	var usage map[string]json.RawMessage
	if err := json.Unmarshal(response.Usage, &usage); err != nil || string(usage["vendor_usage"]) != `{"route":"fixture"}` || len(usage["cost"]) == 0 {
		t.Errorf("usage was not preserved: %s (err %v)", response.Usage, err)
	}
	if got := requests.Load(); got != 1 || httpClient.calls.Load() != 1 {
		t.Errorf("request attempts = server:%d client:%d; want exactly one", got, httpClient.calls.Load())
	}
	if got := httpClient.closes.Load(); got != 1 {
		t.Errorf("response body close count = %d; want 1", got)
	}
}

func TestOpenRouterRequiresSSEDoneAfterFinish(t *testing.T) {
	keyFile := writeTestKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeToolCallStream(w, false)
	}))
	defer server.Close()
	httpClient := &countingHTTPClient{base: &http.Client{}}
	provider := NewOpenRouter(Options{KeyFile: keyFile, BaseURL: server.URL, HTTPClient: httpClient})
	_, err := provider.Generate(context.Background(), testRequest())
	assertProviderError(t, err, ErrorStream)
	if got := httpClient.closes.Load(); got != 3 {
		t.Errorf("response body close count = %d; want 3 attempts", got)
	}
}

func TestOpenRouterDoneCompletesWithoutHTTPEOFAndRetainsTextAfterNull(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"id":"x","object":"chat.completion.chunk","created":1,"model":"test/model","choices":[{"index":0,"delta":{"role":"assistant","content":null},"finish_reason":null}]}`)
		writeSSE(w, `{"id":"x","object":"chat.completion.chunk","created":1,"model":"test/model","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`)
		writeSSE(w, "[DONE]")
		<-r.Context().Done() // A completed SSE need not close its HTTP connection.
	}))
	defer server.Close()
	client := &countingHTTPClient{base: &http.Client{}}
	p := NewOpenRouter(Options{KeyFile: writeTestKey(t), BaseURL: server.URL, HTTPClient: client})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	response, err := p.Generate(ctx, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	var message struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(response.Message, &message); err != nil || message.Content != "answer" {
		t.Fatalf("lost streamed text: %s (%v)", response.Message, err)
	}
	if client.closes.Load() != 1 {
		t.Fatalf("body close count = %d; want 1", client.closes.Load())
	}
}

type scriptedHTTPClient struct {
	status int
	body   io.ReadCloser
}

func (c *scriptedHTTPClient) Do(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: c.status,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       c.body,
	}, nil
}

type errorAfterBody struct {
	data   []byte
	err    error
	closes atomic.Int32
}

func (b *errorAfterBody) Read(p []byte) (int, error) {
	if len(b.data) > 0 {
		n := copy(p, b.data)
		b.data = b.data[n:]
		return n, nil
	}
	return 0, b.err
}

func (b *errorAfterBody) Close() error {
	b.closes.Add(1)
	return nil
}

func TestOpenRouterRejectsBodyReadErrorAfterFinish(t *testing.T) {
	keyFile := writeTestKey(t)
	payload := toolCallStreamBytes(false)
	body := &errorAfterBody{data: payload, err: io.ErrUnexpectedEOF}
	provider := NewOpenRouter(Options{KeyFile: keyFile, BaseURL: "http://fixture.invalid", HTTPClient: &scriptedHTTPClient{status: http.StatusOK, body: body}})
	_, err := provider.Generate(context.Background(), testRequest())
	assertProviderError(t, err, ErrorStream)
	if got := body.closes.Load(); got != 3 {
		t.Errorf("response body close count = %d; want 3 attempts", got)
	}
}

func TestOpenRouterCancellationWhileWaitingForBody(t *testing.T) {
	keyFile := writeTestKey(t)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, ": waiting for the next event\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	httpClient := &countingHTTPClient{base: &http.Client{}, readStarted: make(chan struct{})}
	provider := NewOpenRouter(Options{KeyFile: keyFile, BaseURL: server.URL, HTTPClient: httpClient})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, err := provider.Generate(ctx, testRequest())
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("fixture request did not start")
	}
	select {
	case <-httpClient.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Generate did not start reading the response body")
	}
	cancel()
	select {
	case err := <-finished:
		assertProviderError(t, err, ErrorCancelled)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("errors.Is(err, context.Canceled) = false: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Generate did not stop after context cancellation")
	}
	if got := httpClient.closes.Load(); got != 1 {
		t.Errorf("response body close count = %d; want 1", got)
	}
}

func TestOpenRouterClassifiesHTTPErrorWithoutEchoingBody(t *testing.T) {
	keyFile := writeTestKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"fixture-secret"}}`)
	}))
	defer server.Close()
	httpClient := &countingHTTPClient{base: &http.Client{}}
	provider := NewOpenRouter(Options{KeyFile: keyFile, BaseURL: server.URL, HTTPClient: httpClient})
	_, err := provider.Generate(context.Background(), testRequest())
	assertProviderError(t, err, ErrorAuthentication)
	if strings.Contains(err.Error(), "fixture-secret") {
		t.Errorf("error leaked server body: %v", err)
	}
	if got := httpClient.closes.Load(); got != 1 {
		t.Errorf("response body close count = %d; want 1", got)
	}
}

func TestOpenRouterClassifiesCredentialAndProviderFailures(t *testing.T) {
	t.Run("missing credential", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
		}))
		defer server.Close()
		provider := NewOpenRouter(Options{KeyFile: t.TempDir() + "/missing-key", BaseURL: server.URL})
		_, err := provider.Generate(context.Background(), testRequest())
		assertProviderError(t, err, ErrorAuthentication)
		if got := requests.Load(); got != 0 {
			t.Errorf("request count = %d; credential failure should happen before HTTP", got)
		}
	})

	for _, test := range []struct {
		status int
		kind   ErrorKind
	}{
		{status: http.StatusPaymentRequired, kind: ErrorQuota},
		{status: http.StatusTooManyRequests, kind: ErrorRateLimit},
		{status: http.StatusServiceUnavailable, kind: ErrorTransport},
	} {
		t.Run(fmt.Sprintf("HTTP %d", test.status), func(t *testing.T) {
			keyFile := writeTestKey(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, `{"error":{"message":"fixture-secret"}}`)
			}))
			defer server.Close()
			httpClient := &countingHTTPClient{base: &http.Client{}}
			provider := NewOpenRouter(Options{KeyFile: keyFile, BaseURL: server.URL, HTTPClient: httpClient})
			_, err := provider.Generate(context.Background(), testRequest())
			assertProviderError(t, err, test.kind)
			want := int32(3)
			if test.kind == ErrorQuota {
				want = 1
			}
			if got := httpClient.closes.Load(); got != want {
				t.Errorf("response body close count = %d; want %d", got, want)
			}
		})
	}
}

func TestOpenRouterReadsCredentialsLazilyAndUsesKeyFileOverride(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	if got, want := defaultKeyFile(), configDir+"/"+defaultKeyFileSubpath; got != want {
		t.Errorf("defaultKeyFile() = %q, want %q", got, want)
	}
	homeDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", homeDir)
	if got, want := defaultKeyFile(), homeDir+"/.config/"+defaultKeyFileSubpath; got != want {
		t.Errorf("defaultKeyFile() without XDG_CONFIG_HOME = %q, want %q", got, want)
	}
	keyOverride := t.TempDir() + "/override-key"
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv(DefaultKeyFileEnv, keyOverride)
	provider := NewOpenRouter(Options{})
	if provider.keyFile != keyOverride {
		t.Fatalf("keyFile = %q, want override %q", provider.keyFile, keyOverride)
	}
	var reads atomic.Int32
	provider.readFile = func(path string) ([]byte, error) {
		reads.Add(1)
		if path != keyOverride {
			return nil, fmt.Errorf("unexpected path")
		}
		return []byte("lazy-key\n"), nil
	}
	if reads.Load() != 0 {
		t.Fatal("credentials were read during provider construction")
	}
	key, err := provider.apiKey()
	if err != nil || key != "lazy-key" || reads.Load() != 1 {
		t.Fatalf("lazy credential read = (%q, %v), reads %d", key, err, reads.Load())
	}
}

func toolCallStreamBytes(terminal bool) []byte {
	var output strings.Builder
	write := func(data string) { _, _ = fmt.Fprintf(&output, "data: %s\n\n", data) }
	write(`{"id":"completion-1","model":"test/model","object":"chat.completion.chunk","created":1,"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"run_js","arguments":"{}"}}]},"finish_reason":null}]}`)
	write(`{"id":"completion-1","model":"test/model","object":"chat.completion.chunk","created":1,"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
	if terminal {
		write("[DONE]")
	}
	return []byte(output.String())
}

func writeTestKey(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/openrouter-api-key"
	if err := osWriteFile(path, []byte("fixture-secret\n")); err != nil {
		t.Fatal(err)
	}
	return path
}

func osWriteFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}

func testRequest() Request {
	return Request{Model: "test/model", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"test"}`)}}
}

func assertProviderError(t *testing.T, err error, kind ErrorKind) {
	t.Helper()
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) {
		t.Fatalf("error = %v, want *ProviderError", err)
	}
	if providerErr.Kind != kind {
		t.Fatalf("error kind = %q, want %q (error %v)", providerErr.Kind, kind, err)
	}
}
