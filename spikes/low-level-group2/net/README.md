# TCP / Unix socket spike

This opt-in test spike checks whether a small Go-to-QuickJS bridge can expose a duplex socket as Web Streams while preserving half-close behavior. It uses only loopback TCP, a temporary Unix domain socket, the pinned `go-quickjs` runtime, and Go's standard library. It does not import or change NOE's `nativeio` implementation.

Run it from the repository root:

```sh
go test -tags lowlevel2_spike ./spikes/low-level-group2/net -count=1 -v
```

The source has a `//go:build lowlevel2_spike` constraint, so this directory is absent from ordinary `go list ./...` and normal repository builds. Running `go test ./spikes/low-level-group2/net` directly without the tag reports that build constraints exclude all Go files; use the tagged command above.

The small bridge exposes `read`, `write`, `cancelRead`, `closeWrite`, `abortWrite`, and `fullClose`. The Web Streams adapter maps `ReadableStream.cancel()` to `CloseRead()`, `WritableStream.close()` to `CloseWrite()`, `WritableStream.abort()` to an immediate write deadline followed by `CloseWrite()`, and explicit full close to `net.Conn.Close()`. Byte chunks are copied before the worker goroutine uses them. QuickJS values and promises are only created or settled on the runtime owner goroutine through `Runtime.Go`.

The half-close case sends two JSONL records, closes only the client writer, and has the server wait for EOF before returning a response containing both binary bytes (`0x00`, `0xff`) and JSONL. The client receives the complete response after `writer.close()`. Separate cases check that Go read/write workers have reached the call site and remain unfinished immediately before cancellation/abort, then verify termination and the expected Promise result while keeping the opposite direction usable. They do not inspect Go's netpoller or assert that a worker is parked inside an OS wait.

The listener cases model Go-side ownership with a referenced `AsyncWork`: an open listener keeps the QuickJS loop running without a JS timer, and Go-side close releases that work. A pending `Accept` also rejects when the listener closes. The connection bridge registers a Go `OnClose` hook; closing the runtime closes the connection and releases a pending read without calling back into JS. This demonstrates the resource behavior the spike needs; it is not an NOE API implementation.

See [RESULTS.md](RESULTS.md) for observed outcomes, contract recommendations, and limits.
