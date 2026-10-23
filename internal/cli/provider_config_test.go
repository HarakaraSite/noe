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
)

func TestCLIConfiguredProviderParentChildSessions(t *testing.T) {
	var mu sync.Mutex
	sessions := map[string][]string{}
	var providerErrors []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model       string                       `json:"model"`
			Messages    []map[string]json.RawMessage `json:"messages"`
			Temperature float64                      `json:"temperature"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		sessions[body.Model] = append(sessions[body.Model], r.Header.Get("x-opencode-session"))
		step := len(sessions[body.Model])
		mu.Unlock()
		if r.Header.Get("User-Agent") != "noe/"+Version || r.Header.Get("X-Configured") != "yes" || r.Header.Get("Authorization") != "Bearer fixture-key" || r.URL.Path != "/api/chat/completions" || body.Temperature != 0.2 {
			t.Error("configured request settings not applied")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(delta map[string]any, finish any) {
			chunk := map[string]any{"id": "x", "object": "chat.completion.chunk", "created": 1, "model": body.Model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
			raw, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", raw)
		}
		if body.Model == "parent" && step == 1 {
			write(map[string]any{"role": "assistant", "reasoning_content": "one "}, nil)
			write(map[string]any{"reasoning_content": "two"}, nil)
			arguments, _ := json.Marshal(map[string]any{"code": "// Run an inherited child task\nconst child=await run_agent({name:'child',model:'child'},'child task');console.log(child.lastResult.answer);"})
			write(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "call-1", "type": "function", "function": map[string]any{"name": "run_js", "arguments": string(arguments)}}}}, "tool_calls")
		} else {
			var reasoning json.RawMessage
			for _, message := range body.Messages {
				if string(message["role"]) == `"assistant"` {
					reasoning = message["reasoning_content"]
				}
			}
			if body.Model == "parent" && string(reasoning) != `"one two"` {
				mu.Lock()
				providerErrors = append(providerErrors, "reasoning replay lost")
				mu.Unlock()
			}
			write(map[string]any{"role": "assistant", "content": "answer-" + body.Model}, "stop")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	dir := t.TempDir()
	key := filepath.Join(dir, "key")
	os.WriteFile(key, []byte("fixture-key"), 0600)
	config := filepath.Join(dir, "config.json")
	data, _ := json.Marshal(map[string]any{"providers": map[string]any{"opencode-go": map[string]any{"baseURL": server.URL + "/api", "keyFile": key, "headers": map[string]string{"X-Configured": "yes"}, "parameters": map[string]any{"temperature": 0.2}}}})
	os.WriteFile(config, data, 0600)
	t.Setenv("NOE_CONFIG_FILE", config)
	t.Setenv("NOE_OPENCODE_GO_API_KEY_FILE", "")
	t.Setenv("NOE_AGENT_RUNTIME_DIR", t.TempDir())
	t.Setenv("NOE_AGENT_TRACE", "")
	logPath := filepath.Join(dir, "events.jsonl")
	source := `import {run_agent} from "noe:agent"; const result=await run_agent({provider:"opencode-go",model:"parent",effort:"auto"},"delegate");if(result.lastResult.state!=="completed")throw Error(result.lastResult.error);console.log(result.lastResult.answer);`
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), []string{"eval", "--events=" + logPath, source}, strings.NewReader(""), &out, &diagnostic)
	if code != 0 || out.String() != "answer-parent\n" {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, &out, &diagnostic)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(providerErrors) > 0 {
		t.Fatal(providerErrors)
	}
	if len(sessions["parent"]) != 2 || len(sessions["child"]) != 1 || sessions["parent"][0] == "" || sessions["parent"][0] != sessions["parent"][1] || sessions["parent"][0] == sessions["child"][0] {
		t.Fatalf("parent/child session IDs: %v", sessions)
	}
	log, _ := os.ReadFile(logPath)
	var thinking strings.Builder
	for _, line := range bytes.Split(log, []byte("\n")) {
		var event struct {
			Type    string `json:"type"`
			RunID   string `json:"runId"`
			AgentID string `json:"agentId"`
			Model   string `json:"model"`
			Text    string `json:"text"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		if event.Type == "agent.started" && (event.AgentID != sessions[event.Model][0] || event.RunID == event.AgentID) {
			t.Errorf("header must identify the instance separately from its task: %+v", event)
		}
		if event.Type == "thinking" {
			thinking.WriteString(event.Text)
		}
	}
	if thinking.String() != "one two" {
		t.Fatalf("thinking not observed: %q", thinking.String())
	}
}

func TestCLIProviderConfigIsLazy(t *testing.T) {
	t.Setenv("NOE_CONFIG_FILE", filepath.Join(t.TempDir(), "missing-config"))
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), []string{"eval", `await import("noe:agent"); console.log(42);`}, strings.NewReader(""), &out, &diagnostic)
	if code != 0 || out.String() != "42\n" {
		t.Fatalf("import read config: code=%d stderr=%q", code, &diagnostic)
	}
}
