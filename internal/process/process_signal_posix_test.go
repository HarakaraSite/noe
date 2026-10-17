//go:build !windows

package process

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

func TestProcessSignalHelper(t *testing.T) {
	if os.Getenv("NOE_PROCESS_TEST_SIGNAL_HELPER") != "1" {
		return
	}
	signal.Reset(syscall.SIGTERM)
	_, _ = io.WriteString(os.Stdout, "ready\n")
	for {
		// Keep a timer live so the Go test runtime does not diagnose deadlock.
		time.Sleep(time.Hour)
	}
}

func TestKillReportsPOSIXSignal(t *testing.T) {
	executable := testExecutable(t)
	child, err := Start(context.Background(), Config{
		Command: executable,
		Args:    []string{"-test.run=^TestProcessSignalHelper$"},
		Env:     []string{"NOE_PROCESS_TEST_SIGNAL_HELPER=1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := bufio.NewReader(child.Stdout).ReadString('\n'); err != nil || ready != "ready\n" {
		t.Fatalf("signal target ready = %q, %v", ready, err)
	}
	if err := child.Kill("SIGTERM"); err != nil {
		t.Fatal(err)
	}
	result, err := child.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != nil || result.Signal != "SIGTERM" {
		t.Fatalf("signal result = %#v, want SIGTERM", result)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
}
