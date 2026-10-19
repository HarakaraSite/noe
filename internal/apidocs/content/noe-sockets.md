# `noe:sockets`

`noe:sockets` exports `connect` and `listen` for raw TCP and Unix stream sockets. These APIs carry bytes through Web Streams; they are distinct from HTTP/WebSocket APIs and from the standard library's WebSocket setting. The module is available in the main runtime and Workers. Open listeners and connections keep the owning runtime active and are reclaimed when it ends.

Socket options use one of these forms:

```js
{network: "tcp", host: "127.0.0.1", port: 9000, signal?}
{network: "unix", path: "/tmp/service.sock", signal?}
```

For TCP, `host` is nonempty and `connect` requires port 1–65535; `listen` also permits port `0` for an OS-selected port. Unix paths are local paths and must fit the operating-system path limit. `signal` cancels only the pending open operation; it does not close an established listener or connection. A Unix listener does not remove a pre-existing path, and forced termination may leave its socket path behind.

## connect

`connect(options) -> Promise<Connection>` opens a TCP or Unix connection and returns `{readable, writable, close}`. Invalid options throw `TypeError` or `RangeError`; network failures and aborts reject the Promise. The stream directions can be shut down independently; call `close()` in `finally` to release the full connection.

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

`listen(options) -> Promise<Listener>` binds and returns `{address, accept, close}`. `address` describes the bound network endpoint; for TCP port `0`, read the selected port from `listener.address.port`. Each `accept()` resolves to a `Connection`. Closing the listener cancels pending accepts and prevents new connections; already accepted connections remain open until closed.

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

`listener.address` is `{network: "tcp", host, port}` for TCP, or `{network: "unix", path}` for Unix sockets. For a TCP listener bound with port `0`, `port` contains the selected port.

## Listener.accept

`listener.accept() -> Promise<Connection>` waits for one incoming connection. Closing the listener rejects a pending accept. Each accepted connection has its own lifetime; closing a listener does not close accepted connections.

## Listener.close

`await listener.close()` stops accepting and releases the bound listener. It is safe to call from cleanup. Close each accepted connection separately.

## Connection.readable

`connection.readable` is a `ReadableStream<Uint8Array>`. It produces received bytes and closes at peer EOF. Cancelling the reader shuts down only the read direction. Read chunks may have any size; do not assume one read corresponds to one write.

## Connection.writable

`connection.writable` is a `WritableStream<Uint8Array>`. Writes accept only `Uint8Array`. Closing the writer sends FIN but leaves the read direction open. Aborting the writer terminates only the write direction.

## Connection.close

`await connection.close()` fully closes the connection and ends both directions. Call it even after a read or write side ends, and use `finally` so errors do not leak an open connection. Network errors reject pending stream operations.
