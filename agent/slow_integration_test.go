//go:build slow_integration

package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

// Exercise sustained waits and cancellation through the Go Client, without a
// CLI, internal HTTP transport, or entry VM. Each normal case waits 30 seconds.
func TestGoClientLongModelAndToolWait(t *testing.T) {
	for _, kind := range []string{"model", "tool"} {
		for _, cancelWait := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancel=%v", kind, cancelWait), func(t *testing.T) {
				t.Parallel()
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				defer cancel()
				entered := make(chan struct{}, 1)
				models := &fixtureModels{generate: func(ctx context.Context, q model.Request) (model.Response, error) {
					if kind == "model" {
						select {
						case entered <- struct{}{}:
						default:
						}
						timer := time.NewTimer(30 * time.Second)
						defer timer.Stop()
						select {
						case <-timer.C:
						case <-ctx.Done():
							return model.Response{}, ctx.Err()
						}
					} else {
						hasTool := false
						for _, raw := range q.Messages {
							var m struct{ Role string }
							_ = json.Unmarshal(raw, &m)
							if m.Role == "tool" {
								hasTool = true
							}
						}
						if !hasTool {
							args, _ := json.Marshal(map[string]string{"code": `const pending=new Promise(resolve=>setTimeout(resolve,30000));notifyLongWait();await pending;console.log("long tool completed");`})
							message, _ := json.Marshal(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "long-tool", "type": "function", "function": map[string]string{"name": "run_js", "arguments": string(args)}}}})
							return model.Response{Message: message, FinishReason: "tool_calls"}, nil
						}
					}
					return model.Response{Message: json.RawMessage(`{"role":"assistant","content":"long wait completed"}`), FinishReason: "stop"}, nil
				}}
				profile := stdlib.Profile{Initialize: func(_ context.Context, b *stdlib.Bindings) (func(), error) {
					return nil, b.Runtime().Set("notifyLongWait", func() {
						select {
						case entered <- struct{}{}:
						default:
						}
					})
				}}
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
				started, err := client.Spawn(ctx, agent.SpawnRequest{Config: taskConfig(), Instruction: textPointer("sustained wait")})
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if cancelWait {
					if _, err := client.CancelTask(ctx, agent.CancelTaskRequest{AgentID: started.AgentID, CommandID: started.CommandID}); err != nil {
						t.Fatal(err)
					}
				}
				result, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: started.AgentID, CommandID: started.CommandID})
				if err != nil {
					t.Fatal(err)
				}
				if cancelWait {
					if result.State != "cancelled" {
						t.Fatalf("cancel result=%+v", result)
					}
				} else if result.State != "completed" || result.Answer != "long wait completed" {
					t.Fatalf("long wait result=%+v", result)
				}
			})
		}
	}
}
