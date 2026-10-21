package noeprofile

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeInstruction(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestInstructionSourceFixesLocationsAndRereadsBodies(t *testing.T) {
	workspace, config, other := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	first := filepath.Join(config, "noe", "instructions", "a.md")
	writeInstruction(t, first, "common-a")
	writeInstruction(t, filepath.Join(config, "noe", "instructions", "z.md"), "common-z")
	writeInstruction(t, filepath.Join(workspace, ".noe", "instructions", "a.md"), "workspace")
	writeInstruction(t, filepath.Join(workspace, ".no", "ignored.md"), "ignored")
	source := InstructionSource(workspace)
	text, err := source(context.Background())
	if err != nil || text != "common-a\n\ncommon-z\n\nworkspace" {
		t.Fatalf("instructions=%q err=%v", text, err)
	}
	t.Setenv("XDG_CONFIG_HOME", other)
	writeInstruction(t, first, "updated-common")
	writeInstruction(t, filepath.Join(other, "noe", "instructions", "a.md"), "other-location")
	text, err = source(context.Background())
	if err != nil || !strings.HasPrefix(text, "updated-common\n\ncommon-z") || strings.Contains(text, "other-location") {
		t.Fatalf("reread=%q err=%v", text, err)
	}
}
func TestInstructionSourceFileRulesAndCancellation(t *testing.T) {
	workspace, config := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	source := InstructionSource(workspace)
	text, err := source(context.Background())
	if err != nil || text != "" {
		t.Fatalf("missing dirs=%q err=%v", text, err)
	}
	dir := filepath.Join(workspace, ".noe", "instructions")
	writeInstruction(t, filepath.Join(dir, "a.md"), "A")
	writeInstruction(t, filepath.Join(dir, "b.txt"), "ignored")
	if err := os.Mkdir(filepath.Join(dir, "directory.md"), 0700); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(dir, "a.md"), filepath.Join(dir, "c.md")); err != nil {
			t.Fatal(err)
		}
	}
	text, err = source(context.Background())
	expected := "A"
	if runtime.GOOS != "windows" {
		expected = "A\n\nA"
	}
	if err != nil || text != expected {
		t.Fatalf("file rules=%q err=%v", text, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := source(ctx); err != context.Canceled {
		t.Fatalf("cancel=%v", err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "d.md")); err != nil {
			t.Fatal(err)
		}
		if _, err := source(context.Background()); err == nil {
			t.Fatal("dangling md symlink ignored")
		}
	}
}
