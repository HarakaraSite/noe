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
