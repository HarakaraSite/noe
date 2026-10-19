# Embed NOE agents in Go

Import `forge.harakara.site/littleisland/noe/v2/agent` alongside the generic
[`stdlib`](go-embedding.md) package to add agent APIs to a caller-owned
go-quickjs runtime. The Go module remains one module; importing only stdlib
does not add the agent, CLI, or provider SDKs to that package's build closure.
The agent package uses the existing provider adapters and broker internally.

A v2 tag has **not been published** by this implementation. Existing v1 tags
are not Go library versions for the `/v2` path. For development, use the local
replacement in the [example](../examples/agent-embedding/README.md).

## Run a finite agent host

The complete [Go example](../examples/agent-embedding/main.go) creates its own
runtime, installs stdlib and agent, and passes an explicit path to a separately
built NOE executable. A deterministic provider requests a real `run_js` child
and checks its output, without credentials or a remote model.

```sh
CGO_ENABLED=0 go build -o /tmp/noe-agent-embedding ./cmd/noe
CGO_ENABLED=0 go run ./examples/agent-embedding -noe /tmp/noe-agent-embedding
```

The order is: create a context/runtime/Loop, install stdlib, install agent,
evaluate JavaScript, and drive the Loop on the runtime owner. Installation
does not grant FS, Fetch, Process, or native IO to the embedding runtime. The
example needs only the pure library, a Loop, and optional Docs. Image helpers
use `node:path` and resolve relative paths through the core process object;
configure `Core.Process.Cwd` when using them. Auth is a separate opt-in below.

Capture **both** fulfillment and rejection of the root Promise and signal a
done channel from the owner. Call `Loop.RunUntil(ctx, done)`, inspect the
captured result, then call `binding.Close()`. Check that the root actually
settled if the loop ended early. A completed `run_agent` task can leave its
managed instance idle and alive; `Loop.Run` can therefore keep waiting. A
long-lived host can deliberately retain the binding instead.

Always close the runtime and Loop as well. Pass the bounded context to CPU
evaluation and loop driving, not just agent.Install. Binding cancellation
stops agent work; it does not interrupt unrelated JavaScript or generic IO.

## Choose the NOE executable and working directory

`Config.Executable` is required. It names a NOE executable, not the Go embedding
application. There is no PATH search or implicit `os.Executable()` fallback.
Relative paths are resolved once from the Go process cwd at installation.
`Config.Cwd` independently selects the working directory used by agent child
processes; empty snapshots the Go process cwd. The executable is started only
when required by a task, so catalog-only setup does not launch it.

Tasks, `run_js`, and child agents run in separate NOE processes with the host
user's OS authority. Capabilities restricted on the root stdlib do not restrict
those children. The existing runner retains its cwd, environment filtering,
output truncation, process-group/Job Object cleanup, and scoped broker
connection. Provider clients and their configuration remain in the root host.
Connection capabilities are private and are not exposed through Config.

Use a NOE executable built from the same source/tag as the library. The CLI
version string alone does not establish protocol compatibility; compatibility
with arbitrary other versions is not guaranteed. This API adds no handshake
or blanket rejection of other versions. Record what you actually use.

## Providers, credentials, decisions, and images

Leave `Config.Provider` nil to use the built-in registry, and configure its
`Providers` field with public `ProviderOptions`. ConfigFile uses NOE's existing
[provider JSON schema](providers.md); its provider/model definitions select
endpoints, authentication, headers, and image settings. The public Go options
include only ConfigFile, UserAgent, KeyFile, BaseURL, HTTPClient, and
DecisionServerURL. KeyFile, BaseURL, and DecisionServerURL retain their legacy
OpenRouter override semantics; configure other providers through ConfigFile.
Adapter-only Profile/Definition types are not public.

Empty configuration and key paths retain existing lazy NOE discovery, including
NOE_CONFIG_FILE and XDG/default locations. Installation itself does not read
credentials/configuration, start a broker, or contact a provider. Configuration
files and user instructions are read when needed, not snapshotted at Install.
Supply explicit paths and an HTTPClient when your application needs different
settings. Credential loading and existing OAuth refresh are unchanged.

For a custom provider, implement `Generate(context.Context, agent.Request)
(agent.Response, error)`. Request/Response/StreamEvent are SDK-independent
aliases using standard Go and JSON types. An optional
`HasProvider(string) (bool, error)` method supplies provider-name validation;
without it, the existing custom-provider fallback accepts `openrouter`.
`Providers` still configures the registry used for model catalog and context
limits, even when generation uses a custom Provider. Configure that catalog
explicitly for custom model names when metadata is needed.

`DecisionProvider` and `ImageProvider` optionally override their operations;
compatible implementations on Provider remain the default fallback. The
associated request/response types and `ImageError` are public aliases without
SDK-specific signatures. Provider operations can run concurrently and must
cooperate with context cancellation.

## Module loading and credential saving

Pass the root runtime's synchronous loader as `Config.Load`. Install sets a
loader that serves agent built-ins first, then calls Load for other source
imports. It cannot discover and wrap a previously installed loader. A nil
fallback rejects other source imports. Preserve canonical module IDs and
referrers in your loader. Agent implementation modules remain private JS
details, despite being available to the built-in module graph.

`Config.ChildImportMap` is separate import-map JSON for the NOE child
processes. Relative map targets are resolved against Cwd and frozen as absolute
targets during Install. It does not configure the root Load callback, and a Go
loader callback cannot be serialized into another process. Provide files and
map entries for modules that tasks, hooks, or run_js children need.

`Config.Auth == nil` leaves the `noe:auth` credential-saving helper uninstalled.
Pass `&agent.AuthOptions{...}` to opt into its existing save API with an
optional HTTPClient and DiscoveryURL. Provider credential reads and OAuth
refresh do not depend on that helper. NOE's CLI keeps installing its usual
auth module. Agent/decision/image/auth remain unsupported in NOE Workers;
the generic stdlib does not automatically propagate this binding to Workers.

`ControlDir` explicitly opts into existing human-control discovery. Empty
does not select the CLI's default control directory. No new public Go control
RPC or capability format is introduced.

## Ownership and callbacks

Install must run on the JavaScript owner with a live, non-nil context and
runtime, the matching `stdlib.NewLoop(rt)`, and an executable path. Do not use
a zero Loop or a Loop belonging to another runtime. Mutable configuration data
is copied; Provider, HTTPClient, loaders, Observe, Trace, and destinations are
borrowed. This does not make concurrent mutation of borrowed resources safe.

One binding owns one operation lifetime, including hook scopes and owned
agents. Context cancellation or runtime shutdown closes that lifetime.
`Binding.Context()` exposes its cancellation context. `Binding.Close()` is
idempotent and may run concurrently from Go; it joins requests and child
processes without requiring the owner to drive JavaScript replies. It does
not close the runtime, Loop, native IO, or original output sinks. Install
after stdlib so runtime close runs agent cleanup before stdlib output cleanup.

Observe is serialized on Go provider/broker goroutines, outside Host.mu. Keep
callbacks short. Do not touch JavaScript there or synchronously re-enter Close
or agent RPCs on that binding; forward notifications to the owner instead.
Copy event maps before retaining them asynchronously. Trace is borrowed too.
Providers and clients that ignore cancellation can delay cleanup; Close cannot
invent cancellation for an uncooperative borrowed implementation.

Installation is not transactional. After an installation error, close and
discard the runtime. Repeated installation, restarting a closed binding, or
using its zero value is unsupported. Fresh runtimes can reuse the same config.

## Distribution checks

Normal unit CI explicitly builds a portable consumer outside the checkout.
The reference-host helper also tests real child NOE processes and a synthetic
module proxy artifact without a local replacement:

```sh
CGO_ENABLED=0 go test ./agent -count=1 -timeout=300s
python3 scripts/check-stdlib-consumer.py --agent --noe /absolute/path/to/noe \
  --output /absolute/path/to/consumer-results --cross
```

The artifact check uses an isolated module cache and preserves dependency
checksum verification. It does not publish a tag or prove remote download.
After an actual authorized v2 release, remove the local replacement, require
that published tag, and run the example with the matching release binary:

```sh
go get forge.harakara.site/littleisland/noe/v2/agent@<published-v2-tag>
go run . -noe /absolute/path/to/released-noe
go list -m forge.harakara.site/littleisland/noe/v2 github.com/go-quickjs/go-quickjs
```

The tested engine is go-quickjs v0.27.0, with Go 1.27.2 or later. Check the
version selected by your application if module version selection upgrades it.
