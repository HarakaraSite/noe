//go:build e2e && linux

package e2e_test

import "testing"

func TestSleepTerminalREPLInterruptAndRecovery(t *testing.T) {
	s := startTerminalREPL(t, t.TempDir(), nil)
	s.enter(t, `42`)
	s.enter(t, `const timers = await import("timers/promises")`)
	s.enter(t, `function waitForSleep(){const p=timers.setTimeout(60000);console.log("SLEEP_WAITING");return p;}`)
	before := s.stdout.snapshot()
	s.writeKeys(t, "await waitForSleep()\r")
	s.waitForOutputLine(t, "SLEEP_WAITING")
	s.writeKeys(t, "\x03")
	s.waitForError(t, "Interrupted")
	s.waitForPromptAfter(t, before)
	s.writeKeys(t, `console.log("LAST_RESULT",_)`+"\r")
	s.waitForOutputAndPrompt(t, "LAST_RESULT 42")
	s.enter(t, `await timers.setTimeout(0)`)
	s.writeKeys(t, `console.log("SLEEP_RECOVERED")`+"\r")
	s.waitForOutputAndPrompt(t, "SLEEP_RECOVERED")
	s.writeKeys(t, "\x04")
	s.finish(t, 0)
}
