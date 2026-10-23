# Embed a managed agent in Go

This example constructs a model service, creates an `agent.Host` with a generic
runtime profile, and runs one task through a Go `Client`. Its local provider
returns a deterministic answer; no NOE executable, credentials, or remote
service is required.

From the repository root, with Go 1.27.2 or later:

```sh
CGO_ENABLED=0 go run ./examples/agent-embedding
# The total is 31.
```

For a separate module, copy `main.go` and `go.mod.template` outside the
checkout. Rename the template to `go.mod`, set its replacement to the absolute
checkout path, and run `GOWORK=off go mod tidy` followed by
`GOWORK=off CGO_ENABLED=0 go run .`. The module version in the template is a
local-development placeholder. For the published release, remove the replacement
and require v2.3.0 instead; see the [v2 migration guide](../../docs/v2-migration.md).

`Client.Run` waits for the managed instance to finish. For a keep-alive
instance, use `Spawn`, retain both the returned `agentId` and `commandId`, then
call `WaitTask`; `Client.Close` cancels and joins work owned by that client.

To expose the same host to JavaScript, create a profile initializer that calls
`agent.Attach(ctx, bindings, host)` and returns `binding.Close` as its cleanup.
The attached binding owns a new client, while the Host, runtime, and loop stay
borrowed. The [agent embedding guide](../../docs/go-agent-embedding.md) covers
model, auth, and log services, loader setup, Host administration, and
cancellation. The [generic embedding guide](../../docs/go-embedding.md)
describes the runtime profile and capability model.
