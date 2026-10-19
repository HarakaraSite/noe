# Global APIs

This page lists globals provided by the pinned go-quickjs engine and the NOE-maintained standard library. Global names do not have a module default export. Core ECMAScript globals are listed for discovery only; this page does not attempt to document the full JavaScript language reference.

### NOE and Node-style globals

console writes to the run's configured stdout/stderr. process is the NOE process object described in node:process. crypto and performance are also globals. Buffer is also available from node:buffer. fetch, Request, Response, and Headers use the Web request/response shape. serve starts an HTTP server. WebSocket opens a WebSocket client; upgradeWebSocket upgrades a request handled by serve. Worker starts a local Worker runtime. Callback timers are setTimeout, setInterval, clearTimeout, and clearInterval; queueMicrotask schedules a microtask. setImmediate is exported by node:timers, not installed globally.

### Web API globals

AbortController, AbortSignal, Blob, BroadcastChannel, ByteLengthQueuingStrategy, CompressionStream, CountQueuingStrategy, CryptoKey, DOMException, DecompressionStream, ErrorEvent, Event, EventTarget, File, FormData, Headers, MessageChannel, MessageEvent, MessagePort, ReadableStream, ReadableStreamDefaultController, ReadableStreamDefaultReader, Request, Response, TextDecoder, TextDecoderStream, TextEncoder, TextEncoderStream, TransformStream, URL, URLPattern, URLSearchParams, WebSocket, Worker, WritableStream, and WritableStreamDefaultWriter.

### ECMAScript and QuickJS globals

The pinned QuickJS runtime also exposes these language and host globals: AggregateError, Array, ArrayBuffer, AsyncDisposableStack, Atomics, BigInt, BigInt64Array, BigUint64Array, Boolean, DataView, Date, DisposableStack, Error, EvalError, FinalizationRegistry, Float16Array, Float32Array, Float64Array, Function, Infinity, Int16Array, Int32Array, Int8Array, Intl, Iterator, JSON, Map, Math, NaN, Number, Object, Promise, Proxy, RangeError, ReferenceError, Reflect, RegExp, Set, ShadowRealm, SharedArrayBuffer, String, SuppressedError, Symbol, SyntaxError, Temporal, TypeError, URIError, Uint16Array, Uint32Array, Uint8Array, Uint8ClampedArray, WeakMap, WeakRef, WeakSet, atob, btoa, clearInterval, clearTimeout, console, decodeURI, decodeURIComponent, encodeURI, encodeURIComponent, escape, eval, globalThis, isFinite, isNaN, parseFloat, parseInt, queueMicrotask, setInterval, setTimeout, structuredClone, undefined, and unescape.

### Exact globalThis inventory

`AbortController`, `AbortSignal`, `AggregateError`, `Array`, `ArrayBuffer`, `AsyncDisposableStack`, `Atomics`, `BigInt`, `BigInt64Array`, `BigUint64Array`, `Blob`, `Boolean`, `BroadcastChannel`, `Buffer`, `ByteLengthQueuingStrategy`, `CompressionStream`, `CountQueuingStrategy`, `CryptoKey`, `DOMException`, `DataView`, `Date`, `DecompressionStream`, `DisposableStack`, `Error`, `ErrorEvent`, `EvalError`, `Event`, `EventTarget`, `File`, `FinalizationRegistry`, `Float16Array`, `Float32Array`, `Float64Array`, `FormData`, `Function`, `Headers`, `Infinity`, `Int16Array`, `Int32Array`, `Int8Array`, `Intl`, `Iterator`, `JSON`, `Map`, `Math`, `MessageChannel`, `MessageEvent`, `MessagePort`, `NaN`, `Number`, `Object`, `Promise`, `Proxy`, `RangeError`, `ReadableStream`, `ReadableStreamDefaultController`, `ReadableStreamDefaultReader`, `ReferenceError`, `Reflect`, `RegExp`, `Request`, `Response`, `Set`, `ShadowRealm`, `SharedArrayBuffer`, `String`, `SuppressedError`, `Symbol`, `SyntaxError`, `Temporal`, `TextDecoder`, `TextDecoderStream`, `TextEncoder`, `TextEncoderStream`, `TransformStream`, `TypeError`, `URIError`, `URL`, `URLPattern`, `URLSearchParams`, `Uint16Array`, `Uint32Array`, `Uint8Array`, `Uint8ClampedArray`, `WeakMap`, `WeakRef`, `WeakSet`, `WebSocket`, `Worker`, `WritableStream`, `WritableStreamDefaultWriter`, `atob`, `btoa`, `clearInterval`, `clearTimeout`, `console`, `crypto`, `decodeURI`, `decodeURIComponent`, `encodeURI`, `encodeURIComponent`, `escape`, `eval`, `fetch`, `globalThis`, `isFinite`, `isNaN`, `parseFloat`, `parseInt`, `performance`, `process`, `queueMicrotask`, `serve`, `setInterval`, `setTimeout`, `structuredClone`, `undefined`, `unescape`, `upgradeWebSocket`

## ReadableStream

new ReadableStream(underlyingSource = {}, strategy = {}) creates a stream. Use source start(controller), pull(controller), and cancel(reason) methods to produce chunks, respond to demand, and release upstream resources. Consumers choose one reading mode: async iteration, piping, or an acquired reader.

~~~js
const stream = new ReadableStream({
  start(controller) {
    controller.enqueue(new TextEncoder().encode("hello"));
    controller.close();
  },
});
for await (const chunk of stream) {
  console.log(new TextDecoder().decode(chunk));
}
~~~

A stream error rejects pending and later reads. cancel(reason) asks the source to stop and releases queued data.

## ReadableStream.getReader

stream.getReader() -> ReadableStreamDefaultReader locks the stream to that reader. reader.read() resolves to {value, done}; reader.cancel(reason) cancels the source; reader.releaseLock() returns ownership when no read is pending.

~~~js
const stream = new ReadableStream({
  start(controller) {
    controller.enqueue(new Uint8Array([1, 2, 3]));
    controller.close();
  },
});
const reader = stream.getReader();
try {
  const { value, done } = await reader.read();
  if (!done) console.log(value.byteLength);
} finally {
  reader.releaseLock();
}
~~~

A stream can have only one active reader. Do not iterate it while a reader lock is held.

## WritableStream

new WritableStream(underlyingSink = {}, strategy = {}) creates a destination. Implement sink write(chunk), close(), and abort(reason) methods to handle data and release resources.

~~~js
const output = [];
const stream = new WritableStream({
  write(chunk) { output.push(chunk); },
});
~~~

## WritableStream.getWriter

stream.getWriter() -> WritableStreamDefaultWriter locks the stream. writer.write(chunk), writer.close(), and writer.abort(reason) return promises; call writer.releaseLock() after outstanding operations finish.

~~~js
const stream = new WritableStream({
  write(chunk) { console.log(chunk); },
});
const writer = stream.getWriter();
try {
  await writer.write("hello");
  await writer.close();
} finally {
  writer.releaseLock();
}
~~~

The writer owns the stream until released. A rejected write propagates through the writer's promises.

## fetch

For explicit imports, use `import { fetch } from "noe:http"`. It is the same function as this global.

Syntax: fetch(input, init = {}) -> Promise<Response>. input may be a URL/string or Request; init supports method, headers, body, and signal. The promise rejects for network, permission, or abort errors. HTTP error status codes still resolve to a Response; inspect response.ok or response.status. Consume the body once with text(), json(), arrayBuffer(), or bytes(), or read response.body incrementally.

~~~js
const response = await fetch("https://example.com/data");
if (!response.ok) throw new Error("HTTP " + response.status);
const data = await response.json();
~~~

NOE enables outbound fetch with its host HTTP client. A ReadableStream request body is sent incrementally and must yield Uint8Array chunks; empty chunks are skipped. The next read waits until the current chunk has been written to the request body. This bounds the NOE upload bridge to one in-flight chunk, not queues in the source stream, HTTP transport, operating system, or peer. Upload bodies are one-shot: an already-used or locked stream is rejected before sending. Passing a Request to fetch consumes the selected body; constructing a Request from an unused Request transfers its body. Use clone() explicitly before consumption when two branches are needed.

The 32 MiB limit applies to response-body reads, not uploads. AbortSignal cancels an in-progress upload or response-body read. If final response headers arrive before upload EOF, NOE stops the unfinished upload and returns a readable Response; this does not provide general full-duplex HTTP. A stream body is not replayed for a 307 or 308 redirect; the host client may follow 301, 302, or 303 by continuing with a bodyless request. Other redirect and cache behavior follows the host client; broader Node/browser redirect and cache options are not implemented.

## Request

new Request(input, init = {}) constructs request data for fetch or an HTTP handler. input may be a URL/string or another Request. Use method, headers, body, and signal as needed.

~~~js
const request = new Request("https://example.com/items", {
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ name: "sample" }),
});
~~~

Bodies are one-shot. Methods such as text() and json() return promises; clone the request before consuming if another read is needed. Constructing a new Request from an unused Request transfers its body, so the original cannot consume that body. A stream body must be unlocked and unused when fetch starts. Producer failures abort the request; a race with response delivery may surface as a response-body read failure. Late errors from an intentionally stopped producer are ignored.

## Response

new Response(body = null, init = {}) creates response data. init supports status, statusText, and headers. Response.json(value, init?) creates a JSON response. Read the body with text(), json(), arrayBuffer(), or bytes(); body is a ReadableStream when present.

~~~js
const response = Response.json({ ok: true }, { status: 201 });
console.log(response.status, await response.json());
~~~

Body consumption is one-shot. For a stream body, closing or cancelling its consumer controls when upstream data finishes or is released.

## serve

For explicit imports, use `import { serve } from "noe:http"`. It is the same function as this global.

Syntax: serve(options, handler) -> server, or serve(handler) -> server. options supports hostname (default 127.0.0.1) and port (default 0, asking the OS for a free port); options.fetch may supply the handler. The handler receives a Web Request and must return a Response, synchronously or as a promise. The server has hostname, port, url, and close().

~~~js
const server = serve({ port: 0 }, async (request) => {
  const { pathname } = new URL(request.url);
  return new Response(pathname);
});
try {
  console.log(server.url);
} finally {
  server.close();
}
~~~

Serving uses NOE's event loop and keeps the run alive until close() or runtime shutdown. The handler receives the Request after headers arrive, before body EOF. GET and HEAD requests have no JavaScript body. For other requests, `request.body` is null when the HTTP server knows there is no body; otherwise it is a ReadableStream, even when an unknown-length body is empty. Its source has a high-water mark of zero and reads a bounded chunk only when JavaScript pulls. The bridge does not read ahead without demand; transport, operating-system, and tee/clone buffering are outside this bound.

In Go `Serve` configuration, `MaxBodyBytes: 0` means the 32 MiB default, a positive value returns only that prefix followed by normal EOF, and a negative value gives JavaScript an empty body. Reaching the cap does not produce a 413 response or change the received `Content-Length` header. This setting is not a JavaScript option. Request body convenience methods use the shared one-shot rules and collect the bytes they consume; use `request.body` and a reader for incremental processing. A malformed or truncated body discovered after dispatch rejects a body read. A caught error can be used to choose a response if the connection remains usable; an uncaught handler error follows the existing `onError`/500 path.

The incoming body remains usable while a returned Response stream is being sent, so the response producer may read `request.body`. Once sending finishes or fails, remaining input is closed. Cancelling the body closes the JavaScript stream and prevents new reads, but it does not cancel response production; a native read already in progress may be released when response sending finishes. With `Expect: 100-continue`, the server sends `100 Continue` before reading body bytes. For a streaming Response, NOE waits until final headers flush before its response pump requests a chunk. A zero-high-water-mark producer such as `new Response(request.body)` can therefore wait for that flush; if the handler cancels a pending body read, NOE interrupts it before flushing final headers. The gate does not stop a producer's `start()`, its own early pull, or another reader started by the handler, so such a read may delay final headers while it waits for input. This ordering does not provide general full-duplex HTTP; `fetch()` still stops an unfinished upload when final response headers arrive.

Handler failures produce a 500 response; options.onError may return a Response. The default bind address is loopback; hostname can choose another interface. `close()` is synchronous and idempotent, waits up to five seconds for graceful shutdown, and then force-closes remaining HTTP connections. Runtime shutdown also force-closes the listener and its active connections.

## WebSocket

Syntax: new WebSocket(url, protocols?). It opens a WebSocket connection, reports open/message/error/close events, and sends text or binary messages with send(data). close(code = 1000, reason = "") starts closing. The default binaryType is arraybuffer.

~~~js
const socket = new WebSocket("wss://example.com/feed");
socket.addEventListener("open", () => socket.send("hello"));
socket.addEventListener("message", (event) => console.log(event.data));
~~~

A WebSocket carries complete protocol messages, not raw TCP bytes. NOE enables outbound WebSockets. For a server, use serve with upgradeWebSocket(request) and return the provided 101 Response; the helper requires an HTTP server in the same runtime. Close the socket when it is no longer needed.

## AbortController

new AbortController() creates a controller with a signal. Call abort(reason?) once to mark it aborted, set its reason, and dispatch an abort event. Pass the signal to fetch or another API that accepts one.

~~~js
const controller = new AbortController();
const pending = fetch("https://example.com/large", { signal: controller.signal });
controller.abort();
~~~

An aborted fetch rejects with the signal's reason; aborting after completion does not change the completed operation.

## AbortSignal

AbortSignal exposes aborted, reason, throwIfAborted(), and the static helpers AbortSignal.abort(reason?) and AbortSignal.timeout(milliseconds). Pass signals to APIs such as fetch and node:timers/promises.

~~~js
const signal = AbortSignal.timeout(1_000);
const response = await fetch("https://example.com/slow", { signal });
~~~

The timeout helper requires the runtime timer loop; NOE provides it. A timeout is delivered as an Error reason.

## setTimeout

Syntax: setTimeout(callback, milliseconds = 0, ...args) -> number. It invokes the callback on the runtime event loop and returns a numeric timer ID. Additional arguments are passed to the callback. Delays outside the accepted range run as soon as possible.

~~~js
const id = setTimeout((value) => console.log(value), 10, "ready");
~~~

A scheduled timer keeps the runtime active. Call clearTimeout(id) to cancel it; clearInterval cancels an interval.

## setInterval

Syntax: setInterval(callback, milliseconds = 0, ...args) -> number. It repeats the callback on the event loop until cleared. Very short intervals are clamped to at least one millisecond.

~~~js
const id = setInterval(() => console.log("tick"), 1_000);
clearInterval(id);
~~~

## clearTimeout

clearTimeout(id) cancels a pending timeout; clearInterval(id) cancels a pending interval. The functions are interchangeable for timer IDs. Timers are owned by the runtime and stop when it closes.

~~~js
const timeout = setTimeout(() => console.log("not run"), 1_000);
clearTimeout(timeout);
~~~

### Source basis

NOE enables Process, FS, Fetch, Serve, WebSockets, Workers, and a Loop in internal/runtime/stdlib.go. Standard-library implementations are in internal/stdlib (webapis.go, streams.go, fetch.go, serve.go, websocket.go, timers.go, worker.go), based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib; see internal/stdlib/UPSTREAM.md for origin and local changes. The inventory above was confirmed with Object.getOwnPropertyNames(globalThis).
