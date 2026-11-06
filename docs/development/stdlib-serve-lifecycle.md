# Serve receive limits and asynchronous shutdown

2026-11-07. The user requested both remaining Serve improvements after the
[HTTP entrance migration](stdlib-noe-http.md), with breaking changes allowed.
This implements the current [API policy](stdlib-api-policy.md) through the
existing listener, body-reader, loop, and execution ownership machinery.
Implementation is local and unreleased.

## Incoming request-body limits

`Profile.Modules.Serve.MaxBodyBytes` retains its Go type and default:

| Setting | Behavior |
| --- | --- |
| Zero | Default 32 MiB limit |
| Positive | Finite limit on the request bytes consumed |
| Negative | Unlimited input, replacing the previous empty-body behavior |

Body reads at a finite boundary check actual EOF with a read of at most one
additional byte. An exact-length body succeeds. Any excess rejects with a
`RangeError` carrying `code: "ERR_HTTP_BODY_TOO_LARGE"`. The probe byte is
never returned to JavaScript, and Content-Length is not used to guess whether
the actual body overflows. A stream consumer may already have received the
permitted prefix before a later read rejects; body collectors reject the
collection instead of returning a successful truncated body.

Uncaught overflow before response headers produces a 413 response. `onError`
may provide its own Response. If response streaming has already started, its
body fails and the connection is aborted; a later status cannot replace the
sent headers. Other handler errors keep the existing onError/500 behavior.

There is no new JavaScript limit option, automatic body drain, or read-ahead.
GET/HEAD handling, one-shot Request ownership, zero-high-water-mark demand,
bounded native read chunks, Expect: 100-continue ordering, and input cleanup
after response sending remain. A caller that intentionally reads only a
prefix has not established EOF and does not trigger an implicit full-body
validation. Cancelling a body still stops read demand while preserving an
independent response where the connection allows it.

## Close and disposal

```js
import {serve} from "noe:http";

{
  await using server = serve({port: 0}, request => Response.json({url: request.url}));
  console.log(server.url);
  // Leaving the scope awaits server cleanup, including on an exception.
}
```

`serve()` still creates a server synchronously. `server.close()` now returns
`Promise<void>` and repeated calls return the same promise, including after
completion. It closes handler admission immediately and performs graceful
HTTP shutdown outside the JavaScript owner. Active handlers and timers can
continue, and the promise resolves to undefined after listener, HTTP handler,
and pending input cleanup. Shutdown allows up to five seconds before forcing
remaining HTTP connections closed.

`server[Symbol.asyncDispose]()` delegates to `close()`, enabling `await using`
on normal and exceptional scope exit. No synchronous close alias or adapter is
added. Existing callers that need completion migrate to `await server.close()`.

A shutdown handler can initiate `server.close()` and return its response.
Await the close promise outside that handler or response producer: its own
HTTP work is part of what graceful shutdown waits for. Awaiting that work's
close promise from inside it prevents its graceful completion until forced
cleanup. The agent-server example starts and awaits close in its deferred
shutdown sequence after the acknowledgement returns.

Runtime cancellation, execution teardown, and Worker termination keep their
independent forced path and do not wait for the graceful deadline. That path
closes native connections and joins body I/O without requiring posted JS work.
Upgraded WebSockets retain their separate ownership; closing the HTTP server
does not replace closing those sockets.

## Implementation

`serve_body.go` retains its admission mutex and read WaitGroup. Finite limits
now probe for overflow, negative values remain unlimited across reads, and
the owner-thread bridge publishes the coded RangeError. `serve.go` maps an
uncaught coded error to 413 before headers and leaves an already-started
response on the existing stream-failure path.

The close function caches one native Promise, closes handler admission, and
tracks graceful shutdown through the execution-owned loop. A separate loop
hold lasts until completion is posted, so releasing the listener's hold
cannot end the run before promise settlement. Runtime-hook removal and JS
settlement stay on the owner; forced cleanup remains independent of the
graceful operation. The existing five-second allowance and shared backends
remain. No Go configuration field or dependency changes.

## Validation

Evidence is retained at `/var/tmp/noe-serve-policy-bOYsbk`, with command JSON/logs,
the final source manifest, and public module-artifact consumer evidence.

- Full core/public stdlib and affected module-map/composition/execution/runtime/
  CLI suites. New tests cover active handlers/timers during close, shared
  promise identity and undefined completion, immediate port reuse, close
  initiated by a handler, normal/exceptional async disposal, graceful timeout,
  and cancellation during an active graceful close.
- Boundary/overflow/unlimited body reads, collectors, RangeError/code,
  default 413 and onError override, response-stream abort, terminal read
  errors, and cancelled boundary probes with Expect are verified.
- Relevant real-CLI Serve/HTTP/WebSocket/EventSource/docs E2E, Worker and
  fixture-agent paths, targeted race checks, agent-server normal/graceful/
  SIGINT cleanup, and authentication-helper startup.
- Public consumers built and ran from a checkout and an independent module
  artifact, with CGO disabled and compilation for all six release targets.
  The consumer directly exercises async disposal, request overflow, and a
  shutdown handler completing its response.
- TypeScript fixtures, API catalog, changed JavaScript lint, affected Go vet,
  and final diff checks. Independent bounded static review found no blocker.
- The initial focused lifecycle check caught incorrect void-promise encoding;
  the owner now supplies the actual undefined value, and the focused check
  plus complete public suite passed. No unresolved failure remains.

The candidate executable is `/var/tmp/noe-serve-policy-bOYsbk/noe`. Installed
executables and running services were not replaced; no push or release was
performed. Previous native filesystem/process verification remains reusable.
Noebot remains outside this work.
