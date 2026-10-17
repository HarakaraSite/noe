//go:build e2e && linux

package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func countREPLHistoryEntry(entries []string, want string) int {
	count := 0
	for _, entry := range entries {
		if entry == want {
			count++
		}
	}
	return count
}
