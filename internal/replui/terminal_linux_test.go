package replui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCloseRestoresTerminalWhileInputIsBlocked(t *testing.T) {
	masterFD, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	master := os.NewFile(uintptr(masterFD), "replui-master")
	t.Cleanup(func() { master.Close() })
	if err := unix.IoctlSetPointerInt(masterFD, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	number, err := unix.IoctlGetInt(masterFD, unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	slave, err := os.OpenFile(filepath.Join("/dev/pts", fmt.Sprint(number)), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slave.Close() })
	initial, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	input := New(slave, slave)
	if input == nil {
		t.Fatal("borrowed terminal descriptors were not accepted")
	}
	t.Cleanup(func() { input.Close() })
	done := make(chan error, 1)
	go func() {
		_, err := input.ReadLine("> ")
		done <- err
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		state, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
		if err != nil {
			t.Fatal(err)
		}
		if state.Lflag&(unix.ICANON|unix.ECHO) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("input did not enter raw mode")
		}
		time.Sleep(time.Millisecond)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	state, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil || *state != *initial {
		t.Fatalf("terminal was not restored: state=%+v err=%v", state, err)
	}
	// Close restores modes but leaves the caller-owned descriptor open. A line
	// ending releases its pending read after canonical mode has been restored.
	if _, err := master.Write([]byte{'\n'}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("terminal read did not finish")
	}
	if _, err := input.ReadLine("> "); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("closed input reentered raw mode: %v", err)
	}
}
