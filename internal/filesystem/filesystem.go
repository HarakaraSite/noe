// Package filesystem implements the path and write policy shared by runtime
// file APIs and Go bindings. A Backend has no mutable execution state.
package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

type Config struct {
	Root     string
	Cwd      string
	ReadOnly bool
}

type Backend struct {
	root        string
	cwd         string
	physicalCwd string
	readOnly    bool
}

// New fixes relative host paths at construction. With Root, script paths and
// Cwd are virtual paths within that root; without it, Cwd is a host path.
func New(cfg Config) (*Backend, error) {
	b := &Backend{readOnly: cfg.ReadOnly}
	if cfg.Root == "" {
		cwd := cfg.Cwd
		if cwd == "" {
			cwd = "."
		}
		var err error
		b.cwd, err = filepath.Abs(cwd)
		if err != nil {
			return nil, err
		}
		b.physicalCwd = b.cwd
		return b, nil
	}
	root, err := filepath.Abs(cfg.Root)
	if err != nil {
		return nil, err
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	b.root = root
	cwd := cfg.Cwd
	if filepath.VolumeName(cwd) != "" {
		return nil, errors.New("filesystem: virtual cwd cannot have a volume")
	}
	if runtime.GOOS == "windows" {
		cwd = strings.ReplaceAll(cwd, "\\", "/")
	}
	b.cwd = path.Clean("/" + strings.TrimPrefix(cwd, "/"))
	b.physicalCwd, err = b.Resolve(".", true)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (b *Backend) Cwd() string         { return b.cwd }
func (b *Backend) PhysicalCwd() string { return b.physicalCwd }
func (b *Backend) Root() string        { return b.root }

func (b *Backend) CheckWritable() error {
	if b.readOnly {
		return errors.New("the filesystem is read-only")
	}
	return nil
}

// Resolve validates links including the final entry. followFinal=false keeps
// that last directory entry for unlink, rename and lstat, after validation.
func (b *Backend) Resolve(p string, followFinal bool) (string, error) {
	if p == "" {
		return "", errors.New("the path is empty")
	}
	if b.root == "" {
		if windowsRootedWithoutVolume(p) {
			volume := filepath.VolumeName(b.cwd)
			if volume == "" {
				return "", fmt.Errorf("filesystem: cannot resolve rooted path %q without a cwd volume", p)
			}
			return filepath.Clean(filepath.Join(volume+string(filepath.Separator), strings.TrimLeft(p, `\/`))), nil
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(b.cwd, p)
		}
		return filepath.Clean(p), nil
	}
	if filepath.VolumeName(p) != "" {
		return "", errors.New("filesystem: virtual path cannot have a volume")
	}
	virtualAbs := strings.HasPrefix(p, "/")
	if runtime.GOOS == "windows" {
		p = strings.ReplaceAll(p, "\\", "/")
		virtualAbs = strings.HasPrefix(p, "/")
	}
	base := filepath.Join(b.root, filepath.FromSlash(strings.TrimPrefix(b.cwd, "/")))
	if virtualAbs {
		base = b.root
		p = strings.TrimPrefix(p, "/")
	}
	clean := filepath.Clean(filepath.Join(base, filepath.FromSlash(p)))
	resolved, err := b.follow(clean, 0)
	if err != nil {
		return "", err
	}
	if followFinal || clean == b.root {
		return resolved, nil
	}
	parent, err := b.follow(filepath.Dir(clean), 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(clean)), nil
}

func windowsRootedWithoutVolume(p string) bool {
	if runtime.GOOS != "windows" || p == "" || filepath.VolumeName(p) != "" || (p[0] != '/' && p[0] != '\\') {
		return false
	}
	// A double separator starts a UNC/device path, not a path rooted on the
	// selected drive. Those paths carry their own volume when well formed.
	return len(p) == 1 || (p[1] != '/' && p[1] != '\\')
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// follow checks each existing prefix, including dangling links and parents of
// a newly created file. EvalSymlinks alone cannot validate nonexisting tails.
func (b *Backend) follow(p string, links int) (string, error) {
	if !within(b.root, p) {
		return "", fmt.Errorf("%s is outside the permitted root", p)
	}
	if links > 40 {
		return "", errors.New("too many symbolic links")
	}
	rel, _ := filepath.Rel(b.root, p)
	if rel == "." {
		return b.root, nil
	}
	parts := strings.Split(rel, string(filepath.Separator))
	current := b.root
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return filepath.Join(append([]string{current}, parts[i+1:]...)...), nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		target, err := os.Readlink(current)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}
		target = filepath.Join(append([]string{target}, parts[i+1:]...)...)
		return b.follow(filepath.Clean(target), links+1)
	}
	return current, nil
}

func (b *Backend) ReadFile(ctx context.Context, p string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, err := b.Resolve(p, true)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return data, err
}

func (b *Backend) Stat(ctx context.Context, p string) (fs.FileInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	full, err := b.Resolve(p, true)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return info, err
}

// OpenFile checks the same policy as the Node APIs. The caller owns the file.
func (b *Backend) OpenFile(ctx context.Context, p string, flags int, mode os.FileMode) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if flags&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		if err := b.CheckWritable(); err != nil {
			return nil, err
		}
	}
	full, err := b.Resolve(p, true)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(full, flags, mode)
	if err == nil && ctx.Err() != nil {
		return nil, errors.Join(ctx.Err(), file.Close())
	}
	return file, err
}

// WriteFileAtomic commits bytes with a sibling rename. Failure or cancellation
// before commit leaves an existing destination intact.
func (b *Backend) WriteFileAtomic(ctx context.Context, p string, data []byte) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := b.CheckWritable(); err != nil {
		return err
	}
	full, err := b.Resolve(p, true)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".noe-write-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmp != nil {
			err = errors.Join(err, tmp.Close())
		}
		_ = os.Remove(tmpName)
	}()
	for offset := 0; offset < len(data); {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(offset+32*1024, len(data))
		n, err := tmp.Write(data[offset:end])
		if err != nil {
			return err
		}
		if n != end-offset {
			return io.ErrShortWrite
		}
		offset = end
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	err = tmp.Close()
	tmp = nil
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(tmpName, full)
}
