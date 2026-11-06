# Global APIs

### API information

Reference: `globals`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: This is the complete reference, not a report of the current runtime. Globals and host operations depend on Profile.Modules; ECMAScript built-ins come from the pinned engine.

Public-definition source: internal/stdlib/stdlib.go, internal/jsexec/profile.go. Reading this reference evaluates no target module and makes no provider request.

This page lists globals provided by the pinned go-quickjs engine and the NOE-maintained standard library. Global names do not have a module default export. Core ECMAScript globals are listed for discovery only; this page does not attempt to document the full JavaScript language reference.

### NOE and Node-style globals

console writes to the run's configured stdout/stderr. process is the NOE process object described in noe:process. crypto and performance are also globals. Buffer is also available from node:buffer. fetch, Request, Response, and Headers use the Web request/response shape. WebSocket opens a WebSocket client. Worker starts a local Worker runtime. Callback timers are setTimeout, setInterval, clearTimeout, and clearInterval; queueMicrotask schedules a microtask. setImmediate is exported by node:timers, not installed globally.

### Web API globals

AbortController, AbortSignal, Blob, BroadcastChannel, ByteLengthQueuingStrategy, CompressionStream, CountQueuingStrategy, CryptoKey, DOMException, DecompressionStream, ErrorEvent, Event, EventSource, EventTarget, File, FormData, Headers, MessageChannel, MessageEvent, MessagePort, ReadableStream, ReadableStreamDefaultController, ReadableStreamDefaultReader, Request, Response, TextDecoder, TextDecoderStream, TextEncoder, TextEncoderStream, TransformStream, URL, URLPattern, URLSearchParams, WebSocket, Worker, WritableStream, and WritableStreamDefaultWriter.

### ECMAScript and QuickJS globals

The pinned QuickJS runtime also exposes these language and host globals: AggregateError, Array, ArrayBuffer, AsyncDisposableStack, Atomics, BigInt, BigInt64Array, BigUint64Array, Boolean, DataView, Date, DisposableStack, Error, EvalError, FinalizationRegistry, Float16Array, Float32Array, Float64Array, Function, Infinity, Int16Array, Int32Array, Int8Array, Intl, Iterator, JSON, Map, Math, NaN, Number, Object, Promise, Proxy, RangeError, ReferenceError, Reflect, RegExp, Set, ShadowRealm, SharedArrayBuffer, String, SuppressedError, Symbol, SyntaxError, Temporal, TypeError, URIError, Uint16Array, Uint32Array, Uint8Array, Uint8ClampedArray, WeakMap, WeakRef, WeakSet, atob, btoa, clearInterval, clearTimeout, console, decodeURI, decodeURIComponent, encodeURI, encodeURIComponent, escape, eval, globalThis, isFinite, isNaN, parseFloat, parseInt, queueMicrotask, setInterval, setTimeout, structuredClone, undefined, and unescape.

### Exact globalThis inventory

`AbortController`, `AbortSignal`, `AggregateError`, `Array`, `ArrayBuffer`, `AsyncDisposableStack`, `Atomics`, `BigInt`, `BigInt64Array`, `BigUint64Array`, `Blob`, `Boolean`, `BroadcastChannel`, `Buffer`, `ByteLengthQueuingStrategy`, `CompressionStream`, `CountQueuingStrategy`, `CryptoKey`, `DOMException`, `DataView`, `Date`, `DecompressionStream`, `DisposableStack`, `Error`, `ErrorEvent`, `EvalError`, `Event`, `EventSource`, `EventTarget`, `File`, `FinalizationRegistry`, `Float16Array`, `Float32Array`, `Float64Array`, `FormData`, `Function`, `Headers`, `Infinity`, `Int16Array`, `Int32Array`, `Int8Array`, `Intl`, `Iterator`, `JSON`, `Map`, `Math`, `MessageChannel`, `MessageEvent`, `MessagePort`, `NaN`, `Number`, `Object`, `Promise`, `Proxy`, `RangeError`, `ReadableStream`, `ReadableStreamDefaultController`, `ReadableStreamDefaultReader`, `ReferenceError`, `Reflect`, `RegExp`, `Request`, `Response`, `Set`, `ShadowRealm`, `SharedArrayBuffer`, `String`, `SuppressedError`, `Symbol`, `SyntaxError`, `Temporal`, `TextDecoder`, `TextDecoderStream`, `TextEncoder`, `TextEncoderStream`, `TransformStream`, `TypeError`, `URIError`, `URL`, `URLPattern`, `URLSearchParams`, `Uint16Array`, `Uint32Array`, `Uint8Array`, `Uint8ClampedArray`, `WeakMap`, `WeakRef`, `WeakSet`, `WebSocket`, `Worker`, `WritableStream`, `WritableStreamDefaultWriter`, `atob`, `btoa`, `clearInterval`, `clearTimeout`, `console`, `crypto`, `decodeURI`, `decodeURIComponent`, `encodeURI`, `encodeURIComponent`, `escape`, `eval`, `fetch`, `globalThis`, `isFinite`, `isNaN`, `parseFloat`, `parseInt`, `performance`, `process`, `queueMicrotask`, `setInterval`, `setTimeout`, `structuredClone`, `undefined`, `unescape`

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

Syntax: fetch(input, init = {}) -> Promise<Response>. input may be a URL/string or Request; init supports method, headers, body, and signal. The promise rejects for network, permission, or abort errors. HTTP error status codes still resolve to a Response; inspect response.ok or response.status. Consume the body once with text(), json(), arrayBuffer(), or bytes(), or read response.body incrementally.

~~~js
const response = await fetch("https://example.com/data");
if (!response.ok) throw new Error("HTTP " + response.status);
const data = await response.json();
~~~

NOE enables outbound fetch with its host HTTP client. A ReadableStream request body is sent incrementally and must yield Uint8Array chunks; empty chunks are skipped. The next read waits until the current chunk has been written to the request body. This bounds the NOE upload bridge to one in-flight chunk, not queues in the source stream, HTTP transport, operating system, or peer. Upload bodies are one-shot: an already-used or locked stream is rejected before sending. Passing a Request to fetch consumes the selected body; constructing a Request from an unused Request transfers its body. Use clone() explicitly before consumption when two branches are needed.

The configured `MaxBodyBytes` limit applies to response-body reads, not uploads. `0` selects the 32 MiB default; a negative value disables the limit. Reads and body collectors reject if they consume bytes beyond the cap, while a body exactly at the cap succeeds. AbortSignal cancels an in-progress upload or response-body read. If final response headers arrive before upload EOF, NOE stops the unfinished upload and returns a readable Response; this does not provide general full-duplex HTTP. A stream body is not replayed for a 307 or 308 redirect; the host client may follow 301, 302, or 303 by continuing with a bodyless request. Other redirect and cache behavior follows the host client; broader Node/browser redirect and cache options are not implemented.

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

## EventSource

Syntax: `new EventSource(url, {withCredentials: false})`. EventSource receives
UTF-8 server-sent events from an absolute HTTP/HTTPS URL. It is available in
NOE main runtimes, Workers, and agent JavaScript. Embedded hosts select
`Profile.Modules.Fetch`; a Loop is required. No additional Go option is needed.

```js
const source = new EventSource("https://example.com/events");
source.onmessage = event => console.log(event.lastEventId, event.data);
source.addEventListener("update", event => console.log(event.data));
// When the subscription is no longer needed:
source.close();
```

`url`, `withCredentials`, and `readyState` are read-only. The constructor and
instances expose `CONNECTING = 0`, `OPEN = 1`, and `CLOSED = 2`. `onopen`,
`onmessage`, and `onerror` are event-handler properties; named events use
`addEventListener`. Received messages are MessageEvents with string `data`,
`lastEventId`, and the final response URL's `origin`.

Successful responses require HTTP 200 and `text/event-stream`. Clean EOF and
transient transport failures reconnect after 3 seconds unless the stream's
`retry:` field changes the interval. Completed event blocks update the
`Last-Event-ID` sent on reconnection. Other HTTP statuses, including 204,
invalid MIME types, refused host permissions, and parser buffer overflow end
the subscription with `readyState = CLOSED` and an `error` event. Reconnecting
errors have state CONNECTING. Error events expose no response body or HTTP
credentials. `close()` synchronously sets CLOSED, cancels pending connection
or body reads, clears reconnect timers, and emits no close event.

Connections use Fetch's selected HTTP client and request/redirect policy.
The default EventSource client has no total connection timeout; an explicitly
provided client's timeout still applies. `FetchOptions.MaxBodyBytes` bounds
one buffered partial SSE event/line (32 MiB by default, negative disables the
bound), rather than the lifetime stream. The borrowed client is not modified
or closed. Execution shutdown and Worker termination cancel owned requests
and discard pending delivery and reconnect timers.

This is the standard GET-based API: no custom headers or POST body option is
provided. NOE has no document base URL, browser cookie store, or CORS origin
model. `withCredentials: true` enables a Go host's supplied HTTP client cookie
jar; false disables that jar for this subscription. It creates no cookies or
provider authentication automatically. A host's custom transport remains
responsible for its own authentication behavior.

## WebSocket

Syntax: `new WebSocket(url, protocols?)`. It opens a WebSocket connection, reports open/message/error/close events, and sends text or binary messages with `send(data)`. `close(code?, reason?)` starts closing and returns undefined. The default `binaryType` is `"blob"`; set `socket.binaryType = "arraybuffer"` to receive binary messages as ArrayBuffers.

~~~js
const socket = new WebSocket("wss://example.com/feed");
socket.addEventListener("open", () => socket.send("hello"));
socket.addEventListener("message", (event) => console.log(event.data));
~~~

Closing while CONNECTING cancels opening and reports error then an unclean close (1006), with CLOSED visible in both handlers. It cannot later open. Supplied close codes must normalize to 1000 or 3000–4999; invalid codes throw InvalidAccessError. Reasons over 123 UTF-8 bytes throw SyntaxError. Validation precedes state changes, including repeated close calls.

For an established socket, `close()` keeps CLOSING while the native queue flushes and the peer replies. NOE bounds this phase to five seconds. A no-argument call sends an empty close frame; use `close(1000)` to send that explicit status. Close events report the first peer code/reason (1005 for an empty frame, 1006 and an empty reason if none arrived). `wasClean` requires a successful close frame in both directions before native cleanup. Missing replies, invalid close frames, and transport failures are unclean. Native error details remain on the error event. Execution cancellation and Worker termination interrupt blocked I/O independently of graceful close.

A WebSocket carries complete protocol messages, not raw TCP bytes. NOE enables outbound WebSockets when the host selects that capability. For custom handshake headers, import `createWebSocket` from `noe:http`; for server-side upgrades, import `upgradeWebSocket` from `noe:http`. See the `noe:http` reference for their capability requirements and details. Close the socket when it is no longer needed.

## Error.stack

NOE-owned execution runtimes enable source maps by default. A
`//# sourceMappingURL=...` annotation can map runtime Error stacks to original
file names, function names, lines, and columns. Inline application/json data
URLs work without filesystem access; relative local sidecars use the selected
filesystem, including its Root and ReadOnly policy. Maps are loaded lazily and
cached by the engine for that VM. Missing, unreadable, or malformed maps leave
generated-code locations intact. The diagnostic stack is retained after VM
cleanup in CLI and managed agent executions; Worker Error stacks are mapped
before transfer to their parent.

Remote and opaque script IDs support inline maps. External HTTP maps are not
fetched. `Error.prepareStackTrace` still receives generated CallSites, and
syntax-error positions retain the engine's generated location. Source maps
describe already generated JavaScript; they do not execute TypeScript.

## AbortController

new AbortController() creates a controller with a signal. Call abort(reason?) once to mark it aborted, set its reason, and dispatch an abort event. Pass the signal to fetch or another API that accepts one.

~~~js
const controller = new AbortController();
const pending = fetch("https://example.com/large", { signal: controller.signal });
controller.abort();
~~~

An aborted fetch rejects with the signal's reason; aborting after completion does not change the completed operation.

## AbortSignal

AbortSignal exposes aborted, reason, throwIfAborted(), and the static helpers AbortSignal.abort(reason?), AbortSignal.timeout(milliseconds), and AbortSignal.any(signals). Pass signals to APIs such as fetch and node:timers/promises.

~~~js
const signal = AbortSignal.timeout(1_000);
const response = await fetch("https://example.com/slow", { signal });
~~~

The timeout helper requires the runtime timer loop; NOE provides it. A timeout is delivered as an Error reason.

## AbortSignal.any

`AbortSignal.any(signals) -> AbortSignal` combines an iterable of AbortSignals.
The entire input is validated before selecting a source. An already-aborted
source wins in iterable order; otherwise the first source to abort supplies
the exact reason, including falsy values. An empty iterable produces a signal
that never aborts. Invalid inputs throw TypeError.

~~~js
const userStop = new AbortController();
const signal = AbortSignal.any([userStop.signal, AbortSignal.timeout(1_000)]);
await fetch("https://example.com/data", {signal});
~~~

The combined signal does not abort its input signals. Combinations follow
actual signal abortion; manually dispatching an abort event does not abort
dependent signals. It is available in Workers as well as the main runtime.

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
