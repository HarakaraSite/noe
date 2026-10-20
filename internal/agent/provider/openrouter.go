package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/models/operations"
	"github.com/OpenRouterTeam/go-sdk/retry"
)

const (
	DefaultBaseURL        = "https://openrouter.ai/api/v1"
	DefaultKeyFileEnv     = "NOE_OPENROUTER_API_KEY_FILE"
	defaultKeyFileSubpath = "noe/credentials/openrouter-api-key"
)

// HTTPClient is the injectable HTTP transport used by the SDK.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Options configures the OpenRouter adapter. An empty KeyFile uses
// NOE_OPENROUTER_API_KEY_FILE, then the XDG/default config location. KeyFile is
// read on the first Generate call, never during construction.
type Options struct {
	ConfigFile string
	UserAgent  string
	Profile    *Definition
	KeyFile    string
	BaseURL    string
	HTTPClient HTTPClient
	// DecisionServerURL is an origin (or gateway prefix), not the Chat base URL.
	DecisionServerURL string
}

// OpenRouter implements Provider using OpenRouter's official Go SDK.
type OpenRouter struct {
	apiKeyFile
	profile           Definition
	client            *openrouter.OpenRouter
	decisionServerURL string
}

// NewOpenRouter constructs an adapter without reading credentials or making a
// network request.
func NewOpenRouter(options Options) *OpenRouter {
	profile := Definition{API: "chat-completions", Path: "/chat/completions", SessionField: "session_id", ReasoningField: "reasoning", EffortField: "reasoning.effort"}
	if options.Profile != nil {
		profile = *options.Profile
	}
	keyFile := options.KeyFile
	if keyFile == "" && options.Profile == nil {
		keyFile = os.Getenv(DefaultKeyFileEnv)
	}
	if keyFile == "" && options.Profile == nil {
		keyFile = defaultKeyFile()
	}
	baseURL := options.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		// No http.Client.Timeout: Generate is bounded by its caller's context.
		httpClient = &http.Client{}
	}
	decisionServerURL := options.DecisionServerURL
	if decisionServerURL == "" {
		decisionServerURL = strings.TrimSuffix(strings.TrimRight(baseURL, "/"), "/api/v1")
	}
	profile.BaseURL = baseURL
	p := &OpenRouter{profile: profile, apiKeyFile: apiKeyFile{keyFile: keyFile, readFile: os.ReadFile}, decisionServerURL: decisionServerURL}
	httpClient = &configuredHTTPClient{client: httpClient, profile: profile}
	noRetries := retry.Config{Strategy: "none"}
	p.client = openrouter.New(
		openrouter.WithServerURL(baseURL),
		openrouter.WithClient(&decisionHTTPClient{client: &observedHTTPClient{client: httpClient}}),
		openrouter.WithSecuritySource(p.securitySource),
		openrouter.WithRetryConfig(noRetries),
	)
	return p
}

func defaultKeyFile() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return ""
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, defaultKeyFileSubpath)
}

func (p *OpenRouter) securitySource(ctx context.Context) (components.Security, error) {
	if err := cancellationError(ctx); err != nil {
		return components.Security{}, err
	}
	key, err := p.apiKey()
	if err != nil {
		return components.Security{}, providerError(ErrorAuthentication, 0, nil)
	}
	return components.Security{APIKey: &key}, nil
}

type callContextKey struct{}

type callState struct {
	requestBody    []byte
	observe        func(StreamEvent)
	sessionID      string
	responseAPI    string
	reasoningField string

	mu     sync.Mutex
	status int
	body   *observedBody
}

func (s *callState) setResponse(status int, body *observedBody) {
	s.mu.Lock()
	s.status = status
	s.body = body
	s.mu.Unlock()
}

func (s *callState) response() (int, *observedBody) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.body
}

func (s *callState) closeBody() {
	_, body := s.response()
	if body != nil {
		_ = body.Close()
	}
}

type observedHTTPClient struct {
	client HTTPClient
}

func (c *observedHTTPClient) Do(request *http.Request) (*http.Response, error) {
	state, _ := request.Context().Value(callContextKey{}).(*callState)
	if state != nil {
		if request.Body != nil {
			_ = request.Body.Close()
		}
		payload := append([]byte(nil), state.requestBody...)
		request.Body = io.NopCloser(bytes.NewReader(payload))
		request.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(payload)), nil
		}
		request.ContentLength = int64(len(payload))
		request.TransferEncoding = nil
		request.Header.Del("Content-Length")
	}
	response, err := c.client.Do(request)
	if response != nil && state != nil {
		var source io.ReadCloser = response.Body
		if source == nil {
			source = http.NoBody
		}
		body := newObservedBody(source)
		body.observe = state.observe
		if state.responseAPI == "responses" {
			body.extract = responsesObservations
		} else if state.reasoningField != "" {
			body.extract = func(raw json.RawMessage) []StreamEvent { return streamObservationsForField(raw, state.reasoningField) }
		}
		state.setResponse(response.StatusCode, body)
		response.Body = body
	}
	return response, err
}

type chatRequestBody struct {
	Model         string            `json:"model"`
	Messages      []json.RawMessage `json:"messages"`
	Tools         []json.RawMessage `json:"tools,omitempty"`
	Stream        bool              `json:"stream"`
	StreamOptions struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options"`
	SessionID string           `json:"session_id,omitempty"`
	MaxTokens *int             `json:"max_tokens,omitempty"`
	Reasoning *reasoningConfig `json:"reasoning,omitempty"`
}

type reasoningConfig struct {
	Effort string `json:"effort"`
}

func makeChatRequestBody(request Request) ([]byte, error) {
	effort, validEffort := NormalizeEffort(request.Effort)
	if strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 || request.MaxTokens < 0 || !validEffort {
		return nil, providerError(ErrorResponse, 0, nil)
	}
	for _, raw := range request.Messages {
		if !isJSONObject(raw) {
			return nil, providerError(ErrorResponse, 0, nil)
		}
	}
	for _, raw := range request.Tools {
		if !isJSONObject(raw) {
			return nil, providerError(ErrorResponse, 0, nil)
		}
	}
	body := chatRequestBody{
		Model:     request.Model,
		Messages:  request.Messages,
		Tools:     request.Tools,
		Stream:    true,
		SessionID: request.CacheKey,
	}
	body.StreamOptions.IncludeUsage = true
	if effort != "auto" {
		body.Reasoning = &reasoningConfig{Effort: effort}
	}
	if request.MaxTokens > 0 {
		maxTokens := request.MaxTokens
		body.MaxTokens = &maxTokens
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, providerError(ErrorResponse, 0, nil)
	}
	return encoded, nil
}

func isJSONObject(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

// Generate sends a streaming Chat Completions request and returns an opaque
// assistant message plus the finish reason and usage object.
func (p *OpenRouter) Generate(ctx context.Context, request Request) (Response, error) {
	return generateWithRetry(ctx, request, p.generateOnce)
}

func (p *OpenRouter) generateOnce(ctx context.Context, request Request) (Response, error) {
	if err := cancellationError(ctx); err != nil {
		return Response{}, err
	}
	if p.profile.API == "responses" {
		return p.generateResponses(ctx, request)
	}
	payload, err := p.chatRequestBody(request)
	if err != nil {
		return Response{}, err
	}
	state := &callState{requestBody: payload, observe: request.Observe, sessionID: request.sessionID(), reasoningField: p.profile.ReasoningField}
	ctx = context.WithValue(ctx, callContextKey{}, state)
	defer state.closeBody()

	model := request.Model
	streaming := true
	sdkRequest := components.ChatRequest{Model: &model, Stream: &streaming}
	noRetries := retry.Config{Strategy: "none"}
	result, err := p.client.Chat.Send(ctx, sdkRequest, nil,
		operations.WithRetries(noRetries),
		operations.WithAcceptHeaderOverride(operations.AcceptHeaderEnumTextEventStream),
	)
	if err != nil {
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return Response{}, cancelErr
		}
		status, _ := state.response()
		return Response{}, classifyHTTPError(status, err)
	}
	if result == nil || result.EventStream == nil {
		status, _ := state.response()
		if status != 200 {
			return Response{}, classifyHTTPError(status, nil)
		}
		return Response{}, providerError(ErrorResponse, status, nil)
	}
	defer result.EventStream.Close()
	for result.EventStream.Next() {
		// The raw event payload is collected by observedBody; the SDK iterator is
		// used to validate and deliver the provider's official SSE response type.
	}
	if cancelErr := cancellationError(ctx); cancelErr != nil {
		return Response{}, cancelErr
	}
	if result.EventStream.Err() != nil {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	_, body := state.response()
	if body == nil {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	// [DONE] terminates an SSE completion, even if the HTTP connection stays
	// open. Do not wait for EOF; the deferred owner closes stream/body.
	events, done, readErr := body.snapshot()
	if readErr != nil || !done {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	accumulator := newStreamAccumulator()
	accumulator.reasoningField = p.profile.ReasoningField
	if err := accumulator.consume(events); err != nil {
		return Response{}, providerError(ErrorResponse, 0, nil)
	}
	response, err := accumulator.response()
	if err != nil {
		return Response{}, err
	}
	if cancelErr := cancellationError(ctx); cancelErr != nil {
		return Response{}, cancelErr
	}
	return response, nil
}
