package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestStreamObservationsReasoningField(t *testing.T) {
	for _, test := range []struct {
		name, field, delta, thought string
	}{
		{"default", "", `"reasoning":"first","reasoning_content":"second"`, "first"},
		{"default-fallback", "", `"reasoning":"","reasoning_content":"second"`, "second"},
		{"reasoning", "reasoning", `"reasoning":"first","reasoning_content":"second"`, "first"},
		{"content", "reasoning_content", `"reasoning":"first","reasoning_content":"second"`, "second"},
		{"unconfigured", "", `"reasoning":"first","reasoning_content":"second"`, "first"},
		{"unconfigured-fallback", "", `"reasoning_content":"second"`, "second"},
		{"unconfigured-empty", "", `"reasoning":"","reasoning_content":"second"`, "second"},
		{"missing", "reasoning_content", `"reasoning":"unselected"`, ""},
		{"null", "reasoning_content", `"reasoning":"unselected","reasoning_content":null`, ""},
		{"empty", "reasoning", `"reasoning":"","reasoning_content":"unselected"`, ""},
		{"invalid", "reasoning", `"reasoning":42,"reasoning_content":"unselected"`, ""},
		{"custom", "thought", `"reasoning":"first","reasoning_content":"second","thought":"selected"`, "selected"},
		{"custom-missing", "thought", `"reasoning":"first","reasoning_content":"second"`, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := json.RawMessage(fmt.Sprintf(`{"choices":[{"index":1,"delta":{"content":"other choice"}},{"index":0,"delta":{%s,"content":"answer"}}]}`, test.delta))
			want := []StreamEvent{}
			if test.thought != "" {
				want = append(want, StreamEvent{Type: "thinking", Text: test.thought})
			}
			want = append(want, StreamEvent{Type: "message_delta", Text: "answer"})
			if got := streamObservationsForField(raw, test.field); !slices.Equal(got, want) {
				t.Fatalf("observations = %#v, want %#v", got, want)
			}
		})
	}
}

func TestChatReasoningFieldObservationAndHistory(t *testing.T) {
	for _, sdk := range []string{"openai", "openrouter"} {
		for _, field := range []string{"reasoning", "reasoning_content", "thought"} {
			t.Run(sdk+"/"+field, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/event-stream")
					for i, text := range []string{"selected ", "thought"} {
						delta := map[string]any{
							"reasoning": "unselected reasoning", "reasoning_content": "unselected reasoning_content",
							"thought": "unselected thought", "role": "assistant",
							"reasoning_details": []any{
								map[string]any{"type": "reasoning.text", "id": "text", "text": text},
								map[string]any{"type": "reasoning.encrypted", "id": "opaque", "data": "ciphertext", "signature": "signature"},
							},
						}
						delta[field] = text
						if i == 1 {
							delta["content"] = "answer"
						}
						encoded, err := json.Marshal(delta)
						if err != nil {
							t.Error(err)
							return
						}
						writeSSE(w, fmt.Sprintf(`{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"local","choices":[{"index":0,"delta":%s,"finish_reason":null}]}`, encoded))
					}
					writeSSE(w, `{"id":"fixture","object":"chat.completion.chunk","created":1,"model":"local","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
					writeSSE(w, "[DONE]")
				}))
				defer server.Close()
				options := Options{BaseURL: server.URL, KeyFile: writeTestKey(t), Profile: &Definition{
					SDK: sdk, API: "chat-completions", Auth: "none", BaseURL: server.URL,
					Path: "/chat/completions", ReasoningField: field,
				}}
				var adapter Provider = NewOpenAI(options)
				if sdk == "openrouter" {
					adapter = NewOpenRouter(options)
				}
				var observed []StreamEvent
				response, err := adapter.Generate(context.Background(), Request{
					Model: "local", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hello"}`)},
					Observe: func(event StreamEvent) { observed = append(observed, event) },
				})
				if err != nil {
					t.Fatal(err)
				}
				want := []StreamEvent{{Type: "thinking", Text: "selected "}, {Type: "thinking", Text: "thought"}, {Type: "message_delta", Text: "answer"}}
				if !slices.Equal(observed, want) {
					t.Fatalf("observations = %#v, want %#v", observed, want)
				}
				var message map[string]json.RawMessage
				if err := json.Unmarshal(response.Message, &message); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"reasoning", "reasoning_content", "thought"} {
					want := "unselected " + name
					if name == field {
						want = "selected thought"
					}
					var got string
					if err := json.Unmarshal(message[name], &got); err != nil || got != want {
						t.Fatalf("history %s = %s, want %q: %v", name, message[name], want, err)
					}
				}
				if !json.Valid(message["reasoning_details"]) || response.FinishReason != "stop" {
					t.Fatalf("response metadata lost: %s", response.Message)
				}
			})
		}
	}
}
