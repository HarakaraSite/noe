# Embed NOE's standard library in Go

Import `forge.harakara.site/littleisland/noe/v2/stdlib` to install generic NOE
APIs into a caller-owned [go-quickjs](https://github.com/go-quickjs/go-quickjs)
runtime. The package does not install the NOE CLI, agent, authentication, or
provider integrations. Agent embedding will be planned separately.

The source module uses the `/v2` path. A v2 tag has **not been published** as part
of this implementation. Existing v1 binaries remain usable; their tags are not
Go library versions for this path.

## Try the example before publication

Use Go 1.27.2 or later. The engine version tested here is go-quickjs v0.27.0.

```sh
CGO_ENABLED=0 go run ./examples/embedding
```

For a separate application, copy [main.go](../examples/embedding/main.go) and
[go.mod.template](../examples/embedding/go.mod.template) into a directory outside
the checkout. Rename the template to `go.mod` and set its `replace` destination
to your absolute checkout path, then run `GOWORK=off go mod tidy` and
`GOWORK=off CGO_ENABLED=0 go run .`. The placeholder version is only for local
development. There is no need to import an `internal` package.

After a v2 tag is separately published, remove the replacement, require that
actual version, and run:

```sh
go get forge.harakara.site/littleisland/noe/v2/stdlib@<published-v2-tag>
go run .
go list -m forge.harakara.site/littleisland/noe/v2 github.com/go-quickjs/go-quickjs
```

Go's minimum version selection can select a higher engine version in the
consumer. Compatibility with an arbitrary newer engine is not established by
the v0.27.0 checks. Record the versions selected by your application.

## Choose capabilities explicitly

```go
rt := quickjs.New()
defer rt.Close()
_, err := stdlib.Install(rt, stdlib.Config{
    Core: stdlib.CoreConfig{NoWebAPIs: true},
    Docs: true,
})
if err != nil {
    return err
}
```

This partial configuration adds the English `noe:docs` reference and pure core
modules without native IO or Web APIs. Docs describes the complete NOE catalog,
including capabilities this host has not installed; it is not introspection of
the host's permissions. Nil FS, Fetch, DNS, Run, Serve, Sockets, and Workers
settings omit those capabilities. A nil Loop omits timers and asynchronous
core scheduling; construct a Loop with `NewLoop`, rather than using its zero
value, when your configuration needs one.

`CoreConfig`, `FS`, `Process`, `OSInfo`, `Fetch`, `DNS`, `Resolver`, `Run`,
`Serve`, `WebSockets`, `Workers`, and `Loop` are aliases of NOE's core types.
Their exported fields, callback signatures, method sets, and type identities
are part of the public Go contract. Individual internal installers are not
public entry points.

A non-nil `IO` opts into **all five** native modules: `noe:io`, `noe:process`,
`noe:fs-handle`, `noe:sockets`, and `noe:signals`. It requires Web APIs and a
non-nil `Core.Process`. This group runs with the host user's OS authority;
`Core.FS` and `Core.Fetch` restrictions do not constrain native IO. `IO.Cwd`
sets native path resolution, while spawn's default cwd and environment come
from JavaScript `process.cwd()` and `process.env`. Supply these deliberately;
installation does not implicitly copy the host environment or grant process
exit authority. The example sets both cwd values to the same directory.

## Drive and close the runtime

The caller owns the runtime and Loop. Evaluate JavaScript, manipulate JavaScript
values, run the Loop, and close the runtime on the runtime's owner goroutine.
Pass a bounded context to CPU evaluation (`EvalContext` or the corresponding
engine method), and use `AbortOn` as in the example. Also pass a context to
`Loop.Run`. A context passed only to the Loop cannot interrupt JavaScript that
was evaluated without it.

The Loop drives core timers and async work as well as engine jobs. Awaiting a
Promise and driving the Loop are different operations. `Loop.Run` continues
while resources keep it alive: close servers, workers, streams, and intervals
when a finite task is finished. Then check its error and the Promise result.
A bounded context provides an upper limit, not a substitute for normal cleanup.

`Post`, `Begin`, `Done`, `Pending`, `Context`, and `Close` may be used from
other Go goroutines under their documented contracts. Balance each `Begin`
with `Done`; `Post` runs its function on the runtime owner. `Run` and `RunUntil`
belong on that owner. A Run context is a drive deadline, whereas `Loop.Context`
tracks the Loop/runtime lifetime. `Loop.Close()` alone does not close the
runtime or all native resources: always call `rt.Close()` too.

The configuration is copied where necessary for installation and may be reused
for another fresh runtime. Clients, resolvers, IO destinations, and callbacks
remain borrowed. This does not make concurrent mutation of those resources
safe. Installation is not transactional; if it fails after registration starts,
close and discard that runtime rather than retrying on the partial instance.

## IO, callbacks, and Workers

Set `Core.Stdout` and `Core.Stderr` to select the console and native IO
destinations. A non-nil `Process.Stdout` or `Process.Stderr` selects that process
stream separately; nil process destinations use the corresponding Core sink.
Passing the same sink shares a serialized output boundary. `IO.Stdin` supplies
the native stream, while `Process.Stdin` supplies the core process input.

`Bindings.Stdout` and `Bindings.Stderr` implement `Output`, which provides
`Write` and `WriteContext`. They are runtime-owned adapters borrowed by the
caller. Closing a runtime releases its adapters and native resources but does
not close the original input or output destinations. The public contract does
not include a concrete output type, a `Close` method, or internal error
identities.

Borrowed `ContextReader` and `ContextWriter` implementations should cooperate
with context cancellation and must return when cancelled. Ordinary blocking
readers and writers that cannot be interrupted can leave their own operation
blocked; the library cannot invent cancellation for the underlying resource.

Workers run on separate runtime owner goroutines. `Workers.New`, `Load`, and
`Installed` execute as part of child setup on the child owner; do not use parent
JavaScript values in them. Each child and grandchild receives the configured
extras once, the original Installed callback once, its own native resources,
and shared borrowed root output. Stdin is not inherited. Child shutdown does
not close root output or the original sink. Other callbacks, such as HTTP or
resolver/client operations, can run on IO goroutines: do not assume every Go
callback executes on the parent owner. Use the callback's specific contract
and Loop.Post for work that must touch the parent runtime.

## Verify a separate consumer

The normal unit gate explicitly copies the portable fixture outside the
checkout and compiles/runs it as a different module:

```sh
CGO_ENABLED=0 go test ./stdlib -count=1 -timeout=300s
python3 scripts/check-stdlib-consumer.py --output /absolute/path/to/check-results
```

The helper also runs the reference-host flow and checks a local module proxy
artifact without `replace`, in an isolated module cache. Its synthetic version
never creates a tag or writes to a remote proxy. Dependencies retain normal
checksum verification; only the synthetic root module is exempted. It checks
distribution layout, not successful download from a published server. The
[release checklist](releases/release-checkpoints.md) requires the latter after
an actual authorized v2 publication.
