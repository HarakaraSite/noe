//go:build linux

package processscope

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

type posixScope struct {
	cmd *exec.Cmd
	pid int
}

func newScope(cmd *exec.Cmd) (*posixScope, error) {
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
	return &posixScope{cmd: cmd}, nil
}

func (s *posixScope) Start() error {
	if err := s.cmd.Start(); err != nil {
		return err
	}
	s.pid = s.cmd.Process.Pid
	return nil
}

func (s *posixScope) PID() int { return s.pid }

func (s *posixScope) Signal(name string) error {
	return killGroup(s.pid, name)
}

func (s *posixScope) Kill() error { return s.Signal("SIGKILL") }

func (s *posixScope) Close() error { return nil }

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
	for {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, process.Pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err
	}
}
