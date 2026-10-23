//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestImportMapAgentSnapshotDelegationAndRootIsolation(t *testing.T) {
	work := t.TempDir()
	paths := map[string]string{}
	for _, suffix := range []string{"A", "B"} {
		paths[suffix] = filepath.Join(work, "maps", suffix+".json")
		writeFixture(t, paths[suffix], `{"imports":{"app/value":"../lib/`+suffix+`.mjs"}}`)
		writeFixture(t, filepath.Join(work, "lib", suffix+".mjs"), `export const value="`+suffix+`";`)
	}
	var started, calls atomic.Int32
	both := make(chan struct{})
	server := agentAPIFixture(t, func(ctx context.Context, req agentChatRequest) agentModelReply {
		calls.Add(1)
		instruction := agentUser(req)
		suffix := strings.TrimPrefix(strings.TrimPrefix(instruction, "parent-"), "child-")
		if suffix != "A" && suffix != "B" {
			t.Errorf("instruction=%q", instruction)
			return agentModelReply{Status: 500}
		}
		encoded, _ := json.Marshal(req)
		if strings.Contains(string(encoded), work) {
			t.Error("host import-map paths leaked to provider")
		}
		parent := strings.HasPrefix(instruction, "parent-")
		if len(req.Messages) == 2 {
			if parent {
				// The CLI and task bootstrap already loaded their snapshots. Later
				// run_js and grandchildren must not read this original configuration.
				var err error
				if suffix == "A" {
					err = os.WriteFile(paths[suffix], []byte(`{"imports":{}}`), 0600)
				} else {
					err = os.Remove(paths[suffix])
				}
				if err != nil {
					t.Error(err)
				}
				if started.Add(1) == 2 {
					close(both)
				}
				select {
				case <-both:
				case <-ctx.Done():
					return agentModelReply{Status: 500}
				}
			}
			code := `// Read the mapped library.
const {value}=await import("app/value");console.log(value);`
			if parent {
				code = `// Read the library and delegate with the same mapping.
import {run_agent} from "noe:agent";
const {value}=await import("app/value");const child=await run_agent({},"child-` + suffix + `");
if(child.lastResult.state!=="completed")throw Error(JSON.stringify(child));
console.log(value+child.lastResult.answer);`
			}
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("mapped-library", code)}}}, FinishReason: "tool_calls"}
		}
		expected := suffix
		if parent {
			expected += suffix
		}
		result := agentToolResult(t, req.Messages[len(req.Messages)-1])
		if result.ExitCode != 0 || result.Stdout != expected+"\n" {
			t.Errorf("%s tool result=%+v", instruction, result)
			return agentModelReply{Content: "FAILED"}
		}
		return agentModelReply{Content: expected}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	results := make([]cliResult, 2)
	var wg sync.WaitGroup
	for i, suffix := range []string{"A", "B"} {
		wg.Go(func() {
			source := `import {run_agent} from "noe:agent";const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"parent-` + suffix + `");console.log(result.lastResult.answer);`
			results[i] = runCLI(t, work, env, "eval", "--import-map", paths[suffix], source)
		})
	}
	wg.Wait()
	for i, want := range []string{"AA\n", "BB\n"} {
		requireExit(t, results[i], 0)
		if results[i].stdout != want || results[i].stderr != "" {
			t.Fatalf("root %d=%+v", i, results[i])
		}
	}
	if calls.Load() != 8 {
		t.Fatalf("model calls=%d want 8", calls.Load())
	}
}
