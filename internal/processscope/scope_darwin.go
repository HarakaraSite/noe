//go:build darwin

package processscope

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

type darwinScope struct {
	cmd *exec.Cmd
	pid int
}

func newScope(cmd *exec.Cmd) (*darwinScope, error) {
	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	} else {
		copy := *attr
		attr = &copy
	}
	attr.Setpgid = true
	attr.Pgid = 0
	cmd.SysProcAttr = attr
	return &darwinScope{cmd: cmd}, nil
}

func (s *darwinScope) Start() error {
	if err := s.cmd.Start(); err != nil {
		return err
	}
	s.pid = s.cmd.Process.Pid
	return nil
}

func (s *darwinScope) PID() int { return s.pid }

func (s *darwinScope) Signal(name string) error { return killGroup(s.pid, name) }

func (s *darwinScope) Kill() error { return s.Signal("SIGKILL") }

func (s *darwinScope) Close() error { return nil }

func killGroup(pid int, name string) error {
	if pid <= 0 {
		return nil
	}
	sig, err := signalNumber(name)
	if err != nil {
		return err
	}
	err = unix.Kill(-pid, sig)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func validateSignal(name string) error {
	_, err := signalNumber(name)
	return err
}

func signalNumber(name string) (unix.Signal, error) {
	sig := unix.SignalNum(name)
	if sig == 0 {
		return 0, errors.New("unsupported signal: " + name)
	}
	return unix.Signal(sig), nil
}

func waitExit(process *os.Process) error {
	if process == nil || process.Pid <= 0 {
		return errors.New("invalid process")
	}
	kq, err := unix.Kqueue()
	if err != nil {
		return err
	}
	defer unix.Close(kq)
	changes := []unix.Kevent_t{{}}
	unix.SetKevent(&changes[0], process.Pid, unix.EVFILT_PROC, unix.EV_ADD|unix.EV_ENABLE|unix.EV_ONESHOT)
	changes[0].Fflags = unix.NOTE_EXIT
	events := make([]unix.Kevent_t, 1)
	for {
		n, err := unix.Kevent(kq, changes, events, nil)
		changes = nil
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n != 0 {
			return nil
		}
	}
}
