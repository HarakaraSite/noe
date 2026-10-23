package provider

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"testing/synctest"
)

func TestAdaptersPreserveCyberPolicyWithoutRetry(t *testing.T) {
	for _, sdk := range []string{"openai", "openrouter"} {
		for _, api := range []string{"responses", "chat-completions"} {
			for _, entry := range []string{"http403", "http429", "stream", "failed", "incomplete"} {
				if (entry == "failed" || entry == "incomplete") && api != "responses" {
					continue
				}
				t.Run(sdk+"/"+api+"/"+entry, func(t *testing.T) {
					key := writeTestKey(t)
					credential, err := os.ReadFile(key)
					if err != nil {
						t.Fatal(err)
					}
					upstream, _ := json.Marshal(map[string]string{"code": "cyber_policy", "message": "Cybersecurity check blocked this request. Daybreak access may be required. " + strings.TrimSpace(string(credential))})
					synctest.Test(t, func(t *testing.T) {
						calls := 0
						client := retryHTTPClient(func(*http.Request) (*http.Response, error) {
							calls++
							status, contentType := 200, "text/event-stream"
							payload := ""
							switch entry {
							case "http403", "http429":
								status, contentType = 403, "application/json"
								if entry == "http429" {
									status = 429
								}
								payload = `{"error":` + string(upstream) + `}`
							case "failed", "incomplete":
								payload = "data: " + `{"type":"response.` + entry + `","sequence_number":1,"response":{"status":"` + entry + `","error":` + string(upstream) + "}}\n\n"
							case "stream":
								// Both SDKs must preserve new error codes even if their
								// event decoders reject an otherwise valid error event.
								if api == "responses" {
									payload = "data: " + `{"type":"error","sequence_number":1,` + string(upstream)[1:] + "\n\n"
								} else {
									payload = "data: " + `{"error":` + string(upstream) + "}\n\n"
								}
							}
							return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}, "X-Request-Id": {"upstream-request-123"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
						})
						opts := Options{KeyFile: key, BaseURL: "http://fixture.invalid", HTTPClient: client, Profile: &Definition{SDK: sdk, API: api, Auth: "api-key", KeyFile: key, BaseURL: "http://fixture.invalid", Path: "/fixture"}}
						var adapter Provider = NewOpenRouter(opts)
						if sdk == "openai" {
							adapter = NewOpenAI(opts)
						}
						response, err := adapter.Generate(t.Context(), testRequest())
						var failure *ProviderError
						if !errors.As(err, &failure) || failure.Kind != ErrorPolicy || calls != 1 || len(response.Message) != 0 {
							t.Fatalf("policy response=%s error=%v calls=%d", response.Message, err, calls)
						}
						details := failure.Details
						if details == nil || details.Code != "cyber_policy" || details.ProviderCode != "cyber_policy" || details.RequestID != "upstream-request-123" || !strings.Contains(details.ProviderMessage, "Daybreak") || !strings.Contains(details.ProviderMessage, "[redacted]") {
							t.Fatalf("missing policy diagnostics: %+v", details)
						}
						encoded, _ := json.Marshal(details)
						if strings.Contains(string(encoded), strings.TrimSpace(string(credential))) || strings.Contains(err.Error(), strings.TrimSpace(string(credential))) {
							t.Fatal("credential escaped in policy failure")
						}
					})
				})
			}
		}
	}
}

func TestCyberPolicyRequiresExplicitErrorCode(t *testing.T) {
	for _, raw := range []string{
		`{"error":{"code":"unknown_error","message":"cyber_policy Daybreak"}}`,
		`{"type":"response.output_text.delta","delta":"cyber_policy"}`,
		`{"type":"message","content":[{"type":"output_text","text":"This content can’t be shown"}]}`,
		`{"type":"error","code":403,"message":"cyber_policy"}`,
	} {
		if failure := cyberPolicyError([]byte(raw), 200, "", ""); failure != nil {
			t.Fatalf("inferred policy error from %s", raw)
		}
	}
}
