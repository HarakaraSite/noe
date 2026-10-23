package cli

import (
	"bytes"
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

func TestCLIExecutionContractAcrossSDKProtocols(t *testing.T) {
	for _, sdk := range []string{"openrouter", "openai"} {
		for _, api := range []string{"chat-completions", "responses"} {
			t.Run(sdk+"/"+api, func(t *testing.T) {
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body map[string]json.RawMessage
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						http.Error(w, "bad request", 400)
						return
					}
					step := calls.Add(1)
					write := func(value any) {
						data, err := json.Marshal(value)
						if err != nil {
							t.Error(err)
							return
						}
						fmt.Fprintf(w, "data: %s\n\n", data)
					}
					if step > 1 {
						field := "messages"
						if api == "responses" {
							field = "input"
						}
						var items []map[string]json.RawMessage
						if err := json.Unmarshal(body[field], &items); err != nil {
							t.Error(err)
						}
						results := map[string]map[string]any{}
						for _, item := range items {
							var role, kind, id, content string
							_ = json.Unmarshal(item["role"], &role)
							_ = json.Unmarshal(item["type"], &kind)
							if role == "tool" {
								_ = json.Unmarshal(item["tool_call_id"], &id)
								_ = json.Unmarshal(item["content"], &content)
							} else if kind == "function_call_output" {
								_ = json.Unmarshal(item["call_id"], &id)
								_ = json.Unmarshal(item["output"], &content)
							} else {
								continue
							}
							var result map[string]any
							if err := json.Unmarshal([]byte(content), &result); err != nil {
								t.Errorf("tool output: %v", err)
							}
							results[id] = result
						}
						good, _ := results["data"]["data"].(map[string]any)
						diagnostics, _ := results["diagnostic"]["diagnostics"].([]any)
						if good["answer"] != float64(42) || len(diagnostics) != 1 {
							t.Errorf("new result lost on %s/%s wire: %v", sdk, api, results)
						} else {
							diagnostic, _ := diagnostics[0].(map[string]any)
							if diagnostic["code"] != "JS_SYNTAX" || diagnostic["line"] != float64(2) || diagnostic["source"] != "[agent tool]" {
								t.Errorf("source/diagnostic mismatch: %v", diagnostic)
							}
						}
						if api == "responses" && !bytes.Contains(body["input"], []byte("opaque-contract-reasoning")) {
							t.Error("native reasoning lost")
						}
					}
					w.Header().Set("Content-Type", "text/event-stream")
					if api == "chat-completions" {
						delta := map[string]any{"role": "assistant", "content": "verified"}
						finish := "stop"
						if step == 1 {
							tool := func(id, code string, index int) map[string]any {
								args, _ := json.Marshal(map[string]string{"code": code})
								return map[string]any{"id": id, "index": index, "type": "function", "function": map[string]any{"name": "run_js", "arguments": string(args)}}
							}
							delta = map[string]any{"role": "assistant", "tool_calls": []any{tool("data", `import {check} from "noe:runtime";const inspected=check('import "noe:auth";import "noe:agent-log";');if(!inspected.ok)throw Error(JSON.stringify(inspected));export default {answer:42};`, 0), tool("diagnostic", "\nexport const value = ;", 1)}}
							finish = "tool_calls"
						}
						write(map[string]any{"id": "contract", "object": "chat.completion.chunk", "created": 1, "model": "fixture", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
						fmt.Fprint(w, "data: [DONE]\n\n")
					} else {
						output := []any{map[string]any{"id": "answer", "type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": "verified", "annotations": []any{}}}}}
						if step == 1 {
							tool := func(id, code string) map[string]any {
								args, _ := json.Marshal(map[string]string{"code": code})
								return map[string]any{"id": "item-" + id, "type": "function_call", "status": "completed", "call_id": id, "name": "run_js", "arguments": string(args)}
							}
							output = []any{map[string]any{"id": "reasoning", "type": "reasoning", "summary": []any{}, "encrypted_content": "opaque-contract-reasoning"}, tool("data", `import {check} from "noe:runtime";const inspected=check('import "noe:auth";import "noe:agent-log";');if(!inspected.ok)throw Error(JSON.stringify(inspected));export default {answer:42};`), tool("diagnostic", "\nexport const value = ;")}
						}
						write(map[string]any{"type": "response.completed", "sequence_number": 1, "response": map[string]any{"id": "contract", "object": "response", "created_at": 1, "status": "completed", "model": "fixture", "error": nil, "incomplete_details": nil, "parallel_tool_calls": true, "tool_choice": "auto", "tools": []any{}, "output": output, "usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}})
					}
				}))
				defer server.Close()
				dir := t.TempDir()
				key := filepath.Join(dir, "key")
				config := filepath.Join(dir, "config.json")
				if err := os.WriteFile(key, []byte("fixture-key"), 0600); err != nil {
					t.Fatal(err)
				}
				raw, err := json.Marshal(map[string]any{"providers": map[string]any{"contract": map[string]any{"sdk": sdk, "auth": "api-key", "api": api, "baseURL": server.URL, "path": "/contract", "keyFile": key}}})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(config, raw, 0600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("NOE_CONFIG_FILE", config)
				t.Setenv("NOE_AGENT_RUNTIME_DIR", dir)
				source := `import {run_agent} from "noe:agent";const value=await run_agent({provider:"contract",model:"fixture",effort:"auto",maxSteps:3},"verify");if(value.lastResult?.state!=="completed")throw Error(JSON.stringify(value));console.log(value.lastResult.answer);`
				var out, diagnostic bytes.Buffer
				code := Run(t.Context(), []string{"eval", source}, strings.NewReader(""), &out, &diagnostic)
				if code != 0 || out.String() != "verified\n" || calls.Load() != 2 {
					t.Fatalf("code=%d calls=%d out=%q diagnostics=%q", code, calls.Load(), out.String(), diagnostic.String())
				}
			})
		}
	}
}
