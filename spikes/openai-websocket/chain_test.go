package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type chainRequest struct {
	Connection    int
	Input         []item `json:"input"`
	Previous      string `json:"previous_response_id"`
	Authorization string
}

type chainFixture struct {
	mu          sync.Mutex
	connections int
	requests    []chainRequest
	sockets     []*websocket.Conn
	miss        bool
	generic     bool
	drop        bool
	server      *httptest.Server
}

func newChainFixture(t *testing.T) *chainFixture {
	t.Helper()
	f := &chainFixture{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer connection.CloseNow()
		f.mu.Lock()
		f.connections++
		id := f.connections
		f.sockets = append(f.sockets, connection)
		f.mu.Unlock()
		for {
			data, err := readChainMessage(connection, r.Context())
			if err != nil {
				return
			}
			var request chainRequest
			if json.Unmarshal(data, &request) != nil {
				return
			}
			request.Connection, request.Authorization = id, r.Header.Get("Authorization")
			f.mu.Lock()
			f.requests = append(f.requests, request)
			count := len(f.requests)
			miss, generic, drop := f.miss, f.generic, f.drop
			f.miss, f.generic, f.drop = false, false, false
			f.mu.Unlock()
			if drop {
				return
			}
			if miss {
				data = []byte(`{"type":"error","status":400,"error":{"code":"previous_response_not_found","message":"fixture lost connection cache"}}`)
				if generic {
					data = []byte(`{"type":"error","status":400,"error":{"type":"invalid_request_error","message":"Invalid previous_response_id"}}`)
				}
			} else {
				response := body{"id": fmt.Sprintf("resp_%d_%d", id, count), "output": []body{
					{"type": "reasoning", "id": fmt.Sprintf("reasoning_%d", count), "encrypted_content": "fixture-opaque", "future_field": body{"retained": true}},
					{"type": "message", "id": fmt.Sprintf("message_%d", count), "role": "assistant", "content": []body{{"type": "output_text", "text": "ok"}}},
				}}
				data, _ = json.Marshal(body{"type": "response.completed", "response": response})
			}
			if connection.Write(r.Context(), websocket.MessageText, data) != nil {
				return
			}
		}
	}))
	t.Cleanup(func() {
		f.mu.Lock()
		sockets := append([]*websocket.Conn(nil), f.sockets...)
		f.mu.Unlock()
		for _, socket := range sockets {
			socket.CloseNow()
		}
		f.server.Close()
	})
	return f
}

func readChainMessage(connection *websocket.Conn, ctx context.Context) ([]byte, error) {
	_, data, err := connection.Read(ctx)
	return data, err
}

func (f *chainFixture) snapshot() []chainRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chainRequest(nil), f.requests...)
}

func testChainRequest(input []item) body {
	return requestBody("fixture", "low", "priority", "fixture-key", "custom", input, forcedTool("custom"))
}

func TestChainAppendAndReplacement(t *testing.T) {
	f := newChainFixture(t)
	c := &chain{client: newClient(f.server.URL, "fixture-session", f.server.Client())}
	defer c.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	history := []item{inputMessage("user", "initial")}
	first, err := c.Exchange(ctx, testChainRequest(history))
	if err != nil {
		t.Fatal(err)
	}
	history = append(history, first.Output...)
	history = append(history, inputMessage("user", "continue"))
	request := testChainRequest(history)
	request["tool_choice"] = "none"
	second, err := c.Exchange(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Incremental || second.ChainDecision != "append" {
		t.Fatalf("incremental turn: %+v", second)
	}
	requests := f.snapshot()
	if len(requests) != 2 || len(requests[1].Input) != 1 || requests[1].Previous != first.ResponseID {
		t.Fatalf("requests: %+v", requests)
	}
	if !strings.Contains(string(c.history[1]), `"encrypted_content":"fixture-opaque"`) || !strings.Contains(string(c.history[1]), `"future_field"`) {
		t.Fatal("opaque native output was lost")
	}
	trimmed := []item{inputMessage("user", "retained summary")}
	third, err := c.Exchange(ctx, testChainRequest(trimmed))
	if err != nil {
		t.Fatal(err)
	}
	if third.Incremental || third.ChainDecision != "history-replaced" {
		t.Fatalf("trim: %+v", third)
	}
	trimmed = append(trimmed, third.Output...)
	trimmed = append(trimmed, inputMessage("user", "continue again"))
	changed := testChainRequest(trimmed)
	changed["model"] = "different-model"
	fourth, err := c.Exchange(ctx, changed)
	if err != nil {
		t.Fatal(err)
	}
	if fourth.Incremental || fourth.ChainDecision != "configuration-changed" {
		t.Fatalf("configuration: %+v", fourth)
	}
	requests = f.snapshot()
	if requests[2].Previous != "" || requests[3].Previous != "" || len(requests[3].Input) != len(trimmed) {
		t.Fatalf("replacement requests: %+v", requests)
	}
}

func TestChainCacheMissAndUncertainDelivery(t *testing.T) {
	f := newChainFixture(t)
	c := &chain{client: newClient(f.server.URL, "fixture-session", f.server.Client())}
	defer c.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	history := []item{inputMessage("user", "initial")}
	first, err := c.Exchange(ctx, testChainRequest(history))
	if err != nil {
		t.Fatal(err)
	}
	history = append(history, first.Output...)
	history = append(history, inputMessage("user", "continue"))
	f.mu.Lock()
	f.miss = true
	f.mu.Unlock()
	second, err := c.Exchange(ctx, testChainRequest(history))
	if err != nil {
		t.Fatal(err)
	}
	requests := f.snapshot()
	if second.Incremental || second.ChainDecision != "cache-miss-full-replay" || len(c.LastAttempts) != 2 || c.LastAttempts[0].FailureCode != "previous_response_not_found" {
		t.Fatalf("cache recovery: %+v, attempts=%+v", second, c.LastAttempts)
	}
	if len(requests) != 3 || requests[1].Previous == "" || requests[2].Previous != "" || len(requests[2].Input) != len(history) || requests[2].Connection == requests[1].Connection {
		t.Fatalf("cache miss requests: %+v", requests)
	}
	history = append(history, second.Output...)
	history = append(history, inputMessage("user", "uncertain delivery"))
	f.mu.Lock()
	f.drop = true
	f.mu.Unlock()
	if _, err := c.Exchange(ctx, testChainRequest(history)); err == nil {
		t.Fatal("unexpected success after dropped reply")
	}
	if len(f.snapshot()) != 4 {
		t.Fatal("uncertain delivery was replayed automatically")
	}
	recovered, err := c.Exchange(ctx, testChainRequest(history))
	if err != nil {
		t.Fatal(err)
	}
	requests = f.snapshot()
	if requests[4].Previous != "" || len(requests[4].Input) != len(history) {
		t.Fatalf("explicit recovery: %+v", requests[4])
	}
	history = append(history, recovered.Output...)
	history = append(history, inputMessage("user", "generic stale-ID rejection"))
	f.mu.Lock()
	f.miss, f.generic = true, true
	f.mu.Unlock()
	if _, err := c.Exchange(ctx, testChainRequest(history)); err == nil {
		t.Fatal("generic error was ignored")
	}
	if len(f.snapshot()) != 6 || len(c.LastAttempts) != 1 || len(c.LastAttempts[0].ErrorEvent) == 0 {
		t.Fatal("generic API error was retried or lost")
	}
}

func TestChainReconnectRefreshesAuthAndConversationIsolation(t *testing.T) {
	f := newChainFixture(t)
	var authorizations atomic.Int32
	transport := &authentication{base: f.server.Client().Transport, token: func(context.Context) (string, error) { return fmt.Sprintf("fixture-%d", authorizations.Add(1)), nil }}
	client := newClient(f.server.URL, "fixture-session", &http.Client{Transport: transport})
	one, two := &chain{client: client}, &chain{client: client}
	defer one.Close()
	defer two.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	oneHistory := []item{inputMessage("user", "conversation one")}
	first, err := one.Exchange(ctx, testChainRequest(oneHistory))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := two.Exchange(ctx, testChainRequest([]item{inputMessage("user", "conversation two")})); err != nil {
		t.Fatal(err)
	}
	oneHistory = append(oneHistory, first.Output...)
	oneHistory = append(oneHistory, inputMessage("user", "continue one"))
	if _, err := one.Exchange(ctx, testChainRequest(oneHistory)); err != nil {
		t.Fatal(err)
	}
	requests := f.snapshot()
	if requests[0].Connection == requests[1].Connection || requests[0].Connection != requests[2].Connection || requests[2].Previous != first.ResponseID {
		t.Fatalf("conversation isolation: %+v", requests)
	}
	if err := one.Reconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Exchange(ctx, testChainRequest(oneHistory)); err != nil {
		t.Fatal(err)
	}
	requests = f.snapshot()
	last := requests[len(requests)-1]
	if last.Previous != "" || last.Authorization != "Bearer fixture-3" || len(last.Input) != len(oneHistory) || authorizations.Load() != 3 {
		t.Fatalf("auth/reconnect: %+v", last)
	}
	if err := one.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := one.Exchange(ctx, testChainRequest(oneHistory)); err == nil {
		t.Fatal("closed conversation reopened")
	}
}
