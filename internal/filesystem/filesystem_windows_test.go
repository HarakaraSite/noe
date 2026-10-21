package filesystem

import (
	"path/filepath"
	"testing"
)

func TestRootlessResolveVolumeLessRootedPathUsesCwdVolume(t *testing.T) {
	cwd := t.TempDir()
	volume := filepath.VolumeName(cwd)
	if volume == "" {
		t.Fatalf("temporary cwd has no Windows volume: %q", cwd)
	}
	backend, err := New(Config{Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join(volume+string(filepath.Separator), "tools", "probe.exe"))
	got, err := backend.Resolve(`\tools\probe.exe`, true)
	if err != nil || got != want {
		t.Fatalf("rooted path = %q, %v; want %q", got, err, want)
	}

	uncCwd := `\\server\share\work`
	uncBackend, err := New(Config{Cwd: uncCwd})
	if err != nil {
		t.Fatal(err)
	}
	uncWant := filepath.Clean(filepath.Join(filepath.VolumeName(uncCwd)+string(filepath.Separator), "tools", "probe.exe"))
	got, err = uncBackend.Resolve(`\tools\probe.exe`, true)
	if err != nil || got != uncWant {
		t.Fatalf("UNC rooted path = %q, %v; want %q", got, err, uncWant)
	}
}
