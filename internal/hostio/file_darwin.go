//go:build darwin

package hostio

import (
	"bytes"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

func newUnixFile(fd int, mode uint32, access int) (*unixFile, error) {
	kind := mode & unix.S_IFMT
	if kind == unix.S_IFCHR {
		nonblockFD, err := reopenDarwinCharacterNonblock(fd, access)
		if err != nil {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("%w: reopen Darwin character device for cancellable I/O: %v", ErrUnsupported, err)
		}
		_ = unix.Close(fd)
		return &unixFile{fd: nonblockFD, nonblock: true}, nil
	}
	return &unixFile{fd: fd, fifo: kind == unix.S_IFIFO, socket: kind == unix.S_IFSOCK}, nil
}

// Reopen the path associated with this character-device descriptor. /dev/fd
// paths are dup aliases on Darwin, so they would share O_NONBLOCK with the
// borrowed file description. Verify the reopened device identity before using
// it so a stale F_GETPATH result cannot redirect host I/O.
func reopenDarwinCharacterNonblock(fd, access int) (int, error) {
	path, err := darwinFDPath(fd)
	if err != nil {
		return -1, err
	}
	original, err := darwinFDStat(fd)
	if err != nil {
		return -1, err
	}
	opened, err := unix.Open(path, access|unix.O_CLOEXEC|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return -1, err
	}
	reopened, statErr := darwinFDStat(opened)
	if statErr != nil || !sameDarwinDevice(original, reopened) {
		_ = unix.Close(opened)
		if statErr != nil {
			return -1, statErr
		}
		return -1, fmt.Errorf("reopened path %q does not identify the borrowed character device", path)
	}
	return opened, nil
}

func darwinFDPath(fd int) (string, error) {
	var path [4096]byte
	_, _, errno := unix.Syscall(unix.SYS_FCNTL, uintptr(fd), uintptr(unix.F_GETPATH), uintptr(unsafe.Pointer(&path[0])))
	if errno != 0 {
		return "", errno
	}
	end := bytes.IndexByte(path[:], 0)
	if end < 0 {
		return "", fmt.Errorf("F_GETPATH returned an unterminated path")
	}
	if end == 0 {
		return "", fmt.Errorf("F_GETPATH returned an empty path")
	}
	return string(path[:end]), nil
}

func darwinFDStat(fd int) (unix.Stat_t, error) {
	var stat unix.Stat_t
	err := unix.Fstat(fd, &stat)
	return stat, err
}

func sameDarwinDevice(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Rdev == b.Rdev && (a.Mode&unix.S_IFMT) == (b.Mode&unix.S_IFMT)
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
