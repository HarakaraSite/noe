package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseExecutionOptions(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		timeout  time.Duration
		operands []string
		err      string
	}{
		{"separate value", []string{"--timeout", "5s", "script.js", "arg"}, 5 * time.Second, []string{"script.js", "arg"}, ""},
		{"equals value", []string{"--timeout=500ms", "code"}, 500 * time.Millisecond, []string{"code"}, ""},
		{"no option", []string{"-1", "--timeout=7s"}, 0, []string{"-1", "--timeout=7s"}, ""},
		{"unknown option is operand", []string{"--other", "code"}, 0, []string{"--other", "code"}, ""},
		{"options stop at source", []string{"script.js", "--timeout=7s"}, 0, []string{"script.js", "--timeout=7s"}, ""},
		{"terminator", []string{"--", "--timeout=7s", "code"}, 0, []string{"--timeout=7s", "code"}, ""},
		{"empty duration", []string{"--timeout=", "code"}, 0, nil, "invalid --timeout duration"},
		{"invalid duration", []string{"--timeout", "soon", "code"}, 0, nil, "invalid --timeout duration"},
		{"zero duration", []string{"--timeout=0s", "code"}, 0, nil, "must be positive"},
		{"negative duration", []string{"--timeout", "-1s", "code"}, 0, nil, "must be positive"},
		{"duration overflow", []string{"--timeout=999999999999999999999999999999999999h", "code"}, 0, nil, "invalid --timeout duration"},
		{"missing duration", []string{"--timeout"}, 0, nil, "requires a duration"},
		{"duplicate forms", []string{"--timeout", "5s", "--timeout=1s", "code"}, 0, nil, "specified only once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			timeout, operands, err := parseExecutionOptions(tc.args)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("error=%v, want substring %q", err, tc.err)
				}
				return
			}
			if err != nil || timeout != tc.timeout || !reflect.DeepEqual(operands, tc.operands) {
				t.Fatalf("timeout=%s operands=%q err=%v", timeout, operands, err)
			}
		})
	}
}

func TestRunExecutionOptionsPreserveOperands(t *testing.T) {
	file := filepath.Join(t.TempDir(), "args.js")
	if err := os.WriteFile(file, []byte(`console.log(JSON.stringify(noe.args));`), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"run", "--timeout", "5s", file, "--timeout", "9s", "--", "--other"}
	if code := Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if got, want := stdout.String(), `["--timeout","9s","--","--other"]`+"\n"; got != want {
		t.Fatalf("stdout=%q want=%q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunExecutionOptionUsageErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"zero timeout", []string{"eval", "--timeout=0s", "1"}, "must be positive"},
		{"duplicate timeout", []string{"eval", "--timeout", "1s", "--timeout=2s", "1"}, "specified only once"},
		{"missing duration", []string{"eval", "--timeout"}, "requires a duration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), tc.args, strings.NewReader(""), &stdout, &stderr); code != 2 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr=%q, want substring %q", stderr.String(), tc.want)
			}
		})
	}
}

func TestRunEvalTerminatorAndLegacyOperands(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{
		{"negative expression", []string{"eval", "-1"}, 0, ""},
		{"negative expression after terminator", []string{"eval", "--", "-1"}, 0, ""},
		{"unknown flag-like code is operand", []string{"eval", "--other"}, 1, "<eval>.js:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), tc.args, strings.NewReader(""), &stdout, &stderr); code != tc.code {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) || (tc.code != 2 && strings.Contains(stderr.String(), "Usage:")) {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunWithExecutionTimeoutPreservesExitResults(t *testing.T) {
	for _, tc := range []struct {
		name       string
		code       string
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{"success", `console.log("done")`, 0, "done\n", ""},
		{"execution error", `throw new Error("boom")`, 1, "", "boom"},
		{"explicit exit 124", `noe.exit(124)`, 124, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"eval", "--timeout=5s", tc.code}, strings.NewReader(""), &stdout, &stderr)
			if code != tc.wantCode || stdout.String() != tc.wantStdout || !strings.Contains(stderr.String(), tc.wantStderr) || (tc.wantStderr == "" && stderr.Len() != 0) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunExecutionTimeout(t *testing.T) {
	reader := newGateReader()
	code, _, stderr := runWaitingForStdin(t, context.Background(), []string{"eval", "--timeout=1s", "await noe.readStdin()"}, reader)
	if code != 124 || !strings.Contains(stderr, "execution timed out after 1s") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if strings.Contains(stderr, "context deadline exceeded") {
		t.Fatalf("timeout diagnostic should be deterministic: %q", stderr)
	}
}

func TestRunParentDeadlineKeepsOrdinaryErrorCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reader := newGateReader()
	code, _, stderr := runWaitingForStdin(t, ctx, []string{"eval", "--timeout=5s", "await noe.readStdin()"}, reader)
	if code != 1 || !strings.Contains(stderr, "context deadline exceeded") || strings.Contains(stderr, "execution timed out after") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

// gateReader provides a blocking read that the test can always release. It
// keeps timeout tests bounded without relying on an interruptible borrowed
// reader or leaving a worker blocked after Execute returns.
type gateReader struct {
	ready       chan struct{}
	release     chan struct{}
	finished    chan struct{}
	readyOnce   sync.Once
	releaseOnce sync.Once
}

func newGateReader() *gateReader {
	return &gateReader{ready: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
}

func (r *gateReader) Read([]byte) (int, error) {
	r.readyOnce.Do(func() { close(r.ready) })
	<-r.release
	close(r.finished)
	return 0, io.EOF
}

func (r *gateReader) unblock() { r.releaseOnce.Do(func() { close(r.release) }) }

func runWaitingForStdin(t *testing.T, ctx context.Context, args []string, reader *gateReader) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	var code int
	done := make(chan struct{})
	go func() {
		code = Run(ctx, args, reader, &stdout, &stderr)
		close(done)
	}()
	defer func() {
		reader.unblock()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("CLI execution did not return after releasing stdin")
		}
		select {
		case <-reader.ready:
			select {
			case <-reader.finished:
			case <-time.After(5 * time.Second):
				t.Error("stdin reader did not finish after release")
			}
		default:
		}
	}()

	select {
	case <-reader.ready:
	case <-done:
		t.Fatalf("CLI returned before reading stdin: code=%d stderr=%q", code, stderr.String())
	case <-time.After(5 * time.Second):
		t.Fatal("CLI did not begin reading stdin")
	}
	select {
	case <-done:
		return code, stdout.String(), stderr.String()
	case <-time.After(5 * time.Second):
		t.Fatal("CLI execution did not return before the outer test deadline")
		return 0, "", ""
	}
}
