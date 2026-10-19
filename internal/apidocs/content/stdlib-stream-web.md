# node:stream/web

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

### Source basis

Pinned implementation: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
