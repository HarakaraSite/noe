# Embed managed agents in Go

The `/v2` `agent` package provides a Go `Host` and scoped `Client`. Agents run
inside fresh in-process JavaScript executions; the embedding application does
not need a separate NOE executable or the CLI. Import
`forge.harakara.site/littleisland/noe/v2/agent`, `agent/model`, and
`stdlib`. The current release is v2.2.0. For an unpublished checkout, use the
local module setup in the [agent example](../examples/agent-embedding/README.md).
To use the published release, use
`go get forge.harakara.site/littleisland/noe/v2/agent@v2.2.0` without a local
replacement. See the [v2 migration guide](v2-migration.md) for breaking changes.

The package requires Go 1.27.2 or later and uses the independently versioned
managed engine module `forge.harakara.site/littleisland/noe/third_party/go-quickjs`
v0.28.1-noe.1, based on upstream go-quickjs v0.28.0. From the checkout root, run the deterministic example with:

```sh
CGO_ENABLED=0 go run ./examples/agent-embedding
```

## Create a model service and host

`agent.NewHost` requires a `model.Service`. `agent/model.NewService` builds a
service from explicit configuration and borrowed transports; its catalog and
operations do not require an agent Host or JavaScript runtime. It snapshots
the supplied environment and credentials map. `Env == nil` is empty, paths
are based at `BaseDir`, and no ambient NOE configuration or home-directory
search is performed. Configuration and credential files are read lazily when
the selected provider is used.

```go
models, err := model.NewService(model.Options{
    BaseDir: cwd,
    ConfigFile: "providers.json",
    Env: map[string]string{
        "NOE_OPENAI_API_KEY_FILE": "credentials/openai-api-key",
    },
    HTTPClient: httpClient, // borrowed; never closed by the service
})
if err != nil {
    return err
}

taskProfile := &stdlib.Profile{
    Environment: stdlib.Environment{Cwd: cwd, Env: map[string]string{}},
    Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{ReadOnly: true}},
    NewLoader: stdlib.FileLoader(nil),
}
host, err := agent.NewHost(ctx, agent.HostConfig{
    Models: models,
    Runtime: taskProfile,
    Instructions: instructionSource,
    Policy: "Use concise, evidence-based answers.",
    Observe: observe,
    Trace: trace,
})
if err != nil {
    return err
}
defer host.Close()
```

`HostConfig.Runtime` is a template for fresh task and tool executions. Its
`Environment.Stdin`, `Stdout`, and `Stderr` must be nil; the executor supplies
empty input and bounded, owned capture streams. Set `Runtime` to nil only for
a host that will not spawn or run tasks; such requests are rejected before a
model call. A selected FS shares the same root, read-only setting, and virtual
cwd for task file access and image helpers. Commands must be selected
separately in `Modules.Commands`; a file `Root` is not a command sandbox.

`Host` borrows the model service, HTTP clients, callbacks, and trace writer.
`Host.Close` cancels and joins every client and owned instance but does not
close a borrowed service or transport.

## Use a scoped Client

Each `Host.NewClient(ctx)` creates one ownership scope. A client's requests,
agents, and children belong to that scope. `Client.Close` cancels and joins
its requests and agents; closing one client does not close the Host or another
client.

```go
client, err := host.NewClient(ctx)
if err != nil {
    return err
}
defer client.Close()

instruction := "Read the project overview and return three key points."
snapshot, err := client.Run(ctx, agent.SpawnRequest{
    Config: agent.TaskConfig{
        Config: agent.AgentConfig{
            Provider: "openrouter", Model: "provider/model-id", Name: "reviewer", Effort: "auto",
            MaxTokens: 1200,
        },
        MaxSteps: 4,
    },
    Instruction: &instruction,
})
if err != nil {
    return err
}
if snapshot.LastResult == nil || snapshot.LastResult.State != "completed" {
    return fmt.Errorf("agent did not complete: %+v", snapshot.LastResult)
}
fmt.Println(snapshot.LastResult.Answer)
```

`Run` starts an instance and waits for its terminal snapshot. For a keep-alive
instance, call `Spawn` with `KeepAlive: true`, retain its `AgentID` and initial
`CommandID`, and use `WaitTask` for an accepted task. `Queue` returns a new
`CommandID`; `WaitTask` returns that task's retained outcome. A short request
context that cancels `WaitTask` does not cancel the task itself. Use
`CancelTask` with both IDs to cancel one accepted task, or `StopAgent` to stop
the instance, active task, and remaining queue. `Observe` returns a live
snapshot; it does not read a saved event file. `ReadTaskResult` reads a
completed answer in UTF-8 byte ranges without consuming it.

Go `Host` management methods (`ListAgents`, `Queue`, `Steer`, `CancelTask`, and
`StopAgent`) are trusted host operations. A scoped Client and JavaScript
binding can act only within their ownership. `Client.Models()` provides
catalog, decision, generation, and image calls through the same scope,
context, group limits, usage accounting, and cancellation rules as its agent
operations.

## Attach the Host to JavaScript

`agent.Attach(ctx, bindings, host)` installs `noe:agent` and gives that runtime
one new, scoped Client. Call it from `stdlib.Profile.Initialize` before entry
evaluation and return the binding's `Close` method as cleanup:

```go
Initialize: func(ctx context.Context, view *stdlib.Bindings) (func(), error) {
    binding, err := agent.Attach(ctx, view, host)
    if err != nil {
        return nil, err
    }
    return binding.Close, nil
}
```

This view borrows the Host, runtime, loop, filesystem, and environment. Closing
the binding closes only its Client and owned agents; it does not close the Host
or the borrowed runtime and loop. Each root/task/tool runtime receives a fresh
initializer call and binding. Do not retain JavaScript values or a binding
across executions. Use the public `stdlib.Attach` advanced path when an
application already owns a QuickJS runtime; otherwise prefer `stdlib.Execute`
or `NewRuntime`.

## Independent model, auth, and log APIs

`agent/model.Service` has four operations: `ListModels`, `Generate`, `Decide`,
and `GenerateImage`. Catalog reads do not load credentials or contact a
provider. A custom `Provider`, `DecisionProvider`, or `ImageProvider` can
implement its operation independently; an unsupported custom operation is
returned as unsupported rather than sent to another provider. Image requests
return bytes and metadata without an output path. A JavaScript model binding
is installed separately with `agent/model.Bind`; its `generate_image` file
helper uses the attached execution filesystem and returns metadata only.

`agent/auth.SaveChatGPT(ctx, request, options)` validates and saves a completed
ChatGPT sign-in result at the explicit path in `request.File`. It does not
create an agent Host or search NOE paths. An HTTP client is borrowed. To expose
`noe:auth` to JavaScript, call `agent/auth.Bind` from a runtime initializer and
return `Binding.Close` as cleanup.

`agent/log.Analyze(ctx, reader, query)` and `AnalyzeBytes` reconstruct an agent
run from caller-supplied JSONL data. They do not open files, poll, contact a
provider, or start a Host. A cursor filters returned events; state is always
computed from the full supplied snapshot. The optional `agent/log.Bind`
installs `noe:agent-log`; its `analyze_log(input, options)` accepts a string or
`Uint8Array` snapshot, not a path. Read a file through the selected filesystem
or another caller-owned reader before invoking the parser.

## Instructions, callbacks, and cleanup

`Instructions` is an optional `func(context.Context) (string, error)`. The
Host calls it once for an instance before its first hook or model request,
caches success or failure for that instance, and calls it again for a new
instance. Queued tasks reuse the cached text. `Policy` is explicit Host
configuration; an empty value adds no application-specific policy.

`Observe` is called on Go request goroutines, outside the Host lock. Keep it
short and do not touch JavaScript or synchronously call Host operations from
it. Copy event values before retaining them. `Trace` is also borrowed. All
provider and initializer work should cooperate with context cancellation;
Host and Client close operations join owned work and can wait for an
uncooperative callback.

## Migrate from the previous agent package

Replace `agent.Install` and its executable/connection configuration with a
`model.Service`, `agent.NewHost`, and scoped clients. Use `agent.Attach` only
when JavaScript should share a Host; it does not create or own that Host.
Replace saved-log variants of `observe_agent` with caller-side file reading
and `agent/log` parsing. Replace task/instance interruption with the separate
`CancelTask` and `StopAgent` operations. Do not pass NOE control directories,
executable paths, or internal connection tokens into the library API.

## Execution contract in v2.2.0

The current checkout returns an ESM default export as detached JSON in
`stdlib.Result.Data`. No export means nil; an explicit null is JSON `null`.
Use `export default await operation()` for async values. A default Promise or
unserializable value fails with `JS_RESULT` in the `serialize` phase after code
may already have run. `Result.Diagnostics` contains structured facts and
`Result.Phase` identifies the processing stage. `stdlib.Diagnostic(err)` remains
available for human-readable stacks and native Go causes remain inspectable.

`stdlib.Check(ctx, source, profile)` links a fresh static module graph without
evaluating module bodies. Only `Profile.InspectInitialize` is used for inspection;
normal `Initialize` is not replayed. Inspection callbacks own their resources
and must not close a running execution's binding or operation. The matching
JavaScript entry is `check(code)` from `noe:runtime`. The docs module's
`describe(topic?, member?)` separates real exports from reference detail IDs.

The managed engine is an independent module under `third_party/go-quickjs`,
with public path `forge.harakara.site/littleisland/noe/third_party/go-quickjs`.
Custom Go bindings that name engine types must import this module instead of
`github.com/go-quickjs/go-quickjs`; types from the two module paths are distinct.
The alias `quickjs` can remain unchanged. For local checkout development, the
example template replaces both NOE and the engine; dependency replacements are
not inherited. Published consumers need neither replacement. Synthetic artifact
checks precede publication; actual published-module retrieval is checked after
the engine submodule tag and NOE tag are pushed.
