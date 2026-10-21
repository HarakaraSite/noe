// Package commandenv resolves command execution from one explicit environment.
package commandenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"
)

// Config is the host-selected environment used to prepare a command.
type Config struct {
	// Cwd is the physical default working directory. An empty value preserves
	// the caller's OS working directory for legacy configurations.
	Cwd string
	Env map[string]string

	Timeout time.Duration

	// Allow sees the requested command and arguments before path resolution.
	// A nil function refuses execution.
	Allow func(command string, args []string) error

	ResolveDirectory func(string) (string, error)

	// Supervisor selects an explicit POSIX helper executable. Empty starts
	// commands directly. Windows uses a Job Object instead of a helper.
	Supervisor string
}

// Prepared is the resolved command shared by Node child_process and
// noe:process. Env is a copy and is never inherited from the host process.
type Prepared struct {
	Command string
	Args    []string
	Dir     string
	Env     map[string]string
}

var errNotAllowed = errors.New("running programs is not allowed")

// Prepare authorizes the request, selects cwd and env overrides, and resolves
// the executable using only the selected environment's PATH/PATHEXT.
// A nil envOverride inherits Config.Env; a non-nil empty map means no env.
func Prepare(config Config, command string, args []string, cwdOverride string, envOverride map[string]string) (Prepared, error) {
	return prepare(config, command, args, cwdOverride, envOverride)
}

// Shell prepares the OS shell for a command string. Policy receives the
// selected shell command and arguments before its executable is resolved.
func Shell(config Config, command, cwdOverride string, envOverride map[string]string) (Prepared, error) {
	env := chooseEnv(config.Env, envOverride)
	shell := "/bin/sh"
	args := []string{"-c", command}
	if runtime.GOOS == "windows" {
		shell, _ = envValue(env, "ComSpec")
		if shell == "" {
			shell = "cmd.exe"
		}
		args = []string{"/d", "/s", "/c", command}
	}
	config.Env = env
	return prepare(config, shell, args, cwdOverride, nil)
}

func prepare(config Config, command string, args []string, cwdOverride string, envOverride map[string]string) (Prepared, error) {
	requestedArgs := slices.Clone(args)
	if config.Allow == nil {
		return Prepared{}, errNotAllowed
	}
	if err := config.Allow(command, requestedArgs); err != nil {
		return Prepared{}, err
	}
	if command == "" {
		return Prepared{}, errors.New("exec: no command")
	}
	if strings.IndexByte(command, 0) >= 0 {
		return Prepared{}, errors.New("exec: command contains NUL")
	}

	env := chooseEnv(config.Env, envOverride)
	dir, err := workingDirectory(config, cwdOverride)
	if err != nil {
		return Prepared{}, err
	}
	path, err := resolveExecutable(command, dir, env)
	if err != nil {
		return Prepared{}, err
	}
	return Prepared{Command: path, Args: requestedArgs, Dir: dir, Env: env}, nil
}

func chooseEnv(defaultEnv, override map[string]string) map[string]string {
	source := defaultEnv
	if override != nil {
		source = override
	}
	return normalizeEnv(source)
}

func normalizeEnv(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if runtime.GOOS != "windows" {
		for _, key := range keys {
			result[key] = source[key]
		}
		return result
	}
	for _, key := range keys {
		for existing := range result {
			if strings.EqualFold(existing, key) {
				delete(result, existing)
			}
		}
		result[key] = source[key]
	}
	return result
}

func workingDirectory(config Config, override string) (string, error) {
	dir := config.Cwd
	if override != "" {
		if config.ResolveDirectory != nil {
			resolved, err := config.ResolveDirectory(override)
			if err != nil {
				return "", err
			}
			dir = resolved
		} else if filepath.IsAbs(override) || windowsRootedWithoutVolume(override) {
			resolved, err := absolutePath(override, config.Cwd, nil)
			if err != nil {
				return "", err
			}
			dir = resolved
		} else if config.Cwd != "" {
			dir = filepath.Join(config.Cwd, override)
		} else {
			dir = override
		}
	}
	if dir == "" || filepath.IsAbs(dir) {
		if dir == "" {
			return "", nil
		}
		return filepath.Clean(dir), nil
	}
	base, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Clean(filepath.Join(base, dir)), nil
}

func resolveExecutable(command, dir string, env map[string]string) (string, error) {
	windows := runtime.GOOS == "windows"
	if strings.IndexByte(command, 0) >= 0 {
		return "", fmt.Errorf("exec: %q: command contains NUL", command)
	}
	if isPathLike(command, windows) {
		path, err := absolutePath(command, dir, env)
		if err != nil {
			return "", err
		}
		if windows {
			if found := findWindowsExecutable(path, env); found != "" {
				return found, nil
			}
			return "", fmt.Errorf("exec: %q: executable file not found", command)
		}
		return path, nil
	}
	pathValue, ok := envValue(env, "PATH")
	if !ok {
		return "", notFound(command, windows)
	}
	for _, entry := range filepath.SplitList(pathValue) {
		if entry == "" {
			if windows {
				continue
			}
			entry = "."
		}
		candidate, err := absolutePath(filepath.Join(entry, command), dir, env)
		if err != nil {
			return "", err
		}
		if windows {
			if found := findWindowsExecutable(candidate, env); found != "" {
				return found, nil
			}
		} else if executable(candidate, false) {
			return candidate, nil
		}
	}
	return "", notFound(command, windows)
}

func isPathLike(command string, windows bool) bool {
	if windows {
		return strings.ContainsAny(command, `:\/`)
	}
	return strings.Contains(command, string(filepath.Separator))
}

func absolutePath(path, dir string, env map[string]string) (string, error) {
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	base := dir
	if base == "" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(path)
		if volume != "" {
			relative := strings.TrimLeft(path[len(volume):], `\/`)
			if strings.EqualFold(volume, filepath.VolumeName(base)) {
				return filepath.Clean(filepath.Join(base, relative)), nil
			}
			// Windows keeps each drive's current directory in a pseudo-variable
			// such as "=C:". Use the selected environment rather than the host's
			// process environment when resolving drive-relative paths.
			driveCwd, ok := envValue(env, "="+volume)
			if ok && filepath.IsAbs(driveCwd) {
				return filepath.Clean(filepath.Join(driveCwd, relative)), nil
			}
			return filepath.Clean(filepath.Join(volume+string(filepath.Separator), relative)), nil
		}
		if windowsRootedWithoutVolume(path) {
			baseVolume := filepath.VolumeName(base)
			if baseVolume == "" {
				// A relative configured cwd has no drive of its own. Resolve it
				// against the current directory selected for this execution.
				cwd, err := os.Getwd()
				if err != nil {
					return "", err
				}
				baseVolume = filepath.VolumeName(filepath.Join(cwd, base))
			}
			if baseVolume == "" {
				return "", fmt.Errorf("exec: cannot resolve rooted path %q without a cwd volume", path)
			}
			relative := strings.TrimLeft(path, `\/`)
			return filepath.Clean(filepath.Join(baseVolume+string(filepath.Separator), relative)), nil
		}
	}
	return filepath.Clean(filepath.Join(base, path)), nil
}

func windowsRootedWithoutVolume(path string) bool {
	if runtime.GOOS != "windows" || path == "" || filepath.VolumeName(path) != "" || !isPathSeparator(path[0]) {
		return false
	}
	// A double separator starts a UNC/device path, not a path rooted on the
	// selected drive. Those paths carry their own volume when well formed.
	return len(path) == 1 || !isPathSeparator(path[1])
}

func isPathSeparator(char byte) bool {
	return char == '/' || char == '\\'
}

func findWindowsExecutable(path string, env map[string]string) string {
	if executable(path, true) && filepath.Ext(path) != "" {
		return path
	}
	for _, ext := range pathExtensions(env) {
		candidate := path + ext
		if executable(candidate, true) {
			return candidate
		}
	}
	return ""
}

func executable(path string, windows bool) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return windows || info.Mode()&0o111 != 0
}

func pathExtensions(env map[string]string) []string {
	value, ok := envValue(env, "PATHEXT")
	if !ok || value == "" {
		return []string{".com", ".exe", ".bat", ".cmd"}
	}
	var result []string
	for _, ext := range strings.Split(value, ";") {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if ext[0] != '.' {
			ext = "." + ext
		}
		result = append(result, strings.ToLower(ext))
	}
	return result
}

func envValue(env map[string]string, name string) (string, bool) {
	if runtime.GOOS != "windows" {
		value, ok := env[name]
		return value, ok
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if strings.EqualFold(key, name) {
			return env[key], true
		}
	}
	return "", false
}

func notFound(command string, windows bool) error {
	pathName := "$PATH"
	if windows {
		pathName = "%PATH%"
	}
	return fmt.Errorf("exec: %q: executable file not found in %s", command, pathName)
}

// EnvList returns a stable environment block for os/exec and process.Start.
func EnvList(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if runtime.GOOS == "windows" {
		seen := make(map[string]int, len(keys))
		result := make([]string, 0, len(keys))
		for _, key := range keys {
			folded := strings.ToUpper(key)
			entry := key + "=" + env[key]
			if index, ok := seen[folded]; ok {
				result[index] = entry
			} else {
				seen[folded] = len(result)
				result = append(result, entry)
			}
		}
		return result
	}
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+env[key])
	}
	return result
}
