package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestREPLInputCleanupOnReadFailureAndInterrupt(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
	}{
		{"read failure", errors.New("input failed"), 1},
		{"terminal interrupt", errREPLInterrupt, 130},
		{"EOF", io.EOF, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := &failingREPLInput{err: tc.err}
			var stdout, stderr bytes.Buffer
			got := runREPLWithInput(context.Background(), input, &stdout, &stderr)
			if got != tc.code || !input.closed {
				t.Fatalf("code=%d closed=%v stderr=%q", got, input.closed, &stderr)
			}
			if tc.code == 1 && !strings.Contains(stderr.String(), "input failed") {
				t.Fatalf("stderr=%q", &stderr)
			}
		})
	}
}

type failingREPLInput struct {
	err    error
	closed bool
}

func (f *failingREPLInput) readLine(string) (string, error) { return "", f.err }
func (f *failingREPLInput) close() error                    { f.closed = true; return nil }
