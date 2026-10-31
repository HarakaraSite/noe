package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

// CodexAuthOptions selects the trusted OAuth profile independently of image options.
type CodexAuthOptions = credentials.Options

type codexImageAdapter struct {
	profile   Definition
	auth      *credentials.CodexImages
	client    HTTPClient
	userAgent string
}

func newCodexImageAdapter(profile Definition, authOptions CodexAuthOptions, client HTTPClient, userAgent string) *codexImageAdapter {
	if client == nil {
		client = &http.Client{}
	}
	if standard, ok := client.(*http.Client); ok {
		copy := *standard
		copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && (req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host) {
				return http.ErrUseLastResponse
			}
			if standard.CheckRedirect != nil {
				return standard.CheckRedirect(req, via)
			}
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		}
		client = &copy
	}
	if userAgent == "" {
		userAgent = "noe/dev"
	}
	return &codexImageAdapter{profile: profile, auth: credentials.NewCodexImages(profile.OAuthFile, profile.OAuthMode, authOptions), client: client, userAgent: userAgent}
}

func (p *codexImageAdapter) GenerateImage(ctx context.Context, request imageapi.Request) (imageapi.Generated, error) {
	if err := request.ValidateGeneration(); err != nil {
		return imageapi.Generated{}, imageapi.DescribeError(err, "generate", "input", imageapi.Generated{})
	}
	return p.call(ctx, "generate", request, nil, p.profile.ImageGeneration)
}

func (p *codexImageAdapter) EditImage(ctx context.Context, request imageapi.EditRequest) (imageapi.Generated, error) {
	return p.call(ctx, "edit", imageapi.Request{Provider: request.Provider, Model: request.Model, Prompt: request.Prompt, Parameters: request.Parameters, RunID: request.RunID}, request.Images, p.profile.ImageEditing)
}

func (p *codexImageAdapter) call(ctx context.Context, operation string, request imageapi.Request, inputs []imageapi.Input, endpoint *ImageGenerationDefinition) (imageapi.Generated, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result := imageapi.Generated{Operation: operation}
	fail := func(code, stage string, status int, providerCode, providerMessage string, secrets []string) (imageapi.Generated, error) {
		details := imageFailure(code, status, result.RequestID, providerCode, providerMessage, "")
		for _, secret := range secrets {
			details.ProviderCode = sanitizeImageMetadata(details.ProviderCode, secret)
			details.ProviderMessage = sanitizeImageMetadata(details.ProviderMessage, secret)
			details.RequestID = sanitizeImageMetadata(details.RequestID, secret)
		}
		return result, imageapi.DescribeError(details, operation, stage, result)
	}
	if endpoint == nil {
		return fail("unsupported", "input", 0, "", "", nil)
	}
	if ctx.Err() != nil {
		return fail("cancelled", "input", 0, "", "", nil)
	}
	profile := p.profile
	profile.ImageGeneration = endpoint
	payload, err := imageRequestBody(profile, request)
	if err != nil {
		return fail("input", "input", 0, "", "", nil)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &fields) != nil {
		return fail("input", "input", 0, "", "", nil)
	}
	delete(fields, "stream") // Codex uses the standalone non-streaming JSON schema.
	for key := range fields {
		if strings.EqualFold(key, "mask") {
			return fail("unsupported", "input", 0, "", "", nil)
		}
	}
	if operation == "edit" {
		images := make([]map[string]string, len(inputs))
		for i, input := range inputs {
			images[i] = map[string]string{"image_url": "data:" + input.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(input.Content)}
		}
		fields["images"], err = json.Marshal(images)
		if err != nil {
			return fail("input", "input", 0, "", "", nil)
		}
	}
	payload, err = json.Marshal(fields)
	if err != nil {
		return fail("input", "input", 0, "", "", nil)
	}
	address, err := url.JoinPath(endpoint.BaseURL, endpoint.Path)
	if err != nil {
		return fail("input", "input", 0, "", "", nil)
	}
	snapshot, err := p.auth.Snapshot(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return fail("cancelled", "authentication", 0, "", "", nil)
		}
		return fail("authentication", "authentication", 0, "", "", nil)
	}
	secrets := append([]string{snapshot.Access, snapshot.AccountID}, snapshot.Secrets...)
	turnID := rand.Text()
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(payload))
		if err != nil {
			return fail("input", "input", 0, "", "", secrets)
		}
		for key, value := range p.profile.Headers {
			req.Header.Set(key, value)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+snapshot.Access)
		req.Header.Set("ChatGPT-Account-ID", snapshot.AccountID)
		req.Header.Set("User-Agent", p.userAgent)
		req.Header.Set("originator", "noe_image_api")
		req.Header.Set("x-codex-image-turn-id", turnID)
		response, requestErr := p.client.Do(req)
		status := 0
		var body []byte
		if response != nil {
			status = response.StatusCode
			result.RequestID = response.Header.Get("x-codex-imagegen-request-id")
			if result.RequestID == "" {
				result.RequestID = response.Header.Get("x-request-id")
			}
			if response.Body != nil {
				var readErr error
				body, readErr = io.ReadAll(response.Body)
				_ = response.Body.Close()
				if requestErr == nil {
					requestErr = readErr
				}
			}
		}
		model, usage, providerCode, providerMessage, refusal, hasError, images := parseImageResponse(body)
		result.ResponseModel = model
		result.Usage = usage
		for _, secret := range secrets {
			result.ResponseModel = sanitizeImageMetadata(result.ResponseModel, secret)
			result.RequestID = sanitizeImageMetadata(result.RequestID, secret)
			result.Usage = sanitizeImageUsage(result.Usage, secret)
		}
		result.RequestID = truncateImageMessage(result.RequestID)
		result.ResponseModel = truncateImageMessage(result.ResponseModel)
		if ctx.Err() != nil {
			return fail("cancelled", "request", status, providerCode, providerMessage, secrets)
		}
		if status == http.StatusUnauthorized && attempt == 0 {
			previous := snapshot
			snapshot, err = p.auth.RecoverUnauthorized(ctx, previous)
			if err != nil || snapshot.Access == previous.Access || snapshot.AccountID != previous.AccountID {
				if ctx.Err() != nil {
					return fail("cancelled", "authentication", status, providerCode, providerMessage, secrets)
				}
				return fail("authentication", "authentication", status, providerCode, providerMessage, secrets)
			}
			secrets = append(secrets, snapshot.Secrets...)
			secrets = append(secrets, snapshot.Access, snapshot.AccountID)
			continue
		}
		if requestErr != nil {
			return fail("transport", "request", status, providerCode, providerMessage, secrets)
		}
		if refusal {
			return fail("refusal", "request", status, providerCode, providerMessage, secrets)
		}
		if status < 200 || status >= 300 || hasError {
			return fail(classifyImageProviderError(status, providerCode, providerMessage), "request", status, providerCode, providerMessage, secrets)
		}
		if len(images) != 1 || images[0] == "" {
			return fail("response", "response", status, providerCode, providerMessage, secrets)
		}
		content, err := base64.StdEncoding.DecodeString(images[0])
		if err != nil {
			return fail("response", "response", status, providerCode, providerMessage, secrets)
		}
		data, err := imageapi.ReadData(ctx, "image-response", content, "")
		if err != nil {
			if ctx.Err() != nil {
				return fail("cancelled", "response", status, providerCode, providerMessage, secrets)
			}
			return fail("response", "response", status, providerCode, providerMessage, secrets)
		}
		result.Content = data.Content
		return result, nil
	}
	return fail("authentication", "authentication", http.StatusUnauthorized, "", "", secrets)
}
