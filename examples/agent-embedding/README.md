# Embed an agent in Go

This example installs the public `agent` package on a caller-owned runtime.
A deterministic Go provider requests a real `run_js` tool execution in a
separate NOE process, checks the result, and returns an English answer.
It requires no provider credentials or remote services.

From the checkout, using Go 1.27.2 or later:

```sh
CGO_ENABLED=0 go build -o /tmp/noe-agent-embedding ./cmd/noe
CGO_ENABLED=0 go run ./examples/agent-embedding -noe /tmp/noe-agent-embedding
# The total is 31.
```

For a separate module, copy `main.go` and `go.mod.template` outside the
checkout. Rename the template to `go.mod`, set its replacement to your absolute
NOE checkout path, and run `GOWORK=off go mod tidy`, followed by
`GOWORK=off CGO_ENABLED=0 go run . -noe /absolute/path/to/noe`.
The module's placeholder version is for local development, not a published tag.

The example uses `Loop.RunUntil` to wait for the root result, then closes the
agent binding. Task completion can leave a managed agent idle, so `Loop.Run`
does not necessarily finish when `run_agent` returns.

See the [agent embedding guide](../../docs/go-agent-embedding.md) for real
providers, credentials, module loading, callbacks, and lifecycle contracts.
