package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

type captureClient struct{ dir string }

func (c captureClient) Do(r *http.Request) (*http.Response, error) {
	response, err := http.DefaultClient.Do(r)
	if response != nil && r.URL.Host == "api.openai.com" && r.URL.Path == "/v1/responses" {
		file, fileErr := os.OpenFile(filepath.Join(c.dir, "responses.sse"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if fileErr != nil {
			response.Body.Close()
			return nil, fileErr
		}
		response.Body = &capturedBody{Reader: io.TeeReader(response.Body, file), source: response.Body, file: file}
	}
	return response, err
}

type capturedBody struct {
	io.Reader
	source io.Closer
	file   io.Closer
}

func (b *capturedBody) Close() error { defer b.file.Close(); return b.source.Close() }

func explicitProviderOptions(t *testing.T, client provider.HTTPClient) provider.Options {
	t.Helper()
	env := map[string]string{}
	for _, key := range []string{
		"NOE_CONFIG_FILE", "XDG_CONFIG_HOME", "HOME",
		"NOE_OPENROUTER_API_KEY_FILE", "NOE_OPENCODE_GO_API_KEY_FILE",
		"NOE_OPENAI_API_KEY_FILE", "NOE_OPENAI_CHATGPT_CREDENTIALS_FILE",
	} {
		if value, ok := os.LookupEnv(key); ok {
			env[key] = value
		}
	}
	baseDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	configHome := env["XDG_CONFIG_HOME"]
	if configHome == "" {
		home := env["HOME"]
		if home == "" {
			home, err = os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
		}
		configHome = filepath.Join(home, ".config")
	}
	configDir := filepath.Join(configHome, "noe")
	configFile := env["NOE_CONFIG_FILE"]
	if configFile == "" {
		candidate := filepath.Join(configDir, "config.json")
		if _, statErr := os.Stat(candidate); statErr == nil {
			configFile = candidate
		} else if !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("inspect default provider configuration: %v", statErr)
		}
	}
	return provider.Options{
		BaseDir: baseDir, ConfigFile: configFile, ConfigDir: configDir,
		Env: env, HTTPClient: client, UserAgent: "noe/validation",
	}
}

// Explicitly enabled diagnostic; ordinary test runs never call a real provider.
func TestOpenAIProviderLive(t *testing.T) {
	dir := os.Getenv("NOE_OPENAI_LIVE_DIR")
	if dir == "" {
		t.Skip("NOE_OPENAI_LIVE_DIR is required for real provider validation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r := provider.NewExplicitRegistry(explicitProviderOptions(t, captureClient{dir: dir}))
	result, err := r.Generate(ctx, provider.Request{Provider: "openai-chatgpt", Model: "gpt-6-luna", RunID: "diagnostic", Effort: "auto", MaxTokens: 2048,
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"Call run_js to calculate 19 * 23."}`)},
		Tools:    []json.RawMessage{json.RawMessage(`{"type":"function","function":{"name":"run_js","description":"Execute JavaScript","parameters":{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}}}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.FinishReason != "tool_calls" {
		t.Fatal("Responses did not retain its tool call")
	}
	raw, _ := json.Marshal(result)
	if err := os.WriteFile(filepath.Join(dir, "result.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
