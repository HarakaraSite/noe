package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	"testing"
	"time"
)

type retainedResultModels struct {
	runtimeFlowModels
	ref string
}

func toolReplyObjects(request model.Request) (map[string]map[string]any, error) {
	replies := map[string]map[string]any{}
	for _, raw := range request.Messages {
		var message struct {
			Role       string
			ToolCallID string `json:"tool_call_id"`
			Content    string
		}
		if json.Unmarshal(raw, &message) != nil || message.Role != "tool" {
			continue
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(message.Content), &value); err != nil {
			return nil, err
		}
		replies[message.ToolCallID] = value
	}
	return replies, nil
}
func (m *retainedResultModels) Generate(ctx context.Context, request model.Request) (model.Response, error) {
	replies, err := toolReplyObjects(request)
	if err != nil {
		return model.Response{}, err
	}
	if runtimeLastUserText(request) == "expired ref" {
		if _, ok := replies["expired"]; !ok {
			return runtimeToolResponse("expired", fmt.Sprintf(`import {read_tool_result,list_models} from "noe:agent";let expired=false;try{await read_tool_result(%q);}catch(error){expired=String(error).includes("unknown tool result reference");}if(!expired)throw Error("old task result remained readable");export default {expired,count:(await list_models()).length};`, m.ref)), nil
		}
		data, _ := replies["expired"]["data"].(map[string]any)
		if data["expired"] != true || data["count"] != float64(1) {
			return model.Response{}, fmt.Errorf("queue expiry failed: %v", replies["expired"])
		}
		return runtimeFinalResponse("expiry verified"), nil
	}
	if _, ok := replies["large"]; !ok {
		return runtimeToolCallsResponse([]runtimeToolCall{
			{id: "large", code: `console.log("HEAD"+"x".repeat(50000)+"MIDDLE"+"x".repeat(50000)+"TAIL");export default {text:"界".repeat(30000),quoted:"\\".repeat(50000)};`},
			{id: "large-error", code: `const error=new Error("retained failure");error.stdout="中".repeat(30000);throw error;`},
		}), nil
	}
	if _, ok := replies["read"]; !ok {
		large, failed := replies["large"], replies["large-error"]
		ref, _ := large["resultRef"].(string)
		failureRef, _ := failed["resultRef"].(string)
		dataOmitted, _ := large["dataOmittedBytes"].(float64)
		diagnosticsOmitted, _ := failed["diagnosticsOmittedBytes"].(float64)
		if ref == "" || failureRef == "" || large["data"] != nil || dataOmitted <= 65536 || diagnosticsOmitted <= 0 || failed["reason"] != "failed" {
			return model.Response{}, fmt.Errorf("large preview/ref contract: %v %v", large, failed)
		}
		m.ref = ref
		code := fmt.Sprintf(`import {read_tool_result} from "noe:agent";
const first=await read_tool_result(%q);
if(new TextEncoder().encode(JSON.stringify(first)).length>65536)throw Error("default page recursively exceeds inline data");
async function full(ref){const chunks=[];let offset=0;for(;;){const page=await read_tool_result(ref,{offset});chunks.push(page.text);if(!page.truncated)break;if(page.nextOffset<=offset)throw Error("paging stalled");offset=page.nextOffset;}return chunks.join("");}
const text=await full(%q);const raw=JSON.parse(text);const failure=JSON.parse(await full(%q));
if(raw.stdout!=="HEAD"+"x".repeat(50000)+"MIDDLE"+"x".repeat(50000)+"TAIL\n")throw Error("full stream middle was lost");
if(raw.data.text!=="界".repeat(30000)||raw.data.quoted!=="\\".repeat(50000))throw Error("full data changed");
if(failure.diagnostics[0].exception.stdout!=="中".repeat(30000)||failure.diagnostics[0].code!=="JS_EXCEPTION")throw Error("failure facts lost");
const firstUnicode=text.indexOf("界");let boundaryRejected=false;try{await read_tool_result(%q,{offset:firstUnicode+1});}catch(error){boundaryRejected=String(error).includes("UTF-8");}
if(!boundaryRejected)throw Error("UTF-8 boundary accepted");
export default {streamLength:raw.stdout.length,unicodeLength:raw.data.text.length,failureLength:failure.diagnostics[0].exception.stdout.length,boundaryRejected,firstPageBytes:new TextEncoder().encode(first.text).length};`, ref, ref, failureRef, ref)
		return runtimeToolResponse("read", code), nil
	}
	read := replies["read"]
	data, _ := read["data"].(map[string]any)
	if read["reason"] != "completed" || data["streamLength"] != float64(100015) || data["unicodeLength"] != float64(30000) || data["failureLength"] != float64(30000) || data["boundaryRejected"] != true || data["firstPageBytes"] != float64(24576) {
		return model.Response{}, fmt.Errorf("retained output not recovered: %v", read)
	}
	return runtimeFinalResponse("artifact verified"), nil
}

func TestToolResultFullRecoveryAndTaskExpiry(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	profile := stdlib.Profile{Modules: stdlib.Modules{Stdio: true}}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: &retainedResultModels{}, Runtime: &profile})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	accepted, err := client.Spawn(ctx, agent.SpawnRequest{Config: agent.TaskConfig{Config: agent.AgentConfig{Provider: "fixture", Model: "fixture-model", Effort: "none", Name: "retained"}, MaxSteps: 4}, Instruction: runtimeStringPointer("large contract"), KeepAlive: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: accepted.AgentID, CommandID: accepted.CommandID})
	if err != nil || first.State != "completed" || first.Answer != "artifact verified" {
		t.Fatalf("first=%+v error=%v", first, err)
	}
	queued, err := client.Queue(ctx, agent.QueueRequest{AgentID: accepted.AgentID, Instruction: "expired ref"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: accepted.AgentID, CommandID: queued.CommandID})
	if err != nil || second.State != "completed" || second.Answer != "expiry verified" {
		t.Fatalf("queued=%+v error=%v", second, err)
	}
	if _, err := client.StopAgent(ctx, agent.StopAgentRequest{AgentID: accepted.AgentID}); err != nil {
		t.Fatal(err)
	}
}
