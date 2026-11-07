package stdlib

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// LocalStorageOptions selects the file used for a runtime's localStorage.
// The selected filesystem backend's root, cwd, and write policy apply.
type LocalStorageOptions struct {
	Path string
}

const emptyStorageDocument = `{"version":1,"entries":[]}`

//go:embed storage.js
var storageJS string

type storageHost struct {
	rt           *quickjs.Runtime
	backend      *filesystem.Backend
	path         string
	lockKey      string
	ctx          context.Context
	fs           *fsHost
	domException quickjs.Value
}

// installStorage adds the per-runtime sessionStorage and, when selected,
// localStorage globals. Persistent state is accessed only through the
// already-selected filesystem backend.
func installStorage(rt *quickjs.Runtime, cfg Config) error {
	if rt == nil {
		return errors.New("storage: runtime is required")
	}
	ctx := rt.Context()
	if cfg.Loop != nil {
		ctx = loopContext(cfg.Loop)
	}

	native := rt.NewObject()
	var persistent *storageHost
	if cfg.LocalStorage != nil {
		if cfg.LocalStorage.Path == "" || strings.ContainsRune(cfg.LocalStorage.Path, '\x00') {
			return errors.New("storage: localStorage path must be nonempty and contain no NUL")
		}
		if cfg.FS == nil {
			return errors.New("storage: localStorage requires filesystem capability")
		}
		backend := cfg.FS.Backend
		if backend == nil {
			var err error
			backend, err = filesystem.New(filesystem.Config{
				Root: cfg.FS.Root, Cwd: cfg.FS.Cwd, ReadOnly: cfg.FS.ReadOnly,
			})
			if err != nil {
				return err
			}
		}
		fullPath, err := backend.Resolve(cfg.LocalStorage.Path, true)
		if err != nil {
			return fmt.Errorf("storage: resolve localStorage path: %w", err)
		}
		ioPath := fullPath
		if root := backend.Root(); root != "" {
			rel, relErr := filepath.Rel(root, fullPath)
			if relErr != nil {
				return fmt.Errorf("storage: localStorage path is outside the filesystem root: %w", relErr)
			}
			ioPath = "/" + filepath.ToSlash(rel)
		} else {
			ioPath = storageCanonicalPath(fullPath)
		}
		persistent = &storageHost{
			rt: rt, backend: backend, path: ioPath,
			lockKey: storageLockKey(fullPath), ctx: ctx,
			fs: &fsHost{
				rt:      rt,
				cfg:     &FS{Root: backend.Root(), Cwd: backend.Cwd(), Backend: backend},
				backend: backend,
			},
		}
		persistent.domException, err = rt.Get("DOMException")
		if err != nil {
			return err
		}
		if err := native.Set("transact", persistent.transact); err != nil {
			return err
		}
	}
	if err := native.Set("hasLocal", persistent != nil); err != nil {
		return err
	}
	_, err := evalWithHost(rt, "<storage>", storageJS, native)
	return err
}

func storageCanonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	abs = filepath.Clean(abs)
	probe := abs
	var suffix []string
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			parts := append([]string{resolved}, suffix...)
			canonical := filepath.Clean(filepath.Join(parts...))
			return canonical
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			break
		}
		suffix = append([]string{filepath.Base(probe)}, suffix...)
		probe = parent
	}
	return abs
}

func storageLockKey(path string) string {
	canonical := storageCanonicalPath(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(canonical)
	}
	return canonical
}

type storagePathLock struct {
	token chan struct{}
	refs  int
}

var storagePathLocks = struct {
	sync.Mutex
	locks map[string]*storagePathLock
}{locks: make(map[string]*storagePathLock)}

func acquireStoragePath(ctx context.Context, path string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	storagePathLocks.Lock()
	entry := storagePathLocks.locks[path]
	if entry == nil {
		entry = &storagePathLock{token: make(chan struct{}, 1)}
		entry.token <- struct{}{}
		storagePathLocks.locks[path] = entry
	}
	entry.refs++
	storagePathLocks.Unlock()

	select {
	case <-ctx.Done():
		releaseStoragePathRef(path, entry)
		return nil, ctx.Err()
	case <-entry.token:
		if err := ctx.Err(); err != nil {
			entry.token <- struct{}{}
			releaseStoragePathRef(path, entry)
			return nil, err
		}
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			entry.token <- struct{}{}
			releaseStoragePathRef(path, entry)
		})
	}, nil
}

func releaseStoragePathRef(path string, entry *storagePathLock) {
	storagePathLocks.Lock()
	entry.refs--
	if entry.refs == 0 && storagePathLocks.locks[path] == entry {
		delete(storagePathLocks.locks, path)
	}
	storagePathLocks.Unlock()
}

// transact runs a private JavaScript transform while holding the path lock.
// The transform receives a JSON document and returns {result, data?}. It is
// installed by storage.js and performs no user coercion or callbacks.
func (h *storageHost) transact(writing bool, transform quickjs.Value) (quickjs.Value, error) {
	if !transform.IsFunction() {
		return quickjs.Value{}, h.rt.ThrowTypeError("storage operation callback must be a function")
	}
	if writing {
		if err := h.backend.CheckWritable(); err != nil {
			return quickjs.Value{}, h.throwSecurityError("The filesystem is read-only")
		}
	}
	release, err := acquireStoragePath(h.ctx, h.lockKey)
	if err != nil {
		return quickjs.Value{}, h.fs.throwError(err)
	}
	document, err := h.backend.ReadFile(h.ctx, h.path)
	if errors.Is(err, fs.ErrNotExist) {
		document = []byte(emptyStorageDocument)
		err = nil
	}
	if err != nil {
		release()
		return quickjs.Value{}, h.fs.throwError(err)
	}
	changed, callErr := transform.CallContext(h.ctx, string(document))
	if callErr != nil {
		release()
		var jsErr *quickjs.Error
		if errors.As(callErr, &jsErr) {
			return quickjs.Value{}, h.rt.Throw(jsErr.Value())
		}
		return quickjs.Value{}, h.fs.throwError(callErr)
	}
	result, err := changed.Get("result")
	if err != nil {
		release()
		return quickjs.Value{}, err
	}
	data, err := changed.Get("data")
	if err != nil {
		release()
		return quickjs.Value{}, err
	}
	if !data.IsUndefined() {
		if !writing || !data.IsString() {
			release()
			return quickjs.Value{}, errors.New("storage: invalid private transaction result")
		}
		if err := h.backend.WriteFileAtomic(h.ctx, h.path, []byte(data.String())); err != nil {
			release()
			return quickjs.Value{}, h.fs.throwError(err)
		}
	}
	release()
	return result, nil
}

func (h *storageHost) throwSecurityError(message string) error {
	exception, err := h.domException.New(message, "SecurityError")
	if err != nil {
		return err
	}
	return h.rt.Throw(exception)
}
