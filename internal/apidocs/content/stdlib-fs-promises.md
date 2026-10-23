# node:fs/promises

### API information

Reference: `node:fs/promises`; aliases: `fs/promises`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.FS.

Public-definition source: internal/stdlib/fs.go. Reading this reference evaluates no target module and makes no provider request.

Provides promise-returning whole-file and metadata operations. Every listed export is present on both the module namespace and its default object. readFile returns bytes unless an encoding is supplied; write operations accept strings or byte data.

### Exports

`appendFile`, `copyFile`, `default`, `lstat`, `mkdir`, `mkdtemp`, `readFile`, `readdir`, `readlink`, `realpath`, `rename`, `rm`, `rmdir`, `stat`, `unlink`, `writeFile`

These are ordinary promises, not Node callback APIs. Use node:fs for synchronous methods and Web Streams, and noe:fs-handle for position-based access to an open file.

Directory listings return string arrays. The Node withFileTypes option and Dirent objects are not implemented; use stat/lstat when entry metadata is needed.

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
