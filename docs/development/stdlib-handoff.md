# Standard library session handoff

Updated 2026-11-07 after the user authorized publishing the prepared v2.17.0
HTTPS/WSS release, following the v2.16.0 publication and binary installation.
This document records the current continuation point; linked policy and
implementation records remain the canonical design and validation references.

## Repository and publication state

- Repository: `/home/agent/projects/noe`, branch `main`.
- Verified implementation checkpoints are EventSource/SQLite `1d04402`,
  filesystem `295fb53`, process/HTTP/Serve `3997dc0`, WebSocket opening
  `248e74a`, and connected closing `617cfcd`.
- On 2026-11-07 the user explicitly authorized releasing the completed work.
  v2.16.0 is now published from annotated tag revision
  `797b9fbfa9884cabdd682fdf79afc2d80015fae5`. CI run 55 (API 236) succeeded;
  all six downloaded checksums/build metadata, native smoke, and direct
  published stdlib/agent consumers passed. See
  [release checkpoints](../releases/release-checkpoints.md#v2160-publication--2026-11-07).
- See [v2.16.0 notes](../releases/v2.16.0.md) and the consolidated
  [migration guide](../releases/v2.16.0-migration.md). The public Go module
  remains `/v2`; JavaScript import and behavior changes have no adapters.
- Latest verified published native binary:
  `/var/tmp/noe-v2.16.0-release-e9ux2qxa/assets/noe-linux-amd64`.
  That directory also retains the clean candidate, all downloaded assets,
  continuity checks, and published-consumer evidence. Earlier implementation
  candidates below remain historical slice evidence.
- After publication, the user explicitly requested binary replacement. Both
  `/home/agent/.local/bin/noe` and `/home/agent/projects/noe/noe` now contain
  the verified published v2.16.0 Linux asset, SHA256
  `13782c9bf8c30c64808da7a9fd577d978e8e123928d7dfbc41cfaae8b601849e`.
  Previous v2.13.0 copies and installation evidence are retained under
  `/var/tmp/noe-v2.16.0-install-014hncw0` (`installation.json`). Version,
  arithmetic, and both authentication-helper help paths passed; no actual
  authentication or running-service restart was performed.
- The user then approved implementing HTTPS/WSS listeners. That addition is
  committed at `6cdc02f`. Publication of v2.17.0 is now explicitly authorized.
  A clean release-flags
  candidate for the prepared v2.17.0 is now at
  `/var/tmp/noe-v2.17.0-release-yk3xb7im/candidate`; the earlier implementation
  candidate remains at `/var/tmp/noe-https-vm9c6p5e/noe`. Installed v2.16.0
  copies do not implement its listener options and were not replaced again.
- The user independently handles noebot. The latest release instruction
  authorizes v2.17.0 publication; installation and service restart remain
  separate actions.

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

## Prepared v2.17.0 (publication authorized, verification pending)

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
  The user explicitly authorized publication after this preparation. Tag a
  documentation-only successor of the candidate revision without changing
  validated source, then verify the uploaded assets and public modules.
  No installation or service restart is included.

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
are published in v2.16.0 and installed at both requested local paths.
HTTPS/WSS listeners are implemented and verified locally on top of that
release, and v2.17.0 is prepared with a verified clean candidate. No required
runtime blocker remains in these approved slices. The user explicitly
authorized the annotated tag and publication; complete the downloaded
asset/module checks afterward. Binary replacement remains a separate action.

Remaining Node utilities, Buffer, and process metadata methods are
use-case-driven work. Avoid wholesale rename churn merely to eliminate Node
names. Useful existing contracts can remain until a concrete migration is
justified. The latest permission allows local breaking changes without
another compatibility prompt, while preserving noebot/publication/install
boundaries. Prefer concrete caller evidence for the next implementation;
raw TLS streams and UDP remain deferred.
Web Crypto expansion and WebSocketStream remain use-case driven as below.

Reuse the completed results for unchanged source/dependencies. A version,
commit, or release instruction alone is not a reason to repeat all unit,
E2E, race, or consumer cross-builds. Use the latest fresh temporary candidate
for source-only APIs; installed executables still describe their own release.

## Other already completed or deferred work

- EventSource is a standard global with reconnection/cancellation and shared
  Fetch ownership; see [implementation](stdlib-eventsource.md).
- SQLite uses the approved asynchronous `noe:sqlite` API; see
  [implementation and limitations](stdlib-sqlite.md).
- Web Crypto expansion and WebSocketStream remain use-case driven. The
  broader [roadmap](stdlib-roadmap.md) applies under the newer policy.
- The former Fetch response-prefix improvement candidate is now fixed and
  verified; see the correction above. The old exploratory probe at
  `/var/tmp/noe-api-direction-7odu19wj/probe.go` is historical evidence, not
  a current unresolved failure.
