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
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
)

func TestCLIOpenAIChatGPTParentChildAndAPIKeyRoute(t *testing.T) {
	var mu sync.Mutex
	cacheKeys := map[string][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model     string            `json:"model"`
			Input     []json.RawMessage `json:"input"`
			CacheKey  string            `json:"prompt_cache_key"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid SDK request")
		}
		mu.Lock()
		cacheKeys[body.Model] = append(cacheKeys[body.Model], body.CacheKey)
		index := len(cacheKeys[body.Model])
		mu.Unlock()
		wantAuth := "Bearer oauth-fixture"
		if body.Model == "api-key" {
			wantAuth = "Bearer api-fixture"
		}
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != wantAuth || r.Header.Get("User-Agent") != "noe/"+Version || body.Reasoning.Effort != "high" {
			t.Errorf("wrong SDK authentication/effort route for %s", body.Model)
		}
		output := []any{map[string]any{"type": "message", "id": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "answer-" + body.Model, "annotations": []any{}}}}}
		if body.Model == "parent" && index == 1 {
			args, _ := json.Marshal(map[string]string{"code": "// Run a child with inherited ChatGPT authentication\nimport {run_agent} from \"noe:agent\";\nconst child=await run_agent({name:'child',model:'child'},'child task');console.log(child.lastResult.answer);"})
			output = []any{map[string]any{"id": "call-item", "type": "function_call", "status": "completed", "call_id": "delegate", "name": "run_js", "arguments": string(args)}}
		} else if body.Model == "parent" {
			input, _ := json.Marshal(body.Input)
			if len(body.Input) < 4 || !strings.Contains(string(input), "answer-child") {
				t.Error("missing tool continuation")
			}
		}
		response := map[string]any{"id": "response", "object": "response", "status": "completed", "created_at": 1, "model": body.Model, "error": nil, "output": output, "usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}
		raw, _ := json.Marshal(map[string]any{"type": "response.completed", "sequence_number": 1, "response": response})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", raw)
	}))
	defer server.Close()
	dir := t.TempDir()
	key := filepath.Join(dir, "api-key")
	if err := os.WriteFile(key, []byte("api-fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "chatgpt.json")
	if err := credentials.Write(file, credentials.Record{Issuer: "https://auth.openai.com", Subject: "user", ClientID: "issued", Access: "oauth-fixture", ExpiresAt: time.Now().Add(time.Hour), Scopes: []string{credentials.PlanScope}}); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(map[string]any{"providers": map[string]any{
		"openai-chatgpt": map[string]any{"baseURL": server.URL + "/v1", "oauthFile": file},
		"openai-api":     map[string]any{"baseURL": server.URL + "/v1", "keyFile": key},
	}})
	if err := os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOE_CONFIG_FILE", config)
	t.Setenv("NOE_OPENAI_CHATGPT_CREDENTIALS_FILE", "")
	t.Setenv("NOE_OPENAI_API_KEY_FILE", "")
	t.Setenv("NOE_AGENT_RUNTIME_DIR", t.TempDir())
	t.Setenv("NOE_AGENT_TRACE", "")
	source := `import {run_agent} from "noe:agent";
const parent=await run_agent({provider:"openai-chatgpt",model:"parent",effort:"high"},"delegate");
const api=await run_agent({provider:"openai-api",model:"api-key",effort:"high"},"answer");
if(parent.lastResult.state!=="completed" || api.lastResult.state!=="completed")throw Error("task failed");
console.log(parent.lastResult.answer,api.lastResult.answer);`
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), []string{"eval", "--events=" + filepath.Join(dir, "events.jsonl"), source}, strings.NewReader(""), &out, &diagnostic)
	if code != 0 || out.String() != "answer-parent answer-api-key\n" {
		t.Fatalf("code=%d out=%q stderr=%q", code, &out, &diagnostic)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(cacheKeys["parent"]) != 2 || len(cacheKeys["child"]) != 1 || len(cacheKeys["api-key"]) != 1 || cacheKeys["parent"][0] == "" || cacheKeys["parent"][0] != cacheKeys["parent"][1] || cacheKeys["parent"][0] == cacheKeys["child"][0] {
		t.Fatalf("cache identities: %v", cacheKeys)
	}
}
