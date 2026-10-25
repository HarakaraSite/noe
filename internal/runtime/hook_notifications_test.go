package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func parseHookNotification(messages []json.RawMessage) (string, []agent.HookFailure, error) {
	if len(messages) == 0 {
		return "", nil, fmt.Errorf("missing notification message")
	}
	var tail struct{ Role, Content string }
	if err := json.Unmarshal(messages[len(messages)-1], &tail); err != nil || tail.Role != "user" || !strings.HasPrefix(tail.Content, "NOE_CONTEXT_STATUS\n") {
		return "", nil, fmt.Errorf("invalid status tail: %s", messages[len(messages)-1])
	}
	var failures []agent.HookFailure
	section := strings.SplitN(tail.Content, "\nHook processing failures for this task:\n", 2)
	if len(section) == 2 {
		for _, line := range strings.Split(section[1], "\n") {
			if !strings.HasPrefix(line, "- ") {
				continue
			}
			var failure agent.HookFailure
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "- ")), &failure); err != nil {
				return "", nil, err
			}
			failures = append(failures, failure)
		}
	}
	return tail.Content, failures, nil
}

func TestHookNotificationDeniedProviderAdmissionRemainsPending(t *testing.T) {
	cwd := t.TempDir()
	writePhaseHook(t, cwd, "failure.mjs", `export function beforeModelRequest(){throw Error("pending before provider limit");}`)
	var calls atomic.Int32
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		calls.Add(1)
		return agentAnswer("unexpected"), nil
	})
	source := `import {run_agent} from "noe:agent";
console.log(JSON.stringify(await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:1,hooks:["./failure.mjs"],executionLimits:{providerCalls:0}},"no provider admission")));`
	out, _ := runLimitedHookFixture(t, cwd, source, p)
	var result struct {
		LastResult agent.TaskOutcome
		Execution  struct {
			Stop *struct{ Reason, LimitName string }
		}
	}
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || result.LastResult.State != "cancelled" || len(result.LastResult.HookFailures) != 1 || result.LastResult.HookFailures[0].NotifiedRequestID != "" || result.Execution.Stop == nil || result.Execution.Stop.Reason != "execution_limit" || result.Execution.Stop.LimitName != "providerCalls" {
		t.Fatalf("result=%+v calls=%d", result, calls.Load())
	}
}
