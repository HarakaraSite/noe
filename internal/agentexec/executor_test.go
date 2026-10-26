package agentexec

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	modelapi "forge.harakara.site/littleisland/noe/v2/agent/model"
	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type resultHistoryProvider struct {
	mu                sync.Mutex
	calls             int
	toolResultContent string
}

func (p *resultHistoryProvider) Generate(_ context.Context, request modelapi.Request) (modelapi.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.calls == 1 {
		return modelapi.Response{
			Message:      json.RawMessage(`{"role":"assistant","content":null,"tool_calls":[{"id":"call-1","type":"function","function":{"name":"run_js","arguments":"{\"code\":\"export default {answer:42}\"}"}}]}`),
			FinishReason: "tool_calls",
		}, nil
	}
	for _, message := range request.Messages {
		var item struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(message, &item) == nil && item.Role == "tool" {
			p.toolResultContent = item.Content
		}
	}
	return modelapi.Response{Message: json.RawMessage(`{"role":"assistant","content":"done"}`), FinishReason: "stop"}, nil
}

func TestToolSourceRequiresExplicitAgentImport(t *testing.T) {
	models, err := modelapi.NewService(modelapi.Options{})
	if err != nil {
		t.Fatal(err)
	}
	host := core.NewHost(core.Options{Models: models})
	t.Cleanup(host.Close)
	executor := &Executor{Host: host, Profile: stdlib.Profile{}}

	for _, test := range []struct {
		name string
		code string
	}{
		{
			name: "no implicit helper",
			code: `if (typeof run_agent !== "undefined") throw new Error("agent helper was implicitly imported");`,
		},
		{
			name: "module-relative URL in run_js",
			code: `const base = new URL(import.meta.url);
if (base.protocol !== "file:" || !decodeURIComponent(base.pathname).endsWith("/<virtual-entry>.mjs")) {
  throw new Error("virtual entry metadata is not a synthetic cwd file URL");
}
if (!new URL("./asset.json", import.meta.url).pathname.endsWith("/asset.json")) {
  throw new Error("module-relative asset URL failed");
}`,
		},
		{
			name: "explicit import",
			code: `import {run_agent} from "noe:agent"; if (typeof run_agent !== "function") throw new Error("explicit agent import failed");`,
		},
		{
			name: "private task tool modules",
			code: `import {executeToolWithHooks as taskPipeline} from "noe:agent/task";
import {executeToolWithHooks} from "noe:agent/tool-pipeline";
import {runJSTool, executeToolCall} from "noe:agent/run-js";
if (taskPipeline !== executeToolWithHooks) throw new Error("task pipeline compatibility export changed");
if (runJSTool.function.name !== "run_js" || runJSTool.function.parameters.additionalProperties !== false) {
  throw new Error("run_js tool schema changed");
}
const failed = await executeToolCall("unused", "unused", {
  id: "bad-arguments", function: {name: "run_js", arguments: "{"},
});
if (failed.reason !== "failed" || failed.stderr !== "" ||
    failed.diagnostics?.[0]?.stage !== "setup" ||
    failed.diagnostics?.[0]?.message !== "run_js arguments must contain valid JSON") {
  throw new Error("invalid run_js arguments no longer fail before execution");
}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			operation := host.NewOperation(context.Background())
			result, err := executor.Execute(context.Background(), core.Execution{
				Operation: operation, Kind: core.ExecutionTool, Code: test.code,
			})
			if err != nil {
				t.Fatalf("execute tool source: %v", err)
			}
			if result.ExitCode != 0 || result.Reason != "completed" {
				t.Fatalf("tool source failed: %+v", result)
			}
		})
	}
}

func TestRunJSDataAndDiagnosticsReachNextModelRequest(t *testing.T) {
	provider := &resultHistoryProvider{}
	models, err := modelapi.NewService(modelapi.Options{Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	var executor *Executor
	execute := func(ctx context.Context, execution core.Execution) (core.Result, error) {
		result, err := executor.Execute(ctx, execution)
		if err == nil && execution.Kind == core.ExecutionTool {
			// Model the documented case where close fails after data has been
			// captured: the result is failed, but its detached output is retained.
			result.ExitCode = 1
			result.Reason = "failed"
			result.Diagnostics = append(result.Diagnostics, executionresult.Diagnostic{
				Code: "cleanup_error", Stage: "cleanup", Message: "tool cleanup failed",
			})
		}
		return result, err
	}
	host := core.NewHost(core.Options{Models: models, Execute: execute})
	t.Cleanup(host.Close)
	executor = &Executor{Host: host, Profile: stdlib.Profile{}}
	operation := host.NewOperation(context.Background())
	result, err := executor.Execute(context.Background(), core.Execution{
		Operation: operation, Kind: core.ExecutionTask,
		Config:      json.RawMessage(`{"provider":"fixture","model":"fixture-model","name":"result-history","effort":"none","maxTokens":64,"maxSteps":3}`),
		Instruction: "calculate and report",
	})
	if err != nil {
		t.Fatalf("execute managed task: %v", err)
	}
	if result.Reason != "completed" {
		t.Fatalf("managed task failed: %+v", result)
	}
	provider.mu.Lock()
	calls, content := provider.calls, provider.toolResultContent
	provider.mu.Unlock()
	if calls != 2 || content == "" {
		t.Fatalf("model calls=%d, second-request tool result=%q", calls, content)
	}
	var toolResult struct {
		Data        map[string]int               `json:"data"`
		Diagnostics []executionresult.Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(content), &toolResult); err != nil {
		t.Fatalf("decode next-request tool result %q: %v", content, err)
	}
	if toolResult.Data["answer"] != 42 || len(toolResult.Diagnostics) != 1 || toolResult.Diagnostics[0].Code != "cleanup_error" {
		t.Fatalf("next model request lost run_js data/diagnostics: %+v; raw=%s", toolResult, content)
	}
}
