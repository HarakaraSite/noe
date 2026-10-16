package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestREPLInputAndExit(t *testing.T) {
	for _, tc := range []struct {
		name, input, out, diagnostic string
		code                         int
	}{
		{"stdlib globals", "typeof noe\n", "> 'undefined'\n> ", "", 0},
		{"state", "let count = 1\ncount += 1\ncount\n", "> > 2\n> 2\n> ", "", 0},
		{"final line", "2 + 3", "> 5\n", "", 0},
		{"blank line", "\n\n", "> > > ", "", 0},
		{"recover", "const invalid = ;\nthrow new Error('boom')\n1 + 1\n", "> > > 2\n> ", "boom", 0},
		{"zero exit", "process.exit(0)\nconsole.log('after')\n", "> ", "", 0},
		{"nonzero exit", "process.exit(7)\nconsole.log('after')\n", "> ", "", 7},
		{"async exit", "await Promise.resolve(process.exit(9))\n", "> ", "", 9},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"repl"}, strings.NewReader(tc.input), &stdout, &stderr)
			if code != tc.code || stdout.String() != tc.out {
				t.Fatalf("code=%d out=%q err=%q", code, &stdout, &stderr)
			}
			if tc.diagnostic == "" {
				if stderr.Len() != 0 {
					t.Fatal(stderr.String())
				}
			} else if !strings.Contains(stderr.String(), tc.diagnostic) {
				t.Fatal(stderr.String())
			}
		})
	}
}

func TestREPLInputCancellationAndFailure(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"repl"}, reader, io.Discard, io.Discard) }()
	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("code=%d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("REPL did not stop while input blocked")
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"repl"}, failedReader{}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "input failed") {
		t.Fatalf("code=%d err=%q", code, &stderr)
	}
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errors.New("input failed") }
