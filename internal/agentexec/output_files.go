package agentexec

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

// OutputDirectory fixes the host directory and its runtime-visible path.
type OutputDirectory struct {
	root, directory, visible string
}

func NewOutputDirectory(path string, profile stdlib.Profile) (*OutputDirectory, error) {
	if path == "" {
		return nil, nil
	}
	if profile.Modules.FS == nil {
		return nil, errors.New("agent: ToolOutputDir requires Runtime.Modules.FS")
	}
	backend, err := filesystem.New(filesystem.Config{Root: profile.Modules.FS.Root, Cwd: profile.Environment.Cwd})
	if err != nil {
		return nil, err
	}
	full, err := backend.Resolve(path, true)
	if err != nil {
		return nil, fmt.Errorf("agent: resolve ToolOutputDir: %w", err)
	}
	dir := &OutputDirectory{root: full, directory: ".", visible: full}
	if backend.Root() != "" {
		dir.root = backend.Root()
		dir.directory, err = filepath.Rel(dir.root, full)
		if err != nil {
			return nil, fmt.Errorf("agent: resolve ToolOutputDir relative to filesystem root: %w", err)
		}
		dir.visible = "/" + filepath.ToSlash(dir.directory)
	}
	opened, err := dir.open()
	if err != nil {
		return nil, fmt.Errorf("agent: open ToolOutputDir: %w", err)
	}
	if err := opened.Close(); err != nil {
		return nil, fmt.Errorf("agent: close ToolOutputDir: %w", err)
	}
	return dir, nil
}

func (d *OutputDirectory) open() (*os.Root, error) {
	root, err := os.OpenRoot(d.root)
	if err != nil || d.directory == "." {
		return root, err
	}
	defer root.Close()
	return root.OpenRoot(d.directory)
}

// outputSpill owns a file only after the stream first exceeds its preview.
// The directory handle keeps cleanup tied to the directory actually opened.
type outputSpill struct {
	directory *OutputDirectory
	stream    string
	root      *os.Root
	file      *os.File
	name      string
	err       error
}

func (s *outputSpill) write(c *capturedOutput, incoming []byte) {
	if s.err != nil || int64(len(incoming)) <= int64(c.limit())-c.total {
		return
	}
	if s.file == nil {
		s.root, s.err = s.directory.open()
		if s.err != nil {
			return
		}
		s.name = "run-js-" + s.stream + "-" + rand.Text() + ".log"
		s.file, s.err = s.root.OpenFile(s.name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if s.err != nil {
			return
		}
		// No earlier bytes have been dropped. The tail has not wrapped yet.
		s.writeBytes(c.head)
		s.writeBytes(c.tail)
	}
	s.writeBytes(incoming)
}

func (s *outputSpill) writeBytes(p []byte) {
	if s.err == nil && len(p) != 0 {
		_, s.err = s.file.Write(p)
	}
}

func (s *outputSpill) finish(total int64) (*executionresult.OutputFile, error) {
	if s.root != nil {
		defer s.root.Close()
	}
	if s.file == nil {
		return nil, s.err
	}
	err := errors.Join(s.err, s.file.Close())
	if err != nil {
		return nil, errors.Join(err, s.root.Remove(s.name))
	}
	return &executionresult.OutputFile{
		Path: filepath.Join(s.directory.visible, s.name), Bytes: total,
	}, nil
}
