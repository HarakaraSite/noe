package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

type limitedInstanceResult struct {
	AgentID     string `json:"agentId"`
	GroupID     string `json:"groupId"`
	State       string `json:"state"`
	Reason      string `json:"reason"`
	QueueLength int    `json:"queueLength"`
	LastResult  struct {
		State  string `json:"state"`
		Answer string `json:"answer"`
	} `json:"lastResult"`
	Execution struct {
		Counters struct{ ProviderCalls, AgentStarts, QueuedTasks, HookCalls, ActiveAgents, ActiveTasks int64 } `json:"counters"`
		Stop     *struct {
			Reason, LimitName string
			Limit, Used       int64
		} `json:"stop"`
		ElapsedMS int64 `json:"elapsedMs"`
	} `json:"execution"`
}

func runLimitedHookFixture(t *testing.T, cwd, source string, p agentProviderFunc) ([]byte, []events.Event) {
	t.Helper()
	var mu sync.Mutex
	var observed []events.Event
	settings := agentTestOptions(p)
	settings.Observe = func(event events.Event) { mu.Lock(); observed = append(observed, event); mu.Unlock() }
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: settings, Stdout: &out, Stderr: &stderr})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v out=%s stderr=%s", code, err, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]byte(nil), out.Bytes()...), append([]events.Event(nil), observed...)
}

func assertLimitSettled(t *testing.T, result limitedInstanceResult, dimension string, limit int64) {
	t.Helper()
	stop := result.Execution.Stop
	if result.State != "stopped" || result.Reason != "execution_limit" || stop == nil || stop.Reason != "execution_limit" || stop.LimitName != dimension || stop.Limit != limit || stop.Used != limit || result.Execution.Counters.ActiveAgents != 0 || result.Execution.Counters.ActiveTasks != 0 || result.QueueLength != 0 {
		t.Fatalf("limit not settled: %+v", result)
	}
}

func TestHooksSharedAgentStartsStopsGrandchildChainAndIsolatesRoot(t *testing.T) {
	cwd := t.TempDir()
	writePhaseHook(t, cwd, "chain.mjs", `export async function beforeModelRequest(event,api) {
 if(event.agentDepth<3) await api.agents.run_agent({name:"depth-"+(event.agentDepth+1)},"finite nested task");
}`)
	var calls atomic.Int32
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		calls.Add(1)
		return agentAnswer("independent complete"), nil
	})
	source := `import {run_agent} from "noe:agent";
const base={provider:"openrouter",model:"fixture/model",effort:"auto"};
const [a,b]=await Promise.all([
 run_agent({...base,name:"limited-root",hooks:["./chain.mjs"],executionLimits:{agentStarts:3}},"root chain"),
 run_agent({...base,name:"independent-root"},"independent task")
]);console.log(JSON.stringify({a,b}));`
	out, observed := runLimitedHookFixture(t, cwd, source, p)
	var results struct{ A, B limitedInstanceResult }
	if err := json.Unmarshal(out, &results); err != nil {
		t.Fatal(err)
	}
	assertLimitSettled(t, results.A, "agentStarts", 3)
	if results.A.GroupID == results.B.GroupID || results.B.LastResult.State != "completed" || results.B.LastResult.Answer != "independent complete" || results.B.Execution.Stop != nil || calls.Load() != 1 {
		t.Fatalf("root isolation failed: %s calls=%d", out, calls.Load())
	}
	starts, limits := 0, 0
	for _, event := range observed {
		if event["groupId"] != results.A.GroupID {
			continue
		}
		if event["type"] == "instance.started" {
			starts++
		}
		if event["type"] == "execution.limit_reached" {
			limits++
		}
	}
	if starts != 3 || limits != 1 {
		t.Fatalf("grandchild/start admission starts=%d limits=%d", starts, limits)
	}
}

func TestHooksSharedQueuedTasksStopsNextTaskChain(t *testing.T) {
	cwd := t.TempDir()
	writePhaseHook(t, cwd, "queue.mjs", `export async function afterModelResponse(event,api) {
 const options={agentId:event.agentId,instruction:"next queued task",commandId:"next-"+event.runId};
 const a=await api.agents.queue_agent(options);
 const b=await api.agents.queue_agent(options);
 if(a.commandId!==b.commandId) throw Error("queue replay created work");
}`)
	var calls atomic.Int32
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		calls.Add(1)
		return agentAnswer("round complete"), nil
	})
	source := `import {run_agent} from "noe:agent";
console.log(JSON.stringify(await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",hooks:["./queue.mjs"],executionLimits:{queuedTasks:2}},"first task",{keepAlive:true})));`
	out, observed := runLimitedHookFixture(t, cwd, source, p)
	var result limitedInstanceResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	assertLimitSettled(t, result, "queuedTasks", 2)
	if calls.Load() != 3 || result.Execution.Counters.QueuedTasks != 2 || result.Execution.Counters.ProviderCalls != 3 {
		t.Fatalf("queue loop/replay accounting: %s calls=%d", out, calls.Load())
	}
	runs := map[string]bool{}
	for _, event := range observed {
		if event["type"] == "agent.started" {
			id, _ := event["runId"].(string)
			runs[id] = true
		}
	}
	if len(runs) != 3 {
		t.Fatalf("queue did not cross fresh task runtimes: %v", runs)
	}
}

func TestHooksSharedHookLimitStopsBeforeNextHandler(t *testing.T) {
	cwd := t.TempDir()
	writePhaseHook(t, cwd, "limited.mjs", `export async function beforeModelRequest(event,api) {
 await api.context.setRuntimeInfo({key:"fixture",text:"first handler admitted"});
}
export async function afterModelResponse() { throw Error("second handler must not execute"); }
`)
	var calls atomic.Int32
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		calls.Add(1)
		return agentAnswer("candidate"), nil
	})
	source := `import {run_agent} from "noe:agent";
console.log(JSON.stringify(await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",hooks:["./limited.mjs"],executionLimits:{hookCalls:1}},"bounded hook fixture")));`
	out, observed := runLimitedHookFixture(t, cwd, source, p)
	var result limitedInstanceResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	assertLimitSettled(t, result, "hookCalls", 1)
	starts := 0
	for _, event := range observed {
		if event["type"] == "hook.started" {
			starts++
		}
		if event["type"] == "hook.failed" {
			t.Fatalf("denied handler executed: %+v", event)
		}
	}
	if starts != 1 || calls.Load() != 1 || result.Execution.Counters.HookCalls != 1 {
		t.Fatalf("wrong invocation/provider counts: %s starts=%d calls=%d", out, starts, calls.Load())
	}
}
