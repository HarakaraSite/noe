# Fetch body consumption

Local continuation approved after Headers/Response commit `66c5703` on
2026-11-08. The [API policy](stdlib-api-policy.md),
[Fetch Body mixin](https://fetch.spec.whatwg.org/#body-mixin) and
[Streams read-all-bytes](https://streams.spec.whatwg.org/#read-loop) guide this
bounded correction. Publication, installation and other repositories are excluded.

### Selection and behavior

The `66c5703` development binary reproduces concrete inconsistencies in shared
HTTP/SDK paths: a reader lock reports bodyUsed true, text() bypasses the lock on
finite bodies, cancelled finite bodies remain readable, bytes() exposes storage
shared with a clone, and string stream chunks become zero bytes instead of errors.
These affect Request/Response helpers used by HTTP examples, the Exa helper and
the prepared MCP SDK. Existing validation for unrelated APIs remains reusable.

Body helpers now read the current stream for finite and incoming bodies alike.
Acquiring a reader only locks; reading or cancellation disturbs the body.
Locked/disturbed bodies reject helpers and cloning with TypeError. Helper reads
retain the stream's reader lock and consume it once; null bodies remain reusable.
Each Uint8Array chunk is copied before requesting the next, and non-byte chunks
reject. Returned byte arrays do not expose body/clone storage. Producer errors
retain their original identity.

Finite Request inputs snapshot the exact byte view. Transfer owns the current
stream, and clone tees it, including finite bodies. An internal finite-body marker
preserves native buffered dispatch, Content-Length and redirect replay after
clone/transfer. Genuine stream uploads remain incremental. The same native HTTP
client, admission hooks, limits, cancellation and runtime cleanup are retained.
Fetch captures headers before buffering yields, and checks abort again before
native dispatch. Mutations or aborts immediately after fetch therefore retain
the established request snapshot and cancellation behavior.
The server checks response usability before sending headers, so locked/cancelled
responses enter onError instead of sending stale bytes or truncated success.

No signature, grant, compatibility layer or dependency is added. Internal
`_take`, `_consume` and `_used` properties are removed; callers should use the
standard body stream and helpers. Full BodyInit/BufferSource/WebIDL conformance,
generic stream tee behavior and browser policy remain outside this slice.

### Validation

Evidence: `/var/tmp/noe-body-consumption-mxrejb5s`. Both affected stdlib packages
pass; final changed Fetch/copy sites also pass focused unit/race verification.
The tests cover finite/stream reader ownership, cancellation, exact byte copies,
invalid chunks, producer errors, incoming JSON, rejected server responses and
clone/transfer through real HTTP 307 replay with admission on every hop. Direct,
cloned and transferred finite requests preserve entry-time headers and observe
immediate abort with the exact reason and zero native admissions.

Seven CLI example/reference/SDK checks pass, including HTTP JSON/streaming
roundtrips, 43 Exa stub checks and the prepared MCP SDK's initialization, tool list
and three parallel Unicode echo requests. SDK bundle/lock/source hashes are
unchanged; no real provider calls occur. Example lint, catalog, formatting and
vet pass. Unchanged declarations/type verification from `66c5703` is reused.
Unrelated package-wide suites were not repeated; current main/Worker/CLI consumers
exercise the affected installation paths. Checkout and standalone module-artifact
consumers pass, with exact runtime/declaration source continuity recorded.

One bounded independent review found two P2 regressions from the new buffering
await: an immediate abort could be missed, and subsequent public Request-header
changes could reach the transport. Both are fixed and covered for direct, cloned
and transferred finite requests. The single fix-only re-review independently
confirmed exact abort reasons, zero aborted requests, entry-time headers and
byte copies without user slice calls. No required finding remains in that scope.

Implementation, verification and local commit are complete. The original
`66c5703` commit is retained. Both installed executables remain v2.23.0, SHA256
`7b7ff1dffbe40b35339e3f999b7a247442f4e91384104d211b6e226ed7cc4cd9`.
No push, release publication, installation or service change is included.

The Deno 2.9.7 baseline agrees on the original lock/cancel/clone-result/chunk
reproductions. Its reused-producer-buffer result differs from the primary
read-loop byte-copy assertion, and its pre-errored optimization leaves bodyUsed
false. Keep these primary assertions separate from the passing common checks.
The existing generic tee implementation and all stream scheduling are not claimed
to be fully standards-conformant by this change.
