package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"noe/internal/decision"
)

func decisionRequest(model string) decision.Request {
	return decision.Request{Provider: "openrouter", Model: model, State: json.RawMessage(`{"number":9007199254740993,"exponent":1e3}`), Questions: map[string]json.RawMessage{
		"q": json.RawMessage(`{"type":"choice","instructions":"Choose","criteria":{"yes":"Y","no":"N"},"extension":{"keep":true}}`),
	}}
}

func TestDecideSDKPreservesJSONAndSharesAuthentication(t *testing.T) {
	const raw = ` {"model":"decision/revision","answers":{"q":{"type":"choice","choice":"yes","probabilities":{"yes":1,"no":0},"future":9007199254740993}},"usage":{"input_tokens":5,"output_tokens":0,"cost":1e-7,"future":true},"extension":{"keep":true}} `
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			t.Error("missing shared auth")
		}
		if r.URL.Path == "/api/v1/chat/completions" {
			writeToolCallStream(w, true)
			return
		}
		if r.URL.Path != "/api/alpha/decisions" || r.Method != "POST" {
			t.Errorf("endpoint=%s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil || len(fields) != 3 || string(fields["model"]) != `"decision/model"` || !strings.Contains(string(fields["state"]), "9007199254740993") || !strings.Contains(string(fields["state"]), "1e3") || !strings.Contains(string(fields["questions"]), `"extension":{"keep":true}`) {
			t.Errorf("body lost original JSON: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, raw)
	}))
	defer server.Close()
	client := &countingHTTPClient{base: server.Client()}
	p := NewOpenRouter(Options{KeyFile: writeTestKey(t), BaseURL: server.URL + "/api/v1", DecisionServerURL: server.URL, HTTPClient: client})
	var reads atomic.Int32
	p.readFile = func(string) ([]byte, error) { reads.Add(1); return []byte("fixture-secret"), nil }
	res, err := p.Decide(context.Background(), decisionRequest("decision/model"))
	if err != nil || string(res.JSON) != raw || res.StatusCode != 200 {
		t.Fatalf("response=%s status=%d err=%v", res.JSON, res.StatusCode, err)
	}
	if _, err := p.Generate(context.Background(), testRequest()); err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 1 || requests.Load() != 2 || client.closes.Load() != 2 {
		t.Fatalf("reads=%d requests=%d closes=%d", reads.Load(), requests.Load(), client.closes.Load())
	}
}

func TestDecideErrorsDoNotRetry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		kind   ErrorKind
	}{
		{"auth", 401, `{"error":{"message":"fixture-secret"}}`, ErrorAuthentication},
		{"quota", 402, `{"error":{"message":"quota"}}`, ErrorQuota},
		{"rate", 429, `{"error":{"message":"rate"}}`, ErrorRateLimit},
		{"server", 503, `{"error":{"message":"server"}}`, ErrorTransport},
		{"truncated", 200, `{"answers":`, ErrorResponse},
		{"empty", 200, ``, ErrorResponse},
		{"missing answers", 200, `{"model":"m"}`, ErrorResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer server.Close()
			p := NewOpenRouter(Options{KeyFile: writeTestKey(t), DecisionServerURL: server.URL, HTTPClient: server.Client()})
			_, err := p.Decide(context.Background(), decisionRequest("m"))
			assertProviderError(t, err, tc.kind)
			if calls.Load() != 1 || strings.Contains(err.Error(), "fixture-secret") {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestDecideConcurrentResponsesAreIndependent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"model": body.Model, "answers": map[string]any{"q": map[string]any{"type": "future", "value": body.Model}}, "usage": map[string]int{"input_tokens": 1, "output_tokens": 0}})
	}))
	defer server.Close()
	p := NewOpenRouter(Options{KeyFile: writeTestKey(t), DecisionServerURL: server.URL, HTTPClient: server.Client()})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			model := fmt.Sprint("model/", i)
			res, err := p.Decide(context.Background(), decisionRequest(model))
			if err != nil || !strings.Contains(string(res.JSON), `"model":"`+model+`"`) {
				t.Errorf("model=%s response=%s err=%v", model, res.JSON, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestDecideCancellationClosesBody(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"answers":`)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	client := &countingHTTPClient{base: server.Client(), readStarted: make(chan struct{})}
	p := NewOpenRouter(Options{KeyFile: writeTestKey(t), DecisionServerURL: server.URL, HTTPClient: client})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := p.Decide(ctx, decisionRequest("m")); done <- err }()
	<-started
	<-client.readStarted
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request not cancelled")
	}
	if client.closes.Load() != 1 {
		t.Fatalf("body closes=%d", client.closes.Load())
	}
}
