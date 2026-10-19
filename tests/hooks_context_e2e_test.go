//go:build e2e

package e2e_test

import (
	"context"
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
			if !strings.Contains(result.stdout, "context: budget=10000") || !strings.Contains(result.stdout, "estimated=") || !strings.Contains(result.stdout, "previous=100 measured") {
				t.Fatalf("context not observed: %s\n%s", result.stdout, result.stderr)
			}
		})
	}
}
