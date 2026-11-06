# WebSocket connected close and native interruption

Implemented locally on 2026-11-07 after opening-lifecycle checkpoint
`248e74a`. The user approved both blocked-send cleanup and established
closing behavior. The same native connection, WebSocket class, grants,
custom-header factory, and upgrade helper remain in use. No dependency,
public Go configuration field, or new JavaScript method is added.

## Contract and migration

`socket.close(code?, reason?)` still returns undefined and immediately enters
CLOSING. Existing close-argument checks run before state changes or repeated
close no-ops. Closing during CONNECTING retains the previous cancellation
correction. Established closing now waits for a completed frame exchange
instead of forcing a drop after 50 ms.

A no-argument close sends an empty close frame. Pass `close(1000)` when that
status is required. A supplied reason without a code uses status 1000.
The close event uses the first peer close code and reason, rather than the
local arguments. An empty peer close has code 1005; absent a peer close,
the event has code 1006 and an empty reason. Simultaneous closes may therefore
report different codes at the two ends. `wasClean` requires successfully
sending and receiving close frames before closing the transport.
These selected semantics follow [WHATWG WebSockets](https://websockets.spec.whatwg.org/)
and [RFC 6455 sections 5.5.1 and 7.1](https://www.rfc-editor.org/rfc/rfc6455.html#section-5.5.1).

NOE allows five seconds from the start of closing for queued writes and the
peer reply together. During this wait JavaScript timers and other sockets
can progress. Missing replies, failed writes, premature EOF, invalid close
payloads, or timeout produce an error event followed by an unclean close.
Both events observe CLOSED. Diagnostics remain on the error event's error
and message fields; the close reason remains the peer's protocol value.
Callers should wait for the close event when graceful completion matters.
The HTTP listener's close promise does not close upgraded WebSockets.

## Native state and resource ownership

Transport Close no longer takes the serialization lock held by a pending
frame write. A single native close interrupts readers and writers; concurrent
close callers wait for that native action. This fixes execution cleanup and
Worker termination waiting indefinitely on a peer that stopped reading.
Write serialization, payload snapshots, client masking, and binary-message
handling continue on the same transport. Idle writers also observe their
context, so a legacy loop ending without queued data does not retain one.

Local close queues its frame after already accepted messages. The writer
pops one frame at a time so a peer close can stop remaining unsent data and
prioritize the response after an in-flight frame. Only one close frame is
queued or written. A peer close validates its payload before contributing
to a successful exchange. One-byte or oversized bodies, invalid UTF-8,
forbidden wire status values, and fragmented/oversized control frames fail
rather than being labelled clean.

Local close starts a native closing reader, serialized with any ordinary
read. It discards later data and reads the peer response without requiring
the JS handler to return or start another receive. Ordinary open-state reads
retain their existing demand behavior. The first valid peer payload is cached
for the owner, including when a failure callback arrives during an active read.

Native workers record frame completion before posting owner work. Once both
frames succeed, they close the native transport and notify the deadline
worker, even if JavaScript is temporarily busy. The owner then publishes the
peer outcome and settles the internal completion promise after cleanup.
Native and published state have separate synchronization. A pending read is
allowed to report an already received peer close before an I/O failure fixes
the final event code. The loop remains held until owned work has delivered
its result. Active hooks, pending writes, timers, and loop holds are removed
once.

Forced runtime/execution/Worker cleanup remains Go-only and independently
interrupts I/O; it does not run JavaScript while joining native work. Shared
capability selection and borrowed runtime/loop ownership remain unchanged.
The transport closes once the frame exchange completes; it does not add a
separate client wait for the server's TCP FIN.

## Verification

Evidence: `/var/tmp/noe-websocket-close-apjtaav1`. The baseline native test
uses a write barrier and an unread pipe to reproduce Close waiting on the
writer mutex, with cleanup that bounds the old failure. Focused protocol
and race tests check native interruption and close-payload validation.
Public regressions cover the observable handshake and cancellation paths.
Existing WebSocket, opening-cancellation, TLS/header, binary, module-selection,
and Worker checks are reused or rerun according to the affected source.
CLI and independent public-consumer fixtures include a peer-initiated empty
close and retain authenticated Blob exchange.

The final affected native/core/public selection passes with the race detector,
including all eight public closing regressions and the native paused-owner
cases. TypeScript fixtures, embedded catalog/reference checks, and vet pass.
Real CLI run/eval/Worker/fixture-agent modes and checkout/standalone module
consumers pass. A review reproduction exposed an intermediate busy-owner
false timeout; the native closing reader fixed it, and the same 5.5-second
handler now closes cleanly. The review also corrected the blocked-peer test
fixture and in-flight drain failure ordering. Focused re-review found no
remaining actionable issue.

Final results and commands are recorded in `verification.json`; source hashes
are in `source-manifest.json`. The new temporary candidate is kept alongside
that evidence. No noebot work, push, publication, executable installation,
or running service restart is included.
