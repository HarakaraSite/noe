package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestToolOutputProjectionAfterHooksAndIntoHistory(t *testing.T) {
	cwd := t.TempDir()
	writePhaseHook(t, cwd, "output.mjs", `export function beforeTool(event) {
 if (event.rawArguments !== "// Original raw JS.\nexport default 1;") throw Error("lost raw source");
 if (event.arguments.code !== event.rawArguments) throw Error("lost normalized code");
 const pragma=event.toolCallId === "expanded" ? '// @noe: {"outputBytes": 12288}\n' : '';
 return {arguments:{code:'// Execute effective code.\n'+pragma+'export default "actual".repeat(1000);'}};
}
export function afterTool(event) {
 if(event.result.data.length!==6000) throw Error("hook received limited result");
 return {result:{hookValue:event.result.data,exitCode:99}};
}`)
	original := "// Original raw JS.\nexport default 1;"
	args, _ := json.Marshal(map[string]string{"code": original})
	var calls []any
	for _, id := range []string{"limited", "expanded"} {
		calls = append(calls, map[string]any{"id": id, "type": "function", "function": map[string]string{"name": "run_js", "arguments": string(args)}, "_noe_raw_input": original})
	}
	native := json.RawMessage(`[{"type":"reasoning","encrypted_content":"opaque"},{"type":"custom_tool_call","call_id":"limited","name":"run_js","input":"// Original raw JS.\nexport default 1;"}]`)
	reply, _ := json.Marshal(map[string]any{"role": "assistant", "content": nil, "tool_calls": calls, "_noe_response_items": native})
	var requests []provider.Request
	var mu sync.Mutex
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, req)
		if len(requests) == 1 {
			return provider.Response{Message: reply, FinishReason: "tool_calls"}, nil
		}
		return agentAnswer("done"), nil
	})
	settings := agentTestOptions(p)
	var observed []events.Event
	var eventMu sync.Mutex
	settings.Observe = func(e events.Event) { eventMu.Lock(); observed = append(observed, e); eventMu.Unlock() }
	source := `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2,hooks:["./output.mjs"]},"test output");
if(result.lastResult?.state!=="completed") throw Error(JSON.stringify(result));
console.log(result.lastResult.answer);`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: settings, Stdout: &out, Stderr: &stderr})
	if code != 0 || err != nil || out.String() != "done\n" {
		t.Fatalf("code=%d err=%v out=%q stderr=%q", code, err, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("model requests=%d", len(requests))
	}
	modelResults := map[string]json.RawMessage{}
	preserved := false
	for _, raw := range requests[1].Messages {
		var message struct {
			Role       string
			ToolCallID string `json:"tool_call_id"`
			Content    string
		}
		if json.Unmarshal(raw, &message) == nil && message.Role == "tool" {
			modelResults[message.ToolCallID] = json.RawMessage(message.Content)
		}
		if bytes.Contains(raw, []byte(`"_noe_response_items"`)) {
			var m map[string]json.RawMessage
			_ = json.Unmarshal(raw, &m)
			preserved = bytes.Equal(m["_noe_response_items"], native)
		}
	}
	if !preserved {
		t.Fatal("native history changed")
	}
	var limited struct {
		OutputPreview string
		OutputLimit   struct {
			LimitBytes, ReturnedBytes, OmittedBytes int
			Complete                                bool
		}
		Execution struct {
			ExitCode int
			Reason   string
		}
	}
	if err := json.Unmarshal(modelResults["limited"], &limited); err != nil {
		t.Fatal(err)
	}
	if limited.OutputLimit.LimitBytes != 3072 || limited.OutputLimit.ReturnedBytes > 3072 || limited.OutputLimit.OmittedBytes <= 0 || limited.OutputLimit.Complete || limited.Execution.ExitCode != 0 || limited.Execution.Reason != "completed" || !strings.Contains(limited.OutputPreview, `"hookValue"`) {
		t.Fatalf("bad preview: %+v", limited)
	}
	var expanded struct {
		HookValue string
		ExitCode  int
	}
	if err := json.Unmarshal(modelResults["expanded"], &expanded); err != nil || len(expanded.HookValue) != 6000 || expanded.ExitCode != 99 {
		t.Fatalf("per-call budget lost: %+v %v", expanded, err)
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	seen := 0
	for _, event := range observed {
		if event["type"] == "run_js.started" && event["text"] != "// Execute effective code." {
			t.Fatalf("operation comment lost: %+v", event)
		}
		if event["type"] != "tool.finished" {
			continue
		}
		seen++
		id := fmt.Sprint(event["toolCallId"])
		result, _ := json.Marshal(event["result"])
		if !bytes.Equal(result, modelResults[id]) {
			t.Fatalf("event differs from history: %s vs %s", result, modelResults[id])
		}
		raw, _ := json.Marshal(event["rawResult"])
		if len(raw) < 6000 {
			t.Fatal("raw result truncated")
		}
		_, hookSaved := event["hookResult"]
		if hookSaved != (id == "limited") {
			t.Fatalf("hook original preservation: id=%s saved=%v", id, hookSaved)
		}
	}
	if seen != 2 {
		t.Fatalf("finished records=%d", seen)
	}
}
