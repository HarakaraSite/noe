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

func nativeResponseFixture(output string) string {
	return `{"id":"response-1","object":"response","created_at":1,"status":"completed","model":"fixture/model","error":null,"incomplete_details":null,"parallel_tool_calls":true,"tool_choice":"auto","tools":[],"output":` + output + `,"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}`
}

func TestConfiguredResponsesCancellationClosesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": waiting\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	key := filepath.Join(t.TempDir(), "key")
	os.WriteFile(key, []byte("fixture"), 0600)
	client := &countingHTTPClient{base: server.Client(), readStarted: make(chan struct{})}
	p := NewOpenRouter(Options{KeyFile: key, BaseURL: server.URL, HTTPClient: client, Profile: &Definition{API: "responses", Path: "/responses"}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := p.Generate(ctx, Request{Model: "model", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"wait"}`)}})
		done <- err
	}()
	select {
	case <-client.readStarted:
	case <-ctx.Done():
		t.Fatal("stream did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request: %v", err)
	}
	if client.closes.Load() != 1 || client.calls.Load() != 1 {
		t.Fatalf("calls=%d closes=%d", client.calls.Load(), client.closes.Load())
	}
}

func TestConfiguredResponsesToolCycleKeepsNativeReasoning(t *testing.T) {
	var calls atomic.Int32
	firstOutput := `[{"id":"rs-1","type":"reasoning","summary":[{"type":"summary_text","text":"check"}],"encrypted_content":"opaque-reasoning"},{"id":"fc-1","type":"function_call","status":"completed","call_id":"call-1","name":"run_js","arguments":"{\"code\":\"console.log(42)\"}"}]`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index := calls.Add(1)
		if r.URL.Path != "/custom/responses" || r.Header.Get("x-opencode-session") != "run-1" || r.Header.Get("User-Agent") != "noe/test" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("incorrect endpoint/identity")
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if string(body["store"]) != "false" || string(body["reasoning"]) != `{"effort":"high"}` {
			t.Errorf("request settings: %v", body)
		}
		if !strings.Contains(string(body["include"]), "reasoning.encrypted_content") {
			t.Error("stateless reasoning content was not requested")
		}
		if index == 2 && (!strings.Contains(string(body["input"]), "opaque-reasoning") || !strings.Contains(string(body["input"]), "function_call_output")) {
			t.Errorf("native reasoning or tool output lost: %s", body["input"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		output := firstOutput
		if index == 1 {
			writeSSE(w, `{"type":"response.reasoning_summary_text.delta","item_id":"rs-1","output_index":0,"summary_index":0,"delta":"check","sequence_number":1}`)
		} else {
			output = `[{"id":"msg-1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"42","annotations":[]}]}]`
			writeSSE(w, `{"type":"response.output_text.delta","item_id":"msg-1","output_index":0,"content_index":0,"delta":"42","sequence_number":1}`)
		}
		writeSSE(w, `{"type":"response.completed","sequence_number":2,"response":`+nativeResponseFixture(output)+`}`)
		// A terminal frame is enough; Generate must not wait for HTTP EOF.
		<-r.Context().Done()
	}))
	defer server.Close()
	key := filepath.Join(t.TempDir(), "key")
	os.WriteFile(key, []byte("fixture"), 0600)
	p := NewOpenRouter(Options{KeyFile: key, BaseURL: server.URL, Profile: &Definition{API: "responses", Path: "/custom/responses", SessionHeader: "x-opencode-session", EffortField: "reasoning.effort", Headers: map[string]string{"User-Agent": "noe/test"}}})
	var observations []StreamEvent
	request := Request{RunID: "run-1", Model: "fixture/model", Effort: "high", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"calculate"}`)}, Tools: []json.RawMessage{json.RawMessage(`{"type":"function","function":{"name":"run_js","parameters":{"type":"object"}}}`)}, Observe: func(e StreamEvent) { observations = append(observations, e) }}
	first, err := p.Generate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.FinishReason != "tool_calls" || !strings.Contains(string(first.Message), "opaque-reasoning") {
		t.Fatalf("first result: %s", first.Message)
	}
	request.Messages = append(request.Messages, first.Message, json.RawMessage(`{"role":"tool","tool_call_id":"call-1","content":"42"}`))
	last, err := p.Generate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if last.FinishReason != "stop" || !strings.Contains(string(last.Message), "42") || calls.Load() != 2 || len(observations) != 2 || observations[0].Type != "thinking_summary" || observations[1].Type != "message_delta" {
		t.Fatalf("result=%s observations=%v calls=%d", last.Message, observations, calls.Load())
	}
}

func TestConfiguredChatReasoningContentIsConcatenatedAndReplayed(t *testing.T) {
	for _, field := range []string{"reasoning_content", "custom_thinking"} {
		t.Run(field, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				for _, text := range []string{"first ", "second", ""} {
					writeSSE(w, fmt.Sprintf(`{"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"role":"assistant",%q:%q},"finish_reason":null}]}`, field, text))
				}
				writeSSE(w, `{"id":"x","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`)
				writeSSE(w, "[DONE]")
			}))
			defer server.Close()
			key := filepath.Join(t.TempDir(), "key")
			os.WriteFile(key, []byte("fixture"), 0600)
			p := NewOpenRouter(Options{KeyFile: key, BaseURL: server.URL, Profile: &Definition{API: "chat-completions", Path: "/chat/completions", ReasoningField: field}})
			var thinking strings.Builder
			result, err := p.Generate(context.Background(), Request{Model: "m", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"task"}`)}, Observe: func(e StreamEvent) {
				if e.Type == "thinking" {
					thinking.WriteString(e.Text)
				}
			}})
			if err != nil {
				t.Fatal(err)
			}
			var message map[string]json.RawMessage
			json.Unmarshal(result.Message, &message)
			if string(message[field]) != `"first second"` || thinking.String() != "first second" {
				t.Fatalf("reasoning lost: %s, observed=%q", result.Message, thinking.String())
			}
		})
	}
}
