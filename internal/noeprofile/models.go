package noeprofile

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
)

// Models captures the application environment but resolves NOE configuration
// locations only when catalog or model operations are first requested.
func Models(options model.Options) model.Service {
	env := make(map[string]string)
	for _, pair := range os.Environ() {
		key, value, ok := strings.Cut(pair, "=")
		if ok {
			env[key] = value
		}
	}
	for key, value := range options.Env {
		env[key] = value
	}
	options.Env = env
	files := make(map[string]model.CredentialFiles, len(options.Credentials))
	for name, value := range options.Credentials {
		files[name] = value
	}
	options.Credentials = files
	return &lazyModels{options: options}
}

type lazyModels struct {
	once    sync.Once
	options model.Options
	service model.Service
	err     error
}

func (s *lazyModels) get() (model.Service, error) {
	s.once.Do(func() {
		options := s.options
		if options.ConfigDir == "" {
			home := options.Env["XDG_CONFIG_HOME"]
			if home == "" {
				home = options.Env["HOME"]
				if home == "" {
					home, s.err = os.UserHomeDir()
					if s.err != nil {
						return
					}
				}
				home = filepath.Join(home, ".config")
			}
			options.ConfigDir = filepath.Join(home, "noe")
		}
		if options.ConfigFile == "" {
			options.ConfigFile = options.Env["NOE_CONFIG_FILE"]
			if options.ConfigFile == "" {
				path := filepath.Join(options.ConfigDir, "config.json")
				if !filepath.IsAbs(path) && options.BaseDir != "" {
					path = filepath.Join(options.BaseDir, path)
				}
				if _, err := os.Stat(path); err == nil {
					options.ConfigFile = path
				} else if !errors.Is(err, os.ErrNotExist) {
					s.err = fmt.Errorf("inspect NOE configuration: %w", err)
					return
				}
			}
		}
		s.service, s.err = model.NewService(options)
	})
	return s.service, s.err
}
func (s *lazyModels) ListModels(ctx context.Context, q model.ModelQuery) ([]model.Model, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service, err := s.get()
	if err != nil {
		return nil, err
	}
	return service.ListModels(ctx, q)
}
func (s *lazyModels) Generate(ctx context.Context, q model.Request) (model.Response, error) {
	if err := ctx.Err(); err != nil {
		return model.Response{}, err
	}
	service, err := s.get()
	if err != nil {
		return model.Response{}, err
	}
	return service.Generate(ctx, q)
}
func (s *lazyModels) Decide(ctx context.Context, q model.DecisionRequest) (model.DecisionResponse, error) {
	if err := ctx.Err(); err != nil {
		return model.DecisionResponse{}, err
	}
	service, err := s.get()
	if err != nil {
		return model.DecisionResponse{}, err
	}
	return service.Decide(ctx, q)
}
func (s *lazyModels) GenerateImage(ctx context.Context, q model.ImageRequest) (model.GeneratedImage, error) {
	if err := ctx.Err(); err != nil {
		return model.GeneratedImage{}, err
	}
	service, err := s.get()
	if err != nil {
		return model.GeneratedImage{}, err
	}
	return service.GenerateImage(ctx, q)
}
func (s *lazyModels) HasProvider(name string) (bool, error) {
	service, err := s.get()
	if err != nil {
		return false, err
	}
	if p, ok := service.(interface{ HasProvider(string) (bool, error) }); ok {
		return p.HasProvider(name)
	}
	return true, nil
}
