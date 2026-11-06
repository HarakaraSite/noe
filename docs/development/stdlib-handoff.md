# Standard library session handoff

Updated 2026-11-06 for a session restart. This document records the current
continuation point; the linked policy and implementation records remain the
canonical design and validation references.

## Repository and publication state

- Repository: `/home/agent/projects/noe`, branch `main`.
- This handoff is committed together with the `noe:fs` migration. Its parent
  is `1d044021074768942a6e4a19e8c1d8258356107d`
  (`feat(stdlib): add EventSource and asynchronous SQLite`).
- The latest release recorded in this checkout is v2.15.0. EventSource,
  asynchronous SQLite, and the new filesystem migration are unreleased.
- The user requested a local commit and this handoff. Publishing, pushing,
  releasing, replacing installed executables, and restarting processes are
  not part of this request.
- No implementation work for the process or HTTP naming migrations has
  started. Resume from the next slice below, not from the old phase-1 design.

## User decisions to retain

Read the [standard library API policy](stdlib-api-policy.md) first.

- Web standards and usability take priority over Node compatibility. Web
  APIs retain their standard names and semantics; NOE-specific host APIs
  use explicit `noe:` module names.
- Preserve the shared native backends, selected capabilities, Root/Cwd/
  ReadOnly behavior, and resource ownership. Do not restore duplicate native
  implementations merely to restore convenient method names.
- Explain breaking changes before each new slice. The user decides whether
  compatibility routes are needed for that slice. Do not treat the FS
  decision as authorization to remove all remaining Node modules.
- For this filesystem slice, the user explicitly chose to remove all four
  old imports without adapters and migrate NOE's own callers.
- **noebot is handled independently by the user.** Do not keep investigating,
  editing, or testing it for this work. An earlier read-only audit and
  [migration memo](noebot-noe-fs-migration.md) are available if requested.
- Documentation and explanatory source text are English; user replies may
  be Japanese. Follow repository AGENTS.md, including reuse of completed
  validation when the tested source and dependencies have not changed.

## Completed filesystem slice

See [implementation, migration, and verification](stdlib-noe-fs.md) and the
[embedded API reference](../../internal/apidocs/content/noe-fs.md).

- Only `noe:fs` is registered. `node:fs`, `node:fs/promises`, `fs`, and
  `fs/promises` were removed; no compatibility adapters remain. Bare FS
  names can now be mapped by import maps; `node:`/`noe:` prefixes remain
  reserved independently.
- Async operations are direct exports. Explicit Sync helpers and a default
  namespace object remain; there is no `promises` property or submodule.
- `open(path, {flags = "r", mode = 0o666} = {})` returns the shared FileHandle.
  Positional string flags are rejected.
- `read(length = 16384)` and `readAt(position, length)` return Uint8Array
  data; `write(data)` and `writeAt(position, data)` return byte counts.
  Native write snapshots, partial-I/O errors, close admission/draining,
  shutdown cleanup, stat, sync, and async disposal are retained.
- Non-append positional I/O preserves the cursor. Append-handle `writeAt`
  ignores its supplied position, writes at EOF, and advances the cursor.
  `truncate` rejects negative lengths without changing the file.
- `watch` returns the existing async iterator. The EventEmitter facade and
  its Go hooks were removed. Native registration starts at first `next()`;
  close/return/asyncDispose and existing AbortSignal behavior remain.
- Async `chmod` pairs with `chmodSync`, including its existing integer
  conversion and permission mask. No new range validation was added.
- Examples, hidden `.noe/lib` code, embedded examples/instructions, tests,
  public Go consumers, TypeScript declarations, and current references were
  migrated. Historical phase and release records retain their old contracts.
- Native file/watch backends, public Go configuration fields, and
  dependencies did not change. Stream helpers still open files synchronously.

## Validation and evidence

Validation is complete for this slice. Local evidence:
`/var/tmp/noe-fs-policy-aj2_zwmi`.

- `source-manifest.json`: checked source hashes before the commit handoff.
  The commit adds documentation only after that verified snapshot.
- `verification.json` and individual command JSON/log files: full
  `internal/stdlib` unit tests; affected integration packages; targeted native
  and public-profile race checks; selected CLI/Worker/fixture-agent E2E;
  final watch and standalone-docs E2E; six offline libraries; Go vet and lint.
- `consumer/`: public stdlib checkout and standalone module-artifact
  consumers ran with CGO disabled and compiled for all six release targets.
  Public agentlib portable and Go-host consumers also passed.
- TypeScript positive/negative fixtures and full public stdlib/API catalog
  tests passed. Initial integration failures were stale import-map/docs
  expectations; the affected gates were corrected and passed. Do not treat
  the earlier failed log as an unresolved runtime failure.
- Independent bounded review found no runtime blocker. Three documentation
  mismatches were fixed and confirmed in one change-only re-review.

The candidate CLI is `/var/tmp/noe-fs-policy-aj2_zwmi/noe-final`; it is not
installed. Its runtime code matches the checked implementation; some bundled
reference wording was corrected afterward. Build a fresh temporary candidate
if the next slice needs the latest bundled docs. Do not use `./noe` or the
installed executable as evidence for source-only APIs without rebuilding.

Reuse these results for unchanged code. Run checks appropriate to the next
actual modification; do not repeat all unit/E2E/race checks for a commit or
release instruction alone.

## Next slice: process and standard I/O

1. Read the policy, then inspect the current `node:child_process`/bare
   `child_process` and `node:process`/bare `process` surfaces. They expose
   NOE's Promise/Web Streams behavior. Global process metadata also exists.
2. Propose a coherent `noe:process` contract for commands, live children,
   standard I/O, and metadata. Exact grouping and legacy entrance removal
   are still undecided. Keep ProcessInfo, Stdio, and Commands capability
   selection independent, and preserve main/Worker and borrowed-I/O rules.
3. Explain import, signature, return-shape, or global changes before
   implementation and obtain the slice's compatibility decision. The user
   expects progress toward this direction but has not approved a specific
   process migration contract yet.
4. Migrate NOE's callers, public types, references, and consumers alongside
   implementation. Leave noebot to its owner.

After process/I/O, review host HTTP/listener/upgrade helpers and nonstandard
extensions. Keep standard global `fetch` and Web APIs. Remaining Node
utilities and globals are demand-driven work, not a wholesale rename.

## Other already completed or deferred work

- EventSource is a standard global with reconnection/cancellation and shared
  Fetch ownership; see [implementation](stdlib-eventsource.md).
- SQLite uses the approved asynchronous `noe:sqlite` API; see
  [implementation and limitations](stdlib-sqlite.md). The filesystem naming
  change does not require a SQLite redesign.
- Web Crypto expansion and WebSocketStream remain use-case driven. The
  broader [roadmap](stdlib-roadmap.md) still applies under the newer policy.
- Separate known improvement candidate, outside this completed slice:
  Fetch's configured response-body limit currently presents a successful
  prefix rather than rejecting an oversized body read. An earlier local
  probe is `/var/tmp/noe-api-direction-7odu19wj/probe.go`. This has not been
  fixed or included in the FS migration; assess it as its own work item.
