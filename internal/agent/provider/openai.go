package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"os"

	"forge.harakara.site/littleisland/noe/v2/internal/credentials"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

// OpenAI uses the official OpenAI SDK for public and compatible endpoints.
// Raw JSON conversion and observation are shared with the OpenRouter adapter.
type OpenAI struct {
	profile Definition
	client  openai.Client
	key     apiKeyFile
	oauth   *credentials.ChatGPT
}

func NewOpenAI(options Options) *OpenAI {
	profile := *options.Profile
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	p := &OpenAI{profile: profile, key: apiKeyFile{keyFile: profile.KeyFile, readFile: os.ReadFile}}
	if profile.Auth == "chatgpt-oauth" {
		p.oauth = credentials.NewChatGPT(profile.OAuthFile, credentials.Options{HTTPClient: client})
	}
	client = &observedHTTPClient{client: &configuredHTTPClient{client: client, profile: profile}}
	p.client = openai.NewClient(
		option.WithBaseURL(profile.BaseURL), option.WithAPIKey(""), option.WithAdminAPIKey(""),
		option.WithOrganization(""), option.WithProject(""),
		option.WithHeaderDel("OpenAI-Organization"), option.WithHeaderDel("OpenAI-Project"),
		option.WithHTTPClient(client), option.WithMaxRetries(0),
	)
	return p
}

func (p *OpenAI) Generate(ctx context.Context, request Request) (Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := cancellationError(ctx); err != nil {
		return Response{}, err
	}
	var payload []byte
	var err error
	if p.profile.API == "responses" {
		payload, err = configuredResponsesRequestBody(p.profile, request)
	} else {
		payload, err = configuredChatRequestBody(p.profile, request)
	}
	if err != nil {
		return Response{}, providerError(ErrorResponse, 0, nil)
	}
	token := ""
	switch p.profile.Auth {
	case "chatgpt-oauth":
		token, err = p.oauth.Token(ctx)
	case "none":
	default:
		token, err = p.key.apiKey()
	}
	if err != nil {
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return Response{}, cancelErr
		}
		return Response{}, providerError(ErrorAuthentication, 0, nil)
	}
	state := &callState{requestBody: payload, observe: request.Observe, runID: request.RunID, reasoningField: p.profile.ReasoningField}
	ctx = context.WithValue(ctx, callContextKey{}, state)
	defer state.closeBody()
	opts := []option.RequestOption{option.WithAPIKey(token), option.WithRequestBody("application/json", payload)}
	completed := false
	if p.profile.API == "responses" {
		state.responseAPI = "responses"
		stream := p.client.Responses.NewStreaming(ctx, responses.ResponseNewParams{}, opts...)
		defer stream.Close()
		for stream.Next() {
			event := stream.Current()
			if event.Type == "response.completed" {
				completed = true
				break
			}
			if event.Type == "response.failed" || event.Type == "response.incomplete" {
				return Response{}, nativeStreamFailure(state)
			}
		}
		err = stream.Err()
	} else {
		stream := p.client.Chat.Completions.NewStreaming(ctx, openai.ChatCompletionNewParams{}, opts...)
		defer stream.Close()
		for stream.Next() {
		}
		err = stream.Err()
	}
	if cancelErr := cancellationError(ctx); cancelErr != nil {
		return Response{}, cancelErr
	}
	status, body := state.response()
	if err != nil {
		if status != http.StatusOK {
			return Response{}, classifyHTTPError(status, err)
		}
		return Response{}, providerError(ErrorStream, status, nil)
	}
	if body == nil {
		return Response{}, providerError(ErrorStream, status, nil)
	}
	events, done, readErr := body.snapshot()
	if readErr != nil {
		return Response{}, providerError(ErrorStream, status, nil)
	}
	if p.profile.API == "responses" {
		if !completed {
			return Response{}, providerError(ErrorStream, status, nil)
		}
		return responseFromEvents(events)
	}
	if !done {
		return Response{}, providerError(ErrorStream, status, nil)
	}
	accumulator := newStreamAccumulator()
	accumulator.reasoningField = p.profile.ReasoningField
	if err := accumulator.consume(events); err != nil {
		return Response{}, providerError(ErrorResponse, status, nil)
	}
	return accumulator.response()
}

// ChatGPT usage-limit failures can arrive after the HTTP 200 stream starts.
func nativeStreamFailure(state *callState) error {
	_, body := state.response()
	if body != nil {
		events, _, _ := body.snapshot()
		for _, raw := range events {
			var event struct {
				Response struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				} `json:"response"`
			}
			if json.Unmarshal(raw, &event) == nil {
				switch event.Response.Error.Code {
				case "subscription_sharing_usage_limit_exceeded", "subscription_sharing_usage_unavailable":
					return providerError(ErrorQuota, 0, nil)
				}
			}
		}
	}
	return providerError(ErrorResponse, 0, nil)
}
