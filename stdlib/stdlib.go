// Package stdlib provides NOE's embeddable JavaScript standard library.
//
// A host creates and owns a go-quickjs runtime, chooses the capabilities to
// install, and closes the runtime when evaluation is done. The zero Config
// grants no filesystem, network, process, or native I/O access. Pure language
// and web APIs are installed unless Core.NoWebAPIs is set.
//
// Core holds the standard-library configuration. IO opts into NOE's native
// file, network, process, and signal modules as a group; it requires
// Core.Process and the web APIs. Docs installs the complete NOE API catalog,
// independently of which capabilities are enabled. In particular, docs
// describe the catalog and do not report which capabilities this runtime has.
//
// A host that needs asynchronous timers or I/O creates a Loop for its runtime
// and passes it through Core. Runtime evaluation, JavaScript value access,
// Loop.Run, and runtime.Close belong on the same goroutine. Loop.Post, Begin,
// Done, and Context support host work performed on other goroutines. A loop
// and runtime are both owned by the caller; close the loop with the runtime,
// for example with rt.OnClose(loop.Close), then close the runtime explicitly.
// Run drives pending jobs, timers, and host operations until none remain or
// its context ends. Awaiting a Promise and driving the loop are separate
// operations.
//
// Install returns borrowed output bindings shared by the runtime's console,
// process, native I/O, and workers. The caller retains ownership of the
// original readers, writers, resolver, HTTP client, callbacks, and runtime.
// Runtime.Close releases adapters created by Install, but does not close the
// original sinks. Worker callbacks run on each worker's own runtime owner;
// other host callbacks run where their API documents them.
//
// A partial installation can be as small as a console, timers, or Docs. For
// example:
//
//	loop := stdlib.NewLoop(rt)
//	rt.OnClose(loop.Close)
//	defer rt.Close()
//	bindings, err := stdlib.Install(rt, stdlib.Config{
//		Core: stdlib.CoreConfig{Loop: loop, Stdout: os.Stdout},
//		Docs: true,
//	})
//	if err != nil {
//		return err
//	}
//	_ = bindings
//	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
//	defer cancel()
//	if _, err := rt.EvalContext(ctx, "console.log('ready')"); err != nil {
//		return err
//	}
//	return loop.Run(ctx)
//
// Install is not transactional. If it returns an error, close and discard the
// runtime rather than trying to reuse a partially configured one.
package stdlib

import (
	"context"
	"io"

	"forge.harakara.site/littleisland/noe/v2/internal/noestdlib"
	core "forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// Config selects the core library, optional native I/O, and optional API docs
// installed on a runtime. The zero value grants no host capabilities.
type Config struct {
	// Core configures the standard JavaScript and Node-compatible modules.
	Core CoreConfig
	// IO opts into NOE's native I/O modules. It requires Core.Process and the
	// web APIs and is not inherited by worker stdin.
	IO *IOConfig
	// Docs installs the full NOE API catalog. It does not indicate which
	// capabilities are enabled on this runtime.
	Docs bool
}

// CoreConfig configures the core standard-library modules. It is an alias so
// its fields and callback types stay identical to the implementation types.
type CoreConfig = core.Config

// IOConfig opts into NOE's native I/O modules. Stdin and the output sinks are
// borrowed; closing the runtime does not close them. Cwd is the base directory
// for relative native filesystem paths.
type IOConfig struct {
	Stdin io.Reader
	Cwd   string
}

// Loop runs JavaScript jobs, timers, and host operations. Create one with
// [NewLoop]; its zero value is not usable.
type Loop = core.Loop

// FS configures filesystem access.
type FS = core.FS

// Process configures the process object and child-process environment.
type Process = core.Process

// OSInfo configures information exposed by the os module.
type OSInfo = core.OSInfo

// Fetch configures outbound HTTP requests.
type Fetch = core.Fetch

// DNS configures name resolution.
type DNS = core.DNS

// Resolver is the DNS interface used by [DNS].
type Resolver = core.Resolver

// Run configures programs scripts may start.
type Run = core.Run

// Serve configures HTTP listeners scripts may open.
type Serve = core.Serve

// WebSockets configures WebSocket connections scripts may open or accept.
type WebSockets = core.WebSockets

// Workers configures worker runtimes, source loading, and worker callbacks.
type Workers = core.Workers

// ContextReader reads from a borrowed source with cancellation. Implementations
// must stop an in-flight read when ctx is cancelled and must not close the
// borrowed source as a consequence of cancellation.
type ContextReader interface {
	ReadContext(context.Context, []byte) (int, error)
}

// ContextWriter writes to a borrowed destination with cancellation.
// Implementations must stop an in-flight write when ctx is cancelled.
type ContextWriter interface {
	WriteContext(context.Context, []byte) (int, error)
}

// Output is a serialized, runtime-owned adapter for a borrowed destination.
// It supports ordinary writes and writes that can stop when a context is
// cancelled. It deliberately does not expose Close.
type Output interface {
	io.Writer
	ContextWriter
}

// Bindings are the normalized output sinks shared by the installed APIs.
// The values are owned by the runtime; the original writers remain borrowed.
type Bindings struct {
	Stdout Output
	Stderr Output
}

// NewLoop creates a loop for rt. The caller owns both the loop and runtime;
// close the loop with the runtime, for example with rt.OnClose(loop.Close).
func NewLoop(rt *quickjs.Runtime) *Loop { return core.NewLoop(rt) }

// Install installs the capabilities selected in cfg on rt and returns
// normalized output bindings. Install borrows rt and every resource in cfg;
// closing rt releases its adapters without closing the caller's writers.
//
// Installation may make partial changes before returning an error. Close and
// discard rt after an error rather than attempting another installation.
func Install(rt *quickjs.Runtime, cfg Config) (Bindings, error) {
	var ioConfig *noestdlib.IOConfig
	if cfg.IO != nil {
		ioConfig = &noestdlib.IOConfig{Stdin: cfg.IO.Stdin, Cwd: cfg.IO.Cwd}
	}
	bindings, err := noestdlib.Install(rt, noestdlib.Config{
		Core: cfg.Core,
		IO:   ioConfig,
		Docs: cfg.Docs,
	})
	if err != nil {
		return Bindings{}, err
	}
	return Bindings{Stdout: bindings.Stdout, Stderr: bindings.Stderr}, nil
}

// ExitCode returns the value assigned to process.exitCode and whether one was
// set. It does not decide how the host should handle that code.
func ExitCode(rt *quickjs.Runtime) (int, bool) { return core.ExitCode(rt) }
