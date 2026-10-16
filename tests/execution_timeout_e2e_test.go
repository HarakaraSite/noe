//go:build e2e

package e2e_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func requireExecutionTimeout(t *testing.T, got cliResult) {
	t.Helper()
	requireExit(t, got, 124)
	if got.stdout != "STARTED\n" || !strings.Contains(got.stderr, "execution timed out after 200ms") || strings.Contains(got.stdout, "AFTER") {
		t.Fatalf("timeout result: %+v", got)
	}
}

func TestExecutionTimeoutCPUEvaluationAndJobs(t *testing.T) {
	for _, tc := range []struct{ name, mode, source string }{
		{"run loop", "run", `console.log("STARTED"); while (true) {} console.log("AFTER");`},
		{"eval loop", "eval", `console.log("STARTED"); while (true) {} console.log("AFTER");`},
		{"unawaited reaction loop", "eval", `Promise.resolve().then(() => { console.log("STARTED"); while (true) {} });`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			work := t.TempDir()
			args := []string{"eval", "--timeout=200ms", tc.source}
			if tc.mode == "run" {
				file := filepath.Join(work, "loop.js")
				writeFixture(t, file, tc.source)
				args = []string{"run", "--timeout", "200ms", file}
			}
			requireExecutionTimeout(t, runCLI(t, work, nil, args...))
		})
	}
}

func TestExecutionTimeoutHTTPWaitCancelsRequest(t *testing.T) {
	for _, awaited := range []bool{true, false} {
		t.Run(strconv.FormatBool(awaited), func(t *testing.T) {
			started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-r.Context().Done():
					close(canceled)
				case <-release:
					_, _ = w.Write([]byte("late response"))
				}
			}))
			t.Cleanup(func() { close(release); server.Close() })
			source := `console.log("STARTED"); const pending = fetch(` + strconv.Quote(server.URL) + `).then(() => console.log("AFTER"));`
			if awaited {
				source += `await pending;`
			}
			requireExecutionTimeout(t, runCLI(t, t.TempDir(), nil, "eval", "--timeout", "200ms", source))
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("HTTP request did not start")
			}
			select {
			case <-canceled:
			case <-time.After(2 * time.Second):
				t.Fatal("timeout did not cancel HTTP request")
			}
		})
	}
}

func TestExecutionTimeoutSIGINTInterruptsFetchWait(t *testing.T) {
	requestStarted := make(chan struct{}, 1)
	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		requestStarted <- struct{}{}
		<-r.Context().Done()
		close(requestCanceled)
	}))
	defer server.Close()
	process := startCLIWithOpenStdin(t, t.TempDir(), "eval", "--timeout", "5s", `console.log("STARTED"); await fetch(`+strconv.Quote(server.URL)+`); console.log("AFTER");`)
	if !process.stdout.waitFor("STARTED\n", 3*time.Second) {
		t.Fatal("fetch evaluation did not start")
	}
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP request did not start")
	}
	if err := process.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	got := process.wait(t, 3*time.Second)
	requireExit(t, got, 1)
	if got.stdout != "STARTED\n" || got.stderr == "" || strings.Contains(got.stderr, "execution timed out") {
		t.Fatalf("SIGINT reclassified as timeout: %+v", got)
	}
	select {
	case <-requestCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("SIGINT did not cancel the HTTP request")
	}
}

func TestExecutionTimeoutPreservesCLIBehavior(t *testing.T) {
	work := t.TempDir()
	file := filepath.Join(work, "args.js")
	writeFixture(t, file, `console.log(JSON.stringify(process.argv.slice(2)));`)
	args := runCLI(t, work, nil, "run", "--timeout", "5s", "--", file, "--timeout", "not-a-duration")
	requireExit(t, args, 0)
	if args.stdout != "[\"--timeout\",\"not-a-duration\"]\n" || args.stderr != "" {
		t.Fatalf("script arguments changed: %+v", args)
	}
	for _, tc := range []struct {
		name    string
		args    []string
		code    int
		stdout  string
		failure string
	}{
		{"success", []string{"eval", "--timeout=5s", `console.log(42)`}, 0, "42\n", ""},
		{"normal error", []string{"eval", "--timeout=5s", `throw new Error("ordinary failure")`}, 1, "", "ordinary failure"},
		{"explicit exit", []string{"eval", "--timeout=5s", `process.exit(7)`}, 7, "", ""},
		{"explicit 124", []string{"eval", "--timeout=5s", `process.exit(124)`}, 124, "", ""},
		{"negative expression", []string{"eval", `-1; console.log("negative expression")`}, 0, "negative expression\n", ""},
		{"zero duration", []string{"eval", "--timeout=0", `console.log("AFTER")`}, 2, "", "timeout"},
		{"REPL option", []string{"repl", "--timeout=1s"}, 2, "", "Usage:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := runCLI(t, work, nil, tc.args...)
			requireExit(t, got, tc.code)
			if got.stdout != tc.stdout || (tc.failure == "" && got.stderr != "") || (tc.failure != "" && !strings.Contains(got.stderr, tc.failure)) {
				t.Fatalf("compatibility result: %+v", got)
			}
		})
	}
}
