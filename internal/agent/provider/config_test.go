package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func registryFixture(t *testing.T, configuration string) (*Registry, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	return NewExplicitRegistry(Options{BaseDir: dir, ConfigDir: filepath.Join(dir, "noe"), ConfigFile: path, UserAgent: "noe/test"}), path
}

func TestExplicitRegistryPartialOverridesModelsAndFreeze(t *testing.T) {
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

func TestExplicitRegistryDefaultsAndLazyConfiguration(t *testing.T) {
	base := t.TempDir()
	r := NewExplicitRegistry(Options{BaseDir: base, ConfigDir: filepath.Join(base, "noe")})
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
	r = NewExplicitRegistry(Options{BaseDir: base, ConfigDir: filepath.Join(base, "noe"), ConfigFile: filepath.Join(t.TempDir(), "missing.json")})
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

func TestRunJSInputDefaultsAndValidation(t *testing.T) {
	var embedded definitions
	if err := json.Unmarshal(defaultDefinitions, &embedded); err != nil {
		t.Fatal(err)
	}
	for providerID, want := range map[string]string{
		"openrouter": "json", "opencode-go": "json", "openai-chatgpt": "javascript", "openai-api": "json",
	} {
		definition, err := decodeDefinition(embedded.Providers[providerID])
		if err != nil {
			t.Fatalf("decode %s: %v", providerID, err)
		}
		if definition.RunJSInput != want {
			t.Errorf("%s runJSInput = %q, want %q", providerID, definition.RunJSInput, want)
		}
	}

	base := `{"sdk":"openai","auth":"none","api":"responses","baseURL":"http://localhost/v1","path":"/responses"}`
	var configured map[string]json.RawMessage
	if err := json.Unmarshal([]byte(base), &configured); err != nil {
		t.Fatal(err)
	}
	configured["runJSInput"] = json.RawMessage(`"javascript"`)
	data, _ := json.Marshal(configured)
	definition, err := decodeDefinition(data)
	if err != nil || definition.RunJSInput != "javascript" {
		t.Fatalf("explicit JavaScript setting: %+v, %v", definition, err)
	}

	for _, setting := range []struct {
		api, format string
	}{
		{"responses", "unknown"},
		{"chat-completions", "javascript"},
	} {
		configured["api"], _ = json.Marshal(setting.api)
		configured["runJSInput"], _ = json.Marshal(setting.format)
		data, _ = json.Marshal(configured)
		if _, err := decodeDefinition(data); err == nil {
			t.Errorf("accepted runJSInput=%q with api=%q", setting.format, setting.api)
		}
	}
}

func TestChatGPTDefaultSessionHeaderAcrossTasks(t *testing.T) {
	cases := []struct{ run, session, cache string }{
		{"task-1", "parent-instance", "parent-instance"},
		{"task-2", "parent-instance", "parent-instance"},
		{"task-3", "parent-instance", "custom-cache"},
		{"child-task", "child-instance", "child-instance"},
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls >= len(cases) {
			t.Error("unexpected extra request")
			w.WriteHeader(500)
			return
		}
		want := cases[calls]
		calls++
		if got := r.Header.Get("session_id"); got != want.session {
			t.Errorf("%s session_id = %q, want %q", want.run, got, want.session)
		}
		var body struct {
			CacheKey string `json:"prompt_cache_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.CacheKey != want.cache {
			t.Errorf("%s prompt_cache_key = %q, want %q", want.run, body.CacheKey, want.cache)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"type":"response.completed","sequence_number":1,"response":`+nativeResponseFixture(`[{"type":"message","id":"msg","role":"assistant","status":"completed","content":[{"type":"output_text","text":"OK","annotations":[]}]}]`)+`}`)
	}))
	defer server.Close()
	// Retain the built-in session settings; replace only auth and endpoint so
	// the real SDK transport can be exercised without live credentials.
	r, _ := registryFixture(t, fmt.Sprintf(`{"providers":{"openai-chatgpt":{"auth":"none","baseURL":%q}}}`, server.URL))
	for _, c := range cases {
		_, err := r.Generate(context.Background(), Request{
			Provider: "openai-chatgpt", Model: "gpt-6.1-sol", RunID: c.run,
			SessionID: c.session, CacheKey: c.cache,
			Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"Reply OK"}`)},
		})
		if err != nil {
			t.Fatalf("%s: %v", c.run, err)
		}
	}
	if calls != len(cases) {
		t.Fatalf("requests = %d, want %d", calls, len(cases))
	}
}

func TestConfiguredCredentialDoesNotFallBackToAnotherProvider(t *testing.T) {
	t.Setenv("NOE_OPENROUTER_API_KEY_FILE", "/some-other-provider-key")
	p := NewOpenRouter(Options{Profile: &Definition{API: "chat-completions", Path: "/chat/completions"}})
	if p.keyFile != "" {
		t.Fatal("configured provider inherited OpenRouter credentials")
	}
}

func TestExplicitRegistryCredentialsLazyAndOverridePrecedence(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "config.json")
	if err := os.WriteFile(config, []byte(`{"providers":{"opencode-go":{"keyFile":"/missing-config-key"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(key, []byte("fixture-key"), 0600); err != nil {
		t.Fatal(err)
	}
	r := NewExplicitRegistry(Options{
		BaseDir: base, ConfigDir: filepath.Join(base, "noe"), ConfigFile: config,
		Env: map[string]string{"NOE_OPENCODE_GO_API_KEY_FILE": key},
	})
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
	_, err = NewExplicitRegistry(Options{ConfigFile: filepath.Join(t.TempDir(), "missing")}).Generate(context.Background(), Request{Provider: "openrouter", Model: "m"})
	if err == nil {
		t.Fatal("invalid config not reported")
	}
}
