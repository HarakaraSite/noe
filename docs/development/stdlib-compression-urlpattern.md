# Compression interfaces and URLPattern inputs

Approved on 2026-11-08 after the local abort/text-stream completion. The
[API policy](stdlib-api-policy.md) governs this local implementation, validation
and commit. Publication, installation, services and noebot are outside this request.

### Compression interfaces

Follow the [Compression Standard](https://compression.spec.whatwg.org/) for the
existing gzip, deflate and deflate-raw formats. Validate required format arguments,
DOMString/enum conversion and unsupported formats synchronously with TypeError
before native state or async work is allocated. Brotli remains unsupported.
Expose branded readonly readable/writable getters using private state; these
interfaces compose streams rather than extending TransformStream. Stream inputs
must be genuine BufferSources with exact byte spans and standard conversion /
error behavior. Keep Node zlib's own string conventions separate.

Preserve the existing native codecs, streaming backpressure, cancellation, task /
Worker cleanup and output limit. Verify malformed/truncated input error propagation
for the supported formats and do not silently accept trailing compressed data or
multiple gzip members where the format contract requires failure. No new format,
dependency, capability, limit or stream engine is added.

### URLPattern inputs

Follow the [URL Pattern Standard](https://urlpattern.spec.whatwg.org/) for the
constructor/test/exec input and baseURL procedures. Accept URLPatternInit objects,
including baseURL and missing components, as well as strings. Apply dictionary /
USVString conversion, distinguish constructor failures from match failures, and
return converted inputs and actual component captures. Resolve relative paths
against base URLs and preserve specified component default/inheritance rules.
Keep public component attributes readonly and branded, with internal compiled
state. Align options/overloads needed by these input contracts and document any
remaining parser/canonicalization limits; this is not a full URLPattern audit.

Retain and reuse the existing pattern compiler and URL backend where possible.
Do not replace them with a new dependency or broaden to every pattern grammar
feature. Types, embedded references, examples, public Go consumers, Workers and
the session handoff must agree with the supported input slice.

### Implementation and migration

Both compression constructors validate formats before allocating native state.
The wrapper objects expose readonly endpoints and reject structured cloning.
Web writes snapshot fixed, non-shared ArrayBuffers or exact view spans, including
DataView. Detached buffers contribute zero bytes. Strings, arrays, shared memory
and variable-length buffers reject with TypeError. Encode text explicitly before
writing. Node zlib synchronous/callback helpers retain their string input behavior;
the create* helpers return these Web interfaces and follow their chunk contract.

Web decompression validates forbidden gzip flags and zlib preset dictionaries in
addition to native checksum/truncation errors. It rejects trailing bytes and extra
gzip members. A write containing invalid data must reject, rather than reporting
success before a later close/read failure. Decoder progress determines write
settlement; bounded output backpressure, cancellation and execution cleanup remain.
The existing 1 GiB cumulative output limit is unchanged.

URLPattern dictionary conversion visits declared members in Web IDL order and
uses USVString conversion. Object inputs carry their own baseURL; combining one
with the separate baseURL argument throws. Constructor parse failures throw,
matching parse failures return false/null, and conversion exceptions propagate.
exec returns converted input snapshots and component captures. The component
attributes and hasRegExpGroups are readonly, branded and private; the ignoreCase
option applies to pathname/search/hash. Pattern objects cannot be cloned.

Base inheritance follows component specificity and never inherits pattern
credentials. Constructor string defaults distinguish an empty relative pathname,
query-only/hash-only input and absolute origin transitions. An omitted password
remains a wildcard, and relative leading double slashes stay in the same-origin
pathname. Shared URL path normalization now retains empty path segments while
removing dot segments, which is required to match those paths consistently.

### Remaining boundaries

This is an input-contract correction, not a claim of complete URLPattern or URL
conformance. The retained compiler does not implement all token grammar, generated
pattern canonicalization or IDNA. For example, dot segments directly adjacent to
a dynamic segment can canonicalize differently (`../:id` against a base path).
Ordinary dictionary routes and tested relative/static component cases are covered;
additional grammar/backend work should follow concrete caller requirements.

Compression still supports only the three existing formats. Brotli, a new stream
engine, new capabilities and dependency updates are outside this continuation.

### Verification and review

Evidence is retained at `/var/tmp/noe-compression-pattern-4xda37jx`. Its `noe` is
a development executable; both installed paths still contain the verified
v2.21.0 asset. No push, release, installation or service change is included.

The final native-source checks pass across all seven affected stdlib/profile /
runtime/CLI packages. Focused race checks cover compression/decompression,
URLPattern and relevant Worker/Bindings cases. Positive/negative public type
fixtures, reference catalog, example lint, Go formatting/diff checks and vet pass.
The actual CLI passes primary URLPattern assertions, public three-format DataView
round trips, the example and embedded references. The installed v2.21.0 reproduces
the original object-input, writable-endpoint and late-format-validation defects.

The candidate matches Deno 2.9.7 on eight representative route/baseURL cases,
three-format round trips, invalid write/read/close outcomes and shared-buffer
rejection. Primary-only optional URLPattern arity/default-input assertions remain
separate from Deno's older required-argument behavior.

Independent bounded reviews covered URLPattern inputs and compression separately.
Three P2 URLPattern findings (constructor defaults, omitted password and relative
double slash) were fixed; one fix-only re-review found no actionable residual.
Three P2 compression findings (shared buffers, forbidden headers and premature
write settlement) were fixed; one fix-only re-review found no actionable residual.
That review also confirmed byte-at-a-time input, empty final writes and optional
gzip headers, while inspecting progress/EOF/error interaction and cleanup wakeups.

Final checkout and standalone module-artifact consumers build and run successfully,
including main and Worker route/compression helpers. Runtime/declaration source
continuity is checked against the artifact manifest and extracted module, with a
final changed-source snapshot retained alongside the test records. No dependency,
grant, Go Profile field or release workflow changed. Completed checks can be
reused for unchanged source and dependencies when publication is requested.
