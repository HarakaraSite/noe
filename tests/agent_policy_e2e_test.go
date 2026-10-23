//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAgentCyberPolicyRetainsReasonAndDoesNotExecutePartialTools(t *testing.T) {
	for _, sdk := range []string{"openai", "openrouter"} {
		for _, entry := range []string{"http", "failed", "stream"} {
			t.Run(sdk+"/"+entry, func(t *testing.T) {
				work := t.TempDir()
				var calls atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					w.Header().Set("X-Request-Id", "upstream-policy-request")
					upstream := `{"code":"cyber_policy","message":"Cybersecurity check blocked this request; see Daybreak."}`
					if entry == "http" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(403)
						fmt.Fprint(w, `{"error":`+upstream+`}`)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					// A complete tool item is still not executable until the
					// entire model response has succeeded.
					code := `import {writeFileSync} from 'node:fs';writeFileSync('must-not-exist','executed');`
					arguments, _ := json.Marshal(map[string]string{"code": code})
					item, _ := json.Marshal(map[string]any{"type": "function_call", "id": "fc", "call_id": "partial-call", "name": "run_js", "arguments": string(arguments), "status": "completed"})
					fmt.Fprintf(w, "data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":%s}\n\n", item)
					if entry == "failed" {
						fmt.Fprint(w, "data: "+`{"type":"response.failed","sequence_number":2,"response":{"status":"failed","error":`+upstream+"}}\n\n")
					} else {
						fmt.Fprint(w, "data: "+`{"type":"error","sequence_number":2,`+upstream[1:]+"\n\n")
					}
				}))
				defer server.Close()
				key := filepath.Join(work, "key")
				writeFixture(t, key, "local-policy-fixture-key")
				config, _ := json.Marshal(map[string]any{"providers": map[string]any{"policy-fixture": map[string]any{"sdk": sdk, "auth": "api-key", "api": "responses", "baseURL": server.URL, "path": "/responses", "keyFile": key}}})
				configPath := filepath.Join(work, "config.json")
				writeFixture(t, configPath, string(config))
				eventPath := filepath.Join(work, "events.jsonl")
				source := `import {run_agent} from 'noe:agent'; console.log(JSON.stringify(await run_agent({provider:'policy-fixture',model:'fixture/model',effort:'auto'},'local policy probe')));`
				result := runCLI(t, work, map[string]string{"NOE_CONFIG_FILE": configPath, "NOE_AGENT_RUNTIME_DIR": t.TempDir(), "XDG_CONFIG_HOME": t.TempDir()}, "eval", "--events="+eventPath, source)
				requireExit(t, result, 0) // run_agent returns task failure as a snapshot.
				var snapshot struct {
					LastResult struct {
						State        string         `json:"state"`
						Answer       string         `json:"answer"`
						Error        string         `json:"error"`
						ErrorDetails map[string]any `json:"errorDetails"`
					} `json:"lastResult"`
				}
				if err := json.Unmarshal([]byte(result.stdout), &snapshot); err != nil {
					t.Fatalf("snapshot: %v, stdout=%s stderr=%s", err, result.stdout, result.stderr)
				}
				outcome := snapshot.LastResult
				if outcome.State != "failed" || outcome.Answer != "" || !strings.Contains(outcome.Error, "cybersecurity policy") || outcome.ErrorDetails["code"] != "cyber_policy" || outcome.ErrorDetails["requestId"] != "upstream-policy-request" || !strings.Contains(fmt.Sprint(outcome.ErrorDetails["providerMessage"]), "Daybreak") || calls.Load() != 1 {
					t.Fatalf("outcome=%+v provider calls=%d", outcome, calls.Load())
				}
				if _, err := os.Stat(filepath.Join(work, "must-not-exist")); !os.IsNotExist(err) {
					t.Fatalf("partial tool ran: %v", err)
				}
				data, err := os.ReadFile(eventPath)
				if err != nil {
					t.Fatal(err)
				}
				seen := map[string]bool{}
				for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
					var event map[string]any
					if err := json.Unmarshal([]byte(line), &event); err != nil {
						t.Fatal(err)
					}
					kind, _ := event["type"].(string)
					if kind == "run_js.started" {
						t.Fatal("failed response started a tool")
					}
					if kind == "model.failed" || kind == "agent.failed" || kind == "command.failed" {
						// command events carry receipt state; task/model events
						// carry diagnostics, as for completed-response refusals.
						if kind != "command.failed" {
							details, _ := event["errorDetails"].(map[string]any)
							if details["code"] != "cyber_policy" || details["requestId"] != "upstream-policy-request" {
								t.Fatalf("missing policy event details: %v", event)
							}
						}
						if kind == "model.failed" && event["reason"] != "policy" {
							t.Fatalf("wrong model failure classification: %v", event)
						}
						seen[kind] = true
					}
				}
				if len(seen) != 3 {
					t.Fatalf("missing terminal events: %v", seen)
				}
			})
		}
	}
}
