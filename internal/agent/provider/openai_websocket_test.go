package provider

import (
	"context"
	"crypto/sha256"
	jsonv1 "encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type wsCommand struct {
	connection int
	input      []jsonv1.RawMessage
	previous   string
	fields     map[string]jsonv1.RawMessage
}

func readWSCommand(connection *websocket.Conn, ctx context.Context) (wsCommand, error) {
	_, raw, err := connection.Read(ctx)
	if err != nil {
		return wsCommand{}, err
	}
	var fields map[string]jsonv1.RawMessage
	if err := jsonv1.Unmarshal(raw, &fields); err != nil {
		return wsCommand{}, err
	}
	var command wsCommand
	command.fields = fields
	if err := jsonv1.Unmarshal(fields["input"], &command.input); err != nil {
		return wsCommand{}, err
	}
	_ = jsonv1.Unmarshal(fields["previous_response_id"], &command.previous)
	return command, nil
}

func writeWSCompleted(connection *websocket.Conn, ctx context.Context, id string, output ...jsonv1.RawMessage) error {
	for index, item := range output {
		event, err := jsonv1.Marshal(map[string]any{"type": "response.output_item.done", "output_index": index, "item": item})
		if err != nil {
			return err
		}
		if err := connection.Write(ctx, websocket.MessageText, event); err != nil {
			return err
		}
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"response.output_text.delta","delta":"ready"}`)); err != nil {
		return err
	}
	if err := connection.Write(ctx, websocket.MessageText, []byte(`{"type":"response.reasoning_summary_text.delta","delta":"checking"}`)); err != nil {
		return err
	}
	event, err := jsonv1.Marshal(map[string]any{
		"type":     "response.completed",
		"response": map[string]any{"id": id, "status": "completed", "output": []jsonv1.RawMessage{}, "usage": map[string]any{"input_tokens": 7, "output_tokens": 2}},
	})
	if err != nil {
		return err
	}
	return connection.Write(ctx, websocket.MessageText, event)
}

func websocketDefinition(serverURL, auth string) *Definition {
	return &Definition{
		SDK: "openai", API: "responses", Transport: "websocket", Auth: auth,
		BaseURL: serverURL, Path: "/custom/responses", Headers: map[string]string{"User-Agent": "noe/websocket-test"},
	}
}

func websocketProvider(t *testing.T, serverURL, auth string) *OpenAI {
	t.Helper()
	profile := websocketDefinition(serverURL, auth)
	if auth == "api-key" {
		profile.KeyFile = filepath.Join(t.TempDir(), "api-key")
		if err := os.WriteFile(profile.KeyFile, []byte("fixture-key"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return NewOpenAI(Options{Profile: profile})
}

func toolCallFixture() jsonv1.RawMessage {
	return jsonv1.RawMessage(`{"type":"function_call","id":"fc1","call_id":"call1","name":"run_js","arguments":"{}","status":"completed","future_field":{"keep":true}}`)
}

func messageOutputFixture(id, text string) jsonv1.RawMessage {
	raw, _ := jsonv1.Marshal(map[string]any{
		"type": "message", "id": id, "role": "assistant", "status": "completed",
		"content": []any{map[string]any{"type": "output_text", "text": text}}, "future_field": map[string]any{"retain": true},
	})
	return raw
}

func TestOpenAIManagedWebSocketContinuesWithNativeOutputAndDeltaInput(t *testing.T) {
	type observedCommand struct {
		command       wsCommand
		authorization string
		session       string
		userAgent     string
	}
	var mu sync.Mutex
	var commands []observedCommand
	connections := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer fixture-key" || r.Header.Get("x-test-session") != "managed-session" || r.Header.Get("User-Agent") != "noe/websocket-test" {
			t.Errorf("handshake headers = %v", r.Header)
		}
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer connection.CloseNow()
		mu.Lock()
		connections++
		connectionID := connections
		mu.Unlock()
		for {
			command, err := readWSCommand(connection, r.Context())
			if err != nil {
				return
			}
			command.connection = connectionID
			mu.Lock()
			commands = append(commands, observedCommand{command: command, authorization: r.Header.Get("Authorization"), session: r.Header.Get("x-test-session"), userAgent: r.Header.Get("User-Agent")})
			turn := len(commands)
			mu.Unlock()
			if turn == 1 {
				reasoning := jsonv1.RawMessage(`{"type":"reasoning","id":"rs1","encrypted_content":"opaque-secret","future_reasoning":{"keep":1}}`)
				if err := writeWSCompleted(connection, r.Context(), "resp_first", reasoning, toolCallFixture()); err != nil {
					return
				}
			} else {
				if err := writeWSCompleted(connection, r.Context(), "resp_second", messageOutputFixture("msg2", "done")); err != nil {
					return
				}
			}
		}
	}))
	defer server.Close()

	profile := websocketDefinition(server.URL, "api-key")
	profile.SessionHeader = "x-test-session"
	profile.KeyFile = filepath.Join(t.TempDir(), "api-key")
	if err := os.WriteFile(profile.KeyFile, []byte("fixture-key"), 0600); err != nil {
		t.Fatal(err)
	}
	p := NewOpenAI(Options{Profile: profile})
	base := context.Background()
	scope := NewRunScope(base)
	defer scope.Close()
	ctx := WithRunScope(base, scope)
	request := Request{
		SessionID: "managed-session", Model: "fixture-model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"run the tool"}`)},
		Tools: []jsonv1.RawMessage{jsonv1.RawMessage(`{"type":"function","function":{"name":"run_js","parameters":{"type":"object"}}}`)},
	}
	var observations []StreamEvent
	request.Observe = func(event StreamEvent) { observations = append(observations, event) }
	first, err := p.Generate(ctx, request)
	if err != nil || first.FinishReason != "tool_calls" {
		t.Fatalf("first response = %+v, err = %v", first, err)
	}
	if !strings.Contains(string(first.Message), "opaque-secret") || !strings.Contains(string(first.Message), "future_reasoning") || !strings.Contains(string(first.Message), "future_field") {
		t.Fatalf("native output was not preserved: %s", first.Message)
	}
	request.Messages = append(request.Messages, first.Message, jsonv1.RawMessage(`{"role":"tool","tool_call_id":"call1","content":"tool result"}`))
	second, err := p.Generate(ctx, request)
	if err != nil || second.FinishReason != "stop" || !strings.Contains(string(second.Message), "done") {
		t.Fatalf("second response = %+v, err = %v", second, err)
	}
	if len(observations) != 4 || observations[0].Type != "message_delta" || observations[1].Type != "thinking_summary" {
		t.Errorf("observations = %#v", observations)
	}

	mu.Lock()
	got := append([]observedCommand(nil), commands...)
	connectionCount := connections
	mu.Unlock()
	if connectionCount != 1 || len(got) != 2 || got[0].command.connection != got[1].command.connection {
		t.Fatalf("connections=%d commands=%+v", connectionCount, got)
	}
	var eventType string
	if jsonv1.Unmarshal(got[0].command.fields["type"], &eventType) != nil || eventType != "response.create" {
		t.Errorf("first event type = %s", got[0].command.fields["type"])
	}
	if _, ok := got[0].command.fields["stream"]; ok {
		t.Error("WebSocket command included stream")
	}
	if _, ok := got[0].command.fields["background"]; ok {
		t.Error("WebSocket command included background")
	}
	if got[0].command.previous != "" || got[1].command.previous != "resp_first" {
		t.Errorf("previous response IDs = %q, %q", got[0].command.previous, got[1].command.previous)
	}
	if len(got[0].command.input) != 1 || len(got[1].command.input) != 1 || !strings.Contains(string(got[1].command.input[0]), "function_call_output") {
		t.Errorf("input lengths/content = %d/%s then %d/%s", len(got[0].command.input), got[0].command.input, len(got[1].command.input), got[1].command.input)
	}
	if got[0].authorization != "Bearer fixture-key" || got[0].session != "managed-session" || got[0].userAgent != "noe/websocket-test" {
		t.Errorf("handshake = %+v", got[0])
	}
}

func TestOpenAIWebSocketCanonicalFingerprintsAndNumberPrecision(t *testing.T) {
	first := []byte(` { "b": 2, "a": {"n": 9007199254740993, "x": [true, null]} } `)
	second := []byte(`{"a":{"x":[true,null],"n":9007199254740993},"b":2}`)
	firstHash, err := canonicalJSONFingerprint(first)
	if err != nil {
		t.Fatal(err)
	}
	secondHash, err := canonicalJSONFingerprint(second)
	if err != nil || firstHash != secondHash {
		t.Fatalf("key order changed fingerprint: %x %x err=%v", firstHash, secondHash, err)
	}
	otherNumber, err := canonicalJSONFingerprint([]byte(`{"a":{"n":9007199254740992,"x":[true,null]},"b":2}`))
	if err != nil || otherNumber == firstHash {
		t.Fatalf("distinct adjacent integers collapsed: %x %x err=%v", firstHash, otherNumber, err)
	}
	configA, err := responsesConfigurationHash([]byte(`{"model":"m","input":[{"b":2,"a":1}],"tools":[{"z":3,"a":4}],"stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	configB, err := responsesConfigurationHash([]byte(`{"tools":[{"a":4,"z":3}],"stream":false,"input":[{"a":1,"b":2}],"model":"m"}`))
	if err != nil || configA != configB {
		t.Fatalf("configuration key ordering changed hash: %x %x err=%v", configA, configB, err)
	}
}

func TestOpenAIManagedWebSocketTrimAndConfigurationChangeSendFullInput(t *testing.T) {
	var mu sync.Mutex
	var commands []wsCommand
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer connection.CloseNow()
		for {
			command, err := readWSCommand(connection, r.Context())
			if err != nil {
				return
			}
			mu.Lock()
			commands = append(commands, command)
			turn := len(commands)
			mu.Unlock()
			if err := writeWSCompleted(connection, r.Context(), fmt.Sprintf("resp_%d", turn), messageOutputFixture(fmt.Sprintf("msg_%d", turn), "ok")); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	p := websocketProvider(t, server.URL, "none")
	base := context.Background()
	scope := NewRunScope(base)
	defer scope.Close()
	ctx := WithRunScope(base, scope)
	request := Request{Model: "model-a", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"original"}`)}}
	if _, err := p.Generate(ctx, request); err != nil {
		t.Fatal(err)
	}
	request.Messages = []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"trimmed"}`)}
	second, err := p.Generate(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.Messages = append(request.Messages, second.Message, jsonv1.RawMessage(`{"role":"user","content":"next"}`))
	request.Model = "model-b"
	if _, err := p.Generate(ctx, request); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	got := append([]wsCommand(nil), commands...)
	mu.Unlock()
	if len(got) != 3 {
		t.Fatalf("commands = %d", len(got))
	}
	if got[0].previous != "" || got[1].previous != "" || got[2].previous != "" {
		t.Errorf("trim/config change retained previous IDs: %q %q %q", got[0].previous, got[1].previous, got[2].previous)
	}
	if len(got[1].input) != 1 || len(got[2].input) != 3 {
		t.Errorf("full input lengths after trim/config change = %d, %d", len(got[1].input), len(got[2].input))
	}
}

func TestOpenAIWebSocketStandaloneRemainsSSE(t *testing.T) {
	var received bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		if r.URL.Path != "/custom/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var body map[string]jsonv1.RawMessage
		if err := jsonv1.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if string(body["stream"]) != "true" {
			t.Errorf("standalone stream = %s", body["stream"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"type":"response.completed","sequence_number":1,"response":`+nativeResponseFixture(`[ {"type":"message","id":"msg","role":"assistant","status":"completed","content":[{"type":"output_text","text":"SSE"}]} ]`)+`}`)
	}))
	defer server.Close()
	p := NewOpenAI(Options{Profile: websocketDefinition(server.URL, "none")})
	response, err := p.Generate(context.Background(), Request{Model: "model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"hello"}`)}})
	if err != nil || !strings.Contains(string(response.Message), "SSE") || !received {
		t.Fatalf("standalone response=%s received=%t err=%v", response.Message, received, err)
	}
}

func TestOpenAIManagedWebSocketCacheMissReplaysFullInputOnce(t *testing.T) {
	for _, test := range []struct {
		name string
		err  string
	}{
		{name: "documented code", err: `{"type":"error","status":400,"error":{"code":"previous_response_not_found","message":"stale"}}`},
		{name: "OAuth observed shape", err: `{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"Invalid previous_response_id"}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			var commands []wsCommand
			connections := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connection, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Errorf("accept websocket: %v", err)
					return
				}
				defer connection.CloseNow()
				mu.Lock()
				connections++
				connectionID := connections
				mu.Unlock()
				for {
					command, err := readWSCommand(connection, r.Context())
					if err != nil {
						return
					}
					command.connection = connectionID
					mu.Lock()
					commands = append(commands, command)
					turn := len(commands)
					mu.Unlock()
					if turn == 1 {
						if err := writeWSCompleted(connection, r.Context(), "resp_tool", toolCallFixture()); err != nil {
							return
						}
					} else if turn == 2 {
						if err := connection.Write(r.Context(), websocket.MessageText, []byte(test.err)); err != nil {
							return
						}
					} else {
						if err := writeWSCompleted(connection, r.Context(), "resp_recovered", messageOutputFixture("msg_recovered", "recovered")); err != nil {
							return
						}
					}
				}
			}))
			defer server.Close()
			p := websocketProvider(t, server.URL, "none")
			base := context.Background()
			scope := NewRunScope(base)
			defer scope.Close()
			ctx := WithRunScope(base, scope)
			request := Request{Model: "model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"compute"}`)}}
			first, err := p.Generate(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			request.Messages = append(request.Messages, first.Message, jsonv1.RawMessage(`{"role":"tool","tool_call_id":"call1","content":"already executed"}`))
			response, err := p.Generate(ctx, request)
			if err != nil || !strings.Contains(string(response.Message), "recovered") {
				t.Fatalf("recovery response=%s err=%v", response.Message, err)
			}
			mu.Lock()
			got := append([]wsCommand(nil), commands...)
			connectionCount := connections
			mu.Unlock()
			if connectionCount != 2 || len(got) != 3 {
				t.Fatalf("connections=%d commands=%+v", connectionCount, got)
			}
			if got[1].previous != "resp_tool" || got[2].previous != "" || len(got[2].input) != 3 || !strings.Contains(string(got[2].input[2]), "function_call_output") {
				t.Errorf("cache recovery did not replay complete current history: second=%+v replay=%+v", got[1], got[2])
			}
		})
	}
}

func TestOpenAIManagedWebSocketRetryReplaysFullHistoryWithoutRepeatingToolWork(t *testing.T) {
	var mu sync.Mutex
	var commands []wsCommand
	connections := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer connection.CloseNow()
		mu.Lock()
		connections++
		connectionID := connections
		mu.Unlock()
		for {
			command, err := readWSCommand(connection, r.Context())
			if err != nil {
				return
			}
			command.connection = connectionID
			mu.Lock()
			commands = append(commands, command)
			turn := len(commands)
			mu.Unlock()
			switch turn {
			case 1:
				if err := writeWSCompleted(connection, r.Context(), "resp_tool", toolCallFixture()); err != nil {
					return
				}
			case 2:
				connection.CloseNow()
				return
			default:
				if err := writeWSCompleted(connection, r.Context(), "resp_retry", messageOutputFixture("msg_retry", "retried")); err != nil {
					return
				}
			}
		}
	}))
	defer server.Close()
	p := websocketProvider(t, server.URL, "none")
	base, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	scope := NewRunScope(base)
	defer scope.Close()
	ctx := WithRunScope(base, scope)
	request := Request{Model: "model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"compute"}`)}}
	first, err := p.Generate(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	toolExecutions := 1 // The caller completes the tool before asking for another response.
	request.Messages = append(request.Messages, first.Message, jsonv1.RawMessage(`{"role":"tool","tool_call_id":"call1","content":"finished once"}`))
	response, err := p.Generate(ctx, request)
	if err != nil || !strings.Contains(string(response.Message), "retried") {
		t.Fatalf("retry response=%s err=%v", response.Message, err)
	}
	mu.Lock()
	got := append([]wsCommand(nil), commands...)
	connectionCount := connections
	mu.Unlock()
	if connectionCount != 2 || len(got) != 3 || got[1].previous != "resp_tool" || got[2].previous != "" {
		t.Fatalf("connections=%d commands=%+v", connectionCount, got)
	}
	if len(got[1].input) != 1 || len(got[2].input) != 3 || !strings.Contains(string(got[2].input[1]), "function_call") || !strings.Contains(string(got[2].input[2]), "function_call_output") {
		t.Errorf("retry did not use full current history: input2=%s input3=%s", got[1].input, got[2].input)
	}
	if strings.Count(string(got[2].input[2]), "function_call_output") != 1 || toolExecutions != 1 {
		t.Errorf("tool work repeated: executions=%d replay=%s", toolExecutions, got[2].input[2])
	}
}

func TestOpenAIManagedWebSocketCancellationAbortsActiveReceive(t *testing.T) {
	started := make(chan struct{})
	peerClosed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer connection.CloseNow()
		if _, err := readWSCommand(connection, r.Context()); err != nil {
			return
		}
		close(started)
		_, _, _ = connection.Read(r.Context())
		close(peerClosed)
	}))
	defer server.Close()
	p := websocketProvider(t, server.URL, "none")
	base := context.Background()
	scope := NewRunScope(base)
	ctx, cancel := context.WithCancel(WithRunScope(base, scope))
	requestDone := make(chan error, 1)
	go func() {
		_, err := p.Generate(ctx, Request{Model: "model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"wait"}`)}})
		requestDone <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not receive the response.create command")
	}
	cancel()
	select {
	case err := <-requestDone:
		var providerErr *ProviderError
		if !errors.As(err, &providerErr) || providerErr.Kind != ErrorCancelled || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled receive error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Generate did not stop after request cancellation")
	}
	select {
	case <-peerClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("request cancellation left the WebSocket open")
	}
	scope.Close()
}

func TestOpenAIManagedWebSocketScopeCloseAbortsIdleConnection(t *testing.T) {
	idleClosed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer connection.CloseNow()
		if _, err := readWSCommand(connection, r.Context()); err != nil {
			return
		}
		if err := writeWSCompleted(connection, r.Context(), "resp_idle", messageOutputFixture("msg_idle", "ok")); err != nil {
			return
		}
		_, _, _ = connection.Read(r.Context())
		close(idleClosed)
	}))
	defer server.Close()
	p := websocketProvider(t, server.URL, "none")
	base := context.Background()
	scope := NewRunScope(base)
	ctx := WithRunScope(base, scope)
	if _, err := p.Generate(ctx, requestForTest()); err != nil {
		t.Fatal(err)
	}
	scope.Close()
	select {
	case <-idleClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("scope close left the idle WebSocket open")
	}
}

func TestOpenAIManagedWebSocketScopeCloseCancelsDialAndRejectsLateUse(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	p := websocketProvider(t, server.URL, "none")
	base := context.Background()
	scope := NewRunScope(base)
	ctx := WithRunScope(base, scope)
	requestDone := make(chan error, 1)
	go func() {
		_, err := p.Generate(ctx, Request{Model: "model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"dial"}`)}})
		requestDone <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not receive the WebSocket handshake")
	}
	scope.Close()
	select {
	case err := <-requestDone:
		var providerErr *ProviderError
		if !errors.As(err, &providerErr) || providerErr.Kind != ErrorCancelled {
			t.Fatalf("scope-closed dial error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scope close did not cancel the active dial")
	}
	_, err := p.Generate(ctx, requestForTest())
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != ErrorCancelled {
		t.Fatalf("closed scope accepted a later request: %v", err)
	}
}

func requestForTest() Request {
	return Request{Model: "model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"later"}`)}}
}

func TestOpenAIManagedWebSocketIncompleteQuotaAndEOFClassification(t *testing.T) {
	for _, test := range []struct {
		name  string
		kind  ErrorKind
		event string
	}{
		{name: "incomplete", kind: ErrorResponse, event: `{"type":"response.incomplete","response":{"status":"incomplete","error":{"code":"max_output_tokens"}}}`},
		{name: "quota", kind: ErrorQuota, event: `{"type":"response.failed","response":{"status":"failed","error":{"code":"subscription_sharing_usage_limit_exceeded"}}}`},
		{name: "usage unavailable", kind: ErrorTransport, event: `{"type":"response.failed","response":{"status":"failed","error":{"code":"subscription_sharing_usage_unavailable"}}}`},
		{name: "user unavailable", kind: ErrorTransport, event: `{"type":"response.failed","response":{"status":"failed","error":{"code":"user_unavailable"}}}`},
		{name: "eof", kind: ErrorStream},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				connection, err := websocket.Accept(w, r, nil)
				if err != nil {
					t.Errorf("accept websocket: %v", err)
					return
				}
				defer connection.CloseNow()
				if _, err := readWSCommand(connection, r.Context()); err != nil {
					return
				}
				if test.event != "" {
					_ = connection.Write(r.Context(), websocket.MessageText, []byte(test.event))
				}
			}))
			defer server.Close()
			p := websocketProvider(t, server.URL, "none")
			base := context.Background()
			scope := NewRunScope(base)
			defer scope.Close()
			resource, err := scope.resource(p, func() managedResource { return newOpenAIWebSocket(p) })
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.generateManagedWebSocketOnce(WithRunScope(base, scope), requestForTest(), resource.(*openAIWebSocket))
			var providerErr *ProviderError
			if !errors.As(err, &providerErr) || providerErr.Kind != test.kind {
				t.Fatalf("error = %v, want %s", err, test.kind)
			}
			if test.kind == ErrorQuota && retryableGenerationError(err) || test.kind == ErrorTransport && !retryableGenerationError(err) {
				t.Fatalf("retryability did not match classified %s failure: %v", test.kind, err)
			}
		})
	}
}

func TestOpenAIWebSocketFingerprintUsesExactJSONNumber(t *testing.T) {
	got, err := canonicalJSONFingerprint([]byte(`{"n":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(`{"n":9007199254740993}`))
	if got != want {
		t.Fatalf("fingerprint = %x, want exact-number canonical bytes hash %x", got, want)
	}
}

type cancelMappingRoundTripper struct {
	request chan *http.Request
	cancel  chan *http.Request
	body    *websocketTestBody
}

func (r *cancelMappingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	r.request <- request
	return &http.Response{StatusCode: http.StatusSwitchingProtocols, Header: make(http.Header), Body: r.body, Request: request}, nil
}

func (r *cancelMappingRoundTripper) CancelRequest(request *http.Request) {
	r.cancel <- request
}

type websocketTestBody struct{}

func (websocketTestBody) Read([]byte) (int, error)       { return 0, io.EOF }
func (websocketTestBody) Write(data []byte) (int, error) { return len(data), nil }
func (websocketTestBody) Close() error                   { return nil }

func TestOpenAIWebSocketBridgePreservesUpgradeAndLegacyCancelRequest(t *testing.T) {
	body := websocketTestBody{}
	roundTripper := &cancelMappingRoundTripper{request: make(chan *http.Request, 1), cancel: make(chan *http.Request, 1), body: &body}
	profile := Definition{BaseURL: "https://fixture.invalid/v1", Path: "/wire/responses", SessionHeader: "x-session", Headers: map[string]string{"X-App": "fixture"}}
	borrowed := &http.Client{Transport: roundTripper}
	configured := &configuredHTTPClient{client: borrowed, profile: profile}
	bridge := configured.WebSocketHTTPClient()
	if bridge == borrowed || bridge.Transport == borrowed.Transport {
		t.Fatal("websocket client did not copy and wrap the borrowed transport")
	}
	if bridge.Timeout != borrowed.Timeout || bridge.Jar != borrowed.Jar || (bridge.CheckRedirect == nil) != (borrowed.CheckRedirect == nil) {
		t.Fatal("websocket client did not preserve the borrowed HTTP policy")
	}
	request := httptest.NewRequest(http.MethodGet, "https://fixture.invalid/v1/responses", nil)
	response, err := bridge.Transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	transportRequest := <-roundTripper.request
	if transportRequest != request {
		t.Fatalf("borrowed RoundTripper got %p, want original request %p", transportRequest, request)
	}
	if response.Body != &body {
		t.Fatal("HTTP 101 response body was wrapped")
	}
	bridge.Transport.(interface{ CancelRequest(*http.Request) }).CancelRequest(request)
	if cancelled := <-roundTripper.cancel; cancelled != request {
		t.Fatalf("CancelRequest got %p, want original request %p", cancelled, request)
	}
	state := &callState{sessionID: "session-1"}
	request = request.WithContext(context.WithValue(request.Context(), callContextKey{}, state))
	configuredRequest, err := configureOpenAIWebSocketRequest(configured, request)
	if err != nil {
		t.Fatal(err)
	}
	if configuredRequest.URL.Path != "/v1/wire/responses" || configuredRequest.Header.Get("x-session") != "session-1" || configuredRequest.Header.Get("X-App") != "fixture" {
		t.Fatalf("configured request = %s headers=%v", configuredRequest.URL, configuredRequest.Header)
	}
	originalURL, _ := url.Parse("https://fixture.invalid/v1/responses")
	if request.URL.String() != originalURL.String() {
		t.Fatalf("configuration mutated the SDK request: %s", request.URL)
	}
}

func TestOpenAIWebSocketBridgeHonorsSameOriginRedirect(t *testing.T) {
	var redirected bool
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/wire/responses" {
			if r.Header.Get("X-Configured") != "redirect-fixture" || r.Header.Get("x-session") != "redirect-session" || r.Header.Get("Cookie") != "route=present" {
				t.Errorf("initial configured request headers = %v", r.Header)
			}
			http.Redirect(w, r, "/accepted", http.StatusTemporaryRedirect)
			return
		}
		if r.URL.Path != "/accepted" {
			t.Errorf("redirect target path was rewritten: %q", r.URL.Path)
		}
		if r.Header.Get("X-Configured") != "" || r.Header.Get("x-session") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("CheckRedirect or Jar policy was bypassed: %v", r.Header)
		}
		redirected = true
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept redirected websocket: %v", err)
			return
		}
		defer connection.CloseNow()
		if _, err := readWSCommand(connection, r.Context()); err != nil {
			return
		}
		_ = writeWSCompleted(connection, r.Context(), "redirect-response", messageOutputFixture("redirect-message", "redirected"))
	}))
	defer server.Close()
	profile := websocketDefinition(server.URL, "none")
	profile.Path = "/wire/responses"
	profile.SessionHeader = "x-session"
	profile.Headers["X-Configured"] = "redirect-fixture"
	serverURL, _ := url.Parse(server.URL)
	jar.SetCookies(serverURL, []*http.Cookie{{Name: "route", Value: "present", Path: "/wire"}})
	httpClient := &http.Client{CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) > 2 {
			return fmt.Errorf("unexpected redirect loop")
		}
		viaRequest := via[len(via)-1]
		if len(via) == 1 && viaRequest.URL.Path != "/wire/responses" {
			return fmt.Errorf("redirect source path = %s", viaRequest.URL.Path)
		}
		if len(via) == 1 {
			// The borrowed policy deliberately strips these on its redirect.
			request.Header.Del("X-Configured")
			request.Header.Del("x-session")
		}
		return nil
	}, Jar: jar}
	p := NewOpenAI(Options{Profile: profile, HTTPClient: httpClient})
	base := context.Background()
	scope := NewRunScope(base)
	defer scope.Close()
	ctx := WithRunScope(base, scope)
	response, err := p.Generate(ctx, Request{SessionID: "redirect-session", Model: "model", Messages: []jsonv1.RawMessage{jsonv1.RawMessage(`{"role":"user","content":"redirect"}`)}})
	if err != nil || !redirected || !strings.Contains(string(response.Message), "redirected") {
		t.Fatalf("redirect response=%s redirected=%t err=%v", response.Message, redirected, err)
	}
}

func TestOpenAIManagedWebSocketClassifiesHandshakeHTTPStatus(t *testing.T) {
	for _, test := range []struct {
		status     int
		kind       ErrorKind
		retryAfter string
		code       string
		wantDelay  time.Duration
	}{
		{status: http.StatusUnauthorized, kind: ErrorAuthentication},
		{status: http.StatusTooManyRequests, kind: ErrorRateLimit},
		{status: http.StatusServiceUnavailable, kind: ErrorTransport, retryAfter: "2500", wantDelay: 2500 * time.Millisecond},
		{status: http.StatusTooManyRequests, kind: ErrorQuota, code: "insufficient_quota"},
	} {
		t.Run(fmt.Sprintf("%d/%s", test.status, test.code), func(t *testing.T) {
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/custom/responses" {
					t.Errorf("handshake path = %q", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				if test.retryAfter != "" {
					w.Header().Set("Retry-After-Ms", test.retryAfter)
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, `{"error":{"message":"fixture rejection","code":"`+test.code+`"}}`)
			}))
			defer server.Close()
			p := websocketProvider(t, server.URL, "none")
			base := context.Background()
			scope := NewRunScope(base)
			defer scope.Close()
			resource, err := scope.resource(p, func() managedResource { return newOpenAIWebSocket(p) })
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.generateManagedWebSocketOnce(WithRunScope(base, scope), requestForTest(), resource.(*openAIWebSocket))
			var providerErr *ProviderError
			if !errors.As(err, &providerErr) || providerErr.Kind != test.kind || providerErr.StatusCode != test.status {
				t.Fatalf("handshake error = %v, want kind=%s status=%d", err, test.kind, test.status)
			}
			if test.wantDelay != 0 && (!providerErr.hasRetryAfter || providerErr.retryAfter != test.wantDelay) {
				t.Fatalf("handshake retry hint = %v/%t, want %v", providerErr.retryAfter, providerErr.hasRetryAfter, test.wantDelay)
			}
			if test.kind == ErrorQuota && retryableGenerationError(err) {
				t.Fatalf("quota handshake was retryable: %v", err)
			}
			if calls != 1 {
				t.Fatalf("handshake attempts = %d, want 1", calls)
			}
		})
	}
}

func TestOpenAIWebSocketErrorCodeDoesNotOverrideAuthenticationStatus(t *testing.T) {
	for _, test := range []struct {
		status int
		code   string
	}{
		{status: http.StatusUnauthorized, code: "subscription_sharing_usage_unavailable"},
		{status: http.StatusForbidden, code: "user_unavailable"},
	} {
		raw := []byte(fmt.Sprintf(`{"type":"error","status":%d,"error":{"code":%q}}`, test.status, test.code))
		err := openAIWebSocketAPIError(raw)
		var failure *ProviderError
		if !errors.As(err, &failure) || failure.Kind != ErrorAuthentication || retryableGenerationError(err) {
			t.Fatalf("status=%d code=%s classified as %v; want terminal authentication", test.status, test.code, err)
		}
	}
}
