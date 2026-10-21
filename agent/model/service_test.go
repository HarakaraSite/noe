package model

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type providerFunc func(context.Context, Request) (Response, error)

func (f providerFunc) Generate(ctx context.Context, request Request) (Response, error) {
	return f(ctx, request)
}

type catalogProvider struct{ providerFunc }

func (catalogProvider) HasProvider(name string) (bool, error) { return name == "custom-chat", nil }

type imageProviderFunc func(context.Context, ImageRequest) (GeneratedImage, error)

func (f imageProviderFunc) GenerateImage(ctx context.Context, request ImageRequest) (GeneratedImage, error) {
	return f(ctx, request)
}

func TestNewServiceIgnoresAmbientConfigAndDefersExplicitRead(t *testing.T) {
	ambient := filepath.Join(t.TempDir(), "ambient.json")
	if err := os.WriteFile(ambient, []byte(`{"providers":{"ambient":{"api":"chat-completions","baseURL":"http://127.0.0.1:1","path":"/chat","auth":"none"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOE_CONFIG_FILE", ambient)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	service, err := NewService(Options{BaseDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewService read configuration eagerly: %v", err)
	}
	models, err := service.ListModels(context.Background(), ModelQuery{Provider: "openrouter"})
	if err != nil || len(models) == 0 {
		t.Fatalf("embedded catalog: %d models, %v", len(models), err)
	}
	hasProvider := service.(interface{ HasProvider(string) (bool, error) })
	if ok, err := hasProvider.HasProvider("ambient"); err != nil || ok {
		t.Fatalf("ambient NOE config was loaded: %v, %v", ok, err)
	}

	missing := filepath.Join(t.TempDir(), "missing.json")
	deferred, err := NewService(Options{ConfigFile: missing})
	if err != nil {
		t.Fatalf("NewService read an explicit config file eagerly: %v", err)
	}
	if _, err := deferred.ListModels(context.Background(), ModelQuery{}); err == nil || !strings.Contains(err.Error(), "read provider configuration") {
		t.Fatalf("missing explicit config was not reported on first use: %v", err)
	}
}

func TestServiceSnapshotsEnvAndAnchorsRelativeCredentialPaths(t *testing.T) {
	base := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer snapshot-key" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, data := range []string{
			`{"id":"local","object":"chat.completion.chunk","created":1,"model":"local","choices":[{"index":0,"delta":{"role":"assistant","content":"answer"},"finish_reason":null}]}`,
			`{"id":"local","object":"chat.completion.chunk","created":1,"model":"local","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
			"[DONE]",
		} {
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
	}))
	defer server.Close()

	for name, content := range map[string]string{
		"selected-key": "snapshot-key",
		"later-key":    "mutated-key",
		"ambient-key":  "ambient-key",
	} {
		if err := os.WriteFile(filepath.Join(base, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config := fmt.Sprintf(`{"providers":{"openai-api":{"baseURL":%q,"api":"chat-completions","path":"/v1/chat/completions"}}}`, server.URL)
	if err := os.WriteFile(filepath.Join(base, "config.json"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOE_OPENAI_API_KEY_FILE", filepath.Join(base, "ambient-key"))
	env := map[string]string{"NOE_OPENAI_API_KEY_FILE": "selected-key"}
	service, err := NewService(Options{BaseDir: base, ConfigFile: "config.json", Env: env})
	if err != nil {
		t.Fatal(err)
	}
	env["NOE_OPENAI_API_KEY_FILE"] = "later-key"
	_, err = service.Generate(context.Background(), Request{
		Provider: "openai-api", Model: "local",
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hello"}`)},
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
}

func TestServiceImageGenerationNeedsNoOutputPath(t *testing.T) {
	base := t.TempDir()
	configDir := filepath.Join(base, "config")
	keyPath := filepath.Join(configDir, "credentials", "openai-api-key")
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("image-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/images/generations" || r.Header.Get("Authorization") != "Bearer image-key" {
			t.Errorf("image request = %s %s, authorization %q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if string(body["prompt"]) != `"draw a tree"` {
			t.Errorf("image prompt = %s", body["prompt"])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"created":1,"model":"fixture/image","data":[{"b64_json":%q}],"usage":{"total_tokens":3}}`, base64.StdEncoding.EncodeToString([]byte("image-bytes")))
	}))
	defer server.Close()

	config := fmt.Sprintf(`{"providers":{"openai-api":{"baseURL":%q,"imageGeneration":{"baseURL":%q,"path":"/images/generations"}}}}`, server.URL, server.URL)
	configPath := filepath.Join(base, "providers.json")
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(Options{BaseDir: base, ConfigFile: configPath, ConfigDir: configDir})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := service.GenerateImage(context.Background(), ImageRequest{
		Provider: "openai-api", Model: "fixture/image", Prompt: "draw a tree",
	})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if string(generated.Content) != "image-bytes" || generated.ResponseModel != "fixture/image" || string(generated.Usage) != `{"total_tokens":3}` {
		t.Fatalf("generated image = %+v, bytes=%q", generated, generated.Content)
	}
}

func TestCustomProviderUnsupportedOperationsDoNotFallBack(t *testing.T) {
	service, err := NewService(Options{Provider: providerFunc(func(context.Context, Request) (Response, error) {
		return Response{Message: json.RawMessage(`{"role":"assistant","content":"custom"}`)}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	if response, err := service.Generate(context.Background(), Request{Provider: "custom", Model: "model"}); err != nil || !strings.Contains(string(response.Message), "custom") {
		t.Fatalf("custom Generate = %+v, %v", response, err)
	}
	if _, err := service.Decide(context.Background(), DecisionRequest{Provider: "openrouter", Model: "model"}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("custom Decision fell back to built-in provider: %v", err)
	}
	_, err = service.GenerateImage(context.Background(), ImageRequest{Provider: "openai-api", Model: "model", Prompt: "prompt"})
	var imageError *ImageError
	if !errors.Is(err, ErrUnsupported) || !errors.As(err, &imageError) || imageError.Code != "unsupported" {
		t.Fatalf("custom image operation error = %v", err)
	}
}

func TestCustomImageProviderIsIndependentOfCustomChatCatalog(t *testing.T) {
	var imageCalls int
	service, err := NewService(Options{
		Provider: catalogProvider{providerFunc(func(context.Context, Request) (Response, error) {
			return Response{Message: json.RawMessage(`{"role":"assistant","content":"custom"}`)}, nil
		})},
		ImageProvider: imageProviderFunc(func(_ context.Context, request ImageRequest) (GeneratedImage, error) {
			imageCalls++
			if request.Provider != "openai-api" || request.Model != "fixture-image" {
				t.Errorf("image generation request = %+v", request)
			}
			return GeneratedImage{Content: []byte("image bytes"), RequestID: "image-request"}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	checker := service.(interface{ HasProvider(string) (bool, error) })
	if ok, err := checker.HasProvider("openai-api"); err != nil || !ok {
		t.Fatalf("independent image provider rejected: ok=%v err=%v", ok, err)
	}
	generated, err := service.GenerateImage(context.Background(), ImageRequest{Provider: "openai-api", Model: "fixture-image", Prompt: "draw"})
	if err != nil || string(generated.Content) != "image bytes" || imageCalls != 1 {
		t.Fatalf("custom image generation = %+v calls=%d err=%v", generated, imageCalls, err)
	}
}
