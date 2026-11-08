# Stream tee lifecycle

Local continuation approved after `66c5703` and `efa3f82` on 2026-11-08.
The [API policy](stdlib-api-policy.md) and
[ReadableStreamDefaultTee](https://streams.spec.whatwg.org/#readable-stream-default-tee)
guide this bounded change. Publication, installed executables, service changes
and other projects are excluded.

### Selection and behavior

The `efa3f82` development binary reproduces two concrete tee problems: the first
branch's cancel promise resolves before underlying cleanup, and a source error
does not discard buffered branch data until a later pull. Request/Response clone
uses this same tee implementation, so premature cleanup completion and stale
data also affect native HTTP response consumers.

Both cancellation operations now adopt one source-cleanup result, including its
rejection. Cancelling one branch waits until the remaining branch reaches EOF /
error, or both cancel and source cleanup finishes. Source cancellation runs once;
its reason array follows branch order. Callers stopping both branches should
start both cancellations before awaiting them, for example
`await Promise.all([left.cancel(), right.cancel()])`.

Source errors propagate to both live branches even with buffered data and no
pending read. Internal read-request steps queue distribution when a chunk is
accepted, preserving already-pending reads before a subsequent error. Unread
buffered data is discarded. Overlapping demand is retained while one source read
is active. Ordinary tee chunk identity and the existing queue/transport mechanisms
remain. Public reader fast paths and pending-promise behavior are preserved.

This does not implement BYOB/byte-stream controllers, Fetch cloneForBranch2 byte
isolation, full BodyInit/BufferSource conversion or complete stream conformance.
No signature, native backend, grant, dependency or compatibility layer is added.

### Validation

Evidence: `/var/tmp/noe-stream-tee-1rb7gg8o`. Focused lifecycle/native tests pass,
including ordered reasons, delayed/rejected cleanup, remaining-branch EOF/error,
queued and enqueue-then-error data, overlapping/slow demand and Body clone errors.
Native Fetch clone cancellation cancels the request context and closes a blocked
response reader exactly once. The scoped contract also passes Deno 2.9.7.
Both affected stdlib packages and focused race pass, including previous Body
consumption and finite native replay/ownership regressions. Six CLI checks cover
the lifecycle contract, new cancellation example, prior body example, real HTTP
clone consumption, reference output and prepared MCP SDK Unicode echo. SDK
bundle/lock/source hashes are unchanged; no real provider or login is used.
Lint, catalog and vet pass. Checkout and standalone module-artifact consumers
cover main and Workers; exact runtime/declaration source continuity is recorded.
Unchanged type validation is reused; unrelated package-wide suites are not repeated.

A bounded independent review found one P2: an extra promise hop discarded a chunk
already accepted by pending reads before a source error. A shared internal
read-request helper now queues tee distribution at acceptance time while retaining
the public read method's ready/pending promise behavior. Regression tests retain
both pending-read delivery and no-reader buffered-data rejection. The single
fix-only re-review independently confirms the fix, public reader promise ordering
and the actual NOE CLI/Deno comparisons. No required finding remains in that scope.

Local implementation, verification, review and commit are complete. Both prior
commits are preserved. Installed executables remain the v2.23.0 asset, SHA256
`7b7ff1dffbe40b35339e3f999b7a247442f4e91384104d211b6e226ed7cc4cd9`.
No push, release publication, binary replacement or service change is included.
