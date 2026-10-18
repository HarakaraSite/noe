package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Exercise the actual example: merged catalog -> Jev choice -> root launch ->
// run_js reading the same host catalog. All provider traffic stays local.
func TestCatalogRoutingExampleAndRunJS(t *testing.T) {
	var decisionCalls, modelCalls atomic.Int32
	var config string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/alpha/decisions" {
			decisionCalls.Add(1)
			var body struct {
				Questions map[string]struct {
					Criteria map[string]string `json:"criteria"`
				} `json:"questions"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			criteria := body.Questions["model"].Criteria
			if len(criteria) != 2 || !strings.Contains(criteria["m1"], "configured worker") || strings.Contains(criteria["m1"], "headers") {
				t.Errorf("Jev did not receive catalog metadata: %v", criteria)
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"model":"typesafe/jev-1.14","answers":{"model":{"type":"choice","choice":"m1"}}}`)
			return
		}
		call := modelCalls.Add(1)
		var body struct {
			Model    string                       `json:"model"`
			Messages []map[string]json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/v1/chat/completions" || body.Model != "fixture-worker" {
			t.Errorf("wrong catalog candidate launched: %s %s", r.URL.Path, body.Model)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		delta := map[string]any{"role": "assistant", "content": "catalog observed"}
		finish := "stop"
		if call == 1 {
			if err := os.Remove(config); err != nil {
				t.Error(err)
			}
			code := "// Inspect the shared host catalog\nconst {list_models}=await import('noe:agent');const models=await list_models({provider:'fixture',kind:'agent'});if(models.length!==2 || models.find(m=>m.model==='fixture-worker')?.description!=='configured worker')throw Error('wrong catalog');console.log('child sees host catalog');"
			args, _ := json.Marshal(map[string]string{"code": code})
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "read-catalog", "type": "function", "function": map[string]any{"name": "run_js", "arguments": string(args)}}}}
			finish = "tool_calls"
		} else if len(body.Messages) < 5 || !strings.Contains(string(body.Messages[len(body.Messages)-2]["content"]), "child sees host catalog") {
			t.Error("run_js did not return the host catalog result")
		}
		chunk := map[string]any{"id": "catalog-test", "object": "chat.completion.chunk", "created": 1, "model": body.Model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
		raw, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", raw)
	}))
	defer server.Close()
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	if err := os.WriteFile(key, []byte("fixture-key"), 0600); err != nil {
		t.Fatal(err)
	}
	config = filepath.Join(dir, "config.json")
	data, _ := json.Marshal(map[string]any{"providers": map[string]any{
		"openrouter": map[string]any{"keyFile": key, "decisionServerURL": server.URL},
		"fixture": map[string]any{"api": "chat-completions", "path": "/chat/completions", "baseURL": server.URL + "/v1", "keyFile": key, "models": map[string]any{
			"a-other-worker": map[string]any{"catalog": map[string]any{"kind": "agent", "description": "Another candidate"}},
			"fixture-worker": map[string]any{"catalog": map[string]any{"kind": "agent", "description": "configured worker"}},
		}},
	}})
	if err := os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOE_CONFIG_FILE", config)
	t.Setenv("NOE_OPENROUTER_API_KEY_FILE", "")
	t.Setenv("NOE_OPENROUTER_BASE_URL", "")
	t.Setenv("NOE_AGENT_RUNTIME_DIR", t.TempDir())
	t.Setenv("NOE_AGENT_TRACE", "")
	// A child process must use the root host's frozen catalog, even when its
	// inherited config path has disappeared before run_js begins.
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), []string{"run", "--events=" + filepath.Join(dir, "events.jsonl"), filepath.Join("..", "..", "examples", "agent-route-model.js"), "fixture", "inspect catalog"}, strings.NewReader(""), &out, &diagnostic)
	if code != 0 || out.String() != "catalog observed\n" || decisionCalls.Load() != 1 || modelCalls.Load() != 2 {
		t.Fatalf("code=%d out=%q stderr=%q decisions=%d model=%d", code, &out, &diagnostic, decisionCalls.Load(), modelCalls.Load())
	}
}
