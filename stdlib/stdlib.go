// Package stdlib provides managed JavaScript execution and explicit host
// capabilities through Profile. NewRuntime and Execute own their VM and Loop;
// advanced Attach borrows a caller-owned VM and matching Loop. JavaScript values,
// evaluation, Run, and Close stay on the owner goroutine. Cancel and Loop.Post
// may be called from other goroutines. Borrowed I/O and clients remain owned by
// the caller. The tested engine version is go-quickjs v0.27.0.
package stdlib

import (
	"context"
	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	core "forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	"io"
)

// Loop runs JavaScript jobs, timers, and host operations. Create one with
// [NewLoop]; its zero value is not usable.
type Loop = core.Loop

// Resolver supplies DNS lookups for DNSOptions.
type Resolver = core.Resolver

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

// NewLoop creates a loop for rt. The caller owns both the loop and runtime;
// close the loop with the runtime, for example with rt.OnClose(loop.Close).
func NewLoop(rt *quickjs.Runtime) *Loop { return core.NewLoop(rt) }

// ExitCode returns the value assigned to process.exitCode and whether one was
// set. It does not decide how the host should handle that code.
func ExitCode(rt *quickjs.Runtime) (int, bool) { return core.ExitCode(rt) }
