package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

func writePhaseHook(t *testing.T, cwd, name, code string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(cwd, name), []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
}

// Check reachability rather than a particular redundant-edge representation.
func assertHookCause(t *testing.T, observed []events.Event, from, cause string) {
	t.Helper()
	graph := map[string][]string{}
	for _, event := range observed {
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		var node struct {
			ActivityID string   `json:"activityId"`
			CausedBy   []string `json:"causedBy"`
		}
		if err := json.Unmarshal(data, &node); err != nil {
			t.Fatal(err)
		}
		graph[node.ActivityID] = append(graph[node.ActivityID], node.CausedBy...)
	}
	seen := map[string]bool{}
	var reaches func(string) bool
	reaches = func(id string) bool {
		if id == "" || seen[id] {
			return false
		}
		if id == cause {
			return true
		}
		seen[id] = true
		for _, parent := range graph[id] {
			if reaches(parent) {
				return true
			}
		}
		return false
	}
	if from == "" || cause == "" || !reaches(from) {
		t.Fatalf("missing causal path %q -> %q (graph=%v)", from, cause, graph)
	}
}

func hookActivity(event events.Event) string { id, _ := event["activityId"].(string); return id }

func TestHooksLogicalStepSelfSteerAndFinalCandidate(t *testing.T) {
	cwd := t.TempDir()
	logPath := filepath.Join(cwd, "hooks.jsonl")
	common := fmt.Sprintf(`import {existsSync,readFileSync,writeFileSync} from "node:fs";
const path=%q;
function record(event) {
 if(!event.invocationId || !event.activityId || !event.groupId || !event.runId || !event.modelStepId || !event.hookId || event.role!=="root" || event.agentDepth!==0) throw Error("invalid hook metadata: "+JSON.stringify(event));
 const item={phase:event.phase,step:event.modelStepId,invocation:event.invocationId,group:event.groupId};
 writeFileSync(path,(existsSync(path)?readFileSync(path,"utf8"):"")+JSON.stringify(item)+"\n");
}
`, logPath)
	writePhaseHook(t, cwd, "first.mjs", common+`
let before=0,after=0;
export async function beforeModelRequest(event,api) {
 record(event);
 const execution=await api.execution.get();
 if(execution.activityId!==event.activityId || JSON.stringify(execution.causedBy)!==JSON.stringify(event.causedBy) || execution.origin.kind!=="hook" || execution.origin.hookId!==event.hookId) throw Error("execution cause mismatch: "+JSON.stringify(execution));
 if(++before===1) await api.agents.steer_agent({runId:event.runId,instruction:"before correction",commandId:"before-hook-steer"});
}
export async function afterModelResponse(event,api) {
 record(event);
 if(event.requestId!==event.response.requestId) throw Error("response request mismatch");
 if(++after===1) await api.agents.steer_agent({runId:event.runId,instruction:"after correction",commandId:"after-hook-steer"});
}
`)
	writePhaseHook(t, cwd, "second.mjs", common+`export async function beforeModelRequest(event) { record(event); }`)
	var mu sync.Mutex
	var requests []provider.Request
	candidate := json.RawMessage(`{"role":"assistant","content":"candidate","reasoning_details":[{"type":"reasoning.text","text":"preserve candidate reasoning"}],"vendor":{"keep":true}}`)
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		requests = append(requests, req)
		n := len(requests)
		mu.Unlock()
		if n == 1 {
			return provider.Response{Message: candidate, FinishReason: "stop"}, nil
		}
		return agentAnswer("corrected final"), nil
	})
	var eventMu sync.Mutex
	var observed []events.Event
	settings := agentTestOptions(p)
	settings.Observe = func(event events.Event) { eventMu.Lock(); observed = append(observed, event); eventMu.Unlock() }
	source := `import {run_agent} from "noe:agent";
console.log(JSON.stringify(await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2,hooks:["./first.mjs","./second.mjs"]},"initial task")));`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: settings, Stdout: &out, Stderr: &stderr})
	var result struct {
		AgentID    string `json:"agentId"`
		LastResult struct {
			State  string `json:"state"`
			Answer string `json:"answer"`
		} `json:"lastResult"`
		Execution struct {
			Counters struct {
				HookCalls int `json:"hookCalls"`
			} `json:"counters"`
		} `json:"execution"`
	}
	decode := json.Unmarshal(out.Bytes(), &result)
	if code != 0 || err != nil || decode != nil || result.LastResult.State != "completed" || result.LastResult.Answer != "corrected final" {
		t.Fatalf("code=%d err=%v decode=%v result=%+v stdout=%q stderr=%q", code, err, decode, result, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("self steer duplicated model judgment: %d requests", len(requests))
	}
	countText := func(req provider.Request, text string) int {
		n := 0
		for _, raw := range req.Messages {
			var item struct {
				Role    string
				Content string
			}
			if json.Unmarshal(raw, &item) == nil && item.Role == "user" && item.Content == text {
				n++
			}
		}
		return n
	}
	if countText(requests[0], "before correction") != 1 || countText(requests[1], "before correction") != 1 || countText(requests[1], "after correction") != 1 {
		t.Fatalf("steer lost/duplicated: first=%s second=%s", requests[0].Messages, requests[1].Messages)
	}
	retained := false
	for _, raw := range requests[1].Messages {
		if strings.Contains(string(raw), "preserve candidate reasoning") && strings.Contains(string(raw), `"vendor"`) {
			retained = true
		}
	}
	if !retained {
		t.Fatalf("final-commit rejection lost full candidate: %s", requests[1].Messages)
	}
	lines, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	steps := map[string]int{}
	invocations := map[string]bool{}
	afterCount := 0
	for _, line := range bytes.Split(bytes.TrimSpace(lines), []byte("\n")) {
		var entry struct {
			Phase      string
			Step       string
			Invocation string
			Group      string
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		if entry.Group != result.AgentID || invocations[entry.Invocation] {
			t.Fatalf("wrong group/repeated hook invocation: %s", line)
		}
		invocations[entry.Invocation] = true
		if entry.Phase == "beforeModelRequest" {
			steps[entry.Step]++
		} else if entry.Phase == "afterModelResponse" {
			afterCount++
		}
	}
	if len(steps) != 2 || afterCount != 2 || len(invocations) != 6 || result.Execution.Counters.HookCalls != 6 {
		t.Fatalf("logical step/hook counts: before=%v after=%d invocations=%d counters=%+v", steps, afterCount, len(invocations), result.Execution.Counters)
	}
	for step, n := range steps {
		if n != 2 {
			t.Fatalf("step %s executed %d before hooks, expected two modules once", step, n)
		}
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	starts, ends := map[string]bool{}, map[string]bool{}
	for _, event := range observed {
		id, _ := event["invocationId"].(string)
		switch event["type"] {
		case "hook.started":
			starts[id] = true
		case "hook.finished":
			ends[id] = true
		case "hook.failed", "hook.cancelled":
			t.Fatalf("unexpected hook terminal: %+v", event)
		}
	}
	if len(starts) != 6 || len(ends) != 6 {
		t.Fatalf("hook lifecycle starts=%v ends=%v", starts, ends)
	}
	for id := range starts {
		if !ends[id] {
			t.Fatalf("hook %s lacks terminal", id)
		}
	}
	var modelIDs, stepIDs []string
	hooksByPhase := map[string][]string{}
	commands := map[string]string{}
	for _, event := range observed {
		switch event["type"] {
		case "model.started":
			modelIDs = append(modelIDs, hookActivity(event))
		case "model.step.started":
			stepIDs = append(stepIDs, hookActivity(event))
		case "hook.started":
			phase, _ := event["phase"].(string)
			hooksByPhase[phase] = append(hooksByPhase[phase], hookActivity(event))
		case "command.accepted":
			id, _ := event["commandId"].(string)
			commands[id] = hookActivity(event)
		}
	}
	if len(modelIDs) != 2 || len(stepIDs) != 2 {
		t.Fatalf("model/step activities: %v %v", modelIDs, stepIDs)
	}
	assertHookCause(t, observed, commands["before-hook-steer"], hooksByPhase["beforeModelRequest"][0])
	assertHookCause(t, observed, commands["after-hook-steer"], hooksByPhase["afterModelResponse"][0])
	assertHookCause(t, observed, modelIDs[0], commands["before-hook-steer"])
	assertHookCause(t, observed, stepIDs[1], modelIDs[0])
	assertHookCause(t, observed, stepIDs[1], commands["after-hook-steer"])
}

func hookToolReply(codes map[string]string, order ...string) provider.Response {
	calls := make([]any, 0, len(order))
	for _, id := range order {
		args, _ := json.Marshal(map[string]string{"code": codes[id]})
		calls = append(calls, map[string]any{"id": id, "type": "function", "function": map[string]string{"name": "run_js", "arguments": string(args)}})
	}
	message, _ := json.Marshal(map[string]any{"role": "assistant", "content": "running tools", "reasoning_details": []any{map[string]string{"type": "reasoning.text", "text": "retain tool reasoning"}}, "tool_calls": calls})
	return provider.Response{Message: message, FinishReason: "tool_calls"}
}

func TestHooksParallelToolsPreserveRawFactsAndCallOrder(t *testing.T) {
	cwd := t.TempDir()
	gate := filepath.Join(cwd, "fast-complete")
	hook := fmt.Sprintf(`import {writeFileSync} from "node:fs";
export async function beforeTool(event) {
 const slow=event.toolCallId==="slow";
 const code="// Execute patched "+event.toolCallId+" tool.\n"+
  'import {existsSync,writeFileSync} from "node:fs";\n'+
  (slow?'while(!existsSync('+JSON.stringify(%q)+')) await new Promise(resolve=>setTimeout(resolve,5));\n':'')+
  'console.log("raw '+event.toolCallId+'");';
 return {arguments:{code}};
}
export async function afterTool(event) {
 if(event.result.exitCode!==0 || event.result.reason!=="completed") throw Error("actual tool failed: "+JSON.stringify(event));
 if(event.toolCallId==="fast") writeFileSync(%q,"done");
 return {result:"effective "+event.toolCallId};
}`, gate, gate)
	writePhaseHook(t, cwd, "tools.mjs", hook)
	var mu sync.Mutex
	var requests []provider.Request
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		requests = append(requests, req)
		n := len(requests)
		mu.Unlock()
		if n == 1 {
			return hookToolReply(map[string]string{"slow": "// Original slow call.\nconsole.log('unpatched');", "fast": "// Original fast call.\nconsole.log('unpatched');"}, "slow", "fast"), nil
		}
		return agentAnswer("parallel done"), nil
	})
	var eventMu sync.Mutex
	var observed []events.Event
	settings := agentTestOptions(p)
	settings.Observe = func(event events.Event) { eventMu.Lock(); observed = append(observed, event); eventMu.Unlock() }
	source := `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2,hooks:["./tools.mjs"]},"parallel tools");
if(result.lastResult?.state!=="completed") throw Error(JSON.stringify(result));
console.log(result.lastResult.answer);`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: settings, Stdout: &out, Stderr: &stderr})
	if code != 0 || err != nil || out.String() != "parallel done\n" {
		t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider calls=%d", len(requests))
	}
	var ids, results []string
	for _, raw := range requests[1].Messages {
		var message struct {
			Role       string
			ToolCallID string `json:"tool_call_id"`
			Content    string
		}
		if json.Unmarshal(raw, &message) == nil && message.Role == "tool" {
			ids = append(ids, message.ToolCallID)
			var value string
			if err := json.Unmarshal([]byte(message.Content), &value); err != nil {
				t.Fatalf("model result was not hook replacement: %s", raw)
			}
			results = append(results, value)
		}
	}
	if strings.Join(ids, ",") != "slow,fast" || strings.Join(results, ",") != "effective slow,effective fast" {
		t.Fatalf("tool history order/results: ids=%v results=%v", ids, results)
	}
	preserved := false
	for _, raw := range requests[1].Messages {
		if strings.Contains(string(raw), "retain tool reasoning") && strings.Contains(string(raw), "Original slow call") {
			preserved = true
		}
	}
	if !preserved {
		t.Fatalf("provider's original assistant/call was changed: %s", requests[1].Messages)
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	var finished []string
	recorded := map[string]bool{}
	for _, event := range observed {
		if event["type"] == "run_js.finished" {
			finished = append(finished, event["toolCallId"].(string))
		}
		if event["type"] == "run_js.started" && !strings.Contains(fmt.Sprint(event["code"]), "Execute patched") {
			t.Fatalf("execution source not patched: %+v", event)
		}
		if event["type"] != "tool.finished" {
			continue
		}
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		var record struct {
			ToolCallID   string `json:"toolCallId"`
			RawArguments string `json:"rawArguments"`
			Arguments    struct {
				Code string `json:"code"`
			} `json:"arguments"`
			RawResult struct {
				Stdout   string `json:"stdout"`
				ExitCode int    `json:"exitCode"`
				Reason   string `json:"reason"`
			} `json:"rawResult"`
			Result    string `json:"result"`
			Available bool   `json:"modelResultAvailable"`
		}
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		id := record.ToolCallID
		if record.RawResult.Stdout != "raw "+id+"\n" || record.RawResult.ExitCode != 0 || record.RawResult.Reason != "completed" || record.Result != "effective "+id || !record.Available || !strings.Contains(record.RawArguments, "Original "+id+" call") || !strings.Contains(record.Arguments.Code, "Execute patched ") {
			t.Fatalf("raw/effective facts mixed: %s", data)
		}
		recorded[id] = true
	}
	if strings.Join(finished, ",") != "fast,slow" || len(recorded) != 2 {
		t.Fatalf("finish order/raw events: finished=%v records=%v", finished, recorded)
	}
	var steps, models, tools []string
	for _, event := range observed {
		switch event["type"] {
		case "model.step.started":
			steps = append(steps, hookActivity(event))
		case "model.started":
			models = append(models, hookActivity(event))
		case "tool.finished":
			tools = append(tools, hookActivity(event))
		}
	}
	if len(steps) != 2 || len(models) != 2 || len(tools) != 2 {
		t.Fatalf("join activities: %v %v %v", steps, models, tools)
	}
	for _, predecessor := range append(tools, models[0]) {
		assertHookCause(t, observed, steps[1], predecessor)
	}
}

func TestHooksFailurePreservesExecutedResultAndCancelsParallelTool(t *testing.T) {
	cwd := t.TempDir()
	ready := filepath.Join(cwd, "waiting-tool-ready")
	ran := filepath.Join(cwd, "executed-once")
	writePhaseHook(t, cwd, "fails.mjs", fmt.Sprintf(`import {existsSync} from "node:fs";
export async function afterTool(event) {
 if(event.toolCallId!=="fails") return;
 while(!existsSync(%q)) await new Promise(resolve=>setTimeout(resolve,5));
 throw Error("after hook fixture failed");
}`, ready))
	failureCode := fmt.Sprintf(`// Record completed execution exactly once.
import {existsSync,readFileSync,writeFileSync} from "node:fs";
const path=%q;writeFileSync(path,(existsSync(path)?readFileSync(path,"utf8"):"")+"x");console.log("actual success");`, ran)
	waitCode := fmt.Sprintf(`// Wait until the task cancels this parallel tool.
import {writeFileSync} from "node:fs";
writeFileSync(%q,"ready");while(true) await new Promise(resolve=>setTimeout(resolve,1000));`, ready)
	var mu sync.Mutex
	calls := 0
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return hookToolReply(map[string]string{"fails": failureCode, "waits": waitCode}, "fails", "waits"), nil
	})
	var eventMu sync.Mutex
	var observed []events.Event
	settings := agentTestOptions(p)
	settings.Observe = func(event events.Event) { eventMu.Lock(); observed = append(observed, event); eventMu.Unlock() }
	source := `import {run_agent} from "noe:agent";
console.log(JSON.stringify(await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3,hooks:["./fails.mjs"]},"fail after execution")));`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: settings, Stdout: &out, Stderr: &stderr})
	var result struct {
		LastResult struct {
			State string `json:"state"`
			Error string `json:"error"`
		} `json:"lastResult"`
	}
	decode := json.Unmarshal(out.Bytes(), &result)
	if code != 0 || err != nil || decode != nil || result.LastResult.State != "failed" || !strings.Contains(result.LastResult.Error, "after hook fixture failed") {
		t.Fatalf("code=%d err=%v decode=%v result=%+v out=%q stderr=%q", code, err, decode, result, &out, &stderr)
	}
	data, err := os.ReadFile(ran)
	if err != nil || string(data) != "x" {
		t.Fatalf("completed tool reran: data=%q error=%v", data, err)
	}
	mu.Lock()
	count := calls
	mu.Unlock()
	if count != 1 {
		t.Fatalf("hook error triggered %d model calls", count)
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	failedHooks, rawRecorded, waitCancelled := 0, false, false
	for _, event := range observed {
		if event["type"] == "hook.failed" {
			failedHooks++
		}
		if event["type"] == "run_js.finished" && event["toolCallId"] == "waits" && event["reason"] == "cancelled" {
			waitCancelled = true
		}
		if event["type"] == "tool.finished" && event["toolCallId"] == "fails" {
			encoded, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			var saved struct {
				RawResult struct {
					Stdout   string `json:"stdout"`
					ExitCode int    `json:"exitCode"`
					Reason   string `json:"reason"`
				} `json:"rawResult"`
				Available *bool           `json:"modelResultAvailable"`
				HookError string          `json:"hookError"`
				Result    json.RawMessage `json:"result"`
			}
			if err := json.Unmarshal(encoded, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.RawResult.Stdout != "actual success\n" || saved.RawResult.ExitCode != 0 || saved.RawResult.Reason != "completed" || saved.Available == nil || *saved.Available || !strings.Contains(saved.HookError, "after hook fixture failed") || string(saved.Result) != "null" {
				t.Fatalf("executed facts lost on hook failure: %s", encoded)
			}
			rawRecorded = true
		}
	}
	if failedHooks != 1 || !rawRecorded || !waitCancelled {
		t.Fatalf("failure cleanup: hookFailures=%d rawRecorded=%v waitCancelled=%v", failedHooks, rawRecorded, waitCancelled)
	}
}

func TestHooksGenericToolWrapperUsesSamePhases(t *testing.T) {
	cwd := t.TempDir()
	writePhaseHook(t, cwd, "generic.mjs", `
export async function beforeTool(event) {
 if(event.toolName!=="fixture_echo") throw Error("wrong generic tool");
 return {arguments:{value:"replaced"}};
}
export async function afterTool(event) {
 if(event.result.echo!=="replaced") throw Error("generic executor did not receive patched arguments");
 return {result:{answer:"fixture effective"}};
}`)
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		return provider.Response{Message: json.RawMessage(`{"role":"assistant","tool_calls":[{"id":"generic-call","type":"function","function":{"name":"fixture_echo","arguments":"{\"value\":\"raw\"}"}}]}`), FinishReason: "tool_calls"}, nil
	})
	var eventMu sync.Mutex
	var observed []events.Event
	settings := agentTestOptions(p)
	settings.Observe = func(event events.Event) { eventMu.Lock(); observed = append(observed, event); eventMu.Unlock() }
	hookPath, _ := json.Marshal(filepath.Join(cwd, "generic.mjs"))
	source := fmt.Sprintf(`import {request} from "noe:agent/internal";
import {executeToolWithHooks} from "noe:agent/task";
import {loadHookModules} from "noe:agent/hooks";
const rpc=async(method,payload)=>JSON.parse(await request(method,JSON.stringify(payload)));
const config={provider:"openrouter",model:"fixture/model",name:"generic-fixture",effort:"auto",maxTokens:0,hooks:[%s]};
const {runId}=await rpc("start",config);
let finished=false;
try {
 const loaded=await loadHookModules(config.hooks);
 const messages=[{role:"user",content:"private generic tool fixture"}];
 const tools=[{type:"function",function:{name:"fixture_echo",parameters:{type:"object",properties:{value:{type:"string"}},required:["value"]}}}];
 const {modelStepId}=await rpc("begin_model_step",{runId,messages,tools});
 const response=await rpc("model",{runId,modelStepId,model:config.model,maxTokens:0,effort:"auto",messages,tools});
 const result=await executeToolWithHooks(loaded,{runId,modelStepId,requestId:response.requestId},response.message.tool_calls[0],async call=>({echo:JSON.parse(call.function.arguments).value}));
 if(result.answer!=="fixture effective") throw Error("wrong result: "+JSON.stringify(result));
 console.log(result.answer);
 await rpc("finish",{runId,status:"completed",answer:"fixture done"});finished=true;
} finally {
 if(!finished) await rpc("finish",{runId,status:"failed",error:"generic fixture failed"});
}`, hookPath)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: settings, Stdout: &out, Stderr: &stderr})
	if code != 0 || err != nil || out.String() != "fixture effective\n" {
		t.Fatalf("code=%d error=%v stdout=%q stderr=%q", code, err, &out, &stderr)
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	phases := map[string]int{}
	genericResult := false
	for _, event := range observed {
		if event["type"] == "run_js.started" {
			t.Fatal("generic wrapper unexpectedly used run_js")
		}
		if event["type"] == "hook.started" {
			phases[fmt.Sprint(event["phase"])]++
		}
		if event["type"] == "tool.finished" && event["toolName"] == "fixture_echo" {
			genericResult = true
		}
	}
	if phases["beforeTool"] != 1 || phases["afterTool"] != 1 || !genericResult {
		t.Fatalf("generic tool hooks/results=%v/%v", phases, genericResult)
	}
}

func TestHooksInterruptReapsStuckCallbackAndOwnedChild(t *testing.T) {
	for _, test := range []struct{ name, body string }{{"cpu", "while(true) {}"}, {"promise", "setInterval(()=>{},1000); await new Promise(()=>{});"}} {
		t.Run(test.name, func(t *testing.T) {
			cwd := t.TempDir()
			ready := filepath.Join(cwd, "hook-ready.json")
			writePhaseHook(t, cwd, "stuck.mjs", fmt.Sprintf(`import {writeFileSync} from "node:fs";
export async function beforeModelRequest(event,api) {
 const {agentId}=await api.agents.spawn_agent({name:"hung-hook-child",hooks:[]},undefined,{keepAlive:true});
 writeFileSync(%q,JSON.stringify({agentId,runId:event.runId}));
 %s
}`, ready, test.body))
			var mu sync.Mutex
			calls := 0
			p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
				mu.Lock()
				calls++
				mu.Unlock()
				return agentAnswer("unexpected provider call"), nil
			})
			var eventMu sync.Mutex
			var observed []events.Event
			settings := agentTestOptions(p)
			settings.Observe = func(event events.Event) { eventMu.Lock(); observed = append(observed, event); eventMu.Unlock() }
			source := fmt.Sprintf(`import {spawn_agent,observe_agent,interrupt_agent} from "noe:agent";
import {existsSync,readFileSync} from "node:fs";
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto",hooks:["./stuck.mjs"]},"interrupt stuck hook");
while(!existsSync(%q)) await new Promise(resolve=>setTimeout(resolve,5));
const ready=JSON.parse(readFileSync(%q,"utf8"));
const receipt=await interrupt_agent({runId:ready.runId});
let state;
while(true) {
 state=await observe_agent({agentId});
 if(state.state==="stopped" || state.state==="failed") break;
 await new Promise(resolve=>setTimeout(resolve,5));
}
const child=await observe_agent({agentId:ready.agentId});
console.log(JSON.stringify({receipt,state,child}));`, ready, ready)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var out, stderr bytes.Buffer
			code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: settings, Stdout: &out, Stderr: &stderr})
			var result struct {
				Receipt struct {
					Status string `json:"status"`
				} `json:"receipt"`
				State struct {
					Reason     string `json:"reason"`
					LastResult struct {
						State string `json:"state"`
					} `json:"lastResult"`
				} `json:"state"`
				Child struct {
					State string `json:"state"`
				} `json:"child"`
			}
			decode := json.Unmarshal(out.Bytes(), &result)
			if code != 0 || err != nil || decode != nil || result.Receipt.Status != "accepted" || result.State.Reason != "task_interrupted" || result.State.LastResult.State != "cancelled" || result.Child.State != "stopped" {
				t.Fatalf("code=%d err=%v decode=%v result=%+v out=%q stderr=%q", code, err, decode, result, &out, &stderr)
			}
			mu.Lock()
			count := calls
			mu.Unlock()
			if count != 0 {
				t.Fatalf("stuck before hook reached provider %d times", count)
			}
			eventMu.Lock()
			defer eventMu.Unlock()
			cancelled := 0
			var invocation, childStart string
			for _, event := range observed {
				if event["type"] == "hook.started" {
					invocation = hookActivity(event)
				}
				if event["type"] == "instance.started" && event["name"] == "hung-hook-child" {
					childStart = hookActivity(event)
				}
				if event["type"] == "hook.cancelled" {
					cancelled++
				}
			}
			if cancelled != 1 {
				t.Fatalf("stuck hook has %d cancellation terminals, want one", cancelled)
			}
			assertHookCause(t, observed, childStart, invocation)
		})
	}
}

func TestHooksApplySteersBeforeAndDuringPreparationWithoutRefiring(t *testing.T) {
	cwd := t.TempDir()
	paths, _ := json.Marshal(map[string]string{
		"moduleReady": filepath.Join(cwd, "module-ready"), "moduleRelease": filepath.Join(cwd, "module-release"),
		"hookReady": filepath.Join(cwd, "hook-ready"), "hookRelease": filepath.Join(cwd, "hook-release"),
	})
	writePhaseHook(t, cwd, "gates.mjs", fmt.Sprintf(`import {existsSync,writeFileSync} from "node:fs";
const paths=%s;
writeFileSync(paths.moduleReady,"ready");
while(!existsSync(paths.moduleRelease)) await new Promise(resolve=>setTimeout(resolve,5));
let before=0;
export async function beforeModelRequest(event) {
 if(++before!==1) throw Error("before hook fired twice for one judgment");
 writeFileSync(paths.hookReady,JSON.stringify({runId:event.runId}));
 while(!existsSync(paths.hookRelease)) await new Promise(resolve=>setTimeout(resolve,5));
}`, paths))
	var mu sync.Mutex
	var requests []provider.Request
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()
		return agentAnswer("steered once"), nil
	})
	var eventMu sync.Mutex
	var observed []events.Event
	settings := agentTestOptions(p)
	settings.Observe = func(event events.Event) { eventMu.Lock(); observed = append(observed, event); eventMu.Unlock() }
	source := fmt.Sprintf(`import {spawn_agent,observe_agent,steer_agent} from "noe:agent";
import {existsSync,writeFileSync,readFileSync} from "node:fs";
const paths=%s;
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:1,hooks:["./gates.mjs"]},"initial");
while(!existsSync(paths.moduleReady)) await new Promise(resolve=>setTimeout(resolve,5));
const current=await observe_agent({agentId});
await steer_agent({runId:current.currentRunId,instruction:"before hook began",commandId:"before-gate"});
writeFileSync(paths.moduleRelease,"release");
while(!existsSync(paths.hookReady)) await new Promise(resolve=>setTimeout(resolve,5));
const ready=JSON.parse(readFileSync(paths.hookReady,"utf8"));
await steer_agent({runId:ready.runId,instruction:"during hook",commandId:"during-gate"});
writeFileSync(paths.hookRelease,"release");
while(true) {
 const state=await observe_agent({agentId});
 if(state.state==="stopped" || state.state==="failed") {console.log(JSON.stringify(state));break;}
 await new Promise(resolve=>setTimeout(resolve,5));
}`, paths)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: settings, Stdout: &out, Stderr: &stderr})
	var result struct {
		LastResult struct {
			State  string `json:"state"`
			Answer string `json:"answer"`
		} `json:"lastResult"`
	}
	decode := json.Unmarshal(out.Bytes(), &result)
	if code != 0 || err != nil || decode != nil || result.LastResult.State != "completed" || result.LastResult.Answer != "steered once" {
		t.Fatalf("code=%d error=%v decode=%v result=%+v stdout=%q stderr=%q", code, err, decode, result, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 {
		t.Fatalf("steer delivery consumed provider calls: %d", len(requests))
	}
	var users []string
	for _, raw := range requests[0].Messages {
		var message struct {
			Role    string
			Content string
		}
		if json.Unmarshal(raw, &message) == nil && message.Role == "user" && (message.Content == "initial" || message.Content == "before hook began" || message.Content == "during hook") {
			users = append(users, message.Content)
		}
	}
	if strings.Join(users, ",") != "initial,before hook began,during hook" {
		t.Fatalf("steer ordering=%v", users)
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	beforeHooks := 0
	applied := map[string]int{}
	for _, event := range observed {
		if event["type"] == "hook.started" && event["phase"] == "beforeModelRequest" {
			beforeHooks++
		}
		if event["type"] == "command.applied" {
			applied[fmt.Sprint(event["commandId"])]++
		}
	}
	if beforeHooks != 1 || applied["before-gate"] != 1 || applied["during-gate"] != 1 {
		t.Fatalf("duplicate/missing hook or applies: before=%d applied=%v", beforeHooks, applied)
	}
}
