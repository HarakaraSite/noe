package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	imageapi "noe/internal/image"
)

const fixtureImage = "fixture-image-bytes"

func imageKey(t *testing.T) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "api-key")
	const key = "fixture-secret-value"
	if err := os.WriteFile(path, []byte(key), 0600); err != nil {
		t.Fatal(err)
	}
	return path, key
}

func imageConfiguration(t *testing.T, providerID string, definition map[string]any) *Registry {
	t.Helper()
	providers := map[string]any{providerID: definition}
	configuration, err := json.Marshal(map[string]any{"providers": providers})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := registryFixture(t, string(configuration))
	return r
}

func successImageBody() string {
	return `{"created":1,"model":"fixture/model","data":[{"b64_json":"` + base64.StdEncoding.EncodeToString([]byte(fixtureImage)) + `"}],"usage":{"total_tokens":9,"cost":0.002}}`
}

func TestRegistryGenerateImageOpenRouterSDKUsesMergedProfileAndOwnedFields(t *testing.T) {
	keyPath, _ := imageKey(t)
	var seen map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/model-image-endpoint" {
			t.Errorf("request target = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer fixture-secret-value" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("x-request-id", "openrouter-fixture-request")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(successImageBody()))
	}))
	defer server.Close()

	r := imageConfiguration(t, "openrouter", map[string]any{
		"baseURL": server.URL,
		"keyFile": keyPath,
		"imageGeneration": map[string]any{
			"baseURL": server.URL,
			"path":    "/provider-image-endpoint",
			"parameters": map[string]any{
				"quality": "low", "custom": map[string]any{"keep": true},
				"model": "provider-override", "prompt": "provider-override", "n": 5, "stream": true,
			},
		},
		"models": map[string]any{
			"image-fixture": map[string]any{
				"imageGeneration": map[string]any{
					"path":       "/model-image-endpoint",
					"parameters": map[string]any{"quality": "high", "model": "model-override"},
				},
			},
		},
	})

	result, err := r.GenerateImage(context.Background(), imageapi.Request{
		Provider: "openrouter", Model: "image-fixture", Prompt: "fixture prompt", Output: "fixture.png",
		Parameters: map[string]json.RawMessage{"size": json.RawMessage(`"1024x1024"`), "future_option": json.RawMessage(`{"x":9007199254740993}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Content) != fixtureImage || result.ResponseModel != "fixture/model" || result.RequestID != "openrouter-fixture-request" || string(result.Usage) != `{"total_tokens":9,"cost":0.002}` {
		t.Fatalf("result: %+v, content=%q", result, result.Content)
	}
	var model, prompt, quality, size, n, stream json.RawMessage
	if json.Unmarshal(seen["model"], &model) != nil || string(model) != `"image-fixture"` ||
		json.Unmarshal(seen["prompt"], &prompt) != nil || string(prompt) != `"fixture prompt"` ||
		json.Unmarshal(seen["quality"], &quality) != nil || string(quality) != `"high"` ||
		json.Unmarshal(seen["size"], &size) != nil || string(size) != `"1024x1024"` ||
		json.Unmarshal(seen["n"], &n) != nil || string(n) != "1" ||
		json.Unmarshal(seen["stream"], &stream) != nil || string(stream) != "false" ||
		string(seen["future_option"]) != `{"x":9007199254740993}` || string(seen["custom"]) != `{"keep":true}` {
		t.Fatalf("request body did not preserve options and owned fields: %s", mustJSON(seen))
	}
}

func TestRegistryGenerateImageOpenAISDKUsesImagesEndpoint(t *testing.T) {
	keyPath, _ := imageKey(t)
	var seen map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/images/custom-generation" {
			t.Errorf("request target = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer fixture-secret-value" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("x-request-id", "openai-fixture-request")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(successImageBody()))
	}))
	defer server.Close()

	r := imageConfiguration(t, "openai-api", map[string]any{
		"keyFile":         keyPath,
		"imageGeneration": map[string]any{"baseURL": server.URL, "path": "/images/custom-generation", "parameters": map[string]any{"quality": "low"}},
	})
	result, err := r.GenerateImage(context.Background(), imageapi.Request{
		Provider: "openai-api", Model: "gpt-image-2.5-flare", Prompt: "fixture prompt", Output: "fixture.png",
		Parameters: map[string]json.RawMessage{"quality": json.RawMessage(`"high"`), "size": json.RawMessage(`"1024x1024"`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Content) != fixtureImage || result.RequestID != "openai-fixture-request" {
		t.Fatalf("result: %+v, content=%q", result, result.Content)
	}
	if string(seen["model"]) != `"gpt-image-2.5-flare"` || string(seen["quality"]) != `"high"` || string(seen["n"]) != "1" || string(seen["stream"]) != "false" {
		t.Fatalf("request body: %s", mustJSON(seen))
	}
}

func TestImageFailuresAreClassifiedAndProviderTextIsRedacted(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		want       string
	}{
		{"explicit moderation refusal", `{"error":{"code":"moderation_blocked","message":"fixture-secret-value data:image/png;base64,QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo="}}`, 400, "refusal"},
		{"invalid input", `{"error":{"code":"invalid_parameter","message":"size is invalid"}}`, 400, "input"},
		{"authentication", `{"error":{"code":"invalid_api_key","message":"invalid key"}}`, 401, "authentication"},
		{"quota", `{"error":{"code":"insufficient_quota","message":"credits exhausted"}}`, 402, "quota"},
		{"rate limit", `{"error":{"code":"rate_limit_exceeded","message":"slow down"}}`, 429, "rate_limit"},
		{"message unsupported", `{"error":{"code":"invalid_request_error","message":"Image generation is not supported for this model"}}`, 400, "unsupported"},
		{"quota429", `{"error":{"code":"insufficient_quota","message":"credits exhausted"}}`, 429, "quota"},
		{"unsupported", `{"error":{"code":"unsupported_feature","message":"feature unsupported"}}`, 400, "unsupported"},
		{"unknown client error", `{"error":{"code":"provider_specific_unknown","message":"content refused for unknown reason"}}`, 400, "response"},
		{"transport", `{"error":{"code":"server_error","message":"upstream error"}}`, 503, "transport"},
		{"HTTP 200 refusal", `{"refusal":"blocked by provider policy","data":[]}`, 200, "refusal"},
		{"empty success", `{"data":[]}`, 200, "response"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			keyPath, secret := imageKey(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("x-request-id", "fixture-request")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			r := imageConfiguration(t, "openai-api", map[string]any{
				"keyFile":         keyPath,
				"imageGeneration": map[string]any{"baseURL": server.URL, "path": "/images/generations"},
			})
			_, err := r.GenerateImage(context.Background(), imageapi.Request{Provider: "openai-api", Model: "model", Prompt: "prompt", Output: "out.png"})
			if err == nil {
				t.Fatal("GenerateImage succeeded for provider error")
			}
			var imageErr *imageapi.Error
			if !errors.As(err, &imageErr) || imageErr.Code != test.want {
				t.Fatalf("error = %#v; want code %q", err, test.want)
			}
			if imageErr.StatusCode != test.status || imageErr.RequestID != "fixture-request" {
				t.Fatalf("error metadata: %+v", imageErr)
			}
			encoded, marshalErr := json.Marshal(imageErr)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			for _, secretValue := range []string{secret, "data:image/png;base64,", "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo="} {
				if strings.Contains(string(encoded), secretValue) || strings.Contains(imageErr.Error(), secretValue) {
					t.Fatalf("sensitive provider data escaped: %s / %v", encoded, imageErr)
				}
			}
			if test.name == "explicit moderation refusal" && (imageErr.ProviderCode != "moderation_blocked" || !strings.Contains(imageErr.ProviderMessage, "[redacted image data]")) {
				t.Fatalf("refusal metadata not retained safely: %+v", imageErr)
			}
			if test.name == "unknown client error" && imageErr.Code == "refusal" {
				t.Fatal("unknown error was guessed to be a refusal")
			}
		})
	}
}

func TestRegistryImageCatalogCoexistsWithAgentAndDecisionAndSupportsOverrides(t *testing.T) {
	r, _ := registryFixture(t, `{"providers":{"openrouter":{"models":{"openai/gpt-image-2.5-flare":{"catalog":{"description":"custom image entry"}}}},"openai-api":{"models":{"gpt-image-2.5-flare":{"catalog":{"tags":["favorite"]}}}}}}`)
	images, err := r.ListModels(ModelQuery{Kind: "image"})
	if err != nil || len(images) != 2 {
		t.Fatalf("image models: %+v %v", images, err)
	}
	if images[0].Provider != "openai-api" || images[0].Description != "Image generation through the OpenAI Images API." || strings.Join(images[0].Tags, ",") != "favorite" ||
		images[1].Provider != "openrouter" || images[1].Description != "custom image entry" {
		t.Fatalf("external catalog overrides: %+v", images)
	}
	agents, err := r.ListModels(ModelQuery{Kind: "agent"})
	if err != nil || len(agents) == 0 {
		t.Fatalf("agent models: %+v %v", agents, err)
	}
	decisions, err := r.ListModels(ModelQuery{Kind: "decision"})
	if err != nil || len(decisions) != 3 {
		t.Fatalf("decision models: %+v %v", decisions, err)
	}
	if _, err := r.ListModels(ModelQuery{Kind: "future-kind"}); err == nil {
		t.Fatal("unknown kind accepted")
	}
	if _, err := r.resolve("openrouter", "openai/gpt-6-luna"); err != nil {
		t.Fatalf("image models interfered with ordinary model resolution: %v", err)
	}
}

func TestRegistryRejectsInvalidImageGenerationDefinitions(t *testing.T) {
	for _, definition := range []string{
		`{"baseURL":"file:///tmp","path":"/images"}`,
		`{"baseURL":"http://localhost:8080","path":"images"}`,
	} {
		t.Run(definition, func(t *testing.T) {
			r, _ := registryFixture(t, `{"providers":{"openrouter":{"imageGeneration":`+definition+`}}}`)
			if _, err := r.HasProvider("openrouter"); err == nil {
				t.Fatal("invalid image endpoint accepted")
			}
		})
	}
}

func TestGenerateImageCancellationAndParallelCalls(t *testing.T) {
	keyPath, _ := imageKey(t)
	entered := make(chan struct{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Prompt == "cancel" {
			entered <- struct{}{}
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(successImageBody()))
	}))
	defer server.Close()
	r := imageConfiguration(t, "openai-api", map[string]any{
		"keyFile":         keyPath,
		"imageGeneration": map[string]any{"baseURL": server.URL, "path": "/images/generations"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := r.GenerateImage(ctx, imageapi.Request{Provider: "openai-api", Model: "model", Prompt: "cancel", Output: "cancel.png"})
		result <- err
	}()
	<-entered
	cancel()
	if err := <-result; err == nil || !errors.As(err, new(*imageapi.Error)) || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("cancellation error = %v", err)
	}

	const count = 6
	var wait sync.WaitGroup
	errorsByCall := make(chan error, count)
	for range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			generated, err := r.GenerateImage(context.Background(), imageapi.Request{Provider: "openai-api", Model: "model", Prompt: "parallel", Output: "parallel.png"})
			if err == nil && string(generated.Content) != fixtureImage {
				err = errors.New("parallel result bytes changed")
			}
			errorsByCall <- err
		}()
	}
	wait.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func mustJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func TestImageErrorRedactsSmallBareImagePayload(t *testing.T) {
	const payload = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+iK1sAAAAASUVORK5CYII="
	err := imageFailure("refusal", 400, "request", "moderation_blocked", "Blocked image "+payload, "")
	if strings.Contains(err.ProviderMessage, payload) || !strings.Contains(err.ProviderMessage, "[redacted image data]") {
		t.Fatalf("payload leaked: %+v", err)
	}
}
