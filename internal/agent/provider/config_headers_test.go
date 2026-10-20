package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type headerFixtureClient func(*http.Request) (*http.Response, error)

func (f headerFixtureClient) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestRegistryModelHeaderMerge(t *testing.T) {
	for _, provider := range []string{"custom", "opencode-go"} {
		for _, names := range []struct{ base, model string }{
			{"x-route", "x-route"},
			{"x-route", "X-Route"},
			{"X-Route", "x-ROUTE"},
		} {
			for _, action := range []string{"override", "delete", "delete-all"} {
				t.Run(provider+"/"+names.base+"/"+names.model+"/"+action, func(t *testing.T) {
					modelHeaders := fmt.Sprintf(`{"%s":"model"}`, names.model)
					want := "model"
					if action == "delete" {
						modelHeaders = fmt.Sprintf(`{"%s":null}`, names.model)
						want = ""
					} else if action == "delete-all" {
						modelHeaders = "null"
						want = ""
					}
					configuration := fmt.Sprintf(`{"providers":{"%s":{
						"api":"chat-completions","baseURL":"http://fixture.invalid","path":"/chat/completions",
						"keyFile":%q,"headers":{"%s":"base","X-Keep":"kept"},
						"parameters":{"remove":1,"keep":2},
						"models":{"header-target":{"headers":%s,"parameters":{"remove":null}}}
					}}}`, provider, writeTestKey(t), names.base, modelHeaders)
					r, _ := registryFixture(t, configuration)
					wantRoute := "base"
					calls := 0
					r.opts.HTTPClient = headerFixtureClient(func(request *http.Request) (*http.Response, error) {
						calls++
						values := request.Header.Values("X-Route")
						if (wantRoute == "" && len(values) != 0) || (wantRoute != "" && (len(values) != 1 || values[0] != wantRoute)) {
							t.Errorf("sent X-Route = %v, want %q", values, wantRoute)
						}
						payload := "data: " + `{"id":"x","object":"chat.completion.chunk","created":1,"model":"fixture","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
						return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
					})
					// Resolve both before and after the target to catch shared-map mutations.
					for _, model := range []string{"before", "header-target", "before", "after"} {
						wantRoute = "base"
						if model == "header-target" {
							wantRoute = want
						}
						resolved, err := r.resolve(provider, model)
						if err != nil {
							t.Fatal(err)
						}
						profile := resolved.(*OpenRouter).profile
						routes := 0
						for key, value := range profile.Headers {
							if strings.EqualFold(key, "X-Route") {
								routes++
								if value != wantRoute {
									t.Errorf("model %s header %s = %q, want %q", model, key, value, wantRoute)
								}
							}
						}
						if (wantRoute == "" && routes != 0) || (wantRoute != "" && routes != 1) {
							t.Errorf("model %s has %d route keys: %v", model, routes, profile.Headers)
						}
						wantKeep := "kept"
						if model == "header-target" && action == "delete-all" {
							wantKeep = ""
						}
						if profile.Headers["X-Keep"] != wantKeep {
							t.Errorf("unrelated header: %v", profile.Headers)
						}
						if string(profile.Parameters["keep"]) != "2" || (model == "header-target" && profile.Parameters["remove"] != nil) || (model != "header-target" && string(profile.Parameters["remove"]) != "1") {
							t.Errorf("model %s parameters: %v", model, profile.Parameters)
						}
						request := testRequest()
						request.Provider, request.Model, request.RunID = provider, model, "header-fixture-run"
						if _, err := r.Generate(t.Context(), request); err != nil {
							t.Fatal(err)
						}
					}
					if calls != 4 {
						t.Errorf("transport calls = %d, want 4", calls)
					}
					// Deletion directives must survive config loading and resolution.
					definition, err := decodeDefinition(r.definitions.Providers[provider])
					if err != nil {
						t.Fatal(err)
					}
					_, overlay, err := splitModel(definition.Models["header-target"])
					if err != nil {
						t.Fatal(err)
					}
					parameters := overlay["parameters"].(map[string]any)
					if value, ok := parameters["remove"]; !ok || value != nil {
						t.Fatalf("parameter deletion directive lost: %v", overlay)
					}
					if action == "delete" {
						headers := overlay["headers"].(map[string]any)
						data, _ := json.Marshal(headers)
						if len(headers) != 1 || !strings.Contains(string(data), ":null") {
							t.Fatalf("header deletion directive lost: %s", data)
						}
					} else if action == "delete-all" {
						if value, ok := overlay["headers"]; !ok || value != nil {
							t.Fatalf("whole-header deletion directive lost: %v", overlay)
						}
					}
				})
			}
		}
	}
}

func TestRegistryModelParameterHeadersMerge(t *testing.T) {
	for _, provider := range []string{"custom", "opencode-go"} {
		t.Run(provider, func(t *testing.T) {
			configuration := fmt.Sprintf(`{"providers":{"%s":{
				"api":"chat-completions","baseURL":"http://fixture.invalid","path":"/chat/completions",
				"keyFile":%q,
				"parameters":{"headers":{"x-body-field":"value","nullable":null},"nested":{"headers":{"x-nested-field":"nested-value"}},"remove":1,"keep":2},
				"models":{"parameter-target":{"parameters":{"headers":{},"nested":{"headers":{}},"remove":null}}}
			}}}`, provider, writeTestKey(t))
			r, _ := registryFixture(t, configuration)
			checkParameters := func(parameters map[string]json.RawMessage, model string) {
				t.Helper()
				var headers map[string]any
				if err := json.Unmarshal(parameters["headers"], &headers); err != nil {
					t.Fatal(err)
				}
				if value, ok := headers["nullable"]; !ok || value != nil || len(headers) != 2 || headers["x-body-field"] != "value" {
					t.Errorf("model %s body headers changed: %s", model, parameters["headers"])
				}
				var nested struct {
					Headers map[string]string `json:"headers"`
				}
				if err := json.Unmarshal(parameters["nested"], &nested); err != nil {
					t.Fatal(err)
				}
				if len(nested.Headers) != 1 || nested.Headers["x-nested-field"] != "nested-value" {
					t.Errorf("model %s nested body headers changed: %s", model, parameters["nested"])
				}
				if string(parameters["keep"]) != "2" || (model == "parameter-target" && parameters["remove"] != nil) || (model != "parameter-target" && string(parameters["remove"]) != "1") {
					t.Errorf("model %s parameters: %v", model, parameters)
				}
			}
			model := ""
			calls := 0
			r.opts.HTTPClient = headerFixtureClient(func(request *http.Request) (*http.Response, error) {
				calls++
				var body map[string]json.RawMessage
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				checkParameters(body, model)
				payload := "data: " + `{"id":"x","object":"chat.completion.chunk","created":1,"model":"fixture","choices":[{"index":0,"delta":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
			})
			for _, id := range []string{"before", "parameter-target", "before", "after"} {
				model = id
				resolved, err := r.resolve(provider, model)
				if err != nil {
					t.Fatal(err)
				}
				checkParameters(resolved.(*OpenRouter).profile.Parameters, model)
				request := testRequest()
				request.Provider, request.Model, request.RunID = provider, model, "parameter-fixture-run"
				if _, err := r.Generate(t.Context(), request); err != nil {
					t.Fatal(err)
				}
			}
			if calls != 4 {
				t.Errorf("transport calls = %d, want 4", calls)
			}
			definition, err := decodeDefinition(r.definitions.Providers[provider])
			if err != nil {
				t.Fatal(err)
			}
			_, overlay, err := splitModel(definition.Models["parameter-target"])
			if err != nil {
				t.Fatal(err)
			}
			parameters := overlay["parameters"].(map[string]any)
			if value, ok := parameters["remove"]; !ok || value != nil {
				t.Fatalf("parameter deletion directive lost: %v", overlay)
			}
		})
	}
}
