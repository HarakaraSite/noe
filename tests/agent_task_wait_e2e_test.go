//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Exercise the generated run_js bindings as well as the public JS export. All
// model requests use a local fixture; task waits need no saved event log.
func TestAgentTaskWaitInGeneratedJS(t *testing.T) {
	var calls atomic.Int32
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		calls.Add(1)
		task := agentUser(req)
		for index := len(req.Messages) - 1; index >= 0; index-- {
			if req.Messages[index].Role == "user" {
				if err := json.Unmarshal(req.Messages[index].Content, &task); err != nil {
					t.Error(err)
				}
				break
			}
		}
		if task == "parent" && len(req.Messages) == 2 {
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("workflow", `// Run two queued tasks without polling.
const worker=await spawn_agent({name:"worker"},undefined,{keepAlive:true});
if("commandId" in worker)throw Error("idle spawn has an initial task");
const first=await queue_agent({agentId:worker.agentId,instruction:"first"});
const second=await queue_agent({agentId:worker.agentId,instruction:"second"});
const [a,b]=await Promise.all([first,second].map(task=>wait_task({agentId:worker.agentId,commandId:task.commandId})));
const again=await wait_task({agentId:worker.agentId,commandId:first.commandId});
if(a.state!=="completed" || a.answer!=="first done" || b.answer!=="second done" || again.runId!==a.runId)throw Error(JSON.stringify({a,b,again}));
await interrupt_agent({agentId:worker.agentId});
console.log("workflow passed");`)}}}, FinishReason: "tool_calls"}
		}
		if task == "parent" {
			if !strings.Contains(string(req.Messages[len(req.Messages)-1].Content), "workflow passed") {
				t.Errorf("parent did not receive workflow result: %s", req.Messages[len(req.Messages)-1].Content)
			}
		}
		return agentModelReply{Content: task + " done"}
	})
	defer server.Close()
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "main.js"), `import {spawn_agent,wait_task} from "noe:agent";
const task=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"parent");
const result=await wait_task(task);
if(result.state!=="completed")throw Error(JSON.stringify(result));
console.log(result.answer);`)
	result := runCLI(t, work, agentCLIEnv(t, server.URL), "run", "main.js")
	requireExit(t, result, 0)
	if result.stdout != "parent done\n" || calls.Load() != 4 {
		t.Fatalf("stdout=%q stderr=%q model calls=%d", result.stdout, result.stderr, calls.Load())
	}
}
