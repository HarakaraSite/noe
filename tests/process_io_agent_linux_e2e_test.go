//go:build e2e && linux

package e2e_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestProcessIOAgentRunInterruptReapsGeneratedProcessTree(t *testing.T) {
	work := t.TempDir()
	rootPIDPath := filepath.Join(work, "agent-run-js.pid")
	shellPIDPath := filepath.Join(work, "agent-shell.pid")
	grandchildPIDPath := filepath.Join(work, "agent-grandchild.pid")
	defer func() {
		if data, err := os.ReadFile(rootPIDPath); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}
		processIOKillPIDFiles(shellPIDPath, grandchildPIDPath)
	}()

	var modelCalls atomic.Int32
	server := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
		if got := agentUser(request); got != "start a process tree and leave it running" {
			t.Errorf("unexpected fixture instruction %q", got)
		}
		if modelCalls.Add(1) != 1 {
			t.Errorf("unexpected additional model call")
			return agentModelReply{Content: "unexpected second model call"}
		}
		code := `import {writeFileSync} from "node:fs";
import {spawn} from "noe:process";
writeFileSync("agent-run-js.pid",String(process.pid));
await spawn("sh",["-c",'echo $$ > "$1"; sleep 60 & echo $! > "$2"; wait',"agent-process-io","agent-shell.pid","agent-grandchild.pid"],{stdin:"ignore",stdout:"ignore",stderr:"ignore"});
await new Promise(()=>{});`
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("process-tree", code)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	source := `import {spawn_agent,observe_agent,interrupt_agent} from "noe:agent";
import {readFile} from "node:fs/promises";
const spawned=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto",name:"process-io"},"start a process tree and leave it running");
const pause=()=>new Promise(resolve=>setTimeout(resolve,2));
let snapshot;
for(let i=0;i<2500;i++){snapshot=await observe_agent({agentId:spawned.agentId});if(snapshot.currentRunId)break;await pause();}
if(!snapshot?.currentRunId)throw Error("managed task did not start a run");
const runId=snapshot.currentRunId;
let filesReady=false;
for(let i=0;i<2500;i++){try{await readFile("agent-run-js.pid");await readFile("agent-shell.pid");await readFile("agent-grandchild.pid");filesReady=true;break}catch{await pause();}}
if(!filesReady)throw Error("generated process tree did not publish all PIDs");
const accepted=await interrupt_agent({runId});
if(accepted.status!=="accepted"||accepted.target!=="run"||accepted.runId!==runId)throw Error("run interrupt was not accepted: "+JSON.stringify(accepted));
let terminal;
for(let i=0;i<2500;i++){terminal=await observe_agent({agentId:spawned.agentId});if(terminal.state==="stopped"&&terminal.lastResult?.state==="cancelled")break;await pause();}
if(terminal?.state!=="stopped"||terminal.reason!=="task_interrupted"||terminal.lastResult?.state!=="cancelled"||terminal.lastResult?.runId!==runId)throw Error("interrupted task did not settle: "+JSON.stringify(terminal));
console.log(JSON.stringify({runId,accepted,terminal}));`
	result := runCLI(t, work, env, "eval", "--timeout=8s", source)
	requireExit(t, result, 0)
	if result.stderr != "" || modelCalls.Load() != 1 {
		t.Fatalf("agent process-tree run: %+v model calls=%d", result, modelCalls.Load())
	}
	var outcome struct {
		RunID    string `json:"runId"`
		Accepted struct {
			Target string `json:"target"`
			Status string `json:"status"`
			RunID  string `json:"runId"`
		} `json:"accepted"`
		Terminal struct {
			State      string `json:"state"`
			Reason     string `json:"reason"`
			CurrentRun string `json:"currentRunId"`
			LastResult struct {
				RunID string `json:"runId"`
				State string `json:"state"`
				Error string `json:"error"`
			} `json:"lastResult"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &outcome); err != nil {
		t.Fatalf("agent interruption output is not JSON: %v\n%s", err, result.stdout)
	}
	if outcome.RunID == "" || outcome.Accepted.Status != "accepted" || outcome.Accepted.Target != "run" || outcome.Accepted.RunID != outcome.RunID || outcome.Terminal.State != "stopped" || outcome.Terminal.Reason != "task_interrupted" || outcome.Terminal.CurrentRun != "" || outcome.Terminal.LastResult.RunID != outcome.RunID || outcome.Terminal.LastResult.State != "cancelled" {
		t.Fatalf("agent task terminal state: %+v", outcome)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rootPID := agentPIDFile(t, ctx, rootPIDPath)
	shellPID := agentPIDFile(t, ctx, shellPIDPath)
	grandchildPID := agentPIDFile(t, ctx, grandchildPIDPath)
	if agentPIDRunning(rootPID) {
		t.Errorf("generated run_js process remains alive: %d", rootPID)
	}
	requireAgentPIDsStopped(t, shellPID, grandchildPID)
}

func TestProcessIOAgentParentCancellationReapsSpawnTreesWithInheritedAndIgnoredPipes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		stdout    string
		stderr    string
		timeout   bool
		exitCode  int
		wantError string
	}{
		{name: "timeout with inherited output", stdout: "inherit", stderr: "inherit", timeout: true, exitCode: 124, wantError: "execution timed out after 2s"},
		{name: "SIGINT with ignored output", stdout: "ignore", stderr: "ignore", exitCode: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := t.TempDir()
			rootPIDPath := filepath.Join(work, "parent-run-js.pid")
			shellPIDPath := filepath.Join(work, "parent-shell.pid")
			grandchildPIDPath := filepath.Join(work, "parent-grandchild.pid")
			defer processIOKillPIDFiles(rootPIDPath, shellPIDPath, grandchildPIDPath)

			var modelCalls atomic.Int32
			server := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
				if got := agentUser(request); got != "start a process tree and leave it running" {
					t.Errorf("unexpected fixture instruction %q", got)
				}
				if modelCalls.Add(1) != 1 {
					t.Errorf("unexpected additional model call")
					return agentModelReply{Content: "unexpected second model call"}
				}
				code := `import {writeFileSync} from "node:fs";
import {spawn} from "noe:process";
writeFileSync("parent-run-js.pid",String(process.pid));
await spawn("sh",["-c",'echo $$ > "$1"; sleep 60 & echo $! > "$2"; wait',"parent-process-io","parent-shell.pid","parent-grandchild.pid"],{stdin:"ignore",stdout:` + strconv.Quote(tc.stdout) + `,stderr:` + strconv.Quote(tc.stderr) + `});
await new Promise(()=>{});`
				return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("process-tree", code)}}}, FinishReason: "tool_calls"}
			})
			defer server.Close()
			env := agentCLIEnv(t, server.URL)
			env["NOE_AGENT_TRACE"] = ""
			parentSource := `import {run_agent} from "noe:agent";
await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",name:"process-io-parent"},"start a process tree and leave it running");
console.log("task ended");`
			args := []string{"eval", parentSource}
			if tc.timeout {
				args = []string{"eval", "--timeout=2s", parentSource}
			}
			command := startProcessIOAgentCLI(t, work, env, args...)
			waitCtx, stopWait := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			rootPID := agentPIDFile(t, waitCtx, rootPIDPath)
			shellPID := agentPIDFile(t, waitCtx, shellPIDPath)
			grandchildPID := agentPIDFile(t, waitCtx, grandchildPIDPath)
			stopWait()
			if !tc.timeout {
				if err := command.cmd.Process.Signal(os.Interrupt); err != nil {
					t.Fatal(err)
				}
			}
			waitedErr := command.wait(t, 5*time.Second)
			if ctxErr := command.ctx.Err(); ctxErr != nil {
				t.Fatalf("external safety timeout fired: %v", ctxErr)
			}
			var exitErr *exec.ExitError
			if !errors.As(waitedErr, &exitErr) || exitErr.ExitCode() != tc.exitCode {
				t.Fatalf("parent cancellation exit=%v stdout=%q stderr=%q", waitedErr, command.stdout.String(), command.stderr.String())
			}
			if tc.wantError != "" && !strings.Contains(command.stderr.String(), tc.wantError) {
				t.Fatalf("timeout diagnostic missing %q: %s", tc.wantError, command.stderr.String())
			}
			if !tc.timeout && (command.stderr.String() == "" || strings.Contains(command.stderr.String(), "execution timed out")) {
				t.Fatalf("SIGINT was not reported as an interrupt: %s", command.stderr.String())
			}
			if modelCalls.Load() != 1 {
				t.Fatalf("model calls=%d, want the single tool-dispatch request", modelCalls.Load())
			}
			if agentPIDRunning(rootPID) {
				t.Errorf("generated run_js process remains alive: %d", rootPID)
			}
			requireAgentPIDsStopped(t, shellPID, grandchildPID)
		})
	}
}

type processIOAgentCLI struct {
	ctx    context.Context
	cancel context.CancelFunc
	cmd    *exec.Cmd
	done   chan error
	stdout *synchronizedCapture
	stderr *synchronizedCapture
	waited bool
}

func startProcessIOAgentCLI(t *testing.T, work string, env map[string]string, args ...string) *processIOAgentCLI {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Second)
	cmd := exec.CommandContext(ctx, cliPath, args...)
	cmd.Dir = work
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = envWith(cmd.Env, key, value)
	}
	process := &processIOAgentCLI{
		ctx: ctx, cancel: cancel, cmd: cmd, done: make(chan error, 1),
		stdout: newSynchronizedCapture(), stderr: newSynchronizedCapture(),
	}
	cmd.Stdout, cmd.Stderr = process.stdout, process.stderr
	cmd.Stdin = strings.NewReader("")
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !process.waited {
			_ = cmd.Process.Kill()
			<-process.done
		}
		cancel()
	})
	go func() { process.done <- cmd.Wait() }()
	return process
}

func (process *processIOAgentCLI) wait(t *testing.T, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-process.done:
		process.waited = true
		return err
	case <-time.After(timeout):
		_ = process.cmd.Process.Kill()
		<-process.done
		process.waited = true
		t.Fatalf("CLI did not return after process-tree cancellation\nstdout:\n%s\nstderr:\n%s", process.stdout.String(), process.stderr.String())
		return nil
	}
}
