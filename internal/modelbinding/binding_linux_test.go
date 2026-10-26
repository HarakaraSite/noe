package modelbinding

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestImageReadRejectsFIFOWithoutWriter(t *testing.T) {
	const childRoot = "NOE_IMAGE_FIFO_CHILD_ROOT"
	if root := os.Getenv(childRoot); root != "" {
		assertNonRegularImageRead(t, root, "/image.png")
		return
	}
	root := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(root, "image.png"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A broken image reader can block in os.ReadFile even after cancellation.
	// Isolate it so a regression fails instead of hanging the parent suite.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestImageReadRejectsFIFOWithoutWriter$", "-test.timeout=4s")
	cmd.Env = append(os.Environ(), childRoot+"="+root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("FIFO image read did not reject without a writer: %v\n%s", err, output)
	}
}
