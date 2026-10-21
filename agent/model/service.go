package model

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

// ErrUnsupported reports that a selected custom provider does not implement
// the requested operation.
var ErrUnsupported = errors.New("model operation unsupported")

// Options selects provider configuration and borrowed model transports.
// Explicit inputs are snapshotted by NewService. Relative paths are anchored
// to BaseDir, which defaults to the process working directory at construction.
type Options struct {
	BaseDir     string
	ConfigFile  string
	ConfigDir   string
	Env         map[string]string
	Credentials map[string]CredentialFiles
	UserAgent   string
	HTTPClient  HTTPClient

	// These fields preserve the existing OpenRouter-specific overrides.
	KeyFile           string
	BaseURL           string
	DecisionServerURL string

	// Custom implementations take precedence for their operation. An
	// unsupported operation on a selected custom Provider is not redirected to
	// a built-in provider.
	Provider         Provider
	DecisionProvider DecisionProvider
	ImageProvider    ImageProvider
}

// Service is the standalone Go entry for catalog and model operations.
type Service interface {
	ListModels(context.Context, ModelQuery) ([]Model, error)
	Generate(context.Context, Request) (Response, error)
	Decide(context.Context, DecisionRequest) (DecisionResponse, error)
	GenerateImage(context.Context, ImageRequest) (GeneratedImage, error)
}

type service struct {
	registry         *provider.Registry
	provider         Provider
	decisionProvider DecisionProvider
	imageProvider    ImageProvider
}

// NewService snapshots Go input and normalizes explicit paths. It does not
// read configuration or credential files, make HTTP requests, or inspect
// process environment variables.
func NewService(options Options) (Service, error) {
	baseDir := options.BaseDir
	if baseDir == "" {
		var err error
		baseDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("model: resolve base directory: %w", err)
		}
	}
	baseDir, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("model: resolve base directory: %w", err)
	}

	configFile, err := resolveInputPath(baseDir, options.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("model: resolve config file: %w", err)
	}
	configDir, err := resolveInputPath(baseDir, options.ConfigDir)
	if err != nil {
		return nil, fmt.Errorf("model: resolve config directory: %w", err)
	}

	env := make(map[string]string, len(options.Env))
	for key, value := range options.Env {
		env[key] = value
	}
	credentials := make(map[string]CredentialFiles, len(options.Credentials))
	for name, files := range options.Credentials {
		credentials[name] = files
	}
	if options.UserAgent == "" {
		options.UserAgent = "noe/dev"
	}
	registry := provider.NewExplicitRegistry(provider.Options{
		BaseDir: baseDir, ConfigFile: configFile, ConfigDir: configDir,
		Env: env, Credentials: credentials, UserAgent: options.UserAgent,
		HTTPClient: options.HTTPClient, KeyFile: options.KeyFile,
		BaseURL: options.BaseURL, DecisionServerURL: options.DecisionServerURL,
	})
	return &service{
		registry: registry, provider: options.Provider,
		decisionProvider: options.DecisionProvider, imageProvider: options.ImageProvider,
	}, nil
}

func resolveInputPath(baseDir, path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	return filepath.Abs(path)
}

func checkContext(ctx context.Context) error {
	if ctx == nil {
		return errors.New("model: context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (s *service) ListModels(ctx context.Context, query ModelQuery) ([]Model, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	return s.registry.ListModels(query)
}

func (s *service) Generate(ctx context.Context, request Request) (Response, error) {
	if err := checkContext(ctx); err != nil {
		return Response{}, err
	}
	if s.provider != nil {
		return s.provider.Generate(ctx, request)
	}
	return s.registry.Generate(ctx, request)
}

func (s *service) Decide(ctx context.Context, request DecisionRequest) (DecisionResponse, error) {
	if err := checkContext(ctx); err != nil {
		return DecisionResponse{}, err
	}
	if s.decisionProvider != nil {
		return s.decisionProvider.Decide(ctx, request)
	}
	if s.provider != nil {
		if decider, ok := s.provider.(DecisionProvider); ok {
			return decider.Decide(ctx, request)
		}
		return DecisionResponse{}, fmt.Errorf("model: decide: %w", ErrUnsupported)
	}
	return s.registry.Decide(ctx, request)
}

func (s *service) GenerateImage(ctx context.Context, request ImageRequest) (GeneratedImage, error) {
	if err := checkContext(ctx); err != nil {
		return GeneratedImage{}, err
	}
	if s.imageProvider != nil {
		return s.imageProvider.GenerateImage(ctx, request)
	}
	if s.provider != nil {
		if imageProvider, ok := s.provider.(ImageProvider); ok {
			return imageProvider.GenerateImage(ctx, request)
		}
		imageErr := &imageapi.Error{Code: "unsupported", Message: "image generation is unsupported"}
		return GeneratedImage{}, fmt.Errorf("model: generate image: %w: %w", ErrUnsupported, imageErr)
	}
	return s.registry.GenerateImage(ctx, imageapi.Request{
		Provider: request.Provider, Model: request.Model, Prompt: request.Prompt,
		Parameters: request.Parameters, RunID: request.RunID,
	})
}

func (s *service) HasProvider(name string) (bool, error) {
	if s.provider != nil {
		if providerWithCatalog, ok := s.provider.(interface{ HasProvider(string) (bool, error) }); ok {
			exists, err := providerWithCatalog.HasProvider(name)
			if err != nil || exists {
				return exists, err
			}
			if s.imageProvider != nil || s.decisionProvider != nil {
				// This catalog belongs to the custom chat provider, while image
				// and decision providers are independently selectable.
				return true, nil
			}
			return false, nil
		}
		// A custom provider without a catalog owns its own provider validation.
		// Do not infer its supported IDs from the built-in registry (or assume
		// that it only represents OpenRouter); Generate/Decide classify requests.
		return true, nil
	}
	if s.imageProvider != nil || s.decisionProvider != nil {
		// These custom operations are independent of the chat catalog. Their
		// own implementation is authoritative when no matching catalog exists.
		return true, nil
	}
	return s.registry.HasProvider(name)
}
