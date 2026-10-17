//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type cliResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func runCLI(t *testing.T, dir string, extraEnv map[string]string, args ...string) cliResult {
	t.Helper()
	return runCLICommand(t, dir, extraEnv, nil, args...)
}

func runCLIWithInput(t *testing.T, dir string, stdin string, args ...string) cliResult {
	t.Helper()
	return runCLICommand(t, dir, nil, strings.NewReader(stdin), args...)
}

func runREPLInput(t *testing.T, dir string, env map[string]string, input string) cliResult {
	t.Helper()
	return runCLICommand(t, dir, env, strings.NewReader(input), "repl")
}

func runCLICommand(t *testing.T, dir string, extraEnv map[string]string, stdin io.Reader, args ...string) cliResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliPath, args...)
	cmd.Dir = dir
	cmd.Stdin = stdin
	cmd.Env = os.Environ()
	for key, value := range extraEnv {
		cmd.Env = envWith(cmd.Env, key, value)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("noe %v exceeded the 10s process limit: %v\nstdout:\n%s\nstderr:\n%s", args, ctx.Err(), stdout.String(), stderr.String())
	}
	result := cliResult{stdout: stdout.String(), stderr: stderr.String()}
	if err == nil {
		return result
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run noe %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, result.stdout, result.stderr)
	}
	result.exitCode = exitErr.ExitCode()
	return result
}

func envWith(env []string, key, value string) []string {
	prefix := key + "="
	filtered := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, prefix) {
			filtered = append(filtered, item)
		}
	}
	return append(filtered, prefix+value)
}

func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func requireExit(t *testing.T, got cliResult, want int) {
	t.Helper()
	if got.exitCode != want {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got.exitCode, want, got.stdout, got.stderr)
	}
}
