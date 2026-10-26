// Package jsexec contains the lower-level, execution-scoped profile and
// binding view used by the public stdlib package.
package jsexec

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	"forge.harakara.site/littleisland/noe/v2/internal/noestdlib"
	lower "forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

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

// bindingsState is the single lifetime gate shared by Bindings and its loader.
// Its mutex also guards cleanup, failures and the close result as before.
type bindingsState struct {
	mu     sync.Mutex
	closed bool
}

// Bindings is a borrowed view of one attached execution. Its methods that
// interact with QuickJS must be called on the runtime owner.
type Bindings struct {
	rt            *quickjs.Runtime
	loop          *lower.Loop
	ctx           context.Context
	env           Environment
	fs            FileSystem
	worker        bool
	loader        executionLoader
	state         bindingsState
	cleanup       func()
	adapter       func()
	cancel        context.CancelFunc
	scope         *lower.ExecutionScope
	io            *executionIO
	stopParent    func() bool
	failures      []error
	exitRequested bool
	exitCode      int
	closeErr      error
	snapshotter   quickjs.Value
	rethrower     quickjs.Value
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
	b := &Bindings{rt: rt, loop: loop, ctx: ctx, env: env, fs: fsview, worker: worker, cancel: cancel}
	factory := profile.NewLoader
	if profile.Loader != nil {
		factory = profile.Loader.NewLoader
	}
	b.loader = executionLoader{
		state: &b.state, rt: rt, ctx: ctx, cwd: env.Cwd, backend: backend,
		fs: fsview, worker: worker, factory: factory, modules: make(map[string]string),
	}
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
	b.rethrower, err = rt.EvalFileContext(ctx, "<stdlib-rethrow>", `(value) => { throw value; }`)
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
	b.installImportMeta()
	if core.Process == nil {
		rt.OnUnhandledRejection(b.unhandled)
	}
	// A direct runtime close also releases this binding's adapters and its
	// initializer. The hook only runs Go cleanup and never touches JS values.
	rt.OnClose(func() { _ = b.Close() })
	if profile.Initialize != nil {
		cleanup, initErr := profile.Initialize(ctx, b)
		b.state.mu.Lock()
		closed := b.state.closed
		if !closed {
			b.cleanup = cleanup
		}
		b.state.mu.Unlock()
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
	return b.loader.defineModule(specifier, source)
}

// installManagedModules registers private runtime modules without replacing
// owner-defined modules.
func (b *Bindings) installManagedModules(helperName, wrapperName, wrapperSource string, helperExports map[string]any) (bool, error) {
	return b.loader.installManagedModules(helperName, wrapperName, wrapperSource, helperExports)
}

// Prepare creates this execution's loader and returns a normalized entry for
// the caller to evaluate. The factory runs on the runtime owner.
func (b *Bindings) Prepare(source Source) (Source, error) {
	return b.prepare(source, false)
}

func (b *Bindings) prepare(source Source, includeEntry bool) (Source, error) {
	return b.loader.prepare(source, includeEntry)
}

// LoadModule resolves a source through this execution's prepared loader. It
// runs on the runtime owner and returns Go text/IDs, never a shared JS value.
func (b *Bindings) LoadModule(specifier, referrer string) (source, resolved string, err error) {
	return b.loader.loadModule(specifier, referrer)
}

func (b *Bindings) prepareWorker(specifier string, eval bool, requestedType string) (source, name string, module bool, err error) {
	return b.loader.prepareWorker(specifier, eval, requestedType)
}

// Close releases this binding's initializer and adapters. It never closes the
// borrowed runtime, loop, stdio, or filesystem backend.
func (b *Bindings) Close() error {
	b.state.mu.Lock()
	if b.state.closed {
		err := b.closeErr
		b.state.mu.Unlock()
		return err
	}
	b.state.closed = true
	cleanup, adapter, cancel := b.cleanup, b.adapter, b.cancel
	b.cleanup, b.adapter, b.cancel = nil, nil, nil
	b.state.mu.Unlock()
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
	b.state.mu.Lock()
	b.closeErr = err
	b.state.mu.Unlock()
	return err
}

func (b *Bindings) fail(err error) {
	if err == nil {
		return
	}
	b.state.mu.Lock()
	b.failures = append(b.failures, err)
	cancel := b.cancel
	b.state.mu.Unlock()
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
	b.state.mu.Lock()
	cancel := b.cancel
	b.state.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (b *Bindings) failure() error {
	b.state.mu.Lock()
	defer b.state.mu.Unlock()
	return errors.Join(b.failures...)
}
