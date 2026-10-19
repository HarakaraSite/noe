# `noe:http`

The recommended HTTP entry point in NOE. It exports `fetch` and `serve` as named functions, with no default export. Both functions are the same values as their globals; `serve` is also the function exported by `node:http` and `http`. Existing entry points remain available.

Use the existing global Headers, Request, Response, ReadableStream, AbortController, WebSocket, and upgradeWebSocket APIs with these functions. The module does not create new types or re-export these globals. It is available in main, Node-style Workers, nested Workers, Web Workers, and agent `run_js` processes.

Only responses stream over the network incrementally in this version. A stream supplied as an outgoing request body is collected before sending. The server reads the incoming request body before invoking its handler.

### Exports

`fetch`, `serve`

### Source basis

NOE-maintained implementation: internal/stdlib/http.go registers existing values installed by fetch.go and serve.go. Its engine and standard-library origin are recorded in internal/stdlib/UPSTREAM.md. In hosts using stdlib.Install with partial configuration, only configured Fetch/Serve functions are exported; neither configured means no module. NOE enables both.

## fetch

```js
import { fetch } from "noe:http";
```

`fetch(input, init = {}) -> Promise<Response>`. Input may be a URL/string or Request. Supported init fields include method, headers, body, and signal. Use a string, bytes, or other supported Web body value; a ReadableStream body is fully collected before transmission.

Network, permission, and abort failures reject. HTTP 4xx and 5xx status codes still resolve to a Response: inspect response.ok or response.status. Consume its body once with text(), json(), arrayBuffer(), or bytes(), or incrementally through response.body. The returned Response, Headers, and body stream use the same global constructors as the rest of NOE.

```js
const response = await fetch("https://example.com/data");
if (!response.ok) throw new Error("HTTP " + response.status);
console.log(await response.json());
```

NOE's default HTTP client timeout is 30 seconds, including response-body reading. The response-body read cap is 32 MiB: only that prefix is returned, and reaching the cap ends the body normally rather than rejecting for excess bytes. Pass an AbortSignal to cancel the request and body reading. Consume or cancel a response body when finished. Redirect and cache behavior follows the host client; broader browser/Node options are not implemented. The call uses the current runtime's loop and owned resources, which are cancelled when that runtime ends.

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

The listener keeps the run alive until close() or runtime shutdown. A Response backed by a ReadableStream sends chunks as they are produced, using the existing response-streaming path. Incoming request bodies are buffered before the handler runs. The default request-body read cap is 32 MiB: only that prefix is read; exceeding it does not automatically produce a 413 response. The default read-header timeout is 30 seconds and idle timeout is 60 seconds. These host settings are not additional JavaScript options.

Handler errors or invalid return values produce a 500 response. An optional onError(error) may supply a Response instead. `server.close()` is synchronous and idempotent: it stops accepting requests and waits up to five seconds for in-flight HTTP requests. Call it outside a handler after consuming the response, as above; calling it in the handler that needs to finish can block that handler's completion. Runtime shutdown, execution timeout, cancellation, and Worker termination retain the existing listener cleanup behavior.

For WebSocket upgrade, call the existing global upgradeWebSocket(request) in a handler, configure the returned socket, and return its response. This uses the same Serve configuration and upgrade-token path as the global serve function. Close WebSockets when finished; server.close() is not a substitute for closing upgraded connections. For raw TCP/Unix streams use `noe:sockets`.
