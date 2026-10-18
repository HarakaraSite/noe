package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// loadUserInstructions reads the common and workspace instruction directories
// for one run. The returned contents are a snapshot; callers should not reload
// them during that run.
func loadUserInstructions(cwd string) (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve common instructions directory: %w", err)
		}
		configHome = filepath.Join(home, ".config")
	}
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve workspace instructions directory: %w", err)
		}
	}

	directories := []string{
		filepath.Join(configHome, "noe", "instructions"),
		filepath.Join(cwd, ".noe", "instructions"),
	}
	var contents []string
	for _, directory := range directories {
		files, err := readInstructionDirectory(directory)
		if err != nil {
			return "", err
		}
		contents = append(contents, files...)
	}
	return strings.Join(contents, "\n\n"), nil
}

func readInstructionDirectory(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read instructions directory %q: %w", directory, err)
	}
	var contents []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := os.Stat(path) // Follow symlinks; only regular files are instructions.
		if err != nil {
			return nil, fmt.Errorf("inspect instruction file %q: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read instruction file %q: %w", path, err)
		}
		if len(data) != 0 {
			contents = append(contents, string(data))
		}
	}
	return contents, nil
}
