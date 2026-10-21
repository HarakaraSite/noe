//go:build e2e && linux

package e2e_test

import (
	"context"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These helpers inspect and clean up OS subprocesses owned by a managed
// execution. JavaScript tasks share the CLI process and must not be targeted.
func agentPIDFile(t *testing.T, ctx context.Context, name string) int {
	t.Helper()
	data := waitAgentFile(t, ctx, name)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid OS subprocess PID fixture %s: %q", name, data)
	}
	return pid
}

func waitAgentFile(t *testing.T, ctx context.Context, name string) []byte {
	t.Helper()
	for ctx.Err() == nil {
		data, err := os.ReadFile(name)
		if err == nil {
			return data
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("managed execution fixture did not publish %s: %v", name, ctx.Err())
	return nil
}

func agentPIDRunning(pid int) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if os.IsNotExist(err) {
		return false
	}
	if err == nil {
		end := strings.LastIndex(string(data), ") ")
		if end >= 0 && len(data) > end+2 && (data[end+2] == 'Z' || data[end+2] == 'X') {
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
			t.Errorf("managed OS subprocess remains running: %d", pid)
		}
	}
}
