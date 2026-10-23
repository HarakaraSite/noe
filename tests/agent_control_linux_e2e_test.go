//go:build e2e && linux

package e2e_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgentHumanRunInterruptReapsNestedProcessGroups(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	work := t.TempDir()
	defer processIOKillPIDFiles(filepath.Join(work, "shell.pid"), filepath.Join(work, "grandchild.pid"))
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		code := `// Delegate to a child task that runs a process group.
import {run_agent} from "noe:agent";
import {writeFileSync} from "node:fs";
writeFileSync("parent-ready","ready");
await run_agent({name:"nested"},"nested child");`
		if agentUser(req) == "nested child" {
			code = `// Run a shell with a long-lived descendant.
import {writeFileSync} from "node:fs";
import {execFile} from "node:child_process";
writeFileSync("child-ready","ready");
await execFile("sh",["-c",'echo $$ > "$1"; sleep 60 & echo $! > "$2"; wait',"runner","shell.pid","grandchild.pid"]);`
		}
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("nested-control", code)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	source := `import {run_agent} from "noe:agent";const s=await run_agent({provider:"openrouter",model:"fixture/model",name:"controlled-parent",effort:"auto"},"parent task");console.log(s.lastResult.state);`
	cmd := exec.CommandContext(ctx, cliPath, "eval", source)
	cmd.Dir, cmd.Env = work, os.Environ()
	for key, value := range env {
		cmd.Env = envWith(cmd.Env, key, value)
	}
	var out, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diagnostic
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer cancel()
	waitAgentFile(t, ctx, filepath.Join(work, "parent-ready"))
	waitAgentFile(t, ctx, filepath.Join(work, "child-ready"))
	shell := agentPIDFile(t, ctx, filepath.Join(work, "shell.pid"))
	grandchild := agentPIDFile(t, ctx, filepath.Join(work, "grandchild.pid"))
	snapshot := awaitControlAgent(t, ctx, work, env, func(s controlAgentSnapshot) bool { return s.Name == "controlled-parent" && s.CurrentRunID != "" })
	result := runCLI(t, work, env, "interrupt", "--run", snapshot.CurrentRunID)
	if result.exitCode != 0 {
		t.Fatalf("interrupt failed: %+v", result)
	}
	select {
	case err := <-done:
		if err != nil || strings.TrimSpace(out.String()) != "cancelled" {
			t.Fatalf("owner termination: %v stdout=%q stderr=%q", err, &out, &diagnostic)
		}
	case <-ctx.Done():
		t.Fatal("run interrupt did not settle owner")
	}
	requireAgentPIDsStopped(t, shell, grandchild)
}
