package agent

import (
	"context"
	"errors"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

// agentTestModelService adapts legacy fixture functions to the public model
// boundary used by Host. Production code never accepts provider adapters.
type agentTestModelService struct {
	chat    provider.Provider
	decider decision.Provider
	imager  imageapi.Provider
	catalog []model.Model
}

func (s agentTestModelService) ListModels(ctx context.Context, query model.ModelQuery) ([]model.Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var result []model.Model
	for _, entry := range s.catalog {
		if query.Provider != "" && entry.Provider != query.Provider {
			continue
		}
		if query.Kind != "" && entry.Kind != query.Kind {
			continue
		}
		result = append(result, entry)
	}
	return result, nil
}

func (s agentTestModelService) Generate(ctx context.Context, request model.Request) (model.Response, error) {
	if s.chat == nil {
		return model.Response{}, errors.New("fixture chat provider is unavailable")
	}
	return s.chat.Generate(ctx, request)
}

func (s agentTestModelService) Decide(ctx context.Context, request model.DecisionRequest) (model.DecisionResponse, error) {
	if s.decider != nil {
		return s.decider.Decide(ctx, request)
	}
	if decider, ok := s.chat.(decision.Provider); ok {
		return decider.Decide(ctx, request)
	}
	return model.DecisionResponse{}, errors.New("fixture decision provider is unavailable")
}

func (s agentTestModelService) GenerateImage(ctx context.Context, request model.ImageRequest) (model.GeneratedImage, error) {
	if s.imager != nil {
		return s.imager.GenerateImage(ctx, imageapi.Request{
			Provider: request.Provider, Model: request.Model, Prompt: request.Prompt,
			Parameters: request.Parameters, RunID: request.RunID,
		})
	}
	if imager, ok := s.chat.(imageapi.Provider); ok {
		return imager.GenerateImage(ctx, imageapi.Request{
			Provider: request.Provider, Model: request.Model, Prompt: request.Prompt,
			Parameters: request.Parameters, RunID: request.RunID,
		})
	}
	return model.GeneratedImage{}, errors.New("fixture image provider is unavailable")
}

func modelsWithChat(p provider.Provider) model.Service {
	return agentTestModelService{chat: p}
}

func modelsWithDecision(p decision.Provider) model.Service {
	return agentTestModelService{decider: p}
}
