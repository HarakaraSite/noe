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
)

func TestExceptionDetailsSurviveRuntimeClose(t *testing.T) {
	for _, source := range []string{
		`throw Object.assign(new Error("command failed"), {code:128, stdout:"partial output", stderr:"fatal: 日本語"});`,
		`Promise.resolve().then(() => {throw Object.assign(new Error("command failed"), {code:128, stdout:"partial output", stderr:"fatal: 日本語"});});`,
		`setTimeout(() => {throw Object.assign(new Error("command failed"), {code:128, stdout:"partial output", stderr:"fatal: 日本語"});}, 1);`,
	} {
		code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "details.js"), source}, Options{})
		if code != 1 || err == nil {
			t.Fatalf("code=%d error=%v", code, err)
		}
		diagnostic := Diagnostic(err)
		for _, want := range []string{"command failed", "details.js", `"code":128`, `"stdout":"partial output"`, `"stderr":"fatal: 日本語"`} {
			if !strings.Contains(diagnostic, want) {
				t.Errorf("missing %q in diagnostic: %s", want, diagnostic)
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
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "details.js"), source}, Options{})
	if code != 1 || err == nil || !strings.Contains(Diagnostic(err), "original failure") || strings.Contains(Diagnostic(err), "exception details:") {
		t.Fatalf("code=%d diagnostic=%s", code, Diagnostic(err))
	}
}

func TestExceptionRetainsOriginalGoCause(t *testing.T) {
	source := Source{filepath.Join(t.TempDir(), "cause.js"), `fail()`}
	h, err := newHost(context.Background(), Options{}, source)
	if err != nil {
		t.Fatal(err)
	}
	defer h.rt.Close()
	want := errors.New("original host error")
	if err := h.rt.Set("fail", func() error { return want }); err != nil {
		t.Fatal(err)
	}
	_, err = h.run()
	if !errors.Is(err, want) {
		t.Fatalf("Go cause lost: %v", err)
	}
}

func TestRunJSCommandFailureDetailsReachModelAndEvents(t *testing.T) {
	executable, _ := json.Marshal(runtimeTestExecutable)
	program, _ := json.Marshal(`console.log("partial output"); console.error("fatal: 日本語"); process.exit(128);`)
	js := "// Inspect a failing command\nconst {execFile} = await import(\"node:child_process\");\nawait execFile(" + string(executable) + ", [\"eval\", " + string(program) + "]);"
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
	last := requests[1].Messages[len(requests[1].Messages)-2]
	if err := json.Unmarshal(last, &tool); err != nil {
		t.Fatal(err)
	}
	var result agent.Result
	if err := json.Unmarshal([]byte(tool.Content), &result); err != nil {
		t.Fatal(err)
	}
	if tool.Role != "tool" || tool.ToolCallID != "call-failure" || result.ExitCode != 1 || result.Reason != "failed" {
		t.Fatalf("tool=%+v result=%+v", tool, result)
	}
	for _, want := range []string{`"code":128`, `"stdout":"partial output\n"`, `"stderr":"fatal: 日本語\n"`} {
		if !strings.Contains(result.Stderr, want) {
			t.Errorf("model did not receive %q: %s", want, result.Stderr)
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
		if event["type"] == "stderr" && event["text"] == result.Stderr {
			foundError = true
		}
	}
	if !foundCode || !foundError {
		t.Fatalf("event recording missing source=%v/error=%v", foundCode, foundError)
	}
}
