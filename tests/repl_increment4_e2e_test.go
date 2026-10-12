//go:build e2e && linux

package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestREPLTerminalPersistentHistoryAcrossSessions(t *testing.T) {
	historyPath := filepath.Join(t.TempDir(), "history", "repl_history.jsonl")
	if err := os.MkdirAll(filepath.Dir(historyPath), 0o700); err != nil {
		t.Fatalf("create history directory: %v", err)
	}
	const command = `console.log("PERSISTED_HISTORY_REPLAYED")`

	first := startTerminalREPL(t, t.TempDir(), map[string]string{"NOE_REPL_HISTORY": historyPath})
	first.writeKeys(t, command+"\r")
	first.waitForOutputAndPrompt(t, "PERSISTED_HISTORY_REPLAYED")
	first.writeKeys(t, "\x04")
	first.finish(t, 0)
	assertREPLHistory(t, historyPath, []string{command})

	second := startTerminalREPL(t, t.TempDir(), map[string]string{"NOE_REPL_HISTORY": historyPath})
	// The input is served from persisted history. The exact console output proves
	// that the recalled command ran; the line-editor contents are not an output
	// marker and cannot satisfy this assertion.
	second.writeKeys(t, "\x1b[A\r")
	second.waitForOutputAndPrompt(t, "PERSISTED_HISTORY_REPLAYED")
	second.writeKeys(t, "\x04")
	second.finish(t, 0)
	assertREPLHistory(t, historyPath, []string{command, command})
}

func readREPLHistory(t *testing.T, path string) []string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read REPL history: %v", err)
	}
	if !strings.HasSuffix(string(contents), "\n") {
		t.Fatalf("history is not newline-delimited JSON: %q", contents)
	}
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	entries := make([]string, len(lines))
	for index, line := range lines {
		if err := json.Unmarshal([]byte(line), &entries[index]); err != nil {
			t.Fatalf("decode history row %d %q: %v", index+1, line, err)
		}
	}
	return entries
}

func assertREPLHistory(t *testing.T, path string, want []string) []string {
	t.Helper()
	entries := readREPLHistory(t, path)
	if len(entries) != len(want) {
		t.Fatalf("history contains %d rows, want %d: %q", len(entries), len(want), entries)
	}
	for index, entry := range entries {
		if entry != want[index] {
			t.Errorf("history row %d = %q, want %q", index+1, entry, want[index])
		}
	}
	return entries
}

func (s *terminalREPL) enterContinuation(t *testing.T, line string) {
	t.Helper()
	before := s.stdout.snapshot()
	s.writeKeys(t, line+"\r")
	s.stdout.waitFor(t, "REPL continuation prompt", func(output string) bool {
		if len(output) < len(before) {
			return false
		}
		return strings.HasSuffix(normalizePTYOutput(output[len(before):]), "... ")
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

func TestREPLTerminalEmptyHistorySettingDisablesPersistence(t *testing.T) {
	work := t.TempDir()
	configDir := filepath.Join(work, "config")
	session := startTerminalREPL(t, work, map[string]string{
		"NOE_REPL_HISTORY": "",
		"XDG_CONFIG_HOME":  configDir,
	})
	session.writeKeys(t, `console.log("HISTORY_DISABLED_STILL_USABLE")`+"\r")
	session.waitForOutputAndPrompt(t, "HISTORY_DISABLED_STILL_USABLE")
	session.writeKeys(t, "\x04")
	session.finish(t, 0)

	defaultHistoryPath := filepath.Join(configDir, "noe", "repl_history.jsonl")
	if _, err := os.Stat(defaultHistoryPath); !os.IsNotExist(err) {
		t.Fatalf("empty NOE_REPL_HISTORY created a history file at %q (stat error %v)", defaultHistoryPath, err)
	}
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

func countREPLHistoryEntry(entries []string, want string) int {
	count := 0
	for _, entry := range entries {
		if entry == want {
			count++
		}
	}
	return count
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

func TestREPLTerminalTabCompletionUsesLiveSessionWithoutCallingGetter(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "completion.txt"), []byte("NOE_COMPLETION_FILE_VALUE"), 0o600); err != nil {
		t.Fatalf("write completion fixture: %v", err)
	}
	session := startTerminalREPL(t, work, nil)

	session.writeKeys(t, `await noe.re`+"\t"+`("completion.txt").then(text => console.log("NOE_COMPLETION:" + text))`+"\r")
	session.waitForOutputAndPrompt(t, "NOE_COMPLETION:NOE_COMPLETION_FILE_VALUE")

	session.writeKeys(t, `console.lo`+"\t"+`("COMPLETION_CONSOLE")`+"\r")
	session.waitForOutputAndPrompt(t, "COMPLETION_CONSOLE")

	session.enter(t, `const e2eUniqueVariable = "USER_VARIABLE_COMPLETED"`)
	session.writeKeys(t, "console.log(e2eUniqueV\t)\r")
	session.waitForOutputAndPrompt(t, "USER_VARIABLE_COMPLETED")

	session.enter(t, `const e2eUniqueObject = {e2eUniqueField: "OBJECT_FIELD_COMPLETED"}`)
	session.writeKeys(t, "console.log(e2eUniqueObject.e2eUniqueF\t)\r")
	session.waitForOutputAndPrompt(t, "OBJECT_FIELD_COMPLETED")

	session.enter(t, "const 商品情報 = {税額: 42}")
	session.writeKeys(t, "console.log(\"JAPANESE_COMPLETION:\" + 商品情\t.税\t)\r")
	session.waitForOutputAndPrompt(t, "JAPANESE_COMPLETION:42")

	session.enter(t, "let getterReads = 0")
	session.enter(t, `const getterTarget = { get zzE2EGetter() { getterReads++; return "getter value"; } }`)
	beforeGetterCompletion := session.stdout.snapshot()
	session.writeKeys(t, "try { getterTarget.zzE2E\t")
	session.stdout.waitFor(t, "getter property completion", func(output string) bool {
		if len(output) < len(beforeGetterCompletion) {
			return false
		}
		return strings.Contains(output[len(beforeGetterCompletion):], "getterTarget.zzE2EGetter")
	})
	beforeGetterAssignment := session.stdout.snapshot()
	session.writeKeys(t, "\x01\x0b = 0; } catch (error) {}\r")
	session.waitForPromptAfter(t, beforeGetterAssignment)
	session.writeKeys(t, `console.log("GETTER_READS:" + getterReads)`+"\r")
	session.waitForOutputAndPrompt(t, "GETTER_READS:0")

	session.enter(t, "let proxyTrapCalls = 0")
	session.enterMultiline(t, []string{
		"const completionProxy = new Proxy({}, {",
		`  ownKeys() { proxyTrapCalls++; throw new Error("completion proxy trap"); },`,
		"})",
	})
	beforeProxyCompletion := session.stdout.snapshot()
	session.writeKeys(t, "completionProxy.\t\x01\x0b"+`console.log("PROXY_TRAP_COUNT:" + proxyTrapCalls)`+"\r")
	session.stdout.waitFor(t, "completion failure recovery and proxy trap", func(output string) bool {
		for _, candidate := range strings.Split(normalizePTYOutput(output), "\n") {
			candidate = strings.TrimRight(candidate, "\r")
			if !strings.HasPrefix(candidate, "PROXY_TRAP_COUNT:") {
				continue
			}
			count, err := strconv.Atoi(strings.TrimPrefix(candidate, "PROXY_TRAP_COUNT:"))
			return err == nil && count > 0
		}
		return false
	})
	session.waitForPromptAfter(t, beforeProxyCompletion)

	session.writeKeys(t, "\x04")
	result := session.finish(t, 0)
	assertNoPTYOutputLine(t, result.stdout, "GETTER_READS:1")
	assertNoPTYOutputLine(t, result.stdout, "PROXY_TRAP_COUNT:0")
}

func TestREPLTerminalCompletionProxyExitRestoresSettings(t *testing.T) {
	session := startTerminalREPL(t, t.TempDir(), nil)
	session.enter(t, `const completionExitProxy = new Proxy({}, { ownKeys() { console.log('COMPLETION_EXIT_TRAP'); noe.exit(7); } })`)
	beforeCompletion := session.stdout.snapshot()
	session.writeKeys(t, "completionExitProxy.\t")
	session.stdout.waitFor(t, "completion exit trap marker", func(output string) bool {
		if len(output) < len(beforeCompletion) {
			return false
		}
		return strings.Contains(output[len(beforeCompletion):], "COMPLETION_EXIT_TRAP")
	})
	session.finish(t, 7)
}
