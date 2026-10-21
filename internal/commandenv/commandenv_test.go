package commandenv

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestPrepareUsesSelectedCwdEnvAndRelativePATH(t *testing.T) {
	root := t.TempDir()
	work := filepath.Join(root, "work")
	defaultBin := filepath.Join(root, "default-bin")
	overrideBin := filepath.Join(root, "override-bin")
	for _, dir := range []string{work, defaultBin, overrideBin} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	defaultExecutable := writeExecutable(t, defaultBin, "probe")
	overrideExecutable := writeExecutable(t, overrideBin, "probe")

	allowCalls := 0
	resolveCalls := 0
	config := Config{
		Cwd: filepath.Join(root, "unused"),
		Env: map[string]string{"PATH": filepath.Join("..", "default-bin"), "MARK": "default"},
		Allow: func(command string, args []string) error {
			allowCalls++
			if allowCalls == 1 && (command != "probe" || !slices.Equal(args, []string{"one"})) {
				t.Fatalf("Allow received %q %q", command, args)
			}
			return nil
		},
		ResolveDirectory: func(cwd string) (string, error) {
			resolveCalls++
			if cwd != "/virtual" {
				return "", errors.New("unexpected cwd override")
			}
			return work, nil
		},
	}
	prepared, err := Prepare(config, "probe", []string{"one"}, "/virtual", nil)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Command != defaultExecutable || prepared.Dir != work || prepared.Env["MARK"] != "default" {
		t.Fatalf("default command environment = %#v", prepared)
	}
	if allowCalls != 1 || resolveCalls != 1 {
		t.Fatalf("Allow/ResolveDirectory calls = %d/%d", allowCalls, resolveCalls)
	}

	prepared, err = Prepare(config, "probe", nil, "/virtual", map[string]string{
		"PATH": filepath.Join("..", "override-bin"), "MARK": "override",
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Command != overrideExecutable || prepared.Env["MARK"] != "override" {
		t.Fatalf("overridden command environment = %#v", prepared)
	}

	empty, err := Prepare(config, defaultExecutable, nil, "/virtual", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Env) != 0 || empty.Command != defaultExecutable {
		t.Fatalf("explicit empty env was not preserved: %#v", empty)
	}
	prepared.Env["MARK"] = "mutated"
	if config.Env["MARK"] != "default" {
		t.Fatalf("prepared command shares selected env map: %#v", config.Env)
	}
}

func TestPrepareDeniesBeforeDirectoryResolutionAndIgnoresHostPATH(t *testing.T) {
	resolved := false
	denied := Config{
		Allow: func(command string, args []string) error {
			if command != "sh" || len(args) != 0 {
				t.Fatalf("Allow received %q %q", command, args)
			}
			return errors.New("refused")
		},
		ResolveDirectory: func(string) (string, error) {
			resolved = true
			return "", nil
		},
	}
	if _, err := Prepare(denied, "sh", nil, "/virtual", nil); err == nil || err.Error() != "refused" {
		t.Fatalf("denied Prepare error = %v", err)
	}
	if resolved {
		t.Fatal("directory resolution ran before Allow")
	}

	command := "sh"
	if runtime.GOOS == "windows" {
		command = "cmd.exe"
	}
	config := Config{Env: map[string]string{}, Allow: func(string, []string) error { return nil }}
	if _, err := Prepare(config, command, nil, "", nil); err == nil || !strings.Contains(err.Error(), "file not found") {
		t.Fatalf("command was resolved through host PATH: %v", err)
	}
}

func TestShellUsesSelectedEnvironmentAndOriginalPolicyRequest(t *testing.T) {
	root := t.TempDir()
	selectedShell := filepath.Join(root, "selected-shell.exe")
	if err := os.WriteFile(selectedShell, []byte("shell placeholder"), 0o700); err != nil {
		t.Fatal(err)
	}
	var allowed string
	var allowedArgs []string
	env := map[string]string{"ComSpec": selectedShell, "PATHEXT": ".EXE", "MARK": "default"}
	config := Config{
		Cwd: root,
		Env: env,
		Allow: func(command string, args []string) error {
			allowed = command
			allowedArgs = slices.Clone(args)
			return nil
		},
	}
	prepared, err := Shell(config, "echo requested", "", map[string]string{"ComSpec": selectedShell, "MARK": "override"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		wantArgs := []string{"/d", "/s", "/c", "echo requested"}
		if allowed != selectedShell || !slices.Equal(allowedArgs, wantArgs) {
			t.Fatalf("Windows shell policy received %q %q, want %q %q", allowed, allowedArgs, selectedShell, wantArgs)
		}
		if prepared.Command != selectedShell || !slices.Equal(prepared.Args, wantArgs) {
			t.Fatalf("selected Windows shell = %#v", prepared)
		}
	} else {
		wantArgs := []string{"-c", "echo requested"}
		if allowed != "/bin/sh" || !slices.Equal(allowedArgs, wantArgs) {
			t.Fatalf("POSIX shell policy received %q %q, want /bin/sh %q", allowed, allowedArgs, wantArgs)
		}
		if prepared.Command != "/bin/sh" || !slices.Equal(prepared.Args, wantArgs) {
			t.Fatalf("POSIX shell = %#v", prepared)
		}
	}
	if prepared.Env["MARK"] != "override" || env["MARK"] != "default" {
		t.Fatalf("shell environment override = %#v, source %#v", prepared.Env, env)
	}
}

func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
