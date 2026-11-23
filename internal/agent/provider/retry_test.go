package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"io"
	"net/http"
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
				if len(attempts) != 3 || attempts[0] != 0 ||
					attempts[1] < 1500*time.Millisecond || attempts[1] > 2*time.Second ||
					attempts[2]-attempts[1] < 3*time.Second || attempts[2]-attempts[1] > 4*time.Second {
					t.Fatalf("fallback retry intervals outside jitter bounds: %v", attempts)
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

func TestServerRetryDelayHints(t *testing.T) {
	future := time.Now().Add(10 * time.Second).UTC().Format(http.TimeFormat)
	for _, test := range []struct {
		name       string
		headers    http.Header
		want       time.Duration
		wantValid  bool
		wantLong   bool
		dateWindow bool
	}{
		{name: "millisecond precedence", headers: http.Header{"Retry-After-Ms": {"1250"}, "Retry-After": {"5"}}, want: 1250 * time.Millisecond, wantValid: true},
		{name: "seconds", headers: http.Header{"Retry-After": {"2.5"}}, want: 2500 * time.Millisecond, wantValid: true},
		{name: "http date", headers: http.Header{"Retry-After": {future}}, wantValid: true, dateWindow: true},
		{name: "invalid milliseconds uses seconds", headers: http.Header{"Retry-After-Ms": {"not-a-number"}, "Retry-After": {"3"}}, want: 3 * time.Second, wantValid: true},
		{name: "negative hint falls back", headers: http.Header{"Retry-After-Ms": {"-1"}, "Retry-After": {"-2"}}},
		{name: "malformed hint falls back", headers: http.Header{"Retry-After-Ms": {"NaN"}, "Retry-After": {"later"}}},
		{name: "too long", headers: http.Header{"Retry-After": {"61"}}, wantValid: true, wantLong: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, valid, tooLong := serverRetryDelay(test.headers)
			if valid != test.wantValid || tooLong != test.wantLong {
				t.Fatalf("serverRetryDelay() valid=%t tooLong=%t, want %t/%t", valid, tooLong, test.wantValid, test.wantLong)
			}
			if test.dateWindow {
				if got < 9*time.Second || got > 10*time.Second {
					t.Fatalf("date delay=%v, want about 10s", got)
				}
			} else if got != test.want {
				t.Fatalf("delay=%v, want %v", got, test.want)
			}
		})
	}
}

func TestGenerationRetryUsesServerHintAndHonorsLongWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		start := time.Now()
		failure := classifyHTTPResponseError(http.StatusTooManyRequests, nil, "", http.Header{
			"Retry-After-Ms": {"1250"}, "Retry-After": {"5"},
		})
		_, err := generateWithRetry(t.Context(), testRequest(), func(context.Context, Request) (Response, error) {
			calls++
			if calls == 2 {
				return Response{Message: json.RawMessage(`{"role":"assistant"}`)}, nil
			}
			return Response{}, failure
		})
		if err != nil || calls != 2 || time.Since(start) != 1250*time.Millisecond {
			t.Fatalf("server hint retry calls=%d elapsed=%v err=%v", calls, time.Since(start), err)
		}
	})

	synctest.Test(t, func(t *testing.T) {
		calls := 0
		failure := classifyHTTPResponseError(http.StatusTooManyRequests, nil, "", http.Header{
			"Retry-After": {"61"}, "X-Should-Retry": {"true"},
		})
		_, err := generateWithRetry(t.Context(), testRequest(), func(context.Context, Request) (Response, error) {
			calls++
			return Response{}, failure
		})
		assertProviderError(t, err, ErrorRateLimit)
		if calls != 1 {
			t.Fatalf("too-long server hint retried %d times", calls)
		}
	})
}

func TestRetryDirectiveCannotOverrideQuotaOrAuthentication(t *testing.T) {
	for _, kind := range []ErrorKind{ErrorAuthentication, ErrorQuota} {
		failure := providerError(kind, http.StatusTooManyRequests, nil)
		applyRetryMetadata(failure, http.Header{"X-Should-Retry": {"true"}})
		if retryableGenerationError(failure) {
			t.Fatalf("x-should-retry:true enabled retry for %s", kind)
		}
	}
	failure := classifyHTTPResponseError(http.StatusServiceUnavailable, nil, "", http.Header{"X-Should-Retry": {"false"}})
	if retryableGenerationError(failure) {
		t.Fatal("x-should-retry:false did not disable a transient retry")
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

func sameJSONDocument(a, b string) bool {
	left, right := jsontext.Value([]byte(a)), jsontext.Value([]byte(b))
	if err := left.Format(jsontext.ReorderRawObjects(true)); err != nil {
		return false
	}
	if err := right.Format(jsontext.ReorderRawObjects(true)); err != nil {
		return false
	}
	return bytes.Equal(left, right)
}

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
					if len(bodies) != 3 || !sameJSONDocument(bodies[0], bodies[1]) || !sameJSONDocument(bodies[1], bodies[2]) || thinking.String() != "observed observed " {
						t.Fatalf("retry changed input or lost partial thinking: bodies=%v thinking=%q", bodies, thinking.String())
					}
					if len(attempts) != 3 || attempts[0] != 0 ||
						attempts[1] < 1500*time.Millisecond || attempts[1] > 2*time.Second ||
						attempts[2]-attempts[1] < 3*time.Second || attempts[2]-attempts[1] > 4*time.Second {
						t.Fatalf("adapter fallback retry intervals outside jitter bounds: %v", attempts)
					}
				})
			})
		}
	}
}

func TestGenerationAdaptersHonorHTTPRetryHintsAndQuotaCodes(t *testing.T) {
	for _, sdk := range []string{"openrouter", "openai"} {
		t.Run(sdk, func(t *testing.T) {
			key := writeTestKey(t)
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				start := time.Now()
				client := retryHTTPClient(func(r *http.Request) (*http.Response, error) {
					if _, err := io.ReadAll(r.Body); err != nil {
						t.Fatal(err)
					}
					calls++
					if calls == 1 {
						return &http.Response{
							StatusCode: http.StatusServiceUnavailable,
							Header:     http.Header{"Content-Type": {"application/json"}, "Retry-After-Ms": {"1250"}},
							Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"server_error","message":"private diagnostic"}}`)),
						}, nil
					}
					payload := "data: " + `{"id":"x","object":"chat.completion.chunk","created":1,"model":"test/model","choices":[{"index":0,"delta":{"role":"assistant","content":"success"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]\n\n"
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
				})
				opts := Options{KeyFile: key, BaseURL: "http://fixture.invalid", HTTPClient: client, Profile: &Definition{SDK: sdk, API: "chat-completions", Auth: "api-key", KeyFile: key, BaseURL: "http://fixture.invalid", Path: "/fixture"}}
				var p Provider = NewOpenRouter(opts)
				if sdk == "openai" {
					p = NewOpenAI(opts)
				}
				result, err := p.Generate(t.Context(), testRequest())
				if err != nil || !strings.Contains(string(result.Message), "success") {
					t.Fatalf("hinted recovery response=%s err=%v", result.Message, err)
				}
				if calls != 2 || time.Since(start) != 1250*time.Millisecond {
					t.Fatalf("hinted retry calls=%d elapsed=%v, want 2/1.25s", calls, time.Since(start))
				}
			})

			calls := 0
			client := retryHTTPClient(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{
					StatusCode: http.StatusTooManyRequests,
					Header:     http.Header{"Content-Type": {"application/json"}, "X-Should-Retry": {"true"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"insufficient_quota","message":"private diagnostic"}}`)),
				}, nil
			})
			opts := Options{KeyFile: key, BaseURL: "http://fixture.invalid", HTTPClient: client, Profile: &Definition{SDK: sdk, API: "chat-completions", Auth: "api-key", KeyFile: key, BaseURL: "http://fixture.invalid", Path: "/fixture"}}
			var p Provider = NewOpenRouter(opts)
			if sdk == "openai" {
				p = NewOpenAI(opts)
			}
			_, err := p.Generate(context.Background(), testRequest())
			assertProviderError(t, err, ErrorQuota)
			if calls != 1 || strings.Contains(err.Error(), "private diagnostic") {
				t.Fatalf("quota response calls=%d err=%v", calls, err)
			}

			calls = 0
			client = retryHTTPClient(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Header:     http.Header{"Content-Type": {"application/json"}, "X-Should-Retry": {"false"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"temporary"}}`)),
				}, nil
			})
			opts.HTTPClient = client
			p = NewOpenRouter(opts)
			if sdk == "openai" {
				p = NewOpenAI(opts)
			}
			_, err = p.Generate(context.Background(), testRequest())
			assertProviderError(t, err, ErrorTransport)
			if calls != 1 {
				t.Fatalf("x-should-retry:false response made %d attempts", calls)
			}
		})
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

func TestGenerationRetriesTemporaryResponsesFailureCodes(t *testing.T) {
	for _, sdk := range []string{"openrouter", "openai"} {
		for _, code := range []string{"server_error", "subscription_sharing_usage_unavailable", "subscription_sharing_user_unavailable", "user_unavailable"} {
			t.Run(sdk+"/"+code, func(t *testing.T) {
				key := writeTestKey(t)
				synctest.Test(t, func(t *testing.T) {
					calls := 0
					client := retryHTTPClient(func(r *http.Request) (*http.Response, error) {
						if _, err := io.ReadAll(r.Body); err != nil {
							t.Fatal(err)
						}
						calls++
						payload := ""
						if calls == 1 {
							response := strings.Replace(nativeResponseFixture(`[]`), `"status":"completed"`, `"status":"failed"`, 1)
							response = strings.Replace(response, `"error":null`, `"error":{"code":"`+code+`"}`, 1)
							payload = "data: " + `{"type":"response.failed","sequence_number":1,"response":` + response + "}\n\n"
						} else {
							output := `[{"type":"message","id":"msg","role":"assistant","status":"completed","content":[{"type":"output_text","text":"recovered","annotations":[]}]}]`
							payload = "data: " + `{"type":"response.output_item.done","output_index":0,"item":` + output[1:len(output)-1] + "}\n\n"
							payload += "data: " + `{"type":"response.completed","sequence_number":2,"response":` + nativeResponseFixture(output) + "}\n\n"
						}
						return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
					})
					opts := Options{KeyFile: key, BaseURL: "http://fixture.invalid", HTTPClient: client, Profile: &Definition{SDK: sdk, API: "responses", Auth: "api-key", KeyFile: key, BaseURL: "http://fixture.invalid", Path: "/fixture"}}
					var p Provider = NewOpenRouter(opts)
					if sdk == "openai" {
						p = NewOpenAI(opts)
					}
					result, err := p.Generate(t.Context(), testRequest())
					if err != nil || !strings.Contains(string(result.Message), "recovered") {
						t.Fatalf("temporary failure was not retried: response=%s err=%v", result.Message, err)
					}
					if calls != 2 {
						t.Fatalf("temporary failure made %d attempts, want 2", calls)
					}
				})
			})
		}
	}
}

func TestGenerationClassifiesResponsesTopLevelErrorCodes(t *testing.T) {
	for _, sdk := range []string{"openrouter", "openai"} {
		for _, test := range []struct {
			code      string
			kind      ErrorKind
			wantCalls int
		}{
			{code: "insufficient_quota", kind: ErrorQuota, wantCalls: 1},
			{code: "server_error", kind: ErrorTransport, wantCalls: 2},
			{code: "subscription_sharing_usage_unavailable", kind: ErrorTransport, wantCalls: 2},
			{code: "user_unavailable", kind: ErrorTransport, wantCalls: 2},
		} {
			t.Run(sdk+"/"+test.code, func(t *testing.T) {
				key := writeTestKey(t)
				synctest.Test(t, func(t *testing.T) {
					calls := 0
					client := retryHTTPClient(func(r *http.Request) (*http.Response, error) {
						if _, err := io.ReadAll(r.Body); err != nil {
							t.Fatal(err)
						}
						calls++
						payload := ""
						if calls == 1 {
							payload = "data: " + `{"type":"error","sequence_number":1,"code":"` + test.code + `","message":"private diagnostic"}` + "\n\n"
						} else {
							output := `[{"type":"message","id":"msg","role":"assistant","status":"completed","content":[{"type":"output_text","text":"recovered","annotations":[]}]}]`
							payload = "data: " + `{"type":"response.output_item.done","output_index":0,"item":` + output[1:len(output)-1] + "}\n\n"
							payload += "data: " + `{"type":"response.completed","sequence_number":2,"response":` + nativeResponseFixture(output) + "}\n\n"
						}
						return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(payload))}, nil
					})
					opts := Options{KeyFile: key, BaseURL: "http://fixture.invalid", HTTPClient: client, Profile: &Definition{SDK: sdk, API: "responses", Auth: "api-key", KeyFile: key, BaseURL: "http://fixture.invalid", Path: "/fixture"}}
					var p Provider = NewOpenRouter(opts)
					if sdk == "openai" {
						p = NewOpenAI(opts)
					}
					result, err := p.Generate(t.Context(), testRequest())
					if calls != test.wantCalls {
						t.Fatalf("error event made %d attempts, want %d (err=%v)", calls, test.wantCalls, err)
					}
					if test.kind == ErrorQuota {
						assertProviderError(t, err, ErrorQuota)
						if strings.Contains(err.Error(), "private diagnostic") {
							t.Fatalf("error event body leaked: %v", err)
						}
					} else if err != nil || !strings.Contains(string(result.Message), "recovered") {
						t.Fatalf("temporary error event did not recover: response=%s err=%v", result.Message, err)
					}
				})
			})
		}
	}
}

func TestOpenRouterClassifiesTypedErrorsAfterHTTP200(t *testing.T) {
	for _, test := range []struct {
		name     string
		api      string
		response string
		kind     ErrorKind
		retry    bool
	}{
		{"responses overload", "responses", `"provider_overloaded"`, ErrorTransport, true},
		{"responses authentication", "responses", `"authentication"`, ErrorAuthentication, false},
		{"responses quota", "responses", `"payment_required"`, ErrorQuota, false},
		{"responses validation", "responses", `"invalid_prompt"`, ErrorResponse, false},
		{"chat overload", "chat-completions", `"code":503,"metadata":{"error_type":"provider_overloaded"}`, ErrorTransport, true},
		{"chat authentication", "chat-completions", `"code":401,"metadata":{"error_type":"authentication"}`, ErrorAuthentication, false},
		{"chat quota", "chat-completions", `"code":402,"metadata":{"error_type":"payment_required"}`, ErrorQuota, false},
		{"chat rate limit", "chat-completions", `"code":429,"metadata":{"error_type":"rate_limit_exceeded"}`, ErrorRateLimit, true},
		{"chat typed policy", "chat-completions", `"code":403,"metadata":{"error_type":"content_policy_violation"}`, ErrorResponse, false},
		{"chat numeric fallback", "chat-completions", `"code":502`, ErrorTransport, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			key := writeTestKey(t)
			synctest.Test(t, func(t *testing.T) {
				const chatChunk = `{"id":"gen-test","object":"chat.completion.chunk","created":1,"model":"test/model","provider":"Fixture",`
				var failed, completed string
				if test.api == "responses" {
					response := strings.Replace(nativeResponseFixture(`[]`), `"status":"completed"`, `"status":"failed"`, 1)
					response = strings.Replace(response, `"error":null`, `"error":{"code":"server_error","message":"private diagnostic"},"error_type":`+test.response, 1)
					failed = `{"type":"response.failed","sequence_number":1,"response":` + response + "}"
					output := `[{"id":"msg","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"recovered","annotations":[]}]}]`
					completed = `{"type":"response.completed","sequence_number":1,"response":` + nativeResponseFixture(output) + "}"
				} else {
					failed = chatChunk + `"error":{"message":"private diagnostic",` + test.response + `},"choices":[{"index":0,"delta":{"content":""},"finish_reason":"error"}]}`
					completed = chatChunk + `"choices":[{"index":0,"delta":{"role":"assistant","content":"recovered"},"finish_reason":"stop"}]}` + "\n\ndata: [DONE]"
				}
				calls := 0
				started := time.Now()
				client := retryHTTPClient(func(*http.Request) (*http.Response, error) {
					calls++
					event := failed
					if calls > 1 {
						event = completed
					}
					headers := http.Header{"Content-Type": {"text/event-stream"}, "Retry-After-Ms": {"25"}}
					if test.kind == ErrorAuthentication || test.kind == ErrorQuota {
						headers.Set("X-Should-Retry", "true")
					}
					return &http.Response{StatusCode: http.StatusOK, Header: headers, Body: io.NopCloser(strings.NewReader("data: " + event + "\n\n"))}, nil
				})
				p := NewOpenRouter(Options{KeyFile: key, BaseURL: "http://fixture.invalid", HTTPClient: client, Profile: &Definition{API: test.api, Path: "/fixture"}})
				result, err := p.Generate(t.Context(), testRequest())
				if test.retry {
					if calls != 2 || time.Since(started) != 25*time.Millisecond || err != nil || !strings.Contains(string(result.Message), "recovered") {
						t.Fatalf("attempts=%d elapsed=%v response=%s err=%v", calls, time.Since(started), result.Message, err)
					}
					return
				}
				assertProviderError(t, err, test.kind)
				if calls != 1 || strings.Contains(err.Error(), "private diagnostic") {
					t.Fatalf("attempts=%d err=%v", calls, err)
				}
			})
		})
	}
}
