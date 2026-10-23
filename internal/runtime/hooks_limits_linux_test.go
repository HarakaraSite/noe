//go:build linux

package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func TestHooksSharedProviderLimitReapsParallelRunJSProcess(t *testing.T) {
	cwd := t.TempDir()
	ready := filepath.Join(cwd, "child.pid")
	wait := fmt.Sprintf(`// Keep an owned child process alive until group cancellation.
const {spawn}=await import("noe:process");
const {writeFileSync}=await import("node:fs");
const child=await spawn("/bin/sh",["-c","exec sleep 60"],{stdin:"ignore",stdout:"ignore",stderr:"ignore"});
writeFileSync(%q,String(child.pid));
await child.exited;`, ready)
	trip := fmt.Sprintf(`// Wait for the parallel child, then consume the shared model budget.
import {run_agent} from "noe:agent";
const {existsSync}=await import("node:fs");
while(!existsSync(%q)) await new Promise(resolve=>setTimeout(resolve,5));
await run_agent({name:"budget-child"},"request beyond group budget");`, ready)
	var calls atomic.Int32
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		calls.Add(1)
		return hookToolReply(map[string]string{"wait": wait, "trip": trip}, "wait", "trip"), nil
	})
	source := `import {run_agent} from "noe:agent";
console.log(JSON.stringify(await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",executionLimits:{providerCalls:1}},"parallel cancellation fixture")));`
	out, _ := runLimitedHookFixture(t, cwd, source, p)
	var result limitedInstanceResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	assertLimitSettled(t, result, "providerCalls", 1)
	if calls.Load() != 1 || result.Execution.Counters.AgentStarts != 2 {
		t.Fatalf("provider admission counts: %s calls=%d", out, calls.Load())
	}
	data, err := os.ReadFile(ready)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		t.Fatalf("invalid PID %q %v", data, err)
	}
	// The run_js owner is killed before its separate process supervisor
	// observes pipe EOF and reaps this target. Retain the reap requirement,
	// but allow the supervisor to finish its asynchronous cleanup.
	processPath := filepath.Join("/proc", strconv.Itoa(pid))
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err := os.Stat(processPath)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			t.Fatalf("inspect owned process %d: %v", pid, err)
		}
		if time.Now().After(deadline) {
			stat, readErr := os.ReadFile(filepath.Join(processPath, "stat"))
			t.Fatalf("owned process %d still exists after cleanup deadline: stat=%q readErr=%v", pid, stat, readErr)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
