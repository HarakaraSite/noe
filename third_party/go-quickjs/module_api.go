package quickjs

import (
	"context"
	"errors"
	"sort"

	"forge.harakara.site/littleisland/noe/third_party/go-quickjs/internal/vm"
)

// ModulePhase identifies the part of module processing that failed.
type ModulePhase string

const (
	ModulePhaseParse    ModulePhase = "parse"
	ModulePhaseCompile  ModulePhase = "compile"
	ModulePhaseResolve  ModulePhase = "resolve"
	ModulePhaseLink     ModulePhase = "link"
	ModulePhaseEvaluate ModulePhase = "evaluate"
)

// ModuleError reports a module failure without hiding the underlying engine
// error or JavaScript exception.
type ModuleError struct {
	Phase  ModulePhase
	Source string
	Err    error
}

func (e *ModuleError) Error() string {
	if e == nil || e.Err == nil {
		return "quickjs: module processing failed"
	}
	return e.Err.Error()
}

// Unwrap returns the engine error or JavaScript exception that caused e.
func (e *ModuleError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ErrModuleOwner reports an attempt to use a prepared module with a different
// Runtime from the one that compiled and linked it.
var ErrModuleOwner = errors.New("quickjs: module belongs to a different runtime")

// Module is a compiled and linked ECMAScript module owned by one Runtime.
// Its JavaScript body has not run until EvaluateContext is called.
type Module struct {
	runtime *Runtime
	module  *vm.Module
}

// ModuleKind identifies how a module entered the Runtime.
type ModuleKind string

const (
	ModuleKindSource    ModuleKind = "source"
	ModuleKindNative    ModuleKind = "native"
	ModuleKindSynthetic ModuleKind = "synthetic"
)

// ModuleMetadata describes the registration name and export names of a
// module. Export names are read from its namespace shape; export values are
// never read.
type ModuleMetadata struct {
	Specifier string
	Kind      ModuleKind
	Exports   []string
}

// StackFrame is one structured JavaScript stack entry captured at a throw.
type StackFrame struct {
	Function string
	Source   string
	Line     int
	Column   int
}

type moduleRegistration struct {
	module *Module
	kind   ModuleKind
}

func (r *Runtime) recordModule(m *vm.Module, kind ModuleKind) *Module {
	if m == nil {
		return nil
	}
	if r.modules == nil {
		r.modules = make(map[string]moduleRegistration)
	}
	prepared := &Module{runtime: r, module: m}
	r.modules[m.Specifier] = moduleRegistration{module: prepared, kind: kind}
	return prepared
}

func (r *Runtime) discoveredModule(m *vm.Module) *Module {
	if m == nil {
		return nil
	}
	if registration, ok := r.modules[m.Specifier]; ok && registration.module.module == m {
		return registration.module
	}
	return r.recordModule(m, ModuleKindSource)
}

func (r *Runtime) moduleFailure(phase ModulePhase, source string, err error) error {
	if err == nil {
		return nil
	}
	var existing *ModuleError
	if errors.As(err, &existing) {
		return existing
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return r.wrapError(err)
	}
	if errors.Is(err, ErrClosed) {
		return err
	}
	var loadErr *vm.ModuleLoadError
	if errors.As(err, &loadErr) {
		phase, source = ModulePhaseResolve, loadErr.Referrer
	}
	return &ModuleError{Phase: phase, Source: source, Err: r.wrapError(err)}
}

// PrepareModuleContext compiles source and links its complete static module
// graph without evaluating any JavaScript module body.
func (r *Runtime) PrepareModuleContext(ctx context.Context, specifier, source string) (_ *Module, err error) {
	if r.closed || r.rt == nil {
		return nil, ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, r.wrapError(ctx.Err())
	}
	defer r.guard(&err)
	_, leave := r.enter(ctx)
	defer leave()

	r.installModuleHooks()
	rt := r.rt
	compiled, err := r.compileAndRegisterModule(specifier, source)
	if err != nil {
		return nil, err
	}
	phase, failedSource, err := rt.LinkWithPhase(compiled)
	if r.closed {
		rt.ReleaseClosed()
		return nil, ErrClosed
	}
	if err != nil {
		return nil, r.moduleFailure(ModulePhase(phase), failedSource, err)
	}
	return r.discoveredModule(compiled), nil
}

// InspectModuleContext resolves a request from referrer, compiles and links
// its static graph, and returns export names without evaluating module bodies.
func (r *Runtime) InspectModuleContext(ctx context.Context, specifier, referrer string) (_ ModuleMetadata, err error) {
	if r.closed || r.rt == nil {
		return ModuleMetadata{}, ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ModuleMetadata{}, r.wrapError(ctx.Err())
	}
	defer r.guard(&err)
	_, leave := r.enter(ctx)
	defer leave()

	r.installModuleHooks()
	rt := r.rt
	compiled, phase, failedSource, err := rt.LoadAndLinkModule(specifier, referrer)
	if r.closed {
		rt.ReleaseClosed()
		return ModuleMetadata{}, ErrClosed
	}
	if err != nil {
		return ModuleMetadata{}, r.moduleFailure(ModulePhase(phase), failedSource, err)
	}
	return r.metadataFor(r.discoveredModule(compiled))
}

// ExportNames returns the names a module exports, without reading any export
// value or invoking a module export getter.
func (m *Module) ExportNames() ([]string, error) {
	if m == nil || m.runtime == nil || m.module == nil {
		return nil, ErrClosed
	}
	r := m.runtime
	if r.closed || r.rt == nil {
		return nil, ErrClosed
	}
	var err error
	defer r.guard(&err)
	names, err := r.rt.ModuleExportNames(m.module)
	if r.closed {
		return nil, ErrClosed
	}
	if err != nil {
		return nil, r.moduleFailure(ModulePhaseLink, m.module.Specifier, err)
	}
	return names, nil
}

// Namespace returns the module namespace object without reading any export
// value. After evaluation, it contains the module's initialized exports.
func (m *Module) Namespace() (Value, error) {
	if m == nil || m.runtime == nil || m.module == nil {
		return Value{}, ErrClosed
	}
	r := m.runtime
	if r.closed || r.rt == nil {
		return Value{}, ErrClosed
	}
	var err error
	defer r.guard(&err)
	ns, err := r.rt.ModuleNamespace(m.module)
	if r.closed {
		return Value{}, ErrClosed
	}
	if err != nil {
		return Value{}, r.moduleFailure(ModulePhaseLink, m.module.Specifier, err)
	}
	return Value{v: vm.Obj(ns), rt: r.rt}, nil
}

func (r *Runtime) metadataFor(m *Module) (ModuleMetadata, error) {
	if m == nil {
		return ModuleMetadata{}, ErrClosed
	}
	registration, ok := r.modules[m.module.Specifier]
	kind := ModuleKindSource
	if ok {
		kind = registration.kind
	}
	exports, err := m.ExportNames()
	if err != nil {
		return ModuleMetadata{}, err
	}
	return ModuleMetadata{Specifier: m.module.Specifier, Kind: kind, Exports: exports}, nil
}

// ModuleMetadata returns names and export names for modules registered or
// compiled by this Runtime. It does not evaluate modules or read export values.
func (r *Runtime) ModuleMetadata() ([]ModuleMetadata, error) {
	if r.closed || r.rt == nil {
		return nil, ErrClosed
	}
	names := make([]string, 0, len(r.modules))
	for name := range r.modules {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ModuleMetadata, 0, len(names))
	for _, name := range names {
		registration := r.modules[name]
		metadata, err := r.metadataFor(registration.module)
		if err != nil {
			return nil, err
		}
		out = append(out, metadata)
	}
	return out, nil
}

func (r *Runtime) evaluatePreparedModule(ctx context.Context, m *Module) (done, namespace Value, err error) {
	if r.closed || r.rt == nil {
		return Value{}, Value{}, ErrClosed
	}
	if m == nil || m.runtime != r {
		return Value{}, Value{}, ErrModuleOwner
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return Value{}, Value{}, r.wrapError(ctx.Err())
	}
	defer r.guard(&err)
	_, leave := r.enter(ctx)
	defer leave()

	rt := r.rt
	promise, err := rt.EvaluateModule(m.module)
	if r.closed {
		rt.ReleaseClosed()
		return Value{}, Value{}, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return Value{}, Value{}, r.wrapError(err)
	}
	if err != nil {
		return Value{}, Value{}, r.moduleFailure(ModulePhaseEvaluate, m.module.Specifier, err)
	}
	ns, err := rt.ModuleNamespace(m.module)
	if r.closed {
		rt.ReleaseClosed()
		return Value{}, Value{}, ErrClosed
	}
	if err != nil {
		return Value{}, Value{}, r.moduleFailure(ModulePhaseLink, m.module.Specifier, err)
	}
	return Value{v: promise, rt: rt}, Value{v: vm.Obj(ns), rt: rt}, nil
}

// EvaluateModuleContext evaluates a prepared module and returns its completion
// Promise, which fulfills with undefined after the full graph, including
// top-level await, has completed. Use Module.Namespace after fulfillment to
// access exports without promise assimilation of a named `then` export.
func (r *Runtime) EvaluateModuleContext(ctx context.Context, m *Module) (Value, error) {
	done, _, err := r.evaluatePreparedModule(ctx, m)
	if err != nil {
		return Value{}, err
	}
	return done, nil
}

// EvaluateContext evaluates this module and returns its completion Promise.
// After it fulfills, use Namespace to read the module's exports.
func (m *Module) EvaluateContext(ctx context.Context) (Value, error) {
	if m == nil || m.runtime == nil {
		return Value{}, ErrClosed
	}
	return m.runtime.EvaluateModuleContext(ctx, m)
}

// Evaluate starts module evaluation with a background context and returns its
// completion Promise.
func (m *Module) Evaluate() (Value, error) {
	return m.EvaluateContext(context.Background())
}

// AwaitContext evaluates this module, waits for the graph including top-level
// await, then returns its namespace. Evaluation failures retain their
// JavaScript exception as the ModuleError cause.
func (m *Module) AwaitContext(ctx context.Context) (Value, error) {
	promise, err := m.EvaluateContext(ctx)
	if err != nil {
		return Value{}, err
	}
	_, err = promise.Await(ctx)
	if err != nil {
		if m == nil || m.runtime == nil {
			return Value{}, err
		}
		return Value{}, m.runtime.moduleFailure(ModulePhaseEvaluate, m.module.Specifier, err)
	}
	return m.Namespace()
}

func moduleSourceError(phase ModulePhase, source string, err error) error {
	if err == nil {
		return nil
	}
	return &ModuleError{Phase: phase, Source: source, Err: err}
}
