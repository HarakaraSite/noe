//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
)

func TestAgentInstructionFilesRunAndEval(t *testing.T) {
	var calls atomic.Int32
	server := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
		calls.Add(1)
		var system string
		if err := json.Unmarshal(request.Messages[0].Content, &system); err != nil {
			t.Errorf("invalid system message: %v", err)
		}
		previous := len(agent.Instructions())
		if !strings.HasPrefix(system, agent.Instructions()) {
			t.Error("built-in instructions lost")
		}
		for _, marker := range []string{"COMMON_FIRST", "COMMON_SECOND", "PROJECT_FIRST", "PROJECT_SECOND", "EXPLICIT_SYSTEM"} {
			position := strings.Index(system, marker)
			if position < previous || strings.Count(system, marker) != 1 {
				t.Errorf("instruction missing, duplicated, or out of order: %s", marker)
			}
			previous = position + len(marker)
		}
		if strings.Contains(system, "WRONG_SCRIPT_WORKSPACE") {
			t.Error("resolved workspace from script path rather than CLI working directory")
		}
		return agentModelReply{Content: "instruction files verified"}
	})
	defer server.Close()
	config := t.TempDir()
	writeFixture(t, filepath.Join(config, "noe", "instructions", "20-shared.md"), "COMMON_SECOND")
	writeFixture(t, filepath.Join(config, "noe", "instructions", "10-shared.md"), "COMMON_FIRST")
	env := agentCLIEnv(t, server.URL)
	env["XDG_CONFIG_HOME"] = config
	source := `const am=await import("noe:agent"); const result=await am.run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",system:"EXPLICIT_SYSTEM"},"verify instruction loading"); if(result.lastResult?.state!=="completed") throw Error(JSON.stringify(result)); console.log(result.lastResult.answer);`
	for _, mode := range []string{"run", "eval"} {
		t.Run(mode, func(t *testing.T) {
			cwd := t.TempDir()
			writeFixture(t, filepath.Join(cwd, ".noe", "instructions", "20-shared.md"), "PROJECT_SECOND")
			writeFixture(t, filepath.Join(cwd, ".noe", "instructions", "10-shared.md"), "PROJECT_FIRST")
			writeFixture(t, filepath.Join(cwd, "scripts", ".noe", "instructions", "bad.md"), "WRONG_SCRIPT_WORKSPACE")
			path := filepath.Join(cwd, "scripts", "entry.mjs")
			writeFixture(t, path, source)
			args := []string{mode}
			switch mode {
			case "run":
				args = append(args, path)
			case "eval":
				args = append(args, source)
			}
			result := runCLI(t, cwd, env, args...)
			requireExit(t, result, 0)
			if !strings.Contains(result.stdout, "instruction files verified") {
				t.Fatalf("missing task result: stdout=%s stderr=%s", result.stdout, result.stderr)
			}
		})
	}
	if calls.Load() != 2 {
		t.Fatalf("provider calls=%d, want one per CLI mode", calls.Load())
	}
}
