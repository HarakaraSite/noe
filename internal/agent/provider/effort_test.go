package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestOpenRouterEffortWireFormat(t *testing.T) {
	for _, effort := range []string{"", "auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		name := effort
		if name == "" {
			name = "omitted"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				if _, exists := body["reasoning_effort"]; exists {
					t.Error("unexpected legacy reasoning_effort field")
				}
				if effort == "" || effort == "auto" {
					if _, exists := body["reasoning"]; exists {
						t.Errorf("auto must leave provider reasoning defaults unchanged: %s", body["reasoning"])
					}
				} else if string(body["reasoning"]) != `{"effort":"`+effort+`"}` {
					t.Errorf("reasoning=%s, want effort %q", body["reasoning"], effort)
				}
				writeToolCallStream(w, true)
			}))
			defer server.Close()
			p := NewOpenRouter(Options{KeyFile: writeTestKey(t), BaseURL: server.URL + "/api/v1"})
			_, err := p.Generate(context.Background(), Request{
				Model: "fixture/model", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"task"}`)}, Effort: effort,
			})
			if err != nil || calls.Load() != 1 {
				t.Fatalf("Generate error=%v requests=%d", err, calls.Load())
			}
		})
	}
}

func TestOpenRouterInvalidEffortDoesNotReadCredentials(t *testing.T) {
	p := NewOpenRouter(Options{KeyFile: "unused"})
	reads := 0
	p.readFile = func(string) ([]byte, error) {
		reads++
		return nil, errors.New("must not read credentials")
	}
	_, err := p.Generate(context.Background(), Request{
		Model: "fixture/model", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"task"}`)}, Effort: "unknown",
	})
	var providerErr *ProviderError
	if !errors.As(err, &providerErr) || providerErr.Kind != ErrorResponse || reads != 0 {
		t.Fatalf("invalid effort error=%v credential reads=%d", err, reads)
	}
}
