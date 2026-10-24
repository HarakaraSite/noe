package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	publicagent "forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

// Both entry points must wake the actual wait RPC, not just a JavaScript race.
// The ordinary Client is deliberately non-control: it produces script origin,
// as does the public JS entry point used by script-hosted control surfaces.
func TestSteerAwareWaitRealExecutor(t *testing.T) {
	for _, entry := range []string{"trusted_host", "script_client"} {
		t.Run(entry, func(t *testing.T) {
			testSteerAwareWaitRealExecutor(t, entry)
		})
	}
}

type steerWaitTaskRef struct {
	AgentID   string `json:"agentId"`
	CommandID string `json:"commandId"`
}

func testSteerAwareWaitRealExecutor(t *testing.T, entry string) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	logDir := t.TempDir()
	if base := os.Getenv("NOE_STEER_WAIT_INTEGRATION_LOG_DIR"); base != "" {
		if err := os.MkdirAll(base, 0700); err != nil {
			t.Fatal(err)
		}
		var err error
		logDir, err = os.MkdirTemp(base, entry+"-")
		if err != nil {
			t.Fatal(err)
		}
	}
	logPath := filepath.Join(logDir, "executor.jsonl")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	t.Logf("executor events and complete model requests: %s", logPath)

	var mu sync.Mutex
	var observed []events.Event
	childContexts := map[string]context.Context{}
	record := func(kind string, value any) {
		mu.Lock()
		defer mu.Unlock()
		// Logging starts before Host creation, including failure paths.
		if err := json.NewEncoder(log).Encode(map[string]any{"kind": kind, "value": value}); err != nil {
			t.Errorf("write executor fixture log: %v", err)
		}
	}
	childStarted := make(chan string, 2)
	waitEntered := make(chan struct{}, 1)
	parentResumed := make(chan struct{}, 1)
	childSteerAccepted := make(chan struct{}, 1)
	rightRelease := make(chan struct{})
	leftRelease := make(chan struct{})
	const parentInstruction = "recover the parent without completing its children"
	const parentSteer = "inspect both children; cancel left and correct right"
	const childSteer = "finish using the parent's explicit correction"

	// An HTTP callback marks the JS call site after wait_tasks has returned its
	// pending promise. It does not hold an observer, Host mutex, or model request.
	// Thus publishing steer below cannot deadlock against this arrival signal.
	arrival := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record("wait-entered", r.URL.Path)
		select {
		case waitEntered <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer arrival.Close()

	var parentCalls atomic.Int32
	var rightCalls atomic.Int32
	var tasks []steerWaitTaskRef // Only the sequential parent provider calls use it.
	p := agentProviderFunc(func(callCtx context.Context, req provider.Request) (provider.Response, error) {
		record("model-request", req)
		switch req.Model {
		case "fixture/left", "fixture/right":
			if req.Model == "fixture/right" && rightCalls.Add(1) > 1 {
				if !queueContainsUser(req.Messages, childSteer) {
					return provider.Response{}, fmt.Errorf("child steer absent from next real request")
				}
				return agentAnswer("right corrected"), nil
			}
			mu.Lock()
			childContexts[req.Model] = callCtx
			mu.Unlock()
			select {
			case childStarted <- req.Model:
			default:
			}
			release := leftRelease
			if req.Model == "fixture/right" {
				release = rightRelease
			}
			select {
			case <-release:
				return agentAnswer("held child candidate"), nil
			case <-callCtx.Done():
				record("child-context-cancelled", req.Model)
				return provider.Response{}, callCtx.Err()
			}
		case "fixture/parent":
			switch parentCalls.Add(1) {
			case 1:
				return hookToolReply(map[string]string{"spawn-children": `// Start two genuinely unfinished child tasks.
import {spawn_agent} from "noe:agent";
const left=await spawn_agent({model:"fixture/left",name:"held-left"},"hold left");
const right=await spawn_agent({model:"fixture/right",name:"held-right"},"hold right");
export default [left,right];`}, "spawn-children"), nil
			case 2:
				if err := steerWaitToolData(req, "spawn-children", &tasks); err != nil {
					return provider.Response{}, err
				}
				if len(tasks) != 2 || tasks[0].AgentID == "" || tasks[1].AgentID == "" {
					return provider.Response{}, fmt.Errorf("invalid spawned tasks: %+v", tasks)
				}
				refs, _ := json.Marshal(tasks)
				code := fmt.Sprintf(`// Wait for the whole set and return the tool on parent steer.
import {wait_tasks} from "noe:agent";
const pending=wait_tasks({tasks:%s,includeAnswer:false});
const arrival=await fetch(%q);
if(arrival.status!==204)throw Error("fixture arrival failed");
export default await pending;`, refs, arrival.URL+"/wait-tasks")
				return hookToolReply(map[string]string{"wait-children": code}, "wait-children"), nil
			case 3:
				var result struct {
					Reason    string             `json:"reason"`
					RunID     string             `json:"runId"`
					Completed []json.RawMessage  `json:"completed"`
					Pending   []steerWaitTaskRef `json:"pending"`
				}
				if err := steerWaitToolData(req, "wait-children", &result); err != nil {
					return provider.Response{}, fmt.Errorf("wait_tasks integration/API connection: %w", err)
				}
				if result.Reason != "steer" || result.RunID != req.RunID || len(result.Completed) != 0 || !reflect.DeepEqual(result.Pending, tasks) {
					return provider.Response{}, fmt.Errorf("wait returned without preserving pending children: %+v, tasks=%+v", result, tasks)
				}
				if err := steerWaitHistoryOrder(req, parentSteer); err != nil {
					return provider.Response{}, err
				}
				mu.Lock()
				for _, model := range []string{"fixture/left", "fixture/right"} {
					childCtx := childContexts[model]
					if childCtx == nil || childCtx.Err() != nil {
						mu.Unlock()
						return provider.Response{}, fmt.Errorf("wait tool ended by cancelling %s", model)
					}
				}
				mu.Unlock()
				select {
				case parentResumed <- struct{}{}:
				default:
				}
				refs, _ := json.Marshal(tasks)
				code := fmt.Sprintf(`// The resumed parent explicitly chooses observation, steer and cancellation.
import {observe_agent,steer_agent,cancel_task,wait_task,read_task_result} from "noe:agent";
const tasks=%s;
const before=await Promise.all(tasks.map(task=>observe_agent({agentId:task.agentId})));
if(before.some(s=>s.state!=="running"||!s.currentRunId||s.lastResult!==null))throw Error(JSON.stringify(before));
const correction=await steer_agent({runId:before[1].currentRunId,instruction:%q,commandId:"child-correction"});
const cancellation=await cancel_task(tasks[0]);
if(correction.status!=="accepted"||cancellation.status!=="accepted")throw Error("explicit action rejected");
const left=await wait_task(tasks[0]);
const right=await wait_task(tasks[1]);
const retained=await read_task_result(tasks[1]);
if(left.state!=="cancelled"||right.state!=="completed"||right.answer!=="right corrected"||retained.text!==right.answer)throw Error(JSON.stringify({left,right,retained}));
export default {observed:before.map(s=>s.state),left:left.state,right:right.answer,retained:retained.text};`, refs, childSteer)
				return hookToolReply(map[string]string{"choose-actions": code}, "choose-actions"), nil
			case 4:
				var result struct {
					Observed []string `json:"observed"`
					Left     string   `json:"left"`
					Right    string   `json:"right"`
					Retained string   `json:"retained"`
				}
				if err := steerWaitToolData(req, "choose-actions", &result); err != nil {
					return provider.Response{}, err
				}
				if !reflect.DeepEqual(result.Observed, []string{"running", "running"}) || result.Left != "cancelled" || result.Right != "right corrected" || result.Retained != result.Right {
					return provider.Response{}, fmt.Errorf("resumed parent's explicit actions failed: %+v", result)
				}
				return agentAnswer("parent recovered"), nil
			}
		}
		return provider.Response{}, fmt.Errorf("unexpected model continuation: %s", req.Model)
	})
	options := agentTestOptions(p)
	options.Observe = func(event events.Event) {
		record("event", event)
		mu.Lock()
		observed = append(observed, event)
		mu.Unlock()
		if event["type"] == "command.accepted" && event["commandId"] == "child-correction" {
			select {
			case childSteerAccepted <- struct{}{}:
			default:
			}
		}
	}
	cwd := t.TempDir()
	h, err := newManagedExecution(ctx, Options{Cwd: cwd, Agent: options}, Source{
		Name: filepath.Join(cwd, "fixture.js"), Code: "export default null;",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	// Use the public managed Host, not private broker state. NewClient is the
	// ordinary ownership scope (control=false), also used for script steering.
	client, err := h.host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	allow := true
	parent, err := client.Spawn(ctx, publicagent.SpawnRequest{
		Config: publicagent.TaskConfig{Config: publicagent.AgentConfig{
			Provider: "openrouter", Model: "fixture/parent", Effort: "auto", Name: "waiting-parent",
		}, MaxSteps: 4}, Instruction: stringPointer(parentInstruction), AllowDelegation: &allow,
	})
	if err != nil {
		t.Fatal(err)
	}
	type completion struct {
		outcome publicagent.TaskOutcome
		err     error
	}
	done := make(chan completion, 1)
	go func() {
		result, err := client.WaitTask(ctx, publicagent.TaskRequest{AgentID: parent.AgentID, CommandID: parent.CommandID})
		done <- completion{result, err}
	}()
	await := func(signal <-chan struct{}, description string) {
		t.Helper()
		select {
		case <-signal:
		case result := <-done:
			t.Fatalf("%s: parent ended early: %+v error=%v (API not connected or fixture failed; log=%s)", description, result.outcome, result.err, logPath)
		case <-ctx.Done():
			t.Fatalf("%s: %v; log=%s", description, ctx.Err(), logPath)
		}
	}
	started := map[string]bool{}
	for len(started) < 2 {
		select {
		case model := <-childStarted:
			started[model] = true
		case result := <-done:
			t.Fatalf("children did not start: %+v error=%v; log=%s", result.outcome, result.err, logPath)
		case <-ctx.Done():
			t.Fatalf("children did not start: %v; log=%s", ctx.Err(), logPath)
		}
	}
	await(waitEntered, "parent run_js did not call wait_tasks")
	snapshot, err := client.Observe(ctx, publicagent.InstanceRequest{AgentID: parent.AgentID})
	if err != nil || snapshot.CurrentRunID == "" {
		t.Fatalf("observe waiting parent: %+v error=%v", snapshot, err)
	}
	steer := publicagent.SteerRequest{RunID: snapshot.CurrentRunID, Instruction: parentSteer, CommandID: "parent-correction"}
	var receipt publicagent.CommandReceipt
	if entry == "trusted_host" {
		receipt, err = h.host.Steer(ctx, steer)
	} else {
		receipt, err = client.Steer(ctx, steer)
	}
	if err != nil || receipt.Status != "accepted" {
		t.Fatalf("parent steer: %+v error=%v", receipt, err)
	}
	await(parentResumed, "steer did not reach the parent's next real model request")
	await(childSteerAccepted, "parent did not choose its explicit child action")
	close(rightRelease) // Only explicit parent action permits this child to finish.
	select {
	case result := <-done:
		if result.err != nil || result.outcome.State != "completed" || result.outcome.Answer != "parent recovered" {
			t.Fatalf("parent did not recover: %+v error=%v; log=%s", result.outcome, result.err, logPath)
		}
	case <-ctx.Done():
		t.Fatalf("parent did not finish explicit actions: %v; log=%s", ctx.Err(), logPath)
	}
	if parentCalls.Load() != 4 || rightCalls.Load() != 2 {
		t.Fatalf("unexpected real request counts parent=%d right=%d", parentCalls.Load(), rightCalls.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	steerWaitAssertEvents(t, observed, snapshot.CurrentRunID, entry)
}

func stringPointer(value string) *string { return &value }

// A completed outer run_js is essential: returning only the promise race would
// leave its wait RPC alive and block executor request joining before this result.
func steerWaitToolData(req provider.Request, id string, target any) error {
	for _, raw := range req.Messages {
		var message struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			Content    string `json:"content"`
		}
		if json.Unmarshal(raw, &message) != nil || message.Role != "tool" || message.ToolCallID != id {
			continue
		}
		var result struct {
			Reason   string          `json:"reason"`
			ExitCode int             `json:"exitCode"`
			Data     json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
			return fmt.Errorf("decode %s tool result: %w", id, err)
		}
		if result.Reason != "completed" || result.ExitCode != 0 || len(result.Data) == 0 {
			return fmt.Errorf("%s tool failed or API unavailable: %s", id, message.Content)
		}
		if err := json.Unmarshal(result.Data, target); err != nil {
			return fmt.Errorf("decode %s tool data: %w", id, err)
		}
		return nil
	}
	return fmt.Errorf("missing complete tool result %s", id)
}

func steerWaitHistoryOrder(req provider.Request, instruction string) error {
	tool, steer, count := -1, -1, 0
	for index, raw := range req.Messages {
		var message struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			Content    string `json:"content"`
		}
		if json.Unmarshal(raw, &message) != nil {
			continue
		}
		if message.Role == "tool" && message.ToolCallID == "wait-children" {
			tool = index
		}
		if message.Role == "user" && message.Content == instruction {
			steer = index
			count++
		}
	}
	if tool < 0 || steer <= tool || count != 1 {
		return fmt.Errorf("checkpointed wait block must precede the single applied steer: tool=%d steer=%d count=%d", tool, steer, count)
	}
	return nil
}

func steerWaitAssertEvents(t *testing.T, observed []events.Event, runID, entry string) {
	t.Helper()
	indices := map[string]int{}
	origin := ""
	for index, event := range observed {
		if event["runId"] != runID {
			continue
		}
		kind, _ := event["type"].(string)
		if event["commandId"] == "parent-correction" {
			if kind == "command.accepted" {
				raw, _ := json.Marshal(event["origin"])
				var decoded struct {
					Kind string `json:"kind"`
				}
				_ = json.Unmarshal(raw, &decoded)
				origin = decoded.Kind
				indices["accepted"] = index
			}
			if kind == "command.applied" {
				indices["applied"] = index
			}
		}
		if event["toolCallId"] == "wait-children" {
			if kind == "run_js.finished" {
				if event["success"] != true || event["reason"] != "completed" {
					t.Fatalf("wait tool cancelled/failed instead of ending its RPC: %+v", event)
				}
				indices["runtime-finished"] = index
			}
			if kind == "tool.finished" {
				indices["tool-recorded"] = index
			}
		}
		if kind == "model.started" {
			if applied, ok := indices["applied"]; ok && index > applied {
				if _, exists := indices["next-model"]; !exists {
					indices["next-model"] = index
				}
			}
		}
	}
	wantOrigin := "human"
	if entry == "script_client" {
		wantOrigin = "script"
	}
	if origin != wantOrigin {
		t.Fatalf("steer origin=%q want %q", origin, wantOrigin)
	}
	// There is no public conversation_checkpoint event. Its success is proved
	// by apply_steers validating the append against the retained complete tool
	// block, and by the next actual request containing that block before steer.
	order := []string{"accepted", "runtime-finished", "tool-recorded", "applied", "next-model"}
	previous := -1
	for _, key := range order {
		index, ok := indices[key]
		if !ok || index <= previous {
			t.Fatalf("tool/checkpoint/apply/model order failed at %s: %+v", key, indices)
		}
		previous = index
	}
}
