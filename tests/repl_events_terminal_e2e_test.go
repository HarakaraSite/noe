//go:build e2e && linux

package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestREPLTerminalEventsPreserveEditingAndExcludeUI(t *testing.T) {
	dir := t.TempDir()
	session := startTerminalREPL(t, dir, map[string]string{"NOE_AGENT_TRACE": ""}, "--events=events.jsonl")
	session.enter(t, "let value=40")
	session.writeKeys(t, "value + 2\r")
	session.waitForOutputAndPrompt(t, "42")
	session.writeKeys(t, "console.lo\t('hello')\r")
	session.waitForOutputAndPrompt(t, "hello")
	// The file is readable while REPL remains alive and interactive.
	data, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	seenResult, seenOutput := false, false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["type"] == "repl.result" && event["text"] == "42" {
			seenResult = true
		}
		if event["type"] == "stdout" && strings.Contains(event["text"].(string), "hello") {
			seenOutput = true
		}
		if text, ok := event["text"].(string); ok && (strings.Contains(text, "\x1b[") || text == "> ") {
			t.Fatalf("terminal UI logged: %+v", event)
		}
	}
	if !seenResult || !seenOutput {
		t.Fatalf("result=%v output=%v: %s", seenResult, seenOutput, data)
	}
	session.writeKeys(t, "\x04")
	session.finish(t, 0)
}

func TestREPLTerminalDirectObserveKeepsEditor(t *testing.T) {
	dir := t.TempDir()
	session := startTerminalREPL(t, dir, map[string]string{"NOE_AGENT_TRACE": ""}, "--observe", "--events=events.jsonl")
	session.enter(t, "let value=40")
	before := session.stdout.snapshot()
	session.writeKeys(t, "value+2\r")
	session.stdout.waitFor(t, "readable evaluation result", func(output string) bool { return strings.Contains(normalizePTYOutput(output), "result: 42") })
	session.waitForPromptAfter(t, before)
	session.writeKeys(t, "\x04")
	session.finish(t, 0)
	data, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["type"] == "repl.result" && event["text"] == "42" {
			seen = true
		}
		if text, ok := event["text"].(string); ok && strings.Contains(text, "\x1b[") {
			t.Fatalf("terminal UI entered JSONL: %+v", event)
		}
	}
	if !seen {
		t.Fatal("missing recorded evaluation result")
	}
}
