//go:build linux

package processscope

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func captureProcessIdentity(pid int) (*ProcessIdentity, error) {
	if pid <= 0 {
		return nil, errors.New("invalid process PID")
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err == nil {
		return &ProcessIdentity{
			close: func() error { return unix.Close(fd) },
			kill: func(signal string) error {
				exited, err := linuxPidfdExited(fd)
				if err != nil {
					return err
				}
				if exited {
					return nil
				}
				return killGroup(pid, signal)
			},
		}, nil
	}
	// Older Linux kernels may not support pidfds, or a container seccomp
	// profile may reject the syscall. /proc starttime still detects PID reuse
	// for the fallback cleanup path.
	startTime, statErr := linuxProcessStartTime(pid)
	if statErr != nil {
		return nil, errors.Join(fmt.Errorf("open target pidfd: %w", err), statErr)
	}
	return &ProcessIdentity{
		kill: func(signal string) error {
			current, err := linuxProcessStartTime(pid)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if current != startTime {
				return nil
			}
			return killGroup(pid, signal)
		},
	}, nil
}

func linuxPidfdExited(fd int) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		_, err := unix.Poll(fds, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return false, err
		}
		if fds[0].Revents&unix.POLLNVAL != 0 {
			return false, errors.New("target pidfd is no longer valid")
		}
		return fds[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0, nil
	}
}

func linuxProcessStartTime(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	// comm is parenthesized and may contain spaces; fields after its final ')'
	// begin with state (field 3). starttime is field 22.
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(data[end+1:]))
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return "", fmt.Errorf("missing start time in /proc/%d/stat", pid)
	}
	if _, err := strconv.ParseUint(fields[startTimeIndex], 10, 64); err != nil {
		return "", fmt.Errorf("parse start time in /proc/%d/stat: %w", pid, err)
	}
	return fields[startTimeIndex], nil
}
