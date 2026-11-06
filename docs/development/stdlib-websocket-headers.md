# Phase 9.1: WebSocket handshake headers

[Roadmap](stdlib-roadmap.md) · [Go embedding](../go-embedding.md)

The user chose Web communication work before Web Crypto, starting with custom
WebSocket request headers. EventSource and WebSocketStream are separate
follow-ups. The implementation is committed and locally verified; this feature has not
been released.

## Contract

```js
const socket = new WebSocket("wss://example.com/feed", ["chat.v1"], {
  headers: { Authorization: "Bearer <token>", Origin: "https://app.example" }
});
```

The optional third argument is a NOE extension. The browser
[WebSocket constructor](https://websockets.spec.whatwg.org/#dom-websocket-websocket)
accepts url and subprotocols; NOE keeps that argument ordering. Use
`new WebSocket(url, undefined, {headers})` when no subprotocol is needed.
An object in the second slot continues to be treated as a protocol value,
rather than being reinterpreted as options. Third-argument options may be
omitted or null; omitted/null headers means no custom fields.

Headers accepts a record, iterable of name/value pairs, or Headers instance.
Names and values are synchronously converted to strings, then copied to a
per-connection Go `http.Header`. Later caller mutations cannot alter the
handshake. The captured iterator method is invoked once with its original
receiver; mapping failures close an iterator as usual. Getter/iteration/string
conversion errors propagate before native connection allocation.

Header names must be nonempty HTTP tokens. Values may contain HTAB and
ordinary HTTP field bytes, but CR, LF, NUL, other control bytes, and DEL are
rejected. Validation precedes trimming of outer SP/HTAB. Header-validation
errors throw TypeError without exposing values. Duplicate iterable entries
remain repeated HTTP fields; Headers supplies its existing combined entries.
The handshake owns these case-insensitive names:

- Host, Connection, Upgrade.
- Sec-WebSocket-Key, Sec-WebSocket-Version, Sec-WebSocket-Protocol,
  Sec-WebSocket-Extensions.
- Content-Length, Transfer-Encoding, Trailer.

Use the URL, protocols argument, and existing connection implementation for
those fields. Authorization, Origin, Cookie, and ordinary application fields
can be supplied explicitly. No credential headers are added automatically.

## Implementation and ownership

`requestHeaders` in `internal/stdlib/websocket.go` normalizes JavaScript input.
`websocket_headers.go` validates and copies strings on the runtime owner.
Only Go-owned URL/protocol/header values enter the asynchronous handshake.
Custom fields are serialized with `http.Header.Write` alongside the generated
handshake fields. The same existing WebSocket connection handles messages,
framing, TLS, and close events.

Validation completes before URL permission checks, socket creation, loop
holds, or goroutine work. A denied valid request still follows the existing
URL Allow callback. TLS configuration, handshake timeout, message limit,
execution-scope cleanup, and Worker inheritance continue through the existing
WebSockets configuration. Go embedding needs no new exported option field;
JavaScript header input is per connection. Fetch is an independent capability.

## Compatibility

Existing valid one/two-argument constructors and server-side upgrades retain
their behavior. The constructor's function length remains two. The third
argument now supplies recognized options; callers that passed unrelated
non-object values there may now receive TypeError.

Protocol names enter an HTTP header and now require token syntax. Malformed
names (including CR/LF or empty tokens) throw SyntaxError before connecting,
instead of being sent in a malformed handshake or failing asynchronously.
Ordinary valid protocol strings and arrays keep their existing behavior.
There is no compatibility adapter for malformed handshake input.

Type declarations add WebSocketOptions and the optional third argument, with
positive/negative compile fixtures. The built-in globals reference and public
Go guide describe the extension and header ownership. The engine and public
Go WebSockets option structs are unchanged.

## Validation

The implementation and local verification are complete:

- Full unit suites for the maintained stdlib, public stdlib, execution
  bindings, runtime, and API catalog. External portable consumer wrappers were
  replaced by the direct checkout/artifact consumer checks below.
- Focused WebSocket race suites for the maintained and public stdlib,
  including WS/WSS connections, main/Worker profiles, and malformed input.
- Real handshake capture confirms Authorization, Origin, Cookie, duplicate
  fields, subprotocols, generated Host/key fields, and synchronous snapshot
  behavior. TLS tests trust the local fixture certificate through the existing
  Go TLS option, including Worker inheritance. WebSockets-only profiles leave
  fetch disabled.
- Invalid header shapes/names/values and owned fields fail before the URL
  policy callback. Valid headers still follow permission rejection. Validation
  failures omit header values; getter exceptions retain their original reason.
- CLI run/eval, Worker, and fixture-agent run_js complete an authenticated
  message exchange and clean close through the existing serve/upgrade path.
- External public Go stdlib consumer builds and runs with a checkout
  replacement and a standalone module artifact. It exercises headers through
  the public profile without Fetch or FS.
- TypeScript positive/negative fixtures, affected-package vet, Go formatting,
  relative documentation links, and whitespace checks.
- Independent read-only review found a header iterator getter being evaluated
  twice, which could silently omit fields. The captured-method conversion and
  self-deleting-getter/IteratorClose regressions resolve that finding. The
  bounded re-review reported no remaining findings.

Execution evidence is from Linux, with loopback WS/WSS fixtures rather than
paid providers or external services. Logs, source manifests, and public Go
consumer artifacts are under `/var/tmp/noe-websocket-headers-syq4pang/`.
The implementation is committed and locally verified, but unreleased. Installed binaries,
services, the engine dependency, and upstream repositories were not changed.
