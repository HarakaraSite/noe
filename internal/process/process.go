// Package process starts and owns a child process with streaming standard I/O.
package process

import (
	"context"
	"errors"
	"os"
	"sync"
)

// Config describes a command to start directly, without a shell.
// Env == nil inherits the caller environment. A non-nil empty Env starts the
// command with an empty environment; otherwise Env is passed to the OS as-is.
type Config struct {
	Command string
	Args    []string
	Dir     string
	Env     []string

	// Supervisor explicitly selects a POSIX helper executable. Empty starts
	// directly. A helper main must call DispatchHelper before application work.
	// Windows uses a Job Object and does not need a helper.
	Supervisor string `json:"-"`

	// testStartGatePath is used only by package tests to hold a startup reply
	// after the target has been spawned. It is passed out-of-band to the POSIX
	// supervisor and is never part of the target command configuration.
	testStartGatePath string
}

// Result is the target command's exit status. Code is present for ordinary
// exits, including nonzero exits. Signal is present when POSIX reports that
// the target was terminated by a signal.
type Result struct {
	Code   *int   `json:"code,omitempty"`
	Signal string `json:"signal,omitempty"`
}

// Child is an owned child process and its standard I/O pipes. PID is the PID
// of the requested command, not an internal supervisor process.
type Child struct {
	Stdin  *os.File
	Stdout *os.File
	Stderr *os.File
	PID    int
	Done   <-chan struct{}

	supervisorPID   int
	done            chan struct{}
	mu              sync.Mutex
	result          Result
	waitErr         error
	killFn          func(string) error
	cleanupGroupFn  func() error
	closeIdentityFn func() error
	closeFn         func() error
	closeOnce       sync.Once
	closeDone       chan struct{}
	closeErr        error
	stopContext     func() bool
}

// Wait waits for the target command and returns its exit result. It is safe to
// call Wait more than once.
func (c *Child) Wait() (Result, error) {
	<-c.done
	c.mu.Lock()
	defer c.mu.Unlock()
	return cloneResult(c.result), c.waitErr
}

// Kill sends a signal to the owned process scope. On Windows SIGTERM and
// SIGKILL both terminate the owned Job Object.
func (c *Child) Kill(signal string) error {
	select {
	case <-c.done:
		return nil
	default:
	}
	return c.killFn(signal)
}

// Close is idempotent. It stops any process that is still alive, closes this
// handle's owned pipes, and waits for process supervision to finish.
func (c *Child) Close() error {
	c.closeOnce.Do(func() {
		if c.closeFn != nil {
			c.closeErr = c.closeFn()
		}
		close(c.closeDone)
	})
	<-c.closeDone
	return c.closeErr
}

func newChild(pid int, stdin, stdout, stderr *os.File) *Child {
	done := make(chan struct{})
	return &Child{
		Stdin: stdin, Stdout: stdout, Stderr: stderr,
		PID: pid, Done: done, done: done, closeDone: make(chan struct{}),
	}
}

func (c *Child) complete(result Result, err error) {
	c.mu.Lock()
	c.result = cloneResult(result)
	c.waitErr = err
	close(c.done)
	stopContext := c.stopContext
	c.stopContext = nil
	c.mu.Unlock()
	if stopContext != nil {
		stopContext()
	}
}

func (c *Child) bindContext(ctx context.Context) {
	if ctx == nil {
		return
	}
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	c.mu.Lock()
	if isDone(c.done) {
		c.mu.Unlock()
		stop()
		return
	}
	c.stopContext = stop
	c.mu.Unlock()
}

func cloneResult(result Result) Result {
	if result.Code != nil {
		code := *result.Code
		result.Code = &code
	}
	return result
}

func isDone(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func validateConfig(ctx context.Context, config Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if config.Command == "" {
		return errors.New("process command is empty")
	}
	return nil
}
