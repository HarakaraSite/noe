# WebSocket opening cancellation and close validation

Implemented locally on 2026-11-07 under the user's autonomous standard-library
and breaking-change approval. The prior process/HTTP/Serve checkpoint is
`3997dc0`. This correction shares the existing WebSocket backend and does not
add another API, dependency, capability, or Go configuration field.

## Observable behavior and migration

`WebSocket`, `noe:http.createWebSocket`, and sockets reserved by
`noe:http.upgradeWebSocket` use the same lifecycle. Closing during CONNECTING
cancels opening. Failure emits error before an unclean close with code 1006;
CLOSED is visible to both handlers. A delayed opening result cannot reopen
the socket or deliver messages. No message is dispatched after leaving OPEN.

Close arguments are validated before state changes or repeated-close no-ops.
After WebIDL unsigned-short clamp conversion, supplied codes must be 1000 or
3000–4999, otherwise InvalidAccessError is thrown. A reason exceeding 123 UTF-8
bytes throws SyntaxError. Invalid arguments preserve the previous state.
These selected behaviors follow the [WebSocket standard](https://websockets.spec.whatwg.org/).

Callers that sent reserved or out-of-range codes must choose an allowed code.
Shorten reasons by UTF-8 bytes rather than JavaScript string length. Stopping
an unused connection immediately no longer waits for a successful handshake.
The host factory retains its synchronous return, custom-header snapshot,
URL authorization, TLS settings, and standard WebSocket identity.

At the opening-correction checkpoint, the established close policy retained
code 1000 by default and a short forced-close allowance. The subsequent
[connected closing correction](stdlib-websocket-closing.md) replaces that
policy with a completed frame exchange and an empty no-argument close.

## Native ownership and cancellation

Each outbound opening has a child cancellation context. Its timeout covers
TCP dialing, TLS negotiation, and the HTTP upgrade together. Once TCP is
connected, a context watcher closes the raw connection to interrupt pending
HTTP reads or writes. The watcher is stopped and joined before handing off
an established transport; clearing the handshake deadline lets an active
connection continue without that opening time limit.

A second temporary watcher covers the interval between completed handshake
and owner delivery. It also protects legacy loops without an execution
scope when delivery is discarded at shutdown. Scoped executions retain a
native cleanup handle until the owner starts the socket. The owner checks
that the socket has not ended before starting its active reader/writer and
settling the opening promise. Runtime, execution, and Worker cancellation
continue to use the existing ownership model.

Explicit CONNECTING close rejects the opening promise on the owner and
cancels native work. Forced cleanup remains Go-only. Closing a reserved
server socket removes its pending upgrade token; a subsequent attachment
closes the incoming connection rather than resurrecting that socket.
Final cleanup removes connection hooks even if an earlier close already
marked the socket ended, and releases its loop hold once.

## Verification

Evidence is recorded in `/var/tmp/noe-websocket-lifecycle-P6J6DU`.
The regression fixture holds an actual HTTP upgrade response behind a
barrier, allowing close and execution cancellation to run while opening is
blocked. Late-response release checks event ordering and peer cleanup.
A separate paused-owner regression reproduces a successful handshake queued
before explicit close. Both this regression and the held-response failures
were reproduced at `3997dc0` and pass after the fix. Worker termination and
borrowed execution cleanup also pass. Other focused checks cover upgrade
reservation cleanup, invalid close arguments, and existing TLS/header/binary
paths. The affected core/public tests pass with the race detector.

Real CLI run/eval/Worker/fixture-agent modes and independent checkout/module
artifact Go consumers pass, including immediate opening cancellation and
validation. TypeScript fixtures, the embedded API catalog, vet, and whitespace
checks pass. A bounded independent static review found no actionable finding.
The fresh candidate is `/var/tmp/noe-websocket-lifecycle-P6J6DU/noe`; it is not
installed. See `verification.json` for commands and resolved baseline failures.

The unchanged filesystem, process, Fetch-limit, and Serve implementation
results remain reusable from the previous checkpoint. No push, release,
installed executable replacement, running service restart, or noebot work
is part of this slice.
