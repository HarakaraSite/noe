# `noe:http`

### API information

Public exports: capability-selected `serve`, `createWebSocket`, and `upgradeWebSocket`.

Reference: `noe:http`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: The module is installed when Serve or WebSockets is selected. Serve selects `serve`; WebSockets selects `createWebSocket`; selecting both also selects `upgradeWebSocket`. With neither host helper selected, the module is absent. Fetch alone does not install it.

Public-definition source: internal/stdlib/http.go publishes the selected helpers installed by internal/stdlib/serve.go and internal/stdlib/websocket.go; internal/noestdlib/noestdlib.go completes capability selection for embedded hosts. Reading this reference evaluates no target module and makes no provider request.

`noe:http` contains NOE-specific listener and WebSocket helpers. It has no default export and no `fetch` export. Standard `fetch`, `Request`, `Response`, `Headers`, `WebSocket`, `EventSource`, and related Web APIs remain available under their standard global names. The module does not create separate Web types. It is available in main and Worker runtimes when the host selects the required capabilities.

## serve

```js
import { serve } from "noe:http";
```

`serve(options, handler) -> server`, `serve(handler) -> server`, or `serve({ ...options, fetch: handler }) -> server`. Options include hostname (default `127.0.0.1`), port (default `0`, an OS-assigned port), and `onError`. The handler receives a Web `Request` and must return a `Response`, synchronously or through a promise. The server handle has `hostname`, `port`, `url`, and `close() -> Promise<void>`. This is an HTTP listener; there is no TLS-listener option.

```js
import { serve } from "noe:http";

const server = serve({ port: 0 }, async request => {
  return Response.json(await request.json());
});
try {
  console.log(server.url);
} finally {
  await server.close();
}
```

The listener keeps the run alive until `close()` or runtime shutdown. A handler starts after request headers arrive, without waiting for body EOF. GET and HEAD requests have no JavaScript body. For other requests, `request.body` is `null` when the HTTP server can determine that no body is present; otherwise it is a `ReadableStream`, including when an unknown-length body later ends without bytes. The stream yields `Uint8Array` values. Its source has a high-water mark of zero and performs at most one bounded native read for each pull, so the NOE bridge does not read ahead while JavaScript has no read demand. HTTP transport, operating-system, and peer buffers are outside that bound, and stream cloning or teeing may queue data in the Web Streams layer.

The default receive cap is 32 MiB. In Go `Serve` configuration, `MaxBodyBytes: 0` selects that default, a positive value sets the limit, and a negative value disables it so the body can be unlimited. The cap is a host setting, not a JavaScript option. Reads at the limit probe for EOF: a body exactly at the limit succeeds, while an additional byte causes a `RangeError` with code `ERR_HTTP_BODY_TOO_LARGE`. Incremental reads may yield the permitted prefix before a later read reports overflow; body collectors such as `text()`, `json()`, `arrayBuffer()`, and `bytes()` reject if the body exceeds the limit. The server does not rewrite the received `Content-Length` header.

If an uncaught body overflow happens before response headers are sent, the default response is 413; `onError(error)` may supply a different response. If overflow happens while sending a streamed response, the transfer terminates and cannot be changed to a 413 after headers have started. GET and HEAD requests continue to have no JavaScript body. For accepted `Expect: 100-continue` requests, the server still sends `100 Continue` before reading body bytes. Demand-driven reads retain their existing ownership and backpressure behavior.

A body read failure, such as a truncated or malformed request body discovered after dispatch, rejects the pending read. A handler may catch that error and return a response while the connection remains usable. An uncaught handler error follows the existing `onError`/500 path; a broken connection cannot be guaranteed to deliver a response.

The incoming body remains available while a returned Response stream is being sent, so that stream may read `request.body`. After response sending succeeds or fails, remaining input is closed and later reads reject. Cancelling the request body closes its JavaScript stream and prevents new reads; a native read already blocked in the HTTP body may be released when response sending finishes. Body cancellation does not cancel response production. For an HTTP/1 response sent with unread input, connection reuse is not guaranteed. Normal EOF remains eligible for the usual keep-alive behavior.

For accepted `Expect: 100-continue` requests, the server sends the interim `100 Continue` before it reads body bytes. When the handler returns a streaming Response, NOE waits for the final response headers to flush before its response pump asks that stream for a chunk. This lets a zero-high-water-mark producer such as `new Response(request.body)` avoid starting a native read before the flush. The gate controls demand from NOE's response pump only: it cannot prevent a producer's `start()`, its own early pull (including the default high-water mark), or a separate body reader started by the handler. If one of those reads waits for input that the client itself delays, final-response progress is not guaranteed until that read settles. A pending body read cancelled by the handler is released before the final headers are flushed. This protocol does not promise general full-duplex HTTP; standard `fetch()` still stops an unfinished upload when final response headers arrive.

The default read-header timeout is 30 seconds and idle timeout is 60 seconds. These host settings are not additional JavaScript options. `server.close()` immediately stops admitting new handlers and returns the same promise on repeated calls. It waits asynchronously for active HTTP handlers without blocking JavaScript, allows up to five seconds for graceful completion, force-closes remaining HTTP connections, then resolves after cleanup. `server[Symbol.asyncDispose]()` delegates to `close()`, so `await using server = serve(handler)` is supported. Await `close()` when completion matters. A handler may call `close()` and return its response, but must not await its own close promise; await that promise outside the handler after it returns. Runtime shutdown, execution timeout, cancellation, and Worker termination retain an independent force-close path that does not wait for graceful close. Handler errors or invalid return values produce a 500 response. An optional `onError(error)` may supply a Response.

See the [Serve lifecycle and body-limit specification](../../../docs/development/stdlib-serve-lifecycle.md) for the complete contract.

## HTTPServer.close

`server.close() -> Promise<void>` starts graceful HTTP shutdown without
blocking JavaScript. It immediately closes handler admission, waits for
active HTTP handlers and body I/O, and forces remaining connections after
at most five seconds. The same Promise is returned on repeated calls and
resolves to undefined after cleanup. Runtime cancellation and Worker
termination keep an independent forced path. Initiate close and return a
Response from a shutdown handler; await completion outside that handler.

## HTTPServer[Symbol.asyncDispose]

`server[Symbol.asyncDispose]() -> Promise<void>` delegates to `close()`.
`await using server = serve(handler)` therefore waits for cleanup at scope
exit, including exceptional exit. Upgraded WebSockets remain separately owned.

## createWebSocket

```js
import { createWebSocket } from "noe:http";
```

`createWebSocket(url, options?) -> WebSocket` constructs and returns the same `WebSocket` type as the global constructor, synchronously in the `CONNECTING` state. `options` supports `protocols` (`string` or a read-only string array) and `headers` (`HeadersInit`). Use this helper when the opening handshake needs custom headers. It uses NOE's existing WebSocket backend, URL policy, TLS configuration, handshake timeout, permissions, and runtime ownership. The WebSocket's events report connection progress; returning from `createWebSocket` does not mean the connection is ready.

```js
import { createWebSocket } from "noe:http";

const socket = createWebSocket("wss://example.com/feed", {
  protocols: ["chat.v1"],
  headers: {
    Authorization: "Bearer <token>",
    Origin: "https://app.example",
  },
});
socket.addEventListener("open", () => socket.send("hello"));
```

Handshake headers are copied synchronously. Header names must be HTTP tokens. Values reject CR, LF, NUL, and other HTTP control bytes except HTAB; leading and trailing spaces or tabs are trimmed. Invalid headers throw `TypeError` before permission checks or connection allocation, and validation errors omit header values. `Host`, `Connection`, `Upgrade`, `Sec-WebSocket-Key`, `Sec-WebSocket-Version`, `Sec-WebSocket-Protocol`, `Sec-WebSocket-Extensions`, `Content-Length`, `Transfer-Encoding`, and `Trailer` are generated or owned by the handshake and cannot be supplied. Use the URL and `protocols` option for the corresponding connection fields. Duplicate pair entries are sent as repeated fields; `Headers` follows its own iteration semantics. Invalid protocol tokens throw `SyntaxError` before connecting.

The factory shares the global WebSocket's close validation and CONNECTING cancellation. Closing a reserved upgraded socket before attachment also prevents it from opening. See [WebSocket lifecycle details](../../../docs/development/stdlib-websocket-lifecycle.md).

The global `new WebSocket(url, protocols?)` follows the standard two-argument constructor API. Its `binaryType` defaults to `"blob"`; set `socket.binaryType = "arraybuffer"` when binary messages should arrive as `ArrayBuffer` values. Close the socket when it is no longer needed.

## upgradeWebSocket

```js
import { serve, upgradeWebSocket } from "noe:http";
```

This export is selected only when both Serve and WebSockets are enabled. In a `serve` handler, call `upgradeWebSocket(request)`, configure the returned socket, and return its `response` to complete the HTTP upgrade. The returned socket uses the same global `WebSocket` type. The helper uses the listener's existing upgrade-token path and requires an HTTP server in the same runtime. Close each upgraded WebSocket when finished; `server.close()` is not a substitute for closing upgraded connections. For raw TCP/Unix streams use `noe:sockets`.

```js
import { serve, upgradeWebSocket } from "noe:http";

const server = serve(async request => {
  const { socket, response } = upgradeWebSocket(request);
  socket.addEventListener("message", event => socket.send(event.data));
  return response;
});
```
