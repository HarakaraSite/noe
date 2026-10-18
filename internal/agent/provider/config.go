package provider

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"noe/internal/decision"
	imageapi "noe/internal/image"
)

//go:embed defaults.json
var defaultDefinitions []byte

// Definition contains data only. API-specific conversion stays in Go.
type Definition struct {
	SDK               string                     `json:"sdk"`
	Auth              string                     `json:"auth"`
	OAuthFile         string                     `json:"oauthFile"`
	OAuthFileEnv      string                     `json:"oauthFileEnv"`
	API               string                     `json:"api"`
	BaseURL           string                     `json:"baseURL"`
	Path              string                     `json:"path"`
	KeyFile           string                     `json:"keyFile"`
	KeyFileEnv        string                     `json:"keyFileEnv"`
	Headers           map[string]string          `json:"headers"`
	Parameters        map[string]json.RawMessage `json:"parameters"`
	SessionHeader     string                     `json:"sessionHeader"`
	SessionField      string                     `json:"sessionField"`
	ReasoningField    string                     `json:"reasoningField"`
	EffortField       string                     `json:"effortField"`
	DecisionServerURL string                     `json:"decisionServerURL"`
	ImageGeneration   *ImageGenerationDefinition `json:"imageGeneration,omitempty"`
	Models            map[string]json.RawMessage `json:"models"`
}

// ImageGenerationDefinition configures the provider-specific image generation
// endpoint independently of the provider's chat or Responses endpoint.
type ImageGenerationDefinition struct {
	BaseURL    string                     `json:"baseURL"`
	Path       string                     `json:"path"`
	Parameters map[string]json.RawMessage `json:"parameters"`
}

type definitions struct {
	Providers map[string]json.RawMessage `json:"providers"`
}

// Registry lazily loads one immutable configuration per host. Model clients
// share the host's credentials/transport, but request identity stays per call.
type Registry struct {
	opts          Options
	once          sync.Once
	err           error
	definitions   definitions
	configDir     string
	mu            sync.Mutex
	clients       map[string]Provider
	sharedClients map[string]Provider
	imageClients  map[string]imageapi.Provider
}

func NewRegistry(opts Options) *Registry {
	return &Registry{opts: opts, clients: make(map[string]Provider), sharedClients: make(map[string]Provider), imageClients: make(map[string]imageapi.Provider)}
}

func decodeObject(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("configuration must be an object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("unexpected trailing configuration data")
	}
	return value, nil
}

func mergeObject(base, override map[string]any) {
	for key, value := range override {
		if key == "headers" {
			if headers, ok := value.(map[string]any); ok {
				normalized := make(map[string]any)
				for name, value := range headers {
					normalized[http.CanonicalHeaderKey(name)] = value
				}
				value = normalized
			}
		}
		if value == nil {
			delete(base, key)
			continue
		}
		if object, ok := value.(map[string]any); ok {
			if old, ok := base[key].(map[string]any); ok {
				mergeObject(old, object)
				continue
			}
		}
		base[key] = value
	}
}

func decodeDefinition(data []byte) (Definition, error) {
	var d Definition
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil {
		return d, fmt.Errorf("invalid provider definition: %w", err)
	}
	if d.SDK == "" {
		d.SDK = "openrouter"
	}
	if d.Auth == "" {
		d.Auth = "api-key"
	}
	if d.SDK != "openrouter" && d.SDK != "openai" {
		return d, fmt.Errorf("unsupported provider SDK %q", d.SDK)
	}
	if d.Auth != "api-key" && d.Auth != "chatgpt-oauth" && d.Auth != "none" {
		return d, fmt.Errorf("unsupported provider authentication %q", d.Auth)
	}
	if d.SDK == "openrouter" && d.Auth != "api-key" {
		return d, errors.New("OpenRouter SDK requires api-key authentication")
	}
	if d.Auth == "chatgpt-oauth" && d.API != "responses" {
		return d, errors.New("ChatGPT plan authentication uses Responses")
	}
	if d.API != "chat-completions" && d.API != "responses" {
		return d, fmt.Errorf("unsupported provider API %q", d.API)
	}
	u, err := url.Parse(d.BaseURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return d, errors.New("provider baseURL must be an HTTP URL")
	}
	if d.Path == "" || !strings.HasPrefix(d.Path, "/") {
		return d, errors.New("provider path must start with /")
	}
	if d.ImageGeneration != nil {
		imageURL, err := url.Parse(d.ImageGeneration.BaseURL)
		if err != nil || imageURL.Host == "" || (imageURL.Scheme != "http" && imageURL.Scheme != "https") {
			return d, errors.New("imageGeneration baseURL must be an HTTP URL")
		}
		if d.ImageGeneration.Path == "" || !strings.HasPrefix(d.ImageGeneration.Path, "/") {
			return d, errors.New("imageGeneration path must start with /")
		}
	}
	return d, nil
}

func (r *Registry) load() {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			r.err = err
			return
		}
		configHome = filepath.Join(home, ".config")
	}
	r.configDir = filepath.Join(configHome, "noe")
	path := r.opts.ConfigFile
	if path == "" {
		path = os.Getenv("NOE_CONFIG_FILE")
	}
	explicit := path != ""
	if !explicit {
		path = filepath.Join(r.configDir, "config.json")
	}
	base, err := decodeObject(defaultDefinitions)
	if err != nil {
		r.err = err
		return
	}
	data, err := os.ReadFile(path)
	if err != nil && (!errors.Is(err, os.ErrNotExist) || explicit) {
		r.err = fmt.Errorf("read provider configuration: %w", err)
		return
	}
	if err == nil {
		override, err := decodeObject(data)
		if err != nil {
			r.err = fmt.Errorf("read provider configuration %s: %w", path, err)
			return
		}
		for key := range override {
			if key != "providers" {
				r.err = fmt.Errorf("unknown configuration field %q", key)
				return
			}
		}
		mergeObject(base, override)
	}
	data, err = json.Marshal(base)
	if err != nil {
		r.err = err
		return
	}
	if err = json.Unmarshal(data, &r.definitions); err != nil {
		r.err = err
		return
	}
	for name, raw := range r.definitions.Providers {
		if strings.TrimSpace(name) == "" {
			r.err = errors.New("provider ID must not be empty")
			return
		}
		d, err := decodeDefinition(raw)
		if err == nil {
			err = validateModels(d)
		}
		if err != nil {
			r.err = fmt.Errorf("provider %s: %w", name, err)
			return
		}
	}
}

func (r *Registry) HasProvider(name string) (bool, error) {
	r.once.Do(r.load)
	if r.err != nil {
		return false, r.err
	}
	_, ok := r.definitions.Providers[name]
	return ok, nil
}

func (r *Registry) resolve(name, model string) (Provider, error) {
	if name == "" {
		name = "openrouter"
	}
	ok, err := r.HasProvider(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", name)
	}
	key := name + "\x00" + model
	r.mu.Lock()
	defer r.mu.Unlock()
	if p := r.clients[key]; p != nil {
		return p, nil
	}
	base, err := decodeObject(r.definitions.Providers[name])
	if err != nil {
		return nil, err
	}
	var modelOverrides map[string]json.RawMessage
	if models, ok := base["models"]; ok {
		data, _ := json.Marshal(models)
		if err := json.Unmarshal(data, &modelOverrides); err != nil {
			return nil, err
		}
	}
	delete(base, "models")
	if raw, ok := modelOverrides[model]; ok {
		_, override, err := splitModel(raw)
		if err != nil {
			return nil, err
		}
		mergeObject(base, override)
	}
	data, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	d, err := decodeDefinition(data)
	if err != nil {
		return nil, err
	}
	userAgent := r.opts.UserAgent
	if userAgent == "" {
		userAgent = "noe/dev"
	}
	expand := strings.NewReplacer("${configDir}", r.configDir, "${userAgent}", userAgent)
	d.KeyFile = expand.Replace(d.KeyFile)
	d.OAuthFile = expand.Replace(d.OAuthFile)
	for key, value := range d.Headers {
		d.Headers[key] = expand.Replace(value)
	}
	if d.KeyFileEnv != "" {
		if value := os.Getenv(d.KeyFileEnv); value != "" {
			d.KeyFile = value
		}
	}
	if d.OAuthFileEnv != "" {
		if value := os.Getenv(d.OAuthFileEnv); value != "" {
			d.OAuthFile = value
		}
	}
	opts := Options{Profile: &d, KeyFile: d.KeyFile, BaseURL: d.BaseURL, HTTPClient: r.opts.HTTPClient, DecisionServerURL: d.DecisionServerURL}
	if name == "openrouter" {
		if r.opts.KeyFile != "" {
			opts.KeyFile = r.opts.KeyFile
		}
		if r.opts.BaseURL != "" {
			opts.BaseURL = r.opts.BaseURL
			opts.DecisionServerURL = strings.TrimSuffix(strings.TrimRight(opts.BaseURL, "/"), "/api/v1")
		}
		if r.opts.DecisionServerURL != "" {
			opts.DecisionServerURL = r.opts.DecisionServerURL
		}
	}
	sharedKey, _ := json.Marshal(struct {
		Profile                             Definition
		KeyFile, BaseURL, DecisionServerURL string
	}{d, opts.KeyFile, opts.BaseURL, opts.DecisionServerURL})
	if p := r.sharedClients[name+string(sharedKey)]; p != nil {
		r.clients[key] = p
		return p, nil
	}
	var p Provider
	if d.SDK == "openai" {
		p = NewOpenAI(opts)
	} else {
		p = NewOpenRouter(opts)
	}
	r.clients[key] = p
	r.sharedClients[name+string(sharedKey)] = p
	return p, nil
}

func (r *Registry) Generate(ctx context.Context, request Request) (Response, error) {
	p, err := r.resolve(request.Provider, request.Model)
	if err != nil {
		return Response{}, err
	}
	return p.Generate(ctx, request)
}

func (r *Registry) Decide(ctx context.Context, request decision.Request) (decision.Response, error) {
	p, err := r.resolve(request.Provider, "")
	if err != nil {
		return decision.Response{}, err
	}
	decider, ok := p.(decision.Provider)
	if !ok {
		return decision.Response{}, errors.New("provider does not support Decisions")
	}
	return decider.Decide(ctx, request)
}
