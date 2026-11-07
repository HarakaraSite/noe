# `noe:sockets`

### API information

Public exports: `connect`, `listen`.

Reference: `noe:sockets`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.RawSockets. Raw TLS can also use the optional host configuration in Profile.Modules.RawSocketsTLS. Platform support and cancellation follow the socket API contract below. Raw TLS is available in source after v2.17.0; the v2.17.0 release binary does not include it.

Public-definition source: internal/nativeio/net.go, internal/nativeio/js/net.js. Reading this reference evaluates no target module and makes no provider request.

`noe:sockets` exports `connect` and `listen` for raw TCP, TLS, and Unix stream sockets. These APIs carry bytes through Web Streams; they are distinct from HTTP/WebSocket APIs and from the standard library's WebSocket setting. The module is available in the main runtime and Workers. Open listeners and connections keep the owning runtime active and are reclaimed when it ends. TLS was added in source after v2.17.0. See the selected detail for its address, platform, and cancellation requirements.

## connect

`connect(options) -> Promise<Connection>` opens a TCP, TLS, or Unix connection and returns `{readable, writable, close}`. Set `options.network` to `"tcp"`, `"tls"`, or `"unix"`. TCP and TLS use `{network, host, port, signal?}`; Unix uses `{network: "unix", path, signal?}`. For TCP and TLS, `host` must be nonempty and `port` must be 1–65535. For Unix sockets, `path` is a local path that must fit the operating-system path limit. An optional `signal` cancels a pending connection, including a TLS handshake; it does not close a returned connection. Invalid options throw `TypeError` or `RangeError`; network failures, TLS failures, and aborts reject the Promise. The stream directions can be shut down independently; call `close()` in `finally` to release the full connection.

For TLS, use `{network: "tls", host, port, serverName?, ca?, cert?, key?, signal?}`. Verification of the certificate chain and hostname is enabled by default with system roots; `serverName` selects the hostname to send as SNI and verify, and otherwise `host` is used. A nonempty PEM `ca` replaces system roots for this connection. Optional client `cert` and `key` must be supplied together as a matching PEM identity. These strings contain PEM data, not file paths. Empty values, malformed PEM, mismatched identities, and conflicts with host TLS configuration throw `TypeError` before dialing, without including PEM data in the error. The API does not expose a certificate-verification bypass. TLS options are rejected for TCP and Unix connections.

```js
import { connect } from "noe:sockets";

const connection = await connect({network: "tcp", host: "127.0.0.1", port: 9000});
try {
  const writer = connection.writable.getWriter();
  await writer.write(new TextEncoder().encode("hello\n"));
  await writer.close(); // Send FIN; the readable side may still receive data.
  writer.releaseLock();
  console.log(await new Response(connection.readable).text());
} finally {
  await connection.close();
}
```

## listen

`listen(options) -> Promise<Listener>` binds and returns `{address, accept, close}`. Set `options.network` to `"tcp"`, `"tls"`, or `"unix"`. TCP and TLS use `{network, host, port, signal?}`; Unix uses `{network: "unix", path, signal?}`. For TCP and TLS, `host` must be nonempty and `port` must be 0–65535; port `0` asks the OS to select a port. For TLS, optional `cert` and `key` must be supplied together as a matching PEM identity unless the Go host supplies the server TLS configuration. CA and `serverName` are client-only and rejected by TLS listeners. TLS options are rejected for TCP and Unix listeners. PEM values contain data, not file paths. Empty values, malformed PEM, mismatched identities, missing server identity, and conflicts with host TLS configuration throw `TypeError` before binding, without including PEM data in the error. For Unix sockets, `path` is a local path that must fit the operating-system path limit. An optional `signal` cancels only the pending listen; it does not close an established listener. A Unix listener does not remove a pre-existing path, and forced termination may leave its socket path behind. `address` describes the bound network endpoint; for TCP or TLS port `0`, read the selected port from `listener.address.port`. Each `accept()` resolves to a `Connection` only after its TLS handshake succeeds. A failed TLS handshake rejects that accept without closing the listener, and multiple accepts may progress separately. Closing the listener cancels pending accepts and handshakes and prevents new connections; already delivered connections remain open until closed.

```js
import { listen } from "noe:sockets";

const listener = await listen({network: "tcp", host: "127.0.0.1", port: 0});
console.log(listener.address.port);
try {
  const connection = await listener.accept();
  try {
    const {value, done} = await connection.readable.getReader().read();
    if (!done) console.log(new TextDecoder().decode(value));
  } finally {
    await connection.close();
  }
} finally {
  await listener.close();
}
```

## Listener.address

`listener.address` is `{network: "tcp", host, port}` for TCP, `{network: "tls", host, port}` for TLS, or `{network: "unix", path}` for Unix sockets. For a TCP or TLS listener bound with port `0`, `port` contains the selected port.

## Listener.accept

`listener.accept() -> Promise<Connection>` waits for one incoming connection. Closing the listener rejects a pending accept. Each accepted connection has its own lifetime; closing a listener does not close accepted connections.

## Listener.close

`await listener.close()` stops accepting and releases the bound listener. It cancels pending accepts, TLS handshakes, and accepted results that have not yet been delivered. Connections already returned by `accept()` retain independent ownership and must be closed or disposed separately.

## Connection.readable

`connection.readable` is a `ReadableStream<Uint8Array>`. It produces received bytes and closes at peer EOF. Cancelling the reader stops local read delivery. For TLS it completes without waiting for protocol-alert writes; pending native protocol work remains owned by the connection until full close. Read chunks may have any size; do not assume one read corresponds to one write.

## Connection.writable

`connection.writable` is a `WritableStream<Uint8Array>`. Writes accept only `Uint8Array`. Closing a TCP writer sends FIN; closing a TLS writer sends TLS `close_notify` while leaving the read direction open. Aborting a TCP writer terminates only the write direction. Aborting a TLS writer interrupts writes without waiting to send a graceful alert. Full connection close or disposal closes the underlying transport promptly; close the writer first when the peer needs a graceful TLS EOF. Cancelling a TLS reader interrupts only reads because TLS has no read-side shutdown message.

## Connection.close

`await connection.close()` fully closes the connection and ends both directions. Call it even after a read or write side ends, and use `finally` so errors do not leak an open connection. Network errors reject pending stream operations.

## Connection[Symbol.asyncDispose]

`connection[Symbol.asyncDispose]() -> Promise<void>` calls `close()`. An
`await using connection = await connect(options)` declaration closes both
directions and releases pending stream operations when its block exits.

## Listener[Symbol.asyncDispose]

`listener[Symbol.asyncDispose]() -> Promise<void>` calls `close()`. An
`await using listener = await listen(options)` declaration stops accepting
and releases pending accepts at block exit. Accepted connections retain their
own ownership and must be closed or disposed separately.
