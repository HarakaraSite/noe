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
	"time"
)

func agentUser(req agentChatRequest) string {
	for _, message := range req.Messages {
		if message.Role == "user" {
			var text string
			json.Unmarshal(message.Content, &text)
			return text
		}
	}
	return ""
}

func agentSystem(req agentChatRequest) string {
	var text string
	if len(req.Messages) > 0 {
		json.Unmarshal(req.Messages[0].Content, &text)
	}
	return text
}

type agentTraceRecord struct {
	RunID       string `json:"runId"`
	ParentRunID string `json:"parentRunId"`
	Name        string `json:"name"`
	Model       string `json:"model"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
}

func agentTraceRecords(t *testing.T, text string) []agentTraceRecord {
	t.Helper()
	var result []agentTraceRecord
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var record agentTraceRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Error(err)
			continue
		}
		result = append(result, record)
	}
	return result
}

func TestAgentChildrenRunConcurrentlyAndInheritSettings(t *testing.T) {
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "a.json"), `[10,20]`)
	writeFixture(t, filepath.Join(work, "b.json"), `[30,40]`)
	var children atomic.Int32
	bothStarted := make(chan struct{})
	var sessionMu sync.Mutex
	sessions := make(map[string]string)
	server := agentAPIFixture(t, func(ctx context.Context, req agentChatRequest) agentModelReply {
		instruction := agentUser(req)
		sessionMu.Lock()
		if req.SessionID == "" {
			t.Errorf("%s has no session_id", instruction)
		}
		if previous, ok := sessions[instruction]; ok && previous != req.SessionID {
			t.Errorf("%s session_id changed: %q -> %q", instruction, previous, req.SessionID)
		}
		sessions[instruction] = req.SessionID
		sessionMu.Unlock()
		switch instruction {
		case "parent":
			if req.Model != "fixture/parent" || req.MaxTokens != 2048 || agentEffort(req) != "high" || !strings.Contains(agentSystem(req), "parent-only-setting") {
				t.Errorf("parent settings: %+v", req)
			}
			if len(req.Messages) == 2 {
				code := `import {run_agent} from "noe:agent";
const results=await Promise.all([
run_agent({name:"left",maxSteps:3},"left"),
run_agent({name:"right",model:"fixture/override",maxTokens:321,effort:"low",system:"right-only-setting",cacheKey:"right-cache",maxSteps:3},"right")
]);console.log(JSON.stringify(results.map(result=>result.lastResult.answer)));`
				return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("delegate", code)}}}, FinishReason: "tool_calls"}
			}
			got := agentToolResult(t, req.Messages[len(req.Messages)-1])
			if got.ExitCode != 0 || got.Stdout != "[\"30\",\"70\"]\n" {
				t.Errorf("parent integration: %+v", got)
			}
			return agentModelReply{Content: "100"}
		case "left", "right":
			if strings.Contains(agentSystem(req), "parent-only-setting") {
				t.Error("child inherited parent's system")
			}
			if instruction == "left" && (req.Model != "fixture/parent" || req.MaxTokens != 2048 || agentEffort(req) != "high" || strings.Contains(agentSystem(req), "right-only-setting")) {
				t.Errorf("left defaults: %+v", req)
			}
			if instruction == "right" && (req.Model != "fixture/override" || req.MaxTokens != 321 || agentEffort(req) != "low" || req.SessionID != "right-cache" || !strings.Contains(agentSystem(req), "right-only-setting")) {
				t.Errorf("right overrides: %+v", req)
			}
			if len(req.Messages) == 2 {
				if children.Add(1) == 2 {
					close(bothStarted)
				}
				select {
				case <-bothStarted:
				case <-ctx.Done():
					return agentModelReply{Status: 500}
				}
				file := "a.json"
				if instruction == "right" {
					file = "b.json"
				}
				code := `import {readFile} from "node:fs/promises";console.log(JSON.parse(await readFile("` + file + `","utf8")).reduce((a,b)=>a+b,0));`
				return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("calculate", code)}}}, FinishReason: "tool_calls"}
			}
			got := agentToolResult(t, req.Messages[len(req.Messages)-1])
			answer := "30"
			if instruction == "right" {
				answer = "70"
			}
			if len(req.Messages) != 4 || got.ExitCode != 0 || got.Stdout != answer+"\n" {
				t.Errorf("child history/JS: %+v %+v", req, got)
			}
			return agentModelReply{Content: answer}
		default:
			t.Errorf("unexpected instruction %q", instruction)
			return agentModelReply{Status: 500}
		}
	})
	defer server.Close()
	source := `import {run_agent} from "noe:agent";const result=await run_agent({provider:"openrouter",name:"parent",model:"fixture/parent",maxTokens:2048,effort:"high",system:"parent-only-setting",maxSteps:3},"parent");console.log(result.lastResult.answer);`
	result := runCLI(t, work, agentCLIEnv(t, server.URL), "eval", source)
	requireExit(t, result, 0)
	if result.stdout != "100\n" || children.Load() != 2 {
		t.Fatalf("result=%+v children=%d", result, children.Load())
	}
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if len(sessions) != 3 || sessions["parent"] == sessions["left"] || sessions["parent"] == sessions["right"] || sessions["left"] == sessions["right"] {
		t.Fatalf("parent/child sessions were not isolated: %+v", sessions)
	}
	starts := map[string]agentTraceRecord{}
	jsRuns := map[string]bool{}
	for _, record := range agentTraceRecords(t, result.stderr) {
		if record.Status == "started" {
			starts[record.RunID] = record
		}
		if record.Status == "js_result" {
			jsRuns[record.RunID] = true
		}
	}
	if len(starts) != 3 || len(jsRuns) != 3 {
		t.Fatalf("missing parent/child JS trace: %s", result.stderr)
	}
	parent := ""
	for id, record := range starts {
		if record.ParentRunID == "" {
			if parent != "" {
				t.Fatal("multiple root trace runs")
			}
			parent = id
		}
	}
	if parent == "" {
		t.Fatal("root missing in trace")
	}
	for id, record := range starts {
		if sessions[record.Name] == id {
			t.Errorf("instance session_id must be distinct from task runId: %+v", record)
		}
		if id != parent && record.ParentRunID != parent {
			t.Fatalf("broken parent trace: %+v", record)
		}
	}
}

func TestAgentChildDefaultsBelongToLaunchingRun(t *testing.T) {
	var parents atomic.Int32
	bothParents := make(chan struct{})
	server := agentAPIFixture(t, func(ctx context.Context, req agentChatRequest) agentModelReply {
		instruction := agentUser(req)
		suffix := strings.TrimPrefix(strings.TrimPrefix(instruction, "parent-"), "child-")
		model, tokens := "fixture/a", 111
		effort := "low"
		if suffix == "b" {
			model, tokens = "fixture/b", 222
			effort = "high"
		}
		if instruction == "child-b" {
			tokens = 0      // An explicit zero overrides the inherited nonzero limit.
			effort = "auto" // Explicit auto resets inherited reasoning settings.
		}
		if req.Model != model || req.MaxTokens != tokens || agentEffort(req) != effort {
			t.Errorf("defaults crossed runs: instruction=%q model=%q tokens=%d effort=%q", instruction, req.Model, req.MaxTokens, agentEffort(req))
		}
		if strings.HasPrefix(instruction, "child-") {
			return agentModelReply{Content: "done-" + suffix}
		}
		if len(req.Messages) == 2 {
			if parents.Add(1) == 2 {
				close(bothParents)
			}
			select {
			case <-bothParents:
			case <-ctx.Done():
				return agentModelReply{Status: 500}
			}
			config := "{}"
			if suffix == "b" {
				config = `{maxTokens:0,effort:"auto"}`
			}
			code := `import {run_agent} from "noe:agent";const result=await run_agent(` + config + `,"child-` + suffix + `");console.log(result.lastResult.answer);`
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("child", code)}}}, FinishReason: "tool_calls"}
		}
		got := agentToolResult(t, req.Messages[len(req.Messages)-1])
		if got.Stdout != "done-"+suffix+"\n" {
			t.Errorf("child failed: %+v", got)
		}
		return agentModelReply{Content: "done-" + suffix}
	})
	defer server.Close()
	result := runCLI(t, t.TempDir(), agentCLIEnv(t, server.URL), "eval", `import {run_agent} from "noe:agent";
console.log(JSON.stringify(await Promise.all([
run_agent({provider:"openrouter",model:"fixture/a",maxTokens:111,effort:"low",maxSteps:3},"parent-a"),
run_agent({provider:"openrouter",model:"fixture/b",maxTokens:222,effort:"high",maxSteps:3},"parent-b")]).then(results=>results.map(result=>result.lastResult.answer))));`)
	requireExit(t, result, 0)
	if result.stdout != "[\"done-a\",\"done-b\"]\n" {
		t.Fatalf("result=%+v", result)
	}
}

func TestAgentFailedRunDoesNotCancelIndependentRun(t *testing.T) {
	work := t.TempDir()
	server := agentAPIFixture(t, func(ctx context.Context, req agentChatRequest) agentModelReply {
		if agentUser(req) == "bad" {
			return agentModelReply{Status: 401}
		}
		timer := time.NewTicker(5 * time.Millisecond)
		defer timer.Stop()
		for {
			if _, err := os.Stat(filepath.Join(work, "bad-finished")); err == nil {
				return agentModelReply{Content: "good completed"}
			}
			select {
			case <-timer.C:
			case <-ctx.Done():
				t.Error("independent run was cancelled")
				return agentModelReply{Status: 500}
			}
		}
	})
	defer server.Close()
	result := runCLI(t, work, agentCLIEnv(t, server.URL), "eval", `import {run_agent} from "noe:agent";import {writeFileSync} from "node:fs";
const config={provider:"openrouter",model:"fixture/model",effort:"auto"};
const bad=run_agent(config,"bad").then(snapshot=>{if(snapshot.lastResult.state!=="failed")throw Error(JSON.stringify(snapshot));writeFileSync("bad-finished","1");return "failed"});
const good=run_agent(config,"good").then(snapshot=>snapshot.lastResult.answer);console.log(JSON.stringify(await Promise.all([bad,good])));`)
	requireExit(t, result, 0)
	if result.stdout != "[\"failed\",\"good completed\"]\n" {
		t.Fatalf("result=%+v", result)
	}
}

func TestAgentSpawnedChildSurvivesToolExitAndIsReclaimedWithParent(t *testing.T) {
	work := t.TempDir()
	cancelled := make(chan struct{})
	var once sync.Once
	server := agentAPIFixture(t, func(ctx context.Context, req agentChatRequest) agentModelReply {
		if agentUser(req) == "blocked child" {
			if err := os.WriteFile(filepath.Join(work, "child-started"), []byte("1"), 0600); err != nil {
				t.Error(err)
			}
			<-ctx.Done()
			once.Do(func() { close(cancelled) })
			return agentModelReply{Status: 500}
		}
		if len(req.Messages) == 2 {
			code := `import {spawn_agent} from "noe:agent";
const {agentId}=await spawn_agent({name:"abandoned"},"blocked child",{keepAlive:true});
const {writeFileSync,existsSync}=await import("node:fs");writeFileSync("child.json",JSON.stringify({agentId}));
while(!existsSync("child-started")) await new Promise(r=>setTimeout(r,5));
process.exit(9);`
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("exit", code)}}}, FinishReason: "tool_calls"}
		}
		got := agentToolResult(t, req.Messages[len(req.Messages)-1])
		if got.ExitCode == 9 && got.Reason == "failed" {
			code := `import {observe_agent,queue_agent} from "noe:agent";
const {readFileSync}=await import("node:fs");
const {agentId}=JSON.parse(readFileSync("child.json","utf8"));
const snapshot=await observe_agent({agentId});
if(snapshot.state!=="running"||!snapshot.currentRunId)throw Error(JSON.stringify(snapshot));
const queued=await queue_agent({agentId,instruction:"queued child task"});
console.log(JSON.stringify({state:snapshot.state,queued:queued.status}));`
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("observe", code)}}}, FinishReason: "tool_calls"}
		}
		if got.ExitCode != 0 || got.Stdout != `{"state":"running","queued":"accepted"}`+"\n" {
			t.Errorf("child did not survive the tool process or accept a queued task: %+v", got)
		}
		return agentModelReply{Content: "parent finished"}
	})
	defer server.Close()
	result := runCLI(t, work, agentCLIEnv(t, server.URL), "eval", agentScript("parent exit", `{maxSteps:3}`))
	requireExit(t, result, 0)
	if result.stdout != "parent finished\n" {
		t.Fatalf("result=%+v", result)
	}
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("parent completion left spawned child running")
	}
	found := false
	for _, record := range agentTraceRecords(t, result.stderr) {
		if record.Name == "abandoned" && record.Status == "cancelled" && record.ParentRunID != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("spawned child cancellation missing: %s", result.stderr)
	}
}
