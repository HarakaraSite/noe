package agent_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"forge.harakara.site/littleisland/noe/v2/internal/runtime"
)

type taskModuleProvider func(context.Context, provider.Request) (provider.Response, error)

func (f taskModuleProvider) Generate(ctx context.Context, request provider.Request) (provider.Response, error) {
	return f(ctx, request)
}

func TestPrivateTaskModulePreservesAssistantHistoryAndReportsCompletion(t *testing.T) {
	var mu sync.Mutex
	var requests []provider.Request
	p := taskModuleProvider(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		requests = append(requests, req)
		call := len(requests)
		mu.Unlock()
		if call == 1 {
			return provider.Response{
				Message:      json.RawMessage(`{"role":"assistant","content":"Checking","reasoning":"keep the full reasoning object","vendor_metadata":{"mode":"fixture"},"tool_calls":[{"id":"call-1","type":"function","function":{"name":"run_js","arguments":"not-json"}}]}`),
				FinishReason: "tool_calls",
			}, nil
		}
		message, _ := json.Marshal(map[string]string{"role": "assistant", "content": "task answer"})
		return provider.Response{Message: message, FinishReason: "stop"}, nil
	})
	var muEvents sync.Mutex
	var observed []events.Event
	var out strings.Builder
	source := `import {executeTask} from "noe:agent/task";
console.log(await executeTask({provider:"openrouter",model:"fixture/model",name:"managed",effort:"auto"},"task instruction"));`
	code, err := runtime.Execute(context.Background(), runtime.Source{Name: filepath.Join(t.TempDir(), "main.js"), Code: source}, runtime.Options{
		Stdout: &out,
		Agent: &agent.Options{Provider: p, Observe: func(event events.Event) {
			muEvents.Lock()
			observed = append(observed, event)
			muEvents.Unlock()
		}},
	})
	if code != 0 || err != nil || out.String() != "task answer\n" {
		t.Fatalf("code=%d error=%v stdout=%q", code, err, out.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || len(requests[1].Messages) != 6 {
		t.Fatalf("model calls/history lengths = %d/%d, want 2/6 including the retained status and new context tail", len(requests), func() int {
			if len(requests) < 2 {
				return 0
			}
			return len(requests[1].Messages)
		}())
	}
	for index, message := range requests[0].Messages {
		if string(message) != string(requests[1].Messages[index]) {
			t.Fatalf("sent prefix changed at message %d: %s -> %s", index, message, requests[1].Messages[index])
		}
	}
	var contextTail struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(requests[1].Messages[5], &contextTail); err != nil || contextTail.Role != "user" || !strings.HasPrefix(contextTail.Content, "NOE_CONTEXT_STATUS\n") {
		t.Fatalf("context tail missing: %s (%v)", requests[1].Messages[5], err)
	}
	var assistant struct {
		Reasoning      string         `json:"reasoning"`
		VendorMetadata map[string]any `json:"vendor_metadata"`
	}
	if err := json.Unmarshal(requests[1].Messages[3], &assistant); err != nil {
		t.Fatal(err)
	}
	if assistant.Reasoning != "keep the full reasoning object" || assistant.VendorMetadata["mode"] != "fixture" {
		t.Fatalf("assistant history was reduced: %s", requests[1].Messages[3])
	}
	var toolResult struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
	}
	if err := json.Unmarshal(requests[1].Messages[4], &toolResult); err != nil {
		t.Fatal(err)
	}
	if toolResult.Role != "tool" || toolResult.ToolCallID != "call-1" || !strings.Contains(toolResult.Content, "run_js arguments must contain valid JSON") {
		t.Fatalf("tool result was not returned to the model: %+v", toolResult)
	}
	muEvents.Lock()
	defer muEvents.Unlock()
	completed := 0
	for _, event := range observed {
		if event["type"] == "agent.completed" {
			completed++
		}
	}
	if completed != 1 {
		t.Fatalf("authenticated completion events = %d, want 1", completed)
	}
}

func TestPrivateTaskModuleReportsFailureBeforeRejecting(t *testing.T) {
	p := taskModuleProvider(func(context.Context, provider.Request) (provider.Response, error) {
		message, _ := json.Marshal(map[string]string{"role": "assistant", "content": ""})
		return provider.Response{Message: message, FinishReason: "stop"}, nil
	})
	var mu sync.Mutex
	var observed []events.Event
	var out strings.Builder
	source := `import {executeTask} from "noe:agent/task";
try { await executeTask({provider:"openrouter",model:"fixture/model",effort:"auto"},"task instruction"); }
catch (error) { console.log(error.message); }`
	code, err := runtime.Execute(context.Background(), runtime.Source{Name: filepath.Join(t.TempDir(), "main.js"), Code: source}, runtime.Options{
		Stdout: &out,
		Agent: &agent.Options{Provider: p, Observe: func(event events.Event) {
			mu.Lock()
			observed = append(observed, event)
			mu.Unlock()
		}},
	})
	if code != 0 || err != nil || out.String() != "model returned an empty final answer\n" {
		t.Fatalf("code=%d error=%v stdout=%q", code, err, out.String())
	}
	mu.Lock()
	defer mu.Unlock()
	failed := 0
	for _, event := range observed {
		if event["type"] == "agent.failed" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("authenticated failure events = %d, want 1", failed)
	}
}
