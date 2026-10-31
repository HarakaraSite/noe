package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

func codexToken(label string) string {
	payload, _ := json.Marshal(map[string]any{"exp": time.Now().Add(time.Hour).Unix(), "sub": "fixture-user", "jti": label})
	return "fixture." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func writeCodexExternal(t *testing.T, path, token string) {
	t.Helper()
	data, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{"access_token": token, "account_id": "fixture-account", "refresh_token": "fixture-refresh-secret", "id_token": "fixture-id-secret"}})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func codexPNG(t *testing.T, value uint8) []byte {
	t.Helper()
	var out bytes.Buffer
	im := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	im.Set(0, 0, color.NRGBA{R: value, A: 255})
	if err := png.Encode(&out, im); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func codexBody(content []byte) string {
	data, _ := json.Marshal(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(content)}}, "model": "actual-image-model", "usage": map[string]any{"total_tokens": 7}})
	return string(data)
}

func codexRegistry(t *testing.T, address, file string) *Registry {
	t.Helper()
	return imageConfiguration(t, "openai-codex-images", map[string]any{
		"oauthMode": "external-readonly", "oauthFile": file,
		"headers":         map[string]string{"Authorization": "must-not-win", "ChatGPT-Account-ID": "wrong-account"},
		"imageGeneration": map[string]any{"baseURL": address, "path": "/images/generations"},
		"imageEditing":    map[string]any{"baseURL": address, "path": "/images/edits"},
	})
}

func TestCodexImagesGenerateEditOrderedBytesAndOwnedHeaders(t *testing.T) {
	first, second := codexPNG(t, 10), codexPNG(t, 80)
	file := filepath.Join(t.TempDir(), "auth.json")
	token := codexToken("first")
	writeCodexExternal(t, file, token)
	before, _ := os.ReadFile(file)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("ChatGPT-Account-ID") != "fixture-account" || r.Header.Get("originator") != "noe_image_api" || r.Header.Get("x-codex-image-turn-id") == "" || r.Header.Get("User-Agent") != "noe/test" {
			t.Error("host-owned headers not applied")
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if string(body["n"]) != "1" || string(body["model"]) != `"gpt-image-2"` || body["stream"] != nil {
			t.Errorf("owned request fields: %s", mustJSON(body))
		}
		if r.URL.Path == "/images/edits" {
			var images []struct {
				URL string `json:"image_url"`
			}
			_ = json.Unmarshal(body["images"], &images)
			if len(images) != 2 {
				t.Errorf("edit inputs: %d", len(images))
			} else {
				for i, want := range [][]byte{first, second} {
					if images[i].URL != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(want) {
						t.Errorf("input order/bytes changed at %d", i)
					}
				}
			}
		} else if r.URL.Path != "/images/generations" || body["images"] != nil {
			t.Errorf("generation route/body: %s", r.URL.Path)
		}
		w.Header().Set("x-codex-imagegen-request-id", "inner-request")
		w.Header().Set("x-request-id", "outer-request")
		_, _ = io.WriteString(w, codexBody(second))
	}))
	defer server.Close()
	r := codexRegistry(t, server.URL, file)
	generated, err := r.GenerateImage(t.Context(), imageapi.Request{Provider: "openai-codex-images", Model: "gpt-image-2", Prompt: "draw"})
	if err != nil {
		t.Fatal(err)
	}
	edited, err := r.EditImage(t.Context(), imageapi.EditRequest{Provider: "openai-codex-images", Model: "gpt-image-2", Prompt: "edit", Images: []imageapi.Input{{Content: first}, {Content: second}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []imageapi.Generated{generated, edited} {
		if !bytes.Equal(result.Content, second) || result.RequestID != "inner-request" || result.ResponseModel != "actual-image-model" || string(result.Usage) != `{"total_tokens":7}` {
			t.Fatalf("result metadata: %+v", result)
		}
	}
	after, _ := os.ReadFile(file)
	if calls.Load() != 2 || !bytes.Equal(before, after) {
		t.Fatal("unexpected calls or external credential write")
	}
	if _, err := os.Stat(file + ".lock"); !os.IsNotExist(err) {
		t.Fatal("external credentials created lock file")
	}
	if _, err := r.Generate(t.Context(), Request{Provider: "openai-codex-images", Model: "gpt-image-2"}); err == nil || calls.Load() != 2 {
		t.Fatal("image-only provider accepted a chat operation")
	}
}

func TestCodexImages401RereadsOnlyNewExternalCredential(t *testing.T) {
	file := filepath.Join(t.TempDir(), "auth.json")
	old, newToken := codexToken("old"), codexToken("new")
	writeCodexExternal(t, file, old)
	content := codexPNG(t, 25)
	var calls atomic.Int32
	turn := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := calls.Add(1)
		if count == 1 {
			turn = r.Header.Get("x-codex-image-turn-id")
			writeCodexExternal(t, file, newToken)
			w.WriteHeader(401)
			return
		}
		if count != 2 || r.Header.Get("Authorization") != "Bearer "+newToken || r.Header.Get("x-codex-image-turn-id") != turn {
			t.Error("auth recovery request changed logical identity or reused old token")
		}
		_, _ = io.WriteString(w, codexBody(content))
	}))
	defer server.Close()
	_, err := codexRegistry(t, server.URL, file).GenerateImage(t.Context(), imageapi.Request{Provider: "openai-codex-images", Model: "gpt-image-2", Prompt: "draw"})
	if err != nil || calls.Load() != 2 {
		t.Fatalf("401 recovery: calls=%d error=%v", calls.Load(), err)
	}
}

func TestCodexImagesFailuresKeepKnownMetadataWithoutReplay(t *testing.T) {
	for _, tc := range []struct {
		name              string
		status            int
		body, code, stage string
	}{
		{"quota", 429, `{"error":{"code":"usage_limit_reached"},"usage":{"total_tokens":5}}`, "quota", "request"},
		{"throttle", 429, `{"error":{"code":"rate_limit_exceeded"}}`, "rate_limit", "request"},
		{"refusal", 400, `{"error":{"code":"moderation_blocked"}}`, "refusal", "request"},
		{"empty", 200, `{"data":[],"usage":{"total_tokens":5}}`, "response", "response"},
		{"bad-base64", 200, `{"data":[{"b64_json":"%%%"}],"usage":{"total_tokens":5}}`, "response", "response"},
		{"multiple", 200, `{"data":[{"b64_json":"a"},{"b64_json":"b"}]}`, "response", "response"},
		{"server", 500, `{"error":{"code":"server_error"}}`, "transport", "request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "auth.json")
			writeCodexExternal(t, file, codexToken("test"))
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("x-codex-imagegen-request-id", "failure-request")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			_, err := codexRegistry(t, server.URL, file).GenerateImage(t.Context(), imageapi.Request{Provider: "openai-codex-images", Model: "gpt-image-2", Prompt: "draw"})
			var details *imageapi.Error
			if !errors.As(err, &details) || details.Code != tc.code || details.Stage != tc.stage || details.RequestID != "failure-request" || calls.Load() != 1 {
				t.Fatalf("failure: %+v calls=%d", details, calls.Load())
			}
			if strings.Contains(tc.body, `"total_tokens":5`) && string(details.Usage) != `{"total_tokens":5}` {
				t.Fatalf("known usage lost: %+v", details)
			}
		})
	}
}

type codexHTTPFunc func(*http.Request) (*http.Response, error)

func (f codexHTTPFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestCodexImagesCancellationRetainsReceivedMetadata(t *testing.T) {
	file := filepath.Join(t.TempDir(), "auth.json")
	writeCodexExternal(t, file, codexToken("test"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := codexBody(codexPNG(t, 20))
	r := codexRegistry(t, "https://image.example", file)
	r.opts.HTTPClient = codexHTTPFunc(func(req *http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": []string{"completed-request"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	_, err := r.GenerateImage(ctx, imageapi.Request{Provider: "openai-codex-images", Model: "gpt-image-2", Prompt: "draw"})
	var details *imageapi.Error
	if !errors.As(err, &details) || details.Code != "cancelled" || details.RequestID != "completed-request" || string(details.Usage) != `{"total_tokens":7}` {
		t.Fatalf("metadata lost on cancellation: %+v", details)
	}
}

func TestCodexImagesRedirectDoesNotForwardCredentials(t *testing.T) {
	file := filepath.Join(t.TempDir(), "auth.json")
	writeCodexExternal(t, file, codexToken("test"))
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	_, err := codexRegistry(t, server.URL, file).GenerateImage(t.Context(), imageapi.Request{Provider: "openai-codex-images", Model: "gpt-image-2", Prompt: "draw"})
	if err == nil || leaked.Load() != 0 {
		t.Fatal("redirect followed with image credentials")
	}
}
