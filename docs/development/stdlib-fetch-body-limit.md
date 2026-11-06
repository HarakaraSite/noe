# Fetch response-body overflow correction

2026-11-06. This fixes the known response-limit issue recorded in the earlier
standard-library handoff. It accompanies, but is independent of, the
[host HTTP entrance migration](stdlib-noe-http.md). The user authorized the
breaking behavior change. This implementation is included in v2.16.0.

## Behavior

Previously, a Fetch response exceeding `FetchOptions.MaxBodyBytes` ended
successfully at the configured prefix. Callers could mistake incomplete text,
JSON, or binary output for a complete response.

Now buffered Fetch and streamed response reads reject overflow. Exactly the
configured number of bytes is accepted only when the peer reaches EOF there.
Zero still selects the 32 MiB default and a negative setting remains unlimited.
No JavaScript option or public Go field is added.

The bound is evaluated against actual response bytes, not Content-Length.
At the boundary the implementation reads at most one additional byte to
distinguish EOF from excess; that probe byte is never returned to JavaScript.
This avoids integer-overflow arithmetic on the configured bound and accepts
an exact-length body even when metadata describes a different representation.

A streaming caller may already have received the permitted prefix before its
next read rejects. Stopping consumption at the cap without requesting EOF
does not establish that the response was complete. Convenience methods such
as `text()`, `json()`, and `bytes()` consume through that check and reject the
whole collection. If a peer sends exactly the cap and leaves its body open,
the next read waits for EOF/excess, cancellation, or the configured timeout.

Buffered failures retain the existing `TypeError("fetch failed")` envelope;
streamed body failures retain `TypeError("terminated")`. Their cause reports
`response body exceeds configured limit`. Owned response bodies are cancelled
and closed on overflow; cancellation while the boundary probe is blocked
also releases the host request. Supplied HTTP clients remain borrowed.

Uploads, redirect behavior, permission callbacks, execution ownership,
AbortSignal behavior and EventSource event-size limits were unchanged. This
fix left Serve's then-prefix cap alone; the subsequent
[Serve lifecycle slice](stdlib-serve-lifecycle.md) aligns its receive-limit
errors and asynchronous shutdown contract.

## Verification

Evidence is under `/var/tmp/noe-http-policy-8FsuKH` alongside the HTTP slice.
Focused tests cover exact versus oversized bodies, chunked stream reads,
convenience collectors, buffered/no-loop operation, mismatched Content-Length,
the one-byte probe bound, and owned-body closure. A public-profile loopback
test aborts a read waiting at the exact cap and confirms that the peer's
request is cancelled. Targeted race checks and the affected core/public,
streaming integration, and E2E checks pass. Independent static review found
no blocker and confirmed the combined data-plus-EOF and non-EOF error paths.
