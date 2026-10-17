//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func expectedREPLOutput(outputs []string, trailingPrompt bool) string {
	var want strings.Builder
	for _, output := range outputs {
		want.WriteString("> ")
		if output != "" {
			want.WriteString(output)
			want.WriteByte('\n')
		}
	}
	if trailingPrompt {
		want.WriteString("> ")
	}
	return want.String()
}

func TestREPLPersistentStateFormattingAndPipedPrompts(t *testing.T) {
	work := t.TempDir()
	lines := []string{
		"let count = 1",
		"",
		"count += 1",
		"function add(n) { return n + count; }",
		"count",
		"add(3)",
		"const obj = { value: count }",
		"obj",
		"[count, add(3)]",
		"undefined",
		"process.argv.slice(2).length",
	}
	got := runREPLInput(t, work, nil, strings.Join(lines, "\n")) // The last input deliberately has no newline.
	requireExit(t, got, 0)
	want := expectedREPLOutput([]string{"", "", "2", "", "2", "5", "", "{ value: 2 }", "[ 2, 5 ]", "", "0"}, false)
	if got.stdout != want {
		t.Fatalf("REPL stdout = %q, want %q", got.stdout, want)
	}
	if got.stderr != "" {
		t.Fatalf("unexpected stderr: %q", got.stderr)
	}
}

func TestREPLRecoversFromSyntaxRuntimeConstAndUnhandledErrors(t *testing.T) {
	work := t.TempDir()
	lines := []string{
		"let mutable = 4",
		"const stable = 6",
		"const broken = ;",
		`mutable = 9; throw new Error("runtime marker")`,
		"stable = 10",
		"stable",
		`Promise.resolve().then(() => { throw new Error("late rejection marker"); })`,
		"mutable",
	}
	got := runREPLInput(t, work, nil, strings.Join(lines, "\n")+"\n")
	requireExit(t, got, 0)
	want := expectedREPLOutput([]string{"", "", "", "", "", "6", "", "9"}, true)
	if got.stdout != want {
		t.Fatalf("REPL stdout = %q, want %q", got.stdout, want)
	}
	for _, marker := range []string{"SyntaxError", "runtime marker", "late rejection marker"} {
		if !strings.Contains(got.stderr, marker) {
			t.Errorf("stderr %q does not contain %q", got.stderr, marker)
		}
	}
	if got.stderr == "" {
		t.Fatal("recoverable input errors produced no diagnostics")
	}
}

func TestREPLAwaitsFileImportHTTPAndSavesResult(t *testing.T) {
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "input.json"), `{"value":6}`)
	writeFixture(t, filepath.Join(work, "helper.js"), `export const label = "local module";`)
	methodSeen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methodSeen <- r.Method
		if r.URL.Path != "/repl-data" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"name":"local server"}`)
	}))
	defer server.Close()
	lines := []string{
		`const fs = await import("node:fs/promises")`,
		"let asyncLet = await Promise.resolve(3)",
		"var asyncVar = await Promise.resolve(4)",
		"asyncLet += 1",
		"asyncVar += 1",
		`const raw = await fs.readFile("input.json", "utf8")`,
		"const data = JSON.parse(raw)",
		"data.value += 1",
		`const helper = await import("./helper.js")`,
		`const response = await fetch(process.env.NOE_REPL_URL)`,
		"const remote = await response.json()",
		`await fs.writeFile("output.json", JSON.stringify({ value: data.value, label: helper.label, remote: remote.name }))`,
		`const saved = await fs.readFile("output.json", "utf8")`,
		"JSON.parse(saved).value",
	}
	got := runREPLInput(t, work, map[string]string{"NOE_REPL_URL": server.URL + "/repl-data"}, strings.Join(lines, "\n")+"\n")
	requireExit(t, got, 0)
	if got.stderr != "" {
		t.Fatalf("unexpected stderr: %q", got.stderr)
	}
	if strings.Count(got.stdout, "7\n") != 2 {
		t.Fatalf("REPL stdout does not show the assignment and saved value: %q", got.stdout)
	}
	if strings.Count(got.stdout, "> ") != len(lines)+1 {
		t.Fatalf("prompt count in stdout = %d, want %d: %q", strings.Count(got.stdout, "> "), len(lines)+1, got.stdout)
	}
	if method := <-methodSeen; method != http.MethodGet {
		t.Fatalf("REPL fetch method = %q, want GET", method)
	}
	saved, err := os.ReadFile(filepath.Join(work, "output.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != `{"value":7,"label":"local module","remote":"local server"}` {
		t.Fatalf("saved output = %q", saved)
	}
}

func TestREPLWaitsForUnawaitedIOBeforeNextInput(t *testing.T) {
	work := t.TempDir()
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseResponse := func() { releaseOnce.Do(func() { close(release) }) }
	requestStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		select {
		case <-release:
			fmt.Fprint(w, "request finished")
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer releaseResponse()
	session := startLiveREPL(t, work, map[string]string{"NOE_REPL_URL": server.URL})
	session.waitPromptCount(t, 1)
	session.writeLine(t, `const fs = await import("node:fs/promises")`)
	session.waitPromptCount(t, 2)
	session.writeLine(t, `fetch(process.env.NOE_REPL_URL).then(response => response.text()).then(text => fs.writeFile("pending.txt", text))`)
	select {
	case <-requestStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("local HTTP request did not start")
	}
	session.writeLine(t, `const pending = await fs.readFile("pending.txt", "utf8")`)
	releaseResponse()
	session.waitPromptCount(t, 3)
	session.writeLine(t, "pending")
	session.waitPromptCount(t, 4)
	got := session.finish(t, true)
	requireExit(t, got, 0)
	if got.stderr != "" {
		t.Fatalf("next input ran before the started I/O completed: %q", got.stderr)
	}
	if !strings.Contains(got.stdout, "request finished") {
		t.Fatalf("next input did not observe the completed I/O: %q", got.stdout)
	}
	saved, err := os.ReadFile(filepath.Join(work, "pending.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(saved) != "request finished" {
		t.Fatalf("unawaited fetch result = %q", saved)
	}
}

func TestREPLAsyncRejectionsAndNeverSettlingAwaitRecover(t *testing.T) {
	work := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"invalid":`)
	}))
	defer server.Close()
	lines := []string{
		`const fs = await import("node:fs/promises")`,
		`const missing = await fs.readFile("missing.json", "utf8")`,
		"typeof missing",
		`const response = await fetch(process.env.NOE_REPL_BAD_JSON)`,
		"await response.json()",
		"await new Promise(() => {})",
		"42",
	}
	got := runREPLInput(t, work, map[string]string{"NOE_REPL_BAD_JSON": server.URL}, strings.Join(lines, "\n")+"\n")
	requireExit(t, got, 0)
	if strings.Count(got.stdout, "> ") != len(lines)+1 || !strings.Contains(got.stdout, "> 42\n> ") {
		t.Fatalf("session did not continue to the final input: %q", got.stdout)
	}
	for _, marker := range []string{"missing.json", "SyntaxError"} {
		if !strings.Contains(got.stderr, marker) {
			t.Errorf("stderr %q does not contain %q", got.stderr, marker)
		}
	}
	if !strings.Contains(got.stdout, "undefined") {
		t.Errorf("failed initializer did not leave the binding undefined: stdout=%q stderr=%q", got.stdout, got.stderr)
	}
}

func TestREPLUnsupportedAsyncFormsDoNotRunSideEffects(t *testing.T) {
	work := t.TempDir()
	lines := []string{
		"let effects = 0",
		"const { value } = await (effects++, Promise.resolve({ value: 1 }))",
		"effects",
		"const first = await (effects++, Promise.resolve(1)), second = await Promise.resolve(2)",
		"effects",
		"await (effects++, Promise.resolve(1)); effects = 99",
		"effects",
	}
	got := runREPLInput(t, work, nil, strings.Join(lines, "\n")+"\n")
	requireExit(t, got, 0)
	if strings.Count(got.stdout, "> ") != len(lines)+1 || strings.Count(got.stdout, "0\n") != 3 {
		t.Fatalf("unsupported input changed state or stopped the session: stdout=%q", got.stdout)
	}
	if got.stderr == "" {
		t.Fatal("unsupported async forms were not diagnosed")
	}
}

func TestREPLAsyncDuplicateConstRunsInitializerBeforeDiagnostic(t *testing.T) {
	work := t.TempDir()
	lines := []string{
		"const duplicate = 1",
		`const fs = await import("node:fs/promises")`,
		`const duplicate = await fs.writeFile("duplicate-side-effect.txt", "written")`,
		"duplicate",
	}
	got := runREPLInput(t, work, nil, strings.Join(lines, "\n")+"\n")
	requireExit(t, got, 0)
	if strings.Count(got.stdout, "> ") != len(lines)+1 || !strings.Contains(got.stdout, "> 1\n> ") {
		t.Fatalf("duplicate declaration did not preserve the original binding and continue: %q", got.stdout)
	}
	if got.stderr == "" {
		t.Fatal("duplicate async const was not diagnosed")
	}
	saved, err := os.ReadFile(filepath.Join(work, "duplicate-side-effect.txt"))
	if err != nil {
		t.Fatalf("async initializer side effect did not finish: %v", err)
	}
	if string(saved) != "written" {
		t.Fatalf("duplicate initializer side effect = %q", saved)
	}
}

func TestREPLExplicitExitStopsBeforeFollowingInput(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		code        int
	}{
		{name: "zero top-level exit", input: "process.exit(0)\nconsole.log(\"AFTER EXIT\")", code: 0},
		{name: "default top-level exit", input: "process.exit()\nconsole.log(\"AFTER EXIT\")", code: 0},
		{name: "nonzero top-level exit", input: "process.exit(7)\nconsole.log(\"AFTER EXIT\")", code: 7},
		{name: "async continuation exit", input: "await Promise.resolve().then(() => process.exit(9))\nconsole.log(\"AFTER EXIT\")", code: 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runREPLInput(t, t.TempDir(), nil, tc.input)
			requireExit(t, got, tc.code)
			if strings.Contains(got.stdout, "AFTER EXIT") || strings.Contains(got.stderr, "AFTER EXIT") {
				t.Fatalf("REPL ran input after process.exit: stdout=%q stderr=%q", got.stdout, got.stderr)
			}
		})
	}
}

func TestREPLInterruptWhileWaitingForInputReturns130(t *testing.T) {
	session := startLiveREPL(t, t.TempDir(), nil)
	session.waitPromptCount(t, 1)
	session.writeLine(t, `console.log("ready for input")`)
	session.waitPromptCount(t, 2)
	session.interrupt(t)
	got := session.finish(t, false)
	requireExit(t, got, 130)
	if !strings.Contains(got.stdout, "ready for input") {
		t.Fatalf("input was not evaluated before interrupt: %q", got.stdout)
	}
}

func TestREPLInterruptDuringEvaluationReturns130(t *testing.T) {
	session := startLiveREPL(t, t.TempDir(), nil)
	session.waitPromptCount(t, 1)
	session.writeLine(t, `console.log("evaluation started"); while (true) {}`)
	session.stdout.waitFor(t, "evaluation readiness log", func(output string) bool {
		return strings.Contains(output, "evaluation started\n")
	})
	session.interrupt(t)
	got := session.finish(t, false)
	requireExit(t, got, 130)
}

func TestREPLInterruptDuringHTTPWaitEndsSession(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	requestCanceled := make(chan struct{})
	requestRelease := make(chan struct{})
	var requestReleaseOnce sync.Once
	releaseRequest := func() { requestReleaseOnce.Do(func() { close(requestRelease) }) }
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		select {
		case <-r.Context().Done():
			close(requestCanceled)
		case <-requestRelease:
		}
	}))
	defer server.Close()
	defer releaseRequest()
	session := startLiveREPL(t, t.TempDir(), map[string]string{"NOE_REPL_URL": server.URL})
	session.waitPromptCount(t, 1)
	session.writeLine(t, `await fetch(process.env.NOE_REPL_URL)`)
	select {
	case <-requestStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP request did not start before interrupt")
	}
	session.interrupt(t)
	got := session.finish(t, false)
	requireExit(t, got, 130)
	select {
	case <-requestCanceled:
	case <-time.After(3 * time.Second):
		t.Fatal("Ctrl+C did not cancel the in-flight HTTP request")
	}
}

type liveREPL struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *replCapture
	stderr      *replCapture
	ctx         context.Context
	cancel      context.CancelFunc
	stdinClosed bool
	finished    bool
}

type replCapture struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	changed chan struct{}
	done    chan struct{}
}

func captureREPLStream(reader io.Reader) *replCapture {
	capture := &replCapture{changed: make(chan struct{}, 1), done: make(chan struct{})}
	go func() {
		defer close(capture.done)
		chunk := make([]byte, 2048)
		for {
			n, err := reader.Read(chunk)
			if n > 0 {
				capture.mu.Lock()
				_, _ = capture.buffer.Write(chunk[:n])
				capture.mu.Unlock()
				select {
				case capture.changed <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return capture
}

func (c *replCapture) snapshot() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.String()
}

func (c *replCapture) waitFor(t *testing.T, description string, ready func(string) bool) string {
	t.Helper()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	for {
		current := c.snapshot()
		if ready(current) {
			return current
		}
		select {
		case <-c.changed:
		case <-c.done:
			t.Fatalf("REPL output ended before %s; stdout=%q", description, c.snapshot())
		case <-timer.C:
			t.Fatalf("timed out waiting for %s; stdout=%q", description, c.snapshot())
		}
	}
}

func startLiveREPL(t *testing.T, dir string, env map[string]string) *liveREPL {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	cmd := exec.CommandContext(ctx, cliPath, "repl")
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = envWith(cmd.Env, key, value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		cancel()
		t.Fatalf("start noe repl: %v", err)
	}
	session := &liveREPL{
		cmd:    cmd,
		stdin:  stdin,
		stdout: captureREPLStream(stdout),
		stderr: captureREPLStream(stderr),
		ctx:    ctx,
		cancel: cancel,
	}
	t.Cleanup(session.abort)
	return session
}

func (s *liveREPL) waitPromptCount(t *testing.T, count int) {
	t.Helper()
	s.stdout.waitFor(t, fmt.Sprintf("prompt %d", count), func(output string) bool {
		return strings.Count(output, "> ") >= count
	})
}

func (s *liveREPL) writeLine(t *testing.T, line string) {
	t.Helper()
	if _, err := io.WriteString(s.stdin, line+"\n"); err != nil {
		t.Fatalf("write REPL input %q: %v", line, err)
	}
}

func (s *liveREPL) interrupt(t *testing.T) {
	t.Helper()
	if err := s.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send Ctrl+C to noe repl: %v", err)
	}
}

func (s *liveREPL) finish(t *testing.T, closeInput bool) cliResult {
	t.Helper()
	if closeInput && !s.stdinClosed {
		_ = s.stdin.Close()
		s.stdinClosed = true
	}
	timer := time.NewTimer(11 * time.Second)
	defer timer.Stop()
	for _, done := range []<-chan struct{}{s.stdout.done, s.stderr.done} {
		select {
		case <-done:
		case <-timer.C:
			t.Fatalf("noe repl did not finish within its process deadline; stdout=%q stderr=%q", s.stdout.snapshot(), s.stderr.snapshot())
		}
	}
	err := s.cmd.Wait()
	timedOut := errors.Is(s.ctx.Err(), context.DeadlineExceeded)
	s.cancel()
	s.finished = true
	if timedOut {
		t.Fatalf("noe repl exceeded the 10s process limit; stdout=%q stderr=%q", s.stdout.snapshot(), s.stderr.snapshot())
	}
	result := cliResult{stdout: s.stdout.snapshot(), stderr: s.stderr.snapshot()}
	if err == nil {
		return result
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("wait for noe repl: %v\nstdout:\n%s\nstderr:\n%s", err, result.stdout, result.stderr)
	}
	result.exitCode = exitErr.ExitCode()
	return result
}

func (s *liveREPL) abort() {
	if s.finished {
		return
	}
	if !s.stdinClosed {
		_ = s.stdin.Close()
		s.stdinClosed = true
	}
	s.cancel()
	<-s.stdout.done
	<-s.stderr.done
	_ = s.cmd.Wait()
	s.finished = true
}
