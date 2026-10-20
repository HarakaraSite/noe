//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	contextTrimOriginalTask = "Keep this original task while trimming old execution history."
	contextTrimQueuedTask   = "Continue the same work and inspect another batch of results."
	contextTrimFutureTask   = "After that, report the final status of the continuing work."
	contextTrimTaskAnswer   = "continued after trimming"
	contextTrimQueuedAnswer = "queued work continued after trimming"
	contextTrimFutureAnswer = "future queued task completed"
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
	for _, mode := range []string{"run", "eval"} {
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
			} else {
				args = append(args, source)
			}
			env := agentCLIEnv(t, server.URL)
			env["NOE_AGENT_TRACE"] = ""
			result := runCLI(t, work, env, args...)
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
			assertContextStatusHistory(t, requestCopy)

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

func TestContextTrimmingQueuedTaskContinuesConversationAndTrimState(t *testing.T) {
	work := t.TempDir()
	var mu sync.Mutex
	var requests []agentChatRequest
	longCalls := 0
	queuedCalls := 0
	futureCalls := 0
	queueReadyPath := filepath.Join(work, "queue-submissions-ready")
	futureReadyPath := filepath.Join(work, "future-task-ready")
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		mu.Lock()
		requests = append(requests, req)
		user := contextTrimLatestUser(req)
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
			queuedCalls++
			call := queuedCalls
			mu.Unlock()
			if call == 1 {
				if !waitForQueueSubmissions(queueReadyPath) {
					t.Errorf("the later queued task was not accepted while this task was active")
					return agentModelReply{Status: 500}
				}
				if contextTrimHasUser(req, contextTrimFutureTask) {
					t.Errorf("a pending future queue instruction leaked into the active task: %v", contextTrimUsers(req))
				}
			}
			if !contextTrimHasUser(req, contextTrimOriginalTask) || !contextTrimHasUser(req, contextTrimQueuedTask) {
				t.Errorf("queue task lost a protected task instruction: %v", contextTrimUsers(req))
			}
			if contextTrimHasUser(req, contextTrimFutureTask) {
				t.Errorf("a later queued instruction leaked into this task: %v", contextTrimUsers(req))
			}
			if call == 1 && !contextTrimHasAssistantText(req, contextTrimTaskAnswer) {
				t.Errorf("queue task did not receive the previous task's final answer")
			}
			if !strings.Contains(req.RuntimeInfo, "Input budget: 16000 tokens") {
				t.Errorf("queued task lost the inherited input budget: %+v", req)
			}
			if call <= 3 {
				return contextTrimQueuedToolReply(call)
			}
			return agentModelReply{Content: contextTrimQueuedAnswer}
		case contextTrimFutureTask:
			futureCalls++
			call := futureCalls
			mu.Unlock()
			if call == 1 && !waitForQueueSubmissions(futureReadyPath) {
				t.Errorf("future task was not held until its prior run snapshot was observed")
				return agentModelReply{Status: 500}
			}
			if !contextTrimHasUser(req, contextTrimOriginalTask) || !contextTrimHasUser(req, contextTrimQueuedTask) || !contextTrimHasUser(req, contextTrimFutureTask) {
				t.Errorf("future task did not receive the protected queue instructions: %v", contextTrimUsers(req))
			}
			if !contextTrimHasAssistantText(req, contextTrimQueuedAnswer) {
				t.Errorf("future task did not receive the previous queued task's final answer")
			}
			return agentModelReply{Content: contextTrimFutureAnswer}
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
	if len(summary.Runs) != 3 {
		t.Fatalf("queued run snapshots = %+v", summary.Runs)
	}
	first, second, third := summary.Runs[0], summary.Runs[1], summary.Runs[2]
	if first.Answer != contextTrimTaskAnswer || first.Context.ExecutionContext.TrimCount == 0 {
		t.Fatalf("first queued task did not trim and continue: %+v", first)
	}
	if second.Answer != contextTrimQueuedAnswer || second.RunID == first.RunID || second.Context.ExecutionContext.Policy != "auto" || second.Context.ExecutionContext.TrimCount <= first.Context.ExecutionContext.TrimCount || second.Context.ExecutionContext.LastTrim == nil {
		t.Fatalf("next queued task did not continue the conversation and trim state: first=%+v second=%+v", first, second)
	}
	if second.Context.Budget.InputTokens == nil || *second.Context.Budget.InputTokens != 16_000 {
		t.Fatalf("next queued task did not inherit input budget: %+v", second.Context.Budget)
	}
	if third.Answer != contextTrimFutureAnswer || third.RunID == second.RunID || third.Context.ExecutionContext.TrimCount != second.Context.ExecutionContext.TrimCount || third.Context.ExecutionContext.LastTrim == nil {
		t.Fatalf("later queued task did not retain the current conversation state: second=%+v third=%+v", second, third)
	}

	mu.Lock()
	requestCopy := append([]agentChatRequest(nil), requests...)
	gotLongCalls, gotQueuedCalls, gotFutureCalls := longCalls, queuedCalls, futureCalls
	mu.Unlock()
	if gotLongCalls != 5 || gotQueuedCalls != 4 || gotFutureCalls != 1 {
		t.Fatalf("fixture requests first=%d queued=%d future=%d, want 5/4/1", gotLongCalls, gotQueuedCalls, gotFutureCalls)
	}
	assertQueueHistoryAndDroppedTools(t, requestCopy)
	assertContextStatusHistory(t, requestCopy)

	eventPath = filepath.Join(work, eventPath)
	firstEvents := readContextTrimEvents(t, eventPath, first.RunID)
	secondEvents := readContextTrimEvents(t, eventPath, second.RunID)
	thirdEvents := readContextTrimEvents(t, eventPath, third.RunID)
	if len(firstEvents) != first.Context.ExecutionContext.TrimCount || len(secondEvents) == 0 || len(secondEvents) != second.Context.ExecutionContext.TrimCount-first.Context.ExecutionContext.TrimCount || len(thirdEvents) != 0 {
		t.Fatalf("queue run event counts first=%d/%d second=%d/%d third=%d", len(firstEvents), first.Context.ExecutionContext.TrimCount, len(secondEvents), second.Context.ExecutionContext.TrimCount-first.Context.ExecutionContext.TrimCount, len(thirdEvents))
	}
	if got := strings.Count(result.stdout, "context trimmed:"); got != len(firstEvents)+len(secondEvents) {
		t.Fatalf("human trim lines=%d, JSONL trim events across the first two runs=%d", got, len(firstEvents)+len(secondEvents))
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
	return `import {spawn_agent,queue_agent,observe_agent,interrupt_agent} from "noe:agent";import {writeFileSync} from "node:fs";const agent=await spawn_agent(` + config + `,undefined,{keepAlive:true});const waitFor=async(commandId,requireIdle)=>{for(let i=0;i<900;i++){const value=await observe_agent({agentId:agent.agentId});if(value.lastResult?.commandId===commandId&&(!requireIdle||value.state==="idle"))return value;await new Promise(resolve=>setTimeout(resolve,5));}throw Error("queued task timed out: "+commandId);};await queue_agent({agentId:agent.agentId,instruction:` + mustJSONString(contextTrimOriginalTask) + `,commandId:"trim-first"});const first=await waitFor("trim-first",true);await queue_agent({agentId:agent.agentId,instruction:` + mustJSONString(contextTrimQueuedTask) + `,commandId:"trim-second"});await queue_agent({agentId:agent.agentId,instruction:` + mustJSONString(contextTrimFutureTask) + `,commandId:"trim-third"});writeFileSync("queue-submissions-ready","ready");const second=await waitFor("trim-second",false);writeFileSync("future-task-ready","ready");const third=await waitFor("trim-third",true);writeFileSync("context-summary.json",JSON.stringify({agentId:agent.agentId,runs:[{runId:first.lastResult.runId,answer:first.lastResult.answer,context:first.context},{runId:second.lastResult.runId,answer:second.lastResult.answer,context:second.context},{runId:third.lastResult.runId,answer:third.lastResult.answer,context:third.context}]}));await interrupt_agent({agentId:agent.agentId});`
}

func contextTrimQueuedToolReply(call int) agentModelReply {
	id := fmt.Sprintf("queue-trim-block-%d", call)
	code := fmt.Sprintf("// emit queued context block %d\nconsole.log('QUEUE_TRIM_E2E_BLOCK_%d:' + 'x'.repeat(24000));", call, call)
	return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool(id, code)}}}, FinishReason: "tool_calls"}
}

func waitForQueueSubmissions(path string) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return false
}

func contextTrimSavedObservationSource() string {
	return `import {observe_agent} from "noe:agent";import {readFileSync,writeFileSync} from "node:fs";const summary=JSON.parse(readFileSync("context-summary.json","utf8"));const result=[];for(const run of summary.runs){const saved=await observe_agent({logPath:"context-events.jsonl",runId:run.runId});result.push({runId:saved.runId,status:saved.status,context:saved.context,events:saved.events.filter(event=>event.type==="context.trimmed")});}writeFileSync("saved-context-observation.json",JSON.stringify(result));`
}

func contextTrimLatestUser(req agentChatRequest) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			return contextTrimMessageText(req.Messages[i].Content)
		}
	}
	return ""
}

func contextTrimUsers(req agentChatRequest) []string {
	var users []string
	for _, message := range req.Messages {
		if message.Role == "user" {
			users = append(users, contextTrimMessageText(message.Content))
		}
	}
	return users
}

func contextTrimHasUser(req agentChatRequest, want string) bool {
	for _, user := range contextTrimUsers(req) {
		if user == want {
			return true
		}
	}
	return false
}

func contextTrimHasAssistantText(req agentChatRequest, want string) bool {
	for _, message := range req.Messages {
		if message.Role == "assistant" && strings.Contains(contextTrimMessageText(message.Content), want) {
			return true
		}
	}
	return false
}

func contextTrimMessageText(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return string(raw)
}

func contextTrimToolCallIDs(req agentChatRequest) map[string]bool {
	ids := make(map[string]bool)
	for _, message := range req.Messages {
		if message.Role == "assistant" {
			for _, call := range message.ToolCalls {
				ids[call.ID] = true
			}
		}
	}
	return ids
}

func assertQueueHistoryAndDroppedTools(t *testing.T, requests []agentChatRequest) {
	t.Helper()
	seenToolIDs := make(map[string]bool)
	droppedToolIDs := make(map[string]bool)
	queuedRequests := 0
	for i, request := range requests {
		user := contextTrimLatestUser(request)
		switch user {
		case contextTrimOriginalTask:
			if !contextTrimHasUser(request, contextTrimOriginalTask) || contextTrimHasUser(request, contextTrimQueuedTask) || contextTrimHasUser(request, contextTrimFutureTask) {
				t.Fatalf("first task history contains another task instruction at request %d: %v", i, contextTrimUsers(request))
			}
		case contextTrimQueuedTask:
			queuedRequests++
			if !contextTrimHasUser(request, contextTrimOriginalTask) || !contextTrimHasUser(request, contextTrimQueuedTask) || contextTrimHasUser(request, contextTrimFutureTask) {
				t.Fatalf("queued task history lost protected instructions or leaked a future one at request %d: %v", i, contextTrimUsers(request))
			}
		case contextTrimFutureTask:
			if !contextTrimHasUser(request, contextTrimOriginalTask) || !contextTrimHasUser(request, contextTrimQueuedTask) || !contextTrimHasUser(request, contextTrimFutureTask) {
				t.Fatalf("future task history lost protected instructions at request %d: %v", i, contextTrimUsers(request))
			}
		default:
			t.Fatalf("request %d has unexpected latest user instruction %q", i, user)
		}

		currentToolIDs := contextTrimToolCallIDs(request)
		if strings.Contains(request.RuntimeInfo, "Context trimmed:") {
			for id := range seenToolIDs {
				if !currentToolIDs[id] {
					droppedToolIDs[id] = true
				}
			}
		}
		for id := range currentToolIDs {
			if droppedToolIDs[id] {
				t.Fatalf("trimmed tool call %q returned in a later request %d", id, i)
			}
			seenToolIDs[id] = true
		}
	}
	if queuedRequests < 4 {
		t.Fatalf("queued task had %d model requests; want tool exchanges followed by another preparation", queuedRequests)
	}
	if len(droppedToolIDs) == 0 {
		t.Fatal("no previously observed tool call was removed by trimming")
	}
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

func assertContextStatusHistory(t *testing.T, requests []agentChatRequest) {
	t.Helper()
	sent := make(map[string]bool)
	for requestIndex, request := range requests {
		notices, assistants := 0, 0
		for index, message := range request.RawMessages {
			var text string
			isStatus := json.Unmarshal(message.Content, &text) == nil && strings.HasPrefix(text, "NOE_CONTEXT_STATUS\n")
			if isStatus {
				notices++
				if message.Role != "user" {
					t.Fatalf("status instruction remained in request %d", requestIndex)
				}
				if index < len(request.RawMessages)-1 {
					if !sent[text] || request.RawMessages[index+1].Role != "assistant" {
						t.Fatalf("historical status changed or lost its response in request %d", requestIndex)
					}
				} else if text != request.RuntimeInfo {
					t.Fatal("latest status does not match the request tail")
				}
			}
			if message.Role == "assistant" {
				assistants++
			}
		}
		if notices != assistants+1 {
			t.Fatalf("status and response blocks were not retained/trimmed together: request %d, notices=%d assistants=%d", requestIndex, notices, assistants)
		}
		if requestIndex > 0 && !strings.Contains(request.RuntimeInfo, "Context trimmed:") {
			previous := requests[requestIndex-1].RawMessages
			if len(previous) >= len(request.RawMessages) {
				t.Fatal("untrimmed history did not append the next turn")
			}
			for index, message := range previous {
				if !reflect.DeepEqual(message, request.RawMessages[index]) {
					t.Fatalf("sent prefix changed at request %d message %d", requestIndex, index)
				}
			}
		}
		sent[request.RuntimeInfo] = true
	}
}
