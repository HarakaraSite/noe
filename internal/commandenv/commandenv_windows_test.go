package commandenv

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
)

func TestWindowsPathLikeCommandsUseOnlySelectedPATHEXT(t *testing.T) {
	root := t.TempDir()
	config := Config{
		Cwd:   root,
		Env:   map[string]string{"PATHEXT": ".CMD"},
		Allow: func(string, []string) error { return nil },
	}

	explicit := filepath.Join(root, "explicit.BAT")
	writeWindowsPlaceholder(t, explicit)
	prepared, err := Prepare(config, explicit, nil, "", nil)
	if err != nil || prepared.Command != filepath.Clean(explicit) {
		t.Fatalf("existing explicit extension outside selected PATHEXT = %q, %v", prepared.Command, err)
	}

	selectedBase := filepath.Join(root, "selected")
	selected := selectedBase + ".CMD"
	writeWindowsPlaceholder(t, selected)
	prepared, err = Prepare(config, selectedBase, nil, "", nil)
	if err != nil || prepared.Command != filepath.Clean(selected) {
		t.Fatalf("selected PATHEXT completion = %q, %v; want %q", prepared.Command, err, selected)
	}

	hostOnlyBase := filepath.Join(root, "host-only")
	writeWindowsPlaceholder(t, hostOnlyBase+".EXE")
	if _, err := Prepare(config, hostOnlyBase, nil, "", nil); err == nil {
		t.Fatal("resolver accepted an extension found only through the host PATHEXT")
	}
}

func TestWindowsRootedPathsUseSelectedCwdVolumeAndProfileResolver(t *testing.T) {
	cwd := t.TempDir()
	volume := filepath.VolumeName(cwd)
	if volume == "" {
		t.Fatalf("temporary cwd has no Windows volume: %q", cwd)
	}
	volumeRoot := volume + string(filepath.Separator)
	want := filepath.Clean(filepath.Join(volumeRoot, "tools", "probe.exe"))

	commandPath, err := absolutePath(`\tools\probe.exe`, cwd, nil)
	if err != nil || commandPath != want {
		t.Fatalf("command rooted path = %q, %v; want %q", commandPath, err, want)
	}
	dir, err := workingDirectory(Config{Cwd: cwd}, `\tools`)
	if err != nil || dir != filepath.Clean(filepath.Join(volumeRoot, "tools")) {
		t.Fatalf("cwd rooted path = %q, %v", dir, err)
	}

	backend, err := filesystem.New(filesystem.Config{Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	got, err := backend.Resolve(`\tools\probe.exe`, true)
	if err != nil || got != want {
		t.Fatalf("rootless backend rooted path = %q, %v; want %q", got, err, want)
	}

	uncCwd := `\\server\share\work`
	uncBackend, err := filesystem.New(filesystem.Config{Cwd: uncCwd})
	if err != nil {
		t.Fatal(err)
	}
	uncVolumeRoot := filepath.VolumeName(uncCwd) + string(filepath.Separator)
	uncWant := filepath.Clean(filepath.Join(uncVolumeRoot, "tools", "probe.exe"))
	got, err = uncBackend.Resolve(`\tools\probe.exe`, true)
	if err != nil || got != uncWant {
		t.Fatalf("UNC backend rooted path = %q, %v; want %q", got, err, uncWant)
	}

	executable := filepath.Join(cwd, "existing.exe")
	writeWindowsPlaceholder(t, executable)
	prepared, err := Prepare(Config{
		Cwd:              backend.PhysicalCwd(),
		Env:              map[string]string{},
		Allow:            func(string, []string) error { return nil },
		ResolveDirectory: func(path string) (string, error) { return backend.Resolve(path, true) },
	}, executable, nil, `\tools`, nil)
	if err != nil || prepared.Dir != filepath.Clean(filepath.Join(volumeRoot, "tools")) {
		t.Fatalf("Profile cwd override = %q, %v", prepared.Dir, err)
	}

	uncCommand, err := absolutePath(`\tools\probe.exe`, uncCwd, nil)
	if err != nil || uncCommand != uncWant {
		t.Fatalf("UNC command rooted path = %q, %v; want %q", uncCommand, err, uncWant)
	}
}

func writeWindowsPlaceholder(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsEnvironmentCollisionsSelectSortedLastKey(t *testing.T) {
	root := t.TempDir()
	losingBin := filepath.Join(root, "losing")
	winningBin := filepath.Join(root, "winning")
	for _, dir := range []string{losingBin, winningBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeWindowsPlaceholder(t, filepath.Join(losingBin, "probe.exe"))
	want := filepath.Join(winningBin, "probe.cmd")
	writeWindowsPlaceholder(t, want)
	losingShell := filepath.Join(root, "losing-shell.exe")
	winningShell := filepath.Join(root, "winning-shell.exe")
	writeWindowsPlaceholder(t, losingShell)
	writeWindowsPlaceholder(t, winningShell)

	source := map[string]string{
		"PATH": losingBin, "Path": losingBin, "path": winningBin,
		"PATHEXT": ".EXE", "PathExt": ".EXE", "pathext": ".CMD",
		"COMSPEC": losingShell, "ComSpec": losingShell, "comspec": winningShell,
	}
	config := Config{Cwd: root, Env: source, Allow: func(string, []string) error { return nil }}
	prepared, err := Prepare(config, "probe", nil, "", nil)
	if err != nil || prepared.Command != want {
		t.Fatalf("selected PATH/PATHEXT command = %q, %v; want %q", prepared.Command, err, want)
	}
	wantList := []string{"comspec=" + winningShell, "path=" + winningBin, "pathext=.CMD"}
	if got := EnvList(prepared.Env); !slices.Equal(got, wantList) {
		t.Fatalf("normalized environment block = %q, want %q", got, wantList)
	}
	// EnvList also handles collisions when passed a raw, non-normalized map.
	if got := EnvList(source); !slices.Equal(got, wantList) {
		t.Fatalf("raw environment block = %q, want %q", got, wantList)
	}
	var policyCommand string
	var policyArgs []string
	config.Allow = func(command string, args []string) error {
		policyCommand, policyArgs = command, slices.Clone(args)
		return nil
	}
	shell, err := Shell(config, "echo case", "", nil)
	if err != nil || shell.Command != winningShell || policyCommand != winningShell ||
		!slices.Equal(policyArgs, []string{"/d", "/s", "/c", "echo case"}) {
		t.Fatalf("selected ComSpec shell = %#v, policy %q %q, error %v", shell, policyCommand, policyArgs, err)
	}
	source["path"] = "changed"
	shell.Env["comspec"] = "changed"
	if prepared.Env["path"] != winningBin || source["comspec"] != winningShell {
		t.Fatal("prepared environments share the source map")
	}
}
