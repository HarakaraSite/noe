//go:build e2e && linux

package e2e_test

import (
	"strings"
	"testing"
)

func waitTerminalServeURL(t *testing.T, s *terminalREPL, before string) string {
	t.Helper()
	output := s.stdout.waitFor(t, "REPL server URL", func(output string) bool {
		return len(output) >= len(before) && serveURLPattern.MatchString(normalizePTYOutput(output[len(before):]))
	})
	return serveURLPattern.FindStringSubmatch(normalizePTYOutput(output[len(before):]))[1]
}

func TestServeTerminalREPLCancellationErrorAndRestart(t *testing.T) {
	s := startTerminalREPL(t, t.TempDir(), nil)
	s.enter(t, `var preserved = 42`)
	before := s.stdout.snapshot()
	s.writeKeys(t, `var firstServer = noe.serve({}, () => ({body:"first"})); console.log("SERVER_URL",firstServer.url)`+"\r")
	address := waitTerminalServeURL(t, s, before)
	requireServeHTTP(t, "GET", address, nil, 200, []byte("first"))
	s.writeKeys(t, "\x03")
	s.waitForError(t, "Interrupted")
	s.waitForPromptAfter(t, before)
	requireServeRebind(t, address)
	s.enter(t, `firstServer.close()`)
	s.writeKeys(t, `console.log("PRESERVED",preserved)`+"\r")
	s.waitForOutputAndPrompt(t, "PRESERVED 42")
	before = s.stdout.snapshot()
	s.writeKeys(t, `var nextServer = noe.serve({port:firstServer.port}, () => {nextServer.close(); return {body:"restarted"}}); console.log("SERVER_URL",nextServer.url)`+"\r")
	address = waitTerminalServeURL(t, s, before)
	requireServeHTTP(t, "GET", address, nil, 200, []byte("restarted"))
	s.waitForPromptAfter(t, before)
	requireServeRebind(t, address)
	before = s.stdout.snapshot()
	s.writeKeys(t, `var failedServer = noe.serve({port:firstServer.port}, () => ({body:"unused"})); console.log("SERVER_URL",failedServer.url); throw new Error("SERVE_INPUT_FAILED")`+"\r")
	address = waitTerminalServeURL(t, s, before)
	s.waitForError(t, "SERVE_INPUT_FAILED")
	s.waitForPromptAfter(t, before)
	requireServeRebind(t, address)
	s.writeKeys(t, `console.log("REPL_RECOVERED",preserved + 1)`+"\r")
	s.waitForOutputAndPrompt(t, "REPL_RECOVERED 43")
	s.writeKeys(t, "\x04")
	got := s.finish(t, 0)
	if !strings.Contains(got.stderr, "Interrupted") || !strings.Contains(got.stderr, "SERVE_INPUT_FAILED") {
		t.Fatalf("REPL diagnostics: %+v", got)
	}
}
