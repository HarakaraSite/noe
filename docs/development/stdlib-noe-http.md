# Host HTTP and WebSocket API migration

2026-11-06. The user authorized autonomous standard-library continuation and
breaking changes. This slice applies the [API policy](stdlib-api-policy.md):
standard Web APIs retain their global entrances, and host-specific functions
use explicit NOE modules. Implementation is local and unreleased.

## Contract and migration

```js
import {serve, upgradeWebSocket, createWebSocket} from "noe:http";

const server = serve({port: 0}, request => {
  const {socket, response} = upgradeWebSocket(request);
  socket.onmessage = event => socket.send(event.data);
  return response;
});

const client = createWebSocket(server.url.replace("http:", "ws:"), {
  headers: {Authorization: "Bearer example"},
});
client.onopen = () => client.send("hello");
client.onmessage = event => { console.log(event.data); client.close(); };
client.onclose = async () => { await server.close(); };
```

| Capability | `noe:http` exports |
| --- | --- |
| Serve only | `serve` |
| WebSockets only | `createWebSocket` |
| Serve and WebSockets | `serve`, `createWebSocket`, `upgradeWebSocket` |
| Neither, including Fetch only | Module absent |

There is no default or `fetch` export. Standard global `fetch`, WebSocket,
Request/Response, Headers, EventSource, and Web Streams remain the normal Web
entry points. `node:http` and bare `http` are removed without adapters. Global
`serve` and `upgradeWebSocket` are no longer installed. Bare `http` can be
assigned by an import map; `node:` and `noe:` prefixes remain reserved.

- Replace old HTTP imports and global listener/upgrade calls with named
  imports from `noe:http`.
- Remove `import {fetch} from "noe:http"` and use global `fetch`. Read its
  bundled reference with `docs("globals", "fetch")`.
- Replace `new WebSocket(url, protocols, {headers})` with
  `createWebSocket(url, {protocols, headers})`. The standard constructor now
  has the two-argument surface; extra arguments do not enable headers.
- The binary default is now `"blob"`. Assign
  `socket.binaryType = "arraybuffer"` before receiving binary messages when
  the caller requires an ArrayBuffer.

`createWebSocket` is synchronous, like construction: it returns the same
global WebSocket type in CONNECTING state. Observe open/message/error/close
events and use `close()` normally, including while connecting. It is not a
readiness promise. `protocols` accepts a string or string array, and `headers`
accepts the existing record, iterable-of-pairs, and Headers inputs. Header
validation and snapshots happen before connection allocation/authorization;
managed handshake/framing headers cannot be supplied.

Blob/File messages now enter the existing binary send queue synchronously.
They previously fell through to text coercion, which broke binary echo with
the new default. Native payload snapshots and message ordering are retained;
default-Blob echo and a following text message are covered together.

## Shared implementation and boundaries

Serve and WebSockets installers return their existing host function values to
`http.go` rather than publishing helper globals. The WebSocket factory uses a
private constructor path on the same class; it does not introduce another
native connection implementation. A private accepted-socket marker replaces
the old public `null` sentinel.

The shared Serve/WebSocket transports, policies, TLS configuration, limits,
upgrade-token handling, event loop, execution cleanup, and main/Worker behavior
remain. Go Profile fields and dependencies do not change. Fetch selection is
independent. Its response-overflow correction is a separate
[behavior fix](stdlib-fetch-body-limit.md).

At the HTTP naming migration stage, Serve retained its old request-prefix cap
and synchronous close behavior. The subsequent approved
[Serve lifecycle slice](stdlib-serve-lifecycle.md) now rejects request overflow,
uses negative limits for unlimited input, and makes `close()`/asyncDispose
asynchronous. Consult that record and the current embedded reference for the
latest contract.

NOE's current examples, authentication helper, hidden instructions, Go
fixtures/consumers, TypeScript, bundled reference, and current guides migrate
together. Historical phase/release records retain their original contracts.
Noebot remains its owner's independent work and was not inspected or changed.

## Verification

Validation passed; evidence is retained under
`/var/tmp/noe-http-policy-8FsuKH`.

- Core and public standard-library suites plus affected module-map,
  composition, execution, runtime, CLI, and API-catalog checks. All eight
  Fetch/Serve/WebSockets selections validate exact exports and removal of
  old modules/globals. Tests exercise synchronous factory identity/startup,
  custom-header validation/snapshots, TLS, Workers, binary Blob/ArrayBuffer
  delivery, Blob binary echo/send ordering, and ignored standard-constructor
  extension arguments.
- Real-CLI Serve/listener ownership, HTTP request/response streaming,
  cancellation, timeout, Worker/agent, WebSocket-header, EventSource, and
  bundled-docs E2E. Relevant example scenarios and authentication help startup
  run against a fresh temporary candidate.
- Targeted race checks for capability registration, WebSocket creation,
  Fetch limit reads, and cancellation while its EOF probe waits.
- Checkout and independent module-artifact public consumers with CGO
  disabled, including compilation for the six release targets.
- TypeScript positive/negative fixtures, changed JavaScript/TypeScript lint,
  affected-package Go vet, and final diff checks.
- Bounded independent static review found no runtime blocker. Initial
  failures were stale test-fixture bindings/concatenated imports and an old
  documentation wording assertion; their focused rechecks passed. No
  failing runtime behavior remains.

The final consumer artifact was rebuilt after correcting Blob sends; the
earlier `consumer/` artifact is retained as intermediate evidence. Use
`consumer-final/` and the `noe-final` candidate for the final implementation.

No release, push, installed-executable replacement, or running-service
restart was performed.
