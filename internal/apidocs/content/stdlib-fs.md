# node:fs

### API information

Reference: `node:fs`; aliases: `fs`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.FS.

Public-definition source: internal/stdlib/fs.go, internal/stdlib/fsstream.go, internal/stdlib/fs_watch.go, internal/stdlib/fs_watch.js. Reading this reference evaluates no target module and makes no provider request.

Reads and writes files synchronously or as Web Streams. The module exposes synchronous file helpers, a promises namespace, and createReadStream/createWriteStream. Stream values are Web Streams, not Node streams: there are no Node-style stream events or pipe() methods.

### Exports

`Dirent`, `appendFileSync`, `chmodSync`, `copyFileSync`, `createReadStream`, `createWriteStream`, `default`, `existsSync`, `lstatSync`, `mkdirSync`, `mkdtempSync`, `promises`, `readFileSync`, `readdirSync`, `readlinkSync`, `realpathSync`, `renameSync`, `rmSync`, `rmdirSync`, `statSync`, `unlinkSync`, `watch`, `writeFileSync`

Use node:fs for synchronous whole-file operations or sequential Web Streams. Use open from node:fs/promises for reads and writes through an open file; its FileHandle methods are documented in node:fs/promises. A stream closes when it completes, is cancelled/aborted, or its runtime closes.

Directory listings return names by default. Pass `{withFileTypes: true}` to receive `Dirent` objects with entry names and type predicates.

Filesystem errors from synchronous calls and Web Streams use string `code` values such as `ENOENT` and `EACCES`. When the operating system provides an errno, `errno` retains its positive platform value; the value varies by platform. Errors may also include `path`, `dest`, and `syscall` when available; path fields are projected relative to the configured filesystem root. Custom codes include `ERR_INVALID_ARG`, `ERR_READ_ONLY`, `ERR_FILE_CLOSED`, and the fallback `ERR_IO`.

## watch

`watch(path, options?, listener?) -> FSWatcher`. The second argument may be
the listener or an encoding string. Paths are nonempty strings. Watches a file
or one directory's immediate entries, using the selected filesystem's Cwd and
Root. ReadOnly permits observation. Recursive watching is not implemented.

The watcher extends `node:events.EventEmitter`. The optional listener handles
`"change"` events with `(eventType, filename)`, where eventType is `"rename"`
or `"change"` and filename is a basename, or null if unavailable. Create,
remove, and rename map to `"rename"`; writes and metadata changes map to
`"change"`. Notifications can coalesce and do not form a lossless journal.
For atomically replaced files, watch their containing directory.

Options: `encoding` is `"utf8"` (default), `"utf-8"`, or `"buffer"`; the last
returns a Buffer filename. `persistent` defaults to true. `signal` accepts an
AbortSignal; actual abortion closes this watcher without an error event.
`maxQueue` is a positive safe integer, default 2048. `overflow` is `"ignore"`
(default: drop excess events and warn when consumed) or `"error"` (close with
`ERR_FS_WATCH_QUEUE_OVERFLOW`). Operating-system queue overflow is a separate
error. `recursive:true`, `ignore`, and `throwIfNoEntry:false` are unsupported;
unsupported modes throw `ERR_NOT_SUPPORTED`.

Missing targets and registration failures throw normal filesystem errors.
Later failures emit `"error"` and stop the watcher. `close()` stops watching
synchronously and emits `"close"` in a microtask; it returns undefined and is
idempotent. `ref()` / `unref()` return the watcher and control whether it keeps
execution alive. `await using` calls `[Symbol.asyncDispose]()` and awaits close
notification. Execution shutdown releases native resources without JS delivery.
The event form needs an event loop and the pure `node:events` module, both
present in the normal execution profiles. Promise iterators also work without
a Loop or web APIs; buffer names require `node:buffer`.

~~~js
import {watch} from "node:fs";

const stop = new AbortController();
const watcher = watch(".", {signal: stop.signal}, (eventType, filename) => {
  console.log(eventType, filename);
});
watcher.on("error", error => console.error(error));
// Call stop.abort() or watcher.close() when observation is complete.
~~~

## readdirSync

Syntax: `readdirSync(path, {withFileTypes = false} = {}) -> string[] | Dirent[]`. Entries are sorted by name. With the default or `withFileTypes: false`, each entry is a name string. With `withFileTypes: true`, each entry is a `Dirent`. Recursive traversal and alternate name encodings are not implemented.

~~~js
import {readdirSync, statSync} from "node:fs";
import {join} from "node:path";

const dir = "docs";
console.log(JSON.stringify(readdirSync(dir).map(name => ({
  name,
  directory: statSync(join(dir, name)).isDirectory()
}))));
~~~

The call throws on path or directory-read errors. For promise-based access, use `await readdir(path, options)` from `node:fs/promises`; the same name/Dirent selection applies.

## statSync

Syntax: statSync(path) -> FileInfo. It follows symbolic links and throws if the target cannot be inspected. The supported result provides size, mode, mtimeMs, mtime, isFile(), isDirectory(), and isSymbolicLink(). size is bytes. mode uses canonical POSIX file-type, permission, and special bits on every platform; extract permissions with `mode & 0o777`. mtime is a JavaScript Date, while mtimeMs is Unix epoch milliseconds and may retain fractional milliseconds. `mtime.getTime()` is an integer and truncates fractional milliseconds. The Node options object is not implemented.

~~~js
import {statSync} from "node:fs";

const info = statSync("README.md");
console.log(JSON.stringify({bytes: info.size, file: info.isFile()}));
~~~

Use lstatSync when the symbolic link itself matters. Use await stat(path) from node:fs/promises for the same result shape asynchronously.

## lstatSync

Syntax: lstatSync(path) -> FileInfo. It has the same fields and methods as statSync but describes the symbolic link itself rather than following its target. Call info.isSymbolicLink() to distinguish a link. It throws on path or metadata-read errors.

## readFileSync

Syntax: readFileSync(path, encoding?) -> string | Uint8Array. It returns bytes by default; a non-empty encoding requests decoded text.

~~~js
import { readFileSync } from "node:fs";

const text = readFileSync("README.md", "utf8");
~~~

The call throws on path or filesystem errors.

## writeFileSync

Syntax: writeFileSync(path, data) -> undefined. It creates or truncates the destination. Strings are written as UTF-8; byte arrays are written as bytes.

~~~js
import { writeFileSync } from "node:fs";

writeFileSync("notes.txt", "ready\\n");
~~~

The call throws on a read-only filesystem, invalid path, or write error.

## appendFileSync

Syntax: appendFileSync(path, data) -> undefined. It creates the destination if needed and appends string or byte data.

~~~js
import { appendFileSync } from "node:fs";

appendFileSync("notes.txt", "next\\n");
~~~

The call throws on filesystem errors.

## createReadStream

Syntax: createReadStream(path, options = {}) -> ReadableStream<Uint8Array>. Supported range options are start (default 0) and end (inclusive). It reads chunks of at most 64 KiB as the consumer asks. Read with a reader or for-await; cancelling closes the file.

~~~js
import { createReadStream } from "node:fs";

for await (const chunk of createReadStream("large.bin", { start: 0, end: 127 })) {
  console.log(chunk.byteLength);
}
~~~

The range is byte-based. The stream reports open/read errors through its error path. Chunks remain bytes; Node stream events are not implemented.

## createWriteStream

Syntax: createWriteStream(path, options = {}) -> WritableStream<Uint8Array | string>. The supported option is flags; "a" and "as" append, while other values create or truncate. The writer accepts text or bytes and the file is closed on stream close or abort.

~~~js
import { createWriteStream } from "node:fs";

const writer = createWriteStream("out.bin").getWriter();
try {
  await writer.write(new TextEncoder().encode("data"));
  await writer.close();
} finally {
  writer.releaseLock();
}
~~~

A write failure rejects the write/close promise.

### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.


## Dirent

`readdirSync(path, {withFileTypes: true})` and promise `readdir` return instances of the `Dirent` class exported by `node:fs` and `fs`. Each entry has `name` and `parentPath`; the parent path is the path supplied to the listing operation. Methods `isFile()`, `isDirectory()`, `isSymbolicLink()`, `isBlockDevice()`, `isCharacterDevice()`, `isFIFO()`, and `isSocket()` describe the directory entry without following its final symlink.

~~~js
import {readdirSync} from "node:fs";

for (const entry of readdirSync(".", {withFileTypes: true})) {
  console.log(entry.name, entry.isDirectory(), entry.isSymbolicLink());
}
~~~
