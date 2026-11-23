package provider

import (
	"context"
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
	profile      Definition
	client       openai.Client
	socketClient openai.Client
	socketHTTP   *configuredHTTPClient
	key          apiKeyFile
	oauth        *credentials.ChatGPT
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
	configured := &configuredHTTPClient{client: client, profile: profile}
	p.socketHTTP = configured
	p.client = newOpenAIClient(profile, &observedHTTPClient{client: configured})
	p.socketClient = newOpenAIClient(profile, configured)
	return p
}

func newOpenAIClient(profile Definition, client HTTPClient) openai.Client {
	return openai.NewClient(
		option.WithBaseURL(profile.BaseURL), option.WithAPIKey(""), option.WithAdminAPIKey(""),
		option.WithOrganization(""), option.WithProject(""),
		option.WithHeaderDel("OpenAI-Organization"), option.WithHeaderDel("OpenAI-Project"),
		option.WithHTTPClient(client), option.WithMaxRetries(0),
	)
}

func (p *OpenAI) Generate(ctx context.Context, request Request) (Response, error) {
	if p.profile.Transport == "websocket" && p.profile.API == "responses" {
		if scope := managedScope(ctx); scope != nil {
			return p.generateManagedWebSocket(ctx, request, scope)
		}
	}
	return generateWithRetry(ctx, request, p.generateOnce)
}

func (p *OpenAI) generateOnce(ctx context.Context, request Request) (Response, error) {
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
	token, err := p.openAIToken(ctx)
	if err != nil {
		if cancelErr := cancellationError(ctx); cancelErr != nil {
			return Response{}, cancelErr
		}
		return Response{}, providerError(ErrorAuthentication, 0, nil)
	}
	state := &callState{requestBody: payload, observe: request.Observe, sessionID: request.sessionID(), reasoningField: p.profile.ReasoningField}
	ctx = context.WithValue(ctx, callContextKey{}, state)
	defer state.closeBody()
	opts := []option.RequestOption{option.WithAPIKey(token), option.WithRequestBody("application/json", payload)}
	sawTerminal := false
	if p.profile.API == "responses" {
		state.responseAPI = "responses"
		stream := p.client.Responses.NewStreaming(ctx, responses.ResponseNewParams{}, opts...)
		defer stream.Close()
		for stream.Next() {
			event := stream.Current()
			if event.Type == "error" || event.Type == "response.completed" || event.Type == "response.failed" || event.Type == "response.incomplete" {
				sawTerminal = true
				break
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
	if err != nil && status != http.StatusOK {
		return Response{}, state.classifyHTTPError(err)
	}
	if p.profile.API == "responses" {
		return nativeStreamResponse(state, sawTerminal)
	}
	if body == nil {
		return Response{}, providerError(ErrorStream, status, nil)
	}
	done, readErr, consumeErr := body.snapshot()
	if failure, ok := consumeErr.(*ProviderError); ok {
		return Response{}, state.streamFailure(failure.Kind)
	}
	if err != nil || readErr != nil || !done {
		return Response{}, providerError(ErrorStream, status, nil)
	}
	if consumeErr != nil {
		return Response{}, providerError(ErrorResponse, status, nil)
	}
	return body.response()
}

func nativeStreamResponse(state *callState, sawTerminal bool) (Response, error) {
	status, body := state.response()
	if body == nil {
		return Response{}, providerError(ErrorStream, status, nil)
	}
	// Prefer the wire failure even if the SDK could not decode the terminal event.
	if kind := body.nativeErrorKind(); kind != "" {
		return Response{}, state.streamFailure(kind)
	}
	_, readErr, _ := body.snapshot()
	if readErr != nil || !sawTerminal {
		return Response{}, providerError(ErrorStream, status, nil)
	}
	return body.response()
}
