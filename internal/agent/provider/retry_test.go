package provider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestGenerationRetryTimingAndLimit(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		t.Run(map[bool]string{true: "recovers", false: "exhausted"}[succeeds], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				var attempts []time.Duration
				failure := providerError(ErrorRateLimit, 429, nil)
				response, err := generateWithRetry(t.Context(), testRequest(), func(context.Context, Request) (Response, error) {
					attempts = append(attempts, time.Since(start))
					if succeeds && len(attempts) == 3 {
						return Response{Message: json.RawMessage(`{"role":"assistant","content":"success"}`)}, nil
					}
					return Response{Message: json.RawMessage(`{"content":"discard this failed response"}`)}, failure
				})
				if want := []time.Duration{0, 2 * time.Second, 6 * time.Second}; !reflect.DeepEqual(attempts, want) {
					t.Fatalf("attempt times=%v, want %v", attempts, want)
				}
				if succeeds {
					if err != nil || !strings.Contains(string(response.Message), "success") {
						t.Fatalf("recovery: response=%s err=%v", response.Message, err)
					}
				} else if err != failure || len(response.Message) != 0 {
					t.Fatalf("exhaustion must return the last error without its response: %+v %v", response, err)
				}
			})
		})
	}
}

func TestGenerationDoesNotRetryPermanentErrors(t *testing.T) {
	for _, failure := range []error{
		providerError(ErrorAuthentication, 401, nil), providerError(ErrorQuota, 402, nil),
		providerError(ErrorResponse, 400, nil), providerError(ErrorCancelled, 0, context.Canceled),
		errors.New("unclassified error"),
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				start := time.Now()
				_, err := generateWithRetry(t.Context(), testRequest(), func(context.Context, Request) (Response, error) {
					calls++
					return Response{}, failure
				})
				if err != failure || calls != 1 || time.Since(start) != 0 {
					t.Fatalf("permanent error retried: calls=%d err=%v", calls, err)
				}
			})
		})
	}
}

func TestGenerationRetryWaitHonorsCancellationAndDeadline(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "deadline"}[deadline], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				if deadline {
					cancel()
					ctx, cancel = context.WithTimeout(t.Context(), time.Second)
				}
				defer cancel()
				calls := 0
				done := make(chan error, 1)
				go func() {
					_, err := generateWithRetry(ctx, testRequest(), func(context.Context, Request) (Response, error) {
						calls++
						return Response{}, providerError(ErrorTransport, 503, nil)
					})
					done <- err
				}()
				synctest.Wait()
				if !deadline {
					cancel()
				}
				err := <-done
				if calls != 1 || !errors.Is(err, ctx.Err()) {
					t.Fatalf("retry did not stop during wait: calls=%d err=%v context=%v", calls, err, ctx.Err())
				}
			})
		})
	}
}

type retryHTTPClient func(*http.Request) (*http.Response, error)

func (f retryHTTPClient) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestGenerationAdaptersRetryWithFreshResponseState(t *testing.T) {
	for _, sdk := range []string{"openrouter", "openai"} {
		for _, api := range []string{"chat-completions", "responses"} {
			t.Run(sdk+"/"+api, func(t *testing.T) {
				key := writeTestKey(t)
				synctest.Test(t, func(t *testing.T) {
					var bodies []string
					var attempts []time.Duration
					var thinking strings.Builder
					start := time.Now()
					client := retryHTTPClient(func(r *http.Request) (*http.Response, error) {
						body, err := io.ReadAll(r.Body)
						if err != nil {
							t.Fatal(err)
						}
						bodies = append(bodies, string(body))
						attempts = append(attempts, time.Since(start))
						if len(bodies) == 1 {
							return nil, errors.New("fixture connection failed")
						}
						payload := ""
						if api == "chat-completions" {
							payload = "data: " + `{"id":"x","object":"chat.completion.chunk","created":1,"model":"test/model","choices":[{"index":0,"delta":{"reasoning":"observed "},"finish_reason":null}]}` + "\n\n"
							if len(bodies) == 3 {
								payload += "data: " + `{"id":"x","object":"chat.completion.chunk","created":1,"model":"test/model","choices":[{"index":0,"delta":{"role":"assistant","content":"success"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
							} else {
								payload += string(toolCallStreamBytes(false))
							}
						} else {
							payload = "data: " + `{"type":"response.reasoning_summary_text.delta","delta":"observed ","sequence_number":1}` + "\n\n"
							if len(bodies) == 3 {
								output := `[{"type":"message","id":"msg","role":"assistant","status":"completed","content":[{"type":"output_text","text":"success","annotations":[]}]}]`
								payload += "data: " + `{"type":"response.output_item.done","output_index":0,"item":` + output[1:len(output)-1] + "}\n\n"
								payload += "data: " + `{"type":"response.completed","sequence_number":2,"response":` + nativeResponseFixture(output) + "}\n\n"
							} else {
								payload += "data: " + `{"type":"response.output_item.done","output_index":0,"item":{"type":"function_call","id":"failed-call","call_id":"failed-call","name":"run_js","arguments":"{}","status":"completed"}}` + "\n\n"
							}
						}
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
					})
					opts := Options{KeyFile: key, BaseURL: "http://fixture.invalid", HTTPClient: client, Profile: &Definition{SDK: sdk, API: api, Auth: "api-key", KeyFile: key, BaseURL: "http://fixture.invalid", Path: "/fixture"}}
					var p Provider = NewOpenRouter(opts)
					if sdk == "openai" {
						p = NewOpenAI(opts)
					}
					request := testRequest()
					request.Observe = func(e StreamEvent) {
						if e.Type == "thinking" || e.Type == "thinking_summary" {
							thinking.WriteString(e.Text)
						}
					}
					result, err := p.Generate(t.Context(), request)
					if err != nil || !strings.Contains(string(result.Message), "success") || strings.Contains(string(result.Message), "function_call") || strings.Contains(string(result.Message), "tool_calls") {
						t.Fatalf("failed attempt contaminated the final response: %s %v", result.Message, err)
					}
					if len(bodies) != 3 || bodies[0] != bodies[1] || bodies[1] != bodies[2] || thinking.String() != "observed observed " {
						t.Fatalf("retry changed input or lost partial thinking: bodies=%v thinking=%q", bodies, thinking.String())
					}
					if want := []time.Duration{0, 2 * time.Second, 6 * time.Second}; !reflect.DeepEqual(attempts, want) {
						t.Fatalf("adapter attempt times=%v, want %v", attempts, want)
					}
				})
			})
		}
	}
}

func TestGenerationDoesNotRetryExplicitResponsesFailure(t *testing.T) {
	for _, sdk := range []string{"openrouter", "openai"} {
		t.Run(sdk, func(t *testing.T) {
			key := writeTestKey(t)
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				client := retryHTTPClient(func(*http.Request) (*http.Response, error) {
					calls++
					response := strings.Replace(nativeResponseFixture(`[]`), `"status":"completed"`, `"status":"failed"`, 1)
					response = strings.Replace(response, `"error":null`, `"error":{"code":"subscription_sharing_usage_limit_exceeded","message":"quota"}`, 1)
					payload := "data: " + `{"type":"response.failed","sequence_number":1,"response":` + response + "}\n\n"
					return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
				})
				opts := Options{KeyFile: key, BaseURL: "http://fixture.invalid", HTTPClient: client, Profile: &Definition{SDK: sdk, API: "responses", Auth: "api-key", KeyFile: key, BaseURL: "http://fixture.invalid", Path: "/fixture"}}
				var p Provider = NewOpenRouter(opts)
				if sdk == "openai" {
					p = NewOpenAI(opts)
				}
				_, err := p.Generate(t.Context(), testRequest())
				assertProviderError(t, err, ErrorQuota)
				if calls != 1 {
					t.Fatalf("explicit failure retried %d times", calls)
				}
			})
		})
	}
}
