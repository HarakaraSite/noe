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

Directory listings return string arrays. The Node withFileTypes option and Dirent objects are not implemented; use stat/lstat when entry metadata is needed.

## open

Syntax: `open(path, flags = "r") -> Promise<FileHandle>`. It opens an existing file. The supported flags are `"r"` for reading and `"r+"` for reading and writing. It does not create or truncate files. Paths are resolved against the execution's working directory and follow its selected filesystem root and read-only setting.

Invalid paths or flags reject with `code: "ERR_INVALID_ARG"`. A read/write open on a read-only filesystem rejects with `code: "ERR_READ_ONLY"`. OS errors retain their numeric errno; other I/O failures use `code: "ERR_IO"`.

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

Invalid arguments reject with `code: "ERR_INVALID_ARG"`; reading a closed handle rejects with `code: "ERR_FILE_CLOSED"`. On a partial read failure, the error exposes `bytesRead` and the original `buffer`, with bytes already read copied into it. OS errors retain their numeric errno; other I/O failures use `code: "ERR_IO"`.

## FileHandle.write

Syntax: `file.write(buffer, offset?, length?, position?)`, `file.write(buffer, {offset?, length?, position?})`, or `file.write(text, position?, encoding?)`.

The buffer form accepts any `ArrayBufferView`, including `Uint8Array` and `DataView`. `offset` defaults to `0`; `length` defaults to the remaining bytes in the buffer after `offset`. The input view is copied when `write()` is called, so later caller changes do not change the pending write. The string form accepts UTF-8 encodings `"utf8"` and `"utf-8"` (default `"utf8"`). An omitted or `null` `position` writes at the current file cursor; a nonnegative safe integer writes at that byte position without changing the cursor. The handle must have been opened with `"r+"`. It resolves to `{bytesWritten, buffer}`, preserving the original view or string in the result.

Invalid arguments reject with `code: "ERR_INVALID_ARG"`; writing through a closed handle rejects with `code: "ERR_FILE_CLOSED"`. A write through an `"r"` handle retains the underlying OS error. `code: "ERR_READ_ONLY"` applies when the selected filesystem policy rejects opening with `"r+"`. On a partial write failure, the error exposes `bytesWritten` and the original `buffer`. OS errors retain their numeric errno; other I/O failures use `code: "ERR_IO"`.

## FileHandle.close

`file.close() -> Promise<void>` is idempotent. It immediately rejects new I/O and normally waits for I/O that already started. Runtime cleanup force-closes remaining handles without waiting for JavaScript promise delivery.

## readdir

Syntax: readdir(path) -> Promise<string[]>. It returns sorted entry names without the parent path. It accepts only a path; withFileTypes, encoding, and recursive options are not implemented.

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

Each name is a string, so use it directly instead of name.name. The promise rejects on path or directory-read errors.

## stat

Syntax: stat(path) -> Promise<object>. It follows symbolic links. The result has size (bytes), mode (permission bits), mtimeMs (Unix epoch milliseconds), mtime (timestamp string), isFile(), isDirectory(), and isSymbolicLink(). It is the same shape as node:fs statSync, including the string mtime rather than a Date object. The Node options object is not implemented. The promise rejects on metadata-read errors.

~~~js
import {stat} from "node:fs/promises";

const info = await stat("README.md");
console.log(JSON.stringify({bytes: info.size, file: info.isFile()}));
~~~

## lstat

Syntax: lstat(path) -> Promise<object>. It has the same result shape as stat but inspects the symbolic link itself instead of following its target. Call info.isSymbolicLink() to distinguish a link. The promise rejects on metadata-read errors.

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
