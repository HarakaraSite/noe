//go:build e2e && linux

package e2e_test

import (
	"context"
	"os"
	"testing"

	"noe/internal/agent"
)

func TestAgentHumanInterruptReturnsREPLPrompt(t *testing.T) {
	dir := t.TempDir()
	env := map[string]string{"NOE_AGENT_RUNTIME_DIR": dir, "NOE_AGENT_TRACE": ""}
	// No initial task: this path must not read a key or contact a provider.
	session := startTerminalREPL(t, t.TempDir(), env)
	session.enter(t, `const am=await import("noe:agent")`)
	session.writeKeys(t, `await am.run_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},undefined,{keepAlive:true}).then(s=>s.reason)`+"\r")
	ctx, cancel := context.WithTimeout(context.Background(), ptyWaitTimeout)
	defer cancel()
	snapshot := awaitControlAgent(t, ctx, dir, func(s agent.AgentSnapshot) bool { return s.State == "idle" })
	result := runCLI(t, t.TempDir(), env, "interrupt", "--agent", snapshot.AgentID)
	if result.exitCode != 0 {
		t.Fatalf("interrupt failed: %+v", result)
	}
	session.waitForOutputAndPrompt(t, "'interrupted'")
	session.writeKeys(t, "2+2\r")
	session.waitForOutputAndPrompt(t, "4")
	session.writeKeys(t, "\x04")
	session.finish(t, 0)
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("REPL registry retained: %v %v", entries, err)
	}
}
