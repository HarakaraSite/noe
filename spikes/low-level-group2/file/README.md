# FileHandle spike

This opt-in Go test puts a small `spike:file` module beside the pinned
go-quickjs `fs` module. It exercises one long-lived `os.File` through
`readAt(offset, length)` and `writeAt(offset, Uint8Array)` while the existing
`fs.createReadStream(path, { start, end })` reads a fixed range sequentially.

Run it from the repository root:

```sh
go test -tags=lowlevel2_spike -v ./spikes/low-level-group2/file
```

The test is excluded from ordinary builds and tests. It uses only dependencies
already in the repository's `go.mod` and a temporary local file. It does not
run the NOE CLI or change the production runtime.

The prototype exports:

- `open(path, "r" | "r+")`, which opens synchronously and returns a handle.
- `readAt(offset, length)`, which asynchronously returns
  `{ data: Uint8Array, bytesRead: number, eof: boolean }`. It keeps partial
  bytes when Go returns both data and `io.EOF`.
- `writeAt(offset, bytes)`, which asynchronously returns the byte count. A
  Go write error rejects the promise.
- `close()`, which is asynchronous and idempotent in this prototype.

File I/O runs on goroutines. Promise conversion and settlement happen through
the go-quickjs stdlib loop, on the runtime's owner goroutine. The host keeps
handles until `close()` or runtime close; an open handle by itself does not
keep the event loop busy. Runtime close closes any handles still in the host
registry.

The spike passes a local absolute path directly to `os.OpenFile`; relative
path/cwd resolution and open flags or permission selection remain unimplemented
in this prototype. It does not reproduce the embedded stdlib's optional
`FS.Root` configuration, which is a separate stdlib setting. That observation
does not add a filesystem sandbox requirement to NOE: NOE continues to use the
OS user's permissions. Cancellation and finalization when a JS handle becomes
unreachable are also outside this prototype.

`readAt`/`writeAt` are useful when one open descriptor needs repeated random
reads or in-place partial updates. For a single range, the existing stream
already works; a caller can open another range stream when needed. Whole-file
reads and writes can use the existing JS filesystem API, and a CLI can do the
same job outside the runtime. The spike establishes an additional access
pattern, not a requirement that every script use a new API.

`stat`, `truncate`, and `sync` are not bundled into the prototype. `stat` is
only needed when a caller needs size or metadata without probing by reading;
`truncate` is needed to shrink a file and is intentionally not implied by
`writeAt`; `sync` is only needed if the API promises an explicit durability
boundary. Their inclusion should be decided separately from positional I/O.

See [RESULTS.md](RESULTS.md) for the observed command output, limits, and
contract recommendation.
