package provider

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func registryFixture(t *testing.T, configuration string) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("NOE_OPENROUTER_API_KEY_FILE", "")
	t.Setenv("NOE_OPENCODE_GO_API_KEY_FILE", "")
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	return NewRegistry(Options{ConfigFile: path, UserAgent: "noe/test"}), path
}

func TestRegistryPartialOverridesModelsAndFreeze(t *testing.T) {
	r, path := registryFixture(t, `{"providers":{"opencode-go":{"baseURL":"http://localhost:2345","headers":{"X-Test":"present"},"parameters":{"temperature":0.25},"models":{"gpt-6-luna":{"parameters":{"top_p":0.8}}}},"custom":{"api":"responses","baseURL":"http://localhost:2346/v1","path":"/generate","keyFile":"${configDir}/credentials/custom"}}}`)
	resolved, err := r.resolve("opencode-go", "gpt-6-luna")
	if err != nil {
		t.Fatal(err)
	}
	p := resolved.(*OpenRouter)
	if p.profile.API != "responses" || p.profile.Path != "/responses" || p.profile.BaseURL != "http://localhost:2345" || p.profile.Headers["User-Agent"] != "noe/test" || p.profile.Headers["X-Test"] != "present" || string(p.profile.Parameters["temperature"]) != "0.25" || string(p.profile.Parameters["top_p"]) != "0.8" {
		t.Fatalf("merged profile: %+v", p.profile)
	}
	if p.key != "" {
		t.Fatal("credentials loaded while resolving")
	}
	if !strings.HasSuffix(p.keyFile, "noe/credentials/opencode-go-api-key") {
		t.Fatal(p.keyFile)
	}
	if err := os.WriteFile(path, []byte(`invalid`), 0600); err != nil {
		t.Fatal(err)
	}
	chat, err := r.resolve("opencode-go", "another-model")
	if err != nil || chat.(*OpenRouter).profile.API != "chat-completions" {
		t.Fatalf("configuration was reloaded: %v", err)
	}
	custom, err := r.resolve("custom", "model")
	if err != nil || custom.(*OpenRouter).profile.Path != "/generate" {
		t.Fatalf("custom provider: %v", err)
	}
	if _, err := r.resolve("missing", "model"); err == nil {
		t.Fatal("unknown provider accepted")
	}
}

func TestRegistryDefaultsAndLazyConfiguration(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("NOE_CONFIG_FILE", "")
	r := NewRegistry(Options{})
	if ok, err := r.HasProvider("opencode-go"); !ok || err != nil {
		t.Fatalf("built-in default: %v", err)
	}
	first, err := r.resolve("openrouter", "model-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.resolve("openrouter", "")
	if err != nil || first != second {
		t.Fatal("chat and decision did not share a client/credential cache")
	}
	r = NewRegistry(Options{ConfigFile: filepath.Join(t.TempDir(), "missing.json")})
	if _, err := r.HasProvider("openrouter"); err == nil {
		t.Fatal("explicit missing file silently ignored")
	}
	for _, configuration := range []string{`[]`, `{"providers":{}} true`, `{"unrecognized":true}`, `{"providers":{"opencode-go":{"api":"messages"}}}`} {
		r, _ := registryFixture(t, configuration)
		if _, err := r.HasProvider("openrouter"); err == nil {
			t.Fatalf("invalid config accepted: %s", configuration)
		}
	}
}

func TestConfiguredCredentialDoesNotFallBackToAnotherProvider(t *testing.T) {
	t.Setenv("NOE_OPENROUTER_API_KEY_FILE", "/some-other-provider-key")
	p := NewOpenRouter(Options{Profile: &Definition{API: "chat-completions", Path: "/chat/completions"}})
	if p.keyFile != "" {
		t.Fatal("configured provider inherited OpenRouter credentials")
	}
}

func TestRegistryCredentialsLazyAndOverridePrecedence(t *testing.T) {
	r, _ := registryFixture(t, `{"providers":{"opencode-go":{"keyFile":"/missing-config-key"}}}`)
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("fixture-key"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOE_OPENCODE_GO_API_KEY_FILE", key)
	resolved, err := r.resolve("opencode-go", "model")
	p := resolved.(*OpenRouter)
	if err != nil || p.keyFile != key {
		t.Fatalf("credential override: %v", err)
	}
	if p.key != "" {
		t.Fatal("key read during resolution")
	}
	value, err := p.apiKey()
	if err != nil || value != "fixture-key" {
		t.Fatalf("key read: %v", err)
	}
}

func TestConfiguredChatBodyKeepsRawHistoryAndMapsParameters(t *testing.T) {
	p := NewOpenRouter(Options{Profile: &Definition{API: "chat-completions", Path: "/chat/completions", EffortField: "reasoning_effort", Parameters: map[string]json.RawMessage{"temperature": json.RawMessage(`0.4`), "max_tokens": json.RawMessage(`999`)}}})
	message := json.RawMessage(`{"role":"assistant","content":"x","reasoning_content":"complete thought","extension":{"large":9007199254740993}}`)
	body, err := p.chatRequestBody(Request{Model: "model", RunID: "run", Messages: []json.RawMessage{message}, CacheKey: "cache", Effort: "high", MaxTokens: 50})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	json.Unmarshal(body, &fields)
	if string(fields["reasoning_effort"]) != `"high"` || string(fields["max_tokens"]) != "50" || string(fields["temperature"]) != "0.4" || fields["session_id"] != nil || !strings.Contains(string(fields["messages"]), string(message)) {
		t.Fatalf("request: %s", body)
	}
	_, err = NewRegistry(Options{ConfigFile: filepath.Join(t.TempDir(), "missing")}).Generate(context.Background(), Request{Provider: "openrouter", Model: "m"})
	if err == nil {
		t.Fatal("invalid config not reported")
	}
}
