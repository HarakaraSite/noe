# Standard library session handoff

Updated 2026-11-08 after local Fetch body-consumption completion following
Headers/Response `66c5703`. Both local continuations are unreleased; installed
executables remain v2.23.0. Linked policy, implementation and release records
remain canonical. Release requests include both local installations.

## Repository and publication state

- Repository: `/home/agent/projects/noe`, branch `main`.
- v2.23.0 includes `99f7f4b` (URL/query) and `68d9437` (Request URL/method).
- Immutable tag revision: `a2efaa4de9ffd0368c323a5da8a7802e75ccc9c8`;
  tag object: `16af6cc9e9428de469d73a9e18160dcd3924c7af`.
- [CI run 62](https://forge.harakara.site/littleisland/noe/actions/runs/62)
  succeeded (API 243, job 264). The
  [release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.23.0)
  is published, neither draft nor prerelease (ID 176).
- All six assets pass checksum/build metadata verification. The native asset
  passes thirteen runtime/reference/example/SDK checks. Direct published stdlib
  and agent consumers pass, including main/Workers and portable/Go-host modes.
- `/home/agent/.local/bin/noe` and `/home/agent/projects/noe/noe` contain the same
  verified Linux amd64 asset, SHA256
  `7b7ff1dffbe40b35339e3f999b7a247442f4e91384104d211b6e226ed7cc4cd9`.
  Versions, hashes, PATH resolution, arithmetic and authentication-helper help
  commands pass. Runtime evidence is reused for the identical installed asset.
- Previous v2.22.0 copies and installation records are retained at
  `/var/tmp/noe-v2.23.0-install-rdk00efo/installation.json`.
- Release evidence: `/var/tmp/noe-v2.23.0-release-rzascld6`. See the
  [publication/install checkpoint](../releases/release-checkpoints.md#v2230-publication-and-installation--2026-11-08),
  [v2.23.0 notes](../releases/v2.23.0.md), and historical
  [v2.22.0 notes](../releases/v2.22.0.md). The Go module remains `/v2`.
- No required release/install work remains. Other hosts, services, noebot,
  real login and provider requests were excluded.

## User decisions to retain

Read the [standard library API policy](stdlib-api-policy.md) first.

- Web standards and usability take priority over Node compatibility. Web
  APIs retain their standard names and semantics; NOE-specific host APIs
  use explicit `noe:` module names.
- Preserve shared native backends, independently selected capabilities,
  Root/Cwd/ReadOnly, and resource ownership. Do not restore duplicate native
  implementations merely to restore convenient method names.
- Explain breaking changes before each new slice. The latest user instruction
  authorizes autonomous local standard-library progress with breaking changes;
  do not repeat compatibility approval prompts within that scope. Choose
  coherent contracts and migrate callers/types/docs/tests together. This is
  not a reason to rename all remaining useful Node utilities without a use case.
- For FS, the user chose removal of all four old imports without adapters.
- For process/I/O, the user chose the contract below and removal of
  `node:child_process`, `child_process`, `node:process`, and `process` imports
  without adapters. Global `process` is retained. Its module form is now
  the named `process` export; the default export is the module namespace.
- **noebot is handled independently by the user.** Do not investigate, edit,
  or test it for this work. An earlier read-only FS audit and
  [migration memo](noebot-noe-fs-migration.md) are available if requested.
- Documentation and explanatory source text are English; user replies may
  be Japanese. Follow AGENTS.md, including reuse of completed validation
  when the tested source and dependencies have not changed.
- A release includes installation of the verified native asset at both local
  executable paths above, unless the user explicitly limits the request.
  Do not ask for separate installation approval. Preserve previous copies,
  replace atomically, and verify hashes/version/PATH; reuse runtime results
  for the identical asset. Do not restart running services under this rule.

## Fetch body consumption local completion

The user authorized a concrete stdlib improvement after `66c5703`, with local
commit only and no push/release/install/service or other-project changes. The
[contract and verification](stdlib-body-consumption.md) is canonical. Baseline
reproductions showed finite helper reads bypassing reader locks and cancellation,
bytes results modifying clone storage, and non-byte chunks silently becoming
zeros. Helpers now read one current stream, distinguish locking from disturbance,
validate/copy chunks and retain producer errors. Finite input/clone/transfer
preserves native Content-Length/replay; unusable server responses enter onError
before sending headers. References, example and main/Worker consumers are updated.

Both affected stdlib packages, focused final units/race, catalog/lint/vet, actual
CLI contract and seven example/reference/SDK checks pass. Exa's 43 stub checks
and the prepared MCP SDK Unicode echo fixture pass without real provider calls or
fixture changes. Checkout and standalone module-artifact main/Worker consumers
pass; runtime/declaration source continuity is recorded. Unchanged type evidence
from `66c5703` is reused; unrelated full suites were not repeated.

A bounded independent review found two P2 regressions at the new buffering await:
missed immediate abort and late Request-header capture. Both are fixed, covered
for direct/clone/transfer and confirmed by the single fix-only re-review. Evidence:
`/var/tmp/noe-body-consumption-mxrejb5s`.

Local implementation, verification, review and commit are complete. The original
`66c5703` commit is preserved. Both installed paths remain the v2.23.0 asset above.
No push, release, binary installation, service, simplejs or noebot change occurred.
Full BodyInit/BufferSource and generic tee/scheduling conformance remain outside
this bounded correction; reuse completed checks for unchanged code/dependencies.

## Headers and Response local completion

The user approved both next improvements after v2.23.0 installation. The
[contract and migration](stdlib-headers-response.md) is canonical. Header inputs,
private lists, live iteration and cookie separation are corrected. Response
construction/static validation, readonly metadata, immutable error/redirect/fetch
headers and native clone metadata are implemented. Shared HTTP byte conversion,
HEAD/null-body cleanup and private server/101-upgrade factories retain existing
transport authority and ownership. Types, references, examples and main/Worker
Go consumers are updated.

Seven-package tests, focused race, types/catalog/lint/vet, nine CLI/reference /
example/SDK/reference-comparison checks and checkout/module-artifact consumers
pass. The prepared MCP SDK loopback fixture remains unchanged. A bounded review
found one P2 in no-Loop HTTP 205 handling; it is fixed, covered by a real payload /
limit regression and confirmed in the single fix-only re-review. Factory order /
lifetime, all byte values and null-body zero-read/one-close behavior were also
independently checked. Evidence: `/var/tmp/noe-headers-response-j6pkokoa`.

Local implementation, verification, review and commit are complete. No push,
release or installation is included. Both executable paths remain the verified
v2.23.0 asset above; reuse completed implementation checks on a later release.
Browser header/CORS/cache guards, full Body/BufferSource and broader Request /
signal conformance remain outside this bounded slice. Noebot/services are excluded.

## Current release: v2.23.0 URL and Request corrections

The user requested publication and both local installations after the two local
slices. Publication/installation are complete. Reuse implementation and release
evidence for unchanged source and dependencies. The verified native asset is
`/var/tmp/noe-v2.23.0-release-rzascld6/assets/noe-linux-amd64`.

The subsequently approved Headers/Response continuation is complete locally above.
Full IDNA/IP / Windows file-URL, URLPattern grammar and broader Request/Body/signal /
browser-policy gaps remain outside the completed slices. Future stdlib work should
follow concrete caller needs and the API policy.

## Request URL and method completion (v2.23.0)

URL/query correction `99f7f4b` and Request correction `68d9437` are published and
installed in v2.23.0. Request URL/method behavior is described in
[its contract](stdlib-request-inputs.md). Shared URL parsing and method validation,
readonly private metadata, outbound transport snapshots and private incoming HTTP
initialization are implemented. Both installed executables contain v2.23.0.

Seven-package units, focused race, types/catalog/lint/vet and actual CLI contract /
example/reference checks pass. The existing MCP SDK loopback example passes with
its prepared bundle unchanged and no external service. Checkout and standalone
module-artifact main/Worker consumers pass; 225 runtime/declaration paths match.
A bounded independent review found no required finding and independently verified
both installer orders, incoming raw TRACK metadata/body clone/transfer, bridge
privacy and clean shutdown. Implementation, validation, review and local commit
are complete. Full Request/Headers/Body/signal and browser-policy gaps remain outside
this slice; see the canonical scope. Implementation evidence is reused for v2.23.0.
Evidence: `/var/tmp/noe-request-inputs-u_xbfrv4`.

## URL normalization and query encoding completion (v2.23.0)

The user authorized this continuation after v2.22.0 installation. The
[contract and boundaries](stdlib-url-encoding.md) is canonical. Component UTF-8
encoding, encoded dot segments, supported relative/setter transitions, private
URL state, stable linked query identity and form conversion/live iteration are
implemented. Types, references, example and main/Worker consumers are updated.

Final seven-package tests, focused race, types/catalog/lint/vet, actual CLI and
external checkout/module-artifact consumers pass, including main/Worker and Fetch
form helpers. Runtime comparisons cover six constructor and twelve setter cases
plus live query behavior; primary-only differences are retained in the contract.
Review found two P2 issues (rootless pathname dot removal, primitive pair inputs)
and one P3 (conversion precedence). All were fixed, along with reentrant setter
record lookup and primary sequence error behavior. One fix-only re-review confirms
no remaining findings in the fixes. Source continuity is recorded with the final
changed-source snapshot. Evidence: `/var/tmp/noe-url-encoding-87ffe6i1`.

This originally local continuation is now published and installed in v2.23.0.
Implementation checks/reviews remain reusable. IDNA/IP/full file-URL and URLPattern
grammar replacement are outside this normalization/encoding slice.

## Compression-interface and URLPattern completion (v2.22.0)

The user approved both next improvements. The
[contract and verification](stdlib-compression-urlpattern.md) is canonical.
Public declarations, reference pages, the route/compression example and main/Worker
consumer helpers are updated. The URLPattern input/baseURL implementation and
its single fix-only independent re-review are complete. Three P2 findings were
fixed: constructor string defaults, omitted credential password and relative
leading double slashes. Shared path normalization retains empty segments.

Compression review found three P2 issues in the initial implementation: shared
BufferSource acceptance, forbidden gzip/zlib header acceptance, and writes settling
before decoder validation. All three were fixed. Native writes now wait for codec
progress; validated header prefixes allow byte-at-a-time input without waiting
forever for the next write. Bounded output backpressure, cancellation and the
existing cumulative output limit are retained.

All seven affected packages, focused race, public types/catalog/lint/vet and actual
CLI primary/common/example/reference checks pass on the corrected native source.
The candidate matches Deno 2.9.7 on ordinary route/base cases, all three formats,
invalid writes and shared-buffer rejection. The single compression fix-only
re-review confirms all findings resolved, including progress/EOF/error interaction,
byte-at-a-time input, empty final writes, optional gzip headers and cleanup wakeups.
Checkout and standalone module-artifact consumers build/run successfully, including
main/Worker helpers; exact runtime/declaration source continuity is recorded.
Evidence: `/var/tmp/noe-compression-pattern-4xda37jx`.

Local implementation, verification and bounded reviews are complete. The retained
compiler/URL backend does not claim full pattern grammar or IDNA conformance; see
the canonical contract's boundaries. Reuse completed checks for unchanged source
and dependencies when a release is requested. The preceding local abort/text-stream
slice is committed at `edb5e73`; both continuations are after v2.21.0.

This originally local slice is now published and installed in v2.22.0. Its
implementation evidence above is reused. No services or noebot were changed.

## Abort-interface and text-stream completion (v2.22.0)

The user approved both next improvements. The
[contract and verification](stdlib-abort-text-streams.md) is canonical.
Implementation, validation and the bounded review follow-up are complete locally.
This originally local slice is now published and installed in v2.22.0. The
implementation evidence below is retained and reused for release verification.

Controllers expose branded readonly private signals; AbortSignal's public
constructor is illegal. Default abort/timeout reasons are AbortError/TimeoutError
DOMExceptions. timeout validates Web IDL conversion before allocation and accepts
0..2^53-1 after truncation, with staged internal deadlines over the existing Loop
queue to avoid ordinary timer/duration overflow. Public clearTimeout cannot
cancel them. Existing timer liveness and execution-scope cleanup are retained.
Legacy WebAPIs without a Loop supports synchronous abort and explicitly rejects
timeout TypeError. onabort preserves object-valued slots and registration order.
The shared handler correction also serves EventSource and WebSocket.

Text streams compose the existing TransformStream and UTF-8 codecs. Metadata /
endpoints are branded readonly getters, the encoder retains split UTF-16 pairs,
and the decoder requires BufferSource chunks and exposes fatal/ignoreBOM.
No new dependency, capability or Go Profile field is added. Public types,
reference pages, examples and main/Worker consumer helpers agree.

All seven affected packages, focused race, types/docs/lint/vet, actual CLI
primary/common/example/reference checks and checkout/standalone-module consumers
pass. The previous v2.21.0 binary reproduces the defects; ordinary cases match
Deno 2.9.7, with primary-only receiver/arity assertions preserved separately.
Evidence: `/var/tmp/noe-abort-text-streams-o4yke3sm`; its `noe` is a development
binary, not an installation. Source/consumer continuity is recorded there.

An independent bounded abort/timer review found two P2 issues: JavaScript's
safe-integer bound for 64-bit EnforceRange, and inert object-valued onabort slots.
Both were fixed and a single fix-only re-review found no material residual in
the fixes. The parent reviewed text-stream internals; that independent review
excluded them. Reuse the completed checks for unchanged source/dependencies
when a release is requested.

## Local UTF-8 decoder and EventTarget continuation

The user approved both next improvements. The
[decoder/listener contract](stdlib-decoder-events.md) is canonical for this slice.
The preceding slice is committed at `a5a52ed`; this correction is `2e7228f`.
Both are now published and installed in v2.21.0. The implementation-stage
evidence below is retained and reused for release verification.

Completed work covers UTF-8 replacement boundaries, streaming/BOM/fatal state,
BufferSource/options conversion, private decoder metadata, and shared event
listener identity/capture/once/passive/signal behavior. Worker globals use the
same listener records. No extra encoding family, DOM tree or host authority is
added. Evidence is retained at `/var/tmp/noe-decoder-events-uagwfg5_`.

All seven affected stdlib/profile/runtime/CLI packages, focused race, public
positive/negative types, reference catalog, lint/vet and the actual CLI example
pass. Checkout and standalone module-artifact consumers build and run, including
the main/Worker helper. The development binary is retained in that evidence
directory; use the current release asset above for published behavior. The previous local binary reproduces the
original UTF-8/BOM, duplicate/removal/signal/passive defects. The candidate matches
the primary expected comparison output. Deno differs on capture phase order and
synthetic abort removal; those are explicit reference differences, not weakened
NOE assertions.

The parent reviewed the decoder implementation and its focused tests. A bounded
independent EventTarget review found one private-abort early-check defect in
AbortController/timeout; it was fixed and independently rechecked during that
review. No actionable listener finding remains. The review did not include
TextDecoder internals or broader AbortSignal contracts. Source continuity and
publication/install boundaries are recorded in the evidence summary. Reuse
these results for unchanged source/dependencies when a release is requested.

## Local encoding, User Timing and CustomEvent

The user approved all three comparison follow-ups: correct TextEncoder.encodeInto,
implement useful Performance entries/query/clear methods, and add CustomEvent.
See the [contract](stdlib-web-basics.md). This slice is now included in the
published and installed v2.21.0 release; the evidence below records its earlier
implementation stage.

Implementation and verification are complete locally. The new encoding,
CustomEvent and Performance regressions fail against v2.20.0. Affected unit
packages, focused race, types/docs/lint/vet, actual CLI example/reference,
legacy installation without a Loop and checkout/module-artifact Go consumers
pass. Main/Workers have independent timelines and require no extra capability.
See `/var/tmp/noe-web-basics-ivf40e59` for source, command and runtime evidence.

A bounded independent review found three Performance binding defects; all
were fixed with regressions. One fix-only re-review found a residual default
option conversion through an ordinary object. The parent corrected defaults
for mark/measure/standalone marks, verified baseline failures and final focused
race, and aligned optional method arities. No further independent re-review of
those final defaults/arity adjustments is claimed. The consumer artifact
predates only that small Performance adjustment; 314 runtime paths were checked, with only that adjustment differing from the
consumer artifact. Exact continuity and targeted verification are recorded.

The development candidate is `/var/tmp/noe-web-basics-ivf40e59/noe`. Its common
fixture matches Deno 2.9.7; primary-specification deviations are separately
recorded, including Deno's undefined default CustomEvent detail versus the
specified null. Do not weaken standard behavior to reproduce those deviations.

PerformanceObserver, resource/navigation timing, new Timeline id/navigationId,
browser DOM trees, full EventTarget capture/passive/signal options, and more
TextDecoder encodings remain outside scope. Existing Node-like opaque Event
cloning is retained; new CustomEvent/Performance objects are non-serializable.
No release, push, installation, service or noebot change is part of this local
slice. Reuse the recorded checks for unchanged paths when release is requested.

## Previous release: v2.20.0 Web Crypto

The user explicitly requested release and placement on 2026-11-08. The verified
Web Crypto implementation `6c8a23d` plus error-order fix `2e8a197` is now
published and installed at both local executable paths above. No required
release or placement work remains. See [release notes](../releases/v2.20.0.md).

Release evidence: `/var/tmp/noe-v2.20.0-release-el4vn8lo`. Clean candidate
`a6b3bc7` passes the release build/metadata, version unit, embedded-doc catalog,
seven native checks and unmodified jose main/Worker/Node interoperability.
All 312 runtime/dependency/reference paths match the verified implementation
except version/availability wording; release preparation adds no runtime logic
or dependency change. See the
[preparation checkpoint](../releases/release-checkpoints.md#v2200-preparation--2026-11-08).

The immutable tag revision `6a2891d` is a documentation-only successor. Its
release is ID 173, neither draft nor prerelease. CI run 59 built all six targets
and checksums without repeating implementation suites. Downloaded assets pass
all hashes/metadata with vcs.modified=false. Native contract/review/example /
reference checks and the jose SDK fixture pass; main and Worker each run 27
checks, and deterministic tokens match Node in both directions. External
stdlib/agent modules fetch v2.20.0 directly, pass verification and consumer
execution, and match all 17 changed Go/JS/TS paths in the immutable tag.

Both installed paths match the published hash and version, with PATH resolution
and authentication-helper help verified. Previous v2.19.0 executables are
retained under `/var/tmp/noe-v2.20.0-install-9cx42on9`. Publication and placement
are complete; do not move the tag or repeat unchanged source suites.

## Local Web Crypto continuation

The user approved: existing Web Crypto contract correction, standard API gap
inventory, then real SDK validation. This work is implemented and verified
locally after v2.19.0, with no new algorithm family or Profile capability.
See the [contract, gaps and results](stdlib-webcrypto.md).

- Supported SHA/HMAC/PBKDF2/HKDF operations now use private snapshotted key
  state, independent operation usages, non-extractability, standard BufferSource
  handling and errors, cached readonly attributes, and native asynchronous
  computation with execution/Worker cancellation and joined cleanup.
- Original verify-only signing and extractability regressions fail on the
  earlier source. Prototype-hook metadata regression also fails on the earlier
  facade and passes with null-prototype private records and own-property copies.
- Affected unit packages, focused race, types/docs/lint/vet, public Go consumers
  from checkout and source artifact, and actual CLI examples pass. The jose
  6.2.15 SDK is unmodified: HS256/384/512 JWT/JWS, claim/signature rejection,
  main/Worker execution, and two-way Node interoperability pass. SDK happy paths
  also passed old v2.19.0, so SDK success alone is not conformance evidence.
- The initial reviewer stopped during automatic screening. A subsequent
  user-requested pi review of `6c8a23d` completed with `opencode-go/glm-5.3`
  and thinking `max`. The parent rejected two recalled-specification claims
  using primary text and runtime probes, and accepted one low-severity
  deriveKey error-precedence issue. Reports and dispositions are at
  `/var/tmp/noe-webcrypto-pi-review-dodw0o1o`.
- The follow-up checks base-key algorithm/deriveKey usage before the target
  get-key-length operation. The baseline-failing regression covers missing
  usage and algorithm mismatch, while a permitted key retains TypeError for
  zero HMAC target length. Focused Web Crypto/legacy subtle tests, lint, native
  build and CLI review cases pass; no independent fix re-review is claimed.
- Remaining gaps are explicit: key generation; JWK/SPKI/PKCS8 formats;
  encryption/wrapping/public-key algorithms; complete Crypto/SubtleCrypto IDL
  shape; CryptoKey structured cloning. Actual SDK probes confirm generation,
  CryptoKey JWK export, and AES-GCM JWE gaps. Do not treat this as complete jose
  or Web Crypto support.

Original evidence: `/var/tmp/noe-webcrypto-46m8xyzx`. Error-order follow-up
evidence and the current development candidate are at
`/var/tmp/noe-webcrypto-derivekey-qgsnbji4` (`noe` executable); installed
executables now contain verified v2.20.0 as recorded above. The implementation
task originally excluded publication and placement; the subsequent release
request authorized and completed both. Reuse results for unchanged code/dependencies.
Other hosts, noebot changes, service restarts, real login and application-provider
requests remain excluded.

## Published and installed v2.19.0 Web Storage

The user explicitly authorized releasing Storage implementation `dfa497e` and
updating both existing vm201 executable paths, including commits, push, tag,
Forgejo assets, verified backups, placement checks, and work records. Other
hosts, service restarts, and noebot remain outside scope.

Implementation verification is complete at
`/var/tmp/noe-web-storage-bvqxqjs1`. Affected units, focused race, CLI
process/Worker/example E2E, types/docs/vet/lint, and checkout/module-artifact
Go consumers pass. The review's prototype descriptor getter deadlock was
fixed, with a baseline-failing regression and a clear fix-only re-review.
See the [Web Storage contract](stdlib-web-storage.md).

Release evidence is at `/var/tmp/noe-v2.19.0-release-pmyv5yje`. All 309
implementation runtime/dependency/reference paths match except the exact CLI
version and availability text in three embedded reference pages. No runtime
logic or dependency changes are included in release preparation. Reuse the
implementation evidence; CI only builds six assets/checksums and publishes.
Publication verification and both installations are complete; the installed
binaries now contain the verified v2.19.0 asset.

Clean release candidate `885af1f` passes its CGO-disabled release build,
version unit, embedded-doc catalog, and seven native Storage/version/example
checks. Metadata is clean and retains the pinned engine/Intl/no-JIT settings.
See the [preparation checkpoint](../releases/release-checkpoints.md#v2190-preparation--2026-11-07).
The immutable documentation-only successor is `4e6721d`. All six downloaded
assets, direct published consumers, and seven native checks pass. Both vm201
paths are updated with verified backups. No required release or install work
remains; future stdlib work should follow concrete caller needs.

## Published and installed v2.18.0

- The user explicitly requested publication and local installation. Clean
  candidate revision: `666116dee34fa0b6d9fc4242f72132b4ac1ca5d3`.
  Runtime logic/dependencies match the verified UDP checkpoint; only the CLI
  version and two embedded-reference availability sentences changed.
- Candidate build/metadata, version unit, affected docs, and five focused native
  checks (version/arithmetic/TLS/UDP/docs) pass. The completed race/E2E, public
  module artifacts, and six-target implementation builds are reused.
- Evidence: `/var/tmp/noe-v2.18.0-release-ur4u1vbf`. See the
  [preparation checkpoint](../releases/release-checkpoints.md#v2180-preparation--2026-11-07).
  The immutable tag points to the documentation-only successor `51f08bc`.
  Release ID 171 is published, neither draft nor prerelease; CI run 57
  (API 238, job 259) succeeded.
- All six downloaded checksums, target/dependency metadata, and clean VCS
  markers pass. Unlike previous releases, all six report vcs.modified=false.
  Downloaded Linux amd64 passes the five native release checks. Public stdlib
  and portable/Go-host agent consumers fetch v2.18.0 directly, run, and pass
  go mod verify; all 27 changed source/declaration paths match the tag.
- Both local binaries were atomically replaced and verified against the
  published checksum/version. PATH, arithmetic, and both auth-helper help
  checks pass. Previous v2.17.0 copies and installed evidence are at
  `/var/tmp/noe-v2.18.0-install-ltin8afa`. No services or noebot were changed.

## Completed UDP datagram sockets (v2.18.0)

See the [contract and evidence](stdlib-udp.md),
[Go embedding guide](../go-embedding.md#udp-datagram-sockets), and
[round-trip example](../../examples/low-level/README.md#udp-datagram-round-trip).

- New `noe:sockets.bind({network: "udp", host, port, signal?})` returns a
  DatagramSocket with address, send, receive, shared async close, and async
  disposal. Stream connect/listen and their return types remain unchanged.
  Packets preserve binary/text/empty payloads and remote source endpoints.
- Send snapshots Uint8Array bytes before asynchronous work; size/routing limits
  follow the OS. Receive uses a 65,536-byte buffer and rejects receive errors
  or a full buffer instead of exposing a possible partial packet.
- Each signal owns only its bind/send/receive operation. Send/receive use
  separate direction gates, joining the cancellation callback and clearing its
  deadline before the next operation. DNS lookup is cancellable. Socket close
  cancels operations and joins native I/O; Host/Worker cleanup also reclaims
  bind results queued before owner-thread handoff.
- Reuses RawSockets selection and native Host workers/resource ownership;
  no additional Go field, capability grant, or dependency. No implicit FS,
  Fetch, HTTP, or WebSocket access. UDP reliability/retry/multicast APIs are
  not introduced by this slice.
- Verified packet boundaries/snapshots/source addresses, empty and maximum
  IPv4 packets, hostname destination selection, IPv6 loopback, operation
  cancellation isolation, close/disposal/port reuse, Worker/runtime cleanup,
  native late bind/blocked write/full-buffer cases, and affected race checks.
  Types, lint, docs, vet, CLI example, and checkout/standalone-module consumers
  pass. Consumers compile with CGO disabled for all six release targets.
  A bounded independent review found no actionable production issue.
- Evidence, source fingerprints, and development executable:
  `/var/tmp/noe-udp-mkdxtl0z`. v2.18.0 publication and installation are complete;
  use the completed verification for unchanged source.

## Completed raw TLS sockets (v2.18.0)

See the [contract and evidence](stdlib-raw-tls.md),
[Go embedding guide](../go-embedding.md#raw-tls-sockets), and
[round-trip example](../../examples/low-level/README.md#raw-tls-round-trip).

- `noe:sockets.connect/listen` accepts `network: "tls"`, reusing the existing
  Connection/Listener, native ownership, Web Streams, and disposal backend.
  Connect and accept publish connections after TLS authentication. Listener
  close cancels pending handshakes and undelivered results, while delivered
  connections keep independent ownership.
- Clients use system roots/hostname verification by default, with optional
  explicit `ca`/`serverName` and client identity PEM. Listeners take `cert`/`key`
  PEM or a Go host server configuration. TLS fields reject before dial/bind
  when invalid or conflicting with authoritative host configuration, without
  returning supplied PEM in diagnostics. No implicit FS/HTTP/WebSocket grant.
- `Modules.RawSockets` stays bool. New `Modules.RawSocketsTLS` configures
  `stdlib.SocketTLSOptions{Client, Server, HandshakeTimeout}` without selecting
  sockets. Configurations are cloned for profiles/runtimes/Workers/listeners;
  Go referenced objects remain immutable in use. Host ALPN is preserved.
  External unkeyed Modules literals must move to named fields.
- Writer close sends TLS close_notify; read cancellation stops local delivery,
  writer abort and full close interrupt the appropriate transport operations.
  A review found that TLS Read can wait to write a protocol alert behind an
  application writer, so read cancellation must not join that native read.
  A deterministic regression failed before the correction; final race checks
  and the bounded re-review pass. Full close still joins owned native I/O.
- Public tests cover trust/hostname failures, mTLS and host snapshots, option
  validation/capability isolation, listener and Worker cleanup, AbortSignal
  reason/ownership transfer, and handshake timeout. Native tests cover queued
  authenticated results, callback context cancellation, and blocked I/O.
  Affected race, CLI example, types, docs, lint, and vet pass. Public consumers
  run against checkout and isolated module artifact with CGO disabled, and
  compile for all six release targets. No dependency changed.
- Evidence, review repro, source manifest, and development executable:
  `/var/tmp/noe-raw-tls-940je7qo`. Reuse this implementation evidence when
  preparing a future release if source/dependencies remain unchanged.

## Published v2.17.0

- Clean candidate revision: `e7d8f5e418eba434d5d131dc31a2b9931669b7b3`.
  This changes version and documentation only after the verified HTTPS/WSS
  implementation. The default CLI version is v2.17.0. Public download/module
  instructions now target the authorized v2.17.0 release.
- The [release notes](../releases/v2.17.0.md) describe cert/key, host TLS,
  capability/cleanup behavior, and the keyed Go ServeOptions migration.
  Both READMEs and the release index identify v2.17.0.
- Implementation source continuity, configured-version unit test, a clean
  CGO-disabled release-flags build, metadata, and four native smoke checks
  pass. Full source suites and six-target consumer builds were reused rather
  than repeated. Evidence: `/var/tmp/noe-v2.17.0-release-yk3xb7im`.
- See the [preparation checkpoint](../releases/release-checkpoints.md#v2170-preparation--2026-11-07).
  The immutable annotated tag points to `438f0bf`; only documentation differs
  from the clean candidate. Release ID 170 is published, not draft/prerelease.
- All six downloaded checksums/build metadata pass. The Linux amd64 asset
  passes version/arithmetic, HTTPS validation/redaction, bundled docs, trusted
  HTTPS health/JSON, HTTP/1.1 ALPN, WSS text/binary/close, interruption cleanup,
  and port reuse. Public stdlib and portable/Go-host agent consumers fetch
  v2.17.0 directly without replacements, run, and pass go mod verify. All 17
  changed source/declaration paths match the tag in both downloaded modules.
- Publication verification is complete. The publication checkpoint and
  `/var/tmp/noe-v2.17.0-release-yk3xb7im` retain metadata, assets, and logs.
  Both installed copies now match the verified v2.17.0 asset. Installation
  checksum/version/PATH, arithmetic, and both authentication-helper help checks
  pass; previous v2.16.0 copies remain backed up. No service restart or noebot
  change was performed.

## Completed HTTPS/WSS listener addition (v2.17.0)

See the [contract and evidence](stdlib-https.md),
[Go embedding guide](../go-embedding.md#https-listeners),
and [HTTPS/WSS example](../../examples/http/README.md#https-and-wss-server).

- `serve({cert, key, ...}, handler)` accepts nonempty PEM strings as a pair.
  Invalid/mismatched input fails synchronously before Allow or bind, without
  including PEM material in diagnostics. Reading files is explicit `noe:fs`
  work; inline credentials grant no additional capabilities.
- `stdlib.ServeOptions.TLS *tls.Config` selects host-configured HTTPS, with
  Certificates/GetCertificate/GetConfigForClient. JavaScript credentials
  conflict with host TLS. Profile/runtime/Worker/listener copies snapshot
  configuration; referenced certificate/key objects and pools remain shared
  and immutable in use. External unkeyed ServeOptions literals must use fields.
- TLS listeners advertise HTTP/1.1, including callback-selected configurations.
  HTTPS request/server URLs and WSS upgrades use the existing HTTP, body,
  close/disposal, socket, and forced-cleanup backends. No new dependency or
  implicit FS/Fetch/WebSocket grant was added.
- HTTPS/HTTP, WSS binary/text/clean close, validation-before-bind, host TLS
  snapshots/callback ALPN, Worker inheritance/termination, stalled-handshake
  cancellation, and real CLI example tests pass. Race validation passes for
  the new cases. Public checkout and standalone-module consumers run, with
  CGO-disabled builds for all six release targets. Types, embedded reference,
  example lint, and affected-package vet also pass.
- One existing queued-WebSocket race test initially failed because Eval may
  drain the opening notification before the test pauses owner delivery. Its
  handshake response is now gated until Eval returns; the focused race test
  passes. This changes only test synchronization, not socket behavior.
- Evidence and the fresh CLI candidate are under
  `/var/tmp/noe-https-vm9c6p5e`. Reuse the successful results for unchanged
  runtime source/dependencies; the initial aggregate race failure and focused
  correction are retained there. Noebot and live credentials were excluded.

## Completed WebSocket connected close and interruption correction

See [contract and migration](stdlib-websocket-closing.md).

- Native transport Close interrupts a blocked writer without taking its
  serialization mutex. Concurrent closes still wait for the single native
  close. Idle writers observe context cancellation as well as queue signals.
- Established close remains synchronous at the API boundary and enters
  CLOSING immediately. It flushes earlier accepted sends, sends one close
  frame, and allows five seconds for the frame exchange. Peer-initiated
  closing discards unsent data and prioritizes the response after an
  in-flight frame. Runtime/execution/Worker force cleanup stays independent.
- `close()` now sends an empty frame; `close(1000)` sends an explicit normal
  status. Close events report the first peer code/reason, with 1005 for an
  empty frame and 1006/empty reason when no peer close arrived. `wasClean`
  requires successful close frames in both directions before native cleanup.
  Diagnostics remain on the error event rather than the close reason.
- Invalid close payloads and fragmented/oversized control frames fail instead
  of being labelled clean. Local closing has a serialized native reader that
  progresses while the JS owner is busy. Native completion stops the deadline
  before owner publication; an in-flight read/drain retains the first peer
  result even if a failure callback arrives earlier.
- The same global/factory/upgrade class, grants, native transport, and
  capability selections remain. No public Go field, dependency, or new JS
  method was added. Current docs/types and CLI/public-consumer fixtures
  reflect the changed default wire payload and completion behavior.

## Connected WebSocket validation and evidence

Validation is complete. Evidence: `/var/tmp/noe-websocket-close-apjtaav1`.

- A deterministic unread-pipe write barrier reproduced the baseline native
  deadlock; the fixed native tests and race checks pass. Seven public
  regressions also reproduced old behavior at detached `248e74a`. A separate
  baseline full-suite pass is incidental evidence, not a requirement to
  repeat every unchanged test for this correction.
- Eight public regressions cover delayed ACK and FIFO sends, empty no-code
  close, actual peer ACK, abrupt EOF, missing-reply timeout, simultaneous
  distinct peer results, and blocked-send cancellation in main and Worker.
  Peer fixtures hold unread payloads behind explicit cleanup barriers.
- Native owner-paused tests cover close before another receive, ordinary
  data queued before the ACK, and failure during an in-flight peer read.
  Existing opening, header/TLS, binary, and capability tests also pass.
- Final affected native/core/public tests pass with the race detector.
  Real CLI run/eval/Worker/fixture-agent modes and independent checkout/
  module-artifact Go consumers pass, including authenticated Blob exchange
  and a clean empty peer close. No platform/dependency change requires
  repeating the prior six-target builds.
- TypeScript fixtures, embedded catalog/reference checks, vet, and whitespace
  checks pass. Evidence includes command logs/JSON, verification and source
  manifests, baseline source, and independent-consumer artifacts.
- Independent review found an intermediate busy-owner timeout and a weak
  cancellation fixture; both were corrected. The same 5.5-second busy-handler
  reproduction now reports the peer's code/reason and clean true. The drain
  handoff, failure ordering, and force cleanup were re-reviewed with no
  remaining actionable finding. Initial/final reproduction notes are saved.
- Candidate: `/var/tmp/noe-websocket-close-apjtaav1/noe`, not installed.
  No noebot work, push, release, executable replacement, or service restart
  was performed.

## Completed WebSocket opening lifecycle correction

See [implementation and migration](stdlib-websocket-lifecycle.md).

- Closing during CONNECTING now cancels outbound dialing/handshake and
  rejects opening on the owner. Error precedes unclean close (1006), and
  both handlers observe CLOSED. Later results cannot reopen or deliver
  messages; messages are suppressed outside OPEN.
- The opening timeout covers TCP/TLS/HTTP together. Context cancellation
  interrupts a connected peer's blocked HTTP handshake, so execution and
  Worker cleanup do not wait for the configured handshake timeout.
- Completed handshakes remain owned while queued for owner delivery,
  including legacy loops without an execution scope. Scoped cleanup and
  active-socket hooks retain the shared native backend and Go-only force
  cleanup. Closing a reserved upgrade removes its token before attachment.
- Close arguments now use unsigned-short clamp conversion, permitted code
  checks, and the 123-byte UTF-8 reason limit. Invalid calls throw before
  changing state, including calls on CLOSING/CLOSED sockets. Callers must
  stop sending reserved/out-of-range codes or oversized reasons.
- This opening checkpoint retained the former established-close policy.
  The newer connected correction above replaces its default code 1000 and
  short drop allowance. No capability, public Go field, or dependency was
  added by either correction.

## WebSocket lifecycle validation and evidence

Validation is complete. Evidence: `/var/tmp/noe-websocket-lifecycle-P6J6DU`.

- A detached `3997dc0` checkout reproduced both CONNECTING close and
  execution cancellation waiting on a held HTTP handshake. Each failed
  within a bounded test and released its peer during cleanup. A separate
  deterministic test reproduced a successful handshake queued before
  close leaking the loop hold. These failures are fixed in the current tree.
- Public regression tests pass for held-response close/cancellation,
  Worker termination while opening, close validation in OPEN/CLOSED, and
  reserved-upgrade close before attachment. Core tests cover a completed
  handshake queued before explicit close or borrowed execution cleanup.
- Existing WebSocket/TLS/custom-header/binary and noe:http tests pass;
  affected core/public tests also pass with the race detector. Real CLI
  run/eval/Worker/fixture-agent modes cover immediate close and validation.
- Separate checkout and independent module-artifact public Go consumers
  build and run with CGO disabled. They include immediate CONNECTING close
  and argument validation. Prior six-target builds remain reusable: there
  are no dependency or platform-specific changes in this correction.
- TypeScript fixtures, embedded API catalog, vet, and whitespace checks
  pass. Bounded independent static review of cancellation, delivery,
  ownership, reservations, and validation found no actionable finding.
- Command logs/JSON, `verification.json`, and `source-manifest.json` record
  the final evidence. `baseline/` and `baseline-checkout/` preserve the
  comparison source. No push, release, installed-binary replacement,
  running service restart, or noebot work occurred.

## Completed Serve limits and lifecycle slice

See [implementation and migration](stdlib-serve-lifecycle.md) and the current
[embedded reference](../../internal/apidocs/content/noe-http.md).

- Request body overflow now rejects with `RangeError` and
  `code: "ERR_HTTP_BODY_TOO_LARGE"`. Exact-bound bodies succeed after actual
  EOF is verified with at most one extra probe byte. Zero retains the 32 MiB
  default and negative now means unlimited instead of an empty body.
- Uncaught overflow produces 413 before response headers. `onError` can
  supply another response. Once response streaming starts, overflow fails
  that transfer rather than turning a prefix into successful completion.
- HWM0 demand, bounded native reads, data/terminal-error ordering, GET/HEAD,
  Expect: 100-continue, early responses without implicit drains, and input
  cancellation/cleanup remain on the existing shared backend.
- `server.close()` now returns one shared `Promise<void>`, resolving to
  undefined after cleanup. It closes handler admission immediately and
  waits off the JS owner, allowing active handlers and timers to finish.
  The existing five-second graceful allowance and forced fallback remain.
- `server[Symbol.asyncDispose]()` delegates to close. `await using` cleans
  up on normal and exceptional scope exit. Runtime/execution/Worker force
  cleanup remains independent of a pending graceful close.
- Initiate close and return a Response from a shutdown handler; await close
  outside its own handler/response producer. Upgraded WebSockets keep their
  separate ownership. No closeSync alias or public Go field is added.
- NOE callers, public types, current guides/bundled reference, auth helper,
  HTTP examples, agent-server shutdown, and external consumers are migrated.
  Older phase/release records retain their original contracts. Noebot was
  not investigated, edited, or tested.

## Serve validation and evidence

Validation is complete. Evidence: `/var/tmp/noe-serve-policy-bOYsbk`.

- Full `internal/stdlib` and public `stdlib` suites, affected module-map,
  composition, execution, runtime, and CLI packages. Focused public tests
  cover shared Promise/undefined completion, active handler/timer progress,
  handler-initiated close, port reuse, normal/exceptional disposal, the
  graceful deadline, and cancellation during close.
- Receive tests cover actual EOF/overflow, negative/unlimited reads,
  collector/stream RangeError and code, default 413/custom onError, transfer
  abort after headers, data/terminal errors, and cancelled Expect probes.
- Relevant real-CLI Serve/HTTP/WebSocket/EventSource/docs E2E, Worker/fixture
  agent paths, targeted race checks, agent-server normal/graceful/SIGINT
  cleanup, auth-helper help, TypeScript/API catalog/lint/vet all passed.
- `consumer/` contains checkout and independent module-artifact consumers
  with CGO disabled and six release-target builds. The consumer exercises
  async disposal, receive overflow, and a handler completing its response
  after initiating close. After this runtime snapshot, standalone reference
  member headings were added; only docs/catalog tests changed and were
  checked separately with a fresh candidate. Do not repeat native consumer
  cross-builds solely for those documentation additions.
- The first focused lifecycle check exposed incorrect void-promise encoding
  (nil becomes null and zero Value becomes numeric zero in this engine).
  Capturing actual global undefined on the owner fixed it; the focused
  recheck and full public suite passed. No unresolved failure remains.
- Bounded independent static review found no actionable finding in graceful/
  force overlap, owner-thread hooks and Promise settlement, handler/body I/O
  joining, cap probing, cancellation, or streamed-response failure.
- `verification.json` records commands and resolutions;
  `source-manifest.json` records the final working-tree hashes. `baseline/`
  preserves the prior HTTP artifact source for this slice's comparison.

The Serve candidate is `/var/tmp/noe-serve-policy-bOYsbk/noe`, including the
final bundled close/disposal details; the newer WebSocket candidate above
supersedes it. It is not installed. The process/HTTP
and earlier native filesystem evidence below remain reusable for unchanged
implementation. No push, release, installed-binary replacement, or running
service restart was performed.

## Completed HTTP/WebSocket slice and response corrections

See [HTTP implementation and migration](stdlib-noe-http.md),
[Fetch limit correction](stdlib-fetch-body-limit.md), and the
[embedded host reference](../../internal/apidocs/content/noe-http.md).

- `noe:http` now exposes only host helpers, with no default/fetch export:
  Serve selects `serve`; WebSockets selects `createWebSocket`; both also
  select `upgradeWebSocket`. Fetch-only does not install this module.
- `node:http`, bare `http`, and installed global serve/upgradeWebSocket are
  removed without adapters. Bare `http` can be import-mapped; reserved
  prefixes remain independent. Standard Fetch, Request/Response, WebSocket,
  EventSource, and Streams stay global.
- Custom handshake headers now use synchronous
  `createWebSocket(url, {protocols, headers})`. It returns the same WebSocket
  class in CONNECTING state, using the existing validation/snapshot,
  authorization, TLS, events, native backend, and execution cleanup.
- The standard constructor has its two-argument surface. Its old third
  argument no longer supplies headers. Default binaryType is now `"blob"`;
  callers requiring raw buffers explicitly set `"arraybuffer"`.
- A final data-flow check found existing Blob sends were text-coerced. They
  now synchronously enter the native binary queue through the built-in Blob
  byte storage, preserving payload snapshots and message order. Default-Blob
  client/server echo plus a following text message is tested. File inherits
  the same supported path.
- Fetch response overflow now rejects in buffered/no-loop and streaming
  paths, instead of succeeding with a prefix. Exact cap is allowed after
  actual EOF; the check probes at most one extra byte, does not trust
  Content-Length, and releases the owned response. Zero stays 32 MiB and
  negative stays unlimited. Abort during the boundary wait releases the peer.
- Go options, dependencies, command/filesystem backends, and WebSocket native
  transports were unchanged by that slice. The subsequent Serve slice below
  replaces its former request-prefix cap and synchronous close contract.
- NOE's callers, hidden instructions, authentication helper, examples,
  declarations, references, and public consumers are migrated. Historical
  phase/release text keeps its original contracts.

## HTTP/Fetch validation and evidence

Validation is complete. Evidence: `/var/tmp/noe-http-policy-8FsuKH`.

- Individual command JSON/logs and `verification.json` cover core/public
  standard-library suites, affected integration packages, TypeScript/API
  catalog/lint/vet, selected HTTP/Serve/WebSocket/EventSource/docs E2E,
  targeted race checks, affected agent-server scenarios, auth help, and
  standalone bundled docs. New tests cover all eight capability selections,
  standard Blob/ArrayBuffer delivery, header-factory identity/validation,
  exact/over-limit reads and cancellation while the EOF probe is blocked.
- Initial full-suite failures were stale test bindings/import concatenation
  and a former docs wording assertion. Focused final rechecks passed; other
  successful tests were reused rather than repeating complete suites.
- The final Blob-send correction passed the WebSocket-focused tests,
  targeted race check, and real-CLI four-mode header E2E. An independent
  bounded static review and one narrow Blob-send re-review found no blocker.
- `consumer-final/` is the final checkout/module-artifact consumer snapshot,
  including authenticated binary Blob echo, with CGO disabled and all six
  release-target builds. The earlier `consumer/` is intermediate evidence.
- `source-manifest.json` records the final working-tree hashes. It includes
  the previously completed process migration, whose code is preserved.

The final fresh candidate is
`/var/tmp/noe-http-policy-8FsuKH/noe-final`; the earlier `noe` candidate predates
Blob-send correction. Neither is installed. Do not test new APIs with `./noe`
or an installed binary unless rebuilt. No external provider or noebot test
was run. Unchanged native filesystem/process results from earlier slices
remain reusable.

## Completed process and standard I/O slice

See [implementation, migration, and verification](stdlib-noe-process.md) and
[embedded API reference](../../internal/apidocs/content/noe-process.md).

- One `noe:process` module is installed if ProcessInfo, Stdio, or Commands
  is selected. With none selected it is absent. All three capabilities
  remain independently selectable, including in Workers.
- Commands exports `exec`, `execFile`, `execFileSync`, `execSync`, `spawn`,
  and `spawnSync` with their existing signatures, results, environment/cwd,
  authorization, timeout, output limits, and child ownership.
- Stdio exports the same `stdin`, `stdout`, and `stderr` byte Web Streams.
  Workers retain immediate stdin EOF and configured output destinations.
  Closing stream endpoints does not close borrowed host readers/writers.
- ProcessInfo exports named `process`, identical to global `process`.
  Arguments, environment snapshot, metadata, clocks, exit/exitCode,
  nextTick, events, and Worker exit behavior remain. With Stdio selected,
  process stream properties are the same named stream instances.
- The default object contains the selected named exports. Metadata is at
  `default.process`, not `default.env` or `default.cwd`; commands do not
  become properties of global `process`.
- All four legacy imports are removed without adapters. Bare `process` and
  `child_process` can now be import-mapped. `node:`/`noe:` prefixes remain
  reserved independently, including removed names.
- `internal/stdlib/process_module.go` composes metadata/captured commands;
  `internal/noestdlib` completes the same module with the native live-child
  and stdio adapters before user imports. No native backend, public Go
  configuration field, or dependency changed.
- NOE callers, hidden libraries/instructions, auth helpers, examples, public
  Go consumers, types, and current references are migrated. The MCP spike's
  NOE transport is migrated; its Node server keeps Node imports. Historical
  release/phase records retain their original contracts.

## Process validation and evidence

Validation is complete for this slice. Local evidence:
`/var/tmp/noe-process-policy-6fHylF`.

- Individual JSON/log files and `verification.json`: full core stdlib and
  native-I/O suites, public stdlib suite, affected integration/API catalog
  packages, selected real-CLI/Worker/fixture-agent process-I/O E2E, five
  affected offline libraries, affected example scenarios, auth-helper help,
  standalone bundled docs, lint, and vet.
- All eight capability selections verify exact exports, namespace/global
  identities, and removed imports. Existing permission, cwd/environment,
  cancellation, borrowing, Worker, disposal, and explicit-helper checks pass.
- `consumer/`: checkout and standalone module-artifact public consumers ran
  with CGO disabled. The artifact consumer compiled for all six release
  targets. Dependencies are unchanged from the completed FS slice.
- TypeScript positive/negative fixtures and the embedded API catalog suite
  passed. The initial integration failure was an export-inventory wording
  mismatch in the new docs; the inventory was fixed and the full runtime
  suite passed. No unresolved runtime failure remains.
- Independent bounded static review found no core-composition blocker.
- `source-manifest.json` records the final working-tree source hashes.

The fresh temporary candidate is
`/var/tmp/noe-process-policy-6fHylF/noe`. It is not installed. Do not use
`./noe` or an installed executable as evidence for these unreleased APIs
without rebuilding. The official MCP SDK spike was not rerun; its transport
change is an import replacement. Noebot was not investigated or validated.

Reuse completed results for unchanged code. Do not rerun all unit/E2E or
long-running checks merely for a local commit, version bump, or release
instruction.

## Completed filesystem slice

See [implementation and verification](stdlib-noe-fs.md) and the
[embedded reference](../../internal/apidocs/content/noe-fs.md).
Only `noe:fs` is registered, with direct async operations, explicit Sync
helpers, concise FileHandle `read`/`readAt`/`write`/`writeAt`, options-based
`open`, and iterator-only watching. All four old FS imports are removed.
Native ownership, positional/append behavior, Root/Cwd/ReadOnly, shutdown,
partial-I/O errors, and borrowed Go filesystem views remain as documented.
The complete FS evidence remains at `/var/tmp/noe-fs-policy-aj2_zwmi`;
process naming changes do not invalidate unchanged native filesystem tests.

## Next continuation

The filesystem/process/HTTP entrance migrations, Fetch response-overflow and
Blob-send corrections, Serve limits/lifecycle, WebSocket opening
cancellation/close validation, and connected closing/blocked-send cleanup
are included since v2.16.0. HTTPS/WSS listeners are included since v2.17.0;
raw TLS and UDP are published in v2.18.0, with assets and public modules
verified. Both local executable paths now contain v2.21.0, including Storage.
No required runtime blocker remains in these approved slices. Future releases
include local installation without
a separate approval prompt; running-service restarts remain separate.

The user asked to keep continuation focused on standard-library improvements.
Release-output metadata cleanup is housekeeping, not a new stdlib milestone.
Its one-line `/dist/` ignore fix is committed at `acb1ee2`; evidence is at
`/var/tmp/noe-ci-provenance-28dxjsmx/verification.json`. It applies to future
builds and does not change the published v2.17.0 artifacts. v2.18.0 uses that
correction and all six artifacts have clean VCS metadata. The user then
approved raw TLS connections/listeners and then UDP in roadmap phase 11. Both
first shipped in v2.18.0 and remain in installed v2.21.0. This completes
the roadmap's core networking additions; multicast/connected UDP are separate
possible extensions. Keep next-step work on stdlib rather than automatically
starting release preparation.

Remaining Node utilities, Buffer, and process metadata methods are
use-case-driven work. Avoid wholesale rename churn merely to eliminate Node
names. Useful existing contracts can remain until a concrete migration is
justified. The latest permission allows local breaking changes without
another compatibility prompt, while preserving noebot/publication/install
boundaries. Prefer concrete caller evidence for the next implementation.
The requested v2.19.0 publication and installation are complete. Keep
continuation focused on concrete standard-library needs. A version or release
instruction alone is not a reason to repeat successful implementation suites.
The existing Web Crypto correction and SDK validation above are complete
locally. Additional algorithms/formats and WebSocketStream remain use-case driven.

Reuse the completed results for unchanged source/dependencies. A version,
commit, or release instruction alone is not a reason to repeat all unit,
E2E, race, or consumer cross-builds. Use the latest fresh temporary candidate
for source-only APIs; installed executables still describe their own release.

## Other already completed or deferred work

- EventSource is a standard global with reconnection/cancellation and shared
  Fetch ownership; see [implementation](stdlib-eventsource.md).
- SQLite uses the approved asynchronous `noe:sqlite` API; see
  [implementation and limitations](stdlib-sqlite.md).
- Web Crypto's existing contracts are corrected locally as above. Further
  expansion and WebSocketStream remain use-case driven. The
  broader [roadmap](stdlib-roadmap.md) applies under the newer policy.
- The former Fetch response-prefix improvement candidate is now fixed and
  verified; see the correction above. The old exploratory probe at
  `/var/tmp/noe-api-direction-7odu19wj/probe.go` is historical evidence, not
  a current unresolved failure.
