package stdlib

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// FS describes the filesystem access a runtime is given.
//
// The zero value gives none: a host has to say what it is handing over. Root
// confines every path to one directory, which is the difference between giving
// a script a filesystem and giving it *a* filesystem:
//
//	stdlib.Files(rt, nil, &stdlib.FS{Root: "/srv/data", ReadOnly: true})
//
// With a root set, a path that climbs out of it with ".." or a symbolic link is
// refused rather than followed, and the script sees the root as "/".
type FS struct {
	// Cwd is fixed at installation; under Root it is a virtual directory.
	Cwd string
	// Backend is the shared implementation for this execution's Go bindings.
	// When supplied, its root, cwd and write policy are authoritative.
	Backend *filesystem.Backend
	// Root confines every path to this directory. Empty means the whole
	// filesystem, which is what a trusted script or a command-line tool wants
	// and what untrusted code must not have.
	Root string
	// ReadOnly refuses everything that would change anything.
	ReadOnly bool
	// Loop schedules whole-file promise operations off the runtime owner.
	// Without one those operations return settled promises. FileHandle I/O
	// always runs asynchronously and delivers results on the runtime owner.
	Loop *Loop
}

// Files installs the single "noe:fs" module. Async operations are direct
// named exports alongside explicit synchronous helpers:
//
//	import {readFile, writeFile, readFileSync} from "noe:fs"
//	const text = await readFile("in.txt", "utf8")
//	writeFileSync("out.txt", text)
//
// Files can also be read and written a piece at a time, or exposed as web
// streams:
//
//	const file = await open("big.bin")
//	const bytes = await file.read(16384)
//	new Response(createReadStream("big.bin"))
func Files(rt *quickjs.Runtime, cfg *FS) error {
	if cfg == nil {
		cfg = &FS{}
	}
	settings := *cfg
	backend := settings.Backend
	if backend == nil {
		var err error
		backend, err = filesystem.New(filesystem.Config{Root: settings.Root, Cwd: settings.Cwd, ReadOnly: settings.ReadOnly})
		if err != nil {
			return err
		}
	}
	settings.Root, settings.Cwd = backend.Root(), backend.Cwd()
	f := &fsHost{rt: rt, cfg: &settings, backend: backend}
	if err := f.installMetadata(); err != nil {
		return err
	}
	dirent, err := f.installDirectoryValues()
	if err != nil {
		return err
	}
	handles, err := newFileHost(f)
	if err != nil {
		return err
	}
	native := rt.NewObject()
	if err := native.Set("open", handles.openFile); err != nil {
		return err
	}
	fileAPI, err := evalWithHost(rt, "<fs-filehandle>", fileHandleJS, native)
	if err != nil {
		return err
	}
	open, err := fileAPI.Get("open")
	if err != nil {
		return err
	}
	watchAPI, err := f.installWatchValues()
	if err != nil {
		return err
	}
	watch, err := watchAPI.Get("watch")
	if err != nil {
		return err
	}

	sync := map[string]any{
		"readFileSync": func(p string, enc quickjs.Value) (any, error) {
			v, err := f.readFile(p, enc)
			return filesystemResult(f, v, err)
		},
		"writeFileSync":  func(p string, data quickjs.Value) error { return f.throwError(f.writeFile(p, data)) },
		"appendFileSync": func(p string, data quickjs.Value) error { return f.throwError(f.appendFile(p, data)) },
		"existsSync":     f.exists,
		"statSync":       func(p string) (quickjs.Value, error) { v, err := f.stat(p, false); return filesystemResult(f, v, err) },
		"lstatSync":      func(p string) (quickjs.Value, error) { v, err := f.stat(p, true); return filesystemResult(f, v, err) },
		"readdirSync": func(p string, opts quickjs.Value) (quickjs.Value, error) {
			v, err := f.readdir(p, opts)
			return filesystemResult(f, v, err)
		},
		"mkdirSync":    func(p string, opts quickjs.Value) error { return f.throwError(f.mkdir(p, opts)) },
		"rmSync":       func(p string, opts quickjs.Value) error { return f.throwError(f.remove(p, opts)) },
		"rmdirSync":    func(p string) error { return f.throwError(f.rmdir(p)) },
		"unlinkSync":   func(p string) error { return f.throwError(f.unlink(p)) },
		"renameSync":   func(from, to string) error { return f.throwError(f.rename(from, to)) },
		"copyFileSync": func(from, to string) error { return f.throwError(f.copyFile(from, to)) },
		"realpathSync": func(p string) (string, error) { v, err := f.realpath(p); return filesystemResult(f, v, err) },
		"readlinkSync": func(p string) (string, error) { v, err := f.readlink(p); return filesystemResult(f, v, err) },
		"chmodSync":    func(p string, mode float64) error { return f.throwError(f.chmod(p, mode)) },
		"mkdtempSync":  func(p string) (string, error) { v, err := f.mkdtemp(p); return filesystemResult(f, v, err) },
	}

	// Async operations are named exports on noe:fs. Each reads what it needs
	// from the runtime and then works with plain Go data off this goroutine.
	async := map[string]any{
		"open":  open,
		"watch": watch,
		"readFile": func(p string, enc quickjs.Value) *quickjs.Promise {
			text := encodingOf(enc) != ""
			return f.promise(func() (any, error) {
				b, err := f.readBytes(p)
				if err != nil || !text {
					return b, err
				}
				return string(b), nil
			})
		},
		"writeFile": func(p string, data quickjs.Value) *quickjs.Promise {
			b := contentsOf(data)
			return f.promise(func() (any, error) { return nil, f.writeBytes(p, b, false) })
		},
		"appendFile": func(p string, data quickjs.Value) *quickjs.Promise {
			b := contentsOf(data)
			return f.promise(func() (any, error) { return nil, f.writeBytes(p, b, true) })
		},
		"stat": func(p string) *quickjs.Promise {
			return f.promise(func() (any, error) { return f.statInfo(p, false) })
		},
		"lstat": func(p string) *quickjs.Promise {
			return f.promise(func() (any, error) { return f.statInfo(p, true) })
		},
		"readdir": func(p string, opts quickjs.Value) *quickjs.Promise {
			withTypes, err := directoryOptions(opts)
			return f.promise(func() (any, error) {
				if err != nil {
					return nil, err
				}
				return f.readDirectory(p, withTypes)
			})
		},
		"mkdir": func(p string, opts quickjs.Value) *quickjs.Promise {
			recursive := optionTruthy(opts, "recursive")
			return f.promise(func() (any, error) { return nil, f.mkdirAt(p, recursive) })
		},
		"rm": func(p string, opts quickjs.Value) *quickjs.Promise {
			recursive, force := optionTruthy(opts, "recursive"), optionTruthy(opts, "force")
			return f.promise(func() (any, error) { return nil, f.removeAt(p, recursive, force) })
		},
		"rmdir": func(p string) *quickjs.Promise {
			return f.promise(func() (any, error) { return nil, f.rmdir(p) })
		},
		"unlink": func(p string) *quickjs.Promise {
			return f.promise(func() (any, error) { return nil, f.unlink(p) })
		},
		"rename": func(from, to string) *quickjs.Promise {
			return f.promise(func() (any, error) { return nil, f.rename(from, to) })
		},
		"copyFile": func(from, to string) *quickjs.Promise {
			return f.promise(func() (any, error) { return nil, f.copyFile(from, to) })
		},
		"realpath": func(p string) *quickjs.Promise {
			return f.promise(func() (any, error) { return f.realpath(p) })
		},
		"readlink": func(p string) *quickjs.Promise {
			return f.promise(func() (any, error) { return f.readlink(p) })
		},
		"chmod": func(p string, mode float64) *quickjs.Promise {
			return f.promise(func() (any, error) { return nil, f.chmod(p, mode) })
		},
		"mkdtemp": func(prefix string) *quickjs.Promise {
			return f.promise(func() (any, error) { return f.mkdtemp(prefix) })
		},
	}

	// The stream halves are script over what the host opened, and are only
	// installed where there are streams to build them out of.
	streamHost := rt.NewObject()
	if err := errors.Join(
		streamHost.Set("openRead", func(p string, start, end int64) (quickjs.Value, error) {
			v, err := f.openRead(p, start, end)
			return filesystemResult(f, v, err)
		}),
		streamHost.Set("openWrite", func(p string, append bool) (quickjs.Value, error) {
			v, err := f.openWrite(p, append)
			return filesystemResult(f, v, err)
		}),
	); err != nil {
		return err
	}
	streams, err := evalWithHost(rt, "<fs-streams>", fsStreamsJS, streamHost)
	if err != nil {
		return err
	}

	exports := make(map[string]any, len(sync)+len(async)+4)
	exports["Dirent"] = dirent
	for k, v := range sync {
		exports[k] = v
	}
	for k, v := range async {
		exports[k] = v
	}
	for _, name := range []string{"createReadStream", "createWriteStream"} {
		v, err := streams.Get(name)
		if err != nil {
			return err
		}
		exports[name] = v
	}
	exports["default"] = copyExports(exports)
	return setFSModule(rt, exports)
}

func setFSModule(rt *quickjs.Runtime, exports map[string]any) error {
	values := make(map[string]quickjs.Value, len(exports))
	for name, value := range exports {
		if name == "default" {
			continue
		}
		encoded, err := rt.Encode(value)
		if err != nil {
			return err
		}
		values[name] = encoded
	}
	if value, ok := exports["default"]; ok {
		def, err := nodeDefault(rt, value, values)
		if err != nil {
			return err
		}
		values["default"] = def
	}
	return rt.SetModuleValues("noe:fs", values)
}

// fsHost keeps JS conversion separate from the shared Go filesystem.
type fsHost struct {
	rt      *quickjs.Runtime
	cfg     *FS
	backend *filesystem.Backend
	dirents quickjs.Value
	date    quickjs.Value
}

func (f *fsHost) rootPath() (string, error)             { return f.backend.Root(), nil }
func (f *fsHost) resolve(p string) (string, error)      { return f.backend.Resolve(p, true) }
func (f *fsHost) resolveEntry(p string) (string, error) { return f.backend.Resolve(p, false) }
func (f *fsHost) writable() error                       { return f.backend.CheckWritable() }

func (f *fsHost) readFile(p string, enc quickjs.Value) (any, error) {
	b, err := f.readBytes(p)
	if err != nil {
		return nil, err
	}
	if encodingOf(enc) != "" {
		return string(b), nil
	}
	return f.rt.NewBytes(b), nil
}

// readBytes is readFile without the runtime, which is what the promise form
// runs on another goroutine.
func (f *fsHost) readBytes(p string) ([]byte, error) {
	full, err := f.resolve(p)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(full)
}

func (f *fsHost) writeFile(p string, data quickjs.Value) error {
	return f.writeBytes(p, contentsOf(data), false)
}

func (f *fsHost) appendFile(p string, data quickjs.Value) error {
	return f.writeBytes(p, contentsOf(data), true)
}

// writeBytes writes or appends, which differ only in how the file is opened.
func (f *fsHost) writeBytes(p string, b []byte, appending bool) error {
	if err := f.writable(); err != nil {
		return err
	}
	full, err := f.resolve(p)
	if err != nil {
		return err
	}
	if !appending {
		return os.WriteFile(full, b, 0o644)
	}
	file, err := os.OpenFile(full, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(b)
	return err
}

func (f *fsHost) exists(p string) bool {
	full, err := f.resolve(p)
	if err != nil {
		return false
	}
	_, err = os.Stat(full)
	return err == nil
}

func (f *fsHost) stat(p string, link bool) (quickjs.Value, error) {
	info, err := f.statInfo(p, link)
	if err != nil {
		return quickjs.Value{}, err
	}
	return f.statValue(info)
}

// statInfo is stat without the runtime.
func (f *fsHost) statInfo(p string, link bool) (os.FileInfo, error) {
	resolve := f.resolve
	if link {
		resolve = f.resolveEntry
	}
	full, err := resolve(p)
	if err != nil {
		return nil, err
	}
	if link {
		return os.Lstat(full)
	}
	return os.Stat(full)
}

func (f *fsHost) mkdir(p string, opts quickjs.Value) error {
	return f.mkdirAt(p, optionTruthy(opts, "recursive"))
}

func (f *fsHost) mkdirAt(p string, recursive bool) error {
	if err := f.writable(); err != nil {
		return err
	}
	full, err := f.resolve(p)
	if err != nil {
		return err
	}
	if recursive {
		return os.MkdirAll(full, 0o755)
	}
	return os.Mkdir(full, 0o755)
}

func (f *fsHost) remove(p string, opts quickjs.Value) error {
	return f.removeAt(p, optionTruthy(opts, "recursive"), optionTruthy(opts, "force"))
}

func (f *fsHost) removeAt(p string, recursive, force bool) error {
	if err := f.writable(); err != nil {
		return err
	}
	full, err := f.resolveEntry(p)
	if err != nil {
		return err
	}
	if recursive {
		err = os.RemoveAll(full)
	} else {
		err = os.Remove(full)
	}
	if err != nil && force && errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func (f *fsHost) rmdir(p string) error {
	if err := f.writable(); err != nil {
		return err
	}
	full, err := f.resolveEntry(p)
	if err != nil {
		return err
	}
	return os.Remove(full)
}

func (f *fsHost) unlink(p string) error { return f.rmdir(p) }

func (f *fsHost) rename(from, to string) error {
	if err := f.writable(); err != nil {
		return err
	}
	src, err := f.resolveEntry(from)
	if err != nil {
		return err
	}
	dst, err := f.resolveEntry(to)
	if err != nil {
		return err
	}
	return os.Rename(src, dst)
}

func (f *fsHost) copyFile(from, to string) error {
	if err := f.writable(); err != nil {
		return err
	}
	src, err := f.resolve(from)
	if err != nil {
		return err
	}
	dst, err := f.resolve(to)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

func (f *fsHost) realpath(p string) (string, error) {
	full, err := f.resolve(p)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	return f.show(real), nil
}

func (f *fsHost) readlink(p string) (string, error) {
	full, err := f.resolveEntry(p)
	if err != nil {
		return "", err
	}
	target, err := os.Readlink(full)
	if err != nil {
		return "", err
	}
	return target, nil
}

func (f *fsHost) chmod(p string, mode float64) error {
	if err := f.writable(); err != nil {
		return err
	}
	full, err := f.resolve(p)
	if err != nil {
		return err
	}
	return os.Chmod(full, os.FileMode(int(mode)&0o777))
}

func (f *fsHost) mkdtemp(prefix string) (string, error) {
	if err := f.writable(); err != nil {
		return "", err
	}
	// Split before resolving: the suffix is appended to the prefix, so a
	// trailing separator means an empty name prefix inside that directory.
	dir, name := filepath.Split(prefix)
	if dir == "" {
		dir = "."
	}
	dir, err := f.resolve(dir)
	if err != nil {
		return "", err
	}
	out, err := os.MkdirTemp(dir, name+"*")
	if err != nil {
		return "", err
	}
	return f.show(out), nil
}

// show turns a host path back into what the script calls it, which under a
// root is the path relative to it.
func (f *fsHost) show(p string) string {
	if f.cfg.Root == "" {
		return p
	}
	root, err := f.rootPath()
	if err != nil {
		return p
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return "/" + filepath.ToSlash(rel)
}

// promise runs an operation and hands back a promise for its result.
//
// With a loop, the work runs on another goroutine: what it is given is plain Go
// data, read out of the runtime before it starts, and what it hands back is
// plain Go data too, turned into a JavaScript value on the loop's goroutine
// where that is allowed. Without a loop there is nowhere to settle a promise
// later, so the work happens at once and the promise comes back settled.
func (f *fsHost) promise(work func() (any, error)) *quickjs.Promise {
	p := f.rt.NewPromise()
	loop := f.cfg.Loop
	if loop == nil {
		v, err := work()
		f.settle(p, v, err)
		return p
	}
	loop.Begin()
	if err := loop.Go(func(context.Context) {
		defer loop.Done()
		v, err := work()
		loop.postExecution(func() { f.settle(p, v, err) })
	}); err != nil {
		loop.Done()
		f.reject(p, err)
	}
	return p
}

// settle resolves or rejects a promise with what an operation produced,
// turning the Go forms the operations deal in into what script expects.
func (f *fsHost) settle(p *quickjs.Promise, v any, err error) {
	if err != nil {
		f.reject(p, err)
		return
	}
	switch x := v.(type) {
	case nil:
		p.Resolve(nil)
	case []byte:
		p.Resolve(f.rt.NewBytes(x))
	case directoryEntries:
		val, err := f.directoryValue(x)
		if err != nil {
			f.reject(p, err)
			return
		}
		p.Resolve(val)
	case os.FileInfo:
		val, err := f.statValue(x)
		if err != nil {
			f.reject(p, err)
			return
		}
		p.Resolve(val)
	default:
		p.Resolve(v)
	}
}

// ---------------------------------------------------------------------------
// Argument helpers
// ---------------------------------------------------------------------------

// encodingOf reads the encoding argument, which is either a string or an
// object with an encoding property, as node accepts both.
func encodingOf(v quickjs.Value) string {
	switch v.Kind() {
	case quickjs.KindString:
		return v.String()
	case quickjs.KindObject:
		if enc, err := v.Get("encoding"); err == nil && enc.Kind() == quickjs.KindString {
			return enc.String()
		}
	}
	return ""
}

// contentsOf is the bytes to write: the bytes of a typed array, or the text of
// anything else.
func contentsOf(v quickjs.Value) []byte {
	if b, ok := v.Bytes(); ok {
		out := make([]byte, len(b))
		copy(out, b)
		return out
	}
	return []byte(v.String())
}

// optionTruthy reads a flag from an options object.
func optionTruthy(v quickjs.Value, name string) bool {
	if v.Kind() != quickjs.KindObject {
		return false
	}
	f, err := v.Get(name)
	return err == nil && f.Bool()
}
