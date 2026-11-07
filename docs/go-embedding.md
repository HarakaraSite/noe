# Embed NOE's standard library in Go

Use `forge.harakara.site/littleisland/noe/v2/stdlib` to run JavaScript with a
per-execution profile. `stdlib` does not create a NOE CLI process and does not
select model providers or agent behavior. The current release is v2.17.0; see the
[v2 migration guide](v2-migration.md) for changes from the previous embedding API.

The current checkout is checked with go-quickjs v0.32.0. Published NOE tags
retain the engine dependency recorded in their own `go.mod`. Use Go 1.27.2
or later. From the repository root, run the example with:

```sh
CGO_ENABLED=0 go run ./examples/embedding
```

For a separate module, copy [the example](../examples/embedding/main.go) and
[module template](../examples/embedding/go.mod.template) outside the checkout,
rename the template to `go.mod`, and set `replace` to the absolute checkout
path. Then run `GOWORK=off go mod tidy` and
`GOWORK=off CGO_ENABLED=0 go run .`. The placeholder version is only for a local
replacement. To use the published release, remove the replacement and use
`go get forge.harakara.site/littleisland/noe/v2/stdlib@v2.17.0`. Downloading that
tag without a replacement is a post-publication check, separate from local
consumer validation.

## Execute a script

`Execute` creates, runs, and closes one runtime. A new runtime has a fresh VM,
module cache, and execution context. The example uses the pure built-in docs
module and writes `console.log` output to a borrowed writer:

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

cwd, err := os.Getwd()
if err != nil {
    return err
}
profile := stdlib.Profile{
    Environment: stdlib.Environment{
        Cwd: cwd, Args: []string{"embedding"}, Env: map[string]string{},
        Stdout: os.Stdout, Stderr: os.Stderr,
    },
}
result, err := stdlib.Execute(ctx, stdlib.Source{
    Name: "[embedding]", Virtual: true, Module: true,
    Code: `import {docs} from "noe:docs"; console.log(docs("noe:docs").split("\n")[0]);`,
}, profile)
if err != nil {
    return fmt.Errorf("run JavaScript: %s: %w", stdlib.Diagnostic(err), err)
}
if result.Code != 0 {
    return fmt.Errorf("JavaScript exited with status %d", result.Code)
}
```

`Source` contains the source name, code, and whether it is a module or a
virtual entry. Virtual entry names are retained for diagnostics; their relative
imports resolve from `Environment.Cwd`. `Result.Code` records `process.exit`
and `process.exitCode`; a JavaScript exception or execution failure is returned
as an error. `Diagnostic(err)` includes a detached JavaScript stack when one is
available.

`noe:docs` is always present and describes the complete catalog. It does not
indicate which optional capabilities this profile selected.

## Select capabilities and one environment

`Profile` combines one `Environment`, independent `Modules`, an optional
structured `Loader` provider or legacy `NewLoader` factory, and an optional
runtime `Initialize` callback. Selecting both loader fields is an error. A nil
capability option disables that capability. The profile snapshots `Args`,
`Env`, and option values when attached; caller-owned readers, writers,
resolvers, clients, providers, and callbacks remain borrowed.

| `Modules` field | Capability |
| --- | --- |
| `ProcessInfo` | Named `noe:process` metadata/control object `process`, identical to the global `process` object |
| `Stdio` | Named `noe:process` standard I/O Web Streams and the selected environment's streams |
| `FS` | `noe:fs`; `FileSystemOptions` selects `Root` and `ReadOnly` |
| `Commands` | `noe:process` captured commands and NOE-specific Web Streams `spawn`; options select `Allow`, timeout, output limit, and optional `Supervisor` |
| `DNS`, `Fetch`, `Serve`, `WebSockets` | The corresponding network capability and its limits/callbacks |
| `RawSockets`, `Signals` | `noe:sockets` and `noe:signals` |
| `Workers`, `OS` | Worker runtimes and selected host-information behavior |
| `Random`, `WindowsPaths` | Random source and path interpretation for the execution |

`ProcessInfo`, `Stdio`, and `Commands` are independent. Any one selects
`noe:process`; its default is a namespace containing the selected named
exports. With ProcessInfo, `import {process} from "noe:process"` returns the
same metadata/control object as the global `process`, also reachable through
the namespace's `process` property. With Stdio alone, the namespace contains
only streams and there is no global `process`. With Commands alone, it
contains only command functions. Selecting ProcessInfo does not select
streams or commands. When Stdio and ProcessInfo are both selected,
`process.stdin`, `process.stdout`, and `process.stderr` are the named Web
Streams. Commands remain namespace exports rather than properties of global
`process`. Workers receive empty stdin (immediate EOF) and share the configured
output destinations. The old `node:process`, `process`, `node:child_process`,
and `child_process` module entrances have been removed.

The same `Environment` supplies cwd, arguments, environment variables, and
standard streams to selected modules. `Env == nil` means an empty environment;
nil stdin is EOF and nil stdout/stderr discard output. The library does not
call `os.Chdir` or `os.Setenv`. Filesystem `Root` scopes file APIs, not the
operating-system authority of allowed commands. A command's default cwd and
environment come from the profile.

`Modules.Commands` defaults to direct process startup. On POSIX, set
`CommandOptions.Supervisor` to an explicit helper executable when host
SIGKILL recovery of a command's descendants is required. A dedicated helper
can call `stdlib.DispatchProcessHelper(os.Args[1:])` before application work;
that avoids running application initialization twice. A self-helper instead
requires every dependency init function to be safe to run again. Other helper
executables can use the same dispatcher. Windows uses Job Objects.

## HTTPS listeners

Available in [v2.17.0](releases/v2.17.0.md) and later. The v2.16.0 module does
not include these options.

The current source adds `ServeOptions.TLS *tls.Config`. An embedding host can
supply certificates/callbacks and TLS policy there without exposing private
keys to JavaScript. It selects HTTPS for the same `serve` function and bind
permission callback; outgoing Fetch and WebSocket trust remain independent.
Configurations are cloned with normal Go immutable-in-use ownership for
keys, pools, and callbacks. Workers inherit the selection with their own
runtime binding. TLS listeners use HTTP/1.1, including WSS upgrades.

```go
profile.Modules.Serve = &stdlib.ServeOptions{
    Allow: func(address string) error { return nil }, // Choose the host's bind policy.
    TLS: &tls.Config{Certificates: []tls.Certificate{certificate}},
}
```

This fragment uses `crypto/tls` and a previously loaded `tls.Certificate`.
With no host TLS configuration, JavaScript may supply nonempty `cert`/`key`
PEM strings; file reads must go through the selected filesystem. JavaScript
PEM options conflict with host TLS rather than overriding its policy.
Use keyed ServeOptions literals when updating callers, since the new field
changes the positional struct shape. See the
[HTTPS contract](development/stdlib-https.md).

## Asynchronous SQLite

`noe:sqlite` is a built-in Promise API in managed runtimes and Workers.
An in-memory database needs no FS capability; a persistent database uses
`Modules.FS` and its existing Root, Cwd and ReadOnly policy. No new public Go
option is added, and the module does not grant filesystem access on its own.

```js
import {open} from "noe:sqlite";
await using db = await open();
await db.exec("CREATE TABLE data(value TEXT)");
await db.run("INSERT INTO data VALUES(?)", ["hello"]);
const row = await db.get("SELECT value FROM data");
```

SQL executes on Go workers rather than the runtime owner. Each connection is
serialized, and callback transactions reserve the connection through commit
or rollback. AbortSignal cancels queries; execution shutdown and Worker
termination cancel, join and close owned database resources. The VM, Loop,
and filesystem backend remain borrowed. Handles cannot be transferred to
another runtime. The selected driver is CGO-free.

See the [API reference](../internal/apidocs/content/noe-sqlite.md) and
[implementation record](development/stdlib-sqlite.md) for transaction-scoped
handles, typed values, supported SQL operations, and filesystem limitations.

## Server-sent events

Selecting `Modules.Fetch` also enables the global `EventSource` constructor,
in main runtimes and Workers. It uses the selected Fetch client and checks
`FetchOptions.Allow` on every attempt and redirect. There is no new public Go
option or separate network grant.

```js
const source = new EventSource("https://example.com/events");
source.onmessage = event => console.log(event.lastEventId, event.data);
// End the subscription explicitly when it is no longer needed.
source.close();
```

The default EventSource client has no lifetime timeout. An explicitly supplied
client retains its timeout, transport, redirect policy, and TLS configuration;
the borrowed client is neither modified nor closed. `withCredentials: true`
uses that client's cookie jar, while false disables the jar for this
subscription. NOE does not implement a browser origin/CORS model or add
provider credentials. Standard EventSource has no headers or POST body option.

`MaxBodyBytes` bounds buffered partial SSE events/lines, with the existing
32 MiB default and negative value for no limit. It does not cap the total
bytes received over a subscription. `close()`, execution close, and Worker
termination cancel requests, reads, and reconnect timers. Low-level
`internal/stdlib.Network` additionally needs WebAPIs and Timers installed to
expose EventSource. See the
[implementation record](development/stdlib-eventsource.md).

## HTTP server lifetime and receive limits

Select `Modules.Serve` for the explicit `serve` export from `noe:http`.
`ServeOptions.MaxBodyBytes == 0` selects 32 MiB, a negative value disables the
limit, and a positive value bounds the actual request bytes consumed. Body
reads/collectors reject overflow with a RangeError carrying
`ERR_HTTP_BODY_TOO_LARGE`; exact-length bodies succeed after EOF is verified.
Uncaught overflow before response headers produces 413; `onError` can override
that response. If response streaming has begun, the transfer fails instead.
Early responses do not automatically drain input or validate an unread body.

```js
import {serve} from "noe:http";

await using server = serve({port: 0}, request => Response.json({url: request.url}));
console.log(server.url);
```

`server.close()` returns the same `Promise<void>` on repeated calls and
`Symbol.asyncDispose` delegates to it. It closes handler admission immediately,
waits off the JS owner for active HTTP requests, then forces remaining
connections after at most five seconds. Await it when completion matters.
A handler can start close and return its Response; waiting for its own close
inside that handler waits on its own completion. Runtime cancellation,
execution cleanup, and Worker termination still force native cleanup
independently. Public Go option fields and dependencies are unchanged.
See the [implementation and migration](development/stdlib-serve-lifecycle.md).

## WebSocket handshake headers

Select `Modules.WebSockets` with a URL `Allow` callback. Its host helper is
`createWebSocket(url, {protocols, headers})` from `noe:http`. It returns the
same global WebSocket type synchronously in CONNECTING state, with normal
events and close behavior. Headers are snapshotted from a record, iterable
of name/value pairs, or Headers instance before authorization/allocation.

```js
import {createWebSocket} from "noe:http";

const socket = createWebSocket("wss://example.com/feed", {
  headers: {Authorization: "Bearer <token>"},
});
```

The standard global constructor remains `new WebSocket(url, protocols?)`;
its old third-argument header extension is removed. The binary default is
now `"blob"`; select `socket.binaryType = "arraybuffer"` for raw buffers.
The same Go WebSockets URL policy, TLS, message limit, handshake timeout,
Worker behavior, and execution cleanup apply to both creation paths.
The handshake manages Host, Connection, Upgrade, Sec-WebSocket negotiation,
and HTTP body-framing fields. Invalid or managed headers throw before
allocation; invalid protocol tokens throw SyntaxError. See the
[current HTTP contract](development/stdlib-noe-http.md).

`Modules.Serve` independently selects `noe:http.serve`; Serve plus WebSockets
also selects `upgradeWebSocket`. Neither helper is installed globally.
Fetch remains independent and global; Fetch alone does not select `noe:http`.
There is no default or fetch export, and `node:http`/`http` are removed.
`FetchOptions.MaxBodyBytes` rejects actual response-body overflow. Exactly the
cap is allowed after EOF; zero means 32 MiB and negative means unlimited.
A reader can receive the allowed prefix before the next read detects excess.
Serve applies an independent request-body cap with the same zero/default and negative/unlimited conventions; see the server lifecycle section above.

## Load local modules

`stdlib.FileLoader(imports)` returns a `LoaderFactory` that creates a fresh
loader for each runtime. The string map maps exact bare specifiers and
slash-terminated prefixes to local file targets; targets are anchored to that execution's cwd and read through
its selected filesystem. The factory snapshots the map. With a nil map,
relative and absolute local file imports still work. To load local files, also
select `Modules.FS`; the loader does not grant filesystem access by itself.

```go
profile := stdlib.Profile{
    Environment: stdlib.Environment{Cwd: projectDir},
    Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{ReadOnly: true}},
    NewLoader: stdlib.FileLoader(map[string]string{
        "app/math": "./lib/math.mjs",
    }),
}
```

The loader and module cache are execution-local. File entry imports resolve
relative to their referrer. A virtual entry's relative import uses the profile
cwd. Root and Worker executions receive fresh loaders; the factory can be
called concurrently and must not share QuickJS values between runtimes.

JSON, text, and bytes imports use `with {type: "json"}`, `"text"`, or
`"bytes"`; dynamic imports use `{with: {type: "json"}}`. Source bytes are
preserved and interpretation belongs to the engine. Filenames have no suffix
restriction. A plain import still executes JavaScript, without TypeScript
conversion or CommonJS support.

Use `ModuleLoader(LoaderOptions{Imports: ..., Scopes: ...})` for referrer
scopes. Scope paths and targets are relative to the execution cwd; directory
scope keys end in `/`, while other keys match an exact referrer. The provider
copies both levels of Scopes at construction. An HTTP target anywhere in a
map requires a non-nil HTTP option. Rooted filesystem execution paths are
used for local scopes, and virtual entries use `<virtual-entry>.mjs` under
cwd. See [import-map resolution and data examples](import-map.md).

`LoaderOptions.Scopes` is a new public field. Existing keyed Go literals
remain valid; callers using unkeyed literals must switch to field names.

## Module metadata

NOE v2.8.1 and later supply `import.meta.url` for each module.
File modules use encoded file URLs; URL modules retain their final canonical
URL. A `Source.Virtual` entry uses a synthetic `<virtual-entry>.mjs` file URL
under the profile cwd, without changing its diagnostic name or requiring that
file to exist. Rooted filesystems expose their execution paths, not host roots.

The default `OnImportMeta` hook is installed before `Profile.Initialize`.
Hosts may replace it in Initialize or after Attach; Prepare does not overwrite
the host hook. See the [metadata contract](import-meta.md), including custom
loader IDs and Worker behavior.

## Opt in to HTTP/HTTPS module imports

v2.8.0 adds `Profile.Loader` and `stdlib.ModuleLoader`. Existing
`NewLoader: stdlib.FileLoader(...)` remains local-only; selecting Fetch does
not implicitly grant URL import, and selecting a URL loader does not grant
FS or JavaScript fetch.

```go
profile := stdlib.Profile{
    Environment: stdlib.Environment{Cwd: projectDir},
    Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{ReadOnly: true}},
    Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{
        Imports: map[string]string{
            "app/local": "./lib/local.mjs",
            "lodash": "https://esm.sh/lodash-es@4.17.21",
        },
        HTTP: &stdlib.ModuleHTTPOptions{
            Client: &http.Client{Timeout: 30 * time.Second},
        },
    }),
}
```

`HTTP == nil` disables URL imports; a non-nil HTTP option enables HTTP/HTTPS.
With no client, the standard provider uses a 30-second timeout.
`ModuleHTTPOptions.Allow` checks the initial request and each redirect;
the borrowed client's CheckRedirect is also respected. `MaxBodyBytes == 0`
means 32 MiB of decoded response body, a negative value means unlimited.
Clients and callbacks remain borrowed and must support the intended
concurrency. Maps and option values are copied; module sources are retained
only within each runtime. No cross-runtime download cache is added.

The standard loader passes HTTP response-body bytes unchanged to the engine.
Content-Type and charset do not select or restrict the import type; missing
or malformed MIME metadata is accepted. A plain import executes JavaScript,
while explicit import attributes select JSON, text, or bytes. URL suffixes
are unrestricted. There is no UTF-8 validation, BOM removal, charset conversion,
TypeScript/JSX conversion, or npm lookup. Transport decompression still
applies, so bytes reflects `http.Response.Body`, not compressed wire bytes.
Text retains its leading BOM, and BOM-prefixed JSON fails the pinned engine's
parser. See [HTTP source interpretation](url-import.md#http-source-interpretation)
for the compatibility changes from earlier MIME/UTF-8/BOM handling.
Remote relative dependencies use the final response URL. Standard Worker
entries remain local, including URL aliases being rejected before an entry
request. See [URL import](url-import.md) for identity, redirects, and limits.

`ModuleLoaderProvider` supplies both `NewLoader(context.Context, Origin)`
and `URLImports() URLImportSupport`. Providers are borrowed by Profile
snapshots. `stdlib.URLImportCapability(profile)` returns enabled, disabled,
or unknown from explicit metadata: legacy NewLoader functions are unknown,
without inspecting their identity, and no loader is disabled. The legacy
FileLoader still rejects HTTP even though an opaque function cannot report
that fact through this metadata API. Custom providers own their metadata's
accuracy and any I/O they implement.

Module downloads are synchronous: JavaScript jobs and timers in the same
runtime do not progress until a load returns. Cancellation must come through
the host execution context or Runtime.Cancel rather than a timer in that
runtime. The selected code executes with the profile's capabilities, not in
an origin sandbox.

## Runtime ownership and cancellation

Use `NewRuntime` when the application needs to retain a runtime across setup
and execution. `Runtime.Run` accepts one entry; create a new runtime for another
entry. Create and use the runtime, access JavaScript values, and call
`Runtime.Close` on its owner goroutine. `Runtime.Cancel` is safe from another
goroutine and cancels evaluation and owned work. `Close` joins work, runs
initializer cleanup, and closes the runtime. `Execute` combines these steps.

Callbacks that perform I/O should honor their context. The stdio adapters
support known in-memory readers/writers, `*os.File`, `io.Discard`, and custom
`ContextReader` or `ContextWriter` implementations. Other custom I/O values
return an unsupported-I/O error when used; they are not placed in background
goroutines to simulate cancellation. Sources and destinations remain borrowed
and are not closed by the runtime.

`Profile.Initialize` runs on the owner after standard modules and `Bindings`
exist, but before the entry executes. It can install independent bindings such
as model, auth, log, or agent APIs and return cleanup. The callback is invoked
for each runtime, including Workers that the selected profile creates. It must
not share QuickJS values across executions. Cleanup runs during execution
close; it must not wait for the owner to process additional JavaScript work.

For advanced integration, `stdlib.Attach(rt, loop, profile)` attaches the same
profile to a caller-owned QuickJS runtime and matching `stdlib.Loop`. The
returned `Bindings` is a borrowed view of that execution. Its `Runtime`,
`Loop`, `Context`, `Environment`, `FileSystem`, and `Worker` methods expose the
execution boundary; `Prepare` creates that entry's loader and normalized
source. `Bindings.Close` cancels and joins its own adapters and initializer,
but does not close the caller's runtime or loop. The caller must evaluate and
drive the loop on the runtime owner and close both objects.

## Run a resident Go host

The [resident HTTP example](../examples/embedding-server/README.md) keeps the Go
host running and calls `Execute` with a fresh profile and output buffers for each
request. It passes request cancellation and deadlines to JavaScript, cancels
active executions on SIGINT/SIGTERM, and waits for handler cleanup during HTTP
shutdown. Its README includes foreground execution and Linux systemd user-service
setup. No agent Host or external service is required.

## Migrate from the previous embedding API

Replace `stdlib.Install(rt, Config{Core, IO, Docs})` with a `Profile` passed to
`stdlib.Execute` or `stdlib.NewRuntime`. Move cwd, args, environment, and stdio
into the single `Environment`; select each native capability through
`Modules`. `noe:docs` no longer needs a `Docs` flag. Replace `agent.Install`
with `agent.NewHost` and a Go `Client`, or call `agent.Attach` from the runtime
initializer to expose the same host to JavaScript. Do not carry over the old
executable, connection, or combined-IO settings.

## Structured execution results

Managed module execution also returns `Result.Data` (detached JSON, nil for no default export) and `Result.Diagnostics`. Explicit null is JSON `null`, not absence. Existing Code/error handling and `Diagnostic(err)` remain available. Plain-script completion and Worker entry execution keep their existing behavior. The [result and migration guide](execution-results.md) describes the S4 development contract, cancellation, JSON conversion, and separate diagnostics.

## Source maps in diagnostics

Since v2.15.0, `NewRuntime` and `Execute` enable source maps by
default. Runtime Error stacks use original file/function names and positions
from inline application/json data URLs or readable local sidecars named by
`sourceMappingURL`. The selected FS capability controls sidecar access, including
Root and ReadOnly. No remote map or original source file is fetched. Missing,
invalid, or unreadable maps fall back to generated-code positions. Structured
`Result.Diagnostics[].Stack` and `Diagnostic(err)` retain the mapped stack after
the VM closes. Custom `Error.prepareStackTrace` retains generated CallSites;
syntax-error Position remains the generated position supplied by the engine.

`Attach` preserves the engine options of the caller-owned VM. To enable mapping
there, supply `quickjs.WithSourceMaps` at VM construction and delegate its loader
to the attached view. This snippet uses `errors`, the public `stdlib` package,
and `github.com/go-quickjs/go-quickjs`:

```go
var view *stdlib.Bindings
rt := quickjs.New(quickjs.WithSourceMaps(func(script, reference string) ([]byte, string, error) {
    if view == nil {
        return nil, "", errors.New("bindings are not attached")
    }
    return view.ReadSourceMap(script, reference)
}))
loop := stdlib.NewLoop(rt)
attached, err := stdlib.Attach(rt, loop, profile)
if err != nil {
    return err
}
view = attached
```

`ReadSourceMap` uses the view's context and filesystem and refuses reads after
`view.Close()`. Inline data works without FS. External maps require local
absolute or file-URL script IDs; opaque or remote IDs support inline maps.
Keep the usual owner-thread evaluation and explicit view/loop/VM cleanup.
The [source map implementation record](development/stdlib-source-maps.md)
describes supported references, diagnostic compatibility, and verification.
