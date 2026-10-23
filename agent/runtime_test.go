package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

type runtimeFlowModels struct {
	mu       sync.Mutex
	requests []model.Request
}

func (*runtimeFlowModels) ListModels(context.Context, model.ModelQuery) ([]model.Model, error) {
	return []model.Model{{Provider: "fixture", Model: "fixture-model"}}, nil
}

func (m *runtimeFlowModels) Generate(ctx context.Context, request model.Request) (model.Response, error) {
	if err := ctx.Err(); err != nil {
		return model.Response{}, err
	}
	m.mu.Lock()
	request.Messages = cloneRuntimeMessages(request.Messages)
	m.requests = append(m.requests, request)
	m.mu.Unlock()

	lastUserText := runtimeLastUserText(request)
	switch {
	case strings.Contains(lastUserText, "parent root"):
		if !runtimeHasToolReply(request, "parent-children") || !runtimeHasToolReply(request, "parent-fresh-vm") {
			return runtimeToolCallsResponse([]runtimeToolCall{
				{id: "parent-children", code: parentChildrenCode()},
				{id: "parent-fresh-vm", code: parentFreshVMCode()},
			}), nil
		}
		return runtimeFinalResponse("parent complete"), nil
	case strings.Contains(lastUserText, "cancel child"):
		<-ctx.Done()
		return model.Response{}, ctx.Err()
	case strings.Contains(lastUserText, "queued child"):
		return runtimeFinalResponse("queued child complete"), nil
	case strings.Contains(lastUserText, "default child"):
		if !runtimeHasToolReply(request, "default-child-check") {
			return runtimeToolResponse("default-child-check", defaultChildCode()), nil
		}
		return runtimeFinalResponse("default child complete"), nil
	case strings.Contains(lastUserText, "allowed child"):
		if !runtimeHasToolReply(request, "allowed-child-check") {
			return runtimeToolResponse("allowed-child-check", allowedChildCode()), nil
		}
		return runtimeFinalResponse("allowed child complete"), nil
	case strings.Contains(lastUserText, "grandchild"):
		return runtimeFinalResponse("grandchild complete"), nil
	default:
		return model.Response{}, fmt.Errorf("unexpected model request for session %q", request.SessionID)
	}
}

func (*runtimeFlowModels) Decide(context.Context, model.DecisionRequest) (model.DecisionResponse, error) {
	return model.DecisionResponse{}, model.ErrUnsupported
}

func (*runtimeFlowModels) GenerateImage(context.Context, model.ImageRequest) (model.GeneratedImage, error) {
	return model.GeneratedImage{}, model.ErrUnsupported
}

func (m *runtimeFlowModels) snapshotRequests() []model.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]model.Request(nil), m.requests...)
}

func cloneRuntimeMessages(messages []json.RawMessage) []json.RawMessage {
	cloned := make([]json.RawMessage, len(messages))
	for i := range messages {
		cloned[i] = append(json.RawMessage(nil), messages[i]...)
	}
	return cloned
}

func runtimeFinalResponse(answer string) model.Response {
	message, _ := json.Marshal(map[string]any{"role": "assistant", "content": answer})
	return model.Response{Message: message, FinishReason: "stop", Usage: json.RawMessage(`{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`)}
}

func runtimeToolResponse(id, code string) model.Response {
	return runtimeToolCallsResponse([]runtimeToolCall{{id: id, code: code}})
}

type runtimeToolCall struct{ id, code string }

func runtimeToolCallsResponse(calls []runtimeToolCall) model.Response {
	encodedCalls := make([]json.RawMessage, 0, len(calls))
	for _, entry := range calls {
		arguments, _ := json.Marshal(map[string]string{"code": entry.code})
		call, _ := json.Marshal(map[string]any{
			"id": entry.id, "type": "function",
			"function": map[string]string{"name": "run_js", "arguments": string(arguments)},
		})
		encodedCalls = append(encodedCalls, call)
	}
	message, _ := json.Marshal(map[string]any{"role": "assistant", "content": nil, "tool_calls": encodedCalls})
	return model.Response{Message: message, FinishReason: "tool_calls"}
}

func runtimeHasUserText(request model.Request, needle string) bool {
	for _, raw := range request.Messages {
		var message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(raw, &message) != nil || message.Role != "user" {
			continue
		}
		var text string
		if json.Unmarshal(message.Content, &text) == nil && strings.Contains(text, needle) {
			return true
		}
		var parts []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(message.Content, &parts) == nil {
			for _, part := range parts {
				if strings.Contains(part.Text, needle) {
					return true
				}
			}
		}
	}
	return false
}

func runtimeLastUserText(request model.Request) string {
	var last string
	for _, raw := range request.Messages {
		var message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(raw, &message) != nil || message.Role != "user" {
			continue
		}
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			if strings.HasPrefix(text, "NOE_CONTEXT_STATUS\n") {
				continue
			}
			last = text
			continue
		}
		var parts []struct {
			Text string `json:"text"`
		}
		if json.Unmarshal(message.Content, &parts) == nil {
			var combined strings.Builder
			for _, part := range parts {
				combined.WriteString(part.Text)
			}
			text = combined.String()
			if !strings.HasPrefix(text, "NOE_CONTEXT_STATUS\n") {
				last = text
			}
		}
	}
	return last
}

func runtimeHasToolReply(request model.Request, callID string) bool {
	for _, raw := range request.Messages {
		var message struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
		}
		if json.Unmarshal(raw, &message) == nil && message.Role == "tool" && message.ToolCallID == callID {
			return true
		}
	}
	return false
}

const markerImport = `import {loaderGeneration} from "fixture:runtime-marker";`

func parentChildrenCode() string {
	return markerImport + `import {cancel_task,queue_agent,spawn_agent,stop_agent,wait_task} from "noe:agent";

globalThis.parentToolSecret = "must stay in this VM";
const config = {provider:"fixture", model:"fixture-model", name:"child", effort:"none", maxSteps:4};
const child = await spawn_agent(config, "default child", {keepAlive:true});
const queued = await queue_agent({agentId:child.agentId, instruction:"queued child"});
const cancelled = await queue_agent({agentId:child.agentId, instruction:"cancel child"});
const cancelledReceipt = await cancel_task({agentId:child.agentId, commandId:cancelled.commandId});
const first = await wait_task({agentId:child.agentId, commandId:child.commandId});
const second = await wait_task({agentId:child.agentId, commandId:queued.commandId});
const cancelledOutcome = await wait_task({agentId:child.agentId, commandId:cancelled.commandId});
if (first.state !== "completed" || second.state !== "completed" || cancelledOutcome.state !== "cancelled" || cancelledReceipt.status !== "accepted") {
  throw new Error("child queue result mismatch: " + JSON.stringify([first, second, cancelledOutcome, cancelledReceipt]));
}
await stop_agent({agentId:child.agentId});
const delegated = await spawn_agent(config, "allowed child", {allowDelegation:true});
const delegatedOutcome = await wait_task({agentId:delegated.agentId, commandId:delegated.commandId});
if (delegatedOutcome.state !== "completed") throw new Error("explicit child delegation failed: " + JSON.stringify(delegatedOutcome));
console.log("parent-tool-one:" + loaderGeneration + ":" + first.state + ":" + second.state + ":" + cancelledOutcome.state);`
}

func parentFreshVMCode() string {
	return markerImport + `
if (typeof globalThis.parentToolSecret !== "undefined") throw new Error("tool JavaScript state leaked");
const api = await import("noe:agent");
if ("interrupt_agent" in api) throw new Error("legacy interrupt API is still exported");
let savedLogRejected = false;
try { await api.observe_agent({logPath:"old.jsonl", runId:"old-run"}); }
catch (error) { savedLogRejected = String(error).includes("live observation"); }
if (!savedLogRejected) throw new Error("observe_agent accepted saved-log options");
console.log("parent-tool-two:" + loaderGeneration + ":fresh");
const body = "界".repeat(30000);
console.log("stdout-head:" + body + ":stdout-tail");
console.error("stderr-head:" + body + ":stderr-tail");`
}

func defaultChildCode() string {
	return markerImport + `import {spawn_agent} from "noe:agent";

if (typeof globalThis.parentToolSecret !== "undefined") throw new Error("parent tool JavaScript state leaked into child");
let denied = false;
try {
  await spawn_agent({provider:"fixture", model:"fixture-model", name:"grandchild", effort:"none"}, "unexpected grandchild");
} catch (error) {
  denied = String(error).includes("not allowed to delegate");
}
if (!denied) throw new Error("child without allowDelegation spawned a grandchild");
console.log("default-delegation-denied:" + loaderGeneration);`
}

func allowedChildCode() string {
	return markerImport + `import {spawn_agent,wait_task} from "noe:agent";

if (typeof globalThis.parentToolSecret !== "undefined") throw new Error("parent tool JavaScript state leaked into child");
const grandchild = await spawn_agent({provider:"fixture", model:"fixture-model", name:"grandchild", effort:"none", maxSteps:2}, "grandchild");
const result = await wait_task({agentId:grandchild.agentId, commandId:grandchild.commandId});
if (result.state !== "completed") throw new Error("grandchild task failed: " + JSON.stringify(result));
console.log("explicit-delegation-allowed:" + loaderGeneration);`
}

func TestGoRuntimeRunsParentToolChildQueueAndFreshScopes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	models := new(runtimeFlowModels)
	var mu sync.Mutex
	var eventsSeen []agent.Event
	var initialized, loaders int
	var loaderEntries []string
	var initErrors []error
	profile := stdlib.Profile{
		Modules: stdlib.Modules{Stdio: true},
		Initialize: func(_ context.Context, bindings *stdlib.Bindings) (func(), error) {
			mu.Lock()
			initialized++
			id := initialized
			mu.Unlock()
			env := bindings.Environment()
			if env.Stdin == nil {
				return nil, errors.New("task/tool stdin was nil")
			}
			var one [1]byte
			n, err := env.Stdin.Read(one[:])
			if n != 0 || !errors.Is(err, io.EOF) {
				mu.Lock()
				initErrors = append(initErrors, fmt.Errorf("initializer stdin read n=%d err=%v", n, err))
				mu.Unlock()
			}
			if env.Stdout == nil {
				return nil, errors.New("task/tool stdout capture was nil")
			}
			if _, err := fmt.Fprintf(env.Stdout, "initializer-%d\n", id); err != nil {
				return nil, fmt.Errorf("write initializer output: %w", err)
			}
			return nil, nil
		},
		NewLoader: func(_ context.Context, origin stdlib.Origin) (quickjs.ModuleLoader, error) {
			mu.Lock()
			loaders++
			generation := loaders
			loaderEntries = append(loaderEntries, origin.Entry)
			mu.Unlock()
			return func(specifier, referrer string) (string, string, error) {
				if specifier == "fixture:runtime-hook" {
					name := fmt.Sprintf("memory:/runtime/%d/hook.mjs", generation)
					return `export async function beforeTool(_event, api) { const models = await api.agents.list_models({provider:"fixture"}); if (!Array.isArray(models)) throw new Error("hook model catalog was not scoped"); }`, name, nil
				}
				if specifier != "fixture:runtime-marker" {
					return "", "", fmt.Errorf("unexpected import %q from %q", specifier, referrer)
				}
				name := fmt.Sprintf("memory:/runtime/%d/marker.mjs", generation)
				return fmt.Sprintf("export const loaderGeneration=%d;", generation), name, nil
			}, nil
		},
	}
	host, err := agent.NewHost(ctx, agent.HostConfig{
		Models: models, Runtime: &profile,
		Observe: func(event agent.Event) {
			mu.Lock()
			eventsSeen = append(eventsSeen, event)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	allowDelegation := true
	config := agent.TaskConfig{Config: agent.AgentConfig{
		Provider: "fixture", Model: "fixture-model", Name: "parent", Effort: "none",
	}, MaxSteps: 5}
	hooks := agent.HookModules{"fixture:runtime-hook"}
	config.Hooks = &hooks
	started, err := client.Spawn(ctx, agent.SpawnRequest{Config: config, Instruction: runtimeStringPointer("parent root"), AllowDelegation: &allowDelegation})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: started.AgentID, CommandID: started.CommandID})
	if err != nil || outcome.State != "completed" || outcome.Answer != "parent complete" {
		mu.Lock()
		var diagnostics []string
		for _, event := range eventsSeen {
			if event["type"] == "stdout" || event["type"] == "stderr" || event["type"] == "tool.error" {
				diagnostics = append(diagnostics, fmt.Sprintf("%s/%s: %v", event["source"], event["type"], event))
			}
		}
		mu.Unlock()
		var transcripts []string
		for _, request := range models.snapshotRequests() {
			transcripts = append(transcripts, fmt.Sprintf("run=%s session=%s user=%q", request.RunID, request.SessionID, runtimeLastUserText(request)))
		}
		t.Fatalf("parent outcome=%+v err=%v diagnostics=%v requests=%v", outcome, err, diagnostics, transcripts)
	}

	requests := models.snapshotRequests()
	var parentSession string
	var defaultChildSession, defaultChildRun string
	var queuedChildSession, queuedChildRun string
	for _, request := range requests {
		if runtimeHasUserText(request, "parent root") {
			parentSession = request.SessionID
		}
		if runtimeHasUserText(request, "default child") && !runtimeHasUserText(request, "queued child") {
			if defaultChildSession == "" {
				defaultChildSession, defaultChildRun = request.SessionID, request.RunID
			}
		}
		if runtimeHasUserText(request, "queued child") {
			queuedChildSession, queuedChildRun = request.SessionID, request.RunID
		}
	}
	if parentSession == "" || defaultChildSession == "" || queuedChildSession == "" {
		var transcript []string
		for _, request := range requests {
			transcript = append(transcript, fmt.Sprintf("run=%s parent=%v default=%v queued=%v", request.RunID,
				runtimeHasUserText(request, "parent root"), runtimeHasUserText(request, "default child"), runtimeHasUserText(request, "queued child")))
		}
		mu.Lock()
		var output []string
		for _, event := range eventsSeen {
			if event["type"] == "stdout" || event["type"] == "stderr" {
				output = append(output, fmt.Sprintf("%s/%s: %v", event["source"], event["type"], event["text"]))
			}
		}
		mu.Unlock()
		t.Fatalf("missing parent/child requests: parent=%q child=%q queued=%q calls=%v output=%v", parentSession, defaultChildSession, queuedChildSession, transcript, output)
	}
	if defaultChildSession != queuedChildSession || defaultChildRun == queuedChildRun || parentSession == defaultChildSession {
		t.Fatalf("session/run ownership parent=%q child=(%q,%q) queued=(%q,%q)", parentSession, defaultChildSession, defaultChildRun, queuedChildSession, queuedChildRun)
	}

	mu.Lock()
	observed := append([]agent.Event(nil), eventsSeen...)
	initCount, loaderCount := initialized, loaders
	entries := append([]string(nil), loaderEntries...)
	initializerErrors := append([]error(nil), initErrors...)
	mu.Unlock()
	if len(initializerErrors) != 0 {
		t.Fatalf("initializer checks=%v", initializerErrors)
	}
	if initCount == 0 || loaderCount != initCount {
		t.Fatalf("fresh runtime initialization/loader count=%d/%d", initCount, loaderCount)
	}
	var taskCapture, toolCapture bool
	var boundedStdout, boundedStderr bool
	var toolMarkers []string
	var hookStarts int
	for _, event := range observed {
		if event["type"] == "hook.started" && event["phase"] == "beforeTool" {
			hookStarts++
		}
		kind, _ := event["type"].(string)
		if kind != "stdout" && kind != "stderr" {
			continue
		}
		source, _ := event["source"].(string)
		text, _ := event["text"].(string)
		if kind == "stdout" && source == "agent_task" && strings.Contains(text, "initializer-") {
			taskCapture = true
		}
		if kind == "stdout" && source == "run_js" && strings.Contains(text, "initializer-") {
			toolCapture = true
		}
		omitted, _ := event["omittedBytes"].(int64)
		if source == "run_js" && kind == "stdout" && strings.Contains(text, "stdout-head:") {
			boundedStdout = omitted > 0 && utf8.ValidString(text) && strings.Contains(text, "stdout-tail") && strings.Contains(text, "NOE: output truncated")
		}
		if source == "run_js" && kind == "stderr" && strings.Contains(text, "stderr-head:") {
			boundedStderr = omitted > 0 && utf8.ValidString(text) && strings.Contains(text, "stderr-tail") && strings.Contains(text, "NOE: output truncated")
		}
		if kind == "stdout" {
			for _, marker := range runtimeMarkerPattern.FindAllString(text, -1) {
				toolMarkers = append(toolMarkers, marker)
			}
		}
	}
	if !taskCapture || !toolCapture {
		t.Fatalf("initializer output capture missing: task=%v tool=%v", taskCapture, toolCapture)
	}
	if !boundedStdout || !boundedStderr {
		t.Fatalf("bounded UTF-8 capture failed: stdout=%v stderr=%v", boundedStdout, boundedStderr)
	}
	if len(toolMarkers) < 4 {
		t.Fatalf("task/tool executions did not load independent runtime modules: %v", toolMarkers)
	}
	if hookStarts < 4 {
		t.Fatalf("parallel task/tool calls did not use scoped beforeTool hooks: %d", hookStarts)
	}
	seenMarkers := make(map[string]bool)
	for _, marker := range toolMarkers {
		if seenMarkers[marker] {
			t.Fatalf("fresh tool runtime reused loader marker %q: %v", marker, toolMarkers)
		}
		seenMarkers[marker] = true
	}
	var sawTaskEntry, sawToolEntry bool
	for _, entry := range entries {
		sawTaskEntry = sawTaskEntry || strings.Contains(entry, "agent task")
		sawToolEntry = sawToolEntry || strings.Contains(entry, "agent tool")
	}
	if !sawTaskEntry || !sawToolEntry {
		t.Fatalf("loader origins omitted task/tool entries: %v", entries)
	}
}

func TestAgentJavaScriptAPIUsesScopedCancelStopAndLiveObservation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: new(runtimeFlowModels)})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	profile := stdlib.Profile{Initialize: func(ctx context.Context, bindings *stdlib.Bindings) (func(), error) {
		binding, err := agent.Attach(ctx, bindings, host)
		if err != nil {
			return nil, err
		}
		return binding.Close, nil
	}}
	runtime, err := stdlib.NewRuntime(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, err = runtime.Run(stdlib.Source{Name: "[agent api fixture]", Virtual: true, Module: true, Code: `
import {cancel_task, stop_agent, observe_agent} from "noe:agent";
import * as api from "noe:agent";
if (typeof cancel_task !== "function" || typeof stop_agent !== "function") throw new Error("scoped cancel/stop API is missing");
if ("interrupt_agent" in api) throw new Error("legacy interrupt API is still exported");
let cancelValidation = false;
try { await cancel_task({agentId:"agent", commandId:"task", extra:true}); }
catch (error) { cancelValidation = String(error).includes("unknown cancel task option"); }
let cancelIdentityValidation = false;
try { await cancel_task({agentId:"", commandId:"task"}); }
catch (error) { cancelIdentityValidation = String(error).includes("agentId must be a nonempty string"); }
let stopValidation = false;
try { await stop_agent({agentId:"agent", extra:true}); }
catch (error) { stopValidation = String(error).includes("unknown stop agent option"); }
let stopIdentityValidation = false;
try { await stop_agent({agentId:" "}); }
catch (error) { stopIdentityValidation = String(error).includes("agentId must be a nonempty string"); }
let liveOnly = false;
try { await observe_agent({logPath:"old.jsonl", runId:"old-run"}); }
catch (error) { liveOnly = String(error).includes("live observation"); }
let observeIdentityValidation = false;
try { await observe_agent({agentId:""}); }
catch (error) { observeIdentityValidation = String(error).includes("agentId must be a nonempty string"); }
if (!cancelValidation || !cancelIdentityValidation || !stopValidation || !stopIdentityValidation || !liveOnly || !observeIdentityValidation) throw new Error("agent API input contract was not enforced");`})
	if err != nil {
		t.Fatal(err)
	}
}

var runtimeMarkerPattern = regexp.MustCompile(`(?:parent-tool-one|parent-tool-two|default-delegation-denied|explicit-delegation-allowed):([0-9]+)`)

func runtimeStringPointer(value string) *string { return &value }
