//go:build e2e && linux

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
)

func agentPIDFile(t *testing.T, ctx context.Context, name string) int {
	t.Helper()
	for ctx.Err() == nil {
		data, err := os.ReadFile(name)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("PID fixture did not start: %s: %v", name, ctx.Err())
	return 0
}

func agentPIDRunning(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if os.IsNotExist(err) {
		return false
	}
	if err == nil {
		end := strings.LastIndex(string(data), ") ")
		if end >= 0 && len(data) > end+2 && (data[end+2] == 'Z' || data[end+2] == 'X') {
			// Descendant zombies belong to the OS reaper. The directly owned NOE
			// process is separately required to be waited by Runner.Run.
			return false
		}
	}
	return syscall.Kill(pid, 0) == nil
}

func requireAgentPIDsStopped(t *testing.T, pids ...int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for _, pid := range pids {
		for agentPIDRunning(pid) && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if agentPIDRunning(pid) {
			t.Errorf("managed process remains running: %d", pid)
		}
	}
}

func TestAgentRunnerCancellationStopsProcessGroup(t *testing.T) {
	work := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	runner := agent.Runner{Executable: cliPath, Cwd: work}
	type outcome struct {
		result agent.Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := runner.Run(ctx, `import {writeFileSync} from "node:fs";
import {execFile} from "node:child_process";
writeFileSync("root.pid",String(process.pid));
await execFile("sh",["-c",'echo $$ > "$1"; sleep 60 & echo $! > "$2"; wait',"runner","shell.pid","grandchild.pid"]);`)
		done <- outcome{result, err}
	}()
	root := agentPIDFile(t, ctx, filepath.Join(work, "root.pid"))
	defer syscall.Kill(-root, syscall.SIGKILL)
	shell := agentPIDFile(t, ctx, filepath.Join(work, "shell.pid"))
	child := agentPIDFile(t, ctx, filepath.Join(work, "grandchild.pid"))
	cancel()
	select {
	case got := <-done:
		if got.err != nil || got.result.Reason != "cancelled" || got.result.ExitCode == 0 {
			t.Fatalf("cancel outcome: %+v error=%v", got.result, got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not terminate and wait its child")
	}
	if err := syscall.Kill(root, 0); err != syscall.ESRCH {
		t.Errorf("owned NOE process was not reaped: pid=%d error=%v", root, err)
	}
	requireAgentPIDsStopped(t, shell, child)
}

func TestAgentRunnerNormalExitStopsBackgroundDescendant(t *testing.T) {
	work := t.TempDir()
	runner := agent.Runner{Executable: cliPath, Cwd: work}
	result := runAgentJS(t, runner, `import {execFileSync} from "node:child_process";
execFileSync("sh",["-c",'sleep 60 </dev/null >/dev/null 2>&1 & echo $! > "$1"',"runner","background.pid"]);
console.log("completed");`)
	if result.Reason != "completed" || result.Stdout != "completed\n" {
		t.Fatalf("normal result: %+v", result)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pid := agentPIDFile(t, ctx, filepath.Join(work, "background.pid"))
	defer syscall.Kill(pid, syscall.SIGKILL)
	requireAgentPIDsStopped(t, pid)
}

func TestAgentRunnerExitDoesNotWaitForDescendantHeldPipes(t *testing.T) {
	work := t.TempDir()
	helper := filepath.Join(work, "noe-wrapper")
	// A NOE wrapper that exits while its descendant retains both captured FDs.
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nsleep 60 &\necho $! > inherited-pipes.pid\necho wrapper-done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	result := runAgentJS(t, agent.Runner{Executable: helper, Cwd: work}, `console.log("unused by wrapper");`)
	if result.Reason != "completed" || result.Stdout != "wrapper-done\n" {
		t.Fatalf("wrapper result: %+v", result)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	pid := agentPIDFile(t, ctx, filepath.Join(work, "inherited-pipes.pid"))
	defer syscall.Kill(pid, syscall.SIGKILL)
	requireAgentPIDsStopped(t, pid)
}
