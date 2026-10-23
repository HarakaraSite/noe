//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

type agentExecutionResult struct {
	Stdout             string `json:"stdout"`
	Stderr             string `json:"stderr"`
	ExitCode           int    `json:"exitCode"`
	Reason             string `json:"reason"`
	StdoutOmittedBytes int64  `json:"stdoutOmittedBytes,omitempty"`
	StderrOmittedBytes int64  `json:"stderrOmittedBytes,omitempty"`
}

type agentChatRequest struct {
	RuntimeInfo string             `json:"-"`
	RawMessages []agentChatMessage `json:"-"`
	Model       string             `json:"model"`
	Messages    []agentChatMessage `json:"messages"`
	Tools       []json.RawMessage  `json:"tools"`
	SessionID   string             `json:"session_id"`
	MaxTokens   int                `json:"max_tokens"`
	Reasoning   *struct {
		Effort string `json:"effort"`
	} `json:"reasoning"`
}

func agentEffort(request agentChatRequest) string {
	if request.Reasoning == nil {
		return "auto"
	}
	return request.Reasoning.Effort
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
		// Project runtime status history out of business-transcript fixtures.
		// Dedicated wire tests cover its retention; hooks use the latest notice.
		if len(request.Messages) == 0 {
			t.Error("missing context notification")
			return
		}
		last := request.Messages[len(request.Messages)-1]
		if last.Role != "user" || json.Unmarshal(last.Content, &request.RuntimeInfo) != nil || !strings.HasPrefix(request.RuntimeInfo, "NOE_CONTEXT_STATUS") {
			t.Errorf("missing tagged context tail: %+v", last)
		}
		request.RawMessages = request.Messages
		transcript := make([]agentChatMessage, 0, len(request.Messages))
		for _, message := range request.Messages {
			var text string
			if message.Role == "user" && json.Unmarshal(message.Content, &text) == nil && strings.HasPrefix(text, "NOE_CONTEXT_STATUS\n") {
				continue
			}
			transcript = append(transcript, message)
		}
		request.Messages = transcript
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
	return map[string]string{"NOE_AGENT_RUNTIME_DIR": t.TempDir(), "NOE_OPENROUTER_API_KEY_FILE": key, "NOE_OPENROUTER_BASE_URL": url, "NOE_AGENT_TRACE": "1"}
}

func agentScript(instruction, configOptions string) string {
	text, _ := json.Marshal(instruction)
	return `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",maxTokens:2048,effort:"auto",...` + configOptions + `},` + string(text) + `);
if(result.lastResult.state==="completed")console.log(result.lastResult.answer);else console.log(JSON.stringify(result));`
}

func agentToolResult(t *testing.T, message agentChatMessage) agentExecutionResult {
	t.Helper()
	var text string
	var result agentExecutionResult
	if err := json.Unmarshal(message.Content, &text); err != nil {
		t.Error(err)
		return result
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Error(err)
	}
	return result
}

func TestAgentSpawnedRootKeepsProcessAliveUntilTaskCompletes(t *testing.T) {
	work := t.TempDir()
	scriptFinished := filepath.Join(work, "script-finished")
	eventsPath := filepath.Join(work, "agent-events.jsonl")
	started := make(chan struct{})
	release := make(chan struct{})
	released := false
	var calls atomic.Int32
	server := agentAPIFixture(t, func(ctx context.Context, req agentChatRequest) agentModelReply {
		if calls.Add(1) != 1 {
			t.Errorf("unexpected model request: %+v", req)
			return agentModelReply{Status: 500}
		}
		if agentUser(req) != "spawned task" {
			t.Errorf("unexpected spawned task: %+v", req)
		}
		poll := time.NewTicker(5 * time.Millisecond)
		defer poll.Stop()
		deadline := time.NewTimer(5 * time.Second)
		defer deadline.Stop()
		for {
			if _, err := os.Stat(scriptFinished); err == nil {
				close(started)
				break
			}
			select {
			case <-ctx.Done():
				return agentModelReply{Status: 500}
			case <-deadline.C:
				t.Error("root script did not finish after accepting the spawned task")
				return agentModelReply{Status: 500}
			case <-poll.C:
			}
		}
		select {
		case <-release:
			return agentModelReply{Content: "spawned task completed"}
		case <-ctx.Done():
			t.Error("spawned task was cancelled before its provider response was released")
			return agentModelReply{Status: 500}
		}
	})
	defer server.Close()
	defer func() {
		if !released {
			close(release)
		}
	}()
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	source := `import {spawn_agent} from "noe:agent";import {writeFileSync} from "node:fs";
const spawned=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto",name:"spawned"},"spawned task");
writeFileSync("script-finished",spawned.agentId);console.log("root script finished");`
	done := make(chan cliResult, 1)
	go func() { done <- runCLI(t, work, env, "eval", "--events="+eventsPath, source) }()
	select {
	case <-started:
	case result := <-done:
		t.Fatalf("root process exited before the spawned task completed: %+v", result)
	case <-time.After(5 * time.Second):
		t.Fatal("root script did not reach its end while the spawned task was pending")
	}
	select {
	case result := <-done:
		t.Fatalf("root process exited before the spawned task was released: %+v", result)
	default:
	}
	close(release)
	released = true
	select {
	case result := <-done:
		requireExit(t, result, 0)
		if result.stdout != "root script finished\n" || calls.Load() != 1 {
			t.Fatalf("result=%+v model calls=%d", result, calls.Load())
		}
		spawnedID, err := os.ReadFile(scriptFinished)
		if err != nil || len(spawnedID) == 0 {
			t.Fatalf("spawned agent ID: %q error=%v", spawnedID, err)
		}
		eventData, err := os.ReadFile(eventsPath)
		if err != nil {
			t.Fatal(err)
		}
		counts := make(map[string]int)
		for _, line := range strings.Split(strings.TrimSpace(string(eventData)), "\n") {
			var event struct {
				Type    string `json:"type"`
				AgentID string `json:"agentId"`
			}
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				t.Fatalf("invalid event %q: %v", line, err)
			}
			if event.AgentID == string(spawnedID) {
				counts[event.Type]++
			}
		}
		if counts["agent.completed"] != 1 || counts["agent.cancelled"] != 0 || counts["agent.failed"] != 0 {
			t.Fatalf("spawned task did not complete exactly once: events=%+v", counts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("root process did not finish after the spawned task completed")
	}
}

func TestAgentIdleKeepAliveRootIsReclaimedByTimeout(t *testing.T) {
	var calls atomic.Int32
	server := agentAPIFixture(t, func(context.Context, agentChatRequest) agentModelReply {
		calls.Add(1)
		return agentModelReply{Content: "unexpected model request"}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	source := `import {spawn_agent} from "noe:agent";
await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto",name:"idle"},undefined,{keepAlive:true});
console.log("idle instance started");`
	result := runCLI(t, t.TempDir(), env, "eval", "--timeout", "500ms", source)
	requireExit(t, result, 124)
	if result.stdout != "idle instance started\n" || calls.Load() != 0 {
		t.Fatalf("timeout result=%+v model calls=%d", result, calls.Load())
	}
}

func TestAgentCLIReadsFilesAndPreservesToolHistory(t *testing.T) {
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "numbers.json"), `[10,20,30,40]`)
	var calls atomic.Int32
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		if req.Model != "fixture/model" || req.MaxTokens != 2048 || req.SessionID != "file-cache" || req.Reasoning != nil {
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
	var sessionID atomic.Value
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		call := calls.Add(1)
		if req.SessionID == "" {
			t.Error("automatic session_id is missing")
		}
		if call == 1 {
			sessionID.Store(req.SessionID)
		} else if req.SessionID != sessionID.Load().(string) {
			t.Errorf("session_id changed during repair: %q", req.SessionID)
		}
		switch call {
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
			requireExit(t, result, 0)
			var snapshot struct {
				State      string `json:"state"`
				Reason     string `json:"reason"`
				LastResult struct {
					State string `json:"state"`
					Error string `json:"error"`
				} `json:"lastResult"`
			}
			if err := json.Unmarshal([]byte(result.stdout), &snapshot); err != nil {
				t.Fatalf("failure snapshot: %q: %v", result.stdout, err)
			}
			wantCalls := int32(1)
			if name == "missing-done" {
				wantCalls = 3
			}
			if snapshot.State != "stopped" || snapshot.Reason != "task_failed" || snapshot.LastResult.State != "failed" || calls.Load() != wantCalls {
				t.Fatalf("failure snapshot=%+v calls=%d", snapshot, calls.Load())
			}
			if name == "steps" && !strings.Contains(snapshot.LastResult.Error, "maxSteps") {
				t.Fatalf("missing limit diagnostic: %+v", snapshot.LastResult)
			}
		})
	}
}
