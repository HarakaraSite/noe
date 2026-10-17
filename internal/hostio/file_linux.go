//go:build linux

package hostio

import (
	"fmt"
	"strconv"

	"golang.org/x/sys/unix"
)

func newUnixFile(fd int, mode uint32, access int) (*unixFile, error) {
	kind := mode & unix.S_IFMT
	if kind == unix.S_IFIFO {
		if nonblockFD, err := reopenLinuxNonblock(fd, access); err == nil {
			_ = unix.Close(fd)
			return &unixFile{fd: nonblockFD, fifo: true, nonblock: true}, nil
		}
		// Anonymous pipes may not be reopenable through procfs on every kernel.
		// Keep the bounded-poll fallback for those descriptors.
		return &unixFile{fd: fd, fifo: true}, nil
	}
	if kind == unix.S_IFCHR {
		nonblockFD, err := reopenLinuxNonblock(fd, access|unix.O_NOCTTY)
		if err != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("%w: reopen Linux character device for cancellable I/O: %v", ErrUnsupported, err)
		}
		_ = unix.Close(fd)
		return &unixFile{fd: nonblockFD, nonblock: true}, nil
	}
	if kind == unix.S_IFSOCK {
		return &unixFile{fd: fd, socket: true}, nil
	}
	return &unixFile{fd: fd}, nil
}

// Opening procfs's descriptor link creates an independent open file
// description, so O_NONBLOCK applies to this adapter instead of the borrowed
// host descriptor.
func reopenLinuxNonblock(fd, access int) (int, error) {
	path := "/proc/self/fd/" + strconv.Itoa(fd)
	return unix.Open(path, access|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
}

func readUnixFile(file *unixFile, p []byte) (int, error) {
	if file.socket {
		n, _, err := unix.Recvfrom(file.fd, p, unix.MSG_DONTWAIT)
		return n, err
	}
	return unix.Read(file.fd, p)
}

func writeUnixFile(file *unixFile, p []byte) (int, error) {
	if file.socket {
		return unix.SendmsgN(file.fd, p, nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
	}
	return unix.Write(file.fd, p)
}
