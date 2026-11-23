package provider

import (
	"bytes"
	"context"
	jsonv1 "encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"forge.harakara.site/littleisland/noe/v2/internal/jsonutil"

	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/models/operations"
	"github.com/OpenRouterTeam/go-sdk/retry"
)

const (
	DefaultBaseURL    = "https://openrouter.ai/api/v1"
	DefaultKeyFileEnv = "NOE_OPENROUTER_API_KEY_FILE"
)

// HTTPClient is the injectable HTTP transport used by the SDK.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Options configures the OpenRouter adapter. Credentials are read from the
// explicitly selected KeyFile on the first Generate call, never during
// construction.
type Options struct {
	ConfigFile string
	// BaseDir anchors relative paths in the explicit registry constructor.
	BaseDir string
	// ConfigDir expands ${configDir} in explicit provider definitions. It is
	// deliberately independent of the process user's home/config directories.
	ConfigDir string
	// Env is a caller-owned environment snapshot used only by an explicit
	// registry. Nil means an empty environment.
	Env map[string]string
	// Credentials overrides key/OAuth files by provider ID in an explicit
	// registry. Nonempty fields override the definition and its Env setting.
	Credentials map[string]CredentialFiles
	UserAgent   string
	Profile     *Definition
	KeyFile     string
	BaseURL     string
	HTTPClient  HTTPClient
	CodexAuth   CodexAuthOptions
	// DecisionServerURL is an origin (or gateway prefix), not the Chat base URL.
	DecisionServerURL string
}

// CredentialFiles selects explicit credential files for a provider.
type CredentialFiles struct {
	KeyFile   string
	OAuthFile string
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

	mu           sync.Mutex
	status       int
	body         *observedBody
	retryHeaders http.Header
	errorCode    string
}

func (s *callState) setResponse(status int, body *observedBody, headers http.Header) {
	s.mu.Lock()
	s.status = status
	s.body = body
	s.retryHeaders = headers.Clone()
	s.errorCode = ""
	s.mu.Unlock()
}

func (s *callState) setResponseMetadata(status int, headers http.Header, code string) {
	s.mu.Lock()
	s.status = status
	s.retryHeaders = headers.Clone()
	s.errorCode = code
	s.mu.Unlock()
}

func (s *callState) response() (int, *observedBody) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.body
}

func (s *callState) classifyHTTPError(cause error) error {
	status, body := s.response()
	s.mu.Lock()
	code := s.errorCode
	headers := s.retryHeaders.Clone()
	s.mu.Unlock()
	if code == "" && body != nil {
		code = body.errorCode()
	}
	return classifyHTTPResponseError(status, cause, code, headers)
}

func (s *callState) streamFailure(kind ErrorKind) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	failure := providerError(kind, 0, nil)
	applyRetryMetadata(failure, s.retryHeaders)
	return failure
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
		body.captureErrorCode = response.StatusCode >= http.StatusBadRequest
		body.observe = state.observe
		if state.responseAPI == "responses" {
			body.chat = nil
			body.native = &responsesAccumulator{}
			body.extract = responsesObservations
		} else {
			body.chat.reasoningField = state.reasoningField
			if state.reasoningField != "" {
				body.extract = func(raw jsonv1.RawMessage) []StreamEvent {
					return streamObservationsForField(raw, state.reasoningField)
				}
			}
		}
		state.setResponse(response.StatusCode, body, response.Header)
		response.Body = body
	}
	return response, err
}

func validateGenerationRequest(request Request) (string, error) {
	effort, validEffort := NormalizeEffort(request.Effort)
	if strings.TrimSpace(request.Model) == "" || len(request.Messages) == 0 || request.MaxTokens < 0 || !validEffort ||
		!utf8.ValidString(request.Model) || !utf8.ValidString(request.CacheKey) {
		return "", providerError(ErrorResponse, 0, nil)
	}
	for _, raw := range request.Messages {
		if !jsonutil.IsObject(raw) {
			return "", providerError(ErrorResponse, 0, nil)
		}
	}
	for _, raw := range request.Tools {
		if !jsonutil.IsObject(raw) {
			return "", providerError(ErrorResponse, 0, nil)
		}
	}
	return effort, nil
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
		return Response{}, state.classifyHTTPError(err)
	}
	if result == nil || result.EventStream == nil {
		status, _ := state.response()
		if status != 200 {
			return Response{}, state.classifyHTTPError(nil)
		}
		return Response{}, providerError(ErrorResponse, status, nil)
	}
	defer result.EventStream.Close()
	for result.EventStream.Next() {
		// A provider error terminates the stream even without [DONE] or HTTP EOF.
		if result.EventStream.Value().GetData().Error != nil {
			break
		}
	}
	if cancelErr := cancellationError(ctx); cancelErr != nil {
		return Response{}, cancelErr
	}
	_, body := state.response()
	if body == nil {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	// [DONE] terminates an SSE completion, even if the HTTP connection stays
	// open. Do not wait for EOF; the deferred owner closes stream/body.
	done, readErr, consumeErr := body.snapshot()
	if failure, ok := consumeErr.(*ProviderError); ok {
		return Response{}, state.streamFailure(failure.Kind)
	}
	if result.EventStream.Err() != nil || readErr != nil || !done {
		return Response{}, providerError(ErrorStream, 0, nil)
	}
	if consumeErr != nil {
		return Response{}, providerError(ErrorResponse, 0, nil)
	}
	response, err := body.response()
	if err != nil {
		return Response{}, err
	}
	if cancelErr := cancellationError(ctx); cancelErr != nil {
		return Response{}, cancelErr
	}
	return response, nil
}
