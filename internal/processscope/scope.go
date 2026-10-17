// Package processscope provides the small OS-specific process ownership
// primitives used by NOE's generic process backend.
package processscope

import (
	"os"
	"os/exec"
	"sync"
)

// Scope owns a process and the descendants associated with it by the host OS.
// On POSIX systems this is a process group; on Windows it is a Job Object.
type Scope interface {
	Start() error
	PID() int
	Signal(name string) error
	Kill() error
	Close() error
}

// New prepares cmd to start in a fresh owned process scope.
func New(cmd *exec.Cmd) (Scope, error) { return newScope(cmd) }

// ValidateSignal reports whether signal is supported by the current platform.
func ValidateSignal(signal string) error { return validateSignal(signal) }

// KillGroup sends a signal to a POSIX process group. Windows process scopes
// use their Job Object handle instead.
func KillGroup(pid int, signal string) error { return killGroup(pid, signal) }

// WaitExit waits until process exits while leaving it unreaped. This lets the
// caller clean up the rest of its process group before the PID can be reused.
func WaitExit(process *os.Process) error { return waitExit(process) }

// ProcessIdentity pins and rechecks the identity of a process whose process
// group may need emergency cleanup if its supervisor exits unexpectedly.
// CaptureProcessIdentity is available on supported POSIX platforms.
type ProcessIdentity struct {
	close     func() error
	closeOnce sync.Once
	closeErr  error
	kill      func(string) error
}

// CaptureProcessIdentity retains the target identity for later group cleanup.
func CaptureProcessIdentity(pid int) (*ProcessIdentity, error) {
	return captureProcessIdentity(pid)
}

// KillGroup sends a signal to this identity's process group if the identity
// captured at spawn still matches the PID.
func (i *ProcessIdentity) KillGroup(signal string) error {
	if i == nil || i.kill == nil {
		return nil
	}
	return i.kill(signal)
}

// Close releases any OS handle retained to prevent PID reuse.
func (i *ProcessIdentity) Close() error {
	if i == nil {
		return nil
	}
	i.closeOnce.Do(func() {
		if i.close != nil {
			i.closeErr = i.close()
		}
	})
	return i.closeErr
}
