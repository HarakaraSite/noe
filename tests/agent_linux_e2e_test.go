//go:build e2e && linux

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentCLITimeoutStopsGeneratedProcessGroup(t *testing.T) {
	work := t.TempDir()
	server := agentAPIFixture(t, func(context.Context, agentChatRequest) agentModelReply {
		code := `import {writeFileSync} from "node:fs";
import {execFile} from "node:child_process";
writeFileSync("tool-ready","ready");
await execFile("sh",["-c",'echo $$ > "$1"; sleep 60 & echo $! > "$2"; wait',"runner","shell.pid","grandchild.pid"]);`
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("wait", code)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	result := runCLI(t, work, agentCLIEnv(t, server.URL), "eval", "--timeout", "500ms", agentScript("run a long child process", "{}"))
	requireExit(t, result, 124)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	shell := agentPIDFile(t, ctx, filepath.Join(work, "shell.pid"))
	child := agentPIDFile(t, ctx, filepath.Join(work, "grandchild.pid"))
	requireAgentPIDsStopped(t, shell, child)
}

func TestAgentParentTimeoutStopsNestedProcessGroups(t *testing.T) {
	work := t.TempDir()
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		code := `import {run_agent} from "noe:agent";
import {writeFileSync} from "node:fs";
writeFileSync("parent-ready","ready");
const child=await run_agent({name:"nested"},"nested child");console.log(child.lastResult.answer);`
		if agentUser(req) == "nested child" {
			code = `import {writeFileSync} from "node:fs";
import {execFile} from "node:child_process";
writeFileSync("child-ready","ready");
await execFile("sh",["-c",'echo $$ > "$1"; sleep 60 & echo $! > "$2"; wait',"runner","shell.pid","grandchild.pid"]);`
		}
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("nested-work", code)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	result := runCLI(t, work, agentCLIEnv(t, server.URL), "eval", "--timeout", "1s", agentScript("nested parent", "{maxSteps:3}"))
	requireExit(t, result, 124)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for _, marker := range []string{"parent-ready", "child-ready"} {
		if _, err := os.Stat(filepath.Join(work, marker)); err != nil {
			t.Fatalf("managed task did not reach %s: %v", marker, err)
		}
	}
	shell := agentPIDFile(t, ctx, filepath.Join(work, "shell.pid"))
	grandchild := agentPIDFile(t, ctx, filepath.Join(work, "grandchild.pid"))
	requireAgentPIDsStopped(t, shell, grandchild)
}
