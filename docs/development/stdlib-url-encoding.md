# URL normalization and query encoding

Approved on 2026-11-08 after v2.22.0 publication and both vm201 installations.
This continuation is local implementation, validation and commit. A subsequent
release/installation is not included. The [API policy](stdlib-api-policy.md)
governs the contracts; the [URL Standard](https://url.spec.whatwg.org/) is primary.

### Scope

Correct USVString/argument conversion and UTF-8 percent encoding for URL path,
query, fragment and credentials in constructors, relative resolution and setters.
Preserve existing percent escapes, empty query/fragment markers and empty path
segments. Normalize percent-encoded dot segments, common special-scheme slash
handling, ASCII scheme/host/default-port serialization and opaque/hierarchical
path distinctions. Keep searchParams identity and bidirectional synchronization.
Match parse/canParse failure behavior without hiding conversion exceptions.

Correct URLSearchParams form encoding/decoding, including malformed percent/UTF-8,
plus, BOM, USVString, iterable/record inputs, required arguments, stable sorting
and live iteration. Keep parameter update/serialization private and branded.
Verify Fetch form bodies, node:url constructor identity, URLPattern consumers,
main/Worker and external Go consumers. Align types, examples and reference pages.

Retain the existing parser and codecs; no dependency, grant or Profile field is
added. Full UTS46/IDNA, IPv4/IPv6 canonicalization/validation, Windows file-URL
quirks and complete URLPattern grammar are outside this slice. Correcting these
specific contracts does not certify every WHATWG URL parser state.

### Primary reference difference

The current URL Standard percent-encodes an opaque-path space immediately before
query/fragment and does not strip that path when queries are subsequently removed.
Deno 2.9.7 retains the older stripping behavior. Keep primary assertions separate
from the common runtime comparison for these cases.

Deno 2.9.7 also exposes an unexpected host/port for a non-authority hierarchical
URL serialized with the protective `/.` prefix (`foo:/.//b`). Primary assertions
require an empty host/port and idempotent serialization; that case is kept out
of the common comparison.

Sequence initialization follows current Web IDL conversion before constructor
pair-length checks. A conversion failure propagates without an IteratorClose
call, and pair iterators remain live after a done result if the list later grows.
These rules are asserted directly; Node/Deno shortcuts are not normative inputs.

### Implementation and migration

URL input and setter values use USVString conversion. The parser uses separate
UTF-8 path, query/special-query, fragment, userinfo and opaque-path percent sets.
Existing percent escapes remain escaped once; malformed percent syntax is retained
where the URL Standard specifies validation without failure. Encoded dot segments
normalize alongside literal dots. Empty segments are retained, and serialization
uses the protective `/.` prefix for non-authority paths that begin with `//`.

Special-scheme ASCII host/scheme/default-port handling and ordinary slash/relative
transitions are normalized. Constructor ports reject invalid/range overflow;
setter overrides preserve or partially update state according to their supported
parse procedure. Opaque paths remain separate from hierarchical paths. Stringifiers
and accessors are branded and private. Setter conversion runs before looking up
the current URL record, including when argument conversion changes href.

The searchParams object remains identical across href/search assignments. Existing
references and live iterators see the current list. Parameter edits use private
form serialization and update the linked query; overriding public toString does
not redirect updates. Empty query/fragment markers remain distinct from absence.
parse/canParse report parsing failures while preserving required-argument and
conversion errors.

URLSearchParams parses form bytes through the existing UTF-8 codec, preserving a
BOM and replacing malformed UTF-8 locally. Literal plus and spaces retain their
distinct form meanings. Iterables, records, argument counts and value filters use
the documented Web IDL conversion rules. Primitive string pairs reject, while
boxed string iterables remain valid pairs. size is readonly; sorting and iteration
use the current UTF-16 list order. No new host capability or Go Profile field is
added. Global and node:url constructors remain identical; node:querystring retains
its separate conventions.

Callers that previously received raw spaces/Unicode or stale parameter objects
must use the normalized href/components and retain the same live query reference.
Use the encoder for URL components rather than assuming form encoding and URL
query encoding are interchangeable. Public types, embedded references, the local
example and main/Worker consumers describe and exercise these corrected contracts.

### Verification and review

Evidence: `/var/tmp/noe-url-encoding-87ffe6i1`. All seven affected stdlib/profile /
runtime/CLI packages, focused URL/query race, positive/negative declarations,
reference catalog, example lint, Go formatting/diff checks and vet pass. Actual
CLI primary/common/example/reference and node:url/global constructor identity
checks pass. Checkout and standalone module-artifact consumers build/run,
including main/Worker URL normalization, query synchronization and Fetch form
bodies. Source fingerprints establish continuity with the tested artifact.

The installed v2.22.0 reproduces raw Unicode/spaces, unnormalized encoded dots /
ports, malformed-query fallback and replaced query-object defects. The development
candidate matches Deno 2.9.7 on six constructor cases, twelve setter cases, live
query synchronization, form decoding/serialization, filters and size. The primary
differences above remain explicit and do not weaken the candidate assertions.

A bounded independent review found two P2 issues (rootless pathname normalization,
primitive pair sequences) and one P3 (conversion precedence). All were fixed.
The parent corrected reentrant setter record lookup; the review's additional
invalid-input iterator-close discrepancy was also aligned with current Web IDL.
One fix-only re-review confirmed the corrections with no remaining finding.
The parent reviewed the integrated public behavior and retained parser boundaries.

This is an unreleased local continuation. Both installed executables remain the
verified v2.22.0 release asset; no second publication, installation or service
change is included. Reuse these checks for unchanged source/dependencies later.
