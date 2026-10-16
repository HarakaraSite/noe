//go:build e2e && linux

package e2e_test

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAgentCLITimeoutStopsGeneratedProcessGroup(t *testing.T) {
	work := t.TempDir()
	server := agentAPIFixture(t, func(context.Context, agentChatRequest) agentModelReply {
		code := `import {writeFileSync} from "node:fs";
import {execFile} from "node:child_process";
writeFileSync("root.pid",String(process.pid));
await execFile("sh",["-c",'echo $$ > "$1"; sleep 60 & echo $! > "$2"; wait',"runner","shell.pid","grandchild.pid"]);`
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("wait", code)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	result := runCLI(t, work, agentCLIEnv(t, server.URL), "eval", "--timeout", "500ms", agentScript("run a long child process", "{}"))
	requireExit(t, result, 124)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	root := agentPIDFile(t, ctx, filepath.Join(work, "root.pid"))
	defer syscall.Kill(-root, syscall.SIGKILL)
	shell := agentPIDFile(t, ctx, filepath.Join(work, "shell.pid"))
	child := agentPIDFile(t, ctx, filepath.Join(work, "grandchild.pid"))
	if err := syscall.Kill(root, 0); err != syscall.ESRCH {
		t.Errorf("generated NOE was not reaped: pid=%d error=%v", root, err)
	}
	requireAgentPIDsStopped(t, shell, child)
}

func TestAgentParentTimeoutStopsNestedProcessGroups(t *testing.T) {
	work := t.TempDir()
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		code := `import {writeFileSync} from "node:fs";
writeFileSync("parent.pid",String(process.pid));
console.log(await createAgent({name:"nested"}).run("nested child"));`
		if agentUser(req) == "nested child" {
			code = `import {writeFileSync} from "node:fs";
import {execFile} from "node:child_process";
writeFileSync("child.pid",String(process.pid));
await execFile("sh",["-c",'echo $$ > "$1"; sleep 60 & echo $! > "$2"; wait',"runner","shell.pid","grandchild.pid"]);`
		}
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("nested-work", code)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	result := runCLI(t, work, agentCLIEnv(t, server.URL), "eval", "--timeout", "1s", agentScript("nested parent", "{maxSteps:3}"))
	requireExit(t, result, 124)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	parent := agentPIDFile(t, ctx, filepath.Join(work, "parent.pid"))
	child := agentPIDFile(t, ctx, filepath.Join(work, "child.pid"))
	defer syscall.Kill(-parent, syscall.SIGKILL)
	defer syscall.Kill(-child, syscall.SIGKILL)
	shell := agentPIDFile(t, ctx, filepath.Join(work, "shell.pid"))
	grandchild := agentPIDFile(t, ctx, filepath.Join(work, "grandchild.pid"))
	for _, pid := range []int{parent, child} {
		if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
			t.Errorf("nested generated NOE was not reaped: pid=%d error=%v", pid, err)
		}
	}
	requireAgentPIDsStopped(t, shell, grandchild)
}
