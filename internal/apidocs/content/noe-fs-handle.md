# `noe:fs-handle`

`noe:fs-handle` exports `open` for positional byte I/O on an existing local file. Use standard `node:fs` for whole-file and sequential stream operations; `noe:fs-handle` provides position-based `readAt` and `writeAt`, not a replacement for the standard filesystem API. It is available in the main runtime and Workers. An open handle alone does not keep an evaluation alive; the runtime owns and closes it at shutdown.

## open

```js
import { open } from "noe:fs-handle";
```

`open(path, {mode = "r"} = {}) -> Promise<FileHandle>` opens an existing file. `path` must be a nonempty string without NUL and is resolved relative to the runtime working directory. `mode` is `"r"` (read-only, default) or `"r+"` (read/write). Neither mode creates or truncates a file.

Invalid paths or modes reject with `Error.code === "ERR_INVALID_ARG"`; filesystem failures reject with an OS error code or `ERR_IO`. Closing the evaluation reclaims handles, but explicitly call `close()` when finished.

```js
import { open } from "noe:fs-handle";

const file = await open("data.bin", {mode: "r+"});
try {
  const {data, bytesRead, eof} = await file.readAt(0, 16);
  console.log(bytesRead, eof, data);
} finally {
  await file.close();
}
```

## FileHandle.readAt

`file.readAt(position, length) -> Promise<{data, bytesRead, eof}>` reads up to `length` bytes starting at the zero-based byte `position`; it does not change a shared cursor. Both arguments must be nonnegative safe integers. `data` is a `Uint8Array`, `bytesRead` is its byte length, and `eof` reports whether the read observed EOF (typically because fewer than `length` bytes remained). A zero length returns an empty byte array without reading.

On a non-EOF I/O error, the rejected error may include partial `data` and `bytesRead`. Invalid integers, unrepresentable sizes, or offset overflow reject as `ERR_INVALID_ARG`. Operations after close reject as `ERR_FILE_CLOSED`. Ordinary OS file reads are not guaranteed to stop immediately on cancellation.

## FileHandle.writeAt

`file.writeAt(position, bytes) -> Promise<number>` writes the `Uint8Array` at the zero-based byte `position` without changing a shared cursor and resolves to the number of bytes written. Position must be a nonnegative safe integer and the input must be a `Uint8Array`. The handle must have been opened in `"r+"` mode.

Invalid arguments or offset overflow reject as `ERR_INVALID_ARG`; closed handles reject as `ERR_FILE_CLOSED`; other I/O failures use the OS error code or `ERR_IO`. A partial write error may include `bytesWritten`.

## FileHandle.close

`await file.close()` closes the file and waits for in-flight operations. It is idempotent. New reads/writes are rejected as soon as closing begins. Close handles in `finally`; runtime shutdown also closes owned handles without waiting for JavaScript callbacks.
