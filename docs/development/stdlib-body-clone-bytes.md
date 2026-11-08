# Fetch body clone byte isolation

Published and installed at both vm201 paths in v2.24.0. The local validation and
installation statements below describe this slice's implementation completion.
See the [publication/install checkpoint](../releases/release-checkpoints.md#v2240-publication-and-installation--2026-11-08).

Local continuation approved after `66c5703`, `efa3f82` and `48659fe` on
2026-11-08. The [API policy](stdlib-api-policy.md),
[Fetch body clone](https://fetch.spec.whatwg.org/#concept-body-clone) and
[Streams tee wrapper](https://streams.spec.whatwg.org/#readablestream-tee)
guide the selected scope. No publication, installation or other-project work.

### Selection and scope

The `48659fe` development binary reproduces corruption across Request/Response
clones: mutating one raw Uint8Array chunk changes the other body's bytes;
transferring its buffer detaches the other branch and breaks consumption. This
affects code forwarding a body chunk to a Worker or editing streamed data while
retaining a clone for parsing or retry. Read-all helper result isolation from
`efa3f82` does not cover raw body chunks.

Body clone now uses a Go-private byte-copy mode of the existing tee implementation.
Each ordinary ArrayBuffer-backed Uint8Array is copied for the second branch before
either branch receives it. Copies preserve the view offset/length and backing
buffer contents. SharedArrayBuffer memory remains shared intentionally, with a
distinct view object. Generic public tee retains shared chunk identity, including
when extra arguments are supplied. No cloning option or factory is published.

The existing tee's source reads, acceptance/error ordering, cancellation adoption
and native cleanup remain shared. Copy failure errors the branches and adopts
source cancellation cleanup. Finite body markers, Content-Length, redirect replay,
headers, grants and native backends remain unchanged. The per-runtime factory
permits either installer order, requires no WebAPIs for byte-only consumers and
is removed on runtime close.

This is byte isolation for valid body chunks, not a general cloneForBranch2
implementation for arbitrary values. Non-byte chunks keep their existing invalid
body handling. Full BodyInit/BufferSource conversion/error mapping, resizable
buffer metadata, BYOB and byte-stream controllers remain separate candidates.

### Validation

Evidence: `/var/tmp/noe-body-clone-bytes-xp2univ6`. Initial focused tests pass for
both body classes and both branch directions, finite/stream/subview chunks,
mutation and buffer transfer, repeated clones, shared-memory behavior, copy-failure
cleanup, incoming HTTP clone data, bridge privacy/order/lifetime, prior tee
lifecycle and finite HTTP replay. Impact-based package/race, main/Worker consumers,
SDK/example checks and bounded independent review are complete. Both affected
stdlib packages, focused race, catalog/lint/vet and seven CLI contract/example /
HTTP/Worker/reference/SDK checks pass. The prepared MCP SDK bundle, lockfile and
source fingerprints are unchanged; no real provider or login is used. An actual
Worker port transfer and real native HTTP request/response cloning are covered.
Checkout and standalone module-artifact consumers pass in main and Workers;
exact runtime/declaration source continuity is recorded. Unchanged type evidence
is reused rather than repeating unrelated checks.

The bounded independent review has no required findings. It checked byte-copy
timing, buffer metadata, shared memory, exceptional cleanup adoption, unchanged
tee ordering, namespace privacy, installer order/rebinding and runtime cleanup.
Independent fresh-CLI probes also confirm cleanup rejection propagation and
captured intrinsic behavior with author-defined getters/replaced globals.

Implementation, validation, review and local commit are complete. The original
three commits are retained. Both installed paths remain the v2.23.0 asset, SHA256
`7b7ff1dffbe40b35339e3f999b7a247442f4e91384104d211b6e226ed7cc4cd9`.
No push, publication, binary replacement or service change is included.

Deno 2.9.7 also shares ordinary default-stream Body clone chunks in these baseline
cases. The primary Streams wrapper invokes tee with cloneForBranch2 true; the
normal public tee invokes it with false. Keep the primary byte-isolation assertions
separate from that runtime difference. Neither full Deno nor full Web conformance
is a completion condition for this change.
