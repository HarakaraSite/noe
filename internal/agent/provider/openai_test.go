package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenAIResponsesToolCycleRetainsNativeReasoning(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.URL.Path != "/custom/responses" || r.Header.Get("x-test-session") != "instance-session" || r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("User-Agent") != "noe/test" {
			t.Error("wrong OpenAI transport configuration")
		}
		var body map[string]json.RawMessage
		json.NewDecoder(r.Body).Decode(&body)
		if string(body["reasoning"]) != `{"effort":"high"}` || string(body["prompt_cache_key"]) != `"stable-cache"` || string(body["store"]) != "false" {
			t.Errorf("wrong effort/cache: %v", body)
		}
		if call == 2 && (!strings.Contains(string(body["input"]), "encrypted-reasoning") || !strings.Contains(string(body["input"]), "function_call_output")) {
			t.Error("Responses tool continuation lost reasoning or tool output")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		output := `[{"type":"reasoning","id":"rs","summary":[],"encrypted_content":"encrypted-reasoning"},{"type":"function_call","id":"fc","call_id":"call","name":"run_js","arguments":"{\"code\":\"console.log(42)\"}","status":"completed"}]`
		if call == 2 {
			output = `[{"type":"message","id":"msg","role":"assistant","status":"completed","content":[{"type":"output_text","text":"42","annotations":[]}]}]`
		}
		writeSSE(w, `{"type":"response.reasoning_summary_text.delta","delta":"checking","sequence_number":1}`)
		var items []json.RawMessage
		json.Unmarshal([]byte(output), &items)
		for index, item := range items {
			writeSSE(w, fmt.Sprintf(`{"type":"response.output_item.done","output_index":%d,"item":%s}`, index, item))
		}
		writeSSE(w, `{"type":"response.completed","sequence_number":2,"response":`+nativeResponseFixture(`[]`)+`}`)
		<-r.Context().Done() // terminal event must return without waiting for EOF
	}))
	defer server.Close()
	key := filepath.Join(t.TempDir(), "key")
	os.WriteFile(key, []byte("fixture-key"), 0600)
	p := NewOpenAI(Options{Profile: &Definition{SDK: "openai", Auth: "api-key", API: "responses", BaseURL: server.URL, Path: "/custom/responses", KeyFile: key, SessionHeader: "x-test-session", SessionField: "prompt_cache_key", EffortField: "reasoning.effort", Headers: map[string]string{"User-Agent": "noe/test"}}})
	var observations []StreamEvent
	request := Request{RunID: "first-run", SessionID: "instance-session", Model: "gpt-6-luna", Effort: "high", CacheKey: "stable-cache", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"compute"}`)}, Tools: []json.RawMessage{json.RawMessage(`{"type":"function","function":{"name":"run_js","parameters":{"type":"object"}}}`)}, Observe: func(event StreamEvent) { observations = append(observations, event) }}
	first, err := p.Generate(context.Background(), request)
	if err != nil || first.FinishReason != "tool_calls" {
		t.Fatalf("first response: %v", err)
	}
	request.Messages = append(request.Messages, first.Message, json.RawMessage(`{"role":"tool","tool_call_id":"call","content":"42"}`))
	request.RunID = "second-run"
	last, err := p.Generate(context.Background(), request)
	if err != nil || last.FinishReason != "stop" || !strings.Contains(string(last.Message), "42") || len(observations) != 2 {
		t.Fatalf("last response: %v observations=%v", err, observations)
	}
}

func TestOpenAICompatibleChatWithoutAuthentication(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "must-not-leak")
	t.Setenv("OPENAI_ORG_ID", "must-not-leak-org")
	t.Setenv("OPENAI_PROJECT_ID", "must-not-leak-project")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("OpenAI-Organization") != "" || r.Header.Get("OpenAI-Project") != "" {
			t.Error("ambient OpenAI credentials leaked to local endpoint")
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Error(r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, text := range []string{"first ", "second"} {
			writeSSE(w, fmt.Sprintf(`{"id":"x","object":"chat.completion.chunk","created":1,"model":"local","choices":[{"index":0,"delta":{"role":"assistant","reasoning_content":%q},"finish_reason":null}]}`, text))
		}
		writeSSE(w, `{"id":"x","object":"chat.completion.chunk","created":1,"model":"local","choices":[{"index":0,"delta":{"content":"local answer"},"finish_reason":"stop"}]}`)
		writeSSE(w, "[DONE]")
	}))
	defer server.Close()
	p := NewOpenAI(Options{Profile: &Definition{SDK: "openai", Auth: "none", API: "chat-completions", BaseURL: server.URL + "/v1", Path: "/chat/completions", ReasoningField: "reasoning_content"}})
	answer, err := p.Generate(context.Background(), Request{Model: "local", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hello"}`)}})
	if err != nil || !strings.Contains(string(answer.Message), "first second") {
		t.Fatalf("local answer: %v %s", err, answer.Message)
	}
}

func TestOpenAIResponsesCancellationAndInStreamQuota(t *testing.T) {
	for _, quota := range []bool{false, true} {
		t.Run(fmt.Sprint(quota), func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				if quota {
					writeSSE(w, `{"type":"response.failed","response":{"status":"failed","error":{"code":"subscription_sharing_usage_limit_exceeded","message":"private upstream detail"}},"sequence_number":1}`)
					return
				}
				fmt.Fprint(w, ": waiting\n\n")
				w.(http.Flusher).Flush()
				close(started)
				<-r.Context().Done()
			}))
			defer server.Close()
			p := NewOpenAI(Options{Profile: &Definition{SDK: "openai", Auth: "none", API: "responses", BaseURL: server.URL, Path: "/responses"}})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if !quota {
				go func() { <-started; cancel() }()
			}
			_, err := p.Generate(ctx, Request{Model: "fixture", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"wait"}`)}})
			if !quota && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
			var providerErr *ProviderError
			if quota && (!errors.As(err, &providerErr) || providerErr.Kind != ErrorQuota || strings.Contains(err.Error(), "private")) {
				t.Fatalf("in-stream quota: %v", err)
			}
		})
	}
}
