//go:build linux

package hostio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestBorrowedCharacterWriterCancelsAndCompletesCanonicalPTYOutput(t *testing.T) {
	master, slave := openHostIOPTY(t)
	defer master.Close()
	defer slave.Close()

	flagsBefore := fileFlags(t, slave)
	writer, err := NewWriter(slave)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan struct {
		n   int
		err error
	}, 1)
	blocked := bytes.Repeat([]byte{'x'}, 1<<20)
	go func() {
		n, writeErr := writer.WriteContext(ctx, blocked)
		result <- struct {
			n   int
			err error
		}{n, writeErr}
	}()
	select {
	case got := <-result:
		t.Fatalf("large PTY write returned before cancellation or draining: n=%d err=%v", got.n, got.err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case got := <-result:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled PTY write error = %v, want context.Canceled", got.err)
		}
		if got.n == 0 {
			t.Fatal("blocked PTY write made no progress before cancellation")
		}
		if _, err := io.CopyN(io.Discard, master, int64(got.n)); err != nil {
			t.Fatalf("drain partial PTY output: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled PTY write did not return")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if flagsAfter := fileFlags(t, slave); flagsAfter != flagsBefore {
		t.Fatalf("borrowed PTY flags changed: before %#x, after %#x", flagsBefore, flagsAfter)
	}

	// The same adapter must be able to finish a write larger than the PTY
	// output queue when its peer drains concurrently. This uses the default
	// canonical terminal settings and bytes unaffected by output processing.
	writer, err = NewWriter(slave)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	want := bytes.Repeat([]byte{'x'}, 1<<20)
	readDone := make(chan error, 1)
	go func() {
		got := make([]byte, len(want))
		if _, err := io.ReadFull(master, got); err != nil {
			readDone <- err
			return
		}
		if !bytes.Equal(got, want) {
			readDone <- fmt.Errorf("PTY output bytes differ")
			return
		}
		readDone <- nil
	}()
	writeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	n, err := writer.WriteContext(writeCtx, want)
	if err != nil || n != len(want) {
		t.Fatalf("full PTY output wrote %d/%d bytes: %v", n, len(want), err)
	}
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-writeCtx.Done():
		t.Fatal("PTY output reader did not receive full payload")
	}
	if flagsAfter := fileFlags(t, slave); flagsAfter != flagsBefore {
		t.Fatalf("full PTY write changed borrowed flags: before %#x, after %#x", flagsBefore, flagsAfter)
	}
}

func TestBorrowedSocketWriterCancellationUsesPerCallNonblocking(t *testing.T) {
	left, right := openHostIOSocketPair(t)
	defer left.Close()
	defer right.Close()

	flagsBefore := fileFlags(t, left)
	filled := fillSocket(t, left)
	writer, err := NewWriter(left)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan struct {
		n   int
		err error
	}, 1)
	go func() {
		n, writeErr := writer.WriteContext(ctx, bytes.Repeat([]byte{'s'}, 1<<20))
		result <- struct {
			n   int
			err error
		}{n, writeErr}
	}()
	select {
	case got := <-result:
		t.Fatalf("full socket write returned before cancellation or draining: n=%d err=%v", got.n, got.err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	var wrote int
	select {
	case got := <-result:
		wrote = got.n
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled socket write error = %v, want context.Canceled", got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled socket write did not return")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if flagsAfter := fileFlags(t, left); flagsAfter != flagsBefore {
		t.Fatalf("borrowed socket flags changed: before %#x, after %#x", flagsBefore, flagsAfter)
	}
	if _, err := io.CopyN(io.Discard, right, int64(filled+wrote)); err != nil {
		t.Fatalf("drain socket after cancellation: %v", err)
	}
	if _, err := unix.SendmsgN(int(left.Fd()), []byte("usable"), nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL); err != nil {
		t.Fatalf("send through borrowed socket after adapter close: %v", err)
	}
	got := make([]byte, len("usable"))
	n, _, err := unix.Recvfrom(int(right.Fd()), got, 0)
	if err != nil || n != len(got) || string(got) != "usable" {
		t.Fatalf("receive after adapter close = %q (%d), %v", got, n, err)
	}
}

func openHostIOPTY(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.IoctlSetPointerInt(masterFD, unix.TIOCSPTLCK, 0); err != nil {
		_ = unix.Close(masterFD)
		t.Fatal(err)
	}
	index, err := unix.IoctlGetInt(masterFD, unix.TIOCGPTN)
	if err != nil {
		_ = unix.Close(masterFD)
		t.Fatal(err)
	}
	slaveFD, err := unix.Open(fmt.Sprintf("/dev/pts/%d", index), unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(masterFD)
		t.Fatal(err)
	}
	return os.NewFile(uintptr(masterFD), "pty-master"), os.NewFile(uintptr(slaveFD), "pty-slave")
}

func openHostIOSocketPair(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	return os.NewFile(uintptr(fds[0]), "socket-left"), os.NewFile(uintptr(fds[1]), "socket-right")
}

func fillSocket(t *testing.T, file *os.File) int {
	t.Helper()
	block := bytes.Repeat([]byte{'f'}, 64<<10)
	total := 0
	for {
		n, err := unix.SendmsgN(int(file.Fd()), block, nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
		total += n
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			return total
		}
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}
