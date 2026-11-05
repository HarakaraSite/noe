# Filesystem API integration: phase 1

[Roadmap](stdlib-roadmap.md) · [Development guide](README.md) · [FileHandle reference](../../internal/apidocs/content/stdlib-fs-promises.md)

2026-11-05. Implemented and locally verified; released in
[v2.13.0](../releases/v2.13.0.md). The user approved
removing `noe:fs-handle` without a compatibility adapter after the consumer
audit recorded in the roadmap. The working tree is based on
`7b7df5750b006913f16f3182729efeee311978cb`.

## Result

`node:fs/promises.open(path, flags = "r")` returns one FileHandle API for
sequential and positional reads and writes. The bare `fs/promises` alias,
named/default promise exports, and `node:fs.promises.open` expose the same
function. Existing-file flags `r` and `r+` are supported in this phase.

Reads accept an ArrayBufferView, including DataView, and return
`{bytesRead, buffer}` with the original view. The options form can allocate
a default 16 KiB Uint8Array. Writes accept views or UTF-8 strings and return
`{bytesWritten, buffer}` with the original input. View offsets and lengths
are byte ranges. Omitted/null positions use the file cursor; explicit safe
integer positions leave that cursor unchanged. Write data is copied on the
runtime owner before asynchronous work starts.

The implementation now lives in `internal/stdlib/filehandle.go` and
`filehandle.js`, installed with the standard filesystem API. The duplicate
native filesystem module and its separate capability-selection path were
removed. Root, working-directory resolution, and ReadOnly use the shared
filesystem backend, as do the existing filesystem entry points.

The filesystem installation owns handles and workers. Open handles do not
keep a runtime alive. Normal `close()` rejects subsequent I/O immediately
and waits for already admitted operations before closing the file. Execution
or runtime cleanup closes the OS resource before joining workers, including
when a graceful close is already waiting. Late opens are owned or disposed
before JavaScript delivery; cleanup never waits for promise callbacks.

## Compatibility changes

| Previous API | Current API |
| --- | --- |
| `import {open} from "noe:fs-handle"` | Import from `node:fs/promises` or its standard aliases. The old module and docs topic are removed. |
| `open(path, {mode: "r+"})` | `open(path, "r+")`. |
| `readAt(position, length)` with `{data, bytesRead, eof}` | `read(buffer, {position, length})` with `{bytesRead, buffer}`. There is no `eof` field; the caller uses the returned byte count. |
| `writeAt(position, bytes)` returning a number | `write(bytes, {position})` returning `{bytesWritten, buffer}`. |
| Partial read errors expose `data` | Partial bytes are copied into the caller's buffer; errors expose that original `buffer` and `bytesRead`. Partial write errors expose `buffer` and `bytesWritten`. |
| User close first closes the OS file and then joins pending I/O | User close drains admitted I/O before closing; shutdown retains forced cleanup. |

The low-level example, public Go consumer, profile and runtime fixtures,
CLI E2E tests, public TypeScript declarations, and current documentation
were migrated together. Historical release and development records retain
their original contracts. Argument and policy errors, numeric OS errno,
and `ERR_IO` retain their phase 1 code conventions.

## Validation

| Check | Result |
| --- | --- |
| Full affected core packages: `internal/stdlib`, `internal/nativeio`, `internal/noestdlib`, `internal/jsexec` | Passed. |
| Public `stdlib`, runtime integration, and API catalog tests | Passed. Covers Root/ReadOnly, outward symlinks, generated agent tool code using the reference, and removal of the former module/topic. |
| FileHandle race tests | Passed. Covers admission, graceful and forced close, late opens, partial errors, writes copied before mutation, idle handles, and scope cleanup with a borrowed runtime. |
| FileHandle public contract tests | Passed. Covers aliases, sequential/positional cursor behavior, view ranges and identity, short/zero reads, validation, missing files, and operation without Web APIs or a Loop. |
| CLI `TestLowLevel*` E2E tests | Passed, including run/eval, Workers, event output, agent `run_js`, cancellation, and resource cleanup. |
| Migrated low-level example | Passed over TCP and Unix sockets with the expected byte update. |
| External stdlib consumer | Local replacement and module artifact both built and ran; the artifact built for Linux, Darwin, and Windows on amd64/arm64. |
| `.noe/lib` | All six offline suites passed: files, find, search, repo-map, exa, and workflow. |
| Remote noebot at `45de74771d9c048448afb1e7f1039b0b45a01443` | S1 31/31, S2 83/83, S3 45/45, images 11/11 passed using a temporary CLI build. No consumer changes were needed. |
| TypeScript positive/negative fixtures, changed JS lint, affected-package `go vet`, and diff whitespace checks | Passed. |
| Independent read-only review | No remaining actionable findings in handle lifetime, worker admission, close ordering, policy enforcement, or buffer contracts. |

Temporary binaries, consumer artifacts, and detailed CLI/library/noebot logs
are under `/tmp/noe-fs-phase1-IJeRd4/`. The remote source audit checkout is
`/tmp/noe-fs-consumers-sHAjJa/noebot`. These are local verification artifacts,
not build inputs. The installed executable and running applications were
not replaced or restarted. No paid provider calls were made.

## Remaining milestone work

Phase 2 adds broader open flags, stat/truncate/sync, Dirent/withFileTypes,
and error normalization. Those changes need their own compatibility
assessment. noebot's synchronous persistence and its `sync -f` fallback
remain a separate consumer contract; asynchronous FileHandle support alone
does not replace it.
