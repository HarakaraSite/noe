//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContextObservationRunAndEval(t *testing.T) {
	server := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
		if !strings.Contains(request.RuntimeInfo, "10000") {
			t.Errorf("budget omitted from notification: %s", request.RuntimeInfo)
		}
		return agentModelReply{Content: "fixture complete"}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	source := `const am=await import("noe:agent"); await am.run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",contextBudget:{inputTokens:10000}},"fixture task");`
	for _, mode := range []string{"run", "eval"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "entry.mjs")
			writeFixture(t, path, source)
			args := []string{mode, "--observe", "--events=" + filepath.Join(dir, "events.jsonl")}
			switch mode {
			case "run":
				args = append(args, path)
			case "eval":
				args = append(args, source)
			}
			result := runCLI(t, dir, env, args...)
			requireExit(t, result, 0)
			if strings.Count(result.stdout, "\ncontext:") != 1 || strings.Contains(result.stdout, "budget=") || strings.Contains(result.stdout, "previous=") {
				t.Fatalf("context not observed: %s\n%s", result.stdout, result.stderr)
			}
			data, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			updates := 0
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var event struct {
					Type, Status string
					Context      struct {
						Budget   struct{ InputTokens int }
						Previous struct{ PromptTokens int }
					}
				}
				if err := json.Unmarshal([]byte(line), &event); err != nil {
					t.Fatal(err)
				}
				if event.Type == "context.updated" {
					updates++
					if event.Status != "completed" || event.Context.Budget.InputTokens != 10000 || event.Context.Previous.PromptTokens != 100 {
						t.Fatalf("context details missing from completed event: %s", line)
					}
				}
			}
			if updates != 1 {
				t.Fatalf("context updates=%d, want one", updates)
			}
		})
	}
}
