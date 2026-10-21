//go:build !windows

package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"forge.harakara.site/littleisland/noe/v2/internal/processscope"
)

// startDirect owns the target group without re-executing the host application.
func startDirect(ctx context.Context, config Config) (*Child, error) {
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
	cmd.Dir, cmd.Env = config.Dir, config.Env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdinRead, stdoutWrite, stderrWrite
	scope, err := processscope.New(cmd)
	if err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		return nil, err
	}
	if err = scope.Start(); err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		_ = scope.Close()
		return nil, fmt.Errorf("start process: %w", err)
	}
	closeFiles(stdinRead, stdoutWrite, stderrWrite)
	c := newChild(scope.PID(), stdinWrite, stdoutRead, stderrRead)
	// Signals and final group cleanup share this gate. WaitExit leaves the
	// leader unreaped until cleanup; no signal can use its PID after reaping.
	var scopeMu sync.Mutex
	exited := false
	signal := func(name string) error {
		if err := processscope.ValidateSignal(name); err != nil {
			return err
		}
		scopeMu.Lock()
		defer scopeMu.Unlock()
		if exited {
			return nil
		}
		return scope.Signal(name)
	}
	c.killFn = signal
	c.closeFn = func() error {
		killErr := signal("SIGKILL")
		pipeErr := closeFilesErr(stdinWrite, stdoutRead, stderrRead)
		_, waitErr := c.Wait()
		return errors.Join(killErr, pipeErr, waitErr)
	}
	go func() {
		exitErr := processscope.WaitExit(cmd.Process)
		scopeMu.Lock()
		killErr := scope.Kill()
		waitErr := cmd.Wait()
		exited = true
		scopeErr := scope.Close()
		scopeMu.Unlock()
		result, resultErr := processResult(cmd.ProcessState, waitErr)
		c.complete(result, errors.Join(exitErr, killErr, scopeErr, resultErr))
	}()
	c.bindContext(ctx)
	if err := ctx.Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}
