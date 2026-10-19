# node:fs

Reads and writes files synchronously or as Web Streams. The module exposes synchronous file helpers, a promises namespace, and createReadStream/createWriteStream. Stream values are Web Streams, not Node streams: there are no Node-style stream events or pipe() methods.

### Exports

`appendFileSync`, `chmodSync`, `copyFileSync`, `createReadStream`, `createWriteStream`, `default`, `existsSync`, `lstatSync`, `mkdirSync`, `mkdtempSync`, `promises`, `readFileSync`, `readdirSync`, `readlinkSync`, `realpathSync`, `renameSync`, `rmSync`, `rmdirSync`, `statSync`, `unlinkSync`, `writeFileSync`

Use node:fs for whole-file operations or sequential Web Streams. Use noe:fs-handle for position-based I/O on an open file; these APIs serve different needs. A stream closes when it completes, is cancelled/aborted, or its runtime closes.

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

A write failure rejects the write/close promise. noe:fs-handle is the separate API for explicit offset reads and writes.

### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
