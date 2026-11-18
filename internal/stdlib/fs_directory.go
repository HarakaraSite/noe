package stdlib

import (
	_ "embed"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed fs_directory.js
var fsDirectoryJS string

// directoryCursor uses the existing FileHandle host for admission, asynchronous
// delivery, and execution/runtime cleanup. Only its batching is directory-specific.
type directoryCursor struct {
	host *fileHost
	path string
	open func(string) (directoryBackend, error)

	mu     sync.Mutex
	closed bool
	file   *fileHandle
}

type directoryBackend interface {
	fileBackend
	ReadDir(int) ([]os.DirEntry, error)
}

type directoryBatch struct {
	entries directoryEntries
	done    bool
}

func (f *fsHost) installDirectoryIterator(host *fileHost) (quickjs.Value, error) {
	native := f.rt.NewObject()
	if err := native.Set("create", func(path string) (quickjs.Value, error) {
		cursor := &directoryCursor{host: host, path: path, open: func(path string) (directoryBackend, error) { return os.Open(path) }}
		object := f.rt.NewObject()
		err := errors.Join(
			object.Set("read", cursor.read),
			object.Set("close", cursor.closeValue),
			object.Set("check", func() error {
				if host.ctx.Err() != nil {
					return f.throwError(fileError(os.ErrClosed, "ERR_FILE_CLOSED"))
				}
				return nil
			}),
		)
		return object, err
	}); err != nil {
		return quickjs.Value{}, err
	}
	return evalWithHost(f.rt, "<fs-directory>", fsDirectoryJS, native)
}

func (c *directoryCursor) opened() (*fileHandle, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, nil
	}
	if c.file != nil {
		file := c.file
		c.mu.Unlock()
		return file, nil
	}
	c.mu.Unlock()

	resolved, err := c.host.fs.resolve(c.path)
	if err != nil {
		return nil, err
	}
	file, err := c.open(resolved)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err == nil && !info.IsDir() {
		err = &os.PathError{Op: "opendir", Path: resolved, Err: syscall.ENOTDIR}
	}
	if err != nil {
		return nil, errors.Join(err, file.Close())
	}
	handle := newFileHandle(c.host, file)
	if !c.host.own(handle) {
		return nil, fileError(os.ErrClosed, "ERR_FILE_CLOSED")
	}
	c.mu.Lock()
	if c.closed || c.host.ctx.Err() != nil {
		c.mu.Unlock()
		return nil, errors.Join(fileError(os.ErrClosed, "ERR_FILE_CLOSED"), handle.Close())
	}
	c.file = handle
	c.mu.Unlock()
	return handle, nil
}

func (c *directoryCursor) read(batchSize int64) *quickjs.Promise {
	if batchSize < 1 || batchSize > maxSafeFileInteger || uint64(batchSize) > uint64(^uint(0)>>1) {
		return c.host.rejectedFilePromise("batchSize must be a positive safe integer supported by this platform", "ERR_INVALID_ARG")
	}
	return c.host.asyncFile(func() (any, error) {
		return c.readBatch(int(batchSize))
	}, func(value any) (any, error) {
		batch := value.(directoryBatch)
		entries, err := c.host.fs.directoryValue(batch.entries)
		if err != nil {
			return nil, err
		}
		object := c.host.rt.NewObject()
		err = errors.Join(object.Set("entries", entries), object.Set("done", batch.done))
		return object, err
	}, func(any) { _ = c.close() }, nil)
}

// readBatch runs only off the runtime owner. FileHandle's operation accounting
// lets owner shutdown close the descriptor before joining an in-flight read.
func (c *directoryCursor) readBatch(batchSize int) (directoryBatch, error) {
	result := directoryBatch{entries: directoryEntries{parent: c.path, entries: make([]map[string]any, 0)}}
	handle, err := c.opened()
	if err != nil {
		return result, err
	}
	if handle == nil {
		result.done = true
		return result, nil
	}
	file, err := handle.beginIO()
	if err != nil {
		return result, err
	}
	entries, readErr := file.(directoryBackend).ReadDir(batchSize)
	handle.ops.Done()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return result, errors.Join(readErr, c.close())
	}
	for _, entry := range entries {
		result.entries.entries = append(result.entries.entries, map[string]any{"name": entry.Name(), "kind": fileKind(entry.Type())})
	}
	result.done = errors.Is(readErr, io.EOF) || len(entries) == 0
	if result.done {
		if err := c.close(); err != nil {
			return directoryBatch{}, err
		}
	}
	return result, nil
}

func (c *directoryCursor) beginClose() *fileHandle {
	c.mu.Lock()
	c.closed = true
	file := c.file
	c.mu.Unlock()
	if file != nil {
		file.beginClose()
	}
	return file
}

func (c *directoryCursor) close() error {
	if file := c.beginClose(); file != nil {
		return file.finishClose(true)
	}
	return nil
}

func (c *directoryCursor) closeValue() *quickjs.Promise {
	file := c.beginClose()
	if file == nil || c.host.ctx.Err() != nil {
		// A cancelled owner's Go cleanup already owns any admitted descriptor.
		p := c.host.rt.NewPromise()
		p.Resolve(nil)
		return p
	}
	return c.host.asyncFile(func() (any, error) { return nil, file.finishClose(true) }, nil, nil, nil)
}
