//go:build darwin || linux

package hostio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

const pollInterval = 40 * time.Millisecond

type fileReader interface {
	ReadContext(context.Context, []byte) (int, error)
	Close() error
}

type fileWriter interface {
	WriteContext(context.Context, []byte) (int, error)
	Close() error
}

type unixFile struct {
	fd       int
	nonblock bool
	fifo     bool
	socket   bool
}

func newFileReader(source *os.File) (fileReader, error) {
	fd, mode, err := duplicateFile(source)
	if err != nil {
		return nil, err
	}
	return newUnixFile(fd, mode, unix.O_RDONLY)
}

func newFileWriter(destination *os.File) (fileWriter, error) {
	fd, mode, err := duplicateFile(destination)
	if err != nil {
		return nil, err
	}
	return newUnixFile(fd, mode, unix.O_WRONLY)
}

func duplicateFile(source *os.File) (int, uint32, error) {
	if source == nil {
		return -1, 0, errors.New("nil host file")
	}
	raw, err := source.SyscallConn()
	if err != nil {
		return -1, 0, err
	}
	fd := -1
	var mode uint32
	var dupErr error
	err = raw.Control(func(handle uintptr) {
		fd, dupErr = unix.FcntlInt(handle, unix.F_DUPFD_CLOEXEC, 0)
		if dupErr != nil {
			return
		}
		var stat unix.Stat_t
		if statErr := unix.Fstat(fd, &stat); statErr != nil {
			_ = unix.Close(fd)
			fd = -1
			dupErr = statErr
			return
		}
		mode = uint32(stat.Mode)
	})
	if err != nil {
		return -1, 0, err
	}
	if dupErr != nil {
		return -1, 0, dupErr
	}
	return fd, mode, nil
}

func (f *unixFile) ReadContext(ctx context.Context, p []byte) (int, error) {
	for {
		revents, err := waitReady(ctx, f.fd, unix.POLLIN)
		if err != nil {
			return 0, err
		}
		if revents&unix.POLLNVAL != 0 {
			return 0, os.ErrClosed
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		readBuffer := p
		if f.fifo && !f.nonblock && len(readBuffer) > 512 {
			// A pipe read returns currently available bytes. Keeping the request
			// bounded also limits interactions with a separately-consumed stdin.
			readBuffer = readBuffer[:512]
		}
		n, readErr := readUnixFile(f, readBuffer)
		if readErr == unix.EINTR || readErr == unix.EAGAIN || readErr == unix.EWOULDBLOCK {
			continue
		}
		if readErr != nil {
			return n, readErr
		}
		if n == 0 {
			return 0, io.EOF
		}
		return n, nil
	}
}

func (f *unixFile) WriteContext(ctx context.Context, p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	written := 0
	for written < len(p) {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		revents, err := waitReady(ctx, f.fd, unix.POLLOUT)
		if err != nil {
			return written, err
		}
		if revents&unix.POLLNVAL != 0 {
			return written, os.ErrClosed
		}
		if revents&(unix.POLLERR|unix.POLLHUP) != 0 {
			return written, unix.EPIPE
		}
		if ctx.Err() != nil {
			return written, ctx.Err()
		}
		writeBuffer := p[written:]
		if f.fifo && !f.nonblock && len(writeBuffer) > 512 {
			// Darwin has no per-duplicate O_NONBLOCK: dup shares file status
			// flags with the source. Poll first and write only a small pipe unit.
			writeBuffer = writeBuffer[:512]
		}
		n, writeErr := writeUnixFile(f, writeBuffer)
		if n > 0 {
			written += n
		} else {
			n = 0
		}
		if writeErr == unix.EINTR || writeErr == unix.EAGAIN || writeErr == unix.EWOULDBLOCK {
			continue
		}
		if writeErr != nil {
			return written, writeErr
		}
		if n == 0 {
			return written, io.ErrNoProgress
		}
		if !f.nonblock && !f.socket {
			return written, nil
		}
	}
	return written, nil
}

func (f *unixFile) Close() error {
	if f.fd < 0 {
		return nil
	}
	err := unix.Close(f.fd)
	f.fd = -1
	return err
}

func waitReady(ctx context.Context, fd int, events int16) (int16, error) {
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		pollFD := []unix.PollFd{{Fd: int32(fd), Events: events}}
		ms := int(pollInterval / time.Millisecond)
		n, err := unix.Poll(pollFD, ms)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("poll host I/O: %w", err)
		}
		if n > 0 {
			return pollFD[0].Revents, nil
		}
	}
}
