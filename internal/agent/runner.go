package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Runner starts a fresh NOE process for each JavaScript tool invocation.
type Runner struct {
	Executable string
	Cwd        string
	Connection *Connection
}

// Result is the observable outcome of a child NOE process. A non-zero exit,
// timeout, or cancellation is returned as a result; error is reserved for
// setup and supervision failures.
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
	Reason   string `json:"reason"`
}

const childSourcePrelude = "import { createAgent } from \"noe:agent\";\n"

type processScope interface {
	start() error
	terminate() error
	close() error
}

// Run writes code to a private temporary module, starts a fresh NOE process,
// captures both output streams, and terminates the process scope after the
// root process exits. The caller context is the only timeout source.
func (r Runner) Run(ctx context.Context, code string) (result Result, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return Result{ExitCode: -1, Reason: reasonForContext(ctx.Err())}, nil
	}
	if r.Executable == "" {
		return failedResult(), errors.New("runner executable is required")
	}
	executable, err := filepath.Abs(r.Executable)
	if err != nil {
		return failedResult(), fmt.Errorf("resolve runner executable: %w", err)
	}
	cwd := r.Cwd
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return failedResult(), fmt.Errorf("resolve runner working directory: %w", err)
		}
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return failedResult(), fmt.Errorf("resolve runner working directory: %w", err)
	}
	if r.Connection != nil {
		if r.Connection.URL == "" || r.Connection.Token == "" || r.Connection.Scope == "" {
			return failedResult(), &RPCError{Code: "connection", Message: "agent connection is incomplete"}
		}
		if err := validateConnectionURL(r.Connection.URL); err != nil {
			return failedResult(), err
		}
	}

	tempBase := sourceTempDirBase(cwd, os.TempDir())
	tempDir, err := os.MkdirTemp(tempBase, "noe-agent-run-")
	if err != nil {
		if tempBase != "" {
			return failedResult(), fmt.Errorf("create private agent source directory on working-directory volume %q: %w", cwd, err)
		}
		return failedResult(), fmt.Errorf("create private agent source directory: %w", err)
	}
	defer func() {
		retErr = errors.Join(retErr, os.RemoveAll(tempDir))
	}()
	sourcePath := filepath.Join(tempDir, "agent-run.js")
	if err := os.WriteFile(sourcePath, []byte(childSourcePrelude+code), 0600); err != nil {
		return failedResult(), fmt.Errorf("write private agent source: %w", err)
	}
	env, err := childEnvironment(os.Environ(), r.Connection)
	if err != nil {
		return failedResult(), fmt.Errorf("prepare agent child environment: %w", err)
	}
	if ctx.Err() != nil {
		return Result{ExitCode: -1, Reason: reasonForContext(ctx.Err())}, nil
	}

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		return failedResult(), fmt.Errorf("create child stdout pipe: %w", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		return failedResult(), fmt.Errorf("create child stderr pipe: %w", err)
	}
	closePipes := func() {
		_ = stdoutReader.Close()
		_ = stdoutWriter.Close()
		_ = stderrReader.Close()
		_ = stderrWriter.Close()
	}
	cmd := exec.Command(executable, "run", sourcePath)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	scope, err := newProcessScope(cmd)
	if err != nil {
		closePipes()
		return failedResult(), fmt.Errorf("prepare child process supervision: %w", err)
	}
	if ctx.Err() != nil {
		_ = scope.close()
		closePipes()
		return Result{ExitCode: -1, Reason: reasonForContext(ctx.Err())}, nil
	}
	if err := scope.start(); err != nil {
		var cleanupErr error
		if cmd.Process != nil {
			cleanupErr = errors.Join(cleanupErr, scope.terminate())
			cleanupErr = errors.Join(cleanupErr, killRoot(cmd.Process))
			cleanupErr = errors.Join(cleanupErr, cmd.Wait())
		}
		cleanupErr = errors.Join(cleanupErr, scope.close())
		closePipes()
		return failedResult(), errors.Join(fmt.Errorf("start child process: %w", err), cleanupErr)
	}
	// The parent must not retain write ends: only child processes should keep
	// them open while output collectors drain the streams.
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	var stdout, stderr bytes.Buffer
	var collectors sync.WaitGroup
	collectorErrors := make(chan error, 2)
	collectors.Add(2)
	go func() {
		defer collectors.Done()
		_, err := io.Copy(&stdout, stdoutReader)
		collectorErrors <- err
	}()
	go func() {
		defer collectors.Done()
		_, err := io.Copy(&stderr, stderrReader)
		collectorErrors <- err
	}()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	var waitErr error
	waitReceived := false
	select {
	case waitErr = <-waited:
		waitReceived = true
	case <-ctx.Done():
		result.Reason = reasonForContext(ctx.Err())
	}
	if ctx.Err() != nil {
		result.Reason = reasonForContext(ctx.Err())
	}
	terminateErr := scope.terminate()
	// POSIX scopes signal the group; Windows scopes terminate the Job Object.
	// Kill the root as a fallback if scope termination failed before assignment.
	if terminateErr != nil && cmd.Process != nil {
		terminateErr = errors.Join(terminateErr, killRoot(cmd.Process))
	}
	closeErr := scope.close()
	if !waitReceived {
		// Reap the root after terminating the process scope.
		waitErr = <-waited
		waitReceived = true
	}
	collectors.Wait()
	_ = stdoutReader.Close()
	_ = stderrReader.Close()
	close(collectorErrors)
	var streamErr error
	for err := range collectorErrors {
		streamErr = errors.Join(streamErr, err)
	}

	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.ExitCode = processExitCode(cmd)
	if result.Reason == "" {
		if waitErr == nil && result.ExitCode == 0 {
			result.Reason = "completed"
		} else {
			result.Reason = "failed"
		}
	}
	var waitFailure *exec.ExitError
	if waitErr != nil && !errors.As(waitErr, &waitFailure) {
		retErr = errors.Join(retErr, fmt.Errorf("wait for child process: %w", waitErr))
	}
	if terminateErr != nil {
		retErr = errors.Join(retErr, fmt.Errorf("terminate child process scope: %w", terminateErr))
	}
	if closeErr != nil {
		retErr = errors.Join(retErr, fmt.Errorf("close child process scope: %w", closeErr))
	}
	if streamErr != nil {
		retErr = errors.Join(retErr, fmt.Errorf("capture child output: %w", streamErr))
	}
	return result, retErr
}

func failedResult() Result {
	return Result{ExitCode: -1, Reason: "failed"}
}

func reasonForContext(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if err != nil {
		return "cancelled"
	}
	return ""
}

func processExitCode(cmd *exec.Cmd) int {
	if cmd.ProcessState == nil {
		return -1
	}
	return cmd.ProcessState.ExitCode()
}

// sourceTempDirBase keeps the generated module on the working-directory
// volume when the system temp directory is on another volume. NOE resolves
// relative module imports from the generated source path, so this preserves
// access to files below cwd on Windows. An empty base retains os.MkdirTemp's
// default system temp location.
func sourceTempDirBase(cwd, systemTemp string) string {
	cwdVolume := filepath.VolumeName(cwd)
	tempVolume := filepath.VolumeName(systemTemp)
	if cwdVolume != "" && tempVolume != "" && !strings.EqualFold(cwdVolume, tempVolume) {
		return cwd
	}
	return ""
}

func killRoot(process *os.Process) error {
	if process == nil {
		return nil
	}
	err := process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

func childEnvironment(parent []string, connection *Connection) ([]string, error) {
	var encoded []byte
	var err error
	if connection != nil {
		encoded, err = json.Marshal(connection)
		if err != nil {
			return nil, err
		}
	}
	env := make([]string, 0, len(parent)+1)
	for _, item := range parent {
		key, _, ok := strings.Cut(item, "=")
		if !ok || key == "" || isSensitiveChildEnv(key) {
			continue
		}
		env = append(env, item)
	}
	if connection != nil {
		env = append(env, "NOE_AGENT_CONNECTION="+string(encoded))
	}
	return env, nil
}

func isSensitiveChildEnv(key string) bool {
	if sameEnvironmentKey(key, "NOE_AGENT_CONNECTION") {
		return true
	}
	for _, name := range []string{
		"OPENROUTER_API_KEY",
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"NOE_OPENROUTER_API_KEY",
	} {
		if sameEnvironmentKey(key, name) {
			return true
		}
	}
	return false
}
