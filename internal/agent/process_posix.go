//go:build !windows

package agent

import (
	"os/exec"
	"syscall"
)

type posixProcessScope struct {
	cmd *exec.Cmd
	pid int
}

func newProcessScope(cmd *exec.Cmd) (processScope, error) {
	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	attr.Setpgid = true
	cmd.SysProcAttr = attr
	return &posixProcessScope{cmd: cmd}, nil
}

func (s *posixProcessScope) start() error {
	if err := s.cmd.Start(); err != nil {
		return err
	}
	s.pid = s.cmd.Process.Pid
	return nil
}

func (s *posixProcessScope) terminate() error {
	if s.pid <= 0 {
		return nil
	}
	err := syscall.Kill(-s.pid, syscall.SIGKILL)
	if err == syscall.ESRCH {
		return nil
	}
	return err
}

func (s *posixProcessScope) close() error { return nil }

func sameEnvironmentKey(got, want string) bool { return got == want }
