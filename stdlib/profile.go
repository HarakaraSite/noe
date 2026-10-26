package stdlib

import (
	"context"
	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
	"forge.harakara.site/littleisland/noe/v2/internal/jsexec"
	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// Environment is the single runtime environment shared by all selected
// modules. Args and Env are snapshotted by Attach; streams are borrowed.
type Environment = jsexec.Environment

// Profile selects one execution's environment, capabilities, loader, and
// owner-thread initializer.
type Profile = jsexec.Profile

// Modules independently selects the runtime capabilities.
type Modules = jsexec.Modules

type FileSystemOptions = jsexec.FileSystemOptions
type CommandOptions = jsexec.CommandOptions
type DNSOptions = jsexec.DNSOptions
type FetchOptions = jsexec.FetchOptions
type ServeOptions = jsexec.ServeOptions
type WebSocketsOptions = jsexec.WebSocketsOptions
type WorkerOptions = jsexec.WorkerOptions
type OSOptions = jsexec.OSOptions

// Source is an entry's source text and identifier. Virtual source identifiers
// are preserved and resolve relative imports against the profile cwd.
type Source = jsexec.Source

// Origin describes the entry for which a loader instance is created.
type Origin = jsexec.Origin

type LoaderFactory = jsexec.LoaderFactory
type ModuleLoaderProvider = jsexec.ModuleLoaderProvider
type URLImportSupport = jsexec.URLImportSupport

const (
	URLImportsEnabled  = jsexec.URLImportsEnabled
	URLImportsDisabled = jsexec.URLImportsDisabled
	URLImportsUnknown  = jsexec.URLImportsUnknown
)

// URLImportCapability reads provider metadata; legacy factories report unknown.
func URLImportCapability(profile Profile) URLImportSupport {
	return jsexec.URLImportCapability(profile)
}

type RuntimeInitializer = jsexec.RuntimeInitializer

// FileSystem is the borrowed Go view of the selected execution filesystem.
// It exposes no backend close or reconfiguration method.
type FileSystem = jsexec.FileSystem

// Bindings is the read-only view of one attached execution.
type Bindings = jsexec.Bindings

// Attach installs a Profile into a caller-owned runtime and loop. The caller
// remains responsible for running and closing both objects. If setup fails,
// any resources created by this call are cleaned up; the runtime may have
// partial module registrations and should be discarded.
func Attach(rt *quickjs.Runtime, loop *stdlib.Loop, profile Profile) (*Bindings, error) {
	return jsexec.Attach(rt, loop, profile)
}

// Runtime owns one execution. VM operations and Close stay on the creator's
// goroutine; Cancel is safe from another goroutine.
type Runtime = jsexec.Runtime

// Result is execution data that remains valid after the runtime closes.
type Result = jsexec.Result

// ExecutionDiagnostic is detached, structured information about an execution
// failure. Exception details contain only supported scalar values or encoded
// command bytes; no JavaScript values escape the runtime.
type ExecutionDiagnostic = executionresult.Diagnostic

// DiagnosticPosition is a source location reported by the engine.
type DiagnosticPosition = executionresult.Position

// DiagnosticEncodedBytes represents binary command output in a diagnostic.
type DiagnosticEncodedBytes = executionresult.EncodedBytes

func NewRuntime(ctx context.Context, profile Profile) (*Runtime, error) {
	return jsexec.NewRuntime(ctx, profile)
}
func Execute(ctx context.Context, source Source, profile Profile) (Result, error) {
	return jsexec.Execute(ctx, source, profile)
}

// Diagnostic includes detached JavaScript stacks and command error details.
func Diagnostic(err error) string { return jsexec.Diagnostic(err) }
