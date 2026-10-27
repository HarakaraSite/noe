package noeprofile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectedInstructionSourceFixesCopyAndKeepsEmpty(t *testing.T) {
	cwd, config := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	writeInstruction(t, filepath.Join(config, "noe", "instructions", "default.md"), "DEFAULT")
	writeInstruction(t, filepath.Join(cwd, "a", "one.md"), "A")
	writeInstruction(t, filepath.Join(cwd, "b", "one.md"), "B")
	dirs := []string{"b", "missing", "a", "b"}
	source := SelectedInstructionSource(cwd, dirs)
	dirs[0] = "a"
	t.Chdir(t.TempDir())
	text, err := source(context.Background())
	if err != nil || text != "B\n\nA\n\nB" {
		t.Fatalf("text=%q err=%v", text, err)
	}
	writeInstruction(t, filepath.Join(cwd, "a", "one.md"), "UPDATED")
	text, err = source(context.Background())
	if err != nil || text != "B\n\nUPDATED\n\nB" {
		t.Fatalf("updated=%q err=%v", text, err)
	}
	empty := SelectedInstructionSource(cwd, []string{})
	text, err = empty(context.Background())
	if err != nil || text != "" {
		t.Fatalf("empty=%q err=%v", text, err)
	}
}

func TestSelectedInstructionSourceErrorsRemainLazy(t *testing.T) {
	cwd := t.TempDir()
	bad := filepath.Join(cwd, "bad")
	if err := os.WriteFile(bad, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	source := SelectedInstructionSource(cwd, []string{"bad"})
	if _, err := source(context.Background()); err == nil || !strings.Contains(err.Error(), bad) {
		t.Fatalf("error=%v", err)
	}
}
