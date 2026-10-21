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
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
)

func TestExplicitRegistryCredentialPrecedenceAndLazyRead(t *testing.T) {
	base := t.TempDir()
	registry := NewExplicitRegistry(Options{
		BaseDir: base,
		Env: map[string]string{
			"NOE_OPENAI_API_KEY_FILE": "env-key",
		},
		Credentials: map[string]CredentialFiles{
			"openai-api": {KeyFile: "credential-key"},
		},
	})
	resolved, err := registry.resolve("openai-api", "gpt-6-luna")
	if err != nil {
		t.Fatal(err)
	}
	configured := resolved.(*OpenAI)
	if want := filepath.Join(base, "credential-key"); configured.key.keyFile != want {
		t.Fatalf("credential override = %q, want %q", configured.key.keyFile, want)
	}
	if configured.key.key != "" {
		t.Fatal("credential was read during provider resolution")
	}

	registry = NewExplicitRegistry(Options{BaseDir: base, Env: map[string]string{"NOE_OPENAI_API_KEY_FILE": "env-key"}})
	resolved, err = registry.resolve("openai-api", "gpt-6-luna")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolved.(*OpenAI).key.keyFile, filepath.Join(base, "env-key"); got != want {
		t.Fatalf("environment override = %q, want %q", got, want)
	}
}

func TestExplicitRegistryNormalizesOpenRouterOverridesAndKeepsCredentialPrecedence(t *testing.T) {
	base := t.TempDir()
	registry := NewExplicitRegistry(Options{
		BaseDir: base, KeyFile: "legacy-key", BaseURL: "http://fixture.invalid/api/v1",
		DecisionServerURL: "http://decision.fixture.invalid",
		Credentials:       map[string]CredentialFiles{"openrouter": {KeyFile: "credential-key"}},
	})
	resolved, err := registry.resolve("openrouter", "model")
	if err != nil {
		t.Fatal(err)
	}
	configured := resolved.(*OpenRouter)
	if got, want := configured.keyFile, filepath.Join(base, "credential-key"); got != want {
		t.Fatalf("selected key path = %q, want %q", got, want)
	}
	if configured.profile.BaseURL != "http://fixture.invalid/api/v1" || configured.decisionServerURL != "http://decision.fixture.invalid" {
		t.Fatalf("OpenRouter endpoint overrides were lost: base=%q decision=%q", configured.profile.BaseURL, configured.decisionServerURL)
	}

	registry = NewExplicitRegistry(Options{BaseDir: base, KeyFile: "legacy-key"})
	resolved, err = registry.resolve("openrouter", "model")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := resolved.(*OpenRouter).keyFile, filepath.Join(base, "legacy-key"); got != want {
		t.Fatalf("relative OpenRouter KeyFile = %q, want %q", got, want)
	}
}

func TestExplicitRegistryDoesNotUseAmbientEnvironmentAndDefersUnusedPaths(t *testing.T) {
	ambient := filepath.Join(t.TempDir(), "ambient-key")
	t.Setenv(DefaultKeyFileEnv, ambient)
	registry := NewExplicitRegistry(Options{BaseDir: t.TempDir()})
	if ok, err := registry.HasProvider("openrouter"); !ok || err != nil {
		t.Fatalf("catalog provider lookup = (%v, %v)", ok, err)
	}
	if _, err := registry.ListModels(ModelQuery{Provider: "openrouter"}); err != nil {
		t.Fatalf("catalog unexpectedly required a credential path: %v", err)
	}
	_, err := registry.resolve("openrouter", "model")
	if err == nil || !strings.Contains(err.Error(), "unresolved placeholder") {
		t.Fatalf("selected unresolved credential path error = %v", err)
	}

	noAuth := NewExplicitRegistry(Options{BaseDir: t.TempDir(), ConfigFile: writeExplicitConfig(t,
		`{"providers":{"openai-chatgpt":{"auth":"none","oauthFile":"${configDir}/unused"}}}`)})
	if _, err := noAuth.resolve("openai-chatgpt", "gpt-6.1-sol"); err != nil {
		t.Fatalf("unused OAuth path caused an error for auth:none: %v", err)
	}
}

func TestExplicitRegistryUsesOnlyNamedConfigFile(t *testing.T) {
	ambient := writeExplicitConfig(t, `{"providers":{"ambient":{"sdk":"openai","api":"chat-completions","baseURL":"http://127.0.0.1:1","path":"/chat","auth":"none"}}}`)
	t.Setenv("NOE_CONFIG_FILE", ambient)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	embedded := NewExplicitRegistry(Options{BaseDir: t.TempDir()})
	if ok, err := embedded.HasProvider("ambient"); err != nil || ok {
		t.Fatalf("ambient NOE config was loaded: %v, %v", ok, err)
	}
	if ok, err := embedded.HasProvider("openrouter"); err != nil || !ok {
		t.Fatalf("embedded provider missing: %v, %v", ok, err)
	}

	config := writeExplicitConfig(t, `{"providers":{"custom":{"sdk":"openai","api":"chat-completions","baseURL":"http://127.0.0.1:1","path":"/chat","auth":"none"}}}`)
	registry := NewExplicitRegistry(Options{BaseDir: t.TempDir(), ConfigFile: config})
	if ok, err := registry.HasProvider("custom"); !ok || err != nil {
		t.Fatalf("named custom config was not loaded: %v, %v", ok, err)
	}
	if _, err := registry.HasProvider("openrouter"); err != nil {
		t.Fatalf("embedded definitions were not retained: %v", err)
	}

	missing := NewExplicitRegistry(Options{BaseDir: t.TempDir(), ConfigFile: filepath.Join(t.TempDir(), "missing.json")})
	if _, err := missing.HasProvider("openrouter"); err == nil || !strings.Contains(err.Error(), "read provider configuration") {
		t.Fatalf("missing explicit config should be reported with context: %v", err)
	}
}
func TestExplicitRegistrySelectsOAuthCredentialFileForSDKRefreshSource(t *testing.T) {
	base := t.TempDir()
	writeRecord := func(name, access string) {
		t.Helper()
		path := filepath.Join(base, name)
		if err := credentials.Write(path, credentials.Record{
			Issuer: "https://auth.openai.com", Subject: "fixture-user", ClientID: "fixture-client",
			Access: access, ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{credentials.PlanScope},
		}); err != nil {
			t.Fatal(err)
		}
	}
	writeRecord("selected-oauth.json", "selected-access")
	writeRecord("env-oauth.json", "env-access")
	writeRecord("ambient-oauth.json", "ambient-access")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer selected-access" {
			t.Errorf("OAuth request = %s %s authorization=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		output := `[{"type":"message","id":"msg","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok","annotations":[]}]}]`
		writeSSE(w, `{"type":"response.output_text.delta","delta":"ok","sequence_number":1}`)
		writeSSE(w, `{"type":"response.completed","sequence_number":2,"response":`+nativeResponseFixture(output)+`}`)
	}))
	defer server.Close()
	config := writeExplicitConfig(t, fmt.Sprintf(`{"providers":{"openai-chatgpt":{"baseURL":%q}}}`, server.URL))
	t.Setenv("NOE_OPENAI_CHATGPT_CREDENTIALS_FILE", filepath.Join(base, "ambient-oauth.json"))
	registry := NewExplicitRegistry(Options{
		BaseDir: base, ConfigFile: config,
		Env:         map[string]string{"NOE_OPENAI_CHATGPT_CREDENTIALS_FILE": "env-oauth.json"},
		Credentials: map[string]CredentialFiles{"openai-chatgpt": {OAuthFile: "selected-oauth.json"}},
	})
	response, err := registry.Generate(context.Background(), Request{
		Provider: "openai-chatgpt", Model: "gpt-6.1-sol", RunID: "run-1",
		Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hello"}`)},
	})
	if err != nil || response.FinishReason != "stop" {
		t.Fatalf("OAuth Responses request: %+v, %v", response, err)
	}
}

func writeExplicitConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
