package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUserInstructionsOrdersCommonBeforeWorkspace(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, "xdg")
	workspace := filepath.Join(root, "workspace")
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("HOME", filepath.Join(root, "home"))

	common := filepath.Join(configHome, "noe", "instructions")
	writeInstructionTestFile(t, filepath.Join(common, "z.md"), "common z")
	writeInstructionTestFile(t, filepath.Join(common, "a.md"), "common a")
	writeInstructionTestFile(t, filepath.Join(common, "shared.md"), "common shared")
	writeInstructionTestFile(t, filepath.Join(common, "ignored.txt"), "ignored")
	writeInstructionTestFile(t, filepath.Join(common, "nested", "nested.md"), "nested ignored")
	writeInstructionTestFile(t, filepath.Join(common, "directory.md", "inside.md"), "directory ignored")
	writeInstructionTestFile(t, filepath.Join(workspace, ".noe", "instructions", "b.md"), "workspace b")
	writeInstructionTestFile(t, filepath.Join(workspace, ".noe", "instructions", "shared.md"), "workspace shared")

	got, err := loadUserInstructions(workspace)
	if err != nil {
		t.Fatalf("loadUserInstructions() error = %v", err)
	}
	want := strings.Join([]string{"common a", "common shared", "common z", "workspace b", "workspace shared"}, "\n\n")
	if got != want {
		t.Fatalf("loadUserInstructions() = %q, want %q", got, want)
	}
}

func TestLoadUserInstructionsUsesXDGConfigHome(t *testing.T) {
	root := t.TempDir()
	xdg := filepath.Join(root, "xdg")
	workspace := filepath.Join(root, "workspace")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", filepath.Join(root, "home"))
	writeInstructionTestFile(t, filepath.Join(xdg, "noe", "instructions", "shared.md"), "xdg")
	writeInstructionTestFile(t, filepath.Join(root, "home", ".config", "noe", "instructions", "shared.md"), "home")

	got, err := loadUserInstructions(workspace)
	if err != nil {
		t.Fatalf("loadUserInstructions() error = %v", err)
	}
	if got != "xdg" {
		t.Fatalf("loadUserInstructions() = %q, want XDG contents", got)
	}
}

func TestLoadUserInstructionsFallsBackToHomeConfig(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeInstructionTestFile(t, filepath.Join(home, ".config", "noe", "instructions", "common.md"), "home config")

	got, err := loadUserInstructions(filepath.Join(root, "workspace"))
	if err != nil {
		t.Fatalf("loadUserInstructions() error = %v", err)
	}
	if got != "home config" {
		t.Fatalf("loadUserInstructions() = %q, want home config contents", got)
	}
}

func TestLoadUserInstructionsMissingDirectoriesAreEmpty(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "missing-config"))
	t.Setenv("HOME", filepath.Join(root, "missing-home"))

	got, err := loadUserInstructions(filepath.Join(root, "workspace"))
	if err != nil {
		t.Fatalf("loadUserInstructions() error = %v", err)
	}
	if got != "" {
		t.Fatalf("loadUserInstructions() = %q, want empty contents", got)
	}
}

func TestLoadUserInstructionsFollowsRegularFileSymlinks(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "config", "noe", "instructions")
	workspace := filepath.Join(root, "workspace")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	writeInstructionTestFile(t, filepath.Join(root, "target.txt"), "linked contents")
	writeInstructionTestFile(t, filepath.Join(root, "target-directory", "inside.md"), "directory target")
	if err := os.MkdirAll(common, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "target.txt"), filepath.Join(common, "file-link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "target-directory"), filepath.Join(common, "directory-link.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	writeInstructionTestFile(t, filepath.Join(common, "directory.md", "inside.md"), "direct directory")

	got, err := loadUserInstructions(workspace)
	if err != nil {
		t.Fatalf("loadUserInstructions() error = %v", err)
	}
	if got != "linked contents" {
		t.Fatalf("loadUserInstructions() = %q, want linked regular-file contents only", got)
	}
}

func TestLoadUserInstructionsReportsProblemFilePath(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "config", "noe", "instructions")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	if err := os.MkdirAll(common, 0o755); err != nil {
		t.Fatal(err)
	}
	loop := filepath.Join(common, "loop.md")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := loadUserInstructions(filepath.Join(root, "workspace"))
	if err == nil {
		t.Fatal("loadUserInstructions() error = nil, want a path diagnostic")
	}
	if !strings.Contains(err.Error(), loop) {
		t.Fatalf("loadUserInstructions() error = %q, want it to contain %q", err, loop)
	}
}

func TestLoadUserInstructionsDanglingSymlinkReportsPath(t *testing.T) {
	root := t.TempDir()
	common := filepath.Join(root, "config", "noe", "instructions")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("HOME", filepath.Join(root, "home"))
	if err := os.MkdirAll(common, 0o755); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(common, "missing.md")
	if err := os.Symlink(filepath.Join(root, "missing-target.md"), dangling); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := loadUserInstructions(filepath.Join(root, "workspace"))
	if err == nil {
		t.Fatal("loadUserInstructions() error = nil, want a path diagnostic")
	}
	if !strings.Contains(err.Error(), dangling) {
		t.Fatalf("loadUserInstructions() error = %q, want it to contain %q", err, dangling)
	}
}

func writeInstructionTestFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
