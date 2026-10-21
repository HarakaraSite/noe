package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFixedHostAndVirtualWorkingDirectories(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "value"), []byte("selected"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []Config{{Cwd: sub}, {Root: root, Cwd: "/sub"}} {
		b, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		got, err := b.ReadFile(context.Background(), "value")
		if err != nil || string(got) != "selected" {
			t.Fatalf("cwd read = %q, %v", got, err)
		}
		physical, err := b.Resolve(".", true)
		if err != nil || physical != b.PhysicalCwd() {
			t.Fatalf("physical cwd = %q, %v", physical, err)
		}
		if cfg.Root != "" && b.Cwd() != "/sub" {
			t.Fatalf("virtual cwd = %q", b.Cwd())
		}
	}
}

func TestAtomicWritePreservesDestinationOnCancellationAndReadOnly(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "image")
	if err := os.WriteFile(file, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := New(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.WriteFileAtomic(ctx, "image", []byte("cancelled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled write = %v", err)
	}
	readonly, err := New(Config{Root: root, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := readonly.WriteFileAtomic(context.Background(), "image", []byte("readonly")); err == nil {
		t.Fatal("read-only write succeeded")
	}
	got, err := os.ReadFile(file)
	if err != nil || string(got) != "original" {
		t.Fatalf("failed write damaged destination: %q, %v", got, err)
	}
	if err := b.WriteFileAtomic(context.Background(), "image", []byte("committed")); err != nil {
		t.Fatal(err)
	}
	got, err = b.ReadFile(context.Background(), "image")
	if err != nil || string(got) != "committed" {
		t.Fatalf("committed bytes: %q, %v", got, err)
	}
}

func TestRootChecksLinksBeforeNonexistentTails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	b, err := New(Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"out/new/leaf", "dangling", "../outside"} {
		for _, follow := range []bool{true, false} {
			if _, err := b.Resolve(p, follow); err == nil {
				t.Errorf("root accepted %q, follow=%v", p, follow)
			}
		}
	}
	if err := os.Symlink("inside", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inside"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := b.Resolve("alias", false)
	if err != nil || entry != filepath.Join(root, "alias") {
		t.Fatalf("link entry = %q, %v", entry, err)
	}
	got, err := b.ReadFile(context.Background(), "alias")
	if err != nil || string(got) != "ok" {
		t.Fatalf("internal link read = %q, %v", got, err)
	}
}

func TestExecutionViewCancellationLeavesBorrowedBackendUsable(t *testing.T) {
	b, err := New(Config{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.WriteFileAtomic(context.Background(), "value", []byte("retained")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	v := WithContext(ctx, b)
	cancel()
	if _, err := v.ReadFile(context.Background(), "value"); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed execution read = %v", err)
	}
	if err := v.WriteFileAtomic(context.Background(), "value", []byte("changed")); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed execution write = %v", err)
	}
	got, err := b.ReadFile(context.Background(), "value")
	if err != nil || string(got) != "retained" {
		t.Fatalf("borrowed backend = %q, %v", got, err)
	}
	caller, stop := context.WithCancel(context.Background())
	stop()
	if _, err := WithContext(context.Background(), b).ReadFile(caller, "value"); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation = %v", err)
	}
}
