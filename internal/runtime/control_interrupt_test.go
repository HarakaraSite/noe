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
	"sync/atomic"
	"testing"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

func interruptUserPrompt(request provider.Request) string {
	for index := len(request.Messages) - 1; index >= 0; index-- {
		var message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(request.Messages[index], &message) == nil && message.Role == "user" {
			return message.Content
		}
	}
	return ""
}

func waitForInterruptProviderPrompt(t *testing.T, ctx context.Context, requests <-chan string, want string) {
	t.Helper()
	select {
	case got := <-requests:
		if got != want {
			t.Fatalf("provider prompt = %q, want %q", got, want)
		}
	case <-ctx.Done():
		t.Fatalf("waiting for provider prompt %q: %v", want, ctx.Err())
	}
}

func TestPublicRunInterruptKeepsKeepAliveAndQueuedTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	providerStarted := filepath.Join(t.TempDir(), "provider-started")
	requests := make(chan string, 4)
	stopped := make(chan struct{})
	var calls atomic.Int32
	p := agentProviderFunc(func(callCtx context.Context, request provider.Request) (provider.Response, error) {
		prompt := interruptUserPrompt(request)
		requests <- prompt
		calls.Add(1)
		if prompt == "hold until interrupted" {
			if err := os.WriteFile(providerStarted, []byte("started"), 0o600); err != nil {
				return provider.Response{}, err
			}
			<-callCtx.Done()
			close(stopped)
			return provider.Response{}, callCtx.Err()
		}
		return agentAnswer(prompt + " done"), nil
	})
	var eventMu sync.Mutex
	var observed []events.Event
	options := agentTestOptions(p)
	options.Observe = func(event events.Event) {
		eventMu.Lock()
		observed = append(observed, event)
		eventMu.Unlock()
	}
	source := fmt.Sprintf(`import {spawn_agent,queue_agent,observe_agent,interrupt_agent} from "noe:agent";
import {existsSync} from "node:fs";
const providerStarted=%q;
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",name:"run-interrupt",effort:"auto"},undefined,{keepAlive:true});
for(const [instruction,commandId] of [["hold until interrupted","hold"],["continue one","next"],["continue two","last"]]) await queue_agent({agentId,instruction,commandId});
for(let i=0;i<10000&&!existsSync(providerStarted);i++)await new Promise(resolve=>setTimeout(resolve,1));
if(!existsSync(providerStarted))throw Error("provider did not begin the first task");
let snapshot;
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.currentRunId)break;await new Promise(resolve=>setTimeout(resolve,1));}
if(!snapshot || !snapshot.currentRunId)throw Error("first task did not start");
const ack=await interrupt_agent({runId:snapshot.currentRunId});
if(ack.target!=="run"||ack.status!=="accepted")throw Error(JSON.stringify(ack));
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.state==="idle"&&snapshot.lastResult?.commandId==="last")break;await new Promise(resolve=>setTimeout(resolve,1));}
if(!snapshot||snapshot.state!=="idle"||snapshot.queueLength!==0||snapshot.lastResult?.answer!=="continue two done")throw Error(JSON.stringify(snapshot));
await interrupt_agent({agentId});
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.state==="stopped")break;await new Promise(resolve=>setTimeout(resolve,1));}
if(snapshot.state!=="stopped"||snapshot.reason!=="interrupted")throw Error(JSON.stringify(snapshot));
console.log("run interrupt preserved queue");`, providerStarted)
	var stdout bytes.Buffer
	code, err := Execute(ctx, Source{"interrupt-run.js", source}, Options{Stdout: &stdout, Agent: options})
	if code != 0 || err != nil || stdout.String() != "run interrupt preserved queue\n" {
		t.Fatalf("code=%d error=%v stdout=%q", code, err, stdout.String())
	}
	waitForInterruptProviderPrompt(t, ctx, requests, "hold until interrupted")
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("active provider request survived task interrupt")
	}
	waitForInterruptProviderPrompt(t, ctx, requests, "continue one")
	waitForInterruptProviderPrompt(t, ctx, requests, "continue two")
	if calls.Load() != 3 {
		t.Fatalf("provider calls=%d, want interrupted task and two queued tasks", calls.Load())
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	var accepted, cancelled []string
	for _, event := range observed {
		if event["type"] == "interrupt.accepted" && event["target"] == "run" {
			accepted = append(accepted, fmt.Sprint(event["runId"]))
		}
		if event["type"] == "command.cancelled" {
			cancelled = append(cancelled, fmt.Sprint(event["commandId"]))
		}
	}
	if len(accepted) != 1 || accepted[0] == "" || len(cancelled) != 1 || cancelled[0] != "hold" {
		t.Fatalf("interrupt events accepted=%v cancelled=%v", accepted, cancelled)
	}
}

func TestPublicAgentInterruptCancelsBusyTaskAndQueue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	providerStarted := filepath.Join(t.TempDir(), "provider-started")
	requests := make(chan string, 4)
	var calls atomic.Int32
	p := agentProviderFunc(func(callCtx context.Context, request provider.Request) (provider.Response, error) {
		prompt := interruptUserPrompt(request)
		requests <- prompt
		calls.Add(1)
		if err := os.WriteFile(providerStarted, []byte("started"), 0o600); err != nil {
			return provider.Response{}, err
		}
		<-callCtx.Done()
		return provider.Response{}, callCtx.Err()
	})
	var eventMu sync.Mutex
	var eventOrder []string
	var cancelledCommands []string
	var wrongOperation []string
	options := agentTestOptions(p)
	options.Observe = func(event events.Event) {
		name := fmt.Sprint(event["type"])
		switch name {
		case "interrupt.accepted":
			if event["target"] == "agent" {
				eventMu.Lock()
				eventOrder = append(eventOrder, "interrupt.accepted")
				eventMu.Unlock()
			}
		case "command.cancelled":
			eventMu.Lock()
			cancelledCommands = append(cancelledCommands, fmt.Sprint(event["commandId"]))
			if event["operation"] != "queue" {
				wrongOperation = append(wrongOperation, fmt.Sprint(event["commandId"]))
			}
			eventOrder = append(eventOrder, "command.cancelled:"+fmt.Sprint(event["commandId"]))
			eventMu.Unlock()
		case "agent.cancelled", "instance.stopping", "instance.stopped":
			eventMu.Lock()
			eventOrder = append(eventOrder, name)
			eventMu.Unlock()
		}
	}
	source := fmt.Sprintf(`import {spawn_agent,queue_agent,observe_agent,interrupt_agent} from "noe:agent";
import {existsSync} from "node:fs";
const providerStarted=%q;
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",name:"agent-interrupt",effort:"auto"},undefined,{keepAlive:true});
for(const [instruction,commandId] of [["block instance","busy"],["cancel queued one","queued-1"],["cancel queued two","queued-2"]]) await queue_agent({agentId,instruction,commandId});
for(let i=0;i<10000&&!existsSync(providerStarted);i++)await new Promise(resolve=>setTimeout(resolve,1));
if(!existsSync(providerStarted))throw Error("provider did not begin the busy task");
let snapshot;
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.currentRunId)break;await new Promise(resolve=>setTimeout(resolve,1));}
if(!snapshot || !snapshot.currentRunId)throw Error("busy task did not start");
const ack=await interrupt_agent({agentId});
if(ack.target!=="agent"||ack.status!=="accepted")throw Error(JSON.stringify(ack));
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.state==="stopped")break;await new Promise(resolve=>setTimeout(resolve,1));}
if(!snapshot||snapshot.state!=="stopped"||snapshot.reason!=="interrupted"||snapshot.queueLength!==0||snapshot.lastResult?.state!=="cancelled")throw Error(JSON.stringify(snapshot));
console.log("agent interrupt stopped queue");`, providerStarted)
	var stdout bytes.Buffer
	code, err := Execute(ctx, Source{"interrupt-agent.js", source}, Options{Stdout: &stdout, Agent: options})
	if code != 0 || err != nil || stdout.String() != "agent interrupt stopped queue\n" {
		t.Fatalf("code=%d error=%v stdout=%q", code, err, stdout.String())
	}
	waitForInterruptProviderPrompt(t, ctx, requests, "block instance")
	if calls.Load() != 1 {
		t.Fatalf("provider calls=%d, queued tasks must not start", calls.Load())
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	if len(cancelledCommands) != 3 {
		t.Fatalf("cancelled command events=%v order=%v", cancelledCommands, eventOrder)
	}
	if len(wrongOperation) != 0 {
		t.Fatalf("queued cancellation events missing operation=queue: %v", wrongOperation)
	}
	if strings.Join(eventOrder, ",") != "interrupt.accepted,command.cancelled:queued-1,command.cancelled:queued-2,agent.cancelled,command.cancelled:busy,instance.stopping,instance.stopped" {
		t.Fatalf("interrupt/terminal event order=%v", eventOrder)
	}
}

func TestPublicIdleAgentInterruptPreservesLastResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var calls atomic.Int32
	p := agentProviderFunc(func(_ context.Context, request provider.Request) (provider.Response, error) {
		calls.Add(1)
		return agentAnswer(interruptUserPrompt(request) + " complete"), nil
	})
	source := `import {spawn_agent,observe_agent,interrupt_agent} from "noe:agent";
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",name:"idle-interrupt",effort:"auto"},"preserve this answer",{keepAlive:true});
let snapshot;
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.state==="idle"&&snapshot.lastResult?.state==="completed")break;await new Promise(resolve=>setTimeout(resolve,1));}
const prior=snapshot?.lastResult;
if(!prior||prior.answer!=="preserve this answer complete")throw Error(JSON.stringify(snapshot));
const ack=await interrupt_agent({agentId});
if(ack.status!=="accepted")throw Error(JSON.stringify(ack));
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.state==="stopped")break;await new Promise(resolve=>setTimeout(resolve,1));}
if(snapshot.state!=="stopped"||snapshot.reason!=="interrupted"||snapshot.lastResult?.answer!==prior.answer||snapshot.lastResult?.state!=="completed")throw Error(JSON.stringify(snapshot));`
	code, err := Execute(ctx, Source{"interrupt-idle.js", source}, Options{Agent: agentTestOptions(p)})
	if code != 0 || err != nil {
		t.Fatalf("code=%d error=%v", code, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls=%d, want one completed task", calls.Load())
	}
}

func TestPublicInterruptRequiresExactlyOneTarget(t *testing.T) {
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		return agentAnswer("unexpected"), nil
	})
	source := `import {interrupt_agent} from "noe:agent";
for(const target of [{},{runId:"run",agentId:"agent"},{runId:""},{agentId:""}]){
  let rejected=false;
  try{await interrupt_agent(target);}catch(error){rejected=error instanceof TypeError;}
  if(!rejected)throw Error("invalid interrupt target accepted: "+JSON.stringify(target));
}
let unknown=false;
try{await interrupt_agent({runId:"missing-run"});}catch(error){unknown=String(error).includes("unknown or unowned");}
if(!unknown)throw Error("unknown run did not produce a target diagnostic");`
	code, err := Execute(context.Background(), Source{"interrupt-validation.js", source}, Options{Agent: agentTestOptions(p)})
	if code != 0 || err != nil {
		t.Fatalf("code=%d error=%v", code, err)
	}
}

func TestPublicRunInterruptRejectsAfterFinalCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	dir := t.TempDir()
	finalReady := filepath.Join(dir, "final-ready")
	releaseFinal := filepath.Join(dir, "release-final")
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		return agentAnswer("committed answer"), nil
	})
	options := agentTestOptions(p)
	options.Observe = func(event events.Event) {
		if event["type"] != "agent.final" {
			return
		}
		_ = os.WriteFile(finalReady, []byte("ready"), 0o600)
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			if _, err := os.Stat(releaseFinal); err == nil {
				return
			}
			select {
			case <-deadline.C:
				return
			case <-ticker.C:
			}
		}
	}
	source := fmt.Sprintf(`import {spawn_agent,observe_agent,interrupt_agent} from "noe:agent";
import {writeFileSync,existsSync} from "node:fs";
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",name:"final-interrupt",effort:"auto"},"finish normally");
let snapshot;
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.currentRunId)break;await new Promise(resolve=>setTimeout(resolve,1));}
if(!snapshot?.currentRunId)throw Error("task did not start");
const runId=snapshot.currentRunId;
for(let i=0;i<10000&&!existsSync(%q);i++)await new Promise(resolve=>setTimeout(resolve,1));
if(!existsSync(%q))throw Error("final commit was not observed");
let rejected=false;
try{await interrupt_agent({runId});}catch(error){if(!String(error).includes("finished"))throw error;rejected=true;}
writeFileSync(%q,"release");
if(!rejected)throw Error("interrupt after final commit was accepted");
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.state==="stopped")break;await new Promise(resolve=>setTimeout(resolve,1));}
if(snapshot.state!=="stopped"||snapshot.lastResult?.state!=="completed"||snapshot.lastResult?.answer!=="committed answer")throw Error(JSON.stringify(snapshot));`, finalReady, finalReady, releaseFinal)
	code, err := Execute(ctx, Source{"interrupt-final-gate.js", source}, Options{Agent: options})
	_ = os.WriteFile(releaseFinal, []byte("release"), 0o600)
	if code != 0 || err != nil {
		t.Fatalf("code=%d error=%v", code, err)
	}
}

func toolCallResponse(t *testing.T, code string) provider.Response {
	t.Helper()
	arguments, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		t.Fatal(err)
	}
	message, err := json.Marshal(map[string]any{
		"role": "assistant", "content": "interrupting from generated JS",
		"tool_calls": []map[string]any{{
			"id": "interrupt-self", "type": "function",
			"function": map[string]string{"name": "run_js", "arguments": string(arguments)},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return provider.Response{Message: message, FinishReason: "tool_calls"}
}

func TestPublicRunJSCanInterruptItsOwnTaskWithoutJoining(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	runID := make(chan string, 1)
	modelRequests := make(chan provider.Request, 4)
	var calls atomic.Int32
	p := agentProviderFunc(func(callCtx context.Context, request provider.Request) (provider.Response, error) {
		modelRequests <- request
		if calls.Add(1) != 1 {
			return agentAnswer("unexpected model continuation"), nil
		}
		var id string
		select {
		case id = <-runID:
		case <-callCtx.Done():
			return provider.Response{}, callCtx.Err()
		}
		code := fmt.Sprintf(`// Interrupt the currently executing managed task.
await interrupt_agent({runId:%q});
console.log("interrupt accepted");`, id)
		return toolCallResponse(t, code), nil
	})
	options := agentTestOptions(p)
	options.Observe = func(event events.Event) {
		if event["type"] == "agent.started" && event["name"] == "self-run-js" {
			runID <- fmt.Sprint(event["runId"])
		}
	}
	source := `import {spawn_agent,queue_agent,observe_agent,interrupt_agent} from "noe:agent";
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",name:"self-run-js",effort:"auto"},undefined,{keepAlive:true});
await queue_agent({agentId,instruction:"run a self interrupt",commandId:"self"});
let snapshot;
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.state==="idle"&&snapshot.lastResult?.commandId==="self")break;await new Promise(resolve=>setTimeout(resolve,1));}
if(snapshot.state!=="idle"||snapshot.lastResult?.state!=="cancelled")throw Error(JSON.stringify(snapshot));
await interrupt_agent({agentId});
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.state==="stopped")break;await new Promise(resolve=>setTimeout(resolve,1));}`
	code, err := Execute(ctx, Source{"interrupt-self-run-js.js", source}, Options{Agent: options})
	if code != 0 || err != nil {
		var request provider.Request
		select {
		case request = <-modelRequests:
			for len(modelRequests) != 0 {
				request = <-modelRequests
			}
		default:
		}
		t.Fatalf("self run interrupt deadlocked or failed: code=%d error=%v model=%s", code, err, request.Messages)
	}
}

func TestPublicRunJSCanInterruptItsOwnInstanceWithoutJoining(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	startedAgent := make(chan string, 1)
	p := agentProviderFunc(func(_ context.Context, request provider.Request) (provider.Response, error) {
		instruction := interruptUserPrompt(request)
		const prefix = "self-agent:"
		if !strings.HasPrefix(instruction, prefix) {
			return provider.Response{}, fmt.Errorf("unexpected task instruction %q", instruction)
		}
		agentID := strings.TrimPrefix(instruction, prefix)
		startedAgent <- agentID
		code := fmt.Sprintf(`// Stop the managed instance which owns this generated JS scope.
await interrupt_agent({agentId:%q});
console.log("interrupt accepted");`, agentID)
		return toolCallResponse(t, code), nil
	})
	source := `import {spawn_agent,queue_agent,observe_agent} from "noe:agent";
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",name:"self-agent-js",effort:"auto"},undefined,{keepAlive:true});
await queue_agent({agentId,instruction:"self-agent:"+agentId,commandId:"self"});
let snapshot;
for(let i=0;i<10000;i++){snapshot=await observe_agent({agentId});if(snapshot.state==="stopped")break;await new Promise(resolve=>setTimeout(resolve,1));}
if(snapshot.state!=="stopped"||snapshot.reason!=="interrupted"||snapshot.lastResult?.state!=="cancelled")throw Error(JSON.stringify(snapshot));`
	code, err := Execute(ctx, Source{"interrupt-self-agent-js.js", source}, Options{Agent: agentTestOptions(p)})
	if code != 0 || err != nil {
		t.Fatalf("self instance interrupt deadlocked or failed: code=%d error=%v", code, err)
	}
	select {
	case id := <-startedAgent:
		if id == "" {
			t.Fatal("provider did not receive the instance identifier")
		}
	default:
		t.Fatal("task never reached the fixture provider")
	}
}
