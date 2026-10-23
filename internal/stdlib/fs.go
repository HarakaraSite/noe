package stdlib

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

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
	// Loop, when set, is where the promise API does its work: the operation
	// runs on another goroutine and settles its promise on the loop. Without
	// one the work happens at once and the promise is handed back settled,
	// which is right for a script that is the only thing running.
	Loop *Loop
}

// Files installs the "fs" module.
//
// The synchronous half is what node calls readFileSync and friends; the
// promise half is fs.promises, and also the default export's promises
// property, so the shapes a program is written against both work:
//
//	import fs from "fs"
//	const text = fs.readFileSync("in.txt", "utf8")
//	await fs.promises.writeFile("out.txt", text)
//
// A file can also be read and written a piece at a time, which is what serving
// something larger than memory needs:
//
//	new Response(fs.createReadStream("big.bin"))
//
// Those two are the web's streams rather than node's -- there are no node
// streams here -- so they are read with for-await and joined with pipeTo.
//
// A path is a string; contents are a string when an encoding is given and a
// Uint8Array when it is not, which is what node does with Buffer.
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

	sync := map[string]any{
		"readFileSync":   f.readFile,
		"writeFileSync":  f.writeFile,
		"appendFileSync": f.appendFile,
		"existsSync":     f.exists,
		"statSync":       func(p string) (quickjs.Value, error) { return f.stat(p, false) },
		"lstatSync":      func(p string) (quickjs.Value, error) { return f.stat(p, true) },
		"readdirSync":    f.readdir,
		"mkdirSync":      f.mkdir,
		"rmSync":         f.remove,
		"rmdirSync":      f.rmdir,
		"unlinkSync":     f.unlink,
		"renameSync":     f.rename,
		"copyFileSync":   f.copyFile,
		"realpathSync":   f.realpath,
		"readlinkSync":   f.readlink,
		"chmodSync":      f.chmod,
		"mkdtempSync":    f.mkdtemp,
	}

	// The promise half runs the same operations, named as node names them. Each
	// reads what it needs out of the runtime here and then does its work with
	// plain Go data, which is what lets the work happen off this goroutine.
	promises := map[string]any{
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
		"readdir": func(p string) *quickjs.Promise {
			return f.promise(func() (any, error) { return f.readdir(p) })
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
		"mkdtemp": func(prefix string) *quickjs.Promise {
			return f.promise(func() (any, error) { return f.mkdtemp(prefix) })
		},
	}

	// The stream halves are script over what the host opened, and are only
	// installed where there are streams to build them out of.
	streamHost := rt.NewObject()
	if err := errors.Join(
		streamHost.Set("openRead", f.openRead),
		streamHost.Set("openWrite", f.openWrite),
	); err != nil {
		return err
	}
	streams, err := evalWithHost(rt, "<fs-streams>", fsStreamsJS, streamHost)
	if err != nil {
		return err
	}

	exports := make(map[string]any, len(sync)+4)
	for k, v := range sync {
		exports[k] = v
	}
	for _, name := range []string{"createReadStream", "createWriteStream"} {
		v, err := streams.Get(name)
		if err != nil {
			return err
		}
		exports[name] = v
	}
	exports["promises"] = promises
	def := copyExports(exports)
	exports["default"] = def

	if err := setNodeModule(rt, "fs", exports); err != nil {
		return err
	}
	// fs/promises is a module of its own, as it is in node.
	pexports := make(map[string]any, len(promises)+1)
	for k, v := range promises {
		pexports[k] = v
	}
	pexports["default"] = copyExports(pexports)
	return setNodeModule(rt, "fs/promises", pexports)
}

// fsHost keeps JS conversion separate from the shared Go filesystem.
type fsHost struct {
	rt      *quickjs.Runtime
	cfg     *FS
	backend *filesystem.Backend
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

// statValue is the object a stat call hands back, with the methods node's has.
func (f *fsHost) statValue(info os.FileInfo) (quickjs.Value, error) {
	o := f.rt.NewObject()
	mode := info.Mode()
	set := func(name string, v any) error { return o.Set(name, v) }
	if err := errors.Join(
		set("size", float64(info.Size())),
		set("mode", float64(mode.Perm())),
		set("mtimeMs", float64(info.ModTime().UnixNano())/1e6),
		set("mtime", info.ModTime().Format(time.RFC3339Nano)),
		set("isFile", func() bool { return mode.IsRegular() }),
		set("isDirectory", func() bool { return mode.IsDir() }),
		set("isSymbolicLink", func() bool { return mode&os.ModeSymlink != 0 }),
	); err != nil {
		return quickjs.Value{}, err
	}
	return o, nil
}

func (f *fsHost) readdir(p string) (any, error) {
	full, err := f.resolve(p)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
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
	dir := ""
	if f.cfg.Root != "" {
		root, err := f.resolve("/")
		if err != nil {
			return "", err
		}
		dir = root
	}
	out, err := os.MkdirTemp(dir, filepath.Base(prefix)+"*")
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
		p.RejectError(err)
	}
	return p
}

// settle resolves or rejects a promise with what an operation produced,
// turning the Go forms the operations deal in into what script expects.
func (f *fsHost) settle(p *quickjs.Promise, v any, err error) {
	if err != nil {
		p.RejectError(err)
		return
	}
	switch x := v.(type) {
	case nil:
		p.Resolve(nil)
	case []byte:
		p.Resolve(f.rt.NewBytes(x))
	case os.FileInfo:
		val, err := f.statValue(x)
		if err != nil {
			p.RejectError(err)
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
