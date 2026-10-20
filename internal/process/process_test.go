package process

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	targetModeEnv = "NOE_PROCESS_TEST_MODE"
	ownerModeEnv  = "NOE_PROCESS_TEST_OWNER"
)

func TestProcessTargetHelper(t *testing.T) {
	mode := os.Getenv(targetModeEnv)
	if mode == "" {
		return
	}
	switch mode {
	case "echo":
		_, _ = fmt.Fprintf(os.Stdout, "pid=%d\n", os.Getpid())
		_, _ = io.WriteString(os.Stderr, "stderr-ready\n")
		reader := bufio.NewReader(os.Stdin)
		for {
			data, err := reader.ReadBytes('\n')
			if len(data) != 0 {
				_, _ = os.Stdout.Write(append([]byte("out:"), data...))
				_, _ = os.Stderr.Write(append([]byte("err:"), data...))
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				os.Exit(3)
			}
		}
	case "exit":
		code, _ := strconv.Atoi(os.Getenv("NOE_PROCESS_TEST_EXIT"))
		os.Exit(code)
	case "hold":
		_, _ = io.WriteString(os.Stdout, "ready\n")
		for {
			time.Sleep(time.Hour)
		}
	case "large-output":
		chunk := bytes.Repeat([]byte{'x'}, 64*1024)
		for i := 0; i < 640; i++ { // 40 MiB, larger than the existing exec capture cap.
			if _, err := os.Stdout.Write(chunk); err != nil {
				os.Exit(4)
			}
		}
		_, _ = io.WriteString(os.Stderr, "stderr-marker")
	case "fd-check":
		for fd := 3; fd < 16; fd++ {
			link, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
			if err == nil && strings.HasPrefix(link, "pipe:[") {
				_, _ = fmt.Fprintf(os.Stderr, "unexpected inherited pipe fd %d: %s\n", fd, link)
				os.Exit(9)
			}
		}
		_, _ = io.WriteString(os.Stdout, "fds-clean\n")
	case "leak":
		grandchild := exec.Command(os.Args[0], "-test.run=^TestProcessGrandchildHelper$")
		grandchild.Env = append(os.Environ(), "NOE_PROCESS_TEST_GRANDCHILD=1")
		if err := grandchild.Start(); err != nil {
			os.Exit(5)
		}
		state := fmt.Sprintf("%d %d\n", os.Getpid(), grandchild.Process.Pid)
		if err := os.WriteFile(os.Getenv("NOE_PROCESS_TEST_STATE"), []byte(state), 0o600); err != nil {
			os.Exit(6)
		}
		for {
			time.Sleep(time.Hour)
		}
	default:
		os.Exit(8)
	}
}

func TestProcessGrandchildHelper(t *testing.T) {
	if os.Getenv("NOE_PROCESS_TEST_GRANDCHILD") == "1" {
		for {
			time.Sleep(time.Hour)
		}
	}
}

func TestProcessOwnerHelper(t *testing.T) {
	if os.Getenv(ownerModeEnv) != "1" {
		return
	}
	executable, err := os.Executable()
	if err != nil {
		os.Exit(10)
	}
	env := append(os.Environ(), targetModeEnv+"=leak", "NOE_PROCESS_TEST_STATE="+os.Getenv("NOE_PROCESS_TEST_STATE"))
	child, err := Start(context.Background(), Config{
		Command: executable,
		Args:    []string{"-test.run=^TestProcessTargetHelper$"},
		Env:     env,
	})
	if err != nil {
		os.Exit(11)
	}
	state := fmt.Sprintf("%d %d %d\n", os.Getpid(), child.PID, child.supervisorPID)
	if err := os.WriteFile(os.Getenv("NOE_PROCESS_TEST_OWNER_STATE"), []byte(state), 0o600); err != nil {
		os.Exit(12)
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestStartStreamsAndTargetPID(t *testing.T) {
	executable := testExecutable(t)
	child, err := Start(context.Background(), Config{
		Command: executable,
		Args:    []string{"-test.run=^TestProcessTargetHelper$"},
		Env:     []string{targetModeEnv + "=echo"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()

	stdout := bufio.NewReader(child.Stdout)
	gotPID, err := stdout.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if want := fmt.Sprintf("pid=%d\n", child.PID); gotPID != want {
		t.Fatalf("target PID mismatch: stdout %q, Child.PID %d", gotPID, child.PID)
	}
	stderr := bufio.NewReader(child.Stderr)
	if got, err := stderr.ReadString('\n'); err != nil || got != "stderr-ready\n" {
		t.Fatalf("stderr ready = %q, %v", got, err)
	}

	input := []byte("binary-\x00-data\n")
	if _, err := child.Stdin.Write(input); err != nil {
		t.Fatal(err)
	}
	wantOut := append([]byte("out:"), input...)
	gotOut := make([]byte, len(wantOut))
	if _, err := io.ReadFull(stdout, gotOut); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotOut, wantOut) {
		t.Fatalf("stdout = %q, want %q", gotOut, wantOut)
	}
	wantErr := append([]byte("err:"), input...)
	gotErr := make([]byte, len(wantErr))
	if _, err := io.ReadFull(stderr, gotErr); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotErr, wantErr) {
		t.Fatalf("stderr = %q, want %q", gotErr, wantErr)
	}

	if err := child.Stdin.Close(); err != nil {
		t.Fatal(err)
	}
	result, err := child.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if result.Code == nil || *result.Code != 0 || result.Signal != "" {
		t.Fatalf("result = %#v, want exit code 0", result)
	}
	if again, err := child.Wait(); err != nil || again.Code == nil || *again.Code != 0 {
		t.Fatalf("second Wait = %#v, %v", again, err)
	}
	trailing, err := io.ReadAll(stdout)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(trailing, []byte("PASS")) {
		t.Fatalf("target's output after Wait was not available: %q", trailing)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStartFailureNonzeroExitAndContextCleanup(t *testing.T) {
	if _, err := Start(context.Background(), Config{Command: "/no/such/noe-process-command"}); err == nil {
		t.Fatal("Start succeeded for a missing command")
	}

	executable := testExecutable(t)
	child, err := Start(context.Background(), Config{
		Command: executable,
		Args:    []string{"-test.run=^TestProcessTargetHelper$"},
		Env:     []string{targetModeEnv + "=exit", "NOE_PROCESS_TEST_EXIT=7"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := child.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if result.Code == nil || *result.Code != 7 {
		t.Fatalf("nonzero result = %#v, want code 7", result)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	holding, err := Start(canceled, Config{
		Command: executable,
		Args:    []string{"-test.run=^TestProcessTargetHelper$"},
		Env:     []string{targetModeEnv + "=hold"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := bufio.NewReader(holding.Stdout).ReadString('\n'); err != nil || ready != "ready\n" {
		t.Fatalf("holding target ready = %q, %v", ready, err)
	}
	cancel()
	select {
	case <-holding.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("context cancellation did not stop the child")
	}
	result, err = holding.Wait()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && result.Signal != "SIGKILL" {
		t.Fatalf("context cancellation result = %#v, want SIGKILL", result)
	}
	if runtime.GOOS == "windows" && result.Code == nil {
		t.Fatalf("context cancellation result = %#v, want Windows exit code", result)
	}
	if err := holding.Close(); err != nil {
		t.Fatal(err)
	}

	preCanceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := Start(preCanceled, Config{Command: executable}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start with canceled context error = %v", err)
	}
}

func TestKillRejectsUnsupportedSignal(t *testing.T) {
	executable := testExecutable(t)
	child, err := Start(context.Background(), Config{
		Command: executable,
		Args:    []string{"-test.run=^TestProcessTargetHelper$"},
		Env:     []string{targetModeEnv + "=hold"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Kill("NO_SUCH_SIGNAL"); err == nil {
		t.Fatal("Kill accepted an unsupported signal")
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStartAcknowledgesFastExitsBeforeTargetIsReaped(t *testing.T) {
	executable := testExecutable(t)
	for i := 0; i < 25; i++ {
		child, err := Start(context.Background(), Config{
			Command: executable,
			Args:    []string{"-test.run=^TestProcessTargetHelper$"},
			Env:     []string{targetModeEnv + "=exit", "NOE_PROCESS_TEST_EXIT=0"},
		})
		if err != nil {
			t.Fatalf("Start iteration %d: %v", i, err)
		}
		result, err := child.Wait()
		if err != nil {
			t.Fatalf("Wait iteration %d: %v", i, err)
		}
		if result.Code == nil || *result.Code != 0 {
			t.Fatalf("result iteration %d = %#v, want exit code 0", i, result)
		}
		if err := child.Close(); err != nil {
			t.Fatalf("Close iteration %d: %v", i, err)
		}
	}
}

func BenchmarkSupervisorStartupCost(b *testing.B) {
	executable := testExecutable(b)
	var totalStartup time.Duration
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		child, err := Start(context.Background(), Config{
			Command: executable,
			Args:    []string{"-test.run=^TestProcessTargetHelper$"},
			Env:     []string{targetModeEnv + "=exit"},
		})
		b.StopTimer()
		totalStartup += time.Since(start)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := child.Wait(); err != nil {
			b.Fatal(err)
		}
		if err := child.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
	b.StopTimer()
	b.ReportMetric(float64(totalStartup)/float64(b.N), "start/op")
	if runtime.GOOS == "windows" {
		b.Log("Windows process startup; each active spawn owns 1 target process plus a Job Object")
	} else {
		b.Log("POSIX supervisor startup; each active spawn owns 2 extra processes (supervisor + target)")
	}
}

func TestLargeOutputStreamsWithoutCaptureLimit(t *testing.T) {
	executable := testExecutable(t)
	child, err := Start(context.Background(), Config{
		Command: executable,
		Args:    []string{"-test.run=^TestProcessTargetHelper$"},
		Env:     []string{targetModeEnv + "=large-output"},
	})
	if err != nil {
		t.Fatal(err)
	}
	count, copyErr := io.Copy(io.Discard, child.Stdout)
	result, waitErr := child.Wait()
	if copyErr != nil {
		t.Fatal(copyErr)
	}
	if waitErr != nil {
		t.Fatal(waitErr)
	}
	if count < 40*1024*1024 {
		t.Fatalf("received %d output bytes, want at least 40 MiB", count)
	}
	if result.Code == nil || *result.Code != 0 {
		t.Fatalf("large-output result = %#v", result)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCloseUnblocksBlockedStdinWrite(t *testing.T) {
	executable := testExecutable(t)
	child, err := Start(context.Background(), Config{
		Command: executable,
		Args:    []string{"-test.run=^TestProcessTargetHelper$"},
		Env:     []string{targetModeEnv + "=hold"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := bufio.NewReader(child.Stdout).ReadString('\n'); err != nil || ready != "ready\n" {
		t.Fatalf("holding target ready = %q, %v", ready, err)
	}
	writeStarted := make(chan struct{})
	writeDone := make(chan error, 1)
	go func() {
		close(writeStarted)
		_, err := child.Stdin.Write(bytes.Repeat([]byte{'z'}, 16*1024*1024))
		writeDone <- err
	}()
	<-writeStarted
	select {
	case err := <-writeDone:
		t.Fatalf("write completed although the target never reads stdin: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	closeErr := child.Close()
	select {
	case err := <-writeDone:
		if err == nil {
			t.Fatal("blocked write unexpectedly succeeded after Close")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Child.Close did not unblock a pending stdin write")
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func testExecutable(t testing.TB) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return executable
}
