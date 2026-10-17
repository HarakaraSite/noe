//go:build e2e && linux

package e2e_test

import (
	"strings"
	"testing"
)

func TestREPLTerminalSearchSelectionAndBracketedPaste(t *testing.T) {
	session := startTerminalREPL(t, t.TempDir(), nil)
	session.enter(t, `console.log("SEARCH_TARGET_A")`)
	session.waitForOutputLine(t, "SEARCH_TARGET_A")
	session.enter(t, `console.log("SEARCH_TARGET_B")`)
	before := session.stdout.snapshot()
	session.writeKeys(t, "\x12TARGET_A\r")
	session.waitForPromptAfter(t, before)
	if count := strings.Count(normalizePTYOutput(session.stdout.snapshot()), "\nSEARCH_TARGET_A\n"); count != 2 {
		t.Fatalf("search did not evaluate the selected history entry twice: count=%d", count)
	}
	before = session.stdout.snapshot()
	session.writeKeys(t, "\x12TARGET_A_missing\r")
	session.waitForPromptAfter(t, before)
	if count := strings.Count(normalizePTYOutput(session.stdout.snapshot()), "\nSEARCH_TARGET_A\n"); count != 2 {
		t.Fatalf("unmatched search executed a stale match: count=%d", count)
	}

	// Several physical paste lines pass through the existing syntax/held-input
	// path before NOE evaluates the complete function once.
	before = session.stdout.snapshot()
	session.writeKeys(t, "\x1b[200~function pastedValue(n) {\n  return n + 1;\n}\x1b[201~\r")
	session.waitForPromptAfter(t, before)
	session.writeKeys(t, "console.log(\"PASTED_RESULT:\" + pastedValue(41))\r")
	session.waitForOutputAndPrompt(t, "PASTED_RESULT:42")
	session.writeKeys(t, "\x04")
	session.finish(t, 0)
}

func TestREPLTerminalEscapeSequenceDoesNotSwallowCtrlC(t *testing.T) {
	for _, prefix := range []string{"\x1b", "\x1b[", "\x1b[1;", "\x1bO"} {
		t.Run(prefix, func(t *testing.T) {
			session := startTerminalREPL(t, t.TempDir(), nil)
			session.writeKeys(t, "discarded"+prefix)
			session.interruptAndContinue(t)
			session.writeKeys(t, "console.log(\"AFTER_ESCAPE_INTERRUPT\")\r")
			session.waitForOutputAndPrompt(t, "AFTER_ESCAPE_INTERRUPT")
			session.writeKeys(t, "\x04")
			session.finish(t, 0)
		})
	}
}
