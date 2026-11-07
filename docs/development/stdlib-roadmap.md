# Standard library integration and expansion roadmap

The [API policy approved on 2026-11-06](stdlib-api-policy.md) governs new work:
Web standards and usability take priority; NOE-specific host APIs use `noe:`
names. Earlier phase decisions below are historical. The first policy
application is the [migration to `noe:fs`](stdlib-noe-fs.md), followed by process
and host-I/O naming, then HTTP helpers. Expansion phases remain available.

The `noe:fs` migration is included in v2.16.0. NOE's
examples, hidden libraries, runtime/Worker fixtures, public Go consumers,
types, and references use the single new entrance. noebot migration is being
handled independently by its owner.

[Development guide](README.md) · [Feature candidates](../future-candidates.md) · [Earlier txiki.js comparison](txikijs-comparison-20261024.md)

2026-11-05. Proposed sequence for integrating NOE's standard-library APIs
and gradually expanding their capabilities. Establish a consistent API and
resource model first, then build new features on that foundation. Detailed
contracts and the canonical module names are decided in the relevant phase.

Phases 1 and 2 were released in [v2.13.0](../releases/v2.13.0.md). See the
[phase 1](stdlib-fs-integration.md) and [phase 2](stdlib-fs-basics.md)
implementation and validation records. This completes the first filesystem
milestone. The approved API uses symbolic filesystem error codes with the
positive platform OS number in `errno`, `mtime` as a Date, and canonical POSIX
file-type, permission, and special bits in `mode`.

Phases 3 and 4 were released in [v2.14.0](../releases/v2.14.0.md). The process, standard-I/O, and cancellation
sections below record their implementation and compatibility details.
Phases 5–7 and phase 9.1 are included in [v2.15.0](../releases/v2.15.0.md):
filesystem watching (`10a8f70`), default source maps (`baa984d`), data imports
and import-map prefixes/scopes (`fd8702b`), and WebSocket handshake headers
(`7db6835`). See the [watch](stdlib-fs-watch.md),
[source-map](stdlib-source-maps.md), [module-loading](stdlib-module-loading.md),
and [header](stdlib-websocket-headers.md) implementation records. The HTTP
raw-body policy includes the approved MIME/UTF-8/BOM behavior changes.

The user chose to defer Web Crypto (phase 8) until concrete SDK requirements
justify an algorithm/key-format addition. EventSource is implemented locally
after the header work, while WebSocketStream remains use-case driven.
The v2.14.0 consumer rollout is covered by the
[noebot migration instructions](noebot-v2.14.0-migration.md).

Phase 9.2 is included in v2.16.0: standard EventSource, named events, close,
reconnection, and shared Fetch authority/ownership. See the
[EventSource implementation record](stdlib-eventsource.md). This addition does
not require migrating existing consumers. WebSocketStream remains deferred
until a concrete application needs it.

Phase 10's SQLite slice is included in v2.16.0,
through the approved asynchronous `noe:sqlite`
API. Memory databases require no filesystem grant; file databases share the
existing FS policy. See the [SQLite implementation record](stdlib-sqlite.md).
Persistent localStorage and sessionStorage remain separate candidates.

Phase 11's HTTPS/WSS listener slice is included in v2.17.0.
`noe:http.serve` accepts explicit PEM-text
`cert`/`key`; Go hosts can instead select `ServeOptions.TLS`. Both use the
existing HTTP/1.1 server, WebSocket upgrade, capability, and cleanup paths.
See the [HTTPS/WSS contract and evidence](stdlib-https.md). Raw TLS streams
and UDP sockets remain deferred until a concrete application needs them.

## Phases

| Phase | Work | Intended outcome |
| --- | --- | --- |
| **1. Integrate filesystem APIs** | Establish a common FileHandle for open, read/write, positional I/O, and close. Choose the canonical entry point and how existing entry points connect to it. | File operations use one coherent API model. |
| **2. Expand basic filesystem operations** | Add FileHandle stat, truncate, and sync; creation and append flags; Dirent and readdir withFileTypes. Align result and error shapes. | Routine file operations follow consistent contracts. |
| **3. Integrate processes and standard I/O** | Connect noe:process, noe:io, and Node-style entry points to shared implementations. Define startup, exit waiting, close, and stream contracts. | Captured command execution and interactive child-process communication fit the same design. |
| **4. Unify cancellation and cleanup** | Add AbortSignal.any, align cancellation in pipe operations, and introduce asynchronous disposal for resources such as FileHandles and child processes. | Timeouts, caller cancellation, and execution shutdown reach APIs consistently. |
| **5. Add filesystem watching** | Provide watch, change notifications, subscription close, and cancellation. | Scripts can reload configuration, wait for file changes, and support long-running services. |
| **6. Enable source maps** | Connect the engine's existing source-map support to NOE and align diagnostics across the CLI, Workers, and agents. | Errors in bundled JavaScript identify their original source locations. |
| **7. Expand module loading** | Support JSON, text, and bytes imports, followed by import-map prefix mappings and scopes. | Data, templates, and existing ESM layouts are easier to use. |
| **8. Expand Web Crypto** | Select algorithms and key formats from actual SDK requirements. Add AES-GCM, key generation, and JWK support in small steps, followed by algorithms such as Ed25519 and ECDSA. | More JavaScript authentication and encryption libraries can run. |
| **9. Expand Web communication APIs** | Add custom WebSocket headers, EventSource, and WebSocketStream. | Authenticated connections and continuous event consumption have standard API forms. |
| **10. Add local data storage** | Evaluate and introduce SQLite, then add persistent localStorage when useful. | JavaScript can query and update local application state and collected data. |
| **11. Expand networking** | HTTPS/WSS listeners are included in v2.17.0. Raw TLS streams and UDP sockets remain deferred. | Applications can terminate HTTPS/WSS directly; additional protocols follow concrete demand. |
| **12. Evaluate larger extensions** | Assess WebAssembly/WASI and general FFI against concrete applications and libraries. | Existing external assets become usable where their integration is worthwhile. |

SQLite and later phases may move earlier when a concrete application needs
them. HTTP/3, XMLHttpRequest, FileReader, and Navigator APIs are additional
candidates when a target service or SDK requires them. sessionStorage can
be implemented independently of SQLite because it is an in-memory store.

## Filesystem integration as the first milestone

Complete phases 1 and 2 as the first coherent milestone: ordinary file
operations should fit one API system, with examples and documentation that
teach that system.

Path resolution, Root, and ReadOnly already have a shared filesystem basis.
The remaining integration concerns public entry points, handles, operations,
and error conversion. Reuse that basis when joining whole-file operations,
Web Streams, and positional I/O.

The approved phase 1 decision is to make `node:fs/promises` the canonical
asynchronous filesystem entry point and remove `noe:fs-handle` without a
compatibility adapter. `node:fs/promises.open`, bare `fs/promises.open`,
`node:fs.promises.open`, and the default `node:fs` object's
`promises.open` property expose the same function. These routes use the
selected filesystem and preserve its Root, ReadOnly, and execution-owned
cleanup behavior.

The supported phase 1 scope is `open(path, flags = "r")` for existing files;
only `"r"` and `"r+"` are accepted, with no create or truncate mode. FileHandle
provides sequential or positional `read` and `write` plus `close`. Reads accept
an ArrayBufferView and return `{bytesRead, buffer}` with the original view;
the options form allocates a 16 KiB Uint8Array by default. Writes accept an
ArrayBufferView or a UTF-8 string and return `{bytesWritten, buffer}` with the
original input. Omitted or null positions use the current cursor; safe,
nonnegative integer positions perform positional I/O without changing it.
`close()` is idempotent, rejects new I/O immediately, and normally waits for
started I/O; runtime cleanup force-closes remaining handles. Argument,
read-only, closed-handle, OS errno, and other I/O errors retain their specified
phase 1 codes and partial-I/O fields. Error normalization and the broader
filesystem operations remain phase 2 work.

This phase does not add `readAt`, `writeAt`, or `eof` to FileHandle. Existing
imports of `noe:fs-handle`, its `{mode: ...}` option, and its positional
methods are breaking changes and must migrate to the standard API.

### Phase 1 consumer compatibility audit

Source audit on 2026-11-05, before changing the filesystem API. NOE was
inspected at `7b7df5750b006913f16f3182729efeee311978cb`, including tracked
hidden libraries and the bundled MCP SDK. The private noebot repository was
cloned from its remote default branch, rather than relying on the older
local checkout. Its `main` resolved to
`45de74771d9c048448afb1e7f1039b0b45a01443` at both the start and end of the audit.

| Consumer | Existing handle API usage | Phase 1 impact |
| --- | --- | --- |
| `examples/` (58 JavaScript/TypeScript files) | Only [index-roundtrip.js](../../examples/low-level/index-roundtrip.js) imports `noe:fs-handle` and calls `open`, `readAt`, and `writeAt`. | Migrate that example if the old API is removed. Other examples use existing whole-file or synchronous functions. |
| `.noe/lib/` (13 JavaScript files, including tests) | No `noe:fs-handle`, `readAt`, or `writeAt` usage. | No caller changes identified if existing filesystem functions retain their contracts. |
| Built-in agent, decision, image, and credential JavaScript (11 files) | No use of the old filesystem handle API. | No caller changes identified. |
| Remote noebot (54 JavaScript/TypeScript/Python files, including tests and generated JavaScript fixtures) | No `noe:fs-handle`, `readAt`, or `writeAt` usage. Uses existing `node:fs` functions and promise `readFile`. | No caller changes identified for the proposed handle integration. |

The audit identified NOE's public type declarations, Go consumer and profile
fixtures, lifecycle and integration tests, and current API documentation as
migration points. Existing imports of `noe:fs-handle`, the `{mode: "r+"}`
argument, and the positional methods/results stop working under the approved
change. Historical release records should continue describing the version
they document.

The user approved migrating the in-repository callers and removing the old API
without a compatibility adapter on 2026-11-05. This records the approved
migration; phase 1 implementation and local validation are complete.

This was a source audit, not an execution regression run. It covers the
tracked consumers above, not arbitrary existing user scripts or future
agent-generated code. noebot's synchronous persistence and its `sync -f`
fallback are separate consumers to revisit when expanding sync and creation
operations in phase 2; adding an asynchronous FileHandle alone does not
replace that persistence contract.

The subsequent execution checks, including the unchanged library and noebot
consumers, are recorded in the [phase 1 validation](stdlib-fs-integration.md).

Current filesystem contracts and implementation entry points:

- [noe:fs](../../internal/apidocs/content/noe-fs.md)
- [Shared filesystem backend](../../internal/filesystem/filesystem.go)
- [Profile installation](../../internal/noestdlib/noestdlib.go)

## Process and standard I/O integration: first slice

2026-11-05, after the v2.13.0 filesystem milestone. Phase 3 has started
with a source and consumer audit. The user then approved the first slice:
publish the existing Promise/Web Streams `spawn` through `node:child_process`
and remove `noe:process` without a compatibility adapter. The standard-I/O
follow-up is recorded in the second slice below.

The command backends are already shared: captured `node:child_process`
operations in `internal/stdlib/exec.go` and streaming `noe:process.spawn`
in `internal/nativeio/process.go` both use `internal/commandenv` and
`internal/process`. They already share executable lookup, configured
environment/cwd resolution, command authorization, and OS process ownership.
The remaining integration is primarily the public API and stream adapters.

| Surface before this slice | Contract to account for |
| --- | --- |
| `node:child_process` and `child_process` | Captured command APIs, including Promise-based `execFile` and `exec`; no streaming `spawn` export. |
| `noe:process.spawn` | Promise-based startup with Web Streams and `exited`, asynchronous `kill`, and idempotent `close`. Profile environments use replacement semantics; the legacy native installer uses an overlay. |
| `noe:io` | Demand-driven byte Web Streams and a cancellation-aware `pipe` helper. |
| `node:process` and global `process` | Default export with synchronous whole-input `stdin.read()` and text `stdout.write()`/`stderr.write()` facades. These differ from both the Web Streams above and full Node streams. |

Unlike the earlier filesystem-handle removal, process entry-point changes
affect existing noebot code. Its remote `main` was checked again at
`45de74771d9c048448afb1e7f1039b0b45a01443`: `app/main.mjs` and
`tests/s1.mjs` import `noe:process`, and the host launcher awaits startup,
inherits all three streams, and uses the owned child's lifecycle methods.
NOE's find, search, and repo-map libraries and several process/agent-server
examples also use that entry point. `examples/json/summarize-stdin.js` uses
the synchronous `process.stdin.read()` facade.

The approved first slice adds the existing Promise/Web Streams `spawn`
to `node:child_process`, sharing the function across named/default exports
and the bare `child_process` alias. `noe:process` is removed. NOE's examples,
hidden libraries, types, and current references migrate with the module.
The required noebot migration is recorded alongside its
[deferred API-adoption work](stdlib-fs-basics.md#required-process-import-migration-for-the-next-noe-release).

Node's [`spawn` contract](https://nodejs.org/api/child_process.html#child_processspawncommand-args-options)
returns a ChildProcess immediately and reports startup and exit through
events. Its standard I/O uses Node streams. Moving NOE's current function
under a Node-style module name retains NOE-specific semantics, documented
explicitly in its reference. It does not establish Node spawn compatibility.

The subsequent standard-I/O slice preserves the separation of ProcessInfo,
Stdio, and Commands capabilities and the main/Worker ownership rules.

### Implementation and validation

The first slice is implemented locally. `internal/nativeio` builds the owned
streaming adapter; `internal/noestdlib` composes it with the captured-command
functions through `internal/stdlib.CommandsWithSpawn`. Composition finishes
before user initializers or Worker entry modules run. The old module is no
longer registered, and its separate embedded reference was merged into the
canonical child-process reference.

The following checks passed:

- Full unit tests for `internal/nativeio`, `internal/noestdlib`,
  `internal/jsexec`, `internal/runtime`, `internal/stdlib`, and public `stdlib`.
- Targeted race checks for streaming/captured API identity and output,
  authorization, command environment and timeouts, close/ownership, and
  Worker process streams.
- CLI `TestProcessIO*` E2E tests, including live/binary pipes, cancellation,
  Worker termination, and fixture-agent subprocess-tree recovery.
- External public stdlib consumer with both a checkout replacement and an
  independent module artifact: startup, binary Web Streams, captured output,
  all module aliases, and removal of the old entry point. The artifact built
  for Linux, Darwin, and Windows on amd64/arm64; runtime checks ran on Linux.
- All six offline `.noe/lib` suites and the 170 existing noebot checks with
  only the two required imports changed in a temporary copy.
- Public TypeScript positive/negative fixtures, embedded API catalog tests,
  changed JavaScript lint, and the migrated Python fixture's syntax check.
- Candidate CLI reference lookup and the migrated process-worker and binary
  process-pipeline examples. The old documentation topic is unavailable.
- Affected-package vet and independent read-only review of module composition,
  capability selection, initialization ordering, and ownership. No actionable
  findings remained.

The first E2E attempt exposed an existing test-fixture race: shell redirection
could create a PID file before `echo` wrote its contents. The PID helper now
waits for nonempty contents within its existing deadline, then applies the
same positive-integer validation. The complete process E2E group passed
after that test-only correction.

Logs, the consumer module artifact, and the temporary noebot migration patch
are under `/tmp/noe-process-integration-863auo4a/`. The installed NOE binaries
and running applications were not changed. No release or paid provider calls
were performed for this slice.

## Process and standard I/O integration: second slice

2026-11-05, based on `64bd5c4`. After committing the process-entry slice,
the user approved standard-I/O integration without compatibility adapters.
Implementation and local validation are complete; released in v2.14.0.

| Previous surface | Approved replacement |
| --- | --- |
| `stdin`, `stdout`, `stderr` imported from `noe:io` | Named imports from `node:process` or bare `process`. The old module is removed. |
| Separate `process.std*` synchronous facades | The same byte Web Streams as the module's named exports. Default and global process properties share their locks and lifecycle. |
| `process.stdin.read()` returning text or `null` | Use a Web reader, async iteration, or `await new Response(stdin).text()`. Whole-input text consumption returns an empty string at empty EOF. |
| `process.stdout.write(text)` / `stderr.write(text)` returning a boolean | Encode text and await a Web writer's `write(Uint8Array)`, releasing the writer when finished. The old `isTTY` facade is also removed. |
| `pipe(source, destination, options)` | `source.pipeTo(destination, options)`. The native helper is removed. Use an actual AbortSignal for the standard signal option; the old helper's falsy/structural signal extensions are not retained. |

The shared streams have one lock each: code cannot independently acquire
writers through named and default/global aliases at the same time. Console
output still uses the configured host destination independently of the
runtime's Web Stream endpoint.

Capability selection remains independent. With ProcessInfo and Stdio selected,
the default process object is also the global object and exposes the same
three streams. With Stdio alone, the module default and named exports provide
only streams; no process metadata or global process object is installed.
With ProcessInfo alone, the module default/global metadata remain available
and standard-I/O properties and named exports are absent. Neither capability
means the process module is unavailable.

Workers retain immediate-EOF stdin and their configured output destinations.
Runtime-owned endpoints are reclaimed without closing borrowed host streams.
The public Go Environment and Profile selection fields remain unchanged;
the obsolete standard-I/O fields in the internal metadata configuration and
its synchronous implementations are removed.

Removing the native pipe helper requires fixing standard `pipeTo` cancellation:
its previous abort listener only set a flag while reads/writes could remain
pending. The implementation must actively stop eligible endpoints, preserve
the abort reason, honor preventClose/preventAbort/preventCancel, and release
locks on completion and failure. The specification reference is the
[WHATWG pipe algorithm](https://streams.spec.whatwg.org/index.html#readable-stream-pipe-to).
The same flag-only implementation is present in the pinned upstream
`github.com/go-quickjs/go-quickjs@v0.32.0/stdlib/streams.go`; a future upstream
proposal should separate this correctness fix from NOE's API consolidation.

The remote noebot revision remains
`45de74771d9c048448afb1e7f1039b0b45a01443`. Its inspected sources do not use
`noe:io`, synchronous `process.std*` calls, or process-module standard-I/O
imports. No further noebot source migration was identified for this slice;
the earlier two required spawn-import changes remain queued separately.

### Second-slice implementation and validation

`internal/stdlib.Processes` now installs metadata only.
`internal/nativeio.Host.Stdio` builds the execution-owned Web Streams, and
`internal/noestdlib` publishes them through `ProcessStdio` before user
initializers or Worker entry imports. The old native module and pipe helper
and the separate synchronous metadata-configuration fields are removed.

The standard pipe implementation now actively handles shutdown while reads,
writes, or backpressure are pending. Cancellation actions start together;
accepted writes settle before pipe completion and lock release. An internal
read-interruption helper preserves source data when preventCancel is set,
without changing public reader.releaseLock behavior. EOF under backpressure
and asynchronous EOF close the destination once. Stop-wait subscriptions are
removed after each operation, avoiding retention across long-lived pipes.

Validation passed:

- Full unit tests for `internal/stdlib`, `internal/nativeio`,
  `internal/noestdlib`, `internal/jsexec`, `internal/runtime`, `internal/cli`,
  and public `stdlib`.
- Targeted race tests for pipe cancellation and prevent flags, scoped standard
  I/O, alias identity, shutdown, and nested Worker cleanup.
- CLI process/standard-I/O, summarize-stdin, low-level ownership/cancellation,
  and HTTP upload/receive E2E tests.
- External public stdlib consumer through a checkout replacement and a
  standalone module artifact. Covers Stdio without ProcessInfo, aliases,
  inherited Worker behavior, and retained ownership of borrowed input/output.
  The artifact built for all six release targets; runtime checks ran on Linux.
- Six offline `.noe/lib` suites and remote noebot S1 31/31, S2 83/83,
  S3 45/45, and images 11/11. The temporary noebot copy contains only the
  previously required spawn-import changes.
- TypeScript positive/negative fixtures, API catalog tests, changed JavaScript
  lint/syntax, affected-package vet, and whitespace checks.
- Candidate CLI reference checks; process-worker, binary process-pipeline,
  and summarize-stdin examples; both authentication-helper help commands.
- Independent read-only review of module registration, capability separation,
  ownership, Worker behavior, and pipe shutdown. Review findings about EOF,
  accepted-write completion, and retained stop subscriptions were fixed and
  checked with targeted runtime probes. No actionable findings remained.

The first full unit run found an old optional-chaining stdin call in
`scripts/auth-codex-images.js`, which the literal-call audit had missed.
It now consumes the Web Stream while retaining sanitized callback errors.
The local OAuth fixture tests and the full runtime package passed after the
fix; no real provider calls or credential changes were needed.

Evidence is under `/tmp/noe-stdio-integration-dkaizbjk/`, including unit,
runtime recheck, race, E2E, public consumer, library, and noebot logs. Installed
NOE binaries and running applications were not replaced or restarted.

## Cancellation and cleanup: phase 4

2026-11-06, based on `eb460bf`. Implemented and locally verified; released in v2.14.0.
The pipe cancellation work was completed during phase 3. This phase adds
combined abort signals and asynchronous disposal to existing owned resources.

`AbortSignal.any(signals)` validates and consumes an iterable of genuine
signals before choosing a pre-aborted input. Pre-aborted sources win in input
order; otherwise the first actual abortion supplies the exact reason.
Nested combinations are flattened, duplicate sources are handled once, and
synthetic abort events do not propagate. Composite states are established
before source events, and dependent events follow source events. Active
composites remain observable while they have abort listeners or an onabort
handler and live sources; inactive or settled dependencies can be collected
or unlinked. Existing controller and timeout Error defaults are retained.

FileHandles, child processes, raw connections, socket listeners, and signal
subscriptions now implement `Symbol.asyncDispose`. Each method awaits its
existing `close()` and fulfills with undefined. The pinned engine already
supports `await using`, so normal block exit, return, and exceptions can invoke
the existing cleanup through that syntax. File close still drains admitted
I/O; child cleanup still stops/reaps a running process; listener/subscription
cleanup still releases waiters. Forced runtime shutdown is unchanged.

### Phase 4 compatibility and validation

No existing module or method is removed in this phase, and normal callers
need no migration. One low-level behavior changes: actual controller/timeout
abort events use internal dispatch rather than an instance's overridden
dispatchEvent method. Such overrides no longer intercept real abortion.
No corresponding use was found in the inspected NOE or noebot consumers.
Ordinary listener and onabort assignments retain their behavior; onabort is
now tracked through an accessor so observed composites remain alive.

Validation passed:

- Full unit tests for `internal/stdlib`, `internal/nativeio`,
  `internal/noestdlib`, `internal/jsexec`, `internal/runtime`, and public
  `stdlib`, including existing abort/fetch/pipe behavior.
- Targeted race tests for abort composition/GC lifetime, pipe cancellation,
  file and native-resource cleanup, and the new disposal integration.
- New CLI run/eval/Worker/events and fixture-agent run_js tests combining
  AbortSignal.any, pipe cancellation, and await-using file cleanup. Existing
  low-level, process/stdio, and HTTP upload/receive E2E tests also passed.
- Public Go tests with real FileHandle write draining and exception cleanup,
  live child reaping, socket/listener/signal pending-waiter release, and
  combined-signal cancellation of a borrowed native stdin reader.
- External stdlib consumer using a checkout replacement and a standalone
  module artifact. The consumer exercises combined cancellation and scoped
  file/child disposal. Builds passed for Linux, Darwin, and Windows on
  amd64/arm64; native execution ran on Linux.
- Remote noebot at `45de74771d9c048448afb1e7f1039b0b45a01443`: existing
  S1/S2/S3/images 170 checks plus Telegram commands 20, Telegram typing 13,
  and Web HTTP/SSE timing 5. Only the previously required two spawn-import
  changes were applied to its temporary copy.
- TypeScript positive/negative fixtures, API catalog tests, changed JavaScript
  lint/syntax, affected-package vet, and whitespace checks. The declarations
  reference the engine-supported explicit-resource-management library.
- Independent read-only review of abort graph validation, propagation,
  reentrancy, weak/observed lifetime, and disposal ownership. Strict sequence
  validation, observed-composite retention, and delivery despite dispatch
  cleanup failures were corrected and rechecked. No actionable findings remain.

The source/reference audit used the
[DOM AbortSignal specification](https://dom.spec.whatwg.org/#dom-abortsignal-any)
and the [FileHandle disposal contract](https://nodejs.org/api/fs.html#filehandlesymbolasyncdispose).
These guide the supported additions; existing NOE Error defaults and owned
child semantics remain as documented. Upstream candidates are recorded in
[UPSTREAM.md](../../internal/stdlib/UPSTREAM.md).

Logs, source manifests, public consumer artifacts, and the temporary noebot
patch are under `/tmp/noe-cancellation-cleanup-v90s154r/`. No paid provider
calls were made. Installed binaries, running applications, and the actual
noebot repositories were not changed.

## Development and distribution tools

TypeScript execution, bundling, and standalone executable generation form a
separate development/distribution track after source-map support. Their
dependencies on stdlib features should be explicit, while their CLI and build
contracts remain part of that track.

Existing discussion: [TypeScript](typescript-discussion-20261024.md) and
[esbuild integration](esbuild-noe-fit.md).

## Principles across phases

- Design imported and NOE-specific APIs together as NOE's maintained stdlib.
- Reuse shared backends. Multiple public entry points should call the same
  implementation rather than develop independent behavior.
- Deliver each feature consistently to the CLI, Workers, agents, and Go
  embedding, within the capabilities selected by the host.
- Update type declarations, documentation, and examples with each phase.
- Report concrete compatibility breaks for each work item. Decide with the
  user whether a compatibility path is needed for that item; do not assume
  that existing entry points must always be retained.
- When adopting Node-style APIs, specify supported arguments, results, and
  errors. Preserve the distinctions between Node streams and Web Streams
  where they affect callers.
- Use the filesystem integration to establish a pattern that can be applied
  to process and I/O integration in later phases.
