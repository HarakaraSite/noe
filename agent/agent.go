// Package agent provides managed agents through a Go Host and scoped Clients.
// Tasks and tools execute in fresh in-process stdlib runtimes. Attach borrows a
// Host and installs a Client in an existing managed or advanced runtime.
// Runtime values and loops remain confined to their owner goroutine.
package agent

import (
	"context"
	"forge.harakara.site/littleisland/noe/v2/internal/agentbridge"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

// Event contains structured lifecycle, activity, and usage fields. Observe
// callbacks are serialized; copy an event before retaining it asynchronously.
type Event = events.Event

// Binding owns the Go-side agent lifetime installed on a runtime. Do not copy
// a Binding or use its zero value. Runtime and loop ownership stays with the
// caller. Once closed, a binding cannot be restarted or reinstalled on that
// runtime; create a fresh runtime for a new binding.
type Binding struct {
	inner *agentbridge.Binding
}

// Context returns the binding's operation context. It is cancelled by the
// supplied context, binding Close, or runtime shutdown.
func (b *Binding) Context() context.Context { return b.inner.Context() }

// Close cancels agent requests, hook scopes, owned tasks, and owned executions,
// and joins Go work. It is idempotent and may run concurrently with other Go
// calls to Close. It never waits for JavaScript callbacks to be driven and
// never closes the caller's runtime, loop, or original sinks. Custom providers
// and HTTP clients must honor cancellation for cleanup to complete.
func (b *Binding) Close() { b.inner.Close() }
