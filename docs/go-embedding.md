# Embed NOE's standard library in Go

Use `forge.harakara.site/littleisland/noe/v2/stdlib` to run JavaScript with a
per-execution profile. `stdlib` does not create a NOE CLI process and does not
select model providers or agent behavior. The current release is v2.1.0; see the
[v2 migration guide](v2-migration.md) for changes from the previous embedding API.

The current checkout is checked with go-quickjs v0.28.0. Published NOE tags
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
`go get forge.harakara.site/littleisland/noe/v2/stdlib@v2.1.0`. Downloading that
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
`LoaderFactory`, and an optional runtime `Initialize` callback. A nil
capability option disables that capability. The profile snapshots `Args`,
`Env`, and option values when attached; caller-owned readers, writers,
resolvers, clients, providers, and callbacks remain borrowed.

| `Modules` field | Capability |
| --- | --- |
| `ProcessInfo` | `process`, `process.cwd()`, and `process.env` |
| `Stdio` | `noe:io` and the selected environment's streams |
| `FS` | `node:fs`, `node:fs/promises`, and `noe:fs-handle`; `FileSystemOptions` selects `Root` and `ReadOnly` |
| `Commands` | `node:child_process` and `noe:process`; options select `Allow`, timeout, output limit, and optional `Supervisor` |
| `DNS`, `Fetch`, `Serve`, `WebSockets` | The corresponding network capability and its limits/callbacks |
| `RawSockets`, `Signals` | `noe:sockets` and `noe:signals` |
| `Workers`, `OS` | Worker runtimes and selected host-information behavior |
| `Random`, `WindowsPaths` | Random source and path interpretation for the execution |

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

## Load local modules

`stdlib.FileLoader(imports)` returns a `LoaderFactory` that creates a fresh
loader for each runtime. The string map maps exact bare specifiers to local
file targets; targets are anchored to that execution's cwd and read through
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
