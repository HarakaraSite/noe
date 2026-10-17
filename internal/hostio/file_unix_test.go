//go:build darwin || linux

package hostio

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestBorrowedFileReaderCancellationLeavesOriginalUsable(t *testing.T) {
	original, producer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	defer producer.Close()

	reader, err := NewReader(original)
	if err != nil {
		t.Fatal(err)
	}
	flagsBefore := fileFlags(t, original)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, readErr := reader.ReadContext(ctx, make([]byte, 1))
		result <- readErr
	}()
	assertWaiting(t, result, "read")
	cancel()
	if err := awaitOperation(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read error = %v, want context.Canceled", err)
	}
	if flagsAfter := fileFlags(t, original); flagsAfter != flagsBefore {
		t.Fatalf("source file flags changed: before %#x, after %#x", flagsBefore, flagsAfter)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := producer.Write([]byte("usable")); err != nil {
		t.Fatalf("write after adapter close: %v", err)
	}
	got := make([]byte, len("usable"))
	if _, err := io.ReadFull(original, got); err != nil || string(got) != "usable" {
		t.Fatalf("borrowed source after cancellation/close = %q, %v", got, err)
	}
}

func TestDuplicatedHostFileDescriptorIsCloseOnExec(t *testing.T) {
	original, producer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer original.Close()
	defer producer.Close()

	fd, _, err := duplicateFile(original)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if err != nil {
		t.Fatal(err)
	}
	if flags&unix.FD_CLOEXEC == 0 {
		t.Fatalf("duplicated host descriptor flags=%#x, want FD_CLOEXEC", flags)
	}
}

func TestBorrowedFileWriterCancellationLeavesOriginalUsable(t *testing.T) {
	consumer, original, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	defer original.Close()
	buffered := fillPipe(t, original)

	writer, err := NewWriter(original)
	if err != nil {
		t.Fatal(err)
	}
	flagsBefore := fileFlags(t, original)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, writeErr := writer.WriteContext(ctx, []byte("x"))
		result <- writeErr
	}()
	assertWaiting(t, result, "write")
	cancel()
	if err := awaitOperation(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled write error = %v, want context.Canceled", err)
	}
	if flagsAfter := fileFlags(t, original); flagsAfter != flagsBefore {
		t.Fatalf("source file flags changed: before %#x, after %#x", flagsBefore, flagsAfter)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(io.Discard, consumer, buffered); err != nil {
		t.Fatalf("drain borrowed pipe: %v", err)
	}
	if _, err := original.Write([]byte("y")); err != nil {
		t.Fatalf("write after adapter close: %v", err)
	}
	if !readUntilByte(t, consumer, 'y') {
		t.Fatal("borrowed destination did not deliver a post-cancellation write")
	}
}

func fileFlags(t *testing.T, file *os.File) int {
	t.Helper()
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	flags := -1
	if err := raw.Control(func(fd uintptr) {
		flags, err = unix.FcntlInt(fd, unix.F_GETFL, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	return flags
}

func fillPipe(t *testing.T, writer *os.File) int64 {
	t.Helper()
	raw, err := writer.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var writeErr error
	var total int64
	if err := raw.Control(func(fd uintptr) {
		flags, getErr := unix.FcntlInt(fd, unix.F_GETFL, 0)
		if getErr != nil {
			writeErr = getErr
			return
		}
		if setErr := unix.SetNonblock(int(fd), true); setErr != nil {
			writeErr = setErr
			return
		}
		defer func() {
			if _, restoreErr := unix.FcntlInt(fd, unix.F_SETFL, flags); restoreErr != nil && writeErr == nil {
				writeErr = restoreErr
			}
		}()
		block := make([]byte, 4096)
		for {
			n, err := unix.Write(int(fd), block)
			total += int64(n)
			writeErr = err
			if writeErr == unix.EINTR {
				continue
			}
			if writeErr == unix.EAGAIN || writeErr == unix.EWOULDBLOCK {
				oneByte := []byte{0}
				for {
					n, err := unix.Write(int(fd), oneByte)
					total += int64(n)
					if err == unix.EINTR {
						continue
					}
					if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
						writeErr = nil
						return
					}
					if err != nil {
						writeErr = err
						return
					}
				}
			}
			if writeErr != nil {
				return
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	return total
}

func assertWaiting(t *testing.T, result <-chan error, operation string) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("blocked %s returned before cancellation: %v", operation, err)
	case <-time.After(80 * time.Millisecond):
	}
}

func awaitOperation(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("canceled host I/O did not return")
		return nil
	}
}

func readUntilByte(t *testing.T, file *os.File, marker byte) bool {
	t.Helper()
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	fd := -1
	if err := raw.Control(func(handle uintptr) { fd = int(handle) }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		if _, err := waitReady(ctx, fd, unix.POLLIN); err != nil {
			return false
		}
		buffer := make([]byte, 4096)
		n, err := unix.Read(fd, buffer)
		if err == unix.EINTR || err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			continue
		}
		if err != nil || n == 0 {
			return false
		}
		for _, b := range buffer[:n] {
			if b == marker {
				return true
			}
		}
	}
}
