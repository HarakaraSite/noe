# Filesystem API integration: phase 2

[Roadmap](stdlib-roadmap.md) · [Phase 1](stdlib-fs-integration.md) · [FileHandle reference](../../internal/apidocs/content/stdlib-fs-promises.md)

2026-11-05. Phase 2 was implemented and locally verified from base `e4368d2`,
then released in [v2.13.0](../releases/v2.13.0.md). Publication and local
placement are tracked in the [release checkpoints](../releases/release-checkpoints.md).
Together with phase 1, this completes the first filesystem milestone.

## Implemented operations

- `open(path, flags, mode)` supports `r`, `r+`, `w`, `wx`, `w+`, `wx+`, `a`,
  `ax`, `a+`, and `ax+`. Creation permissions default to `0o666`, subject to
  umask; explicit modes range from `0` to `0o777`. Append handles write at the
  end even if a position is supplied. Numeric flags and synchronous variants
  such as `rs` are outside this slice.
- `FileHandle.stat()` uses the open descriptor and shares the path-stat
  conversion. Renaming or replacing the original pathname does not redirect
  the operation to another file.
- `FileHandle.truncate(length = 0)` shrinks or extends the file without moving
  its cursor. Negative safe integers mean zero. `sync()` flushes the open
  descriptor through the operating system.
- The new operations use the existing I/O admission and cleanup mechanism.
  Normal close drains admitted operations; all operations reject after close
  begins. Shutdown retains forced cleanup.
- `readdir` and `readdirSync` accept `{withFileTypes: true}` and return
  `node:fs.Dirent` instances. Entries contain `name`, `parentPath`, and seven
  file-type predicates. The default remains a sorted array of names.

The shared backend now resolves exclusive creation against the final
directory entry, avoiding creation through a dangling symlink. Root and
ReadOnly policies also apply to the new flags. A shared read-only error
sentinel preserves the existing message and enables consistent classification.

The public declarations, current filesystem reference, public Go consumer,
and CLI fixtures are updated for these operations.

## Approved error and metadata contract

On 2026-11-05, the user approved both compatibility changes without adapters.
Filesystem OS errors now expose symbolic string `code` values such as
`ENOENT`, `EEXIST`, and `EBADF`; `errno`, when available, retains the original
positive platform OS number. The numeric values vary by platform. Filesystem
errors may also expose `path`, `dest`, and `syscall` from the underlying
operation, with paths projected relative to the configured root. The custom
codes `ERR_INVALID_ARG`, `ERR_READ_ONLY`, `ERR_FILE_CLOSED`, and fallback
`ERR_IO` remain. Partial FileHandle failures retain their byte counts and
original buffers. Existing callers comparing numeric filesystem `code` values
must use the symbolic code and may inspect `errno` for the platform number.

Synchronous functions, Promise functions, FileHandle operations, and file Web
Streams use one error conversion. JavaScript exceptions raised by caller
getters retain their identity, and uncaught native failures retain their Go
cause for `errors.Is`. Windows native filesystem codes are classified before
the POSIX fallback; unknown failures retain `ERR_IO` and any available errno.

FileInfo now exposes `mtime` as a JavaScript Date and `mtimeMs` as epoch
milliseconds, which may retain fractional milliseconds; `mtime.getTime()`
truncates those fractions. `mode` contains canonical POSIX file-type,
permission, and special bits on every platform. Existing permission checks
must use `mode & 0o777`.

Use `info.mtime.toISOString()` for a textual timestamp or `mtimeMs` for a
numeric value. Date/JSON timestamps have millisecond precision; `mtimeMs`
may retain a fractional millisecond. The supported FileInfo remains a subset
of Node Stats: no BigInt metadata, additional timestamps, or Stats constructor
was introduced.

The remote noebot `main` was checked again and still resolved to
`45de74771d9c048448afb1e7f1039b0b45a01443`. No affected timestamp or numeric
filesystem-code comparisons were found in the inspected examples, hidden
libraries, or noebot sources. noebot checks modes through `& 0o777`; its
`lstatMaybe` already recognizes `ENOENT` and otherwise uses a directory-list
fallback. This audit covers those consumers, not arbitrary external scripts.

## Validation

The finalized contracts, including the approved breaking changes, passed:

- Full affected `internal/filesystem` and `internal/stdlib` unit tests,
  public `stdlib`, runtime integration, and API catalog tests.
- Targeted race tests for filesystem errors and metadata, new handle methods,
  close admission and cleanup, Dirent, and exclusive creation. New tests cover
  code/errno across synchronous, Promise, handle, and stream entry points;
  Date and mode parity; JS exception identity; and Go cause retention.
- CLI filesystem and low-level I/O E2E tests, including run/eval, Workers,
  event output, fixture agent `run_js`, cancellation, and cleanup. The new
  execution-mode fixtures assert normalized errors and metadata as well as
  handle/Dirent behavior.
- The public stdlib consumer built and ran using both a local replacement
  and a module artifact. The artifact also built for Linux, Darwin, and
  Windows on amd64/arm64. These are cross-builds, not runtime tests on the
  other operating systems.
- All six offline `.noe/lib` suites. Remote noebot S1 31/31, S2 83/83,
  S3 45/45, and images 11/11 with the final temporary CLI build. No consumer
  source changes were needed.
- TypeScript positive/negative fixtures, changed JavaScript lint,
  affected-package vet, and whitespace checks.
- Independent read-only runtime review. Common Windows mapping gaps found
  during review were corrected; no actionable findings remained. Windows
  mapping behavior received source review and compilation only.

Detailed local consumer, library, and noebot results are under
`/tmp/noe-fs-phase2-0N5iae/`. Final consumer artifacts use the
`normalized-consumer` directory; library/noebot results use the `normalized-`
prefix, and the final CLI is `noe-final`. Installed binaries and running
applications were not replaced or restarted. No paid provider calls were made.

The semantics were checked against the official Node.js documentation for
[filesystem flags and FileHandle operations](https://nodejs.org/api/fs.html)
and [system errors](https://nodejs.org/api/errors.html). These references guide
the supported subset; they do not imply implementation of the full Node API.

## Deferred noebot API adoption

Recorded on 2026-11-05 for a later consolidated instruction in the noebot
repository. The remote `main` was rechecked at
`45de74771d9c048448afb1e7f1039b0b45a01443`. Implementation is deferred to
noebot-side work; no noebot sources were changed for this assessment.

The existing code passed the 170 checks recorded above without migration.
These are optional improvements, with no measured performance gains yet.

| Candidate | Current noebot code | Benefit and recommended scope |
| --- | --- | --- |
| Notes directory traversal | `app/notes.mjs`, `Notes.list()`: name-only `readdirSync`, followed by `lstatSync` for every entry. | Use `readdirSync(dir, {withFileTypes: true})` and Dirent predicates to avoid the separate per-entry metadata call. This is the smallest adoption candidate. Preserve symlink rejection, note-name validation, temporary-directory exclusion, and sorted results. Keep the path checks used for reads and writes. |
| Durable persistence | `app/store.mjs`, `syncPath()` and `atomicWriteFile()`; used by Store, Notes, and image storage. A normal atomic write invokes GNU `sync -f` before rename and again for the parent directory after rename. | FileHandle-based writes and `sync()` can remove external command launches and narrow the synchronization scope. This has the largest potential benefit, but requires a persistence redesign rather than a local API replacement. |
| Missing-file handling | `app/store.mjs`, `lstatMaybe()`: an `ENOENT` fast path followed by a parent-listing fallback for older NOE errors. | NOE v2.13.0 already activates the existing fast path without code changes. Remove the fallback and update the outdated error comment only when older NOE support is dropped. Cleanup alone adds little performance benefit on v2.13.0. |

### Persistence work to define in noebot

`FileHandle.sync()` is asynchronous. `Store.transaction()` currently completes
synchronously and rejects asynchronous callbacks; `syncNow()` also rejects
thenable sync adapters. A future change must address caller completion,
write serialization, initialization, and handle cleanup together.

Preserve the current atomic-publication guarantees: finish writing and syncing
the temporary file before rename, then sync the destination's parent directory
before reporting durable success. A file-only sync does not replace the
post-rename directory sync. Preserve private file/directory permissions and
the existing distinction between failures before publication and uncertain
commits after a rename attempt, including the stop-writes behavior.

GNU [`sync -f`](https://www.gnu.org/s/coreutils/manual/html_node/sync-invocation.html)
synchronizes the filesystem containing the path; NOE's FileHandle `sync()`
operates on the open descriptor. Check directory-handle syncing on noebot's
Linux target as part of that redesign. Remove the GNU sync startup prerequisite
and documentation only when all persistence callers have migrated.

### Compatibility decisions for later work

noebot currently documents and enforces NOE >= 2.11.0 in `README.md` and
`app/main.mjs`. Adopting the new APIs requires a decision on raising that
minimum to v2.13.0 or providing an older-version path. Explain that change
when proposing each work item, as requested by the user. Asynchronous
persistence also changes internal Store/Notes caller contracts and their
injected filesystem/sync test adapters; identify the affected callers before
implementation.

Suggested sequence: adopt Dirent for Notes as a small change, then plan the
larger persistence change separately. Include missing-file fallback cleanup
when the minimum-version decision makes it appropriate. Reuse the existing
S1, S2, S3, and image suites and their persistence-failure cases for the
affected changes.

### Required process import migration for the next NOE release

Later on 2026-11-05, the user approved moving the Promise/Web Streams
`spawn` into `node:child_process` and removing `noe:process` without an
adapter. Apply this migration when noebot adopts the NOE release containing
that change, before the optional filesystem improvements above.

At the inspected noebot revision, change the imports in `app/main.mjs` and
`tests/s1.mjs`:

```js
import {spawn} from 'node:child_process';
```

Continue awaiting `spawn` and using its existing stream options and
`exited`, `kill()`, and `close()` methods. This remains NOE's Promise/Web
Streams API; Node-style ChildProcess events are not introduced. The matching
embedded reference is now `docs('node:child_process', 'spawn')`.

Update noebot's documented and enforced minimum NOE version in `README.md`
and `app/main.mjs` to the release that ships this change. That version is
not assigned yet; published NOE v2.13.0 still uses `noe:process` and does not
export the new `spawn` entry point. The filesystem-only minimum-version
decision above is separate from this required process migration.

Validation used a temporary copy of the remote source at the same revision.
The unmodified S1 suite failed to import the removed module, as expected.
Changing only those two imports passed S1 31/31, S2 83/83, S3 45/45, and
images 11/11 on the candidate NOE binary. The patch and logs are under
`/tmp/noe-process-integration-863auo4a/`, including
`noebot-spawn-import.patch`. The actual noebot repositories and running
applications were not changed.

### Standard-I/O audit for the following slice

The next approved NOE slice replaces `noe:io` and synchronous `process.std*`
facades with byte Web Streams from `node:process`, using standard `pipeTo`
instead of the native `pipe` helper. A source audit at the same remote noebot
revision found no use of those APIs in its application or test sources.
No additional noebot edits were identified for standard I/O. Keep the required
two spawn-import changes above in the consolidated noebot instruction.

The completed standard-I/O candidate was also checked against a temporary
copy containing only those two spawn-import changes: S1 31/31, S2 83/83,
S3 45/45, and images 11/11 passed. The remote `main` still resolved to the
same revision at the end of the audit. Logs and the import patch are under
`/tmp/noe-stdio-integration-dkaizbjk/`. The real noebot repositories remain
unchanged.

### Cancellation and disposal additions

Phase 4 adds `AbortSignal.any()` and asynchronous disposal without requiring
new noebot imports or rewrites. Its temporary copy passed the original 170
checks plus Telegram commands 20, typing 13, and Web timing 5 with only the
previous two spawn-import changes. No signal dispatch override was found.
Evidence is under `/tmp/noe-cancellation-cleanup-v90s154r/`.

Keep existing transport semantics when considering optional adoption:
`app/telegram.mjs` distinguishes caller stop from deadline expiry, includes
response-body work in the deadline, and clears its timer and listener after
completion. Combining signals alone does not replace that adapter. The host
launcher's existing explicit child cleanup is also valid; await using is an
optional way to express scoped ownership rather than a required migration.
