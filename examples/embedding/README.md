# Embed NOE's generic APIs in Go

This host uses the public `forge.harakara.site/littleisland/noe/v2/stdlib`
package. It creates a go-quickjs runtime and loop, reads the built-in English API
reference, and pipes borrowed input to borrowed output using Web Streams.
No CLI, agent, provider credentials, or external service is required.

Inside the checkout, with Go 1.27.2 or later:

```sh
CGO_ENABLED=0 go run ./examples/embedding
```

To use a separate module before publication, copy `main.go` and
`go.mod.template` outside this repository, rename the template to `go.mod`, and
set its replacement to your absolute checkout path. Then run:

```sh
GOWORK=off go mod tidy
GOWORK=off CGO_ENABLED=0 go run .
```

`v2.0.0-dev.0` is a development placeholder for this local replacement, not a
published tag. The [Go embedding guide](../../docs/go-embedding.md) explains
capabilities, ownership, cancellation, Workers, and publication-time commands.
Agent embedding is a later step and is not exposed by this package.
