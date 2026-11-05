# node:fs/promises

### API information

Reference: `node:fs/promises`; aliases: `fs/promises`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.FS.

Public-definition source: internal/stdlib/fs.go. Reading this reference evaluates no target module and makes no provider request.

Provides promise-returning file, metadata, and open-handle operations. Every listed export is present on both the module namespace and its default object. readFile returns bytes unless an encoding is supplied; write operations accept strings or byte data.

### Exports

`appendFile`, `copyFile`, `default`, `lstat`, `mkdir`, `mkdtemp`, `open`, `readFile`, `readdir`, `readlink`, `realpath`, `rename`, `rm`, `rmdir`, `stat`, `unlink`, `writeFile`

These are ordinary promises, not Node callback APIs. Use node:fs for synchronous methods and Web Streams, and open from this module for reads and writes through an open file handle. `open` is the same function through `node:fs/promises`, `fs/promises`, `node:fs.promises`, and the default `node:fs` object's `promises` property.

Directory listings return name strings by default. Use `{withFileTypes: true}` for `Dirent` objects with entry-type predicates.

Filesystem errors from synchronous methods, promise methods, FileHandle operations, and Web Streams share one shape. OS errors use symbolic string `code` values such as `ENOENT`, `EEXIST`, and `EBADF`; `errno`, when available, is the original positive platform OS number, which varies by platform. Errors may include `path`, `dest`, and `syscall` when the underlying operation provides them. Paths are projected relative to the configured filesystem root. The custom codes `ERR_INVALID_ARG`, `ERR_READ_ONLY`, `ERR_FILE_CLOSED`, and fallback `ERR_IO` remain available. Partial FileHandle I/O errors also retain `bytesRead` or `bytesWritten` and the original `buffer`.

## open

Syntax: `open(path, flags = "r", mode = 0o666) -> Promise<FileHandle>`. Paths follow the execution's working directory, selected root, and read-only setting. Creation permissions use `mode` (an integer from `0` to `0o777`), restricted by the operating system's umask.

| Flags | Access and effect |
| --- | --- |
| `r`, `r+` | Open an existing file for reading or reading/writing. |
| `w`, `w+` | Open for writing or reading/writing; create if missing, truncate if present. |
| `wx`, `wx+` | As `w`/`w+`, but fail if the final entry already exists, including a dangling symlink. |
| `a`, `a+` | Open for appending or reading/appending; create if missing. |
| `ax`, `ax+` | As `a`/`a+`, but fail if the final entry already exists. |

Append handles always write at the end, including when a position is supplied to `write`. Other string flags and numeric flags are not implemented.

Invalid paths, flags, and creation modes reject with `code: "ERR_INVALID_ARG"`. A read/write open on a read-only filesystem rejects with `code: "ERR_READ_ONLY"`. Operating-system failures use the shared filesystem error shape described above.

An open handle alone does not keep an evaluation alive. Call `close()` when finished; runtime cleanup force-closes any remaining handles.

~~~js
import {open} from "node:fs/promises";

const file = await open("index.bin", "r+");
try {
  const buffer = new Uint8Array(16);
  const {bytesRead} = await file.read(buffer, 0, buffer.byteLength, 0);
  console.log(bytesRead, buffer.subarray(0, bytesRead));
} finally {
  await file.close();
}
~~~

## FileHandle.read

Syntax: `file.read(buffer, offset?, length?, position?)`, `file.read(buffer, {offset?, length?, position?})`, or `file.read({buffer?, offset?, length?, position?} = {})`.

The supplied buffer may be any `ArrayBufferView`, including `Uint8Array` and `DataView`. The options form allocates a `Uint8Array(16384)` when `buffer` is omitted. `offset` defaults to `0`; `length` defaults to the remaining bytes in the buffer after `offset`. An omitted or `null` `position` reads at the current file cursor; a nonnegative safe integer reads from that byte position without changing the cursor. It resolves to `{bytesRead, buffer}`, where `buffer` is the same view passed to the call.

Invalid arguments reject with `code: "ERR_INVALID_ARG"`; reading a closed handle rejects with `code: "ERR_FILE_CLOSED"`. On a partial read failure, the error exposes `bytesRead` and the original `buffer`, with bytes already read copied into it. Operating-system failures use the shared filesystem error shape described above.

## FileHandle.write

Syntax: `file.write(buffer, offset?, length?, position?)`, `file.write(buffer, {offset?, length?, position?})`, or `file.write(text, position?, encoding?)`.

The buffer form accepts any `ArrayBufferView`, including `Uint8Array` and `DataView`. `offset` defaults to `0`; `length` defaults to the remaining bytes in the buffer after `offset`. The input view is copied when `write()` is called, so later caller changes do not change the pending write. The string form accepts UTF-8 encodings `"utf8"` and `"utf-8"` (default `"utf8"`). An omitted or `null` `position` writes at the current file cursor; a nonnegative safe integer writes at that byte position without changing the cursor. The handle must have write access. It resolves to `{bytesWritten, buffer}`, preserving the original view or string in the result.

Invalid arguments reject with `code: "ERR_INVALID_ARG"`; writing through a closed handle rejects with `code: "ERR_FILE_CLOSED"`. A write through an `"r"` handle reports the underlying operating-system error. `code: "ERR_READ_ONLY"` applies when the selected filesystem policy rejects opening with `"r+"`. On a partial write failure, the error exposes `bytesWritten` and the original `buffer`. Operating-system failures use the shared filesystem error shape described above.

## FileHandle.close

`file.close() -> Promise<void>` is idempotent. It immediately rejects new I/O and normally waits for I/O that already started. Runtime cleanup force-closes remaining handles without waiting for JavaScript promise delivery.

## readdir

Syntax: `readdir(path, {withFileTypes = false} = {}) -> Promise<string[] | Dirent[]>`. It returns sorted entry names by default. With `withFileTypes: true`, entries are instances of `node:fs.Dirent`, with `name`, `parentPath`, and entry-type predicates. Recursive traversal and alternate name encodings are not implemented.

~~~js
import {readdir, stat} from "node:fs/promises";
import {join} from "node:path";

const dir = "docs";
const names = await readdir(dir);
const entries = await Promise.all(names.map(async name => ({
  name,
  directory: (await stat(join(dir, name))).isDirectory()
})));
console.log(JSON.stringify(entries));
~~~

Without `withFileTypes`, use each returned name directly. With `withFileTypes: true`, use `entry.name` and methods such as `entry.isDirectory()`. The promise rejects on path or directory-read errors.

## stat

Syntax: stat(path) -> Promise<FileInfo>. It follows symbolic links. The supported result has size (bytes), mode, mtimeMs, mtime, isFile(), isDirectory(), and isSymbolicLink(). mode uses canonical POSIX file-type, permission, and special bits on every platform; extract permissions with `mode & 0o777`. mtime is a JavaScript Date, while mtimeMs is Unix epoch milliseconds and may retain fractional milliseconds. `mtime.getTime()` is an integer and truncates fractional milliseconds. The Node options object is not implemented. The promise rejects on metadata-read errors.

~~~js
import {stat} from "node:fs/promises";

const info = await stat("README.md");
console.log(JSON.stringify({bytes: info.size, file: info.isFile()}));
~~~

## lstat

Syntax: lstat(path) -> Promise<FileInfo>. It has the same result shape as stat but inspects the symbolic link itself instead of following its target. Call info.isSymbolicLink() to distinguish a link. The promise rejects on metadata-read errors.

## readFile

Syntax: readFile(path, encoding?) -> Promise<string | Uint8Array>. It resolves to bytes by default or decoded text when encoding is non-empty. It rejects if the file cannot be read.

~~~js
import { readFile } from "node:fs/promises";

const bytes = await readFile("image.bin");
const text = await readFile("README.md", "utf8");
~~~

## writeFile

Syntax: writeFile(path, data) -> Promise<void>. It creates or truncates the file; strings are UTF-8 and byte data is written unchanged.

~~~js
import { writeFile } from "node:fs/promises";

await writeFile("notes.txt", "saved\\n");
~~~

The promise rejects on a read-only filesystem or path/write error. This implementation accepts path and data; it does not expose the broader Node options object.

## appendFile

Syntax: appendFile(path, data) -> Promise<void>. It creates the file if absent, then appends UTF-8 string or byte data.

~~~js
import { appendFile } from "node:fs/promises";

await appendFile("notes.txt", "more\\n");
~~~

The promise rejects on filesystem errors. These operations have no AbortSignal option.

### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.


## FileHandle.stat

`file.stat() -> Promise<FileInfo>` returns the same supported metadata shape as path `stat`, using the open descriptor. It continues to describe the opened file after the path is renamed, replaced, or unlinked. BigInt metadata and per-operation cancellation options are not implemented.

## FileHandle.truncate

`file.truncate(length = 0) -> Promise<void>` resizes a file opened for writing. The length must be a safe integer; negative values mean zero. Shrinking discards trailing bytes; extending fills the new region with zero bytes. It leaves the current file cursor unchanged.

## FileHandle.sync

`file.sync() -> Promise<void>` asks the operating system to flush the open file's data and metadata to storage. It joins the same operation lifetime as reads, writes, and metadata requests: normal `close()` waits for admitted operations, and all new operations reject once close starts. `sync()` covers that descriptor; it does not separately flush a containing directory or replace an application's transaction protocol.
