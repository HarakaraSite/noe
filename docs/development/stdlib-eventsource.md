# EventSource

[Roadmap](stdlib-roadmap.md) · [Globals reference](../../internal/apidocs/content/stdlib-globals.md#eventsource)

2026-11-06. Phase 9.2 adds the standard EventSource API for SSE consumers.
The normative reference is the
[WHATWG server-sent events specification](https://html.spec.whatwg.org/multipage/server-sent-events.html).
WebSocketStream and custom EventSource headers remain outside this slice.

## Public contract

`new EventSource(url, {withCredentials: false})` returns an EventTarget
immediately in CONNECTING state. The URL must be absolute because NOE has no
document base URL. Invalid URL syntax throws a SyntaxError DOMException;
unsupported transport schemes fail the connection asynchronously.

The API provides read-only `url`, `withCredentials`, and `readyState`, the
CONNECTING/OPEN/CLOSED constants on constructor and prototype, `onopen`,
`onmessage`, `onerror`, named events through `addEventListener`, and idempotent
`close()`. Messages are MessageEvents with string data, final-response origin,
and lastEventId. Closing is synchronous, cancels the current attempt/body read,
clears pending reconnect timers, and does not emit a close event.

HTTP 200 and the text/event-stream MIME essence are required. MIME parameters
are accepted. Other statuses, including 204, invalid MIME, host policy
refusals, and buffer overflow are terminal and emit an error event in CLOSED
state. Transient network failures and EOF emit error in CONNECTING state and
reconnect, with a 3-second default that a valid `retry:` field can replace.
Completed blocks update the Last-Event-ID header for reconnection, including
id-only blocks and empty-ID resets. EOF discards an incomplete event.

The parser handles split UTF-8 sequences and BOM, replacement decoding,
CR/LF/CRLF line endings across chunks, comments, named events, multiline and
empty data, and ignored unknown fields. Embedded NUL invalidates an id field;
retry fields require ASCII digits. Event delivery has task boundaries, so
microtasks and close from one event run before a later queued event.

## Capability and ownership

EventSource is installed with Fetch in managed profiles and inherited by
Workers. No new public Go option field or network capability is added.
Selecting Serve or WebSockets alone does not grant EventSource. Low-level
Network installation requires a Loop and the WebAPIs/Timers prerequisites;
the existing no-WebAPIs and synchronous Fetch modes remain usable without it.

`eventsource.go` copies the chosen HTTP client and reuses Fetch's `doFetch`,
streaming `bodyReader`, redirect authorization and response delivery.
`eventsource_api.go` supplies the EventTarget interface, parser and reconnect
state. Only Go-owned request data enters goroutines; runtime values stay on
the owner. Request contexts, body cleanup, queued delivery and timers belong
to the same execution scope as Fetch. Worker termination and execution close
stop that work without closing the borrowed HTTP client.

The default EventSource client has no whole-subscription timeout. An explicitly
provided client retains its total timeout, transport and TLS configuration.
Unlike Fetch's lifetime-response cap, FetchOptions.MaxBodyBytes limits one
buffered partial event/line (32 MiB default; negative means unlimited), so
small events can be received indefinitely.

`withCredentials: true` permits a caller-provided HTTP client cookie jar;
false uses the same client settings without that jar. NOE has no browser cookie
store, document origin, or CORS enforcement. Host transports can implement
authentication independently. There is no automatic provider authentication,
custom request-header option, or POST-body extension in this implementation.

## Compatibility

This is an additive global. Existing Fetch, WebSocket, Serve, and module
exports retain their contracts. Public Go option layouts and engine
dependencies are unchanged. No compatibility adapter or noebot migration is
required. Existing scripts can keep their Fetch-based SSE consumers.

The local example at `examples/http/event-source.js` receives message and
named events over a loopback server, then closes both resources. The same
example is used by the CLI run/eval/Worker/fixture-agent integration check.

## Validation

Implementation and local verification are complete:

- Full unit suites for the maintained stdlib, public stdlib, execution
  bindings, runtime, and API catalog. External consumer wrappers were excluded
  from this unit run because the direct consumer checks below cover them.
- Focused EventSource race checks after the final constructor conversion
  checks: parsing/chunk boundaries, named and empty messages, EOF and broken
  transport recovery, persistent IDs and explicit resets, terminal status/MIME
  handling, URL/redirect permission refusals, close from handlers, blocked
  header/body cancellation, TLS/cookie selection, Worker inheritance and
  termination, and per-event versus lifetime stream bounds.
- CLI run/eval, Worker, and fixture-agent run_js execute the loopback example,
  receiving both messages and named events before closing the resources.
- External public Go consumer builds and runs with a checkout replacement and
  a standalone module artifact, without a checkout replacement in the latter.
  The consumer includes an EventSource-only subscription under Fetch authority.
- Public TypeScript positive/negative fixtures, affected-package vet, Go
  formatting, documentation links, and whitespace checks.
- Independent read-only review found a nil Allow callback on redirect, a
  property-handler double-dispatch/getter issue, and an ID reset on reconnect.
  All were corrected with regression cases; the bounded fix-only re-review
  reported no remaining findings. EventSource now reuses EventTarget's
  existing handler registry through an internal symbol-keyed helper.

Execution evidence is from Linux and loopback HTTP/HTTPS fixtures. No paid
provider requests, releases, installed binary replacements, or service
restarts were performed. Logs and the consumer module artifact are under
`/var/tmp/noe-eventsource-q6gsfklc/`. The source snapshot and verification
record preserve the final implementation hashes; subsequent documentation-only
updates do not invalidate the source checks.
