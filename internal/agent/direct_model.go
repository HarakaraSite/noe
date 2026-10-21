package agent

import (
	"context"
	"errors"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

// directModelProvider adapts the borrowed public model service to the existing
// core provider boundary. Agent admission, cancellation, usage, and event
// accounting remain in the shared agent path.
type directModelProvider struct {
	service model.Service
}

func (p directModelProvider) Generate(ctx context.Context, request provider.Request) (provider.Response, error) {
	if p.service == nil {
		return provider.Response{}, errors.New("direct agent host has no model service")
	}
	return p.service.Generate(ctx, request)
}

func (p directModelProvider) Decide(ctx context.Context, request decision.Request) (decision.Response, error) {
	if p.service == nil {
		return decision.Response{}, errors.New("direct agent host has no model service")
	}
	return p.service.Decide(ctx, request)
}

func (p directModelProvider) GenerateImage(ctx context.Context, request imageapi.Request) (imageapi.Generated, error) {
	if p.service == nil {
		return imageapi.Generated{}, errors.New("direct agent host has no model service")
	}
	return p.service.GenerateImage(ctx, model.ImageRequest{
		Provider: request.Provider, Model: request.Model, Prompt: request.Prompt,
		Parameters: request.Parameters, RunID: request.RunID,
	})
}
