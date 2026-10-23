package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestRunJSResultsReachHooksEventsAndNativeProviderHistory(t *testing.T) {
	cwd := t.TempDir()
	writePhaseHook(t, cwd, "result.mjs", `export function afterTool(event) {
 if(event.toolCallId === "returned") return {result:{...event.result,data:{...event.result.data,viaHook:true}}};
}`)
	sources := []struct{ id, code string }{
		{"returned", `// Return data using an explicit import and a local helper-name binding.
import {observe_agent} from "noe:agent";
const run_agent=42;export default {answer:run_agent,imported:typeof observe_agent};`},
		{"null", `// Return an explicit null value.
export default null;`},
		{"no-default", `// Write ordinary output without returning data.
console.log("actual output");`},
		{"failure", `// Produce a runtime diagnostic without writing stderr.
throw new Error("vm detail");`},
	}
	var calls []any
	for _, source := range sources {
		arguments, err := json.Marshal(map[string]string{"code": source.code})
		if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, map[string]any{"id": source.id, "type": "function", "function": map[string]string{"name": "run_js", "arguments": string(arguments)}})
	}
	message, err := json.Marshal(map[string]any{
		"role": "assistant", "content": nil, "tool_calls": calls,
		"reasoning_details": []any{map[string]any{"type": "reasoning.text", "text": "keep native reasoning"}},
		"vendor":            map[string]bool{"preserve": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []provider.Request
	var observed []events.Event
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, req)
		if len(requests) == 1 {
			return provider.Response{Message: message, FinishReason: "tool_calls"}, nil
		}
		return agentAnswer("done"), nil
	})
	options := agentTestOptions(p)
	options.Observe = func(event events.Event) {
		mu.Lock()
		defer mu.Unlock()
		observed = append(observed, event)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(cwd, "main.mjs"), `import {run_agent} from "noe:agent";
await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",hooks:["./result.mjs"]},"return data and a diagnostic");`}, Options{Cwd: cwd, Agent: options})
	if code != 0 || err != nil {
		t.Fatalf("task code=%d diagnostic=%s", code, Diagnostic(err))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider calls=%d", len(requests))
	}
	results := make(map[string]agent.Result)
	keptNative := false
	for _, raw := range requests[1].Messages {
		var message struct {
			Role      string          `json:"role"`
			ID        string          `json:"tool_call_id"`
			Content   string          `json:"content"`
			Vendor    map[string]bool `json:"vendor"`
			Reasoning json.RawMessage `json:"reasoning_details"`
		}
		if err := json.Unmarshal(raw, &message); err != nil {
			t.Fatal(err)
		}
		if message.Role == "assistant" && message.Vendor["preserve"] && bytes.Contains(message.Reasoning, []byte("keep native reasoning")) {
			keptNative = true
		}
		if message.Role == "tool" {
			var result agent.Result
			if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
				t.Fatal(err)
			}
			results[message.ID] = result
		}
	}
	if !keptNative || len(results) != len(sources) {
		t.Fatalf("native=%v results=%+v", keptNative, results)
	}
	returned := results["returned"]
	var data struct {
		Answer   int
		Imported string
		ViaHook  bool
	}
	if err := json.Unmarshal(returned.Data, &data); err != nil || data.Answer != 42 || data.Imported != "function" || !data.ViaHook || returned.Reason != "completed" {
		t.Fatalf("hook-adjusted result=%+v data=%+v error=%v", returned, data, err)
	}
	if string(results["null"].Data) != "null" || results["no-default"].Data != nil || results["no-default"].Stdout != "actual output\n" {
		t.Fatalf("absence/null/output: %+v", results)
	}
	failure := results["failure"]
	if failure.Stderr != "" || failure.Data != nil || failure.Reason != "failed" || len(failure.Diagnostics) != 1 || !strings.Contains(failure.Diagnostics[0].Message, "vm detail") {
		t.Fatalf("diagnostic lost or mixed with output: %+v", failure)
	}
	foundRaw := false
	for _, event := range observed {
		if event["type"] != "tool.finished" || event["toolCallId"] != "returned" {
			continue
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		var values struct {
			RawResult agent.Result
			Result    agent.Result
		}
		if err := json.Unmarshal(encoded, &values); err != nil {
			t.Fatal(err)
		}
		foundRaw = !bytes.Contains(values.RawResult.Data, []byte("viaHook")) && bytes.Contains(values.Result.Data, []byte("viaHook"))
	}
	if !foundRaw {
		t.Fatal("raw and hook-adjusted data not distinguished in events")
	}
}
