package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if err := os.WriteFile(file, []byte(`console.log(JSON.stringify(process.argv.slice(2)));`), 0600); err != nil {
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
		{"explicit exit 124", `process.exit(124)`, 124, "", ""},
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
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"eval", "--timeout=100ms", "await new Promise(resolve => setTimeout(resolve, 5_000))"}, strings.NewReader(""), &stdout, &stderr)
	if code != 124 || !strings.Contains(stderr.String(), "execution timed out after 100ms") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "context deadline exceeded") {
		t.Fatalf("timeout diagnostic should be deterministic: %q", stderr.String())
	}
}

func TestRunParentDeadlineKeepsOrdinaryErrorCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var stdout, stderr bytes.Buffer
	code := Run(ctx, []string{"eval", "--timeout=5s", "await new Promise(resolve => setTimeout(resolve, 5_000))"}, strings.NewReader(""), &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "context deadline exceeded") || strings.Contains(stderr.String(), "execution timed out after") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
