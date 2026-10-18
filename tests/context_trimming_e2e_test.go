//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	contextTrimOriginalTask = "Keep this original task while trimming old execution history."
	contextTrimQueuedTask   = "Run a fresh queued task with the inherited context policy."
	contextTrimTaskAnswer   = "continued after trimming"
)

type contextTrimExecution struct {
	Policy                 string `json:"policy"`
	TrimCount              int    `json:"trimCount"`
	BudgetTokens           int64  `json:"budgetTokens"`
	TotalInputBudgetTokens int64  `json:"totalInputBudgetTokens"`
	LastTrim               *struct {
		ID            string `json:"id"`
		BeforeTokens  int64  `json:"beforeTokens"`
		AfterTokens   int64  `json:"afterTokens"`
		RemovedBlocks int    `json:"removedBlocks"`
	} `json:"lastTrim"`
}

type contextTrimRunSnapshot struct {
	ExecutionContext contextTrimExecution `json:"executionContext"`
	Budget           struct {
		InputTokens *int64 `json:"inputTokens"`
	} `json:"budget"`
}

type contextTrimSummary struct {
	AgentID string                  `json:"agentId"`
	Runs    []contextTrimRunSummary `json:"runs"`
}

type contextTrimRunSummary struct {
	RunID   string                 `json:"runId"`
	Answer  string                 `json:"answer"`
	Context contextTrimRunSnapshot `json:"context"`
}

type contextTrimEvent struct {
	Type         string `json:"type"`
	RunID        string `json:"runId"`
	ActivityID   string `json:"activityId"`
	BeforeTokens int64  `json:"beforeTokens"`
	AfterTokens  int64  `json:"afterTokens"`
	Context      struct {
		ExecutionContext contextTrimExecution `json:"executionContext"`
	} `json:"context"`
}

type contextTrimSavedRun struct {
	RunID   string `json:"runId"`
	Status  string `json:"status"`
	Context struct {
		ExecutionContext contextTrimExecution `json:"executionContext"`
	} `json:"context"`
	Events []struct {
		Type         string `json:"type"`
		ActivityID   string `json:"activityId"`
		BeforeTokens int64  `json:"beforeTokens"`
		AfterTokens  int64  `json:"afterTokens"`
	} `json:"events"`
}

func TestContextTrimmingCLISurfacesAndSavedObservation(t *testing.T) {
	for _, mode := range []string{"run", "eval", "repl"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			var mu sync.Mutex
			var requests []agentChatRequest
			longCalls := 0
			server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
				mu.Lock()
				requests = append(requests, req)
				user := agentUser(req)
				switch user {
				case contextTrimOriginalTask:
					longCalls++
					call := longCalls
					mu.Unlock()
					if call <= 4 {
						return contextTrimToolReply(call)
					}
					return agentModelReply{Content: contextTrimTaskAnswer}
				default:
					mu.Unlock()
					t.Errorf("unexpected model task %q", user)
					return agentModelReply{Status: 500}
				}
			})
			defer server.Close()

			source := contextTrimRunSource(false)
			if mode == "run" {
				writeFixture(t, filepath.Join(work, "main.js"), source)
			}
			eventPath := "context-events.jsonl"
			args := []string{mode, "--observe", "--events=" + eventPath}
			if mode == "run" {
				args = append(args, "main.js")
			} else if mode == "eval" {
				args = append(args, source)
			}
			env := agentCLIEnv(t, server.URL)
			env["NOE_AGENT_TRACE"] = ""
			var result cliResult
			if mode == "repl" {
				replSource := strings.TrimPrefix(source, `import {run_agent,observe_agent} from "noe:agent";import {writeFileSync} from "node:fs";`)
				replSource = strings.Replace(replSource, "await run_agent(", "await am.run_agent(", 1)
				replSource = strings.Replace(replSource, "await observe_agent(", "await am.observe_agent(", 1)
				replSource = strings.Replace(replSource, "writeFileSync(", "fs.writeFileSync(", 1)
				input := "await (async()=>{const am=await import(\"noe:agent\");const fs=await import(\"node:fs\");" + replSource + "})();\n"
				result = runCLICommand(t, work, env, strings.NewReader(input), args...)
			} else {
				result = runCLI(t, work, env, args...)
			}
			requireExit(t, result, 0)
			if strings.Count(result.stdout, "context trimmed:") == 0 {
				t.Fatalf("human --observe output omitted trimming for %s:\n%s", mode, result.stdout)
			}

			var summary contextTrimSummary
			summaryPath := filepath.Join(work, "context-summary.json")
			if _, err := os.Stat(summaryPath); err != nil {
				t.Fatalf("live context summary was not written: %v\nstdout:\n%s\nstderr:\n%s", err, result.stdout, result.stderr)
			}
			readJSONFile(t, summaryPath, &summary)
			if summary.AgentID == "" || len(summary.Runs) != 1 {
				t.Fatalf("live context summary: %+v", summary)
			}
			run := summary.Runs[0]
			if run.RunID == "" || run.Answer != contextTrimTaskAnswer || run.Context.ExecutionContext.Policy != "auto" || run.Context.ExecutionContext.TrimCount == 0 {
				t.Fatalf("run did not continue with a trimmed context: %+v", run)
			}
			if run.Context.Budget.InputTokens == nil || *run.Context.Budget.InputTokens != 16_000 {
				t.Fatalf("live context lost its explicit total-input budget: %+v", run.Context.Budget)
			}

			mu.Lock()
			requestCopy := append([]agentChatRequest(nil), requests...)
			mu.Unlock()
			assertCompleteLatestBlockAfterTrim(t, requestCopy, contextTrimOriginalTask)

			eventPath = filepath.Join(work, eventPath)
			trimEvents := readContextTrimEvents(t, eventPath, run.RunID)
			if len(trimEvents) != run.Context.ExecutionContext.TrimCount {
				t.Fatalf("saved context.trimmed events=%d, live trimCount=%d", len(trimEvents), run.Context.ExecutionContext.TrimCount)
			}
			if run.Context.ExecutionContext.LastTrim == nil || len(trimEvents) == 0 || run.Context.ExecutionContext.LastTrim.ID != trimEvents[len(trimEvents)-1].ActivityID {
				t.Fatalf("live lastTrim does not match the final JSONL event: last=%+v events=%+v", run.Context.ExecutionContext.LastTrim, trimEvents)
			}

			writeFixture(t, filepath.Join(work, "observe-saved.js"), contextTrimSavedObservationSource())
			savedResult := runCLI(t, work, nil, "run", "observe-saved.js")
			requireExit(t, savedResult, 0)
			var saved []contextTrimSavedRun
			readJSONFile(t, filepath.Join(work, "saved-context-observation.json"), &saved)
			if len(saved) != 1 || saved[0].RunID != run.RunID || saved[0].Status != "completed" {
				t.Fatalf("saved observe_agent result: %+v", saved)
			}
			if saved[0].Context.ExecutionContext.TrimCount != run.Context.ExecutionContext.TrimCount || len(saved[0].Events) != len(trimEvents) {
				t.Fatalf("saved context disagrees with live snapshot: saved=%+v live=%+v", saved[0], run.Context.ExecutionContext)
			}
			for i, event := range saved[0].Events {
				if event.Type != "context.trimmed" || event.ActivityID != trimEvents[i].ActivityID || event.BeforeTokens != trimEvents[i].BeforeTokens || event.AfterTokens != trimEvents[i].AfterTokens {
					t.Fatalf("saved observe_agent event %d differs from JSONL: saved=%+v JSONL=%+v", i, event, trimEvents[i])
				}
			}
			if got := strings.Count(result.stdout, "context trimmed:"); got != len(trimEvents) {
				t.Fatalf("human trim lines=%d, JSONL trim events=%d", got, len(trimEvents))
			}
		})
	}
}

func TestContextTrimmingQueuedTaskResetsRunSnapshotAndInheritsPolicy(t *testing.T) {
	work := t.TempDir()
	var mu sync.Mutex
	var requests []agentChatRequest
	longCalls := 0
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		mu.Lock()
		requests = append(requests, req)
		user := agentUser(req)
		switch user {
		case contextTrimOriginalTask:
			longCalls++
			call := longCalls
			mu.Unlock()
			if call <= 4 {
				return contextTrimToolReply(call)
			}
			return agentModelReply{Content: contextTrimTaskAnswer}
		case contextTrimQueuedTask:
			mu.Unlock()
			if len(req.Messages) != 2 || !strings.Contains(req.RuntimeInfo, "Older completed history may be discarded") || !strings.Contains(req.RuntimeInfo, "Input budget: 16000 tokens") {
				t.Errorf("queued task did not start fresh with inherited automatic policy and budget: %+v", req)
			}
			return agentModelReply{Content: "queued task completed"}
		default:
			mu.Unlock()
			t.Errorf("unexpected model task %q", user)
			return agentModelReply{Status: 500}
		}
	})
	defer server.Close()

	eventPath := "queue-context-events.jsonl"
	writeFixture(t, filepath.Join(work, "main.js"), contextTrimRunSource(true))
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	result := runCLI(t, work, env, "run", "--observe", "--events="+eventPath, "main.js")
	requireExit(t, result, 0)
	if !strings.Contains(result.stdout, "context trimmed:") {
		t.Fatalf("human --observe output omitted queue run trimming:\n%s", result.stdout)
	}
	var summary contextTrimSummary
	readJSONFile(t, filepath.Join(work, "context-summary.json"), &summary)
	if len(summary.Runs) != 2 {
		t.Fatalf("queued run snapshots = %+v", summary.Runs)
	}
	first, second := summary.Runs[0], summary.Runs[1]
	if first.Answer != contextTrimTaskAnswer || first.Context.ExecutionContext.TrimCount == 0 {
		t.Fatalf("first queued task did not trim and continue: %+v", first)
	}
	if second.Answer != "queued task completed" || second.RunID == first.RunID || second.Context.ExecutionContext.Policy != "auto" || second.Context.ExecutionContext.TrimCount != 0 || second.Context.ExecutionContext.LastTrim != nil {
		t.Fatalf("next queued task did not inherit policy with fresh trim state: %+v", second)
	}
	if second.Context.Budget.InputTokens == nil || *second.Context.Budget.InputTokens != 16_000 {
		t.Fatalf("next queued task did not inherit input budget: %+v", second.Context.Budget)
	}

	mu.Lock()
	requestCopy := append([]agentChatRequest(nil), requests...)
	mu.Unlock()
	assertCompleteLatestBlockAfterTrim(t, requestCopy, contextTrimOriginalTask, contextTrimQueuedTask)

	eventPath = filepath.Join(work, eventPath)
	firstEvents := readContextTrimEvents(t, eventPath, first.RunID)
	secondEvents := readContextTrimEvents(t, eventPath, second.RunID)
	if len(firstEvents) != first.Context.ExecutionContext.TrimCount || len(secondEvents) != 0 {
		t.Fatalf("queue run event counts first=%d/%d second=%d", len(firstEvents), first.Context.ExecutionContext.TrimCount, len(secondEvents))
	}
	if got := strings.Count(result.stdout, "context trimmed:"); got != len(firstEvents) {
		t.Fatalf("human trim lines=%d, first-run JSONL trim events=%d", got, len(firstEvents))
	}
}

func contextTrimToolReply(call int) agentModelReply {
	id := fmt.Sprintf("trim-block-%d", call)
	code := fmt.Sprintf("// emit completed context block %d\nconsole.log('TRIM_E2E_BLOCK_%d:' + 'x'.repeat(24000));", call, call)
	return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool(id, code)}}}, FinishReason: "tool_calls"}
}

func contextTrimRunSource(queued bool) string {
	config := `{provider:"openrouter",model:"fixture/model",name:"context-trimmer",maxTokens:2048,effort:"auto",maxSteps:6,contextBudget:{inputTokens:16000},contextPolicy:{retainRatio:0.1}}`
	if !queued {
		return `import {run_agent,observe_agent} from "noe:agent";import {writeFileSync} from "node:fs";const task=await run_agent(` + config + `,` + mustJSONString(contextTrimOriginalTask) + `);const live=await observe_agent({agentId:task.agentId});if(live.lastResult?.answer!==` + mustJSONString(contextTrimTaskAnswer) + `)throw Error(JSON.stringify(live));writeFileSync("context-summary.json",JSON.stringify({agentId:live.agentId,runs:[{runId:live.lastResult.runId,answer:live.lastResult.answer,context:live.context}]}));`
	}
	return `import {spawn_agent,queue_agent,observe_agent,interrupt_agent} from "noe:agent";import {writeFileSync} from "node:fs";const agent=await spawn_agent(` + config + `,undefined,{keepAlive:true});const waitFor=async commandId=>{for(let i=0;i<900;i++){const value=await observe_agent({agentId:agent.agentId});if(value.state==="idle"&&value.lastResult?.commandId===commandId)return value;await new Promise(resolve=>setTimeout(resolve,5));}throw Error("queued task timed out: "+commandId);};await queue_agent({agentId:agent.agentId,instruction:` + mustJSONString(contextTrimOriginalTask) + `,commandId:"trim-first"});const first=await waitFor("trim-first");await queue_agent({agentId:agent.agentId,instruction:` + mustJSONString(contextTrimQueuedTask) + `,commandId:"trim-second"});const second=await waitFor("trim-second");writeFileSync("context-summary.json",JSON.stringify({agentId:agent.agentId,runs:[{runId:first.lastResult.runId,answer:first.lastResult.answer,context:first.context},{runId:second.lastResult.runId,answer:second.lastResult.answer,context:second.context}]}));await interrupt_agent({agentId:agent.agentId});`
}

func contextTrimSavedObservationSource() string {
	return `import {observe_agent} from "noe:agent";import {readFileSync,writeFileSync} from "node:fs";const summary=JSON.parse(readFileSync("context-summary.json","utf8"));const result=[];for(const run of summary.runs){const saved=await observe_agent({logPath:"context-events.jsonl",runId:run.runId});result.push({runId:saved.runId,status:saved.status,context:saved.context,events:saved.events.filter(event=>event.type==="context.trimmed")});}writeFileSync("saved-context-observation.json",JSON.stringify(result));`
}

func mustJSONString(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v\n%s", path, err, data)
	}
}

func readContextTrimEvents(t *testing.T, path, runID string) []contextTrimEvent {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result []contextTrimEvent
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var event contextTrimEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid event %q: %v", line, err)
		}
		if event.Type == "context.trimmed" && event.RunID == runID {
			result = append(result, event)
		}
	}
	return result
}

func assertCompleteLatestBlockAfterTrim(t *testing.T, requests []agentChatRequest, tasks ...string) {
	t.Helper()
	allowedTasks := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		allowedTasks[task] = true
	}
	requestsByTask := make(map[string]int, len(tasks))
	trimmedRequests := 0
	for _, request := range requests {
		task := agentUser(request)
		if !allowedTasks[task] {
			t.Fatalf("original task was lost or replaced: got %q, want one of %v", task, tasks)
		}
		requestsByTask[task]++
		if !strings.Contains(request.RuntimeInfo, "Context trimmed:") {
			continue
		}
		trimmedRequests++
		var callIDs []string
		results := make(map[string]string)
		for _, message := range request.Messages {
			if message.Role == "assistant" {
				for _, call := range message.ToolCalls {
					callIDs = append(callIDs, call.ID)
				}
			}
			if message.Role == "tool" {
				var content string
				_ = json.Unmarshal(message.Content, &content)
				results[message.ToolCallID] = content
			}
		}
		if len(callIDs) != 1 || !strings.HasPrefix(callIDs[0], "trim-block-") {
			t.Fatalf("trimmed request should retain exactly the newest complete block: calls=%v", callIDs)
		}
		wantCallID := fmt.Sprintf("trim-block-%d", requestsByTask[task]-1)
		if callIDs[0] != wantCallID {
			t.Fatalf("trimmed request retained call %q, want newest completed call %q", callIDs[0], wantCallID)
		}
		content, ok := results[callIDs[0]]
		if !ok {
			t.Fatalf("trimmed request retained tool call %s without its result", callIDs[0])
		}
		if !strings.Contains(content, "TRIM_E2E_BLOCK_"+strings.TrimPrefix(callIDs[0], "trim-block-")+":") {
			t.Fatalf("latest tool result was replaced or truncated: %s", content)
		}
	}
	if trimmedRequests == 0 {
		t.Fatal("no provider request used the post-trim history and notice")
	}
}
