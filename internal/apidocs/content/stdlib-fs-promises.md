# node:fs/promises

Provides promise-returning whole-file and metadata operations. Every listed export is present on both the module namespace and its default object. readFile returns bytes unless an encoding is supplied; write operations accept strings or byte data.

### Exports

`appendFile`, `copyFile`, `default`, `lstat`, `mkdir`, `mkdtemp`, `readFile`, `readdir`, `readlink`, `realpath`, `rename`, `rm`, `rmdir`, `stat`, `unlink`, `writeFile`

These are ordinary promises, not Node callback APIs. Use node:fs for synchronous methods and Web Streams, and noe:fs-handle for position-based access to an open file.

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

Pinned implementation: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
