package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestGoClientWaitTasksStandaloneCancellationAndRetainedResults(t *testing.T) {
	ctx := testContext(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	models := &fixtureModels{generate: func(ctx context.Context, q model.Request) (model.Response, error) {
		entered <- struct{}{}
		select {
		case <-release:
			return model.Response{Message: json.RawMessage(`{"role":"assistant","content":"結論🙂"}`), FinishReason: "stop"}, nil
		case <-ctx.Done():
			return model.Response{}, ctx.Err()
		}
	}}
	profile := stdlib.Profile{}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: models, Runtime: &profile})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var tasks []agent.TaskReference
	for range 2 {
		spawned, err := client.Spawn(ctx, agent.SpawnRequest{Config: taskConfig(), Instruction: textPointer("task"), KeepAlive: true})
		if err != nil {
			t.Fatal(err)
		}
		tasks = append(tasks, agent.TaskReference(spawned))
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	requestCtx, cancel := context.WithCancel(ctx)
	interrupted := make(chan error, 1)
	go func() {
		_, err := client.WaitTasks(requestCtx, agent.WaitTasksRequest{Tasks: tasks})
		interrupted <- err
	}()
	cancel()
	if err := <-interrupted; !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitTasks cancellation=%v", err)
	}
	for _, ref := range tasks {
		snapshot, err := client.Observe(ctx, agent.InstanceRequest{AgentID: ref.AgentID})
		if err != nil || snapshot.State != "running" {
			t.Fatalf("cancelling wait changed task=%+v err=%v", snapshot, err)
		}
	}
	include := false
	resultCh := make(chan agent.WaitTasksResult, 1)
	errorsCh := make(chan error, 1)
	go func() {
		result, err := client.WaitTasks(ctx, agent.WaitTasksRequest{Tasks: []agent.TaskReference{tasks[1], tasks[0]}, IncludeAnswer: &include})
		resultCh <- result
		errorsCh <- err
	}()
	close(release)
	var result agent.WaitTasksResult
	select {
	case result = <-resultCh:
	case <-ctx.Done():
		t.Fatal("standalone grouped wait did not complete")
	}
	if err := <-errorsCh; err != nil {
		t.Fatal(err)
	}
	if result.Reason != "completed" || result.RunID != "" || len(result.Pending) != 0 || len(result.Completed) != 2 || result.Completed[0].AgentID != tasks[1].AgentID || result.Completed[1].AgentID != tasks[0].AgentID {
		t.Fatalf("standalone input-ordered result=%+v", result)
	}
	for _, completed := range result.Completed {
		if completed.Outcome.Answer != "" || completed.Outcome.AnswerBytes == nil || *completed.Outcome.AnswerBytes != len("結論🙂") {
			t.Fatalf("metadata=%+v", completed)
		}
	}
	*result.Completed[0].Outcome.AnswerBytes = 999
	repeated, err := client.WaitTasks(ctx, agent.WaitTasksRequest{Tasks: tasks})
	if err != nil || repeated.Completed[0].Outcome.Answer != "結論🙂" || repeated.Completed[1].Outcome.Answer != "結論🙂" || repeated.Completed[0].Outcome.AnswerBytes != nil {
		t.Fatalf("retained results changed=%+v err=%v", repeated, err)
	}
	for _, ref := range tasks {
		outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: ref.AgentID, CommandID: ref.CommandID})
		if err != nil || outcome.Answer != "結論🙂" {
			t.Fatalf("legacy retained result=%+v err=%v", outcome, err)
		}
	}
}

func TestWaitTasksJavaScriptValidationSnapshotAndBridge(t *testing.T) {
	ctx := testContext(t)
	profile := stdlib.Profile{}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: new(fixtureModels), Runtime: &profile})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	rt, err := stdlib.NewRuntime(ctx, stdlib.Profile{Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
		binding, err := agent.Attach(ctx, b, host)
		if err != nil {
			return nil, err
		}
		return binding.Close, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	_, err = rt.Run(stdlib.Source{Name: "[wait-tasks]", Virtual: true, Module: true, Code: `
import {spawn_agent, wait_task, wait_tasks} from "noe:agent";
import {createAgentAPI} from "noe:agent/api";
const assert = (test, message) => { if (!test) throw new Error(message); };
let calls = 0;
let payload;
const api = createAgentAPI({
  defaults: () => "{}",
  request: async (method, json) => {
    calls++;
    assert(method === "wait_tasks", "wrong RPC method");
    payload = JSON.parse(json);
    return JSON.stringify({reason:"completed", completed:[], pending:[]});
  },
});
for (const invalid of [
  null, [], {}, {tasks:[]}, {tasks:[{}]}, {tasks:[{agentId:" ",commandId:"x"}]},
  {tasks:[{agentId:"a",commandId:"x"},{agentId:"a",commandId:"x"}]},
  {tasks:[{agentId:"a",commandId:"x"}],includeAnswer:0},
  {tasks:[{agentId:"a",commandId:"x",runId:"forged"}]},
  {tasks:[{agentId:"a",commandId:"x"}],runId:"forged"},
]) {
  let rejected = false;
  try { await api.wait_tasks(invalid); } catch (e) { rejected = e instanceof TypeError; }
  assert(rejected, "invalid wait_tasks request was accepted");
}
assert(calls === 0, "invalid request reached RPC");
const options = {tasks:[{agentId:"a",commandId:"x"}],includeAnswer:false};
const pending = api.wait_tasks(options);
options.tasks[0].agentId = "changed";
options.tasks.push({agentId:"b",commandId:"y"});
options.includeAnswer = true;
await pending;
assert(calls === 1 && payload.tasks.length === 1 && payload.tasks[0].agentId === "a" && payload.includeAnswer === false, "RPC input was not snapshotted");

const first = await spawn_agent({provider:"fixture",model:"fixture-model",name:"first",effort:"none"}, "task");
const second = await spawn_agent({provider:"fixture",model:"fixture-model",name:"second",effort:"none"}, "task");
const grouped = await wait_tasks({tasks:[second,first],includeAnswer:false});
assert(grouped.reason === "completed" && grouped.runId === undefined && grouped.pending.length === 0 && grouped.completed.length === 2, "bad bridge grouped response");
assert(grouped.completed[0].agentId === second.agentId && grouped.completed[1].agentId === first.agentId, "bridge changed input order");
assert(!("answer" in grouped.completed[0].outcome) && grouped.completed[0].outcome.answerBytes > 0, "bridge lost includeAnswer projection");
const retained = await wait_task(first);
assert(retained.answer === "こんにちは世界", "grouped wait consumed retained result");
`})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWaitTasksHookScopedBridgeUsesOwnerRun(t *testing.T) {
	ctx := testContext(t)
	seen := make(chan string, 1)
	profile := stdlib.Profile{
		Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
			return nil, b.Runtime().Set("hookWaitSeen", func(value string) { seen <- value })
		},
		NewLoader: func(context.Context, stdlib.Origin) (quickjs.ModuleLoader, error) {
			return func(specifier, referrer string) (string, string, error) {
				if specifier != "fixture:grouped-wait-hook" {
					return "", "", fmt.Errorf("unexpected import %q from %q", specifier, referrer)
				}
				return `
export async function beforeModelRequest(event, api) {
  const child = await api.agents.spawn_agent({name:"hook-child",hooks:[]}, "child task");
  const result = await api.agents.wait_tasks({tasks:[child],includeAnswer:false});
  if (result.reason !== "completed" || result.runId !== event.runId || result.completed.length !== 1 || result.pending.length !== 0) {
    throw new Error("hook grouped wait lost owner or task");
  }
  hookWaitSeen(JSON.stringify(result));
}
`, "memory:/grouped-wait-hook.mjs", nil
			}, nil
		},
	}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: new(fixtureModels), Runtime: &profile})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	config := taskConfig()
	hooks := agent.HookModules{"fixture:grouped-wait-hook"}
	config.Hooks = &hooks
	allow := true
	parent, err := client.Spawn(ctx, agent.SpawnRequest{Config: config, Instruction: textPointer("parent task"), AllowDelegation: &allow})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: parent.AgentID, CommandID: parent.CommandID})
	if err != nil || outcome.State != "completed" {
		t.Fatalf("hook parent=%+v err=%v", outcome, err)
	}
	select {
	case wire := <-seen:
		var result agent.WaitTasksResult
		if err := json.Unmarshal([]byte(wire), &result); err != nil || result.RunID != outcome.RunID || result.Completed[0].Outcome.AnswerBytes == nil {
			t.Fatalf("hook result=%s err=%v", wire, err)
		}
	case <-ctx.Done():
		t.Fatal("hook did not report grouped wait")
	}
}
