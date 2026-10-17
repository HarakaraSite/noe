//go:build e2e

package e2e_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"noe/internal/agent"
)

func runAgentJS(t *testing.T, runner agent.Runner, source string) agent.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := runner.Run(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAgentRunnerFreshProcessCwdAndImports(t *testing.T) {
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "data.txt"), "cwd data")
	writeFixture(t, filepath.Join(work, "helper.js"), "export const value = 42;")
	runner := agent.Runner{Executable: cliPath, Cwd: work}
	source := `import {readFile} from "node:fs/promises";
import {relative, dirname} from "node:path";
console.log(await readFile("data.txt","utf8"));
let prefix=relative(dirname(process.argv[1]),process.cwd()).replaceAll("\\","/");
if (!prefix.startsWith(".")) prefix="./"+prefix;
console.log((await import(prefix+"/helper.js")).value);
globalThis.runnerSecret=17;
console.log(typeof run_agent);console.log(typeof createAgent);`
	result := runAgentJS(t, runner, source)
	if result.ExitCode != 0 || result.Reason != "completed" || result.Stdout != "cwd data\n42\nfunction\nundefined\n" || result.Stderr != "" {
		t.Fatalf("runner result: %+v", result)
	}
	result = runAgentJS(t, runner, `console.log(typeof globalThis.runnerSecret);`)
	if result.ExitCode != 0 || result.Stdout != "undefined\n" {
		t.Fatalf("globals leaked between processes: %+v", result)
	}
}

func TestAgentRunnerReportsFailuresAndExit(t *testing.T) {
	runner := agent.Runner{Executable: cliPath, Cwd: t.TempDir()}
	for _, tc := range []struct {
		code           string
		exit           int
		stdout, stderr string
	}{
		{`console.log("out");console.error("err");throw Error("runner failure");`, 1, "out\n", "runner failure"},
		{`console.log("exiting");process.exit(7);console.log("unreachable");`, 7, "exiting\n", ""},
		{`const invalid = ;`, 1, "", "SyntaxError"},
	} {
		result := runAgentJS(t, runner, tc.code)
		if result.ExitCode != tc.exit || result.Reason != "failed" || result.Stdout != tc.stdout || !strings.Contains(result.Stderr, tc.stderr) {
			t.Fatalf("code %s: %+v", tc.code, result)
		}
	}
}

func TestAgentRunnerFiltersProviderSecretsAndStaleConnection(t *testing.T) {
	for _, key := range []string{"OPENROUTER_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "NOE_OPENROUTER_API_KEY"} {
		t.Setenv(key, "runner-fixture-secret")
	}
	t.Setenv("NOE_AGENT_CONNECTION", "stale-parent-connection")
	result := runAgentJS(t, agent.Runner{Executable: cliPath, Cwd: t.TempDir()}, `
for (const key of ["OPENROUTER_API_KEY","OPENAI_API_KEY","ANTHROPIC_API_KEY","NOE_OPENROUTER_API_KEY","NOE_AGENT_CONNECTION"]) {
  if (process.env[key] !== undefined) throw Error("inherited "+key);
}
console.log("clean");`)
	if result.ExitCode != 0 || result.Stdout != "clean\n" || strings.Contains(result.Stderr, "runner-fixture-secret") {
		t.Fatalf("child environment: %+v", result)
	}
}

func TestAgentRunnerTimeoutAndPreCancelled(t *testing.T) {
	runner := agent.Runner{Executable: cliPath, Cwd: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	result, err := runner.Run(ctx, `setInterval(()=>{},1000);await new Promise(()=>{});`)
	// Keep an active timer so NOE has work rather than diagnosing never-settles.
	if err != nil || result.Reason != "timeout" || result.ExitCode == 0 {
		t.Fatalf("timeout result: %+v error=%v", result, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	result, err = runner.Run(ctx, `console.log("must not start");`)
	if err != nil || result.Reason != "cancelled" || result.Stdout != "" {
		t.Fatalf("pre-cancelled result: %+v error=%v", result, err)
	}
}
