//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFakeNOE(t *testing.T, body string) string {
	t.Helper()
	executable := filepath.Join(t.TempDir(), "noe-fixture")
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	return executable
}

func TestRunnerRunTaskUsesPrivateBootstrapAndJSONInputs(t *testing.T) {
	executable := writeFakeNOE(t, `
if [ "$1" != "run" ]; then
	printf 'unexpected command: %s\n' "$1" >&2
	exit 90
fi
cat "$2"
`)
	config := json.RawMessage(`{"provider":"openrouter","model":"fixture/model\"; globalThis.injected = true; //","maxSteps":4}`)
	instruction := "first line\n\"; globalThis.injected = true; //"
	result, err := (Runner{Executable: executable}).RunTask(context.Background(), config, instruction)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "completed" || result.ExitCode != 0 {
		t.Fatalf("runner result = %#v, want completed exit 0", result)
	}
	if !strings.HasPrefix(result.Stdout, "import { executeTask } from \"noe:agent/task\";\n") {
		t.Fatalf("task bootstrap import missing: %q", result.Stdout)
	}
	if strings.Contains(result.Stdout, "createAgent") || strings.Count(result.Stdout, "import {") != 1 {
		t.Fatalf("task bootstrap contains public or duplicate imports: %q", result.Stdout)
	}
	configLiteral, _ := json.Marshal(string(config))
	instructionLiteral, _ := json.Marshal(instruction)
	if !strings.Contains(result.Stdout, "const config = JSON.parse("+string(configLiteral)+");") {
		t.Errorf("config was not embedded as JSON data: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "const instruction = "+string(instructionLiteral)+";") {
		t.Errorf("instruction was not embedded as a JS string: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "try { await executeTask(config, instruction); } catch (error) { console.error(error); }") {
		t.Errorf("task failure catch/report missing: %q", result.Stdout)
	}
}

func TestRunnerRunTaskRejectsInvalidConfigBeforeLaunching(t *testing.T) {
	started := filepath.Join(t.TempDir(), "started")
	executable := writeFakeNOE(t, `touch "$NOE_RUNNER_STARTED_FILE"
`)
	t.Setenv("NOE_RUNNER_STARTED_FILE", started)
	result, err := (Runner{Executable: executable}).RunTask(context.Background(), json.RawMessage(`{"model":`), "task")
	if err == nil || result.Reason != "failed" || result.ExitCode != -1 {
		t.Fatalf("invalid config result = %#v, error=%v", result, err)
	}
	if _, statErr := os.Stat(started); !os.IsNotExist(statErr) {
		t.Fatalf("runner launched for invalid config: stat error = %v", statErr)
	}
}

func TestRunnerCapturesOutputSourceCwdAndScopedEnvironment(t *testing.T) {
	for key, value := range map[string]string{
		"OPENROUTER_API_KEY":      "fake-openrouter-value",
		"OPENAI_API_KEY":          "fake-openai-value",
		"ANTHROPIC_API_KEY":       "fake-anthropic-value",
		"NOE_OPENROUTER_API_KEY":  "fake-noe-value",
		"NOE_AGENT_CONNECTION":    "stale-capability",
		"NOE_RUNNER_FIXTURE_MARK": "fixture-environment",
	} {
		t.Setenv(key, value)
	}
	cwd := t.TempDir()
	executable := writeFakeNOE(t, `
if [ "$1" != "run" ]; then
	printf 'unexpected command: %s\n' "$1" >&2
	exit 90
fi
printf 'cwd=%s\n' "$PWD"
printf 'connection=%s\n' "${NOE_AGENT_CONNECTION-unset}"
printf 'marker=%s\n' "${NOE_RUNNER_FIXTURE_MARK-unset}"
if env | grep -E '^(OPENROUTER_API_KEY|OPENAI_API_KEY|ANTHROPIC_API_KEY|NOE_OPENROUTER_API_KEY)=' >/dev/null; then
	printf 'provider key leaked\n' >&2
	exit 91
fi
if [ "${NOE_AGENT_CONNECTION-unset}" = "stale-capability" ]; then
	printf 'stale connection retained\n' >&2
	exit 92
fi
printf 'source-begin\n'
cat "$2"
printf 'source-end\n'
printf 'fixture stderr\n' >&2
`)
	connection := &Connection{
		URL: "http://127.0.0.1:12345", Token: "fixture-capability", Scope: "fixture-scope",
		Defaults: Config{Provider: "openrouter", Model: "fixture/model"},
	}

	result, err := (Runner{Executable: executable, Cwd: cwd, Connection: connection}).Run(context.Background(), "globalThis.fixtureValue = 42;\n")
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "completed" || result.ExitCode != 0 {
		t.Fatalf("runner result = %#v, want completed exit 0", result)
	}
	if !strings.Contains(result.Stdout, "cwd="+cwd+"\n") {
		t.Errorf("stdout did not use configured cwd: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, `connection={"url":"http://127.0.0.1:12345","token":"fixture-capability","scope":"fixture-scope","defaults":{"provider":"openrouter","model":"fixture/model"}}`) {
		t.Errorf("stdout did not receive the scoped connection: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "marker=fixture-environment\n") {
		t.Errorf("ordinary environment was not preserved: %q", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "source-begin\nimport { run_agent, spawn_agent, observe_agent, queue_agent, wait_task, read_task_result, steer_agent, interrupt_agent } from \"noe:agent\";\nglobalThis.fixtureValue = 42;\nsource-end\n") {
		t.Errorf("temporary source did not include the agent prelude and input: %q", result.Stdout)
	}
	if result.Stderr != "fixture stderr\n" {
		t.Errorf("stderr = %q, want fixture stderr", result.Stderr)
	}
}

func TestRunnerReportsNonzeroExitAsFailedResult(t *testing.T) {
	executable := writeFakeNOE(t, `
printf 'script failed\n'
printf 'failure detail\n' >&2
exit 7
`)
	result, err := (Runner{Executable: executable}).Run(context.Background(), "throw new Error('fixture');")
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "failed" || result.ExitCode != 7 || result.Stdout != "script failed\n" || result.Stderr != "failure detail\n" {
		t.Fatalf("nonzero result = %#v", result)
	}
}

func TestRunnerTerminatesDescendantsAfterNormalRootExit(t *testing.T) {
	executable := writeFakeNOE(t, `
sleep 30 &
printf 'root complete\n'
exit 0
`)
	started := time.Now()
	result, err := (Runner{Executable: executable}).Run(context.Background(), "globalThis.done = true;")
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "completed" || result.ExitCode != 0 || result.Stdout != "root complete\n" {
		t.Fatalf("normal-exit result = %#v", result)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("runner waited for a descendant-held output pipe: %s", elapsed)
	}
}

func TestRunnerCancellationTerminatesProcessGroupAndDrainsOutput(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("NOE_RUNNER_CHILD_PID_FILE", pidFile)
	executable := writeFakeNOE(t, `
sleep 30 &
echo "$!" > "$NOE_RUNNER_CHILD_PID_FILE"
printf 'started\n'
wait
`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultCh := make(chan struct {
		result Result
		err    error
	}, 1)
	go func() {
		result, err := (Runner{Executable: executable}).Run(ctx, "await new Promise(() => {});")
		resultCh <- struct {
			result Result
			err    error
		}{result: result, err: err}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(pidFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture process did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.result.Reason != "cancelled" || got.result.Stdout != "started\n" {
			t.Fatalf("cancelled result = %#v", got.result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runner did not return after cancelling its process group")
	}
}
