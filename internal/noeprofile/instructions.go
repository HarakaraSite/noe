// Package noeprofile supplies application-owned NOE defaults and adapters.
package noeprofile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// InstructionSource fixes NOE's directory locations at first use. It rereads
// their contents for each invocation; the agent instance owns its body snapshot.
func InstructionSource(cwd string) func(context.Context) (string, error) {
	var once sync.Once
	var directories []string
	var locationErr error
	return func(ctx context.Context) (string, error) {
		if ctx == nil {
			return "", errors.New("NOE instructions: context is required")
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		once.Do(func() {
			configHome := os.Getenv("XDG_CONFIG_HOME")
			if configHome == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					locationErr = err
					return
				}
				configHome = filepath.Join(home, ".config")
			}
			base := cwd
			if base == "" {
				var err error
				base, err = os.Getwd()
				if err != nil {
					locationErr = err
					return
				}
			}
			base, locationErr = filepath.Abs(base)
			if locationErr != nil {
				return
			}
			configHome, locationErr = filepath.Abs(configHome)
			if locationErr != nil {
				return
			}
			directories = []string{filepath.Join(configHome, "noe", "instructions"), filepath.Join(base, ".noe", "instructions")}
		})
		if locationErr != nil {
			return "", fmt.Errorf("resolve NOE instruction locations: %w", locationErr)
		}
		var contents []string
		for _, directory := range directories {
			entries, err := readInstructionDirectory(ctx, directory)
			if err != nil {
				return "", err
			}
			contents = append(contents, entries...)
		}
		return strings.Join(contents, "\n\n"), nil
	}
}
func readInstructionDirectory(ctx context.Context, directory string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read instructions directory %q: %w", directory, err)
	}
	var contents []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := filepath.Join(directory, entry.Name())
		info, err := os.Stat(name)
		if err != nil {
			return nil, fmt.Errorf("inspect instruction file %q: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(name)
		if err != nil {
			return nil, fmt.Errorf("open instruction file %q: %w", name, err)
		}
		var content bytes.Buffer
		_, err = io.Copy(&content, instructionReader{ctx: ctx, reader: file})
		closeErr := file.Close()
		if err != nil {
			return nil, fmt.Errorf("read instruction file %q: %w", name, err)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if content.Len() > 0 {
			contents = append(contents, content.String())
		}
	}
	return contents, nil
}

type instructionReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r instructionReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > 32*1024 {
		p = p[:32*1024]
	}
	return r.reader.Read(p)
}
