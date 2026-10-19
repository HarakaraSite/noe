//go:build linux

package process

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestOwnerSIGKILLCleansTargetGroupAndSupervisor(t *testing.T) {
	executable := testExecutable(t)
	dir := t.TempDir()
	targetState := dir + "/target-state"
	ownerState := dir + "/owner-state"
	owner := exec.Command(executable, "-test.run=^TestProcessOwnerHelper$")
	owner.Env = append(os.Environ(),
		ownerModeEnv+"=1",
		"NOE_PROCESS_TEST_STATE="+targetState,
		"NOE_PROCESS_TEST_OWNER_STATE="+ownerState,
	)
	owner.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	owner.Stdout = io.Discard
	owner.Stderr = io.Discard
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if owner.Process != nil {
			_ = syscall.Kill(-owner.Process.Pid, syscall.SIGKILL)
			_, _ = owner.Process.Wait()
		}
	}()

	ownerPIDs := waitForPIDs(t, ownerState, 3)
	targetPIDs := waitForPIDs(t, targetState, 2)
	if ownerPIDs[0] != owner.Process.Pid {
		t.Fatalf("owner process state PID %d, started PID %d", ownerPIDs[0], owner.Process.Pid)
	}
	if ownerPIDs[1] != targetPIDs[0] {
		t.Fatalf("owner target PID %d, target helper PID %d", ownerPIDs[1], targetPIDs[0])
	}
	if ownerPIDs[2] <= 0 {
		t.Fatalf("invalid supervisor PID %d", ownerPIDs[2])
	}
	if targetPIDs[1] <= 0 {
		t.Fatalf("invalid grandchild PID %d", targetPIDs[1])
	}

	if err := syscall.Kill(-owner.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL owner group: %v", err)
	}
	if err := owner.Wait(); err == nil {
		t.Fatal("owner unexpectedly exited successfully after SIGKILL")
	}
	owner.Process = nil

	for _, pid := range []int{targetPIDs[0], targetPIDs[1], ownerPIDs[2]} {
		waitForProcessGone(t, pid)
	}
}

func TestStartupCancellationCleansSpawnedTargetAndSupervisor(t *testing.T) {
	executable := testExecutable(t)
	dir := t.TempDir()
	targetState := dir + "/target-state"
	gateState := dir + "/start-gate"
	ctx, cancel := context.WithCancel(context.Background())
	type startResult struct {
		child *Child
		err   error
	}
	started := make(chan startResult, 1)
	go func() {
		child, err := Start(ctx, Config{
			Command:           executable,
			Args:              []string{"-test.run=^TestProcessTargetHelper$"},
			Env:               []string{targetModeEnv + "=leak", "NOE_PROCESS_TEST_STATE=" + targetState},
			testStartGatePath: gateState,
		})
		started <- startResult{child: child, err: err}
	}()

	completed := false
	t.Cleanup(func() {
		cancel()
		if completed {
			return
		}
		select {
		case result := <-started:
			if result.child != nil {
				_ = result.child.Close()
			}
		case <-time.After(5 * time.Second):
			t.Error("Start did not finish during cleanup after cancellation")
		}
	})

	gatePIDs := waitForPIDs(t, gateState, 2)
	targetPIDs := waitForPIDs(t, targetState, 2)
	if gatePIDs[1] != targetPIDs[0] {
		t.Fatalf("startup gate target PID %d, target helper PID %d", gatePIDs[1], targetPIDs[0])
	}
	cancel()
	result := <-started
	completed = true
	if result.child != nil {
		_ = result.child.Close()
		t.Fatalf("Start returned a child during canceled startup: PID %d", result.child.PID)
	}
	if !errors.Is(result.err, context.Canceled) {
		t.Fatalf("Start during startup cancellation error = %v, want context.Canceled", result.err)
	}
	for _, pid := range []int{gatePIDs[0], gatePIDs[1], targetPIDs[1]} {
		waitForProcessGone(t, pid)
	}
}

func TestSupervisorFailureKillsTargetGroup(t *testing.T) {
	executable := testExecutable(t)
	targetState := t.TempDir() + "/target-state"
	child, err := Start(context.Background(), Config{
		Command: executable,
		Args:    []string{"-test.run=^TestProcessTargetHelper$"},
		Env:     []string{targetModeEnv + "=leak", "NOE_PROCESS_TEST_STATE=" + targetState},
	})
	if err != nil {
		t.Fatal(err)
	}
	targetPIDs := waitForPIDs(t, targetState, 2)
	if targetPIDs[0] != child.PID {
		t.Fatalf("target state PID %d, Child.PID %d", targetPIDs[0], child.PID)
	}
	if err := syscall.Kill(child.supervisorPID, syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL supervisor: %v", err)
	}
	select {
	case <-child.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("supervision failure did not finish Child.Done")
	}
	if _, err := child.Wait(); err == nil {
		t.Fatal("Wait succeeded after supervisor SIGKILL without a result")
	}
	if err := child.Close(); err == nil {
		t.Fatal("Close succeeded after supervisor SIGKILL")
	}
	for _, pid := range []int{child.PID, targetPIDs[1], child.supervisorPID} {
		waitForProcessGone(t, pid)
	}
}

func TestProcessDescriptorAndResidentCost(t *testing.T) {
	executable := testExecutable(t)
	baselineFDs := countOpenFDs(t)
	child, err := Start(context.Background(), Config{
		Command: executable,
		Args:    []string{"-test.run=^TestProcessTargetHelper$"},
		Env:     []string{targetModeEnv + "=hold"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := bufio.NewReader(child.Stdout).ReadString('\n'); err != nil || ready != "ready\n" {
		t.Fatalf("active target ready = %q, %v", ready, err)
	}
	if child.supervisorPID <= 0 || child.supervisorPID == child.PID {
		t.Fatalf("target PID %d, supervisor PID %d", child.PID, child.supervisorPID)
	}
	for _, pid := range []int{child.PID, child.supervisorPID} {
		alive, err := linuxProcessAlive(pid)
		if err != nil || !alive {
			t.Fatalf("active process %d alive=%v, err=%v", pid, alive, err)
		}
	}
	supervisorRSS := readRSS(t, child.supervisorPID)
	targetRSS := readRSS(t, child.PID)
	t.Logf("active POSIX spawn: 2 extra processes; supervisor RSS=%d KiB, target RSS=%d KiB", supervisorRSS, targetRSS)
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	for _, pid := range []int{child.PID, child.supervisorPID} {
		waitForProcessGone(t, pid)
	}
	if _, err := child.Stdin.Write([]byte("after-close")); err == nil {
		t.Fatal("stdin remained open after Child.Close")
	}

	for i := 0; i < 8; i++ {
		short, err := Start(context.Background(), Config{
			Command: executable,
			Args:    []string{"-test.run=^TestProcessTargetHelper$"},
			Env:     []string{targetModeEnv + "=exit"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := short.Wait(); err != nil {
			t.Fatal(err)
		}
		if err := short.Close(); err != nil {
			t.Fatal(err)
		}
	}
	runtime.GC()
	afterFDs := countOpenFDs(t)
	if afterFDs > baselineFDs+1 {
		t.Fatalf("file descriptors grew from %d to %d after repeated Start/Close", baselineFDs, afterFDs)
	}
	t.Logf("process pipe/supervisor fd cleanup: baseline=%d open descriptors, after 9 Start/Close cycles=%d", baselineFDs, afterFDs)
}

func TestSupervisorPipesAreNotInheritedByTarget(t *testing.T) {
	executable := testExecutable(t)
	child, err := Start(context.Background(), Config{
		Command: executable,
		Args:    []string{"-test.run=^TestProcessTargetHelper$"},
		Env:     []string{targetModeEnv + "=fd-check"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := bufio.NewReader(child.Stdout).ReadString('\n'); err != nil || got != "fds-clean\n" {
		t.Fatalf("target fd check = %q, %v", got, err)
	}
	result, err := child.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if result.Code == nil || *result.Code != 0 {
		t.Fatalf("fd check result = %#v", result)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
}

func countOpenFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func readRSS(t *testing.T, pid int) int64 {
	t.Helper()
	file, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "VmRSS:" {
			value, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return value
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Fatalf("VmRSS missing for process %d", pid)
	return 0
}

func waitForPIDs(t *testing.T, path string, count int) []int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			fields := strings.Fields(string(data))
			if len(fields) == count {
				pids := make([]int, count)
				valid := true
				for i, field := range fields {
					pids[i], err = strconv.Atoi(field)
					if err != nil || pids[i] <= 0 {
						valid = false
						break
					}
				}
				if valid {
					return pids
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d PIDs in %s", count, path)
	return nil
}

func waitForProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		alive, err := linuxProcessAlive(pid)
		if err != nil {
			t.Fatal(err)
		}
		if !alive {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d remained alive after owner death", pid)
}

func linuxProcessAlive(pid int) (bool, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	// A process can disappear after procfs lookup but before the stat read.
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// The command name is parenthesized and may contain spaces. Fields after
	// its final ')' begin with state (field 3); zombies no longer execute.
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return false, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) == 0 {
		return false, fmt.Errorf("missing state in /proc/%d/stat", pid)
	}
	return fields[0] != "Z" && fields[0] != "X", nil
}
