// Package runner owns the operating-system side of child process execution.
// It has no dependency on the agent package; callers provide the complete
// command, working directory, and child environment.
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"forge.harakara.site/littleisland/noe/v2/internal/processscope"
)

// Result is the observable outcome of a child process. A non-zero exit,
// timeout, or cancellation is returned as a result; error is reserved for
// setup and supervision failures.
type Result struct {
	Stdout             string `json:"stdout"`
	Stderr             string `json:"stderr"`
	ExitCode           int    `json:"exitCode"`
	Reason             string `json:"reason"`
	StdoutOmittedBytes int64  `json:"stdoutOmittedBytes,omitempty"`
	StderrOmittedBytes int64  `json:"stderrOmittedBytes,omitempty"`
}

// Command contains all inputs needed to start one child. Callers supply
// absolute Executable and Cwd paths. Env is explicit: a nil or empty slice
// does not request inheritance of the caller's environment.
type Command struct {
	Executable string
	Args       []string
	Cwd        string
	Env        []string
}

// Run starts and supervises one process scope, drains both output streams,
// and reaps the root process before returning. It does not construct a child
// environment or resolve application-specific command arguments.
func Run(ctx context.Context, command Command) (result Result, retErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return cancelledResult(ctx.Err()), nil
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

	cmd := exec.Command(command.Executable, command.Args...)
	cmd.Dir = command.Cwd
	cmd.Env = command.Env
	if cmd.Env == nil {
		cmd.Env = []string{}
	}
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	scope, err := processscope.New(cmd)
	if err != nil {
		closePipes()
		return failedResult(), fmt.Errorf("prepare child process supervision: %w", err)
	}
	if ctx.Err() != nil {
		_ = scope.Close()
		closePipes()
		return cancelledResult(ctx.Err()), nil
	}
	if err := scope.Start(); err != nil {
		var cleanupErr error
		if cmd.Process != nil {
			cleanupErr = errors.Join(cleanupErr, scope.Kill())
			cleanupErr = errors.Join(cleanupErr, killRoot(cmd.Process))
			cleanupErr = errors.Join(cleanupErr, cmd.Wait())
		}
		cleanupErr = errors.Join(cleanupErr, scope.Close())
		closePipes()
		return failedResult(), errors.Join(fmt.Errorf("start child process: %w", err), cleanupErr)
	}
	// The parent must not retain write ends: only child processes should keep
	// them open while output collectors drain the streams.
	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	var stdout, stderr capturedOutput
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
	terminateErr := scope.Kill()
	// POSIX scopes signal the group; Windows scopes terminate the Job Object.
	// Kill the root as a fallback if scope termination failed before assignment.
	if terminateErr != nil && cmd.Process != nil {
		terminateErr = errors.Join(terminateErr, killRoot(cmd.Process))
	}
	closeErr := scope.Close()
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

	result.Stdout, result.StdoutOmittedBytes = stdout.result()
	result.Stderr, result.StderrOmittedBytes = stderr.result()
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

func cancelledResult(err error) Result {
	return Result{ExitCode: -1, Reason: reasonForContext(err)}
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
