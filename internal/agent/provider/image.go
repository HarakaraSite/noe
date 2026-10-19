package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"

	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/models/operations"
	"github.com/OpenRouterTeam/go-sdk/retry"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

type imageAdapter struct {
	sdk        string
	profile    Definition
	key        *apiKeyFile
	openrouter *openrouter.OpenRouter
	openai     openai.Client
}

type imageCallContextKey struct{}

type imageCallState struct {
	payload []byte

	mu          sync.Mutex
	status      int
	requestID   string
	response    bytes.Buffer
	authFailure bool
}

func (s *imageCallState) recordResponse(response *http.Response) {
	if response == nil {
		return
	}
	s.mu.Lock()
	s.status = response.StatusCode
	s.requestID = response.Header.Get("x-request-id")
	if s.requestID == "" {
		s.requestID = response.Header.Get("x-openrouter-request-id")
	}
	if s.requestID == "" {
		s.requestID = response.Header.Get("request-id")
	}
	s.mu.Unlock()
}

func (s *imageCallState) recordBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	s.mu.Lock()
	_, _ = s.response.Write(data)
	s.mu.Unlock()
}

func (s *imageCallState) snapshot() (int, string, []byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.requestID, append([]byte(nil), s.response.Bytes()...), s.authFailure
}

func newImageAdapter(profile Definition, key *apiKeyFile, client HTTPClient) *imageAdapter {
	if client == nil {
		client = &http.Client{}
	}
	imageProfile := profile
	imageProfile.BaseURL = profile.ImageGeneration.BaseURL
	imageProfile.Path = profile.ImageGeneration.Path
	transport := &imageHTTPClient{client: &configuredHTTPClient{client: client, profile: imageProfile}}
	p := &imageAdapter{sdk: profile.SDK, profile: imageProfile, key: key}
	switch profile.SDK {
	case "openai":
		p.openai = openai.NewClient(
			option.WithBaseURL(imageProfile.BaseURL), option.WithAPIKey(""), option.WithAdminAPIKey(""),
			option.WithOrganization(""), option.WithProject(""),
			option.WithHeaderDel("OpenAI-Organization"), option.WithHeaderDel("OpenAI-Project"),
			option.WithHTTPClient(transport), option.WithMaxRetries(0),
		)
	default:
		p.openrouter = openrouter.New(
			openrouter.WithServerURL(imageProfile.BaseURL),
			openrouter.WithClient(transport),
			openrouter.WithSecuritySource(p.securitySource),
			openrouter.WithRetryConfig(retry.Config{Strategy: "none"}),
		)
	}
	return p
}

// imageHTTPClient keeps provider-specific SDK request construction while
// applying the shared host configuration and retaining only response data that
// the image adapter needs to classify the result.
type imageHTTPClient struct{ client HTTPClient }

func (c *imageHTTPClient) Do(request *http.Request) (*http.Response, error) {
	state, _ := request.Context().Value(imageCallContextKey{}).(*imageCallState)
	if state != nil {
		request = request.Clone(request.Context())
		if request.Body != nil {
			_ = request.Body.Close()
		}
		payload := append([]byte(nil), state.payload...)
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
		state.recordResponse(response)
		body := response.Body
		if body == nil {
			body = http.NoBody
		}
		response.Body = &imageResponseBody{ReadCloser: body, state: state}
	}
	return response, err
}

type imageResponseBody struct {
	io.ReadCloser
	state *imageCallState
}

func (b *imageResponseBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	b.state.recordBytes(data[:n])
	return n, err
}

func (b *imageResponseBody) Close() error {
	_, _ = io.Copy(io.Discard, b)
	return b.ReadCloser.Close()
}

func (p *imageAdapter) securitySource(ctx context.Context) (components.Security, error) {
	if err := cancellationError(ctx); err != nil {
		return components.Security{}, err
	}
	if p.key == nil {
		return components.Security{}, errors.New("image provider authentication unavailable")
	}
	key, err := p.key.apiKey()
	if err != nil {
		if state, _ := ctx.Value(imageCallContextKey{}).(*imageCallState); state != nil {
			state.mu.Lock()
			state.authFailure = true
			state.mu.Unlock()
		}
		return components.Security{}, errors.New("image provider authentication unavailable")
	}
	return components.Security{APIKey: &key}, nil
}

func imageRequestBody(profile Definition, request imageapi.Request) ([]byte, error) {
	fields := make(map[string]json.RawMessage)
	if profile.ImageGeneration != nil {
		for key, value := range profile.ImageGeneration.Parameters {
			fields[key] = append(json.RawMessage(nil), value...)
		}
	}
	for key, value := range request.Parameters {
		fields[key] = append(json.RawMessage(nil), value...)
	}
	// Configuration parameters can contain provider-specific options, but the
	// request identity and cardinality are owned by NOE.
	for key := range fields {
		switch strings.ToLower(key) {
		case "model", "prompt", "n", "stream", "provider", "baseurl", "path", "output", "api_key", "authorization", "headers", "session_id", "prompt_cache_key", "input_references", "image", "images":
			delete(fields, key)
		}
	}
	for key, value := range map[string]any{
		"model":  request.Model,
		"prompt": request.Prompt,
		"n":      1,
		"stream": false,
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fields[key] = encoded
	}
	return json.Marshal(fields)
}

func (p *imageAdapter) GenerateImage(ctx context.Context, request imageapi.Request) (imageapi.Generated, error) {
	if err := request.Validate(); err != nil {
		return imageapi.Generated{}, imageapi.AsError(err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return imageapi.Generated{}, imageFailure("cancelled", 0, "", "", "", "")
	}
	if p.profile.Auth == "chatgpt-oauth" {
		return imageapi.Generated{}, imageFailure("unsupported", 0, "", "", "", "")
	}
	payload, err := imageRequestBody(p.profile, request)
	if err != nil {
		return imageapi.Generated{}, imageFailure("input", 0, "", "", "", "")
	}
	state := &imageCallState{payload: payload}
	ctx = context.WithValue(ctx, callContextKey{}, &callState{requestBody: payload, runID: request.RunID})
	ctx = context.WithValue(ctx, imageCallContextKey{}, state)

	var sdkErr error
	sdkResponsePresent := false
	switch p.sdk {
	case "openai":
		var token string
		if p.profile.Auth != "none" {
			if p.key == nil {
				return imageapi.Generated{}, imageFailure("authentication", 0, "", "", "", "")
			}
			token, err = p.key.apiKey()
			if err != nil {
				if ctx.Err() != nil {
					return imageapi.Generated{}, imageFailure("cancelled", 0, "", "", "", "")
				}
				return imageapi.Generated{}, imageFailure("authentication", 0, "", "", "", "")
			}
		}
		response, callErr := p.openai.Images.Generate(ctx, openai.ImageGenerateParams{},
			option.WithAPIKey(token), option.WithRequestBody("application/json", payload))
		sdkErr = callErr
		sdkResponsePresent = response != nil
	default:
		response, callErr := p.openrouter.Images.Generate(ctx, components.ImageGenerationRequest{}, operations.WithRetries(retry.Config{Strategy: "none"}))
		sdkErr = callErr
		sdkResponsePresent = response != nil && response.ImageGenerationResponse != nil
	}
	if ctx.Err() != nil {
		return imageapi.Generated{}, imageFailure("cancelled", 0, "", "", "", "")
	}
	status, requestID, responseBody, authFailure := state.snapshot()
	responseModel, usage, providerCode, providerMessage, refusal, hasProviderError, imageData := parseImageResponse(responseBody)
	if authFailure {
		return imageapi.Generated{}, imageFailure("authentication", status, requestID, providerCode, providerMessage, p.credential())
	}
	if refusal {
		return imageapi.Generated{}, imageFailure("refusal", status, requestID, providerCode, providerMessage, p.credential())
	}
	if hasProviderError {
		code := classifyImageProviderError(status, providerCode, providerMessage)
		return imageapi.Generated{}, imageFailure(code, status, requestID, providerCode, providerMessage, p.credential())
	}
	if status < 200 || status >= 300 {
		code := classifyImageProviderError(status, providerCode, providerMessage)
		return imageapi.Generated{}, imageFailure(code, status, requestID, providerCode, providerMessage, p.credential())
	}
	if sdkErr != nil || !sdkResponsePresent {
		return imageapi.Generated{}, imageFailure("response", status, requestID, providerCode, providerMessage, p.credential())
	}
	if ctx.Err() != nil {
		return imageapi.Generated{}, imageFailure("cancelled", 0, "", "", "", "")
	}
	if len(imageData) != 1 || imageData[0] == "" {
		return imageapi.Generated{}, imageFailure("response", status, requestID, providerCode, providerMessage, p.credential())
	}
	content, err := base64.StdEncoding.DecodeString(imageData[0])
	if err != nil {
		return imageapi.Generated{}, imageFailure("response", status, requestID, providerCode, providerMessage, p.credential())
	}
	credential := p.credential()
	return imageapi.Generated{
		Content:       content,
		ResponseModel: truncateImageMessage(sanitizeImageMetadata(responseModel, credential)),
		RequestID:     truncateImageMessage(sanitizeImageMetadata(requestID, credential)),
		Usage:         sanitizeImageUsage(usage, credential),
	}, nil
}

func (p *imageAdapter) credential() string {
	if p.key == nil {
		return ""
	}
	p.key.keyMu.Lock()
	defer p.key.keyMu.Unlock()
	return p.key.key
}

type imageResponseFields struct {
	ID      string            `json:"id"`
	Model   string            `json:"model"`
	Data    []json.RawMessage `json:"data"`
	Usage   json.RawMessage   `json:"usage"`
	Refusal string            `json:"refusal"`
	Blocked bool              `json:"blocked"`
	Error   json.RawMessage   `json:"error"`
}

type imageDataFields struct {
	B64JSON string `json:"b64_json"`
}

func parseImageResponse(body []byte) (model string, usage json.RawMessage, code, message string, refusal, hasError bool, images []string) {
	var response imageResponseFields
	if json.Unmarshal(body, &response) != nil {
		return "", nil, "", "", false, false, nil
	}
	model = response.Model
	if len(response.Usage) > 0 && json.Valid(response.Usage) {
		usage = append(json.RawMessage(nil), response.Usage...)
	}
	if strings.TrimSpace(response.Refusal) != "" {
		refusal = true
		message = response.Refusal
		code = "refusal"
	} else if response.Blocked {
		refusal = true
		code = "refusal"
	}
	if len(response.Error) > 0 && string(response.Error) != "null" {
		hasError = true
		code, message, refusal = parseProviderImageError(response.Error)
	}
	for _, raw := range response.Data {
		var entry imageDataFields
		if json.Unmarshal(raw, &entry) != nil {
			images = append(images, "")
			continue
		}
		images = append(images, entry.B64JSON)
	}
	return model, usage, code, message, refusal, hasError, images
}

func parseProviderImageError(raw json.RawMessage) (code, message string, refusal bool) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return "", "", false
	}
	code = rawString(fields["code"])
	if code == "" {
		code = rawString(fields["type"])
	}
	message = rawString(fields["message"])
	refusal = isImageRefusalCode(code) || isImageRefusalCode(rawString(fields["type"]))
	return code, message, refusal
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	if len(raw) <= 80 && (raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9') {
		return string(raw)
	}
	return ""
}

func normalizeImageCode(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "-", "_"))
}

func isImageRefusalCode(value string) bool {
	switch normalizeImageCode(value) {
	case "moderation_blocked", "content_policy_violation", "content_filter", "content_filter_violation", "blocked_by_policy", "refusal", "safety_system_blocked":
		return true
	default:
		return false
	}
}

func classifyImageProviderError(status int, code, message string) string {
	normalized := normalizeImageCode(code)
	messageLower := strings.ToLower(message)
	if isImageRefusalCode(normalized) {
		return "refusal"
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return "authentication"
	}
	if strings.Contains(messageLower, "not supported") || strings.Contains(messageLower, "does not support") || strings.Contains(messageLower, "unsupported") {
		return "unsupported"
	}

	switch normalized {
	case "insufficient_quota", "quota_exceeded", "billing_hard_limit_reached", "usage_limit_exceeded":
		return "quota"
	case "rate_limit_exceeded", "too_many_requests", "rate_limited":
		return "rate_limit"
	case "unsupported_feature", "unsupported_operation", "unsupported_parameter", "model_not_supported", "not_supported":
		return "unsupported"
	case "invalid_request_error", "invalid_prompt", "invalid_parameter", "invalid_image", "parameter_missing", "request_validation_error":
		return "input"
	}
	switch status {
	case http.StatusPaymentRequired:
		return "quota"
	case http.StatusTooManyRequests:
		return "rate_limit"
	}
	if status >= 500 {
		return "transport"
	}

	switch {
	case status == 0:
		return "transport"
	default:
		return "response"
	}
}

var (
	imageDataURLPattern     = regexp.MustCompile(`(?i)data:[^\s,;]+(?:/[^\s,;]+)?(?:;[^,\s]*)?,[^\s"'<>]+`)
	imageShortBase64Pattern = regexp.MustCompile(`[A-Za-z0-9+/]{32,}={0,2}`)
	imageBase64Pattern      = regexp.MustCompile(`[A-Za-z0-9+/]{96,}={0,2}`)
	imageBearerPattern      = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/-]+=*`)
	imageCredentialPattern  = regexp.MustCompile(`(?i)(?:sk-(?:proj-)?[A-Za-z0-9_-]{8,}|or-v1-[A-Za-z0-9_-]{8,})`)
)

func sanitizeImageMetadata(value, credential string) string {
	if credential != "" {
		value = strings.ReplaceAll(value, credential, "[redacted]")
	}
	value = imageDataURLPattern.ReplaceAllString(value, "[redacted image data]")
	value = imageBearerPattern.ReplaceAllString(value, "Bearer [redacted]")
	value = imageCredentialPattern.ReplaceAllString(value, "[redacted credential]")
	value = imageShortBase64Pattern.ReplaceAllStringFunc(value, func(candidate string) string {
		decoded, err := base64.StdEncoding.DecodeString(candidate)
		if err == nil {
			switch http.DetectContentType(decoded) {
			case "image/png", "image/jpeg", "image/webp":
				return "[redacted image data]"
			}
		}
		return candidate
	})
	value = imageBase64Pattern.ReplaceAllString(value, "[redacted data]")
	return value
}

func sanitizeImageUsage(usage json.RawMessage, credential string) json.RawMessage {
	if len(usage) == 0 || !json.Valid(usage) {
		return nil
	}
	text := sanitizeImageMetadata(string(usage), credential)
	if strings.Contains(text, "[redacted") {
		return nil
	}
	return json.RawMessage(text)
}

func imageFailure(code string, status int, requestID, providerCode, providerMessage, credential string) *imageapi.Error {
	messages := map[string]string{
		"input":          "image provider rejected the request input",
		"refusal":        "image provider refused the request",
		"authentication": "image provider authentication failed",
		"quota":          "image provider usage limit reached",
		"rate_limit":     "image provider rate limit reached",
		"unsupported":    "image generation is unsupported by this provider",
		"transport":      "image provider transport failed",
		"response":       "invalid image provider response",
		"filesystem":     "image output could not be saved",
		"cancelled":      "image generation request cancelled",
	}
	if messages[code] == "" {
		code = "response"
	}
	return &imageapi.Error{
		Code:            code,
		Message:         messages[code],
		ProviderCode:    truncateImageMessage(sanitizeImageMetadata(providerCode, credential)),
		ProviderMessage: truncateImageMessage(sanitizeImageMetadata(providerMessage, credential)),
		StatusCode:      status,
		RequestID:       truncateImageMessage(sanitizeImageMetadata(requestID, credential)),
	}
}

func truncateImageMessage(value string) string {
	if len(value) > 512 {
		return value[:512]
	}
	return value
}

func (r *Registry) GenerateImage(ctx context.Context, request imageapi.Request) (imageapi.Generated, error) {
	if err := request.Validate(); err != nil {
		return imageapi.Generated{}, imageapi.AsError(err)
	}
	resolved, err := r.resolve(request.Provider, request.Model)
	if err != nil {
		var imageErr *imageapi.Error
		if errors.As(err, &imageErr) {
			return imageapi.Generated{}, imageErr
		}
		return imageapi.Generated{}, imageFailure("input", 0, "", "", "", "")
	}
	var profile Definition
	var key *apiKeyFile
	switch configured := resolved.(type) {
	case *OpenRouter:
		profile = configured.profile
		key = &configured.apiKeyFile
	case *OpenAI:
		profile = configured.profile
		key = &configured.key
	default:
		return imageapi.Generated{}, imageFailure("unsupported", 0, "", "", "", "")
	}
	if profile.ImageGeneration == nil || profile.Auth == "chatgpt-oauth" {
		return imageapi.Generated{}, imageFailure("unsupported", 0, "", "", "", "")
	}
	cacheKey := request.Provider + "\x00" + request.Model
	r.mu.Lock()
	imageProvider := r.imageClients[cacheKey]
	if imageProvider == nil {
		imageProvider = newImageAdapter(profile, key, r.opts.HTTPClient)
		r.imageClients[cacheKey] = imageProvider
	}
	r.mu.Unlock()
	return imageProvider.GenerateImage(ctx, request)
}
