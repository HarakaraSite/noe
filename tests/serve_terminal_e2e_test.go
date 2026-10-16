//go:build e2e && linux

package e2e_test

import "testing"

func waitTerminalServeURL(t *testing.T, s *terminalREPL, before string) string {
	t.Helper()
	output := s.stdout.waitFor(t, "REPL server URL", func(output string) bool {
		return len(output) >= len(before) && serveURLPattern.MatchString(normalizePTYOutput(output[len(before):]))
	})
	return serveURLPattern.FindStringSubmatch(normalizePTYOutput(output[len(before):]))[1]
}

func TestServeTerminalREPLInterruptClosesSessionAndListener(t *testing.T) {
	s := startTerminalREPL(t, t.TempDir(), nil)
	before := s.stdout.snapshot()
	s.writeKeys(t, `var activeServer = serve({}, () => new Response("first")); console.log("SERVER_URL",activeServer.url)`+"\r")
	address := waitTerminalServeURL(t, s, before)
	requireServeHTTP(t, "GET", address, nil, 200, []byte("first"))
	s.interruptKey(t)
	result := s.finish(t, 1)
	requireExit(t, result, 1)
	if result.stderr == "" {
		t.Fatalf("server REPL interruption produced no diagnostic: %+v", result)
	}
	requireServeRebind(t, address)
}
