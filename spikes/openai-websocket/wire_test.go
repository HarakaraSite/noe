package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	openai "github.com/openai/openai-go/v3"
)

func clientForFixture(t *testing.T, handler http.Handler) (*httptest.Server, openai.Client) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, newClient(server.URL+"/v1", "fixture-test", server.Client())
}

func writeSSEFixture(w http.ResponseWriter, events []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is unavailable", http.StatusInternalServerError)
		return
	}
	for _, event := range events {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
		flusher.Flush()
	}
}

func acceptFixtureSocket(w http.ResponseWriter, r *http.Request) (*coderws.Conn, error) {
	return coderws.Accept(w, r, &coderws.AcceptOptions{InsecureSkipVerify: true})
}

func readSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestSSEAndWebSocketShareRawAssemblyAndOutputItemFallback(t *testing.T) {
	items := []string{
		`{"id":"rs_fixture","type":"reasoning","summary":[],"encrypted_content":"opaque-reasoning-ciphertext","provider_extension":{"revision":7,"flags":["keep","raw"]}}`,
		`{"id":"msg_fixture","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"fixture answer","annotations":[]}],"provider_extension":{"trace":"fixture-message"}}`,
	}
	events := []string{
		`{"type":"response.output_text.delta","item_id":"msg_fixture","output_index":1,"content_index":0,"delta":"fixture answer"}`,
		`{"type":"response.output_item.done","output_index":0,"item":` + items[0] + `}`,
		`{"type":"response.output_item.done","output_index":1,"item":` + items[1] + `}`,
		`{"type":"response.completed","response":{"id":"resp_fixture","model":"fixture-model","status":"completed","service_tier":"default","usage":{"input_tokens":5,"output_tokens":2},"output":[]}}`,
	}
	serverErrors := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			serverErrors <- fmt.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost {
			writeSSEFixture(w, events)
			return
		}
		if r.Method != http.MethodGet {
			serverErrors <- fmt.Errorf("unexpected request method %q", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		connection, err := acceptFixtureSocket(w, r)
		if err != nil {
			serverErrors <- err
			return
		}
		defer connection.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		_, command, err := connection.Read(ctx)
		if err != nil {
			serverErrors <- fmt.Errorf("read response.create: %w", err)
			return
		}
		var request struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(command, &request); err != nil || request.Type != "response.create" {
			serverErrors <- fmt.Errorf("unexpected websocket command %s: %v", command, err)
			return
		}
		for _, event := range events {
			if err := connection.Write(ctx, coderws.MessageText, []byte(event)); err != nil {
				serverErrors <- fmt.Errorf("write websocket event: %w", err)
				return
			}
		}
	})
	_, client := clientForFixture(t, handler)

	request := body{"model": "fixture-model", "input": []item{}}
	sseResult, err := sseTurn(context.Background(), client, request)
	if err != nil {
		t.Fatalf("sseTurn() error = %v", err)
	}
	connection, _, err := openSocket(context.Background(), client)
	if err != nil {
		t.Fatalf("openSocket() error = %v", err)
	}
	defer connection.Abort()
	websocketResult, err := websocketTurn(context.Background(), connection, request)
	if err != nil {
		t.Fatalf("websocketTurn() error = %v", err)
	}

	select {
	case serverErr := <-serverErrors:
		t.Fatalf("fixture server: %v", serverErr)
	default:
	}
	if sseResult.ResponseID != "resp_fixture" || websocketResult.ResponseID != sseResult.ResponseID {
		t.Fatalf("response IDs differ: SSE=%q WebSocket=%q", sseResult.ResponseID, websocketResult.ResponseID)
	}
	if sseResult.Model != "fixture-model" || websocketResult.Model != sseResult.Model {
		t.Errorf("models differ: SSE=%q WebSocket=%q", sseResult.Model, websocketResult.Model)
	}
	if sseResult.ServiceTier != "default" || websocketResult.ServiceTier != sseResult.ServiceTier ||
		string(sseResult.Usage) != `{"input_tokens":5,"output_tokens":2}` || string(websocketResult.Usage) != string(sseResult.Usage) {
		t.Errorf("metadata differs: SSE=(tier %q, usage %s) WebSocket=(tier %q, usage %s)",
			sseResult.ServiceTier, sseResult.Usage, websocketResult.ServiceTier, websocketResult.Usage)
	}
	if sseResult.Text != "fixture answer" || websocketResult.Text != sseResult.Text {
		t.Errorf("assembled text differs: SSE=%q WebSocket=%q", sseResult.Text, websocketResult.Text)
	}
	if sseResult.OutputItems != 2 || websocketResult.OutputItems != sseResult.OutputItems ||
		sseResult.ReasoningItems != 1 || websocketResult.ReasoningItems != sseResult.ReasoningItems {
		t.Errorf("assembled counts differ: SSE=(%d output, %d reasoning), WebSocket=(%d output, %d reasoning)",
			sseResult.OutputItems, sseResult.ReasoningItems, websocketResult.OutputItems, websocketResult.ReasoningItems)
	}
	if len(sseResult.Output) != len(items) || len(websocketResult.Output) != len(sseResult.Output) {
		t.Fatalf("fallback output lengths: SSE=%d WebSocket=%d", len(sseResult.Output), len(websocketResult.Output))
	}
	for i, expected := range items {
		if string(sseResult.Output[i]) != expected || string(websocketResult.Output[i]) != expected {
			t.Errorf("output item %d was not preserved: SSE=%s WebSocket=%s", i, sseResult.Output[i], websocketResult.Output[i])
		}
	}
	var reasoning map[string]json.RawMessage
	if err := json.Unmarshal(sseResult.Output[0], &reasoning); err != nil {
		t.Fatalf("decode fallback reasoning item: %v", err)
	}
	if string(reasoning["encrypted_content"]) != `"opaque-reasoning-ciphertext"` ||
		string(reasoning["provider_extension"]) != `{"revision":7,"flags":["keep","raw"]}` {
		t.Errorf("fallback discarded opaque fields: %s", sseResult.Output[0])
	}
	if sseResult.EventCount != len(events) || websocketResult.EventCount != sseResult.EventCount {
		t.Errorf("event counts differ: SSE=%d WebSocket=%d", sseResult.EventCount, websocketResult.EventCount)
	}
	if sseResult.FirstEventMS == nil || websocketResult.FirstEventMS == nil ||
		sseResult.FirstTextMS == nil || websocketResult.FirstTextMS == nil {
		t.Errorf("transport assembly did not record event/text timing: SSE=%+v WebSocket=%+v", sseResult, websocketResult)
	}
}

func TestToolOutputSupportsFunctionAndCustomCalls(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		call       string
		outputType string
		callID     string
	}{
		{name: "function", kind: "function", call: `{"id":"fc_fixture","type":"function_call","name":"probe_value","call_id":"call_function","arguments":"{}"}`, outputType: "function_call_output", callID: "call_function"},
		{name: "custom", kind: "custom", call: `{"id":"ct_fixture","type":"custom_tool_call","name":"run_js","call_id":"call_custom","input":"console.log(probe_value())"}`, outputType: "custom_tool_call_output", callID: "call_custom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := toolOutput(turn{Output: []item{json.RawMessage(tt.call)}}, tt.kind, "fixture-nonce")
			if err != nil {
				t.Fatalf("toolOutput() error = %v", err)
			}
			var decoded struct {
				Type   string `json:"type"`
				CallID string `json:"call_id"`
				Output string `json:"output"`
			}
			if err := json.Unmarshal(output, &decoded); err != nil {
				t.Fatalf("decode tool output %s: %v", output, err)
			}
			if decoded.Type != tt.outputType || decoded.CallID != tt.callID || decoded.Output != "fixture-nonce" {
				t.Errorf("tool output = %+v, want type=%q call_id=%q output=fixture-nonce", decoded, tt.outputType, tt.callID)
			}
		})
	}
}

func TestCollectDoesNotTreatFailureIncompleteOrEOFAsCompletion(t *testing.T) {
	tests := []struct {
		name  string
		event string
		next  func() (item, error)
	}{
		{
			name:  "failed",
			event: `{"type":"response.failed","response":{"id":"resp_failed","error":{"code":"fixture_failure","message":"fixture failed"}}}`,
		},
		{
			name:  "incomplete",
			event: `{"type":"response.incomplete","response":{"id":"resp_incomplete","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`,
		},
		{
			name: "eof",
			next: func() (item, error) { return nil, io.EOF },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := tt.next
			if next == nil {
				sent := false
				next = func() (item, error) {
					if sent {
						return nil, io.EOF
					}
					sent = true
					return json.RawMessage(tt.event), nil
				}
			}
			result, err := collect(time.Now(), turn{}, next)
			if err == nil {
				t.Fatal("collect() error = nil, want non-completion error")
			}
			if result.ResponseID != "" {
				t.Errorf("non-completed response got ID %q", result.ResponseID)
			}
			if tt.name == "eof" && !errors.Is(err, io.EOF) {
				t.Errorf("collect() error = %v, want io.EOF", err)
			}
		})
	}
}

func TestSSEAndWebSocketRequireResponseCompleted(t *testing.T) {
	tests := []struct {
		name   string
		events []string
	}{
		{name: "failed", events: []string{`{"type":"response.failed","response":{"id":"resp_failed","error":{"code":"fixture_failure","message":"fixture failed"}}}`}},
		{name: "incomplete", events: []string{`{"type":"response.incomplete","response":{"id":"resp_incomplete","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`}},
		{name: "eof"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverErrors := make(chan error, 2)
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					writeSSEFixture(w, tt.events)
					return
				}
				connection, err := acceptFixtureSocket(w, r)
				if err != nil {
					serverErrors <- err
					return
				}
				defer connection.CloseNow()
				ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
				defer cancel()
				if _, _, err := connection.Read(ctx); err != nil {
					serverErrors <- fmt.Errorf("read response.create: %w", err)
					return
				}
				for _, event := range tt.events {
					if err := connection.Write(ctx, coderws.MessageText, []byte(event)); err != nil {
						serverErrors <- fmt.Errorf("write websocket event: %w", err)
						return
					}
				}
			})
			_, client := clientForFixture(t, handler)
			request := body{"model": "fixture-model", "input": []item{}}

			sseResult, sseErr := sseTurn(context.Background(), client, request)
			if sseErr == nil || sseResult.ResponseID != "" {
				t.Errorf("sseTurn() = (ID %q, error %v), want an error before completion", sseResult.ResponseID, sseErr)
			}
			connection, _, err := openSocket(context.Background(), client)
			if err != nil {
				t.Fatalf("openSocket() error = %v", err)
			}
			websocketResult, websocketErr := websocketTurn(context.Background(), connection, request)
			connection.Abort()
			if websocketErr == nil || websocketResult.ResponseID != "" {
				t.Errorf("websocketTurn() = (ID %q, error %v), want an error before completion", websocketResult.ResponseID, websocketErr)
			}
			select {
			case serverErr := <-serverErrors:
				t.Errorf("fixture server: %v", serverErr)
			default:
			}
		})
	}
}

func TestResponseConnectionRecvRawCancellationLeavesSocketUsable(t *testing.T) {
	connected := make(chan struct{}, 1)
	releaseEvent := make(chan struct{})
	fixtureEvent := `{"type":"response.output_text.delta","item_id":"msg_later","output_index":0,"content_index":0,"delta":"arrived after cancelled wait"}`
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := acceptFixtureSocket(w, r)
		if err != nil {
			return
		}
		defer connection.CloseNow()
		connected <- struct{}{}
		select {
		case <-releaseEvent:
		case <-r.Context().Done():
			return
		}
		writeCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_ = connection.Write(writeCtx, coderws.MessageText, []byte(fixtureEvent))
		_, _, _ = connection.Read(writeCtx)
	})
	_, client := clientForFixture(t, handler)
	connection, _, err := openSocket(context.Background(), client)
	if err != nil {
		t.Fatalf("openSocket() error = %v", err)
	}
	defer connection.Abort()
	readSignal(t, connected, "websocket connection")

	cancelled, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := connection.RecvRaw(cancelled); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RecvRaw(cancelled) error = %v, want context deadline exceeded", err)
	}
	close(releaseEvent)

	receive, cancelReceive := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelReceive()
	got, err := connection.RecvRaw(receive)
	if err != nil {
		t.Fatalf("RecvRaw() after cancelled wait: %v", err)
	}
	if string(got) != fixtureEvent {
		t.Errorf("later event = %s, want %s", got, fixtureEvent)
	}
}

func TestWebsocketTurnCancellationAbortsSocket(t *testing.T) {
	connected := make(chan struct{}, 1)
	command := make(chan []byte, 1)
	peerReadResult := make(chan error, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := acceptFixtureSocket(w, r)
		if err != nil {
			return
		}
		defer connection.CloseNow()
		connected <- struct{}{}
		serverCtx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		_, data, err := connection.Read(serverCtx)
		if err != nil {
			peerReadResult <- fmt.Errorf("read create command: %w", err)
			return
		}
		command <- data
		_, _, err = connection.Read(serverCtx)
		peerReadResult <- err
	})
	_, client := clientForFixture(t, handler)
	connection, _, err := openSocket(context.Background(), client)
	if err != nil {
		t.Fatalf("openSocket() error = %v", err)
	}
	defer connection.Abort()
	readSignal(t, connected, "websocket connection")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err = websocketTurn(ctx, connection, body{"model": "fixture-model", "input": []item{}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("websocketTurn() error = %v, want context deadline exceeded", err)
	}
	select {
	case raw := <-command:
		var request struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &request); err != nil || request.Type != "response.create" {
			t.Errorf("server received command %s: decode error %v", raw, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive the create command")
	}
	select {
	case err := <-peerReadResult:
		if err == nil || errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("server read after create ended with %v; want client socket closure", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("websocketTurn cancellation did not close the server-side socket")
	}
}
