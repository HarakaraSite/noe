//go:build darwin

package processscope

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

func captureProcessIdentity(pid int) (*ProcessIdentity, error) {
	if pid <= 0 {
		return nil, errors.New("invalid process PID")
	}
	start, err := darwinProcessStartTime(pid)
	if err != nil {
		return nil, err
	}
	return &ProcessIdentity{
		kill: func(signal string) error {
			current, err := darwinProcessStartTime(pid)
			if errors.Is(err, unix.ESRCH) {
				return nil
			}
			if err != nil {
				return err
			}
			if current != start {
				return nil
			}
			return killGroup(pid, signal)
		},
	}, nil
}

func darwinProcessStartTime(pid int) (string, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	if int(info.Proc.P_pid) != pid {
		return "", fmt.Errorf("process %d identity changed during lookup", pid)
	}
	return fmt.Sprintf("%d:%d", info.Proc.P_starttime.Sec, info.Proc.P_starttime.Usec), nil
}
