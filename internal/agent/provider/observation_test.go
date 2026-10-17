package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOpenRouterObservesTextDuringStreamAndPreservesDetails(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	inputMessage := json.RawMessage(`{"role":"assistant","content":null,"reasoning_details":[{"type":"reasoning.encrypted","data":"input-ciphertext","signature":"input-signature"}],"provider_extension":{"keep":true}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var requestBody struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Errorf("decode request body: %v", err)
			return
		}
		if len(requestBody.Messages) != 1 || string(requestBody.Messages[0]) != string(inputMessage) {
			t.Errorf("request history changed: %s", requestBody.Messages)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"choices":[{"index":0,"delta":{"reasoning":"think","reasoning_details":[{"type":"reasoning.text","id":"reason-1","text":"think","provider_extension":{"keep":true}},{"type":"reasoning.summary","id":"summary-1","summary":"In"},{"type":"reasoning.encrypted","id":"encrypted-1","data":"ciphertext-fragment","signature":"signature-fragment"}]},"finish_reason":null}]}`)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		writeSSE(w, `{"choices":[{"index":0,"delta":{"reasoning_details":[{"type":"reasoning.summary","id":"summary-1","summary":"sight"}]},"finish_reason":null}]}`)
		writeSSE(w, `{"choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`)
		writeSSE(w, "[DONE]")
	}))
	defer func() {
		releaseOnce.Do(func() { close(release) })
		server.Close()
	}()

	events := make(chan StreamEvent, 8)
	type result struct {
		response Response
		err      error
	}
	finished := make(chan result, 1)
	provider := NewOpenRouter(Options{KeyFile: writeTestKey(t), BaseURL: server.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		response, err := provider.Generate(ctx, Request{
			Model:    "test/model",
			Messages: []json.RawMessage{inputMessage},
			Observe: func(event StreamEvent) {
				events <- event
			},
		})
		finished <- result{response: response, err: err}
	}()

	first := receiveStreamEvent(t, events)
	if first != (StreamEvent{Type: "thinking", Text: "think"}) {
		t.Fatalf("first observation = %#v", first)
	}
	second := receiveStreamEvent(t, events)
	if second != (StreamEvent{Type: "thinking_summary", Text: "In"}) {
		t.Fatalf("second observation = %#v", second)
	}
	select {
	case result := <-finished:
		t.Fatalf("Generate finished before the gated stream continued: %v", result.err)
	default:
	}

	releaseOnce.Do(func() { close(release) })
	third := receiveStreamEvent(t, events)
	if third != (StreamEvent{Type: "thinking_summary", Text: "sight"}) {
		t.Fatalf("third observation = %#v", third)
	}
	fourth := receiveStreamEvent(t, events)
	if fourth != (StreamEvent{Type: "message_delta", Text: "answer"}) {
		t.Fatalf("fourth observation = %#v", fourth)
	}
	select {
	case extra := <-events:
		t.Fatalf("unexpected or duplicate observation: %#v", extra)
	default:
	}

	var got result
	select {
	case got = <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("Generate did not finish after the stream gate opened")
	}
	if got.err != nil {
		t.Fatalf("Generate() error = %v", got.err)
	}
	var message struct {
		Content         string `json:"content"`
		Reasoning       string `json:"reasoning"`
		ReasoningDetail []struct {
			Type              string          `json:"type"`
			Text              string          `json:"text"`
			Summary           string          `json:"summary"`
			Data              string          `json:"data"`
			Signature         string          `json:"signature"`
			ProviderExtension json.RawMessage `json:"provider_extension"`
		} `json:"reasoning_details"`
	}
	if err := json.Unmarshal(got.response.Message, &message); err != nil {
		t.Fatalf("decode response message: %v", err)
	}
	if message.Content != "answer" || message.Reasoning != "think" || len(message.ReasoningDetail) != 3 {
		t.Fatalf("response fields were lost: %s", got.response.Message)
	}
	byType := make(map[string]struct {
		Text              string
		Summary           string
		Data              string
		Signature         string
		ProviderExtension json.RawMessage
	})
	for _, detail := range message.ReasoningDetail {
		byType[detail.Type] = struct {
			Text              string
			Summary           string
			Data              string
			Signature         string
			ProviderExtension json.RawMessage
		}{detail.Text, detail.Summary, detail.Data, detail.Signature, detail.ProviderExtension}
	}
	if detail := byType["reasoning.text"]; detail.Text != "think" || string(detail.ProviderExtension) != `{"keep":true}` {
		t.Errorf("reasoning text detail was not preserved: %#v", detail)
	}
	if detail := byType["reasoning.summary"]; detail.Summary != "Insight" {
		t.Errorf("summary fragments were not assembled: %#v", detail)
	}
	if detail := byType["reasoning.encrypted"]; detail.Data != "ciphertext-fragment" || detail.Signature != "signature-fragment" {
		t.Errorf("encrypted response details were not preserved: %#v", detail)
	}
	var observedText strings.Builder
	for _, event := range []StreamEvent{first, second, third, fourth} {
		observedText.WriteString(event.Text)
	}
	if strings.Contains(observedText.String(), "ciphertext") || strings.Contains(observedText.String(), "signature-fragment") {
		t.Errorf("encrypted detail leaked to observation: %q", observedText.String())
	}
}

func receiveStreamEvent(t *testing.T, events <-chan StreamEvent) StreamEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for provider stream observation")
		return StreamEvent{}
	}
}
