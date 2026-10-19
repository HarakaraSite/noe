package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLI(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		code     int
		out, err string
	}{
		{"help", []string{"--help"}, 0, "noe run", ""},
		{"version", []string{"--version"}, 0, "noe v1.14.1\n", ""},
		{"eval", []string{"eval", `console.log("hello"); console.warn("warning"); console.error("error");`}, 0, "hello\n", "warning\nerror\n"},
		{"syntax", []string{"eval", `const broken = ;`}, 1, "", "<eval>.js:1:"},
		{"exception", []string{"eval", `throw new Error("boom")`}, 1, "", "boom"},
		{"primitive throw", []string{"eval", `throw "string error"`}, 1, "", "<eval>.js: string error"},
		{"usage", []string{"run"}, 2, "", "Usage:"},
		{"unknown", []string{"missing"}, 2, "", "unknown command"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), tc.args, strings.NewReader(""), &stdout, &stderr)
			if code != tc.code || !strings.Contains(stdout.String(), tc.out) || !strings.Contains(stderr.String(), tc.err) {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if tc.code == 0 && tc.err == "" && stderr.Len() != 0 {
				t.Fatalf("unexpected stderr: %s", &stderr)
			}
		})
	}
}

func TestRemovedREPLHelpAndUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), nil, strings.NewReader(""), &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "noe run") || strings.Contains(strings.ToLower(stdout.String()), "repl") || stderr.Len() != 0 {
		t.Fatalf("help code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"repl"}, strings.NewReader(""), &stdout, &stderr); code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), `unknown command "repl"`) || strings.Count(strings.ToLower(stderr.String()), "repl") != 1 {
		t.Fatalf("rejected command code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hello.js")
	if err := os.WriteFile(file, []byte(`console.log("hello from file")`), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"run", file}, strings.NewReader(""), &stdout, &stderr); code != 0 || stdout.String() != "hello from file\n" || stderr.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", &stdout, &stderr)
	}
	if err := os.WriteFile(file, []byte("function fail() { throw new Error('file error'); }\nfail();"), 0600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run(context.Background(), []string{"run", file}, strings.NewReader(""), &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), file) || !strings.Contains(stderr.String(), "file error") {
		t.Fatalf("code=%d stderr=%q", code, &stderr)
	}
}
