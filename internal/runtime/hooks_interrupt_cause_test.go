package runtime

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func TestHooksInterruptUsesRequestingInvocationCause(t *testing.T) {
	for _, target := range []string{"task", "agent"} {
		t.Run(target, func(t *testing.T) {
			cwd := t.TempDir()
			call := `api.agents.cancel_task({agentId:event.agentId,commandId:event.commandId})`
			reason := "task_interrupted"
			if target == "agent" {
				call = `api.agents.stop_agent({agentId:event.agentId})`
				reason = "interrupted"
			}
			writePhaseHook(t, cwd, "interrupt.mjs", `export async function beforeModelRequest(event,api) { await `+call+`; }`)
			var calls atomic.Int32
			p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
				calls.Add(1)
				return agentAnswer("unexpected"), nil
			})
			out, observed := runLimitedHookFixture(t, cwd, `import {run_agent} from "noe:agent";
console.log(JSON.stringify(await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",hooks:["./interrupt.mjs"]},"stop self from hook")));`, p)
			var result limitedInstanceResult
			if err := json.Unmarshal(out, &result); err != nil {
				t.Fatal(err)
			}
			if result.State != "stopped" || result.Reason != reason || result.LastResult.State != "cancelled" || calls.Load() != 0 {
				t.Fatalf("interrupt not settled: %s calls=%d", out, calls.Load())
			}
			var invocation, interrupt, creation string
			accepted := 0
			for _, event := range observed {
				if event["type"] == "hook.started" {
					invocation = hookActivity(event)
				}
				if event["type"] == "agent.started" {
					creation = hookActivity(event)
				}
				if event["type"] != "interrupt.accepted" {
					continue
				}
				accepted++
				interrupt = hookActivity(event)
				encoded, err := json.Marshal(event["origin"])
				if err != nil {
					t.Fatal(err)
				}
				var origin struct{ Kind, HookID, RunID string }
				if err := json.Unmarshal(encoded, &origin); err != nil {
					t.Fatal(err)
				}
				if origin.Kind != "hook" || origin.HookID == "" || origin.RunID == "" || event["target"] != target {
					t.Fatalf("wrong interrupt sender: %+v", event)
				}
			}
			if accepted != 1 || interrupt == "" || interrupt == invocation || interrupt == creation {
				t.Fatalf("interrupt reused target activity: accepted=%d interrupt=%s hook=%s target=%s", accepted, interrupt, invocation, creation)
			}
			assertHookCause(t, observed, interrupt, invocation)
		})
	}
}
