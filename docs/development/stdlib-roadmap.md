# Standard library integration and expansion roadmap

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
| **11. Expand networking** | Add raw TLS and UDP sockets, plus HTTPS listeners for serve. | Applications can use additional network protocols and terminate TLS directly. |
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

- [node:fs](../../internal/apidocs/content/stdlib-fs.md)
- [node:fs/promises](../../internal/apidocs/content/stdlib-fs-promises.md)
- [Shared filesystem backend](../../internal/filesystem/filesystem.go)
- [Profile installation](../../internal/noestdlib/noestdlib.go)

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
