# noe:fs

### API information

Reference: `noe:fs`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe the exported surface, not whether a Profile installed filesystem access. Detailed headings below are documentation entries, not a second export inventory.

Availability: Requires Profile.Modules.FS.

Public-definition source: internal/stdlib/fs.go, filehandle.js, fsstream.go, fs_directory.go, fs_directory.js, fs_watch.go, and fs_watch.js. Reading this reference evaluates no target module and makes no provider request.

`noe:fs` provides asynchronous operations as named exports, explicit `*Sync` helpers, open file handles, asynchronous directory/watch iterators, and Web Streams. Its named functions and `Dirent` value are also available on the default object. The module has no `promises` property. The separate `node:fs`, `node:fs/promises`, `fs`, and `fs/promises` imports are not provided by NOE.

### Exports

`Dirent`, `appendFile`, `appendFileSync`, `chmod`, `chmodSync`, `copyFile`, `copyFileSync`, `createReadStream`, `createWriteStream`, `default`, `existsSync`, `iterateDir`, `lstat`, `lstatSync`, `mkdir`, `mkdirSync`, `mkdtemp`, `mkdtempSync`, `open`, `readFile`, `readFileSync`, `readdir`, `readdirSync`, `readlink`, `readlinkSync`, `realpath`, `realpathSync`, `rename`, `renameSync`, `rm`, `rmSync`, `rmdir`, `rmdirSync`, `stat`, `statSync`, `unlink`, `unlinkSync`, `watch`, `writeFile`, `writeFileSync`

Asynchronous file and metadata operations return promises. `iterateDir` and `watch` return async iterators immediately; native work begins when iteration requests its first item. The selected Root, Cwd, and ReadOnly policy applies consistently to async operations, sync helpers, file handles, and streams. Directory iteration and watching are allowed on a read-only filesystem.

Filesystem errors use symbolic string `code` values such as `ENOENT`, `EEXIST`, and `EBADF`. When available, `errno` is the positive platform OS number, which varies by platform. Errors may include `path`, `dest`, and `syscall`; paths are projected relative to the configured filesystem root. Custom codes include `ERR_INVALID_ARG`, `ERR_READ_ONLY`, `ERR_FILE_CLOSED`, and fallback `ERR_IO`. Partial handle reads expose `data` and `bytesRead`; partial writes expose `bytesWritten`.

## watch

`watch(path, options?) -> WatchIterator<{eventType, filename}>`. It returns an async iterator. Each value has `eventType: "rename" | "change"` and a basename filename (`string`, Buffer with `encoding: "buffer"`, or null). Use `for await` to receive notifications. Breaking the loop calls `return()` and closes observation. The iterator also has `close() -> Promise<void>` and `[Symbol.asyncDispose]() -> Promise<void>` for explicit or `await using` cleanup. Closing settles pending reads as done.

Options and operating-system caveats match the current native backend: nonrecursive file or directory observation, UTF-8 or buffer filenames, `persistent` (default true), `signal`, positive safe-integer `maxQueue` (default 2048), and `overflow: "ignore" | "error"` (default ignore). Ignore mode drops excess queued events and warns when the iterator consumes an event; error mode rejects with `ERR_FS_WATCH_QUEUE_OVERFLOW`. Operating-system queue overflow is a separate error. `recursive: true`, `ignore`, and `throwIfNoEntry: false` are unsupported. Missing targets and registration failures reject `next()` with normal filesystem errors. The target is resolved within the selected Root and Cwd.

Aborting the signal rejects pending reads with an Error named `AbortError`, code `ABORT_ERR`, and the exact original reason in `cause`. A pre-aborted signal rejects the first read without registering a native watch; subsequent reads are done. Synthetic abort events do not cancel observation. Runtime teardown closes native resources without waiting for JavaScript delivery.

~~~js
import {watch} from "noe:fs";

await using changes = watch(".", {signal: AbortSignal.timeout(10000)});
for await (const event of changes) console.log(event.eventType, event.filename);
~~~

## open

Syntax: `open(path, {flags = "r", mode = 0o666} = {}) -> Promise<FileHandle>`. The options argument is an object; positional string flags are rejected. Creation permissions range from `0` through `0o777` and are restricted by the operating system's umask.

| Flags | Access and effect |
| --- | --- |
| `r`, `r+` | Open an existing file for reading or reading and writing. |
| `w`, `w+` | Open for writing or reading and writing; create if missing and truncate if present. |
| `wx`, `wx+` | As `w` or `w+`, but fail if the final entry already exists, including a dangling symlink. |
| `a`, `a+` | Open for appending or reading and appending; create if missing. |
| `ax`, `ax+` | As `a` or `a+`, but fail if the final entry already exists. |

Invalid paths, flags, and modes reject with `code: "ERR_INVALID_ARG"`. A read/write open on a read-only filesystem rejects with `code: "ERR_READ_ONLY"`. Operating-system failures use the shared filesystem error shape above.

An open handle alone does not keep an evaluation alive. Close it when finished; runtime cleanup force-closes remaining handles.

~~~js
import {open} from "noe:fs";

await using file = await open("index.bin", {flags: "r+"});
const header = await file.readAt(0, 16);
console.log(header);
~~~

## FileHandle.read

`file.read(length = 16384) -> Promise<Uint8Array>` reads at the current file cursor. `length` must be a nonnegative safe integer; native limits cap the maximum read size. The returned array contains only bytes read. At EOF it is empty. Sequential reads advance the cursor.

Invalid arguments reject with `code: "ERR_INVALID_ARG"`; reading a closed handle rejects with `code: "ERR_FILE_CLOSED"`. A partial read failure exposes the bytes already read in `error.data` and their count in `error.bytesRead`.

## FileHandle.readAt

`file.readAt(position, length) -> Promise<Uint8Array>` reads from a nonnegative safe-integer byte position without changing the current file cursor. The length has the same validation and native maximum as `read`. EOF returns an empty array. A partial read failure exposes its returned bytes in `error.data` and their count in `error.bytesRead`.

## FileHandle.write

`file.write(data) -> Promise<number>` writes a string as UTF-8 or the bytes in an `ArrayBufferView`. It returns the number of bytes written. The native write takes a snapshot of byte input when called, so later caller changes do not alter a pending write. Sequential writes use the current cursor and advance it; append-mode handles write at EOF.

The handle must have write access. Writing through a closed handle rejects with `code: "ERR_FILE_CLOSED"`. A partial write failure exposes the number of bytes written in `error.bytesWritten`. Operating-system failures use the shared filesystem error shape above.

## FileHandle.writeAt

`file.writeAt(position, data) -> Promise<number>` writes a string as UTF-8 or the bytes in an `ArrayBufferView` at a nonnegative safe-integer byte position, without changing the current file cursor on a non-append handle. It returns the number of bytes written. On append-mode handles, the supplied position is ignored: the write goes to EOF and advances the descriptor cursor. Byte input retains the same native snapshot behavior as `write`; partial failures expose `error.bytesWritten`.

## FileHandle.stat

`file.stat() -> Promise<FileInfo>` returns metadata for the open descriptor. It continues to describe the opened file after the path is renamed, replaced, or unlinked. BigInt metadata and per-operation cancellation options are not implemented.

## FileHandle.truncate

`file.truncate(length = 0) -> Promise<void>` resizes a file opened for writing. The length must be a safe integer; negative values reject with `code: "ERR_INVALID_ARG"`. Shrinking discards trailing bytes; extending fills the new region with zero bytes. It leaves the current file cursor unchanged.

## FileHandle.sync

`file.sync() -> Promise<void>` asks the operating system to flush the open file's data and metadata to storage. It shares the handle's operation lifetime: normal `close()` waits for admitted operations, and new operations reject once close starts. It does not separately flush a containing directory or replace an application's transaction protocol.

## FileHandle.close

`file.close() -> Promise<void>` is idempotent. It immediately rejects new I/O and normally waits for I/O that already started. Runtime cleanup force-closes remaining handles without waiting for JavaScript promise delivery.

## FileHandle[Symbol.asyncDispose]

`file[Symbol.asyncDispose]() -> Promise<void>` calls the existing `close()` method. It waits for admitted operations and closes the handle with the same idempotence and error behavior. Use `await using` to close a file on normal block exit, return, or exception.

~~~js
import {open} from "noe:fs";

{
  await using file = await open("notes.txt", {flags: "a"});
  await file.write("saved\n");
}
~~~

## readdir

`readdir(path, {withFileTypes = false} = {}) -> Promise<string[] | Dirent[]>`. It returns sorted entry names by default. With `withFileTypes: true`, entries are `Dirent` instances with `name`, `parentPath`, and entry-type predicates. Recursive traversal and alternate name encodings are not implemented. The promise rejects on path or directory-read errors.

## readdirSync

`readdirSync(path, {withFileTypes = false} = {}) -> string[] | Dirent[]`. It applies the same sorted names and `Dirent` selection as `readdir`, synchronously.

## iterateDir

`iterateDir(path, {batchSize = 128, signal} = {}) -> DirectoryIterator<Dirent>` returns a lazy async iterator immediately. Opening starts on the first `next()`, and native directory reads run asynchronously. Each value is a `Dirent` with the same type predicates as `readdir(..., {withFileTypes: true})`; `parentPath` is the supplied path. Entries describe symbolic links themselves. Root/Cwd resolution and read-only access use the shared filesystem policy.

Entries follow filesystem order, which may vary and is not sorted. The iterator does not recurse or provide a snapshot: concurrent directory changes may affect which entries are returned. Existing `readdir`/`readdirSync` retain their sorted-array behavior. `batchSize` must be a positive safe integer; it bounds the number of buffered entries, not their byte size or operating-system buffers. Only one batch is requested at a time. Concurrent `next()` calls are serialized, and consumed entries are released from the batch.

The iterator provides `next()`, `return(value?)`, `throw(error?)`, `close() -> Promise<void>`, readonly `closed`, and `[Symbol.asyncDispose]()`. EOF, loop break, explicit close, read failure, signal cancellation, and execution/runtime teardown release the descriptor. A handle alone does not keep an execution alive. `close()` is idempotent, stops further iteration immediately, and waits for admitted native work, including a late open. Pending reads finish as done when explicitly closed. The iterator belongs to its original execution; it cannot continue yielding a buffered batch after that owner has ended.

Arguments/options are checked at creation; paths must be nonempty strings without NUL. Missing paths, non-directories, and native read failures reject `next()` with the shared filesystem error shape, close the iterator, and leave subsequent reads done. A genuine AbortSignal is required when supplied. A pre-aborted signal opens nothing. Abortion closes the iterator and rejects a pending or first subsequent read with the exact signal reason; later reads are done. Synthetic abort events do not cancel it. Arbitrary operating-system filesystem calls may take time to finish during cleanup.

~~~js
import {iterateDir} from "noe:fs";

await using entries = iterateDir(".", {batchSize: 64});
for await (const entry of entries) {
  if (entry.isFile()) console.log(entry.name);
}
~~~

## copyFile

`copyFile(from, to) -> Promise<void>` copies file contents, overwriting an existing destination. New files use mode 0644 subject to umask; an existing destination keeps its mode. The operation uses native Go/OS copying where available and a bounded 32 KiB fallback rather than loading the whole file into memory. Same-file aliases, including hard links, leave contents intact.

Root/Cwd and ReadOnly checks are shared with the other file APIs. Source open/read-directory failures occur before an existing destination is truncated. Copying is not atomic: a failure during copying can leave a partial destination. Concurrent changes to source contents are not a snapshot.

## copyFileSync

`copyFileSync(from, to) -> undefined` performs the same bounded-memory copy synchronously, with the same policy, overwrite, mode, and error behavior.

## stat

`stat(path) -> Promise<FileInfo>` follows symbolic links. The result provides size in bytes, mode, `mtimeMs`, a JavaScript Date `mtime`, and `isFile()`, `isDirectory()`, and `isSymbolicLink()`. Mode uses canonical POSIX file-type, permission, and special bits on every platform; extract permissions with `mode & 0o777`. `mtimeMs` may retain fractional milliseconds; `mtime.getTime()` truncates them. The Node options object is not implemented.

## statSync

`statSync(path) -> FileInfo` returns the same metadata shape as `stat` synchronously. It follows symbolic links and throws if the target cannot be inspected.

## lstat

`lstat(path) -> Promise<FileInfo>` has the same result shape as `stat` but inspects the symbolic link itself. Call `isSymbolicLink()` to distinguish a link.

## lstatSync

`lstatSync(path) -> FileInfo` inspects the symbolic link itself, synchronously.

## readFile

`readFile(path, encoding?) -> Promise<Uint8Array | string>`. It returns bytes by default or decoded text when a nonempty encoding is supplied. It rejects if the file cannot be read.

## readFileSync

`readFileSync(path, encoding?) -> Uint8Array | string` returns bytes by default or decoded text when a nonempty encoding is supplied.

## writeFile

`writeFile(path, data) -> Promise<void>` creates or truncates the file. Strings are written as UTF-8; byte data is written unchanged. It rejects on a read-only filesystem or path/write error.

## writeFileSync

`writeFileSync(path, data) -> undefined` creates or truncates the file synchronously. Strings are written as UTF-8; byte data is written unchanged.

## appendFile

`appendFile(path, data) -> Promise<void>` creates the file if absent, then appends UTF-8 string or byte data. It rejects on filesystem errors.

## appendFileSync

`appendFileSync(path, data) -> undefined` creates the file if absent and appends string or byte data synchronously.

## chmod

`chmod(path, mode) -> Promise<void>` changes file permissions using the same conversion as `chmodSync`: the mode is converted to an integer, then masked with `0o777`. Out-of-range and negative values are not rejected before this mask. Operating-system umask rules apply when files are created, not to this requested mode.

## createReadStream

`createReadStream(path, options = {}) -> ReadableStream<Uint8Array>`. Supported range options are `start` (default 0) and `end` (inclusive). The stream reads chunks of at most 64 KiB as the consumer asks. Read with a reader or for-await; cancelling closes the file. Its range is byte-based, and stream open/read errors use the Web Streams error path. Opening the file currently happens synchronously when `createReadStream` is called.

## createWriteStream

`createWriteStream(path, options = {}) -> WritableStream<Uint8Array | string>`. The supported option is `flags`; `a` and `as` append, while other values create or truncate. It accepts text or bytes and closes the file on stream close or abort. Opening the file currently happens synchronously when `createWriteStream` is called.

## Dirent

`iterateDir` always yields instances of the exported `Dirent` class; `readdir` and `readdirSync` return them when `withFileTypes: true`. Each entry has `name`, `parentPath`, and methods `isFile()`, `isDirectory()`, `isSymbolicLink()`, `isBlockDevice()`, `isCharacterDevice()`, `isFIFO()`, and `isSocket()`. These describe the entry without following its final symbolic link.

### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this NOE-specific module through its standard-library registry. The `node:` prefix remains reserved, but the Node-style filesystem modules are not registered; bare `fs` and `fs/promises` are no longer registered aliases.
