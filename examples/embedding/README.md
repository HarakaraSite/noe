# Embed NOE's generic APIs in Go

This example uses the public `stdlib.Profile` API to execute a JavaScript
module, print the embedded API-reference index, and write console output to a
borrowed Go writer. It uses no CLI process, agent Host, provider credentials,
or external service.

From the repository root, with Go 1.27.2 or later:

```sh
CGO_ENABLED=0 go run ./examples/embedding
```

To use a separate module before publication, copy `main.go` and
`go.mod.template` outside the repository, rename the template to `go.mod`, and
set its replacement to the absolute checkout path. Then run
`GOWORK=off go mod tidy` and `GOWORK=off CGO_ENABLED=0 go run .`.
`v2.0.0-dev.0` is a local-development placeholder, not a published tag.
After v2.0.0 is published, remove the replacement and require that stable tag
instead; see the [v2 migration guide](../../docs/v2-migration.md).

For a long-running Go HTTP host that creates a fresh runtime per request, see
[the resident server example](../embedding-server/README.md). It includes
signal-driven shutdown and a Linux systemd user-service setup.

The [Go embedding guide](../../docs/go-embedding.md) covers capability
selection, file loading, cancellation, ownership, and the advanced `Attach`
path. The separate [agent example](../agent-embedding/README.md) demonstrates
the Go Host and Client APIs.
