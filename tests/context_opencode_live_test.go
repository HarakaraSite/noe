//go:build e2e && context_live

package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type contextLiveReceipt struct {
	Model          string   `json:"model"`
	Session        string   `json:"session"`
	UserAgent      string   `json:"userAgent"`
	Notice         string   `json:"notice"`
	Calls          []string `json:"calls"`
	Results        []string `json:"results"`
	Roles          []string `json:"roles"`
	CompleteBlocks bool     `json:"completeBlocks"`
}

// This opt-in test sends ordinary coding tasks to the one authorized provider.
// The proxy forwards unchanged bodies but only persists allowlisted metadata.
func TestContextOpenCodeLive(t *testing.T) {
	if os.Getenv("NOE_AGENT_CONTEXT_LIVE") != "1" {
		t.Skip("set NOE_AGENT_CONTEXT_LIVE=1")
	}
	root := os.Getenv("NOE_AGENT_CONTEXT_LIVE_DIR")
	if root == "" {
		t.Fatal("NOE_AGENT_CONTEXT_LIVE_DIR required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	key := os.Getenv("NOE_OPENCODE_GO_API_KEY_FILE")
	if key == "" {
		config, e := os.UserConfigDir()
		if e != nil {
			t.Fatal(e)
		}
		key = filepath.Join(config, "noe", "credentials", "opencode-go-api-key")
	}
	if stat, e := os.Stat(key); e != nil || !stat.Mode().IsRegular() {
		t.Fatal("authorized OpenCode credential file unavailable")
	}
	for _, tc := range []struct {
		Name, Policy string
		Controlled   bool
	}{
		{"controlled_retain_01", `{"retainRatio":0.1}`, true},
		{"controlled_retain_02", `{"retainRatio":0.2}`, true},
		{"controlled_disabled", `null`, true},
		{"natural_task", `{}`, false},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			dir := filepath.Join(root, tc.Name)
			if e := os.Mkdir(dir, 0700); e != nil {
				t.Fatalf("use a fresh NOE_AGENT_CONTEXT_LIVE_DIR for each trial: %v", e)
			}
			var mu sync.Mutex
			receipts := []contextLiveReceipt{}
			target, _ := url.Parse("https://opencode.ai/inference/go/openai/v1")
			proxy := httputil.NewSingleHostReverseProxy(target)
			director := proxy.Director
			proxy.Director = func(r *http.Request) { director(r); r.Host = target.Host }
			proxy.ErrorLog = log.New(io.Discard, "", 0)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, e := io.ReadAll(r.Body)
				_ = r.Body.Close()
				if e != nil {
					http.Error(w, "read failed", 400)
					return
				}
				var request struct {
					Model    string `json:"model"`
					Messages []struct {
						Role    string          `json:"role"`
						Content json.RawMessage `json:"content"`
						ToolID  string          `json:"tool_call_id"`
						Calls   []struct {
							ID string `json:"id"`
						} `json:"tool_calls"`
					} `json:"messages"`
				}
				if json.Unmarshal(body, &request) != nil || request.Model != "deepseek-v4.1-flash" || r.URL.Path != "/chat/completions" {
					http.Error(w, "unexpected provider request", 400)
					return
				}
				receipt := contextLiveReceipt{Model: request.Model, Session: r.Header.Get("x-opencode-session"), UserAgent: r.Header.Get("User-Agent"), CompleteBlocks: true}
				pending := map[string]bool{}
				notices := 0
				for _, m := range request.Messages {
					receipt.Roles = append(receipt.Roles, m.Role)
					for _, call := range m.Calls {
						pending[call.ID] = true
						receipt.Calls = append(receipt.Calls, call.ID)
					}
					if m.Role == "tool" {
						receipt.Results = append(receipt.Results, m.ToolID)
						if !pending[m.ToolID] {
							receipt.CompleteBlocks = false
						}
						delete(pending, m.ToolID)
					}
					var text string
					_ = json.Unmarshal(m.Content, &text)
					if m.Role == "system" && strings.HasPrefix(text, "NOE_CONTEXT_STATUS\n") {
						receipt.Notice = text
						notices++
					}
				}
				if len(pending) > 0 || notices != 1 {
					receipt.CompleteBlocks = false
				}
				mu.Lock()
				receipts = append(receipts, receipt)
				mu.Unlock()
				r.Body = io.NopCloser(bytes.NewReader(body))
				proxy.ServeHTTP(w, r)
			}))
			defer server.Close()
			writeFixture(t, filepath.Join(dir, "config.json"), fmt.Sprintf(`{"providers":{"opencode-go":{"baseURL":%q}}}`, server.URL))
			writeFixture(t, filepath.Join(dir, "control.mjs"), `export async function afterTool(event) {
 const result=event.result;
 return {result:{...result,stdout:result.stdout+"\nCONTROLLED_CONTEXT_PADDING\n"+"x".repeat(24000)}};
}`)
			for i := 1; i <= 4; i++ {
				writeFixture(t, filepath.Join(dir, fmt.Sprintf("input-%d.json", i)), fmt.Sprintf(`{"id":%d,"value":%d}`, i, i*7))
			}
			hooks := "[]"
			if tc.Controlled {
				hooks = `["./control.mjs"]`
			}
			source := fmt.Sprintf(`import {run_agent} from "noe:agent";
const result=await run_agent({provider:"opencode-go",model:"deepseek-v4.1-flash",effort:"auto",name:%q,maxTokens:8192,maxSteps:12,executionLimits:{providerCalls:12},contextBudget:{inputTokens:11000},contextPolicy:%s,hooks:%s},
"In this temporary workspace, read input-1.json through input-4.json and create report.json with shape {items:[{id:number,value:number}],total:number}. Preserve the numeric id and value from each source; do not turn them into strings. Process exactly one previously unprocessed input file per run_js call, updating report.json in that same call. Read the existing report if necessary to avoid repeated work. Do not read all inputs in one call. Do not delegate or use network. After all four are recorded, check report.json in one separate run_js call, then report the total briefly. Do not print padding or long file contents. Extra memory files are optional and should only be created if useful. This is a coding task; use the existing NOE JavaScript APIs.");
console.log(JSON.stringify(result));
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result.lastResult));`, tc.Name, tc.Policy, hooks)
			writeFixture(t, filepath.Join(dir, "task.mjs"), source)
			ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, cliPath, "run", "--timeout=175s", "--observe", "--events="+filepath.Join(dir, "events.jsonl"), "task.mjs")
			cmd.Dir = dir
			cmd.Env = envWith(envWith(os.Environ(), "NOE_CONFIG_FILE", filepath.Join(dir, "config.json")), "NOE_OPENCODE_GO_API_KEY_FILE", key)
			var out, stderr bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &stderr
			started := time.Now()
			runErr := cmd.Run()
			writeFixture(t, filepath.Join(dir, "observe.txt"), out.String())
			writeFixture(t, filepath.Join(dir, "stderr.txt"), stderr.String())
			mu.Lock()
			data, _ := json.MarshalIndent(receipts, "", "  ")
			saved := append([]contextLiveReceipt{}, receipts...)
			mu.Unlock()
			writeFixture(t, filepath.Join(dir, "wire.json"), string(data))
			summary := map[string]any{"elapsedMs": time.Since(started).Milliseconds(), "requests": len(saved), "policy": json.RawMessage(tc.Policy), "controlled": tc.Controlled, "commandSuccess": runErr == nil}
			defer func() {
				summary["validationPassed"] = !t.Failed()
				encoded, _ := json.MarshalIndent(summary, "", "  ")
				writeFixture(t, filepath.Join(dir, "summary.json"), string(encoded))
			}()
			trims := 0
			raw, e := os.Open(filepath.Join(dir, "events.jsonl"))
			if e == nil {
				scanner := bufio.NewScanner(raw)
				scanner.Buffer(make([]byte, 65536), 16*1024*1024)
				for scanner.Scan() {
					var event map[string]any
					if json.Unmarshal(scanner.Bytes(), &event) == nil && event["type"] == "context.trimmed" {
						trims++
					}
				}
				_ = raw.Close()
			}
			summary["trims"] = trims
			summaryData, _ := json.MarshalIndent(summary, "", "  ")
			writeFixture(t, filepath.Join(dir, "summary.json"), string(summaryData))
			t.Logf("artifacts=%s elapsed=%s requests=%d trims=%d", dir, time.Since(started).Round(time.Second), len(saved), trims)
			if runErr != nil {
				t.Fatalf("live task failed: %v; stderr=%s", runErr, stderr.String())
			}
			if len(saved) == 0 || len(saved) > 12 {
				t.Fatalf("unexpected request count %d", len(saved))
			}
			for _, receipt := range saved {
				if !receipt.CompleteBlocks || receipt.Session == "" || receipt.Session != saved[0].Session || !strings.HasPrefix(receipt.UserAgent, "noe/") {
					t.Fatalf("wire contract violated; see wire.json")
				}
			}
			reportData, e := os.ReadFile(filepath.Join(dir, "report.json"))
			if e != nil {
				t.Fatal(e)
			}
			var report struct {
				Items []struct{ ID, Value int }
				Total int
			}
			if json.Unmarshal(reportData, &report) != nil || report.Total != 70 || len(report.Items) != 4 {
				t.Fatalf("incorrect report: %s", reportData)
			}
			seen := map[int]bool{}
			for _, item := range report.Items {
				if item.ID < 1 || item.ID > 4 || seen[item.ID] || item.Value != item.ID*7 {
					t.Fatalf("incorrect items: %s", reportData)
				}
				seen[item.ID] = true
			}
			if tc.Controlled && tc.Policy != "null" && trims < 2 {
				t.Fatalf("controlled task did not exercise repeated trimming: %d", trims)
			}
			if tc.Policy == "null" && trims != 0 {
				t.Fatalf("disabled policy trimmed %d times", trims)
			}
		})
	}
}
