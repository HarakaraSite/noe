# Filesystem API direction and implementation

2026-11-06. This slice applies the approved
[standard library API policy](stdlib-api-policy.md). The user approved one
`noe:fs` module and removal of `node:fs`, `node:fs/promises`, `fs`, and
`fs/promises`, without compatibility adapters. Implementation is local and
unreleased. The process and HTTP migrations are separate slices.

## Public contract

Async operations are exported directly from `noe:fs`. Explicit `*Sync`
helpers remain available. Named imports, namespace imports, and the default
object expose the same methods. There is no `promises` property or promises
submodule. Whole-file, metadata, directory, symbolic errno, Date-valued
`mtime`, POSIX `mode`, and Web Stream helper contracts are retained.

```js
import {open, readFile, watch} from "noe:fs";

await using file = await open("index.bin", {flags: "r+"});
const bytes = await file.readAt(2, 3);
const written = await file.writeAt(2, bytes.map(byte => byte + 1));
console.log(written, await readFile("index.bin"));
```

| Operation | Contract |
| --- | --- |
| `open(path, options?)` | Promise of FileHandle. Options are `{flags: "r", mode: 0o666}` by default; existing POSIX flag strings including create, exclusive, and append forms are supported. |
| `file.read(length = 16384)` | Promise of a Uint8Array containing bytes read at the current cursor. Empty at EOF for a positive request. |
| `file.readAt(position, length)` | Same byte result, without moving the current cursor. |
| `file.write(data)` | Promise of the number of bytes written; accepts a UTF-8 string or ArrayBufferView. |
| `file.writeAt(position, data)` | Same count result, without moving the current cursor for non-append handles. Append handles ignore the supplied position, write at EOF, and advance their cursor. |
| `file.stat()/truncate(length = 0)/sync()/close()` | Existing native metadata, resize, flush, and ownership operations. Negative truncate rejects with `ERR_INVALID_ARG` without modifying the file. |
| `watch(path, options?)` | Async iterator of `{eventType, filename}` with close/return/asyncDispose; existing signal, persistent, queue, and overflow options. No EventEmitter watcher. |

Positions and lengths must be nonnegative safe integers; native per-read
limits remain in force. Partial I/O errors retain `bytesRead` with `data`, or
`bytesWritten`. The returned data and count avoid the old Node buffer wrapper
and overloads. Callers can pass a typed-array subarray to write a selected range.

The default object is a convenience for a coherent namespace, not an old-module
adapter. Sync functions retain their explicit suffixes; `watch` always means
the iterator API. Existing `createReadStream`/`createWriteStream` return Web
Streams; they currently open the underlying file synchronously and move chunk
I/O off the owner when a Loop is installed.

## Shared implementation and boundaries

`internal/stdlib/fs.go` composes existing filesystem operations, directory
values, stream helpers, the watch iterator, and the simplified
`filehandle.js` facade. Native file handles and filesystem watchers keep their
existing ownership. No second native handle implementation or new public Go
configuration field is introduced.

`Profile.Modules.FS`, Root/Cwd/ReadOnly, selected capability installation,
main/Worker behavior, close admission and draining, and forced execution
cleanup continue through the shared backend. Removing the bare FS modules
also removes their import-map built-in reservations; `node:` and `noe:`
prefixes remain reserved independently.

## Migration

- Replace all four old FS imports with `noe:fs`.
- Replace `fs.promises.method(...)` with `fs.method(...)`.
- Replace `open(path, flags, mode)` with `open(path, {flags, mode})`.
- Replace `file.read(buffer, offset, length, position)` with
  `file.readAt(position, length)` and use its returned bytes, or `read(length)`
  for sequential reads. Copy into an existing destination only when needed.
- Replace `file.write(data, offset, length, position)` with
  `file.writeAt(position, data.subarray(offset, offset + length))`; sequential
  writes use `write(data)`. DataView ranges can use a Uint8Array view.
- Replace `{bytesRead, buffer}` and `{bytesWritten, buffer}` handling with byte
  arrays and numeric counts. Partial read errors expose `data` directly.
- Replace callback/EventEmitter watching with `for await` or `next()`, and
  close the iterator or use `await using`. Runtime references start at first
  `next()`; use `{persistent:false}` for an active unreferenced watch.
- Handle negative truncate as an error instead of relying on the old clamp
  to zero. Use `truncate(0)` when clearing a file is intended.

NOE's examples, hidden libraries, fixture JS, Go consumers, and current
references migrate in this slice. Older release and phase records retain the
contracts they originally documented. The separate
[noebot migration instructions](noebot-noe-fs-migration.md) identify its runtime
imports and development-tool declaration pins.

## Verification

Local validation passed. Evidence is retained under
`/var/tmp/noe-fs-policy-aj2_zwmi`, with command logs, JSON results, consumer
artifact manifests, and the final source manifest.

- Full `internal/stdlib` unit suite; affected `internal/modulemap`,
  `internal/noestdlib`, `internal/jsexec`, `internal/runtime`, `internal/cli`,
  public `stdlib`, and embedded API catalog tests. The initial integration
  run exposed stale docs/import-map expectations; these were migrated and
  the affected gates passed.
- Targeted race checks for FileHandle operations/partial results/close,
  watcher ownership, public filesystem profile behavior, and file disposal.
- CLI run/eval, Worker, event-output, and fixture-agent filesystem/watch,
  positional I/O, cancellation/disposal, binary file, JSON collection, and
  HTTP example checks. The final iterator-only watch and standalone bundled
  docs checks also passed after the legacy adapter was removed.
- All six offline `.noe/lib` suites using the new candidate executable.
- Public stdlib consumers built and ran from both a checkout replacement and
  an independent module artifact with CGO disabled. The consumer built for
  Linux, Darwin, and Windows on amd64/arm64. Public agentlib portable and
  Go-host consumers also passed with their migrated filesystem import.
- TypeScript positive/negative fixtures, changed JavaScript/TypeScript lint,
  affected-package Go vet, and `git diff --check`.
- Bounded independent review found no runtime blocker. Three reference
  corrections (append cursor behavior, chmod masking, and reserved prefixes)
  were applied and confirmed by one change-only re-review.

The native file and watcher backends, Go options, and dependencies did not
change. Stream creation still opens files synchronously, and existing watch
backend limits remain; this slice changes their public composition rather
than adding capabilities. The installed executable was not replaced, and no
release was made. This migration is committed together with the
[session handoff](stdlib-handoff.md). noebot work is handled independently by its owner;
the read-only source audit and migration note do not constitute its validation.
