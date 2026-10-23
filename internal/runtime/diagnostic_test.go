package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func executeStructuredResult(t *testing.T, source Source) (stdlib.Result, error) {
	t.Helper()
	execution, err := newManagedExecution(context.Background(), Options{}, source)
	if err != nil {
		t.Fatal(err)
	}
	result, runErr := execution.runtime.Run(execution.source)
	return result, errors.Join(runErr, execution.Close())
}

func TestExceptionDetailsSurviveRuntimeClose(t *testing.T) {
	for _, source := range []string{
		`throw Object.assign(new Error("command failed"), {code:128, stdout:"partial output", stderr:"fatal: 日本語"});`,
		`Promise.resolve().then(() => {throw Object.assign(new Error("command failed"), {code:128, stdout:"partial output", stderr:"fatal: 日本語"});});`,
		`setTimeout(() => {throw Object.assign(new Error("command failed"), {code:128, stdout:"partial output", stderr:"fatal: 日本語"});}, 1);`,
	} {
		result, err := executeStructuredResult(t, Source{filepath.Join(t.TempDir(), "details.js"), source})
		if result.Code != 1 || err == nil || len(result.Diagnostics) != 1 {
			t.Fatalf("result=%+v error=%v", result, err)
		}
		diagnostic := result.Diagnostics[0]
		if diagnostic.Code != "JS_EXCEPTION" || diagnostic.Phase != "evaluate" || !strings.Contains(diagnostic.Message, "command failed") {
			t.Errorf("diagnostic classification/phase/message = %+v", diagnostic)
		}
		if !strings.Contains(diagnostic.Source, "details.js") || diagnostic.Line == 0 || diagnostic.Column == 0 {
			t.Errorf("diagnostic location = %q:%d:%d, want details.js with line/column", diagnostic.Source, diagnostic.Line, diagnostic.Column)
		}
		if len(diagnostic.Stack) == 0 {
			t.Errorf("diagnostic stack omitted engine frames: %+v", diagnostic)
		}
		for _, frame := range diagnostic.Stack {
			if frame.Source == "" || !strings.Contains(frame.Source, "details.js") {
				t.Errorf("diagnostic frame source = %q, want details.js when available", frame.Source)
			}
		}
		if got, ok := diagnostic.Exception["code"].(float64); !ok || got != 128 {
			t.Errorf("exception code = %#v, want 128", diagnostic.Exception["code"])
		}
		for key, want := range map[string]string{"stdout": "partial output", "stderr": "fatal: 日本語"} {
			if got, ok := diagnostic.Exception[key].(string); !ok || got != want {
				t.Errorf("exception %s = %#v, want %q", key, diagnostic.Exception[key], want)
			}
		}
	}
}

func TestExceptionDetailsSkipNonScalarAndUnreadableProperties(t *testing.T) {
	source := `const error = new Error("original failure");
const cycle = {}; cycle.self = cycle;
error.code = cycle;
error.stdout = {toJSON(){throw Error("must not serialize");}};
Object.defineProperty(error, "stderr", {get(){throw Error("unreadable property");}});
throw error;`
	result, err := executeStructuredResult(t, Source{filepath.Join(t.TempDir(), "details.js"), source})
	if result.Code != 1 || err == nil || len(result.Diagnostics) != 1 {
		t.Fatalf("result=%+v error=%v", result, err)
	}
	diagnostic := result.Diagnostics[0]
	if !strings.Contains(diagnostic.Message, "original failure") || len(diagnostic.Exception) != 0 {
		t.Fatalf("diagnostic unexpectedly retained non-scalar/unreadable properties: %+v", diagnostic)
	}
}

func TestExceptionRetainsOriginalGoCause(t *testing.T) {
	source := Source{filepath.Join(t.TempDir(), "cause.js"), `fail()`}
	h, err := newManagedExecution(context.Background(), Options{}, source)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	want := errors.New("original host error")
	if err := h.runtime.Bindings().Runtime().Set("fail", func() error { return want }); err != nil {
		t.Fatal(err)
	}
	_, err = h.run()
	if !errors.Is(err, want) {
		t.Fatalf("Go cause lost: %v", err)
	}
}

func TestRunJSCommandFailureDetailsReachModelAndEvents(t *testing.T) {
	for _, call := range []string{"await execFile", "execFileSync"} {
		t.Run(call, func(t *testing.T) { testRunJSCommandFailureDetailsReachModelAndEvents(t, call) })
	}
}

func testRunJSCommandFailureDetailsReachModelAndEvents(t *testing.T, call string) {
	t.Helper()
	executable, _ := json.Marshal(runtimeTestExecutable)
	program, _ := json.Marshal(`console.log("partial output"); console.error("fatal: 日本語"); process.exit(128);`)
	js := "// Inspect a failing command\nconst {execFile, execFileSync} = await import(\"node:child_process\");\n" + call + "(" + string(executable) + ", [\"eval\", " + string(program) + "]);"
	args, _ := json.Marshal(map[string]string{"code": js})
	message, _ := json.Marshal(map[string]any{
		"role": "assistant", "content": nil,
		"tool_calls": []any{map[string]any{"id": "call-failure", "type": "function", "function": map[string]string{"name": "run_js", "arguments": string(args)}}},
	})
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
		return agentAnswer("diagnosed"), nil
	})
	opts := agentTestOptions(p)
	opts.Observe = func(event events.Event) {
		mu.Lock()
		defer mu.Unlock()
		observed = append(observed, event)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "main.js"), `import {run_agent} from "noe:agent"; await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"inspect failure");`}, Options{Agent: opts})
	if code != 0 || err != nil {
		t.Fatalf("code=%d error=%v", code, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("model requests=%d, want 2", len(requests))
	}
	var tool struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
	}
	foundTool := false
	for _, raw := range requests[1].Messages {
		var candidate struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			Content    string `json:"content"`
		}
		if err := json.Unmarshal(raw, &candidate); err != nil {
			t.Fatal(err)
		}
		if candidate.Role == "tool" && candidate.ToolCallID == "call-failure" {
			tool = candidate
			foundTool = true
			break
		}
	}
	if !foundTool {
		t.Fatalf("tool result missing from continued provider history: %+v", requests[1].Messages)
	}
	var result agent.Result
	if err := json.Unmarshal([]byte(tool.Content), &result); err != nil {
		t.Fatal(err)
	}
	if tool.Role != "tool" || tool.ToolCallID != "call-failure" || result.ExitCode != 1 || result.Reason != "failed" {
		t.Fatalf("tool=%+v result=%+v", tool, result)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("model did not receive structured failure diagnostic: %+v", result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Code != "JS_EXCEPTION" || diagnostic.Phase != "evaluate" || !strings.Contains(diagnostic.Message, "exited with 128") {
		t.Errorf("model received wrong diagnostic classification: %+v", diagnostic)
	}
	if got, ok := diagnostic.Exception["code"].(float64); !ok || got != 128 {
		t.Errorf("model exception code = %#v, want 128", diagnostic.Exception["code"])
	}
	for key, want := range map[string]string{"stdout": "partial output\n", "stderr": "fatal: 日本語\n"} {
		if got, ok := diagnostic.Exception[key].(string); !ok || got != want {
			t.Errorf("model exception %s = %#v, want %q", key, diagnostic.Exception[key], want)
		}
	}
	var foundCode, foundError bool
	for _, event := range observed {
		if event["toolCallId"] != "call-failure" || event["runId"] == "" {
			continue
		}
		if event["type"] == "run_js.started" && event["code"] == js {
			foundCode = true
		}
		if event["type"] == "run_js.finished" && event["success"] == false {
			eventDiagnostics, _ := json.Marshal(event["diagnostics"])
			var finished []agent.ExecutionDiagnostic
			if err := json.Unmarshal(eventDiagnostics, &finished); err == nil && len(finished) == 1 && finished[0].Code == diagnostic.Code && finished[0].Exception["stderr"] == diagnostic.Exception["stderr"] {
				foundError = true
			}
		}
	}
	if !foundCode || !foundError {
		t.Fatalf("event recording missing source=%v/error=%v", foundCode, foundError)
	}
}
