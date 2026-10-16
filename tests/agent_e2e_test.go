//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"noe/internal/agent"
)

type agentToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type agentChatMessage struct {
	Role             string          `json:"role"`
	Content          json.RawMessage `json:"content"`
	ToolCallID       string          `json:"tool_call_id"`
	ToolCalls        []agentToolCall `json:"tool_calls"`
	ReasoningDetails json.RawMessage `json:"reasoning_details"`
}

type agentChatRequest struct {
	Model     string             `json:"model"`
	Messages  []agentChatMessage `json:"messages"`
	Tools     []json.RawMessage  `json:"tools"`
	SessionID string             `json:"session_id"`
	MaxTokens int                `json:"max_tokens"`
}

type agentModelReply struct {
	Deltas       []map[string]any
	Content      string
	FinishReason string
	Status       int
	OmitDone     bool
}

func agentTool(id, code string) map[string]any {
	arguments, _ := json.Marshal(map[string]string{"code": code})
	return map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": "run_js", "arguments": string(arguments)}}
}

func agentAPIFixture(t *testing.T, handle func(context.Context, agentChatRequest) agentModelReply) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer agent-fixture-secret" {
			t.Error("agent fixture authorization missing")
			http.Error(w, "unauthorized", 401)
			return
		}
		var request agentChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", 400)
			return
		}
		var tool struct {
			Type     string `json:"type"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		if len(request.Tools) != 1 || json.Unmarshal(request.Tools[0], &tool) != nil || tool.Type != "function" || tool.Function.Name != "run_js" {
			t.Errorf("model must receive exactly run_js: %s", request.Tools)
		}
		reply := handle(r.Context(), request)
		if reply.Status != 0 {
			http.Error(w, "fixture provider error", reply.Status)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		write := func(delta map[string]any, finish any) {
			chunk := map[string]any{"id": "fixture", "object": "chat.completion.chunk", "created": 1, "model": request.Model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
			body, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", body)
		}
		write(map[string]any{"role": "assistant", "content": nil}, nil)
		for _, delta := range reply.Deltas {
			write(delta, nil)
		}
		if reply.Content != "" {
			write(map[string]any{"content": reply.Content}, nil)
		}
		finish := reply.FinishReason
		if finish == "" {
			finish = "stop"
		}
		write(map[string]any{}, finish)
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120,\"prompt_tokens_details\":{\"cached_tokens\":25},\"cost\":0.0001}}\n\n")
		if !reply.OmitDone {
			fmt.Fprint(w, "data: [DONE]\n\n")
		}
	}))
}

func agentCLIEnv(t *testing.T, url string) map[string]string {
	t.Helper()
	key := filepath.Join(t.TempDir(), "key")
	writeFixture(t, key, "agent-fixture-secret\n")
	return map[string]string{"NOE_OPENROUTER_API_KEY_FILE": key, "NOE_OPENROUTER_BASE_URL": url, "NOE_AGENT_TRACE": "1"}
}

func agentScript(instruction, options string) string {
	text, _ := json.Marshal(instruction)
	return `import {createAgent} from "noe:agent";console.log(await createAgent({model:"fixture/model",maxTokens:2048}).run(` + string(text) + `,` + options + `));`
}

func agentToolResult(t *testing.T, message agentChatMessage) agent.Result {
	t.Helper()
	var text string
	var result agent.Result
	if err := json.Unmarshal(message.Content, &text); err != nil {
		t.Error(err)
		return result
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Error(err)
	}
	return result
}

func TestAgentCLIReadsFilesAndPreservesToolHistory(t *testing.T) {
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "numbers.json"), `[10,20,30,40]`)
	var calls atomic.Int32
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		if req.Model != "fixture/model" || req.MaxTokens != 2048 || req.SessionID != "file-cache" {
			t.Errorf("settings: %+v", req)
		}
		if calls.Add(1) == 1 {
			callA := agentTool("sum", `import {readFile} from "node:fs/promises";const values=JSON.parse(await readFile("numbers.json","utf8"));console.log(JSON.stringify({count:values.length,total:values.reduce((a,b)=>a+b,0),average:values.reduce((a,b)=>a+b,0)/values.length}));globalThis.onlyThisProcess=true;`)
			callB := agentTool("isolation", `console.log(typeof globalThis.onlyThisProcess);`)
			callB["index"] = 1
			return agentModelReply{Deltas: []map[string]any{{"reasoning_details": []any{map[string]any{"type": "reasoning.text", "id": "r1", "index": 0, "text": "reason", "vendor_extension": map[string]any{"keep": true}}}, "tool_calls": []any{callA, callB}}}, FinishReason: "tool_calls"}
		}
		if len(req.Messages) != 5 {
			t.Errorf("history length=%d want 5", len(req.Messages))
			return agentModelReply{Status: 500}
		}
		if req.Messages[2].Role != "assistant" || !strings.Contains(string(req.Messages[2].ReasoningDetails), "vendor_extension") || len(req.Messages[2].ToolCalls) != 2 {
			t.Error("assistant reasoning/tool history was lost")
		}
		if req.Messages[3].ToolCallID != "sum" || req.Messages[4].ToolCallID != "isolation" {
			t.Error("tool IDs/results crossed")
		}
		a, b := agentToolResult(t, req.Messages[3]), agentToolResult(t, req.Messages[4])
		if a.ExitCode != 0 || a.Reason != "completed" || strings.TrimSpace(a.Stdout) != `{"count":4,"total":100,"average":25}` || b.Stdout != "undefined\n" {
			t.Errorf("tool results: %+v %+v", a, b)
		}
		return agentModelReply{Content: "count=4 total=100 average=25"}
	})
	defer server.Close()
	result := runCLI(t, work, agentCLIEnv(t, server.URL), "eval", agentScript("Read numbers.json and calculate count total average", `{cacheKey:"file-cache",maxSteps:3}`))
	requireExit(t, result, 0)
	if result.stdout != "count=4 total=100 average=25\n" || calls.Load() != 2 {
		t.Fatalf("result=%+v calls=%d", result, calls.Load())
	}
	for _, private := range []string{"agent-fixture-secret", "numbers.json", "vendor_extension", "onlyThisProcess"} {
		if strings.Contains(result.stderr, private) {
			t.Fatalf("trace leaked %q: %s", private, result.stderr)
		}
	}
	if !strings.Contains(result.stderr, `"cachedTokens":25`) || !strings.Contains(result.stderr, `"cost":0.0001`) {
		t.Fatalf("trace missing cache/cost usage: %s", result.stderr)
	}
}

func TestAgentCLIFixesFailedJavaScriptAndMalformedArguments(t *testing.T) {
	var calls atomic.Int32
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		switch calls.Add(1) {
		case 1:
			call := agentTool("bad-args", "")
			call["function"] = map[string]any{"name": "run_js", "arguments": "{broken"}
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{call}}}, FinishReason: "tool_calls"}
		case 2:
			got := agentToolResult(t, req.Messages[len(req.Messages)-1])
			if got.Reason != "failed" || got.ExitCode == 0 || got.Stderr == "" {
				t.Errorf("bad arguments hidden: %+v", got)
			}
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("work", `throw Error("intentional failure");`)}}}, FinishReason: "tool_calls"}
		case 3:
			got := agentToolResult(t, req.Messages[len(req.Messages)-1])
			if got.Reason != "failed" || got.ExitCode == 0 || !strings.Contains(got.Stderr, "intentional failure") {
				t.Errorf("JS failure hidden: %+v", got)
			}
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("work", `console.log(100);`)}}}, FinishReason: "tool_calls"}
		default:
			got := agentToolResult(t, req.Messages[len(req.Messages)-1])
			if got.Reason != "completed" || got.Stdout != "100\n" {
				t.Errorf("repair not executed: %+v", got)
			}
			return agentModelReply{Content: "100"}
		}
	})
	defer server.Close()
	result := runCLI(t, t.TempDir(), agentCLIEnv(t, server.URL), "eval", agentScript("Repair failed JS", `{maxSteps:5}`))
	requireExit(t, result, 0)
	if result.stdout != "100\n" || calls.Load() != 4 {
		t.Fatalf("result=%+v calls=%d", result, calls.Load())
	}
	if !strings.Contains(result.stderr, `"reason":"failed"`) || !strings.Contains(result.stderr, `"reason":"completed"`) {
		t.Fatalf("trace missing failed/corrected JS: %s", result.stderr)
	}
}

func TestAgentCLILimitsAndProviderFailures(t *testing.T) {
	for _, name := range []string{"steps", "length", "auth", "missing-done", "empty"} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := agentAPIFixture(t, func(context.Context, agentChatRequest) agentModelReply {
				calls.Add(1)
				switch name {
				case "steps":
					return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("again", `console.log(1);`)}}}, FinishReason: "tool_calls"}
				case "length":
					return agentModelReply{Content: "incomplete", FinishReason: "length"}
				case "auth":
					return agentModelReply{Status: 401}
				case "missing-done":
					return agentModelReply{Content: "incomplete", OmitDone: true}
				default:
					return agentModelReply{}
				}
			})
			defer server.Close()
			result := runCLI(t, t.TempDir(), agentCLIEnv(t, server.URL), "eval", agentScript("must not report incomplete success", `{maxSteps:1}`))
			requireExit(t, result, 1)
			if result.stdout != "" || calls.Load() != 1 {
				t.Fatalf("failure/retry: %+v calls=%d", result, calls.Load())
			}
			if name == "steps" && !strings.Contains(result.stderr, "maxSteps") {
				t.Fatalf("missing limit diagnostic: %s", result.stderr)
			}
		})
	}
}
