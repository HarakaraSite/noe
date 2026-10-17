//go:build windows

package hostio

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

func TestBorrowedFileReaderCancellationBeforeReadFileStarts(t *testing.T) {
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
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	openGate := func() { releaseOnce.Do(func() { close(release) }) }
	beforeFileIOCall = func() {
		close(started)
		<-release
	}
	defer func() {
		openGate()
		beforeFileIOCall = nil
		_ = reader.Close()
	}()

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, readErr := reader.ReadContext(ctx, make([]byte, 1))
		result <- readErr
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("read did not reach pre-syscall gate")
	}
	cancel()
	openGate()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled read error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		// Unblock a lost-cancellation regression before failing so the test
		// cannot leave a goroutine using the borrowed file and read buffer.
		_, _ = producer.Write([]byte("x"))
		<-result
		t.Fatal("cancellation before ReadFile started was lost")
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
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, readErr := reader.ReadContext(ctx, make([]byte, 1))
		result <- readErr
	}()
	assertWindowsWaiting(t, result, "read")
	cancel()
	if err := awaitWindowsOperation(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read error = %v, want context.Canceled", err)
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

func TestBorrowedFileWriterCancellationLeavesOriginalUsable(t *testing.T) {
	consumer, original, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	defer original.Close()

	const fillSize = 8 << 20
	fillerDone := make(chan error, 1)
	go func() {
		_, writeErr := original.Write(make([]byte, fillSize))
		fillerDone <- writeErr
	}()
	assertWindowsWaiting(t, fillerDone, "pipe-filling write")

	writer, err := NewWriter(original)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, writeErr := writer.WriteContext(ctx, []byte("x"))
		result <- writeErr
	}()
	assertWindowsWaiting(t, result, "write")
	cancel()
	if err := awaitWindowsOperation(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled write error = %v, want context.Canceled", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	drained := make(chan int64, 1)
	go func() {
		n, _ := io.Copy(io.Discard, consumer)
		drained <- n
	}()
	if err := awaitWindowsOperation(t, fillerDone); err != nil {
		t.Fatalf("original filler write: %v", err)
	}
	if _, err := original.Write([]byte("y")); err != nil {
		t.Fatalf("write after adapter close: %v", err)
	}
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	if got := <-drained; got != fillSize+1 {
		t.Fatalf("borrowed destination delivered %d bytes, want %d", got, fillSize+1)
	}
}

func assertWindowsWaiting(t *testing.T, result <-chan error, operation string) {
	t.Helper()
	select {
	case err := <-result:
		t.Fatalf("blocked %s returned before cancellation: %v", operation, err)
	case <-time.After(100 * time.Millisecond):
	}
}

func awaitWindowsOperation(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("canceled host I/O did not return")
		return nil
	}
}
