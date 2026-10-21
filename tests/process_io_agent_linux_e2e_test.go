//go:build e2e && linux

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProcessIOAgentRunInterruptReapsGeneratedProcessTree(t *testing.T) {
	work := t.TempDir()
	shellPIDPath := filepath.Join(work, "agent-shell.pid")
	grandchildPIDPath := filepath.Join(work, "agent-grandchild.pid")
	defer processIOKillPIDFiles(shellPIDPath, grandchildPIDPath)

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
writeFileSync("agent-run-js-ready","ready");
await spawn("sh",["-c",'echo $$ > "$1"; sleep 60 & echo $! > "$2"; wait',"agent-process-io","agent-shell.pid","agent-grandchild.pid"],{stdin:"ignore",stdout:"ignore",stderr:"ignore"});
await new Promise(()=>{});`
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("process-tree", code)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	eventPath := filepath.Join(work, "events.jsonl")
	source := `import {spawn_agent,observe_agent,cancel_task,wait_task} from "noe:agent";
import {readFile} from "node:fs/promises";
const spawned=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto",name:"process-io"},"start a process tree and leave it running");
const commandId=spawned.commandId;
if(!commandId)throw Error("spawn did not return its accepted commandId");
const pause=()=>new Promise(resolve=>setTimeout(resolve,2));
let filesReady=false;
for(let i=0;i<2500;i++){try{await readFile("agent-run-js-ready");await readFile("agent-shell.pid");await readFile("agent-grandchild.pid");filesReady=true;break}catch{await pause();}}
if(!filesReady)throw Error("generated process tree did not publish all PIDs");
const receipt=await cancel_task({agentId:spawned.agentId,commandId});
const outcome=await wait_task({agentId:spawned.agentId,commandId});
const terminal=await observe_agent({agentId:spawned.agentId});
if(receipt.status!=="accepted"||receipt.commandId!==commandId)throw Error("task cancellation was not accepted: "+JSON.stringify(receipt));
if(outcome.state!=="cancelled"||outcome.commandId!==commandId)throw Error("cancelled task did not settle: "+JSON.stringify(outcome));
if(terminal?.state!=="stopped"||terminal.reason!=="task_interrupted"||terminal.lastResult?.state!=="cancelled"||terminal.lastResult?.commandId!==commandId)throw Error("interrupted task terminal state mismatch: "+JSON.stringify(terminal));
console.log(JSON.stringify({commandId,receipt,outcome,terminal}));`
	result := runCLI(t, work, env, "eval", "--timeout=8s", "--events="+eventPath, source)
	requireExit(t, result, 0)
	if result.stderr != "" || modelCalls.Load() != 1 {
		t.Fatalf("agent process-tree run: %+v model calls=%d", result, modelCalls.Load())
	}
	var outcome struct {
		CommandID string `json:"commandId"`
		Accepted  struct {
			Status    string `json:"status"`
			CommandID string `json:"commandId"`
		} `json:"receipt"`
		Outcome struct {
			State     string `json:"state"`
			CommandID string `json:"commandId"`
		} `json:"outcome"`
		Terminal struct {
			State      string `json:"state"`
			Reason     string `json:"reason"`
			LastResult struct {
				CommandID string `json:"commandId"`
				RunID     string `json:"runId"`
				State     string `json:"state"`
			} `json:"lastResult"`
		} `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &outcome); err != nil {
		t.Fatalf("agent interruption output is not JSON: %v\n%s", err, result.stdout)
	}
	if outcome.CommandID == "" || outcome.Accepted.Status != "accepted" || outcome.Accepted.CommandID != outcome.CommandID || outcome.Outcome.State != "cancelled" || outcome.Outcome.CommandID != outcome.CommandID || outcome.Terminal.State != "stopped" || outcome.Terminal.Reason != "task_interrupted" || outcome.Terminal.LastResult.CommandID != outcome.CommandID || outcome.Terminal.LastResult.State != "cancelled" {
		t.Fatalf("agent task terminal state: %+v", outcome)
	}
	events, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	acceptedEvent, cancelledEvent := false, false
	for _, line := range bytes.Split(events, []byte("\n")) {
		var event struct {
			Type      string `json:"type"`
			Target    string `json:"target"`
			CommandID string `json:"commandId"`
			RunID     string `json:"runId"`
			Status    string `json:"status"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		if event.Type == "interrupt.accepted" && event.Target == "task" && event.CommandID == outcome.CommandID {
			acceptedEvent = true
		}
		if event.Type == "agent.cancelled" && event.RunID == outcome.Terminal.LastResult.RunID && event.Status == "cancelled" {
			cancelledEvent = true
		}
	}
	if !acceptedEvent || !cancelledEvent {
		t.Fatalf("task cancellation lifecycle events missing: accepted=%v cancelled=%v", acceptedEvent, cancelledEvent)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	shellPID := agentPIDFile(t, ctx, shellPIDPath)
	grandchildPID := agentPIDFile(t, ctx, grandchildPIDPath)
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
			runReadyPath := filepath.Join(work, "parent-run-js-ready")
			shellPIDPath := filepath.Join(work, "parent-shell.pid")
			grandchildPIDPath := filepath.Join(work, "parent-grandchild.pid")
			defer processIOKillPIDFiles(shellPIDPath, grandchildPIDPath)

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
writeFileSync("parent-run-js-ready","ready");
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
			waitAgentFile(t, waitCtx, runReadyPath)
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
