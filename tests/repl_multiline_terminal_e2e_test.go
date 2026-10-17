//go:build e2e && linux

package e2e_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func (s *terminalREPL) enterContinuation(t *testing.T, line string) {
	t.Helper()
	before := s.stdout.snapshot()
	s.writeKeys(t, line+"\r")
	s.stdout.waitFor(t, "REPL continuation prompt", func(output string) bool {
		if len(output) < len(before) {
			return false
		}
		return hasTerminalPrompt(output[len(before):], "... ")
	})
}

func (s *terminalREPL) enterMultiline(t *testing.T, lines []string) {
	t.Helper()
	if len(lines) < 2 {
		t.Fatalf("enterMultiline needs at least two lines, got %d", len(lines))
	}
	for _, line := range lines[:len(lines)-1] {
		s.enterContinuation(t, line)
	}
	s.enter(t, lines[len(lines)-1])
}

func TestREPLTerminalLastResultTracksSuccessfulValues(t *testing.T) {
	session := startTerminalREPL(t, t.TempDir(), nil)

	session.writeKeys(t, `console.log("UNDERSCORE_INITIAL:" + String(_))`+"\r")
	session.waitForOutputAndPrompt(t, "UNDERSCORE_INITIAL:undefined")

	session.enter(t, `({value: 41})`)
	session.writeKeys(t, `console.log("UNDERSCORE_OBJECT:" + _.value)`+"\r")
	session.waitForOutputAndPrompt(t, "UNDERSCORE_OBJECT:41")

	beforeError := session.stdout.snapshot()
	session.writeKeys(t, `throw new Error("UNDERSCORE_ERROR_MARKER")`+"\r")
	session.waitForError(t, "UNDERSCORE_ERROR_MARKER")
	session.waitForPromptAfter(t, beforeError)
	session.writeKeys(t, `console.log("UNDERSCORE_AFTER_ERROR:" + _.value)`+"\r")
	session.waitForOutputAndPrompt(t, "UNDERSCORE_AFTER_ERROR:41")

	session.enter(t, "undefined")
	session.writeKeys(t, `console.log("UNDERSCORE_AFTER_UNDEFINED:" + _.value)`+"\r")
	session.waitForOutputAndPrompt(t, "UNDERSCORE_AFTER_UNDEFINED:41")

	session.enter(t, "await Promise.resolve(73)")
	session.writeKeys(t, `console.log("UNDERSCORE_AFTER_AWAIT:" + _)`+"\r")
	session.waitForOutputAndPrompt(t, "UNDERSCORE_AFTER_AWAIT:73")

	session.enter(t, "const assignedLastResult = {value: 7}")
	session.enter(t, "_ = assignedLastResult")
	session.enter(t, "(() => { assignedLastResult.value = 8; })()")
	session.writeKeys(t, `console.log("UNDERSCORE_ASSIGNED_REFERENCE:" + _.value)`+"\r")
	session.waitForOutputAndPrompt(t, "UNDERSCORE_ASSIGNED_REFERENCE:8")

	session.writeKeys(t, "\x04")
	result := session.finish(t, 0)
	if !strings.Contains(result.stderr, "UNDERSCORE_ERROR_MARKER") {
		t.Fatalf("missing expected JS error diagnostic: %q", result.stderr)
	}
}

func TestREPLTerminalMultilineEvaluationAndHistoryReplay(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "repl_history.jsonl")
	const historyCommand = "(() => {\n  console.log(\"MULTILINE_HISTORY_REPLAY\");\n})()"
	session := startTerminalREPL(t, t.TempDir(), map[string]string{"NOE_REPL_HISTORY": historyPath})
	session.enter(t, "let effectCount = 0")

	session.enterMultiline(t, []string{
		"const multiObject = {",
		"  value: ++effectCount,",
		`  label: "object",`,
		"}",
	})
	session.writeKeys(t, `console.log("MULTILINE_OBJECT:" + multiObject.value + ":" + multiObject.label + ":" + effectCount)`+"\r")
	session.waitForOutputAndPrompt(t, "MULTILINE_OBJECT:1:object:1")

	session.enterMultiline(t, []string{
		"function makeMultiValue(seed) {",
		"  return {",
		"    value: seed + 1,",
		`    label: "function",`,
		"  };",
		"}",
	})
	session.writeKeys(t, `console.log("MULTILINE_FUNCTION:" + makeMultiValue(41).value)`+"\r")
	session.waitForOutputAndPrompt(t, "MULTILINE_FUNCTION:42")

	session.enterMultiline(t, []string{
		"const multiText = `first",
		"second`",
	})
	session.writeKeys(t, `console.log("MULTILINE_STRING:" + JSON.stringify(multiText))`+"\r")
	session.waitForOutputAndPrompt(t, `MULTILINE_STRING:"first\nsecond"`)

	session.enterMultiline(t, []string{
		"const asyncValue = await Promise.resolve(",
		"  29",
		")",
	})
	session.writeKeys(t, `console.log("MULTILINE_AWAIT:" + asyncValue)`+"\r")
	session.waitForOutputAndPrompt(t, "MULTILINE_AWAIT:29")

	beforeError := session.stdout.snapshot()
	session.writeKeys(t, "const definitelyBroken = ;\r")
	session.waitForError(t, "SyntaxError")
	session.waitForPromptAfter(t, beforeError)
	session.writeKeys(t, `console.log("MULTILINE_RECOVERED")`+"\r")
	session.waitForOutputAndPrompt(t, "MULTILINE_RECOVERED")

	session.enterMultiline(t, strings.Split(historyCommand, "\n"))
	session.waitForOutputLine(t, "MULTILINE_HISTORY_REPLAY")
	session.writeKeys(t, "\x04")
	session.finish(t, 0)

	entries := readREPLHistory(t, historyPath)
	multilineInputs := []string{
		"const multiObject = {\n  value: ++effectCount,\n  label: \"object\",\n}",
		"function makeMultiValue(seed) {\n  return {\n    value: seed + 1,\n    label: \"function\",\n  };\n}",
		"const multiText = `first\nsecond`",
		"const asyncValue = await Promise.resolve(\n  29\n)",
		historyCommand,
	}
	for _, input := range multilineInputs {
		if countREPLHistoryEntry(entries, input) != 1 {
			t.Errorf("completed multiline source was not saved as one history entry: %q", input)
		}
	}
	if countREPLHistoryEntry(entries, "  console.log(\"MULTILINE_HISTORY_REPLAY\");") != 0 {
		t.Fatalf("history stored a physical continuation line separately: %q", entries)
	}
	if countREPLHistoryEntry(entries, historyCommand) != 1 {
		t.Fatalf("completed multiline command was not saved as one history entry: %q", entries)
	}

	replay := startTerminalREPL(t, t.TempDir(), map[string]string{"NOE_REPL_HISTORY": historyPath})
	replay.writeKeys(t, "\x1b[A\r")
	replay.waitForOutputAndPrompt(t, "MULTILINE_HISTORY_REPLAY")
	replay.writeKeys(t, "\x04")
	replay.finish(t, 0)
	updatedEntries := readREPLHistory(t, historyPath)
	if len(updatedEntries) != len(entries)+1 || updatedEntries[len(updatedEntries)-1] != historyCommand {
		t.Fatalf("multiline replay was not appended as one complete source: before=%q after=%q", entries, updatedEntries)
	}
	entries = updatedEntries
	if countREPLHistoryEntry(entries, historyCommand) != 2 {
		t.Fatalf("multiline history replay did not append the complete source once: %q", entries)
	}
}

func TestREPLTerminalIncompleteInputAtEOFIsDiagnosed(t *testing.T) {
	session := startTerminalREPL(t, t.TempDir(), nil)
	session.enterContinuation(t, `(() => { console.log("INCOMPLETE_SHOULD_NOT_RUN");`)
	session.writeKeys(t, "\x04")
	result := session.finish(t, 0)
	if !strings.Contains(result.stderr, "SyntaxError") {
		t.Fatalf("EOF during incomplete input produced no syntax diagnostic: %q", result.stderr)
	}
	for _, line := range strings.Split(normalizePTYOutput(result.stdout), "\n") {
		if strings.TrimRight(line, "\r") == "INCOMPLETE_SHOULD_NOT_RUN" {
			t.Fatalf("incomplete source ran before EOF: %q", result.stdout)
		}
	}
}
