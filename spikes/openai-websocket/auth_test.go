package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitAPIKeySelectionHasNoOAuthFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte(" fixture-api-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOE_OPENAI_API_KEY_FILE", path)
	t.Setenv("NOE_OPENAI_CHATGPT_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "missing-oauth"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-api-key" {
			t.Error("selected API key was not used")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	auth, err := liveAuthentication(server.Client(), "api-key")
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := auth.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if auth.redact("value=fixture-api-key") != "value=[redacted]" {
		t.Fatal("selected API key was not redacted")
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.token(context.Background()); err == nil {
		t.Fatal("empty API key was accepted")
	}
	if err := os.WriteFile(path, []byte("fixture-api-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	oauth, err := liveAuthentication(server.Client(), "chatgpt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oauth.token(t.Context()); err == nil {
		t.Fatal("missing OAuth credential fell back to API key")
	}
	if _, err := liveAuthentication(server.Client(), "unknown"); err == nil {
		t.Fatal("unknown authentication selection was accepted")
	}
}
