package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func TestHooksIsolateFourPhasesAndKeepLastValidValues(t *testing.T) {
	cwd := t.TempDir()
	logPath := filepath.Join(cwd, "order")
	ran := filepath.Join(cwd, "tool-once")
	common := fmt.Sprintf(`import {appendFileSync} from "node:fs";
function record(e,label){appendFileSync(%q,label+":"+e.phase+"\n");}
`, logPath)
	writePhaseHook(t, cwd, "first.mjs", common+fmt.Sprintf(`
export function beforeModelRequest(e){record(e,"first"); return {ignored:1};}
export function afterModelResponse(e){record(e,"first"); return {ignored:2};}
export function beforeTool(e){
 record(e,"first");
 return {arguments:{code:'// Execute the effective tool once.\nimport {appendFileSync} from "node:fs";appendFileSync('+JSON.stringify(%q)+',"x");console.log("effective execution");'}};
}
export function afterTool(e){record(e,"first");return {result:{kept:true}};}
`, ran))
	writePhaseHook(t, cwd, "fails.mjs", common+`
import {writeFileSync} from "node:fs";
import {execFile} from "node:child_process";
export async function beforeModelRequest(e,api){
 record(e,"fails");
 await api.context.setRuntimeInfo({key:"partial",text:"kept runtime info after failure"});
 writeFileSync("./missing-directory/output","unavailable");
}
export async function afterModelResponse(e){record(e,"fails");return Promise.reject(Error("agent broker returned an invalid hook scope"));}
export async function beforeTool(e){record(e,"fails");await execFile("sh",["-c","exit 7"]);}
export function afterTool(e){record(e,"fails");throw undefined;}
`)
	writePhaseHook(t, cwd, "invalid.mjs", `
export function beforeTool(){const value={};value.self=value;return {arguments:value};}
export function afterTool(){const value={};value.self=value;return {result:value};}
`)
	writePhaseHook(t, cwd, "schema.mjs", `export function beforeTool(){return {arguments:{code:42}};}`)
	writePhaseHook(t, cwd, "last.mjs", common+`
export function beforeModelRequest(e){record(e,"last");}
export function afterModelResponse(e){record(e,"last");}
export function beforeTool(e){record(e,"last");if(!e.arguments.code.includes("effective tool"))throw Error("lost arguments");e.arguments.code="mutated snapshot";}
export function afterTool(e){record(e,"last");if(e.result.kept!==true)throw Error("lost result");e.result.kept=false;}
`)
	notices := make(chan []agent.HookFailure, 2)
	var calls atomic.Int32
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		n := calls.Add(1)
		_, failures, err := parseHookNotification(req.Messages)
		if err != nil {
			return provider.Response{}, err
		}
		notices <- failures
		want := 7
		if n == 1 {
			want = 1
		}
		if len(failures) != want {
			t.Errorf("request %d notification failures=%+v want %d", n, failures, want)
		}
		if !strings.Contains(fmt.Sprint(req.Messages), "kept runtime info after failure") {
			t.Error("failed hook's runtime side effect lost")
		}
		if n == 1 {
			return hookToolReply(map[string]string{"one": "// Original tool must be replaced.\nthrow Error('unpatched');"}, "one"), nil
		}
		toolCount := 0
		for _, raw := range req.Messages {
			var msg struct{ Role, Content string }
			if json.Unmarshal(raw, &msg) == nil && msg.Role == "tool" {
				toolCount++
				if msg.Content != `{"kept":true}` {
					t.Errorf("last valid result lost: %s", raw)
				}
			}
		}
		if toolCount != 1 {
			t.Errorf("tool results=%d want 1", toolCount)
		}
		return agentAnswer("four phase recovery"), nil
	})
	source := `import {run_agent,wait_task,wait_tasks,observe_agent,read_task_result} from "noe:agent";
const s=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2,hooks:["./first.mjs","./fails.mjs","./invalid.mjs","./schema.mjs","./last.mjs"]},"recover all phases");
const ref={agentId:s.agentId,commandId:s.lastResult.commandId};
const a=await wait_task({...ref,includeAnswer:false});
const b=await wait_tasks({tasks:[ref],includeAnswer:false});
const c=await observe_agent({agentId:s.agentId,includeAnswer:false});
const text=await read_task_result(ref);
for(const value of [a,b.completed[0].outcome,c.lastResult]){
 if("answer" in value || value.answerBytes!==s.lastResult.answer.length || JSON.stringify(value.hookFailures)!==JSON.stringify(s.lastResult.hookFailures))throw Error("projection lost failure: "+JSON.stringify(value));
}
if(text.text!==s.lastResult.answer)throw Error("answer was modified");
console.log(JSON.stringify(s));`
	out, observed := runLimitedHookFixture(t, cwd, source, p)
	var result struct{ LastResult agent.TaskOutcome }
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	outcome := result.LastResult
	if outcome.State != "completed" || outcome.Answer != "four phase recovery" || outcome.Error != "" || calls.Load() != 2 || len(outcome.HookFailures) != 9 {
		t.Fatalf("outcome=%+v providerCalls=%d output=%s", outcome, calls.Load(), out)
	}
	expected := map[string]int{"beforeModelRequest": 2, "afterModelResponse": 2, "beforeTool": 3, "afterTool": 2}
	ids := map[string]bool{}
	var requestIDs []string
	for _, event := range observed {
		if event["type"] == "model.started" {
			requestIDs = append(requestIDs, fmt.Sprint(event["requestId"]))
		}
	}
	if len(requestIDs) != 2 {
		t.Fatalf("provider request IDs=%v", requestIDs)
	}
	notified := map[string]string{}
	for _, requestID := range requestIDs {
		for _, failure := range <-notices {
			if notified[failure.InvocationID] != "" || failure.NotifiedRequestID != "" {
				t.Fatalf("duplicate/nonpending delivery: %+v", failure)
			}
			notified[failure.InvocationID] = requestID
		}
	}
	if len(notified) != 8 {
		t.Fatalf("delivered failures=%v", notified)
	}
	for _, failure := range outcome.HookFailures {
		expected[failure.Phase]--
		if failure.InvocationID == "" || ids[failure.InvocationID] || failure.Module == "" || failure.ModelStepID == "" || failure.Error == "" || failure.NotifiedRequestID != notified[failure.InvocationID] {
			t.Fatalf("invalid failure: %+v", failure)
		}
		ids[failure.InvocationID] = true
		if failure.NotifiedRequestID == "" && (failure.Phase != "afterModelResponse" || failure.RequestID != requestIDs[1]) {
			t.Fatalf("nonfinal failure left pending: %+v", failure)
		}
		if failure.Phase == "afterModelResponse" && failure.RequestID == requestIDs[0] && failure.NotifiedRequestID != requestIDs[1] {
			t.Fatalf("source/notification request mixed: %+v", failure)
		}
		if failure.Phase == "beforeModelRequest" && failure.RequestID != "" {
			t.Fatalf("unexpected source request: %+v", failure)
		}
		if failure.Phase != "beforeModelRequest" && failure.RequestID == "" {
			t.Fatalf("missing source request: %+v", failure)
		}
		if strings.HasSuffix(failure.Phase, "Tool") && (failure.ToolCallID != "one" || failure.ToolName != "run_js") {
			t.Fatalf("wrong tool source: %+v", failure)
		}
	}
	for phase, n := range expected {
		if n != 0 {
			t.Errorf("failure count mismatch phase=%s remaining=%d", phase, n)
		}
	}
	failedEvents := 0
	for _, event := range observed {
		if event["type"] == "hook.failed" {
			failedEvents++
			if !ids[fmt.Sprint(event["invocationId"])] {
				t.Fatalf("unretained hook failure: %+v", event)
			}
		}
	}
	if failedEvents != len(ids) {
		t.Fatalf("failure events=%d records=%d", failedEvents, len(ids))
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	for _, phase := range []string{"beforeModelRequest", "afterModelResponse", "beforeTool", "afterTool", "beforeModelRequest", "afterModelResponse"} {
		for _, label := range []string{"first", "fails", "last"} {
			fmt.Fprintf(&want, "%s:%s\n", label, phase)
		}
	}
	if string(log) != want.String() {
		t.Fatalf("hook order/one-time effects:\ngot %s\nwant %s", log, want.String())
	}
	if data, err := os.ReadFile(ran); err != nil || string(data) != "x" {
		t.Fatalf("tool effects=%q err=%v", data, err)
	}
}
