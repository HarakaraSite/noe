# `noe:http`

The recommended HTTP entry point in NOE. It exports `fetch` and `serve` as named functions, with no default export. Both functions are the same values as their globals; `serve` is also the function exported by `node:http` and `http`. Existing entry points remain available.

Use the existing global Headers, Request, Response, ReadableStream, AbortController, WebSocket, and upgradeWebSocket APIs with these functions. The module does not create new types or re-export these globals. It is available in main, Node-style Workers, nested Workers, Web Workers, and agent `run_js` runtimes when the host selects the required capabilities.

Outgoing `ReadableStream` request bodies, incoming request bodies, and response bodies can all be consumed as streams. A server invokes its handler after request headers arrive, before waiting for the incoming body to reach EOF. The receive stream reads from the HTTP body only when JavaScript asks for data.

### Exports

`fetch`, `serve`

### Source basis

NOE-maintained implementation: internal/stdlib/http.go registers values from the independently selected Fetch and Serve capabilities. Its engine and standard-library origin are recorded in internal/stdlib/UPSTREAM.md. A runtime receives `noe:http` when at least one of those capabilities is enabled; the exported functions reflect the selected capabilities. NOE's CLI profile enables both.

## fetch

```js
import { fetch } from "noe:http";
```

`fetch(input, init = {}) -> Promise<Response>`. Input may be a URL/string or Request. Supported init fields include method, headers, body, and signal. A `ReadableStream` body is sent incrementally; each chunk must be a `Uint8Array`. NOE preserves each view's `byteOffset` and `byteLength`, skips empty chunks, and reads the next chunk only after the current chunk has been written to the HTTP request body. The upload bridge holds at most one chunk at a time. This does not bound the stream's own queue or buffering in the HTTP transport, operating system, or peer, and write completion does not mean the peer application consumed the bytes. Other supported Web body values retain their existing finite-body behavior.

Upload bodies are one-shot. An already-used or locked stream is rejected before network activity begins. Passing a Request to `fetch`, with or without init overrides, consumes the selected body; constructing a new Request from an unused Request transfers its body. Use `request.clone()` explicitly before consumption when two branches are needed. A stream body is not replayed for a redirect: 307 and 308 responses are returned when following them would require resending the body. The host client may follow 301, 302, and 303 by continuing with a bodyless request. If final response headers arrive before the upload reaches EOF, NOE stops the unfinished upload and returns the still-readable Response without waiting for producer EOF. Producer failures abort the request; a race with response delivery may surface as a response-body read failure. Late errors from an intentionally stopped producer are ignored. An AbortSignal cancels an in-progress upload or response-body read. This early-response behavior does not provide general full-duplex HTTP.

Network, permission, and abort failures reject. HTTP 4xx and 5xx status codes still resolve to a Response: inspect response.ok or response.status. Consume its body once with text(), json(), arrayBuffer(), or bytes(), or incrementally through response.body. The returned Response, Headers, and body stream use the same global constructors as the rest of NOE.

```js
const response = await fetch("https://example.com/data");
if (!response.ok) throw new Error("HTTP " + response.status);
console.log(await response.json());
```

NOE's default HTTP client timeout is 30 seconds, including upload and response-body reading. The 32 MiB limit applies to response-body reads, not uploads: only that response prefix is returned, and reaching the cap ends the body normally rather than rejecting for excess bytes. Pass an AbortSignal to cancel an in-progress upload or response-body read. Consume or cancel a response body when finished. Redirect and cache behavior follows the host client subject to the one-shot stream rule above; broader browser/Node options are not implemented. The call uses the current runtime's loop and owned resources, which are cancelled when that runtime ends.

## serve

```js
import { serve } from "noe:http";
```

`serve(options, handler) -> server`, `serve(handler) -> server`, or `serve({ ...options, fetch: handler }) -> server`. Options include hostname (default 127.0.0.1), port (default 0, an OS-assigned port), and onError. The handler receives a Web Request and must return a Response, synchronously or through a promise. The server handle has hostname, port, url, and close(). This is an HTTP listener; there is no TLS-listener option.

```js
import { fetch, serve } from "noe:http";

const server = serve({ port: 0 }, async request => {
  return Response.json(await request.json());
});
try {
  const response = await fetch(server.url, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ message: "hello" }),
  });
  console.log(await response.json());
} finally {
  server.close();
}
```

The listener keeps the run alive until close() or runtime shutdown. A handler starts after headers arrive, without waiting for request-body EOF. GET and HEAD requests have no JavaScript body. For other methods, `request.body` is `null` when the HTTP server can determine that no body is present; otherwise it is a `ReadableStream`, including when an unknown-length body later ends without bytes. The stream yields `Uint8Array` values. Its source has a high-water mark of zero and performs at most one bounded native read for each pull, so the NOE bridge does not read ahead while JavaScript has no read demand. HTTP transport, operating-system, and peer buffers are outside that bound, and stream cloning or teeing may queue data in the Web Streams layer.

The default receive cap is 32 MiB. In Go `Serve` configuration, `MaxBodyBytes: 0` selects that default, a positive value exposes only that prefix and then ends the stream normally, and a negative value exposes an empty body. Reaching the cap does not trigger an extra-byte check or a 413 response, and does not rewrite the received `Content-Length` header. The cap is a host setting, not a JavaScript option. `text()`, `json()`, `arrayBuffer()`, `bytes()`, and other body convenience methods retain the shared one-shot Request rules and collect the bytes they consume; use `request.body` with a reader for incremental processing.

A body read failure, such as a truncated or malformed request body discovered after dispatch, rejects the pending read. A handler may catch that error and return a response while the connection remains usable. An uncaught handler error follows the existing `onError`/500 path; a broken connection cannot be guaranteed to deliver a response. GET and HEAD bodies are not exposed to JavaScript.

The incoming body remains available while a returned Response stream is being sent, so that stream may read `request.body`. After response sending succeeds or fails, remaining input is closed and later reads reject. Cancelling the request body closes its JavaScript stream and prevents new reads; a native read already blocked in the HTTP body may be released when response sending finishes. Body cancellation does not cancel response production. For an HTTP/1 response sent with unread input, connection reuse is not guaranteed. Normal EOF remains eligible for the usual keep-alive behavior.

For accepted `Expect: 100-continue` requests, the server sends the interim `100 Continue` before it reads body bytes. When the handler returns a streaming Response, NOE waits for the final response headers to flush before its response pump asks that stream for a chunk. This lets a zero-high-water-mark producer such as `new Response(request.body)` avoid starting a native read before the flush. The gate controls demand from NOE's response pump only: it cannot prevent a producer's `start()`, its own early pull (including the default high-water mark), or a separate body reader started by the handler. If one of those reads waits for input that the client itself delays, final-response progress is not guaranteed until that read settles. A pending body read cancelled by the handler is released before the final headers are flushed. This protocol does not promise general full-duplex HTTP; in particular, NOE `fetch()` still stops an unfinished upload when final response headers arrive.

The default read-header timeout is 30 seconds and idle timeout is 60 seconds. These host settings are not additional JavaScript options. `server.close()` is synchronous and idempotent: it stops accepting requests, waits up to five seconds for graceful shutdown, then force-closes remaining HTTP connections. Runtime shutdown also has a force-close path. Call `close()` outside a handler after consuming the response; calling it in the handler that must finish can block that handler's completion.

Handler errors or invalid return values produce a 500 response. An optional `onError(error)` may supply a Response instead. Runtime shutdown, execution timeout, cancellation, and Worker termination close the listener and active HTTP connections.

For WebSocket upgrade, call the existing global upgradeWebSocket(request) in a handler, configure the returned socket, and return its response. This uses the same Serve configuration and upgrade-token path as the global serve function. Close WebSockets when finished; server.close() is not a substitute for closing upgraded connections. For raw TCP/Unix streams use `noe:sockets`.
