# `noe:io`

### API information

Public exports: `pipe`, `stderr`, `stdin`, `stdout`.

Reference: `noe:io`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.Stdio. Workers receive empty stdin and share the configured output destinations.

Public-definition source: internal/nativeio/host.go, internal/nativeio/js/streams.js. Reading this reference evaluates no target module and makes no provider request.

`noe:io` exports host `stdin`, `stdout`, and `stderr` as Web Streams of `Uint8Array`, plus a cancellation-aware `pipe` function. It is available in the main runtime and Workers. These are byte streams; encode and decode text explicitly. Importing `stdin` does not start reading.

The main runtime's input comes from its configured host input. Workers receive empty `stdin` (immediate EOF), rather than sharing the process input. Worker `stdout` and `stderr` use the host output destinations; send input to a Worker through messages when needed.

## stdin

`stdin` is a `ReadableStream<Uint8Array>`. Read it with a reader, `Response`, or `pipe`. Reading is demand-driven. The stream is owned by the runtime; cancellation releases its reader endpoint.

In Workers, this stream is empty and resolves to EOF. Reading it does not consume the main runtime's input.

## stdout

`stdout` is a `WritableStream<Uint8Array>`. Use a writer or `pipe` to send bytes to the host's standard output. Closing the stream closes this endpoint for the current runtime.

## stderr

`stderr` is a `WritableStream<Uint8Array>` that writes to the host's standard error. It has the same byte and ownership behavior as `stdout`.

## pipe

`pipe(source, destination, {signal?, preventClose = false} = {}) -> Promise<void>` reads from a Web `ReadableStream` and writes each chunk to a Web `WritableStream`. On source EOF it closes the destination unless `preventClose` is true. It acquires and releases reader/writer locks during the operation.

If `signal` aborts, `pipe` starts both source cancellation and destination abort, then rejects with `signal.reason` or an abort error. Read/write failures also cancel the source and abort the destination before rejection. This explicit cancellation handles pending I/O on both sides; the pinned standard-library `pipeTo` abort-signal path may wait for more input in some cases.

```js
import { stdin, stdout, pipe } from "noe:io";

await pipe(stdin, stdout, {preventClose: true});
```

`pipe` requires readable/writable stream objects and a valid `AbortSignal` when one is supplied. Errors from stream reads, writes, close, or cancellation reject the Promise. Owner shutdown reclaims the host endpoints.
