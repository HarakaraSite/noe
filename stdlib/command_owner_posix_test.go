//go:build !windows

package stdlib

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Called only on Linux; the runtime-specific proc format stays out of the API.
func checkCommandOwnerDeath(t *testing.T, ctx context.Context, consumer, helper string) {
	t.Helper()
	state := filepath.Join(t.TempDir(), "target-pids")
	cmd := exec.CommandContext(ctx, consumer)
	cmd.Env = consumerEnvironment(map[string]string{"NOE_TEST_SUPERVISOR": helper, "NOE_TEST_INIT_FILE": "", "NOE_TEST_LEAK_STATE": state, "NOE_TEST_SKIP_DISPATCH": ""})
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	var pids []int
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(state)
		fields := strings.Fields(string(data))
		if len(fields) == 2 {
			for _, field := range fields {
				pid, err := strconv.Atoi(field)
				if err != nil || pid <= 0 {
					t.Fatalf("invalid pid file %q", data)
				}
				pids = append(pids, pid)
			}
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(pids) != 2 {
		t.Fatal("command did not create target/descendant state")
	}
	// Capture the helper PID before killing the owner so it is checked too.
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pids[0]))
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))
	if len(fields) < 2 {
		t.Fatalf("invalid proc stat %q", data)
	}
	supervisor, err := strconv.Atoi(fields[1])
	if err != nil || supervisor <= 0 {
		t.Fatalf("invalid helper pid %q", fields[1])
	}
	pids = append(pids, supervisor)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	deadline = time.Now().Add(5 * time.Second)
	for _, pid := range pids {
		for {
			data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))
			if len(fields) > 0 && fields[0] == "Z" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("process %d survived owner SIGKILL with helper %s", pid, helper)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}
