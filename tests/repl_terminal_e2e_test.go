//go:build e2e && linux

package e2e_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const (
	ptyWaitTimeout = 4 * time.Second
	ptyRunTimeout  = 10 * time.Second
)

type terminalREPL struct {
	cmd      *exec.Cmd
	master   *os.File
	slave    *os.File
	initial  unix.Termios
	stdout   *replCapture
	stderr   *replCapture
	ctx      context.Context
	cancel   context.CancelFunc
	finished bool
}

func startTerminalREPL(t *testing.T, dir string, env map[string]string) *terminalREPL {
	t.Helper()

	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("open PTY master: %v", err)
	}
	master := os.NewFile(uintptr(masterFD), "noe-pty-master")
	cleanupMaster := func() { _ = master.Close() }
	if err := unix.IoctlSetPointerInt(masterFD, unix.TIOCSPTLCK, 0); err != nil {
		cleanupMaster()
		t.Fatalf("unlock PTY slave: %v", err)
	}
	ptyNumber, err := unix.IoctlGetInt(masterFD, unix.TIOCGPTN)
	if err != nil {
		cleanupMaster()
		t.Fatalf("get PTY slave number: %v", err)
	}
	slaveFD, err := unix.Open(filepath.Join("/dev/pts", fmt.Sprint(ptyNumber)), unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		cleanupMaster()
		t.Fatalf("open PTY slave: %v", err)
	}
	slave := os.NewFile(uintptr(slaveFD), "noe-pty-slave")
	cleanupSlave := func() { _ = slave.Close() }
	initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		cleanupSlave()
		cleanupMaster()
		t.Fatalf("read initial PTY settings: %v", err)
	}
	if err := unix.IoctlSetWinsize(masterFD, unix.TIOCSWINSZ, &unix.Winsize{Row: 24, Col: 40}); err != nil {
		cleanupSlave()
		cleanupMaster()
		t.Fatalf("set PTY window size: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), ptyRunTimeout)
	cmd := exec.CommandContext(ctx, cliPath, "repl")
	cmd.Dir = dir
	cmd.Env = envWith(os.Environ(), "TERM", "xterm-256color")
	cmd.Env = envWith(cmd.Env, "NOE_REPL_HISTORY", "")
	for key, value := range env {
		cmd.Env = envWith(cmd.Env, key, value)
	}
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		cleanupSlave()
		cleanupMaster()
		t.Fatalf("create REPL stderr pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = stderr.Close()
		cleanupSlave()
		cleanupMaster()
		t.Fatalf("start noe repl on PTY: %v", err)
	}
	session := &terminalREPL{
		cmd:     cmd,
		master:  master,
		slave:   slave,
		initial: *initial,
		stdout:  captureREPLStream(master),
		stderr:  captureREPLStream(stderr),
		ctx:     ctx,
		cancel:  cancel,
	}
	t.Cleanup(session.abort)
	session.waitForEditMode(t)
	session.waitInitialPrompt(t)
	return session
}

func (s *terminalREPL) waitInitialPrompt(t *testing.T) {
	t.Helper()
	s.stdout.waitFor(t, "initial REPL prompt", func(output string) bool {
		return strings.Contains(output, "> ")
	})
}

func (s *terminalREPL) waitForEditMode(t *testing.T) {
	t.Helper()
	deadline := time.NewTimer(ptyWaitTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := unix.IoctlGetTermios(int(s.slave.Fd()), unix.TCGETS)
		if err == nil && current.Lflag&(unix.ICANON|unix.ECHO) == 0 {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("REPL did not enter terminal edit mode; termios=%+v err=%v", current, err)
		}
	}
}

func (s *terminalREPL) writeKeys(t *testing.T, keys string) {
	t.Helper()
	if _, err := s.master.Write([]byte(keys)); err != nil {
		t.Fatalf("write keys to REPL PTY: %v", err)
	}
}

func (s *terminalREPL) enter(t *testing.T, line string) string {
	t.Helper()
	before := s.stdout.snapshot()
	s.writeKeys(t, line+"\r")
	s.waitForPromptAfter(t, before)
	return before
}

func normalizePTYOutput(output string) string {
	return strings.ReplaceAll(output, "\r\n", "\n")
}

func (s *terminalREPL) waitForPromptAfter(t *testing.T, before string) {
	t.Helper()
	s.stdout.waitFor(t, "next REPL prompt", func(output string) bool {
		if len(output) < len(before) {
			return false
		}
		tail := normalizePTYOutput(output[len(before):])
		return strings.Contains(tail, "\n> ") || strings.HasSuffix(tail, "> ")
	})
}

func (s *terminalREPL) waitForOutputLine(t *testing.T, line string) {
	t.Helper()
	s.stdout.waitFor(t, "console output line "+line, func(output string) bool {
		for _, candidate := range strings.Split(normalizePTYOutput(output), "\n") {
			if strings.TrimRight(candidate, "\r") == line {
				return true
			}
		}
		return false
	})
}

func (s *terminalREPL) waitForOutputAndPrompt(t *testing.T, line string) {
	t.Helper()
	s.stdout.waitFor(t, "console output and following prompt for "+line, func(output string) bool {
		normalized := normalizePTYOutput(output)
		marker := line + "\n"
		index := strings.LastIndex(normalized, marker)
		return index >= 0 && strings.Contains(normalized[index+len(marker):], "> ")
	})
}

func (s *terminalREPL) waitForError(t *testing.T, marker string) {
	t.Helper()
	s.stderr.waitFor(t, "REPL diagnostic "+marker, func(output string) bool {
		return strings.Contains(output, marker)
	})
}

func (s *terminalREPL) waitForSearchPrompt(t *testing.T) {
	t.Helper()
	s.stdout.waitFor(t, "reverse history search mode", func(output string) bool {
		return strings.Contains(output, "(reverse-i-search)")
	})
}

func (s *terminalREPL) interruptKey(t *testing.T) {
	t.Helper()
	s.writeKeys(t, "\x03")
}

func (s *terminalREPL) interruptAndContinue(t *testing.T) {
	t.Helper()
	before := s.stdout.snapshot()
	s.interruptKey(t)
	s.waitForError(t, "Interrupted")
	s.waitForPromptAfter(t, before)
}

func (s *terminalREPL) finish(t *testing.T, wantExit int) cliResult {
	t.Helper()
	wait := make(chan error, 1)
	go func() { wait <- s.cmd.Wait() }()
	var err error
	select {
	case err = <-wait:
	case <-time.After(ptyRunTimeout + time.Second):
		t.Fatalf("noe repl did not exit; stdout=%q stderr=%q", s.stdout.snapshot(), s.stderr.snapshot())
	}
	if errors.Is(s.ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("noe repl exceeded the %s process limit; stdout=%q stderr=%q", ptyRunTimeout, s.stdout.snapshot(), s.stderr.snapshot())
	}
	if current, ioctlErr := unix.IoctlGetTermios(int(s.slave.Fd()), unix.TCGETS); ioctlErr != nil {
		t.Errorf("read PTY settings after REPL exit: %v", ioctlErr)
	} else if *current != s.initial {
		t.Errorf("REPL did not restore PTY settings: before=%+v after=%+v", s.initial, *current)
	}
	_ = s.slave.Close()
	_ = s.master.Close()
	select {
	case <-s.stdout.done:
	case <-time.After(ptyWaitTimeout):
		t.Errorf("PTY stdout capture did not finish; stdout=%q", s.stdout.snapshot())
	}
	select {
	case <-s.stderr.done:
	case <-time.After(ptyWaitTimeout):
		t.Errorf("stderr capture did not finish; stderr=%q", s.stderr.snapshot())
	}
	s.cancel()
	s.finished = true
	result := cliResult{stdout: s.stdout.snapshot(), stderr: s.stderr.snapshot()}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("wait for noe repl: %v\nstdout:\n%s\nstderr:\n%s", err, result.stdout, result.stderr)
		}
		result.exitCode = exitErr.ExitCode()
	}
	if result.exitCode != wantExit {
		t.Fatalf("REPL exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", result.exitCode, wantExit, result.stdout, result.stderr)
	}
	return result
}

func (s *terminalREPL) abort() {
	if s.finished {
		return
	}
	s.cancel()
	_ = s.slave.Close()
	_ = s.master.Close()
	_ = s.cmd.Wait()
	<-s.stdout.done
	<-s.stderr.done
	s.finished = true
}

func TestREPLTerminalEditingHistoryAndPersistentState(t *testing.T) {
	session := startTerminalREPL(t, t.TempDir(), nil)

	// Move left into the line, insert a rune, and evaluate the corrected line.
	session.writeKeys(t, `console.log("EDT")`+strings.Repeat("\x1b[D", 3)+"I\r")
	session.waitForOutputAndPrompt(t, "EDIT")

	// Delete removes the character under the cursor after moving left.
	session.writeKeys(t, `console.log("DELETEX")`+strings.Repeat("\x1b[D", 3)+"\x1b[3~\r")
	session.waitForOutputAndPrompt(t, "DELETE")

	// Backspace removes the trailing invalid character before Enter.
	session.writeKeys(t, `console.log("BACKSPACE")x`+"\x7f\r")
	session.waitForOutputAndPrompt(t, "BACKSPACE")

	// Editing a Japanese rune verifies cursor and deletion handling on UTF-8 input.
	session.writeKeys(t, `console.log("日本語誤")`+strings.Repeat("\x1b[D", 2)+"\x7f編\r")
	session.waitForOutputAndPrompt(t, "日本語編")

	longValue := strings.Repeat("long-line-", 9)
	session.writeKeys(t, `console.log("LONG:`+longValue+`")`+"\r")
	session.waitForOutputAndPrompt(t, "LONG:"+longValue)

	session.enter(t, `console.log("HISTORY:ORIGINAL")`)
	session.waitForOutputLine(t, "HISTORY:ORIGINAL")
	session.writeKeys(t, "\x1b[A; console.log(\"HISTORY:MODIFIED\")\r")
	session.waitForOutputAndPrompt(t, "HISTORY:MODIFIED")

	session.enter(t, "let terminalState = 42")
	session.enter(t, `throw new Error("PTY_ERROR_MARKER")`)
	session.waitForError(t, "PTY_ERROR_MARKER")
	session.writeKeys(t, `console.log("STATE:" + terminalState)`+"\r")
	session.waitForOutputAndPrompt(t, "STATE:42")

	session.writeKeys(t, "\x04")
	result := session.finish(t, 0)
	if !strings.Contains(result.stderr, "PTY_ERROR_MARKER") {
		t.Fatalf("missing expected JS error diagnostic: %q", result.stderr)
	}
}

func TestREPLTerminalEOFAndExplicitExitRestoreSettings(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		code  int
	}{
		{name: "Ctrl+D EOF", input: "\x04", code: 0},
		{name: "explicit exit", input: "noe.exit(7)\r", code: 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			session := startTerminalREPL(t, t.TempDir(), nil)
			session.writeKeys(t, tc.input)
			result := session.finish(t, tc.code)
			requireExit(t, result, tc.code)
		})
	}
}

func TestREPLTerminalCtrlCInHistorySearchRestoresSettings(t *testing.T) {
	session := startTerminalREPL(t, t.TempDir(), nil)
	session.writeKeys(t, `console.log("SEARCH_SEED")`+"\r")
	session.waitForOutputAndPrompt(t, "SEARCH_SEED")
	session.writeKeys(t, "\x12")
	session.waitForSearchPrompt(t)
	session.interruptAndContinue(t)
	session.writeKeys(t, `console.log("SEARCH_CONTINUED")`+"\r")
	session.waitForOutputAndPrompt(t, "SEARCH_CONTINUED")
	session.writeKeys(t, "\x04")
	result := session.finish(t, 0)
	if !strings.Contains(result.stdout, "SEARCH_SEED") {
		t.Fatalf("history seed was not evaluated before search: %q", result.stdout)
	}
}

func TestREPLTerminalCtrlCDuringInputReturnsToFreshPrompt(t *testing.T) {
	t.Run("edited line", func(t *testing.T) {
		session := startTerminalREPL(t, t.TempDir(), nil)
		session.writeKeys(t, `console.log("DISCARDED_EDITED_INPUT")`)
		session.interruptAndContinue(t)
		session.writeKeys(t, `console.log("INPUT_AFTER_INTERRUPT")`+"\r")
		session.waitForOutputAndPrompt(t, "INPUT_AFTER_INTERRUPT")
		session.writeKeys(t, "\x04")
		result := session.finish(t, 0)
		assertNoPTYOutputLine(t, result.stdout, "DISCARDED_EDITED_INPUT")
	})

	t.Run("held multiline input", func(t *testing.T) {
		session := startTerminalREPL(t, t.TempDir(), nil)
		session.enterContinuation(t, "function abandonedFunction() {")
		session.interruptAndContinue(t)
		session.writeKeys(t, `console.log("HELD_FUNCTION_TYPE:" + typeof abandonedFunction)`+"\r")
		session.waitForOutputAndPrompt(t, "HELD_FUNCTION_TYPE:undefined")
		session.writeKeys(t, "\x04")
		session.finish(t, 0)
	})

	t.Run("empty line", func(t *testing.T) {
		session := startTerminalREPL(t, t.TempDir(), nil)
		session.interruptAndContinue(t)
		session.writeKeys(t, `console.log("EMPTY_INTERRUPT_CONTINUED")`+"\r")
		session.waitForOutputAndPrompt(t, "EMPTY_INTERRUPT_CONTINUED")
		session.writeKeys(t, "\x04")
		session.finish(t, 0)
	})
}

func TestREPLTerminalCtrlCDuringEvaluationAndHTTPWaitContinues(t *testing.T) {
	t.Run("evaluation", func(t *testing.T) {
		session := startTerminalREPL(t, t.TempDir(), nil)
		session.enter(t, "let terminalState = 40")
		before := session.stdout.snapshot()
		session.writeKeys(t, `terminalState += 2; console.log("PTY_EVALUATION_STARTED"); while (true) {}`+"\r")
		session.waitForOutputLine(t, "PTY_EVALUATION_STARTED")
		session.interruptKey(t)
		session.waitForError(t, "Interrupted")
		session.waitForPromptAfter(t, before)
		session.writeKeys(t, `console.log("STATE_AFTER_EVALUATION_INTERRUPT:" + terminalState)`+"\r")
		session.waitForOutputAndPrompt(t, "STATE_AFTER_EVALUATION_INTERRUPT:42")
		session.writeKeys(t, "\x04")
		result := session.finish(t, 0)
		if !strings.Contains(result.stdout, "PTY_EVALUATION_STARTED") {
			t.Fatalf("evaluation readiness was not printed: %q", result.stdout)
		}
	})

	t.Run("HTTP wait", func(t *testing.T) {
		requestStarted := make(chan struct{}, 1)
		requestCanceled := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestStarted <- struct{}{}
			<-r.Context().Done()
			close(requestCanceled)
			_, _ = fmt.Fprint(w, "late body")
		}))
		defer server.Close()
		session := startTerminalREPL(t, t.TempDir(), nil)
		session.enter(t, "let httpState = 42")
		before := session.stdout.snapshot()
		session.writeKeys(t, `await fetch("`+server.URL+`").then(response => response.text()).then(() => console.log("HTTP_LATE_OUTPUT"))`+"\r")
		select {
		case <-requestStarted:
		case <-time.After(ptyWaitTimeout):
			t.Fatal("local HTTP request did not start before Ctrl+C")
		}
		session.interruptKey(t)
		session.waitForError(t, "Interrupted")
		session.waitForPromptAfter(t, before)
		select {
		case <-requestCanceled:
		case <-time.After(ptyWaitTimeout):
			t.Fatal("Ctrl+C did not cancel the local HTTP request")
		}
		session.writeKeys(t, `console.log("HTTP_STATE_AFTER_INTERRUPT:" + httpState)`+"\r")
		session.waitForOutputAndPrompt(t, "HTTP_STATE_AFTER_INTERRUPT:42")
		session.writeKeys(t, "\x04")
		result := session.finish(t, 0)
		assertNoPTYOutputLine(t, result.stdout, "HTTP_LATE_OUTPUT")
	})
}

func assertNoPTYOutputLine(t *testing.T, output, line string) {
	t.Helper()
	for _, candidate := range strings.Split(normalizePTYOutput(output), "\n") {
		if strings.TrimRight(candidate, "\r") == line {
			t.Fatalf("PTY output unexpectedly contained line %q: %q", line, output)
		}
	}
}

func TestREPLTerminalExternalSIGINTRestoresSettings(t *testing.T) {
	session := startTerminalREPL(t, t.TempDir(), nil)
	if err := session.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("send external SIGINT: %v", err)
	}
	result := session.finish(t, 130)
	requireExit(t, result, 130)
}

func TestREPLPipeWithDumbTermKeepsPlainPromptAndLineInput(t *testing.T) {
	got := runREPLInput(t, t.TempDir(), map[string]string{"TERM": "dumb"}, `console.log("PIPE_DUMB_OK")`+"\n")
	requireExit(t, got, 0)
	if got.stdout != "> PIPE_DUMB_OK\n> " {
		t.Fatalf("TERM=dumb piped REPL stdout = %q, want plain prompts and evaluated input", got.stdout)
	}
	if strings.ContainsAny(got.stdout, "\x1b") || got.stderr != "" {
		t.Fatalf("piped dumb terminal emitted editing controls or diagnostics: stdout=%q stderr=%q", got.stdout, got.stderr)
	}
}
