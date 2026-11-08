# Global APIs

### API information

Reference: `globals`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: This is the complete reference, not a report of the current runtime. Globals and host operations depend on Profile.Modules; ECMAScript built-ins come from the pinned engine.

Web Storage is available in v2.19.0 and later. Earlier executables do not provide these APIs.

Public-definition source: internal/stdlib/stdlib.go, internal/jsexec/profile.go. Reading this reference evaluates no target module and makes no provider request.

This page lists globals provided by the pinned go-quickjs engine and the NOE-maintained standard library. Global names do not have a module default export. Core ECMAScript globals are listed for discovery only; this page does not attempt to document the full JavaScript language reference.

### NOE and Node-style globals

console writes to the run's configured stdout/stderr. process is the NOE process object described in noe:process. crypto and performance are also globals. Buffer is also available from node:buffer. fetch, Request, Response, and Headers use the Web request/response shape. WebSocket opens a WebSocket client. Worker starts a local Worker runtime. Callback timers are setTimeout, setInterval, clearTimeout, and clearInterval; queueMicrotask schedules a microtask. setImmediate is exported by node:timers, not installed globally.

### Web API globals

AbortController, AbortSignal, Blob, BroadcastChannel, ByteLengthQueuingStrategy, CompressionStream, CountQueuingStrategy, CryptoKey, CustomEvent, DOMException, DecompressionStream, ErrorEvent, Event, EventSource, EventTarget, File, FormData, Headers, MessageChannel, MessageEvent, MessagePort, Performance, PerformanceEntry, PerformanceMark, PerformanceMeasure, ReadableStream, ReadableStreamDefaultController, ReadableStreamDefaultReader, Request, Response, Storage, TextDecoder, TextDecoderStream, TextEncoder, TextEncoderStream, TransformStream, URL, URLPattern, URLSearchParams, WebSocket, Worker, WritableStream, and WritableStreamDefaultWriter. `sessionStorage` is available by default. `localStorage` is installed only when the host explicitly selects its filesystem-backed path.

### ECMAScript and QuickJS globals

The pinned QuickJS runtime also exposes these language and host globals: AggregateError, Array, ArrayBuffer, AsyncDisposableStack, Atomics, BigInt, BigInt64Array, BigUint64Array, Boolean, DataView, Date, DisposableStack, Error, EvalError, FinalizationRegistry, Float16Array, Float32Array, Float64Array, Function, Infinity, Int16Array, Int32Array, Int8Array, Intl, Iterator, JSON, Map, Math, NaN, Number, Object, Promise, Proxy, RangeError, ReferenceError, Reflect, RegExp, Set, ShadowRealm, SharedArrayBuffer, String, SuppressedError, Symbol, SyntaxError, Temporal, TypeError, URIError, Uint16Array, Uint32Array, Uint8Array, Uint8ClampedArray, WeakMap, WeakRef, WeakSet, atob, btoa, clearInterval, clearTimeout, console, decodeURI, decodeURIComponent, encodeURI, encodeURIComponent, escape, eval, globalThis, isFinite, isNaN, parseFloat, parseInt, queueMicrotask, setInterval, setTimeout, structuredClone, undefined, and unescape.

### Exact globalThis inventory

`AbortController`, `AbortSignal`, `AggregateError`, `Array`, `ArrayBuffer`, `AsyncDisposableStack`, `Atomics`, `BigInt`, `BigInt64Array`, `BigUint64Array`, `Blob`, `Boolean`, `BroadcastChannel`, `Buffer`, `ByteLengthQueuingStrategy`, `CompressionStream`, `CountQueuingStrategy`, `CryptoKey`, `CustomEvent`, `DOMException`, `DataView`, `Date`, `DecompressionStream`, `DisposableStack`, `Error`, `ErrorEvent`, `EvalError`, `Event`, `EventSource`, `EventTarget`, `File`, `FinalizationRegistry`, `Float16Array`, `Float32Array`, `Float64Array`, `FormData`, `Function`, `Headers`, `Infinity`, `Int16Array`, `Int32Array`, `Int8Array`, `Intl`, `Iterator`, `JSON`, `Map`, `Math`, `MessageChannel`, `MessageEvent`, `MessagePort`, `NaN`, `Number`, `Object`, `Performance`, `PerformanceEntry`, `PerformanceMark`, `PerformanceMeasure`, `Promise`, `Proxy`, `RangeError`, `ReadableStream`, `ReadableStreamDefaultController`, `ReadableStreamDefaultReader`, `ReferenceError`, `Reflect`, `RegExp`, `Request`, `Response`, `Set`, `ShadowRealm`, `SharedArrayBuffer`, `Storage`, `String`, `SuppressedError`, `Symbol`, `SyntaxError`, `Temporal`, `TextDecoder`, `TextDecoderStream`, `TextEncoder`, `TextEncoderStream`, `TransformStream`, `TypeError`, `URIError`, `URL`, `URLPattern`, `URLSearchParams`, `Uint16Array`, `Uint32Array`, `Uint8Array`, `Uint8ClampedArray`, `WeakMap`, `WeakRef`, `WeakSet`, `WebSocket`, `Worker`, `WritableStream`, `WritableStreamDefaultWriter`, `atob`, `btoa`, `clearInterval`, `clearTimeout`, `console`, `crypto`, `decodeURI`, `decodeURIComponent`, `encodeURI`, `encodeURIComponent`, `escape`, `eval`, `fetch`, `globalThis`, `isFinite`, `isNaN`, `localStorage`, `parseFloat`, `parseInt`, `performance`, `process`, `queueMicrotask`, `sessionStorage`, `setInterval`, `setTimeout`, `structuredClone`, `undefined`, `unescape`

In this inventory, `localStorage` is conditional: it appears only when the host explicitly opts in to filesystem-backed storage. `Storage` and `sessionStorage` are always available with the Web APIs selected.

## CompressionStream

Construct with a required gzip, deflate or deflate-raw format. Unsupported formats throw TypeError immediately. readonly readable and writable getters expose the composed Web streams. Writes require fixed, non-shared BufferSources and snapshot exact view spans; encode strings first. The wrapper cannot be structured-cloned. Consume the readable side concurrently with writes. The node:zlib reference describes stream lifecycle and the related create* helpers.

## DecompressionStream

Uses the same required formats, BufferSource input and readonly endpoints as CompressionStream. Invalid headers/data/checksums, trailing bytes and extra gzip members reject with TypeError; incomplete input may fail at close. Incremental output can precede a validation error. The existing cumulative decompressed-output limit is 1 GiB and exceeding it raises RangeError. Consume output concurrently and await successful completion.

## URL

Construct with a required input and optional base. Values undergo USVString conversion. Constructors and setters use UTF-8 percent encoding for credentials, paths, queries and fragments, preserve existing percent escapes, and normalize dot segments (including percent-encoded dots), empty path segments and default ports. Special schemes normalize path backslashes. Opaque paths retain their separate behavior; pathname/credential setters cannot replace them.

searchParams is a readonly SameObject attribute: retain its reference across href/search assignments, and edit parameters to update the URL query. Empty search/hash strings remove the component; assigning "?"/"#" retains an empty marker in href. Form serialization can rewrite spaces as + and ~ as %7E. parse/canParse return null/false for parsing failures; missing arguments and conversion exceptions still throw. Stringification and getters use private branded state.

Full UTS46/IDNA, IP address canonicalization/validation and Windows file-URL quirks remain outside the normalization/encoding slice. The implementation does not certify every URL parser state. The current primary URL Standard's opaque-space rule is used even where older runtimes differ.

## URLSearchParams

Parse and serialize UTF-8 application/x-www-form-urlencoded data. A space serializes as +; a literal + serializes as %2B. Percent decoding tolerates malformed escapes and replaces malformed UTF-8 while preserving valid text and a leading BOM. Inputs and method arguments use USVString conversion, including replacement of lone UTF-16 surrogates; Symbols reject TypeError.

The constructor accepts a string, a generic iterable of exactly two-element iterable pairs, or a record. append/set require two arguments; get/getAll/has/delete require a name. has/delete accept an optional value filter. size is readonly. Sorting is stable in UTF-16 key order. Iterators and forEach observe mutations to the current list. Updates to a linked URL use private serialization. The wrapper cannot be structured-cloned.

## URLPattern

Construct a pattern from a string or a URLPatternInit dictionary containing protocol, username, password, hostname, port, pathname, search, hash and baseURL. Unknown dictionary keys are ignored. Missing pattern components default to wildcards; baseURL supplies eligible earlier components. Pattern credentials are never inherited from baseURL. Relative string patterns require a separate baseURL; dictionary patterns carry baseURL themselves.

~~~js
const baseURL = "https://example.test/api/";
const route = new URLPattern({pathname: "books/:id", baseURL});
const match = route.exec({pathname: "books/42", baseURL});
console.log(match.pathname.groups.id); // "42"
~~~

test and exec accept a string with an optional baseURL, or a dictionary with its own baseURL. Missing matching components are empty strings, subject to base inheritance. A dictionary plus a separate baseURL throws TypeError. Constructor parsing errors throw TypeError; matching parse failures return false/null. Argument conversion exceptions propagate. exec returns converted input snapshots and per-component input/groups records, including wildcard captures.

The eight component attributes and hasRegExpGroups are branded readonly getters. The optional ignoreCase constructor option affects pathname, search and hash. URLPattern wrappers cannot be structured-cloned. The existing component compiler and URL backend remain limited: full pattern grammar, custom-regexp canonicalization and IDNA are not certified. Use component dictionaries for routes that avoid ambiguous whole-URL pattern syntax.

## TextEncoder

TextEncoder encodes UTF-8. encodeInto(source, destination) accepts a genuine
Uint8Array, respects its view boundaries, and writes complete scalar values.
The returned read counts consumed source UTF-16 code units; written counts
output bytes. Unpaired surrogates encode as U+FFFD. A destination that cannot
fit the next complete character receives none of that character's bytes.

This correction is available in v2.21.0 and later. Shared fixed backing is
allowed. Detached destinations are empty; resizable/growable backing is rejected
under the operation's current Web IDL annotation. Invalid receivers, missing
arguments, Symbols and wrong destination types throw TypeError. TextDecoder
remains limited to UTF-8.

## TextDecoder

TextDecoder supports UTF-8 labels (case-insensitive, with surrounding ASCII
whitespace), fatal errors, and ignoreBOM. decode accepts an ArrayBuffer,
SharedArrayBuffer or buffer view and an optional stream flag. It keeps partial
sequences and BOM state across streaming calls. Ending input replaces each
malformed subsequence at the Encoding Standard's boundaries, or throws TypeError
in fatal mode. Metadata is readonly and receiver-branded. Fixed shared backing
is supported; detached inputs are empty and resizable/growable backing is rejected. Other encoding families
throw RangeError. These corrections are available in v2.21.0 and later.

## EventTarget

addEventListener accepts boolean capture or {capture, once, passive, signal}.
removeEventListener accepts boolean capture or {capture}. The type, callback
identity and capture flag identify a registration; adding it again keeps its
original options. Capture listeners run first at the single target, followed
by non-capture listeners, both with AT_TARGET phase. There is no DOM tree.

Removal during dispatch skips the removed listener. Once listeners are removed
before invocation. Passive listeners cannot cancel an event. A real signal abort
removes bound listeners before its abort event; synthetic abort events do not.
Already-aborted signals prevent registration. Callback objects resolve handleEvent
at invocation. These corrections also apply to EventSource, WebSocket, messaging,
Performance and Worker event targets in v2.21.0 and later.

## CustomEvent

new CustomEvent(type, {detail, bubbles, cancelable, composed}) creates an Event
with a branded getter-only detail attribute. Defaults are null detail and false
flags. Detail retains its original reference and is not cloned. The constructor
requires a DOMString type; null/undefined initialization dictionaries use the
defaults. Primitive non-dictionaries reject TypeError.

CustomEvent uses the existing EventTarget dispatcher. Inherited attributes are
readonly; preventDefault affects cancelable dispatch, and stopImmediatePropagation
stops remaining listeners. initCustomEvent(type, bubbles, cancelable, detail)
reinitializes an event outside dispatch and is inert during dispatch. Nested
dispatch of the same event rejects InvalidStateError. CustomEvent is not
structured-cloneable and rejects DataCloneError. These additions are available in
v2.21.0 and later; no browser DOM tree is introduced.

## performance

The User Timing implementation in v2.21.0 and later replaces the previous
empty mark/measure methods. now and timeOrigin retain the existing native clock.
mark(name, {startTime, detail}) returns and records a PerformanceMark.
measure(name, startMark, endMark) and measure(name, {start, end, duration, detail})
return and record a PerformanceMeasure, using specified timestamps or the latest
matching marks. Null measure options use an empty dictionary. Supported
overloads, required arguments, finite/nonnegative
explicit timestamp validation and error names follow User Timing.

getEntries(), getEntriesByName(name, type), and getEntriesByType(type) return
new arrays in stable chronological order. clearMarks(name) and clearMeasures(name)
remove matching entries; omitting the name clears that type. Each runtime/Worker
has an independent timeline. Failed operations add no entry. No entry quota or
observer loop is added.

## Performance

Performance is an illegal constructor. The global performance instance extends
EventTarget and provides the User Timing methods above. Its toJSON() returns
timeOrigin. PerformanceObserver and resource/navigation timing are not provided.

## PerformanceEntry

PerformanceEntry is an illegal constructor. Entries have branded readonly name,
entryType, startTime and duration attributes, plus toJSON(). Performance objects
and entries reject DataCloneError on structured cloning. Newer Timeline
id/navigationId fields are outside this supported surface.

## PerformanceMark

new PerformanceMark(name, {startTime, detail}) creates a mark without inserting
it into the global timeline. performance.mark inserts one. detail is
structured-cloned on creation and exposed through a readonly attribute;
subsequent input mutations do not change the entry. toJSON() includes detail.

## PerformanceMeasure

PerformanceMeasure is an illegal constructor; use performance.measure().
Entries expose the readonly common fields, cloned detail and toJSON(). Reversed
start/end timestamps can produce a negative calculated duration. Explicit
parameter validation follows the supported User Timing contract.

## crypto

Global crypto provides integer-view getRandomValues, randomUUID v4, and the
implemented subtle operations described by node:crypto. The Node module's
webcrypto/subtle exports share these exact objects. getRandomValues preserves
the input view and enforces the standard 65,536-byte limit; Float/DataView
inputs reject with TypeMismatchError and oversized views with QuotaExceededError.

## CryptoKey

In v2.20.0 and later, CryptoKey is an illegal constructor with
readonly type, algorithm, extractable, and usages attributes. Import through
crypto.subtle.importKey. Cached algorithm/usages metadata objects are mutable
views; private snapshots govern actual operations even if those views are
changed or attributes are shadowed. The implemented key type is secret.
Key usage is operation-specific, and non-extractability cannot be changed by
assigning a public property. Unsupported structured cloning rejects with
DataCloneError. See node:crypto for formats, errors, and remaining API gaps.

## Storage

`Storage` is the shared interface for `sessionStorage` and opt-in `localStorage`. It is an illegal constructor; use the getter-only global references. Each object has `Storage.prototype` and the `Storage` toStringTag.

Both objects provide synchronous `length`, `key(index)`, `getItem(key)`, `setItem(key, value)`, `removeItem(key)`, and `clear()` operations. `getItem()` and `key()` return `null` when there is no result; `key()` converts its index to an unsigned 32-bit integer. A missing named property reads as `undefined`. Strings use Web IDL DOMString conversion and preserve UTF-16 code units, including unpaired surrogates. Named assignment, deletion, enumeration, descriptors, and collisions with method names follow Storage's legacy named-property behavior. Keys enumerate in insertion order in NOE; updating a key retains its position. No storage quota is imposed.

`sessionStorage` begins empty and belongs to one runtime. It is stable for that runtime and is not shared with another execution or Worker. `localStorage` has no default path and is absent unless selected with `noe run --local-storage path` or, for an embedded Go host, `Modules.LocalStorage = &stdlib.LocalStorageOptions{Path: "state.json"}`. The selected `Modules.FS` must also be enabled. The storage path uses that filesystem's Root, Cwd, and ReadOnly policy; its containing directory must already exist. Relative paths are fixed against execution Cwd before Workers start. Matching local-storage paths share their persisted data across runtimes and Workers in the same process, while each Worker has its own session storage.
Local-storage calls synchronously read the latest committed version and commit changes atomically through the selected filesystem before returning. Missing files start empty and are created on the first mutation. Existing invalid data raises `InvalidStateError` and is not reset; failed writes leave the previous contents intact. Mutations against a ReadOnly filesystem raise `SecurityError`, and other I/O failures retain filesystem error codes. The file is a versioned JSON document containing ordered key/value pairs. Separate OS processes and ordinary edits of the file are outside NOE's operation coordination; use `noe:sqlite` for cross-process transactional state.

Storage instances are not structured-cloneable; cloning or transferring one raises `DataCloneError`. These globals adapt the WHATWG Storage interface to NOE. There is no Window, Document, browser origin partitioning, or storage event. Selecting localStorage grants no network, command, or other module capability.

The Proxy facade rejects `Object.defineProperty` requests with
`configurable: false` before changing storage. Configurable data descriptors
are supported, including `value: undefined`, stored as the string `"undefined"`.

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

`tee()` returns two branches over one locked source reader. Ordinary chunks
retain their identity. Cancelling one branch waits for the remaining branch's
EOF/error or shared source cleanup after both cancel. Both cancellations adopt
the cleanup result, including rejection; the source receives reasons in branch
order. To stop both branches, start both cancellations and await `Promise.all`.
Source errors discard unread buffered data in both live branches. Reads already
accepted from an enqueue retain that chunk before later reads reject. The same
lifecycle applies to Request/Response clone bodies.

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

## Headers

`new Headers(init?)` accepts an iterable of name/value pairs or a string record.
Names must be HTTP tokens; names with surrounding whitespace reject. Names and
values use ByteString conversion: Symbols and characters above U+00FF reject.
Values trim surrounding HTTP whitespace and reject remaining CR, LF or NUL.

`append`, `set`, `delete`, `get`, `has` and `getSetCookie` operate on private data.
Iteration sorts names and combines ordinary repeated fields with comma-space;
each Set-Cookie remains a separate entry. `getSetCookie()` returns individual
cookie values. Iterators and `forEach` observe subsequent mutations. The existing
`toJSON()` utility returns a record; use iteration or `getSetCookie()` to preserve
multiple cookies.

## Request

new Request(input, init = {}) constructs request data for fetch or an HTTP handler. input may be a URL/string or another Request. Use method, headers, body, and signal as needed.

String/URL inputs use USVString conversion and the shared URL backend, including Unicode/space/default-port normalization. Missing input, invalid URLs and embedded credentials reject TypeError. NOE supplies no ambient document base URL; resolve relative input with URL first. Request.url retains fragments, while native HTTP serialization omits them on the wire.

method uses ByteString and HTTP token validation. CONNECT, TRACE and TRACK reject; DELETE/GET/HEAD/OPTIONS/POST/PUT normalize to uppercase, while extension methods retain their case. Empty methods reject instead of defaulting to GET. url and method are branded readonly getters backed by private state; copy, clone and fetch use that state even when a public property shadows a getter. Copy/clone preserve the existing mode/credentials/redirect metadata. URL/method errors occur before acquiring an upload body.

The noe:http adapter creates incoming requests through a private factory so the native incoming method and raw URL remain available, including when copied/cloned. This does not add a public factory or browser CORS/cache/redirect policy. Other Request/Body/Headers contracts retain their documented limits. Advanced Network installation without WebAPIs can install, but construction needs the Web URL backend (which may be installed later).

~~~js
const request = new Request("https://example.com/items", {
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ name: "sample" }),
});
~~~

Bodies are one-shot. Methods such as text() and json() return promises; clone the request before consuming if another read is needed. Constructing a new Request from an unused Request transfers its body, so the original cannot consume that body. A stream body must be unlocked and unused when fetch starts. Producer failures abort the request; a race with response delivery may surface as a response-body read failure. Late errors from an intentionally stopped producer are ignored.

A reader lock alone leaves bodyUsed false, but helpers, clone and transfer still
reject a locked body. Reading or cancellation sets bodyUsed true. Finite byte
inputs are snapshotted at construction. Clone and transfer preserve the native
finite-body Content-Length and redirect replay behavior.

## Response

`new Response(body = null, init = {})` creates response data. `init` supports
`status`, `statusText`, and `headers`. After unsigned-short conversion, status
must be 200..599. Status text must be a ByteString HTTP reason phrase. Statuses
204, 205 and 304 require a null body, including when the proposed body is empty.
Constructed responses have type `default`, an empty URL and `redirected: false`.
Metadata and the stable headers reference are readonly. `ResponseInit.url` is
ignored; native fetch responses supply their own URL and redirect metadata.

`Response.json(value, init?)` produces UTF-8 JSON with `application/json` unless
an explicit content type exists; unserializable top-level values reject.
`Response.error()` creates a bodyless status-0 response with type `error`.
`Response.redirect(url, status = 302)` resolves an absolute URL through the shared
URL API and accepts 301, 302, 303, 307 or 308. NOE has no ambient document base URL.
Error, redirect and fetched response headers are immutable, including after clone.
Constructed/JSON response headers remain mutable for host HTTP handlers, including
Set-Cookie. Browser forbidden-header/CORS guards are outside NOE's host policy.

Read a body with `text()`, `json()`, `arrayBuffer()` or `bytes()`, or its stream.
HEAD and null-body HTTP responses expose `body: null`. Clone preserves native
URL, redirect, type and header immutability while retaining existing body ownership.

~~~js
const response = Response.json({ ok: true }, { status: 201 });
console.log(response.status, await response.json());
~~~

Body consumption is one-shot. For a stream body, closing or cancelling its consumer controls when upstream data finishes or is released.

Body helpers read the current stream even for a finite body. Locked or disturbed
bodies reject TypeError; cancelled finite data cannot be read again. Successful
helper reads retain the stream's reader lock. Null bodies remain readable as
empty data without setting bodyUsed. Stream chunks must be Uint8Array values;
read-all helpers copy each chunk before requesting the next, and bytes() returns
independent storage. A server response with a locked/used body enters onError
before sending headers.

Request/Response clones isolate ordinary ArrayBuffer-backed Uint8Array chunks
before either branch receives them. Mutating or transferring a raw chunk buffer
from one body cannot corrupt the sibling clone. Stream subviews preserve their
offset/length and backing-buffer contents. SharedArrayBuffer memory remains shared
intentionally. This uses an internal byte-copy mode; public `ReadableStream.tee()`
retains shared chunk identity. Arbitrary non-byte chunk cloning and full buffer
metadata/error conversion remain outside this bounded byte-isolation contract.

For FormData bodies, multipart serialization normalizes text field values from
bare CR/LF to CRLF without mutating the FormData or changing file bytes.
`formData()` recognizes delimiter lines at the message start or after CRLF;
inline occurrences of the boundary marker remain data. It accepts MIME delimiter
padding, ignores preamble/epilogue, and preserves part order, duplicate names and
file trailing line breaks. Full MIME header parsing and field-name/filename
escaping remain outside this scoped correction.

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

new AbortController() creates a private signal exposed by a branded, readonly
SameObject signal getter. abort(reason?) preserves explicit reason identity;
omitting the reason creates an AbortError DOMException. Repeated abort keeps the
first reason. Shadowing public properties does not replace the internal signal.
These corrections are unreleased after v2.21.0.

## AbortSignal

AbortSignal has no public constructor. Use a controller, AbortSignal.abort(reason?),
AbortSignal.any(signals), or AbortSignal.timeout(milliseconds). Default abort and
timeout reasons are AbortError and TimeoutError DOMExceptions. throwIfAborted()
throws the original reason. onabort participates in listener registration order;
replacement keeps its position and non-object values remove it. Object values
retain their slot and identity; non-callable objects are inert when invoked.

timeout requires milliseconds and applies Web IDL EnforceRange unsigned-long-long
conversion: reject non-finite/out-of-range values with TypeError and truncate
fractions toward zero. The supported JavaScript integer range is 0 through 2^53-1.
Negative fractions between -1 and 0 therefore become zero.
Long deadlines do not wrap through setTimeout's 32-bit limit or Go duration bounds.
They use the runtime's existing timer queue and execution cleanup; clearTimeout
does not cancel internal abort deadlines. A timer keeps the existing Loop timer
liveness behavior. Legacy WebAPIs installation without a Loop supports synchronous
abort, but timeout rejects TypeError because no scheduler is available. These
corrections are unreleased after v2.21.0.

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
