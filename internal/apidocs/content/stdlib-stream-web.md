# node:stream/web

### API information

Reference: `node:stream/web`; aliases: `stream/web`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/streams.go. Reading this reference evaluates no target module and makes no provider request.

Exports Web Streams classes and strategies, including text encoding/decoding streams. They are used by node:fs file streams, fetch response bodies, and process standard streams.

### Exports

`ByteLengthQueuingStrategy`, `CountQueuingStrategy`, `ReadableStream`, `ReadableStreamDefaultController`, `ReadableStreamDefaultReader`, `TextDecoderStream`, `TextEncoderStream`, `TransformStream`, `WritableStream`, `WritableStreamDefaultWriter`, `default`

The implementation provides default readers and writers, not BYOB readers. Stream errors reject read/write/pipe promises.

## ReadableStream

Create a readable stream with an underlying source. Its start, pull, and cancel methods produce chunks, respond to demand, and release source resources. Consumers can use async iteration, pipeTo, or a reader.

~~~js
import { ReadableStream } from "node:stream/web";

const stream = new ReadableStream({
  start(controller) {
    controller.enqueue(new TextEncoder().encode("hello"));
    controller.close();
  },
});
~~~

## ReadableStream.getReader

stream.getReader() -> ReadableStreamDefaultReader locks the stream to one reader. reader.read() resolves to {value, done}; reader.cancel(reason) cancels the source; reader.releaseLock() unlocks it when no read is pending.

~~~js
const stream = new ReadableStream({
  start(controller) {
    controller.enqueue(new TextEncoder().encode("hello"));
    controller.close();
  },
});
const reader = stream.getReader();
try {
  const { value, done } = await reader.read();
  if (!done) console.log(new TextDecoder().decode(value));
} finally {
  reader.releaseLock();
}
~~~

Do not use a reader and async iteration on the same locked stream.

## WritableStream

Create a writable stream with an underlying sink. Its write, close, and abort hooks accept writes, finish the sink, and stop it after failure.

~~~js
import { WritableStream } from "node:stream/web";

const chunks = [];
const sink = new WritableStream({
  write(chunk) { chunks.push(chunk); },
});
~~~

## WritableStream.getWriter

stream.getWriter() -> WritableStreamDefaultWriter locks the stream. writer.write(chunk), writer.close(), and writer.abort(reason) return promises; writer.releaseLock() unlocks it after pending operations settle.

~~~js
const sink = new WritableStream({
  write(chunk) { console.log(chunk); },
});
const writer = sink.getWriter();
try {
  await writer.write("hello");
  await writer.close();
} finally {
  writer.releaseLock();
}
~~~

A writer owns the stream lock until released. Closing or aborting the writer also settles the stream's closed promise.

## TransformStream

TransformStream(transformer?, writableStrategy?, readableStrategy?) connects a writable side to a readable side. The transformer can provide start, transform, flush, and cancel hooks. Each transform receives an input chunk and a controller with enqueue, error, terminate, and desiredSize.

The readable highWaterMark defaults to 0. Writes wait for output demand, and resume when reads make room according to the readable strategy. TextEncoderStream, TextDecoderStream, and CompressionStream use the same flow control. Consume output concurrently with writes; pipeThrough and pipeTo arrange this automatically.

~~~js
const upper = new TransformStream({
  transform(chunk, controller) { controller.enqueue(chunk.toUpperCase()); },
});
await ReadableStream.from(["hello", "world"])
  .pipeThrough(upper)
  .pipeTo(new WritableStream({write(chunk) { console.log(chunk); }}));
~~~

Readable cancellation or writable abort calls the transformer's cancel hook. Cancellation, errors, and termination release writes waiting for output demand. Callback failures propagate to both sides; termination closes the readable side and errors the writable side.

The highWaterMark controls when the next transform may run. A transform or flush callback can enqueue multiple chunks at once, and callers can queue writes without awaiting them, so it is not a total memory limit.

### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
