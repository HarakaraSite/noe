//go:build !windows

package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/unix"
	"noe/internal/processscope"
)

const (
	supervisorArg   = "--noe-private-process-supervisor-v1"
	supervisorMagic = "NOE-PROCESS-SUPERVISOR-V1"
)

type helperMessage struct {
	Magic         string  `json:"magic,omitempty"`
	Type          string  `json:"type,omitempty"`
	Config        *Config `json:"config,omitempty"`
	PID           int     `json:"pid,omitempty"`
	Signal        string  `json:"signal,omitempty"`
	Result        *Result `json:"result,omitempty"`
	Error         string  `json:"error,omitempty"`
	TestStartGate string  `json:"test_start_gate,omitempty"`
}

type controlWriter struct {
	mu     sync.Mutex
	file   *os.File
	closed atomic.Bool
}

func newControlWriter(file *os.File) *controlWriter {
	return &controlWriter{file: file}
}

func (w *controlWriter) send(message helperMessage) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed.Load() {
		return os.ErrClosed
	}
	return json.NewEncoder(w.file).Encode(message)
}

func (w *controlWriter) close() error {
	// File.Close is safe concurrently with writes and interrupts a blocked write.
	if w.closed.Swap(true) {
		return nil
	}
	file := w.file
	return file.Close()
}

// Start starts config.Command directly and returns its live standard I/O pipes.
// The context owns the returned child: cancellation closes it and kills its
// process group. Each POSIX child is supervised by a small re-executed copy of
// the current Go binary so owner death remains observable after SIGKILL.
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
	controlRead, controlWriteFile, err := os.Pipe()
	if err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite)
		return nil, err
	}
	statusRead, statusWrite, err := os.Pipe()
	if err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite, controlRead, controlWriteFile)
		return nil, err
	}
	control := newControlWriter(controlWriteFile)

	executable, err := os.Executable()
	if err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite, controlRead, statusRead, statusWrite)
		_ = control.close()
		return nil, fmt.Errorf("locate process supervisor executable: %w", err)
	}
	helperCmd := exec.Command(executable, supervisorArg)
	helperCmd.Stdin = stdinRead
	helperCmd.Stdout = stdoutWrite
	helperCmd.Stderr = stderrWrite
	helperCmd.ExtraFiles = []*os.File{controlRead, statusWrite}
	helperScope, err := processscope.New(helperCmd)
	if err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite, controlRead, statusRead, statusWrite)
		_ = control.close()
		return nil, err
	}
	if err := helperScope.Start(); err != nil {
		closeFiles(stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite, controlRead, statusRead, statusWrite)
		_ = control.close()
		_ = helperScope.Close()
		return nil, fmt.Errorf("start process supervisor: %w", err)
	}
	closeFiles(stdinRead, stdoutWrite, stderrWrite, controlRead, statusWrite)

	stopStartupCancel := context.AfterFunc(ctx, func() { _ = control.close() })
	if err := control.send(helperMessage{Magic: supervisorMagic, Config: &config, TestStartGate: config.testStartGatePath}); err != nil {
		stopStartupCancel()
		_ = control.close()
		closeFiles(stdinWrite, stdoutRead, stderrRead)
		_, waitErr := readAndWaitForSupervisor(statusRead, helperCmd, helperScope)
		return nil, errors.Join(fmt.Errorf("send process start request: %w", err), waitErr)
	}

	decoder := json.NewDecoder(statusRead)
	var started helperMessage
	if err := decoder.Decode(&started); err != nil {
		stopStartupCancel()
		_ = control.close()
		closeFiles(stdinWrite, stdoutRead, stderrRead)
		_, waitErr := readAndWaitForSupervisor(statusRead, helperCmd, helperScope)
		if ctx.Err() != nil {
			return nil, errors.Join(ctx.Err(), waitErr)
		}
		return nil, errors.Join(fmt.Errorf("process supervisor exited before target startup: %w", err), waitErr)
	}
	if started.Type != "started" || started.PID <= 0 {
		stopStartupCancel()
		_ = control.close()
		closeFiles(stdinWrite, stdoutRead, stderrRead)
		waitErr := waitSupervisor(helperCmd, helperScope, statusRead)
		if ctx.Err() != nil {
			return nil, errors.Join(ctx.Err(), waitErr)
		}
		startErr := errors.New(started.Error)
		if started.Type != "start_error" || started.Error == "" {
			startErr = fmt.Errorf("invalid process supervisor startup response %q", started.Type)
		}
		return nil, errors.Join(startErr, waitErr)
	}
	identity, err := processscope.CaptureProcessIdentity(started.PID)
	if err != nil {
		stopStartupCancel()
		_ = control.send(helperMessage{Type: "signal", Signal: "SIGKILL"})
		_ = control.close()
		closeFiles(stdinWrite, stdoutRead, stderrRead)
		waitErr := waitSupervisor(helperCmd, helperScope, statusRead)
		if ctx.Err() != nil {
			return nil, errors.Join(ctx.Err(), waitErr)
		}
		return nil, errors.Join(fmt.Errorf("capture target process identity: %w", err), waitErr)
	}
	if err := control.send(helperMessage{Type: "ack"}); err != nil {
		stopStartupCancel()
		killErr := identity.KillGroup("SIGKILL")
		identityErr := identity.Close()
		_ = control.close()
		closeFiles(stdinWrite, stdoutRead, stderrRead)
		waitErr := waitSupervisor(helperCmd, helperScope, statusRead)
		if ctx.Err() != nil {
			return nil, errors.Join(ctx.Err(), killErr, identityErr, waitErr)
		}
		return nil, errors.Join(fmt.Errorf("acknowledge process startup: %w", err), killErr, identityErr, waitErr)
	}

	c := newChild(started.PID, stdinWrite, stdoutRead, stderrRead)
	c.supervisorPID = helperCmd.Process.Pid
	c.cleanupGroupFn = func() error { return identity.KillGroup("SIGKILL") }
	c.closeIdentityFn = identity.Close
	c.killFn = func(name string) error {
		if err := processscope.ValidateSignal(name); err != nil {
			return err
		}
		return control.send(helperMessage{Type: "signal", Signal: name})
	}
	c.closeFn = func() error {
		var closeErr error
		closeErr = errors.Join(closeErr, control.close())
		closeErr = errors.Join(closeErr, closeFilesErr(stdinWrite, stdoutRead, stderrRead))
		_, waitErr := c.Wait()
		return errors.Join(closeErr, waitErr)
	}
	go monitorSupervisor(c, decoder, helperCmd, helperScope, statusRead)
	stopStartupCancel()
	if ctx.Err() != nil {
		_ = control.close()
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

func monitorSupervisor(c *Child, decoder *json.Decoder, helperCmd *exec.Cmd, helperScope processscope.Scope, statusRead *os.File) {
	var message helperMessage
	protocolErr := decoder.Decode(&message)
	if protocolErr == nil && message.Type != "exited" {
		protocolErr = fmt.Errorf("invalid process supervisor completion response %q", message.Type)
	}
	var result Result
	if protocolErr == nil && message.Result != nil {
		result = *message.Result
	}
	if protocolErr == nil && message.Result == nil {
		protocolErr = errors.New("process supervisor result is missing")
	}
	if protocolErr == nil && (result.Code == nil) == (result.Signal == "") {
		protocolErr = errors.New("process supervisor returned an invalid target result")
	}
	if protocolErr != nil {
		if !errors.Is(protocolErr, io.EOF) {
			protocolErr = fmt.Errorf("read process supervisor result: %w", protocolErr)
		} else {
			protocolErr = errors.New("process supervisor closed without a target result")
		}
		// A missing or malformed completion means the supervisor may have died
		// before it could stop the target group. The owner is outside that group,
		// so it must perform the fallback cleanup itself.
		if c.cleanupGroupFn != nil {
			protocolErr = errors.Join(protocolErr, c.cleanupGroupFn())
		}
	}
	operationErr := error(nil)
	if message.Error != "" {
		operationErr = errors.New(message.Error)
	}
	cmdWaitErr := helperCmd.Wait()
	if cmdWaitErr != nil && protocolErr == nil {
		// The supervisor sent a completion frame but did not exit normally.
		// Treat that as a supervision failure as well.
		if c.cleanupGroupFn != nil {
			cmdWaitErr = errors.Join(cmdWaitErr, c.cleanupGroupFn())
		}
	}
	scopeErr := helperScope.Close()
	_ = statusRead.Close()
	var identityErr error
	if c.closeIdentityFn != nil {
		identityErr = c.closeIdentityFn()
	}
	c.complete(result, errors.Join(protocolErr, operationErr, cmdWaitErr, scopeErr, identityErr))
}

func readAndWaitForSupervisor(statusRead *os.File, helperCmd *exec.Cmd, helperScope processscope.Scope) (helperMessage, error) {
	var message helperMessage
	decodeErr := json.NewDecoder(statusRead).Decode(&message)
	if decodeErr != nil && !errors.Is(decodeErr, io.EOF) {
		decodeErr = fmt.Errorf("read process supervisor startup response: %w", decodeErr)
	}
	waitErr := waitSupervisor(helperCmd, helperScope, statusRead)
	if errors.Is(decodeErr, io.EOF) {
		decodeErr = errors.New("process supervisor exited without a startup response")
	}
	return message, errors.Join(decodeErr, waitErr)
}

func waitSupervisor(helperCmd *exec.Cmd, helperScope processscope.Scope, statusRead *os.File) error {
	waitErr := helperCmd.Wait()
	scopeErr := helperScope.Close()
	closeErr := statusRead.Close()
	if errors.Is(closeErr, os.ErrClosed) {
		closeErr = nil
	}
	return errors.Join(waitErr, scopeErr, closeErr)
}

func runSupervisor(config Config, testStartGatePath string, decoder *json.Decoder, statusFile *os.File) int {
	status := json.NewEncoder(statusFile)
	controlEvents := make(chan helperControlEvent, 1)
	go readControlMessages(decoder, controlEvents)
	if event, ownerGone := pollOwnerEvent(controlEvents); ownerGone && event.err != nil {
		_ = status.Encode(helperMessage{Type: "start_error", Error: "process owner closed during startup"})
		return 0
	}

	cmd := exec.Command(config.Command, config.Args...)
	cmd.Dir = config.Dir
	cmd.Env = config.Env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	scope, err := processscope.New(cmd)
	if err != nil {
		_ = status.Encode(helperMessage{Type: "start_error", Error: err.Error()})
		return 0
	}
	if err := scope.Start(); err != nil {
		_ = status.Encode(helperMessage{Type: "start_error", Error: err.Error()})
		if cmd.Process != nil {
			_ = scope.Kill()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = scope.Close()
		return 0
	}
	pid := cmd.Process.Pid
	_ = os.Stdin.Close()
	_ = os.Stdout.Close()
	_ = os.Stderr.Close()
	startupOwnerGone := false
	if testStartGatePath != "" {
		gate := []byte(fmt.Sprintf("%d %d\n", os.Getpid(), pid))
		if err := os.WriteFile(testStartGatePath, gate, 0o600); err != nil {
			finishSupervisorStartupFailure(cmd, scope, status, err)
			return 0
		}
		// Package tests cancel the owner only after observing this marker. That
		// makes the otherwise narrow target-start/start-reply race deterministic.
		event := <-controlEvents
		if event.err != nil {
			startupOwnerGone = true
		} else if event.message.Type == "signal" {
			_ = scope.Signal(event.message.Signal)
		}
	} else if event, ownerGone := pollOwnerEvent(controlEvents); ownerGone {
		if event.err != nil {
			startupOwnerGone = true
		} else if event.message.Type == "signal" {
			_ = scope.Signal(event.message.Signal)
		}
	}
	if startupOwnerGone {
		finishSupervisorStartupFailure(cmd, scope, status, errors.New("process owner closed during startup"))
		return 0
	}
	_ = status.Encode(helperMessage{Type: "started", PID: pid})
	// Do not reap the target until the owner has captured its identity. This
	// closes the race where a fast target exits before Start can open a pidfd.
	for controlEvents != nil {
		event := <-controlEvents
		if event.err != nil {
			_ = scope.Kill()
			controlEvents = nil
			break
		}
		if event.message.Type == "ack" {
			break
		}
		if event.message.Type == "signal" {
			_ = scope.Signal(event.message.Signal)
			continue
		}
		_ = scope.Kill()
		controlEvents = nil
		break
	}

	targetExit := make(chan error, 1)
	go func() { targetExit <- processscope.WaitExit(cmd.Process) }()

	var waitIDErr error
	for {
		select {
		case event := <-controlEvents:
			if event.err != nil {
				// EOF is the owner-death signal. A malformed or incomplete
				// control message has the same safe cleanup behavior.
				_ = scope.Kill()
				controlEvents = nil
				continue
			}
			if event.message.Type == "signal" {
				_ = scope.Signal(event.message.Signal)
			}
		case waitIDErr = <-targetExit:
			// Waitid used WNOWAIT, so the group leader's PID remains reserved
			// while descendants are killed, avoiding a stale-PGID race.
			_ = scope.Kill()
			goto reaped
		}
	}

reaped:
	waitErr := cmd.Wait()
	result, resultErr := processResult(cmd.ProcessState, waitErr)
	if waitIDErr != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("wait for target process: %w", waitIDErr))
	}
	_ = scope.Close()
	_ = status.Encode(helperMessage{Type: "exited", Result: &result, Error: errorString(resultErr)})
	return 0
}

func finishSupervisorStartupFailure(cmd *exec.Cmd, scope processscope.Scope, status *json.Encoder, startErr error) {
	_ = scope.Kill()
	waitIDErr := processscope.WaitExit(cmd.Process)
	waitErr := cmd.Wait()
	_, resultErr := processResult(cmd.ProcessState, waitErr)
	_ = scope.Close()
	_ = status.Encode(helperMessage{Type: "start_error", Error: errors.Join(startErr, waitIDErr, resultErr).Error()})
}

func pollOwnerEvent(events <-chan helperControlEvent) (helperControlEvent, bool) {
	select {
	case event := <-events:
		return event, true
	default:
		return helperControlEvent{}, false
	}
}

type helperControlEvent struct {
	message helperMessage
	err     error
}

func readControlMessages(decoder *json.Decoder, events chan<- helperControlEvent) {
	for {
		var message helperMessage
		if err := decoder.Decode(&message); err != nil {
			events <- helperControlEvent{err: err}
			return
		}
		events <- helperControlEvent{message: message}
	}
}

func processResult(state *os.ProcessState, waitErr error) (Result, error) {
	if state == nil {
		return Result{}, errors.Join(errors.New("target process has no exit state"), waitErr)
	}
	if waitStatus, ok := state.Sys().(syscall.WaitStatus); ok && waitStatus.Signaled() {
		return Result{Signal: signalName(waitStatus.Signal())}, nil
	}
	code := state.ExitCode()
	if code < 0 {
		return Result{}, errors.Join(errors.New("target process exit code is unavailable"), waitErr)
	}
	return Result{Code: &code}, nil
}

func signalName(signal syscall.Signal) string {
	return unix.SignalName(signal)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
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
	if len(os.Args) != 2 || os.Args[1] != supervisorArg {
		return
	}
	control := os.NewFile(uintptr(3), "noe-process-control")
	status := os.NewFile(uintptr(4), "noe-process-status")
	if control == nil || status == nil {
		os.Exit(125)
	}
	syscall.CloseOnExec(3)
	syscall.CloseOnExec(4)
	decoder := json.NewDecoder(control)
	var hello helperMessage
	if err := decoder.Decode(&hello); err != nil || hello.Magic != supervisorMagic || hello.Config == nil {
		_ = control.Close()
		_ = status.Close()
		os.Exit(125)
	}
	exitCode := runSupervisor(*hello.Config, hello.TestStartGate, decoder, status)
	_ = control.Close()
	_ = status.Close()
	os.Exit(exitCode)
}
