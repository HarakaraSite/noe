//go:build e2e

package e2e_test

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestAgentDelegationRequiresExplicitParentPermission(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		instruction := agentUser(req)
		mu.Lock()
		calls[instruction]++
		step := calls[instruction]
		mu.Unlock()
		var code string
		switch instruction {
		case "root":
			if step == 1 {
				code = `const blocked = await run_agent({name:"blocked"}, "blocked");
if (blocked.lastResult?.answer !== "blocked correctly") throw Error(JSON.stringify(blocked));
const allowed = await run_agent({name:"allowed"}, "allowed", {allowDelegation:true});
if (allowed.lastResult?.answer !== "delegated correctly") throw Error(JSON.stringify(allowed));
console.log("done");`
			} else {
				result := agentToolResult(t, req.Messages[len(req.Messages)-1])
				if result.ExitCode != 0 || result.Stdout != "done\n" {
					t.Errorf("parent could not continue: %+v", result)
				}
				return agentModelReply{Content: "done"}
			}
		case "blocked":
			if step == 1 {
				code = `for (const launch of [spawn_agent,run_agent]) {
  try { await launch({name:"forbidden"},"forbidden",{allowDelegation:true}); }
  catch(error) {
    if (!String(error).includes("not allowed to delegate")) throw error;
    continue;
  }
  throw Error("unauthorized launch succeeded");
}
console.log("both rejected");`
			} else {
				result := agentToolResult(t, req.Messages[len(req.Messages)-1])
				if result.ExitCode != 0 || result.Stdout != "both rejected\n" {
					t.Errorf("child launch permission: %+v", result)
				}
				return agentModelReply{Content: "blocked correctly"}
			}
		case "allowed":
			if step == 1 {
				code = `const grandchild=await run_agent({name:"grandchild"},"grandchild");
if(grandchild.lastResult?.answer!=="grandchild done")throw Error(JSON.stringify(grandchild));
console.log("grandchild done");`
			} else {
				result := agentToolResult(t, req.Messages[len(req.Messages)-1])
				if result.ExitCode != 0 || result.Stdout != "grandchild done\n" {
					t.Errorf("authorized launch: %+v", result)
				}
				return agentModelReply{Content: "delegated correctly"}
			}
		case "grandchild":
			return agentModelReply{Content: "grandchild done"}
		default:
			t.Errorf("unexpected model request from %q", instruction)
			return agentModelReply{Content: "unexpected"}
		}
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("work", code)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	source := `import {run_agent} from "noe:agent";
const root=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"root");
if(root.lastResult?.answer!=="done")throw Error(JSON.stringify(root));console.log("done");`
	result := runCLI(t, t.TempDir(), agentCLIEnv(t, server.URL), "eval", source)
	requireExit(t, result, 0)
	if strings.TrimSpace(result.stdout) != "done" {
		t.Fatalf("result: %+v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 4 || calls["root"] != 2 || calls["blocked"] != 2 || calls["allowed"] != 2 || calls["grandchild"] != 1 {
		t.Fatalf("unexpected provider calls: %v", calls)
	}
}
