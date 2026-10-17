//go:build e2e && linux

package e2e_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"noe/internal/agent"
)

func TestProcessIOBlockedStdoutWriteCancelsOnSIGINT(t *testing.T) {
	work := t.TempDir()
	source := `import {stdout} from "noe:io";
import {writeFileSync} from "node:fs";
writeFileSync("stream-ready", "ready");
const writer=stdout.getWriter(), chunk=new Uint8Array(64*1024);
while(true) await writer.write(chunk);`
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliPath, "eval", "--timeout=5s", source)
	cmd.Dir = work
	cmd.Stdin = strings.NewReader("")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = stdout // Keep the unread pipe open to apply real backpressure.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	readyCtx, stopReady := context.WithTimeout(context.Background(), 3*time.Second)
	for readyCtx.Err() == nil {
		if _, err := os.Stat(filepath.Join(work, "stream-ready")); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if readyCtx.Err() != nil {
		stopReady()
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("script did not enter stream output: %v\nstderr:\n%s", readyCtx.Err(), stderr.String())
	}
	stopReady()
	// Give the producer time to fill the unread OS pipe and block in Write.
	time.Sleep(100 * time.Millisecond)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
		<-done
		t.Fatal(err)
	}
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			t.Fatalf("SIGINT output cancellation exit=%v stderr=%q", err, stderr.String())
		}
		if stderr.Len() == 0 {
			t.Fatal("SIGINT output cancellation did not report an interrupted execution")
		}
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("CLI did not stop after SIGINT during blocked stdout write\nstderr:\n%s", stderr.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("external safety timeout fired: %v", ctx.Err())
	}
}

func TestProcessIOBlockedStdoutWriteCancelsOnTimeout(t *testing.T) {
	work := t.TempDir()
	source := `import {stdout} from "noe:io";
import {writeFileSync} from "node:fs";
writeFileSync("stream-ready", "ready");
const writer=stdout.getWriter(), chunk=new Uint8Array(64*1024);
while(true) await writer.write(chunk);`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliPath, "eval", "--timeout=300ms", source)
	cmd.Dir = work
	cmd.Stdin = strings.NewReader("")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = stdout // Keep the unread pipe open to apply real backpressure.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	readyCtx, stopReady := context.WithTimeout(context.Background(), 3*time.Second)
	for readyCtx.Err() == nil {
		if _, err := os.Stat(filepath.Join(work, "stream-ready")); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if readyCtx.Err() != nil {
		stopReady()
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("script did not enter stream output: %v\nstderr:\n%s", readyCtx.Err(), stderr.String())
	}
	stopReady()
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 124 || !strings.Contains(stderr.String(), "execution timed out after 300ms") {
			t.Fatalf("blocked stdout timeout exit=%v stderr=%q", err, stderr.String())
		}
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Fatalf("CLI did not stop after timeout during blocked stdout write\nstderr:\n%s", stderr.String())
	}
	if ctx.Err() != nil {
		t.Fatalf("external safety timeout fired: %v", ctx.Err())
	}
}

func TestProcessIOAgentRunnerCancellationReapsNoeProcessChildAndGrandchild(t *testing.T) {
	work := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	type outcome struct {
		result agent.Result
		err    error
	}
	done := make(chan outcome, 1)
	source := `import {writeFileSync} from "node:fs";
import {spawn} from "noe:process";
writeFileSync("root.pid",String(process.pid));
await spawn("sh",["-c",'echo $$ > "$1"; sleep 60 & echo $! > "$2"; wait',"process-io-runner","shell.pid","grandchild.pid"],{stdin:"ignore",stdout:"ignore",stderr:"ignore"});`
	runner := agent.Runner{Executable: cliPath, Cwd: work}
	go func() {
		result, err := runner.Run(ctx, source)
		done <- outcome{result: result, err: err}
	}()
	waitCtx, stopWait := context.WithTimeout(context.Background(), 8*time.Second)
	defer stopWait()
	rootPID := agentPIDFile(t, waitCtx, filepath.Join(work, "root.pid"))
	defer syscall.Kill(-rootPID, syscall.SIGKILL)
	shellPID := agentPIDFile(t, waitCtx, filepath.Join(work, "shell.pid"))
	grandchildPID := agentPIDFile(t, waitCtx, filepath.Join(work, "grandchild.pid"))
	cancel()
	select {
	case got := <-done:
		if got.err != nil || got.result.Reason != "cancelled" || got.result.ExitCode == 0 {
			t.Fatalf("Runner cancellation result=%+v error=%v", got.result, got.err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Runner did not terminate and reap its NOE process")
	}
	if err := syscall.Kill(rootPID, 0); err != syscall.ESRCH {
		t.Errorf("Runner did not reap NOE process %d: %v", rootPID, err)
	}
	requireAgentPIDsStopped(t, shellPID, grandchildPID)
}

func TestProcessIOWorkerTerminateStopsActiveSpawn(t *testing.T) {
	work := t.TempDir()
	shellPath := filepath.Join(work, "worker-shell.pid")
	grandchildPath := filepath.Join(work, "worker-grandchild.pid")
	defer processIOKillPIDFiles(shellPath, grandchildPath)
	workerSource := `import {parentPort} from "node:worker_threads";
import {readFile} from "node:fs/promises";
import {spawn} from "noe:process";
const shellPath=` + strconv.Quote(shellPath) + `, grandchildPath=` + strconv.Quote(grandchildPath) + `;
await spawn("sh",["-c",'echo $$ > "$1"; sleep 60 & echo $! > "$2"; wait',"process-io-worker",shellPath,grandchildPath],{stdin:"ignore",stdout:"ignore",stderr:"ignore"});
let childReady=false;
for(let i=0;i<5000;i++){try{await readFile(shellPath);await readFile(grandchildPath);childReady=true;break}catch{await new Promise(r=>setTimeout(r,1));}}
if(!childReady)throw Error("worker child pid files did not appear");
parentPort.postMessage("child-ready");
await new Promise(()=>{});`
	writeFixture(t, filepath.Join(work, "worker.js"), workerSource)
	main := `import {Worker} from "node:worker_threads";
const worker=new Worker("./worker.js");
const message=await new Promise((resolve,reject)=>{worker.on("message",resolve);worker.on("error",reject)});
if(message!=="child-ready")throw Error("worker child did not start");
await worker.terminate();
console.log("worker terminated");`
	got := runCLI(t, work, nil, "eval", main)
	requireExit(t, got, 0)
	if got.stdout != "worker terminated\n" || got.stderr != "" {
		t.Fatalf("Worker terminate output: %+v", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	shellPID := agentPIDFile(t, ctx, shellPath)
	grandchildPID := agentPIDFile(t, ctx, grandchildPath)
	requireAgentPIDsStopped(t, shellPID, grandchildPID)
}

func processIOKillPIDFiles(paths ...string) {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}
