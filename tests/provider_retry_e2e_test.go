//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestProviderRetryKeepsHistoryToolsHooksAndEvents(t *testing.T) {
	work := t.TempDir()
	var calls atomic.Int32
	var retryInput string
	var mu sync.Mutex
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		mu.Lock()
		defer mu.Unlock()
		attempt := calls.Add(1)
		if attempt == 1 {
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("once", `// Record one completed tool execution.
import {appendFile} from "node:fs/promises"; await appendFile("tool-count", "x");`)}}}, FinishReason: "tool_calls"}
		}
		body, _ := json.Marshal(req)
		if attempt == 2 {
			retryInput = string(body)
		} else if string(body) != retryInput {
			t.Errorf("retry changed the committed conversation: %s", body)
		}
		if strings.Contains(string(body), "partial-thinking") || strings.Contains(string(body), "should-not-run") {
			t.Error("failed stream was appended to the next request")
		}
		switch attempt {
		case 2:
			return agentModelReply{Status: 503}
		case 3:
			return agentModelReply{Deltas: []map[string]any{
				{"reasoning": "partial-thinking"},
				{"tool_calls": []any{agentTool("discarded", `// This incomplete tool call must never execute.
import {writeFile} from "node:fs/promises"; await writeFile("should-not-run", "bad");`)}},
			}, FinishReason: "tool_calls", OmitDone: true}
		default:
			return agentModelReply{Content: "retry succeeded"}
		}
	})
	defer server.Close()
	writeFixture(t, filepath.Join(work, "hooks.mjs"), `import {appendFile} from "node:fs/promises";
async function record(phase) { await appendFile("hooks.log", phase+"\n"); }
export async function beforeModelRequest() { await record("beforeModelRequest"); }
export async function afterModelResponse() { await record("afterModelResponse"); }
export async function beforeTool() { await record("beforeTool"); }
export async function afterTool() { await record("afterTool"); }`)
	source := `import {run_agent} from "noe:agent"; import {writeFile} from "node:fs/promises";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2,
hooks:["./hooks.mjs"],executionLimits:{providerCalls:2}},"run one tool and finish");
if(result.lastResult?.state!=="completed"||result.lastResult.answer!=="retry succeeded")throw Error(JSON.stringify(result));
await writeFile("snapshot.json",JSON.stringify(result));`
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	result := runCLI(t, work, env, "eval", "--observe", "--events=events.jsonl", source)
	requireExit(t, result, 0)
	if calls.Load() != 4 || result.stderr != "" || !strings.Contains(result.stdout, "partial-thinking") || !strings.Contains(result.stdout, "retry succeeded") {
		t.Fatalf("retry calls=%d result=%+v", calls.Load(), result)
	}
	if data, err := os.ReadFile(filepath.Join(work, "tool-count")); err != nil || string(data) != "x" {
		t.Fatalf("completed tool replayed: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(work, "should-not-run")); !os.IsNotExist(err) {
		t.Fatalf("incomplete tool call executed: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(work, "hooks.log")); err != nil || string(data) != "beforeModelRequest\nafterModelResponse\nbeforeTool\nafterTool\nbeforeModelRequest\nafterModelResponse\n" {
		t.Fatalf("hooks replayed: %q %v", data, err)
	}
	data, err := os.ReadFile(filepath.Join(work, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event struct{ Type string }
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(event.Type, "retry") {
			t.Fatalf("unexpected retry event: %s", line)
		}
		counts[event.Type]++
	}
	if counts["model.started"] != 2 || counts["model.finished"] != 2 || counts["model.failed"] != 0 || counts["context.updated"] != 2 || counts["run_js.finished"] != 1 {
		t.Fatalf("retry emitted extra lifecycle events: %v", counts)
	}
}
