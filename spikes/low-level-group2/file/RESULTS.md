# FileHandle spike results

## Environment and command

The repository context supplied for this plan identifies Linux amd64,
Go 1.27.2, and go-quickjs v0.27.0. The local `go version` output was:

```text
go version go1.27.2 linux/amd64
```

Command run from the repository root:

```sh
go test -tags=lowlevel2_spike -v ./spikes/low-level-group2/file
```

Observed output:

```text
=== RUN   TestFileHandleAgainstRangeStream
--- PASS: TestFileHandleAgainstRangeStream (0.02s)
=== RUN   TestOpenHandleDoesNotKeepRuntimeAliveAndRuntimeCloseReclaimsIt
--- PASS: TestOpenHandleDoesNotKeepRuntimeAliveAndRuntimeCloseReclaimsIt (0.01s)
=== RUN   TestFileReadDoesNotBlockQuickJSTimerAndPromiseDelivery
--- PASS: TestFileReadDoesNotBlockQuickJSTimerAndPromiseDelivery (0.01s)
PASS
ok  	noe/spikes/low-level-group2/file	0.042s
```

Additional checks completed:

```text
go test -race -tags=lowlevel2_spike -count=1 -v ./spikes/low-level-group2/file
PASS
ok   noe/spikes/low-level-group2/file 1.219s

go vet -tags=lowlevel2_spike ./spikes/low-level-group2/file
# completed with no diagnostics

gofmt -d spikes/low-level-group2/file/filehandle_spike_test.go
# no output
```

## Confirmed in Go and JavaScript

These assertions ran JavaScript inside a Go-created go-quickjs runtime with the
pinned stdlib filesystem and loop installed. They are embedded-runtime
observations, not a test of starting the `noe` executable.

- `fs.createReadStream(path, {start: 2, end: 4})` produced `cde` and then EOF.
  This stream is opened for a fixed range and consumed sequentially.
- One explicit handle read offset 2 (`cd`) and then offset 0 (`ab`). Its file
  position did not force the second read to continue at offset 4.
- Reading 4 bytes at offset 4 in a 6-byte file returned the two available
  bytes (`ef`), `bytesRead: 2`, and `eof: true`. Reading at offset 6 returned
  zero bytes and EOF. This preserves Go `ReadAt`'s partial-data-plus-EOF case
  in the JS result.
- Writing `XY` at offset 2 returned 2 and left the file as `abXYef`. Bytes
  outside the write range remained intact, and the file did not shrink.
- Calling `close()` twice succeeded; a subsequent read rejected.
- A test gate before the `ReadAt` syscall held the file-read worker until a JS
  timer ran. The timer fired while the read promise was pending and released
  the worker; this confirms that the owner loop can deliver timer work while
  native work is pending, then deliver the read result back to JS.
- With only a synchronously opened handle remaining, `stdlib.Loop.Run` returned
  and `Runtime.Busy()` was false while the OS file was still open. Closing the
  runtime closed that handle; a later `os.File.Stat` returned `os.ErrClosed`.

The Go test itself also confirms the host's direct `os.File` state and EOF
errors. JavaScript assertions confirm how those results cross the module and
Promise boundary. No `noe` CLI invocation, process exit, JS garbage collection,
or runtime shutdown during active file I/O was tested.

## Contract recommendation and limits

Keep `readAt` and `writeAt` as the candidate core if the plan needs repeated
random access on one open file or bounded in-place updates. The prototype's
`readAt` result object carries both partial bytes and an explicit EOF bit;
turning `io.EOF` into a rejected promise would lose the partial bytes. A write
does not imply truncation. The prototype uses a direct absolute temporary path
and only `r`/`r+` modes, so relative path/cwd resolution and open flag/permission
selection remain unimplemented. It does not reproduce the embedded stdlib's
optional `FS.Root` setting; that separate setting does not make sandboxing a
new NOE requirement, whose filesystem access remains governed by the OS user's
permissions.

The observed ordinary-file operations used temporary local storage and
completed quickly. The spike did not measure a blocked filesystem syscall or
immediate cancellation; its gate was before `ReadAt`, not inside the OS call.
These methods have no context or `AbortSignal`; closing a handle while an
operation is in flight is not demonstrated as an immediate cancel contract,
and shutdown behavior for a blocked filesystem operation remains unverified.

Existing JS `fs` calls can still accomplish equivalent bounded work by opening
a new range stream per read, or by reading and rewriting a whole file. External
CLI tools can also perform these operations. The new handle API adds reuse of
one open descriptor and in-place positional updates; it does not make existing
approaches impossible.

`stat` is useful when the caller must know file size or metadata without
reading. It is not needed to perform a bounded positional read or detect EOF.
`truncate` should be separate because replacing bytes and shrinking a file are
different operations. `sync` should be added only with an explicit durability
requirement; this spike did not test crash consistency or stable-storage
behavior.
