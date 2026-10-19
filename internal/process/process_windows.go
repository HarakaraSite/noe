//go:build windows

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"forge.harakara.site/littleisland/noe/v2/internal/processscope"
)

// Start starts config.Command directly and returns its live standard I/O pipes.
// A Job Object owns the target and its descendants. Windows maps SIGTERM and
// SIGKILL to forced termination of that Job Object.
func Start(ctx context.Context, config Config) (*Child, error) {
	if ctx == nil {
		return nil, errors.New("process context is nil")
	}
	if err := validateConfig(ctx, config); err != nil {
		return nil, err
	}
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		closeFiles(stdinRead, stdinWrite)
		return nil, err
	}
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite)
		return nil, err
	}
	cmd := exec.Command(config.Command, config.Args...)
	cmd.Dir = config.Dir
	cmd.Env = config.Env
	cmd.Stdin = stdinRead
	cmd.Stdout = stdoutWrite
	cmd.Stderr = stderrWrite
	scope, err := processscope.New(cmd)
	if err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		return nil, err
	}
	if err := scope.Start(); err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		_ = scope.Close()
		return nil, fmt.Errorf("start process: %w", err)
	}
	closeFiles(stdinRead, stdoutWrite, stderrWrite)
	c := newChild(scope.PID(), stdinWrite, stdoutRead, stderrRead)
	c.killFn = scope.Signal
	c.closeFn = func() error {
		killErr := scope.Kill()
		pipeErr := closeFilesErr(stdinWrite, stdoutRead, stderrRead)
		_, waitErr := c.Wait()
		return errors.Join(killErr, pipeErr, waitErr)
	}
	go func() {
		waitErr := cmd.Wait()
		result, resultErr := windowsProcessResult(cmd.ProcessState, waitErr)
		killErr := scope.Kill()
		closeErr := scope.Close()
		c.complete(result, errors.Join(resultErr, killErr, closeErr))
	}()
	if ctx.Err() != nil {
		_ = c.Close()
		return nil, ctx.Err()
	}
	c.bindContext(ctx)
	if ctx.Err() != nil {
		_ = c.Close()
		return nil, ctx.Err()
	}
	return c, nil
}

func windowsProcessResult(state *os.ProcessState, waitErr error) (Result, error) {
	if state == nil {
		return Result{}, errors.Join(errors.New("target process has no exit state"), waitErr)
	}
	code := state.ExitCode()
	if code < 0 {
		return Result{}, errors.Join(errors.New("target process exit code is unavailable"), waitErr)
	}
	return Result{Code: &code}, nil
}

func closeFiles(files ...*os.File) {
	_ = closeFilesErr(files...)
}

func closeFilesErr(files ...*os.File) error {
	var err error
	for _, file := range files {
		if file != nil {
			closeErr := file.Close()
			if errors.Is(closeErr, os.ErrClosed) {
				closeErr = nil
			}
			err = errors.Join(err, closeErr)
		}
	}
	return err
}

func init() {
	// The private supervisor is a POSIX-only mode. Never let an accidental
	// invocation with its reserved argument fall through to the CLI or test run.
	if len(os.Args) == 2 && os.Args[1] == "--noe-private-process-supervisor-v1" {
		os.Exit(125)
	}
}
