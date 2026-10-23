package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
	"strings"
	"sync"
	"testing"
	"time"
)

type executionContractModels struct{ runtimeFlowModels }

func (m *executionContractModels) Generate(ctx context.Context, request model.Request) (model.Response, error) {
	replies, err := toolReplyObjects(request)
	if err != nil {
		return model.Response{}, err
	}
	if len(replies) == 0 {
		return runtimeToolCallsResponse([]runtimeToolCall{
			{id: "structured", code: `import {list_models} from "noe:agent"; const spawn_agent="local"; export default {value:42,marker:spawn_agent,count:(await list_models()).length};`},
			{id: "check", code: `import {check} from "noe:runtime"; import {list_models} from "noe:agent"; const good=check('import {spawn_agent} from "noe:agent";export default 42;');const bad=check('export const value=;');const count=(await list_models()).length;export default {good,bad,count};`},
			{id: "promise", code: `export default Promise.resolve({value:42});`},
			{id: "then", code: `export function then(){throw Error("then export must not run");} export default 42;`},
		}), nil
	}
	if len(replies) != 4 {
		return model.Response{}, fmt.Errorf("incomplete tool continuation: %v", replies)
	}
	structured, ok := replies["structured"]["data"].(map[string]any)
	if !ok || structured["value"] != float64(42) || structured["marker"] != "local" || structured["hook"] != true {
		return model.Response{}, fmt.Errorf("structured data/hook lost: %v", replies["structured"])
	}
	if !strings.Contains(fmt.Sprint(replies["structured"]["stdout"]), "hook-before") {
		return model.Response{}, fmt.Errorf("beforeTool source did not execute")
	}
	checked, ok := replies["check"]["data"].(map[string]any)
	if !ok {
		return model.Response{}, fmt.Errorf("check did not return data: %v", replies["check"])
	}
	good, _ := checked["good"].(map[string]any)
	bad, _ := checked["bad"].(map[string]any)
	if good["ok"] != true || good["stage"] != "link" || bad["ok"] != false || checked["count"] != float64(1) {
		return model.Response{}, fmt.Errorf("check/parent operation contract: %v", checked)
	}
	failed := replies["promise"]
	diagnostics, _ := failed["diagnostics"].([]any)
	if failed["reason"] != "failed" || failed["phase"] != "serialize" || len(diagnostics) != 1 {
		return model.Response{}, fmt.Errorf("Promise result loss: %v", failed)
	}
	diagnostic, _ := diagnostics[0].(map[string]any)
	if diagnostic["code"] != "JS_RESULT" {
		return model.Response{}, fmt.Errorf("wrong Promise diagnostic: %v", diagnostic)
	}
	if replies["then"]["data"] != float64(42) || replies["then"]["reason"] != "completed" {
		return model.Response{}, fmt.Errorf("namespace then export executed: %v", replies["then"])
	}
	return runtimeFinalResponse("contract verified"), nil
}

func TestExecutionContractReachesHooksHistoryAndProvider(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	profile := stdlib.Profile{Modules: stdlib.Modules{Stdio: true}, NewLoader: func(context.Context, stdlib.Origin) (quickjs.ModuleLoader, error) {
		return func(specifier, referrer string) (string, string, error) {
			if specifier != "fixture:contract-hook" {
				return "", "", fmt.Errorf("unknown fixture module %s", specifier)
			}
			return `export function beforeTool(event){if(event.toolCallId==="structured")return {arguments:{...event.arguments,code:event.arguments.code+"\nconsole.log(\"hook-before\");"}};}
export function afterTool(event){if(event.toolCallId==="structured")return {result:{...event.result,data:{...event.result.data,hook:true}}};}`, specifier, nil
		}, nil
	}}
	var mu sync.Mutex
	var observed bool
	var observationError string
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: &executionContractModels{}, Runtime: &profile, Observe: func(event agent.Event) {
		if event["type"] != "tool.finished" || event["toolCallId"] != "structured" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		observed = true
		rawArgs, _ := json.Marshal(event["rawArguments"])
		args, _ := json.Marshal(event["arguments"])
		rawResult, _ := json.Marshal(event["rawResult"])
		result, _ := json.Marshal(event["result"])
		if strings.Contains(string(rawArgs), "hook-before") || !strings.Contains(string(args), "hook-before") || strings.Contains(string(rawResult), `"hook":true`) || !strings.Contains(string(result), `"hook":true`) {
			observationError = fmt.Sprintf("execution/transformation facts mixed: %s %s %s %s", rawArgs, args, rawResult, result)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	hooks := agent.HookModules{"fixture:contract-hook"}
	accepted, err := client.Spawn(ctx, agent.SpawnRequest{Config: agent.TaskConfig{Config: agent.AgentConfig{Provider: "fixture", Model: "fixture-model", Name: "contract", Effort: "none", Hooks: &hooks}, MaxSteps: 3}, Instruction: runtimeStringPointer("new contract")})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: accepted.AgentID, CommandID: accepted.CommandID})
	if err != nil || outcome.State != "completed" || outcome.Answer != "contract verified" {
		t.Fatalf("outcome=%+v error=%v", outcome, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !observed || observationError != "" {
		t.Fatalf("tool observation=%v error=%s", observed, observationError)
	}
}
