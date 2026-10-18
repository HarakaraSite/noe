package imageapi

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"
)

func TestReadImageRejectsFIFOWithoutOpeningIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "image.fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadFile(context.Background(), path, "auto")
	if err == nil || AsError(err).Code != "input" {
		t.Fatalf("FIFO error=%v", err)
	}
}
