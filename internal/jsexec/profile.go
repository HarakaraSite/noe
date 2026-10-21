// Package jsexec contains the lower-level, execution-scoped profile and
// binding view used by the public stdlib package.
package jsexec

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	"forge.harakara.site/littleisland/noe/v2/internal/noestdlib"
	lower "forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// Environment is the one snapshot shared by the modules installed on an
// execution. Readers and writers are borrowed; Args and Env are copied.
type Environment struct {
	Cwd    string
	Args   []string
	Env    map[string]string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Profile selects the environment and capabilities for one runtime.
type Profile struct {
	Environment Environment
	Modules     Modules
	NewLoader   LoaderFactory
	Initialize  RuntimeInitializer
}

// Modules selects capabilities independently. A nil option pointer disables
// that capability; option pointers are copied when the profile is attached.
type Modules struct {
	ProcessInfo  bool
	Stdio        bool
	FS           *FileSystemOptions
	Commands     *CommandOptions
	DNS          *DNSOptions
	Fetch        *FetchOptions
	Serve        *ServeOptions
	WebSockets   *WebSocketsOptions
	RawSockets   bool
	Signals      bool
	Workers      *WorkerOptions
	OS           *OSOptions
	Random       io.Reader
	WindowsPaths bool
}

type FileSystemOptions struct {
	Root     string
	ReadOnly bool
}

type CommandOptions struct {
	// Supervisor is an explicit process-helper executable; empty starts directly.
	Supervisor     string
	Allow          func(name string, args []string) error
	Timeout        time.Duration
	MaxOutputBytes int
}

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
	LookupAddr(context.Context, string) ([]string, error)
	LookupCNAME(context.Context, string) (string, error)
	LookupMX(context.Context, string) ([]*net.MX, error)
	LookupNS(context.Context, string) ([]*net.NS, error)
	LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error)
	LookupTXT(context.Context, string) ([]string, error)
}

type DNSOptions struct {
	Resolver Resolver
	Allow    func(name string) error
	Servers  []string
	Timeout  time.Duration
}

type FetchOptions struct {
	Client       *http.Client
	Allow        func(*http.Request) error
	MaxBodyBytes int64
}

type ServeOptions struct {
	Allow        func(address string) error
	MaxBodyBytes int64
	ReadTimeout  time.Duration
	IdleTimeout  time.Duration
}

type WebSocketsOptions struct {
	Allow            func(target *url.URL) error
	MaxMessageBytes  int
	HandshakeTimeout time.Duration
	TLS              *tls.Config
}

type WorkerOptions struct {
	Max int
}

type OSOptions struct {
	Hostname string
	Homedir  string
	Tmpdir   string
	Real     bool
}

// Source is an entry's source text and its identifier. Virtual sources keep
// their supplied name and resolve relative imports from Environment.Cwd.
type Source struct {
	Name    string
	Code    string
	Module  bool
	Virtual bool
}

// Origin identifies the entry and execution view for one new module loader.
type Origin struct {
	// Source is a snapshot of an already supplied entry, or nil while loading
	// a Worker entry for the first time. Factories can seed its module cache.
	Source     *Source
	Entry      string
	Cwd        string
	Virtual    bool
	Worker     bool
	FileSystem FileSystem
}

// LoaderFactory creates a runtime-local module loader on the runtime owner.
type LoaderFactory func(context.Context, Origin) (quickjs.ModuleLoader, error)

type RuntimeInitializer func(context.Context, *Bindings) (cleanup func(), err error)

// FileSystem is the shared Go boundary for the runtime's selected filesystem.
// It intentionally offers no method to close or reconfigure the backend.
type FileSystem interface {
	ReadFile(context.Context, string) ([]byte, error)
	WriteFileAtomic(context.Context, string, []byte) error
	CheckWritable() error
	Stat(context.Context, string) (fs.FileInfo, error)
	PhysicalCwd() string
}

// Bindings is a borrowed view of one attached execution. Its methods that
// interact with QuickJS must be called on the runtime owner.
type Bindings struct {
	rt            *quickjs.Runtime
	loop          *lower.Loop
	ctx           context.Context
	env           Environment
	fs            FileSystem
	backend       *filesystem.Backend
	worker        bool
	loader        LoaderFactory
	modules       map[string]string
	closed        bool
	cleanup       func()
	adapter       func()
	cancel        context.CancelFunc
	scope         *lower.ExecutionScope
	io            *executionIO
	stopParent    func() bool
	load          quickjs.ModuleLoader
	prepared      bool
	failures      []error
	exitRequested bool
	exitCode      int
	closeErr      error
	snapshotter   quickjs.Value
	mu            sync.Mutex
}

// Attach installs a profile into a caller-owned runtime and loop. It does not
// close either borrowed object, including when setup fails.
func Attach(rt *quickjs.Runtime, loop *lower.Loop, profile Profile) (*Bindings, error) {
	return attach(rt, loop, profile, false, nil, nil)
}

func attach(rt *quickjs.Runtime, loop *lower.Loop, profile Profile, worker bool, workerConfig *lower.Config, parent context.Context) (_ *Bindings, err error) {
	if rt == nil {
		return nil, errors.New("stdlib: runtime is required")
	}
	if loop == nil {
		return nil, errors.New("stdlib: loop is required")
	}
	if !loop.Owns(rt) {
		return nil, errors.New("stdlib: loop belongs to a different runtime")
	}
	if err := loop.Context().Err(); err != nil {
		return nil, fmt.Errorf("stdlib: execution is closed: %w", err)
	}

	profile, env, backend, err := normalize(profile)
	if err != nil {
		return nil, err
	}
	if worker && workerConfig != nil {
		cwd, args, values := lower.ExecutionEnvironment(*workerConfig)
		if cwd != "" {
			env.Cwd = cwd
		}
		env.Args, env.Env = args, values
		env.Stdin = nil
	}
	if parent == nil {
		parent = loop.Context()
	}
	ctx, cancel := context.WithCancel(parent)
	var fsview FileSystem
	if profile.Modules.FS != nil {
		fsview = filesystem.WithContext(ctx, backend)
	}
	b := &Bindings{rt: rt, loop: loop, ctx: ctx, env: env, fs: fsview, backend: backend, worker: worker, loader: profile.NewLoader, modules: make(map[string]string), cancel: cancel}
	b.stopParent = context.AfterFunc(loop.Context(), cancel)
	defer func() {
		if err != nil {
			err = b.retainException(err)
			_ = b.Close()
		}
	}()
	b.snapshotter, err = rt.EvalFileContext(ctx, "<stdlib-diagnostic>", diagnosticSource)
	if err != nil {
		return nil, err
	}
	b.scope, err = lower.NewExecutionScope(loop, ctx)
	if err != nil {
		return nil, err
	}
	env, b.io, err = bindIO(ctx, env, b.fail)
	if err != nil {
		return nil, err
	}
	b.env = env
	var workerInstall lower.WorkerInstaller
	if profile.Modules.Workers != nil {
		workerProfile := profile
		// Share the parent's serialized output boundary, but each Worker owns
		// its own cancellable facade and gets empty input.
		workerProfile.Environment.Stdout, workerProfile.Environment.Stderr = env.Stdout, env.Stderr
		workerInstall = func(workerRT *quickjs.Runtime, workerLoop *lower.Loop, config lower.Config) (lower.WorkerInstallation, error) {
			view, installErr := attach(workerRT, workerLoop, workerProfile, true, &config, nil)
			if installErr != nil {
				return lower.WorkerInstallation{}, installErr
			}
			return lower.WorkerInstallation{Prepare: view.prepareWorker, Close: func() { _ = view.Close() }}, nil
		}
	}
	core, native, err := lowerConfig(profile, env, backend, loop)
	if err != nil {
		cancel()
		return nil, err
	}
	if core.Process != nil {
		lower.SetExecutionExit(core.Process, b.requestExit)
		core.Process.Unhandled = b.unhandled
	}
	if workerConfig != nil {
		lower.InheritWorkerState(&core, *workerConfig)
	}
	installed, installErr := noestdlib.InstallProfile(rt, noestdlib.ProfileConfig{
		Context: ctx, Core: core, IO: noestdlib.IOConfig{Stdin: env.Stdin, Cwd: backend.PhysicalCwd()}, Native: native, WorkerInstall: workerInstall,
	})
	if installErr != nil {
		return nil, installErr
	}
	b.adapter = installed.Close
	if core.Process == nil {
		rt.OnUnhandledRejection(b.unhandled)
	}
	// A direct runtime close also releases this binding's adapters and its
	// initializer. The hook only runs Go cleanup and never touches JS values.
	rt.OnClose(func() { _ = b.Close() })
	if profile.Initialize != nil {
		cleanup, initErr := profile.Initialize(ctx, b)
		b.mu.Lock()
		closed := b.closed
		if !closed {
			b.cleanup = cleanup
		}
		b.mu.Unlock()
		if closed {
			if cleanup != nil {
				cleanup()
			}
			if initErr != nil {
				return nil, initErr
			}
			return nil, errors.New("stdlib: bindings closed during initialization")
		}
		if initErr != nil {
			return nil, initErr
		}
	}
	return b, nil
}

func lowerConfig(profile Profile, env Environment, backend *filesystem.Backend, loop *lower.Loop) (lower.Config, noestdlib.NativeModules, error) {
	modules := profile.Modules
	cfg := lower.Config{
		Stdout: env.Stdout, Stderr: env.Stderr, Loop: loop,
		Random: modules.Random, WindowsPaths: modules.WindowsPaths,
	}
	lower.SetExecutionEnvironment(&cfg, env.Cwd, env.Args, env.Env)
	if modules.FS != nil {
		cfg.FS = &lower.FS{Cwd: env.Cwd, Backend: backend, Loop: loop}
	}
	if modules.ProcessInfo {
		cfg.Process = &lower.Process{
			Args: cloneArgs(env.Args), Env: cloneMap(env.Env), Cwd: env.Cwd,
			Stdin: env.Stdin, Stdout: env.Stdout, Stderr: env.Stderr,
		}
	}
	if modules.OS != nil {
		cfg.OS = &lower.OSInfo{Hostname: modules.OS.Hostname, Homedir: modules.OS.Homedir, Tmpdir: modules.OS.Tmpdir, Real: modules.OS.Real}
	}
	if modules.Fetch != nil {
		cfg.Fetch = &lower.Fetch{Loop: loop, Client: modules.Fetch.Client, Allow: modules.Fetch.Allow, MaxBodyBytes: modules.Fetch.MaxBodyBytes}
	}
	if modules.DNS != nil {
		cfg.DNS = &lower.DNS{Loop: loop, Resolver: modules.DNS.Resolver, Allow: modules.DNS.Allow, Servers: append([]string(nil), modules.DNS.Servers...), Timeout: modules.DNS.Timeout}
	}
	if modules.Commands != nil {
		cfg.Run = &lower.Run{Supervisor: modules.Commands.Supervisor, Loop: loop, Allow: modules.Commands.Allow, Dir: backend.PhysicalCwd(), Env: cloneMap(env.Env), Timeout: modules.Commands.Timeout, MaxOutputBytes: modules.Commands.MaxOutputBytes}
		cfg.Run.ResolveDirectory = func(dir string) (string, error) { return backend.Resolve(dir, true) }
	}
	var serve *lower.Serve
	if modules.Serve != nil {
		serve = &lower.Serve{Loop: loop, Allow: modules.Serve.Allow, MaxBodyBytes: modules.Serve.MaxBodyBytes, ReadTimeout: modules.Serve.ReadTimeout, IdleTimeout: modules.Serve.IdleTimeout}
		cfg.Serve = serve
	}
	if modules.WebSockets != nil {
		var socketServe *lower.Serve
		if serve != nil {
			socketServe = serve
		}
		var tlsConfig *tls.Config
		if modules.WebSockets.TLS != nil {
			tlsConfig = modules.WebSockets.TLS.Clone()
		}
		cfg.Sockets = &lower.WebSockets{Loop: loop, Allow: modules.WebSockets.Allow, Serve: socketServe, MaxMessageBytes: modules.WebSockets.MaxMessageBytes, HandshakeTimeout: modules.WebSockets.HandshakeTimeout, TLS: tlsConfig}
	}
	if modules.Workers != nil {
		cfg.Workers = &lower.Workers{Max: modules.Workers.Max}
	}
	selection := noestdlib.NativeModules{
		Stdio: modules.Stdio, Commands: modules.Commands != nil, FS: modules.FS != nil,
		RawSockets: modules.RawSockets, Signals: modules.Signals,
	}
	return cfg, selection, nil
}

func normalize(profile Profile) (Profile, Environment, *filesystem.Backend, error) {
	profile = cloneProfile(profile)
	env := profile.Environment
	if env.Env == nil {
		env.Env = map[string]string{}
	}
	if env.Args == nil {
		env.Args = []string{}
	}
	if env.Stdin == nil {
		env.Stdin = strings.NewReader("")
	}
	if env.Stdout == nil {
		env.Stdout = io.Discard
	}
	if env.Stderr == nil {
		env.Stderr = io.Discard
	}
	root, readOnly := "", false
	if profile.Modules.FS != nil {
		root, readOnly = profile.Modules.FS.Root, profile.Modules.FS.ReadOnly
	}
	backend, err := filesystem.New(filesystem.Config{Root: root, Cwd: env.Cwd, ReadOnly: readOnly})
	if err != nil {
		return Profile{}, Environment{}, nil, fmt.Errorf("stdlib: configure execution cwd/filesystem: %w", err)
	}
	env.Cwd = backend.Cwd()
	if profile.Modules.FS != nil {
		// Carry the creation-time absolute root into worker profiles so later
		// attaches never reinterpret a relative Root against ambient cwd.
		profile.Modules.FS.Root = backend.Root()
	}
	if commands := profile.Modules.Commands; commands != nil && commands.Supervisor != "" {
		helper, helperErr := filepath.Abs(commands.Supervisor)
		if helperErr != nil {
			return Profile{}, Environment{}, nil, fmt.Errorf("stdlib: process helper: %w", helperErr)
		}
		commands.Supervisor = helper
	}
	profile.Environment = cloneEnvironment(env)
	return profile, cloneEnvironment(env), backend, nil
}

func cloneProfile(profile Profile) Profile {
	profile.Environment = cloneEnvironment(profile.Environment)
	if profile.Modules.FS != nil {
		v := *profile.Modules.FS
		profile.Modules.FS = &v
	}
	if profile.Modules.Commands != nil {
		v := *profile.Modules.Commands
		profile.Modules.Commands = &v
	}
	if profile.Modules.DNS != nil {
		v := *profile.Modules.DNS
		v.Servers = append([]string(nil), v.Servers...)
		profile.Modules.DNS = &v
	}
	if profile.Modules.Fetch != nil {
		v := *profile.Modules.Fetch
		profile.Modules.Fetch = &v
	}
	if profile.Modules.Serve != nil {
		v := *profile.Modules.Serve
		profile.Modules.Serve = &v
	}
	if profile.Modules.WebSockets != nil {
		v := *profile.Modules.WebSockets
		if v.TLS != nil {
			v.TLS = v.TLS.Clone()
		}
		profile.Modules.WebSockets = &v
	}
	if profile.Modules.Workers != nil {
		v := *profile.Modules.Workers
		profile.Modules.Workers = &v
	}
	if profile.Modules.OS != nil {
		v := *profile.Modules.OS
		profile.Modules.OS = &v
	}
	return profile
}

func cloneEnvironment(env Environment) Environment {
	env.Args = cloneArgs(env.Args)
	env.Env = cloneMap(env.Env)
	return env
}

func cloneArgs(src []string) []string {
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

func cloneMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// Runtime returns the attached runtime. QuickJS access belongs to its owner.
func (b *Bindings) Runtime() *quickjs.Runtime { return b.rt }

// Loop returns the attached runtime's loop.
func (b *Bindings) Loop() *lower.Loop { return b.loop }

// Context returns the execution lifetime context.
func (b *Bindings) Context() context.Context { return b.ctx }

// Environment returns a copy of the execution snapshot; stdio interfaces are
// borrowed and therefore retain their identity.
func (b *Bindings) Environment() Environment { return cloneEnvironment(b.env) }

// FileSystem returns the backend selected for this execution, or nil when FS
// was not selected.
func (b *Bindings) FileSystem() FileSystem { return b.fs }

// Worker reports whether this binding belongs to a worker runtime.
func (b *Bindings) Worker() bool { return b.worker }

// DefineModule registers owner-provided JavaScript source without replacing
// the Profile loader. Native modules continue to take normal precedence.
func (b *Bindings) DefineModule(specifier, source string) error {
	if strings.TrimSpace(specifier) == "" {
		return errors.New("stdlib: module specifier is empty")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errors.New("stdlib: bindings are closed")
	}
	b.modules[specifier] = source
	return nil
}

// Prepare creates this execution's loader and returns a normalized entry for
// the caller to evaluate. The factory runs on the runtime owner.
func (b *Bindings) Prepare(source Source) (Source, error) {
	return b.prepare(source, false)
}

func (b *Bindings) prepare(source Source, includeEntry bool) (Source, error) {
	if source.Name == "" {
		return Source{}, errors.New("stdlib: source name is required")
	}
	normalized, err := normalizeSource(source, b.env, b.backend)
	if err != nil {
		return Source{}, err
	}
	sourceSnapshot := normalized
	origin := Origin{Source: &sourceSnapshot, Entry: normalized.Name, Cwd: b.env.Cwd, Virtual: source.Virtual, Worker: b.worker, FileSystem: b.fs}
	var entry *Source
	if includeEntry {
		entry = &normalized
	}
	if err := b.installLoader(origin, entry); err != nil {
		return Source{}, err
	}
	return normalized, nil
}

func (b *Bindings) installLoader(origin Origin, entry *Source) error {
	b.mu.Lock()
	closed, prepared, hasModules := b.closed, b.prepared, len(b.modules) > 0
	b.mu.Unlock()
	if closed {
		return errors.New("stdlib: bindings are closed")
	}
	if prepared {
		return errors.New("stdlib: an entry was already prepared")
	}
	if err := b.ctx.Err(); err != nil {
		return err
	}
	var loader quickjs.ModuleLoader
	var err error
	if b.loader != nil {
		loader, err = b.loader(b.ctx, origin)
		if err != nil {
			return err
		}
		if loader == nil {
			return errors.New("stdlib: loader factory returned nil")
		}
	}
	b.load = func(specifier, referrer string) (string, string, error) {
		if err := b.ctx.Err(); err != nil {
			return "", "", err
		}
		if entry != nil && specifier == entry.Name {
			return entry.Code, entry.Name, nil
		}
		b.mu.Lock()
		module, ok := b.modules[specifier]
		b.mu.Unlock()
		if ok {
			return module, specifier, nil
		}
		if loader == nil {
			return "", "", fmt.Errorf("stdlib: no module loader for %q from %q", specifier, referrer)
		}
		// Virtual entry IDs are kept in stacks; their imports use the fixed
		// environment cwd instead of pretending the ID is a disk file.
		if origin.Virtual && referrer == origin.Entry {
			referrer = filepath.Join(origin.Cwd, "<virtual-entry>.mjs")
		}
		return loader(specifier, referrer)
	}
	// Advanced Attach can preserve a loader installed directly by the caller.
	if b.loader != nil || hasModules || entry != nil {
		b.rt.SetModuleLoader(b.load)
	}
	b.mu.Lock()
	b.prepared = true
	b.mu.Unlock()
	return nil
}

// LoadModule resolves a source through this execution's prepared loader. It
// runs on the runtime owner and returns Go text/IDs, never a shared JS value.
func (b *Bindings) LoadModule(specifier, referrer string) (source, resolved string, err error) {
	if b.load == nil {
		return "", "", errors.New("stdlib: entry is not prepared")
	}
	return b.load(specifier, referrer)
}

func (b *Bindings) prepareWorker(specifier string, eval bool, requestedType string) (source, name string, module bool, err error) {
	if eval {
		module = requestedType == "module" || requestedType != "classic" && b.rt.CheckSyntax(specifier) != nil && b.rt.CheckModuleSyntax(specifier) == nil
		entry, err := b.prepare(Source{Name: "[worker eval]", Code: specifier, Virtual: true, Module: module}, false)
		return entry.Code, entry.Name, entry.Module, err
	}
	origin := Origin{Entry: specifier, Cwd: b.env.Cwd, Worker: true, FileSystem: b.fs}
	if err := b.installLoader(origin, nil); err != nil {
		return "", "", false, err
	}
	source, name, err = b.LoadModule(specifier, filepath.Join(b.env.Cwd, "<worker-entry>.mjs"))
	if err != nil {
		return "", "", false, err
	}
	return source, name, requestedType != "classic", nil
}

func normalizeSource(source Source, env Environment, backend *filesystem.Backend) (Source, error) {
	if source.Virtual {
		return source, nil
	}
	if backend != nil && backend.Root() != "" {
		name := filepath.ToSlash(source.Name)
		if !strings.HasPrefix(name, "/") {
			name = path.Join(env.Cwd, name)
		}
		name = path.Clean("/" + strings.TrimPrefix(name, "/"))
		if _, err := backend.Resolve(name, true); err != nil {
			return Source{}, fmt.Errorf("stdlib: source path: %w", err)
		}
		source.Name = name
		return source, nil
	}
	if !filepath.IsAbs(source.Name) {
		source.Name = filepath.Join(env.Cwd, source.Name)
	}
	name, err := filepath.Abs(filepath.Clean(source.Name))
	if err != nil {
		return Source{}, err
	}
	source.Name = name
	return source, nil
}

// Close releases this binding's initializer and adapters. It never closes the
// borrowed runtime, loop, stdio, or filesystem backend.
func (b *Bindings) Close() error {
	b.mu.Lock()
	if b.closed {
		err := b.closeErr
		b.mu.Unlock()
		return err
	}
	b.closed = true
	cleanup, adapter, cancel := b.cleanup, b.adapter, b.cancel
	b.cleanup, b.adapter, b.cancel = nil, nil, nil
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if b.stopParent != nil {
		b.stopParent()
	}
	if cleanup != nil {
		cleanup()
	}
	if b.scope != nil {
		b.scope.Close()
	}
	if adapter != nil {
		adapter()
	}
	var err error
	if b.io != nil {
		err = b.io.Close()
	}
	b.mu.Lock()
	b.closeErr = err
	b.mu.Unlock()
	return err
}

func (b *Bindings) fail(err error) {
	if err == nil {
		return
	}
	b.mu.Lock()
	b.failures = append(b.failures, err)
	cancel := b.cancel
	b.mu.Unlock()
	lower.FailExecution(b.loop, err)
	if cancel != nil {
		cancel()
	}
}
func (b *Bindings) unhandled(value quickjs.Value) {
	b.fail(b.snapshotUnhandled(value))
}
func (b *Bindings) requestExit(code int) {
	b.exitRequested, b.exitCode = true, code
	if process, err := b.rt.Get("process"); err == nil && process.IsObject() {
		_ = process.Set("exitCode", code)
	}
	b.mu.Lock()
	cancel := b.cancel
	b.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (b *Bindings) failure() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return errors.Join(b.failures...)
}

// Snapshot freezes mutable profile settings and creation-time filesystem paths
// without creating a VM or invoking user initialization. Streams stay borrowed.
func Snapshot(profile Profile) (Profile, error) {
	input, output, diagnostics := profile.Environment.Stdin, profile.Environment.Stdout, profile.Environment.Stderr
	frozen, _, _, err := normalize(profile)
	if err != nil {
		return Profile{}, err
	}
	frozen.Environment.Stdin, frozen.Environment.Stdout, frozen.Environment.Stderr = input, output, diagnostics
	return frozen, nil
}
