package hostio

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestBorrowedReaderUsesMemoryInputAndCloseKeepsSource(t *testing.T) {
	source := strings.NewReader("memory input")
	reader, err := NewReader(source)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 6)
	if n, err := reader.ReadContext(context.Background(), got); err != nil || n != len(got) || string(got) != "memory" {
		t.Fatalf("ReadContext() = %d, %q, %v", n, got, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(source); err != nil || string(got) != " input" {
		t.Fatalf("source after adapter close = %q, %v", got, err)
	}
}

func TestUnsupportedReaderDoesNotStartUncancellableRead(t *testing.T) {
	source, sink := io.Pipe()
	defer source.Close()
	defer sink.Close()
	reader, err := NewReader(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadContext(context.Background(), make([]byte, 1)); err != ErrUnsupported {
		t.Fatalf("ReadContext() error = %v, want ErrUnsupported", err)
	}
	written := make(chan error, 1)
	go func() {
		_, writeErr := sink.Write([]byte("still usable"))
		written <- writeErr
	}()
	got := make([]byte, len("still usable"))
	if _, err := io.ReadFull(source, got); err != nil || string(got) != "still usable" {
		t.Fatalf("source after unsupported adapter call = %q, %v", got, err)
	}
	if err := <-written; err != nil {
		t.Fatalf("write after unsupported adapter call: %v", err)
	}
}

func TestSharedWriterSerializesLegacyAndContextWrites(t *testing.T) {
	const writers = 16
	const blockSize = 4096
	var destination bytes.Buffer
	writer := NewSharedWriter(&destination)
	defer writer.Close()

	var group sync.WaitGroup
	for i := 0; i < writers; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			marker := byte('A' + index)
			block := bytes.Repeat([]byte{marker}, blockSize)
			var err error
			if index%2 == 0 {
				_, err = writer.Write(block)
			} else {
				_, err = writer.WriteContext(context.Background(), block)
			}
			if err != nil {
				t.Errorf("write %q: %v", marker, err)
			}
		}(i)
	}
	group.Wait()
	if got, want := destination.Len(), writers*blockSize; got != want {
		t.Fatalf("destination size = %d, want %d", got, want)
	}

	seen := make(map[byte]bool, writers)
	data := destination.Bytes()
	for offset := 0; offset < len(data); {
		marker := data[offset]
		if seen[marker] {
			t.Fatalf("write %q was split by another writer", marker)
		}
		seen[marker] = true
		end := offset
		for end < len(data) && data[end] == marker {
			end++
		}
		if got := end - offset; got != blockSize {
			t.Fatalf("contiguous write %q has %d bytes, want %d", marker, got, blockSize)
		}
		offset = end
	}
}
