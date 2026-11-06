# Asynchronous SQLite

[Roadmap](stdlib-roadmap.md) · [API reference](../../internal/apidocs/content/noe-sqlite.md)

2026-11-06. Phase 10 introduces SQLite through the NOE-specific asynchronous
`noe:sqlite` module. The user selected a Promise API after comparing the
synchronous Node API with the needs of NOE's event loop and long-lived services.
Implementation and local verification are complete; it is not yet released.

## API and execution model

`open(path = ":memory:", {readOnly, busyTimeout, signal})` returns a database.
Its exec/run/get/all/prepare/transaction/close methods are asynchronous.
Prepared statements have run/get/all/close methods; databases and statements
support Symbol.asyncDispose. There is no synchronous Node-style facade or
compatibility adapter. Public Go option layouts are unchanged.

Memory databases perform no filesystem access. File databases use the existing
FS capability and selected Root, Cwd, and ReadOnly. The module is installed
with the base Web APIs when a Loop is present. Workers inherit that selection
and create their own connections; handles are not transferable. Idle handles
do not independently hold the event loop alive.

The JS facade snapshots SQL parameter data and orders submissions on a
connection queue. Native SQL work runs in Go away from the VM owner. Only
plain Go request/result data crosses that boundary; promises and typed-array
results are created on the owner. Cancelling an operation can release its
caller immediately, while the queue waits for actual native completion before
starting later work. Execution close cancels, joins and closes resources
without needing pending JavaScript callbacks to run.

Transactions reserve the entire queue for their callback, including awaits.
The callback receives its own tx methods. Other submissions on the database
wait until commit or rollback completes. A callback throw, failed transaction
operation, or cancellation rolls back before releasing the reservation.
Statements created inside the transaction belong to its scope and are
finalized automatically. Manual transaction SQL and nested transactions are
outside this first slice.

Parameters use positional arrays or named records. Native execution resets
and clears prepared bindings for each invocation. SQLite integers retain
their signed 64-bit value: safe results become numbers, larger results become
bigints; unsafe integral number inputs require bigint. BLOBs are copied at
submission and returned as Uint8Array. Row objects have null prototypes.

## Driver selection

The selected driver is
[`github.com/ncruces/go-sqlite3` v0.36.0](https://github.com/ncruces/go-sqlite3/tree/v0.36.0).
It uses SQLite translated through wasm2go, supports CGO-free builds, and
provides direct statement/column/binding APIs plus a Go VFS. This version does
not require the older Wazero execution model. Its
[VFS contract](https://github.com/ncruces/go-sqlite3/blob/v0.36.0/vfs/README.md)
supports the six NOE release targets, which are checked separately below.

The isolated comparison also tested modernc.org/sqlite v1.61.1. Both passed
CGO-disabled memory/file persistence, UTF-8/BLOB/int64, cancellation, and
post-cancellation reuse probes. One exploratory Linux amd64 run inserted
10,000 rows in about 13 ms for each driver; this is not a NOE API benchmark.
Minimal stripped probe binaries were 7,491,744 bytes for modernc and
8,265,888 bytes for ncruces, versus 1,216,672 bytes for an empty program.
Those sizes are standalone probes, not the increase to the NOE executable.
The direct API and scoped VFS integration determine the selection.

The comparison source and module are under `/var/tmp/noe-sqlite-w4mhlx_e/`.
No SQLite dependency was added to NOE during that comparison; the selected
dependency was added only after the asynchronous API decision.

## Filesystem authority

Opening only the main database path is insufficient. The probe confirmed
that a read-only main connection can ATTACH and write another database.
SQLite can also open journal/WAL files and a shared-memory index. The scoped
VFS therefore validates path operations against the selected filesystem.
Temporary files use memory, and filesystem-directory PRAGMAs are refused.

ATTACH and VACUUM (including INTO) are outside the first slice. Arbitrary SQL connection
URIs could select a different VFS, so only the host-generated VFS URI is used
for the approved filename. Native extension loading and JavaScript SQL
callbacks are also outside this implementation.

Read-only filesystem policy also accounts for WAL's -shm index: the driver's
ordinary shared-memory implementation can create/write that index even when
the main connection is read-only. FS.ReadOnly profiles therefore reject a
WAL-format database at open with ERR_READ_ONLY before SQLite can create a
sidecar, and their VFS file wrapper suppresses default shared memory. Ordinary
rollback-journal read-only databases work. Writable-filesystem WAL, including
a readOnly:true connection there, uses the original locking/shared-memory
implementation. No actively changing database is silently marked immutable.

## Compatibility and validation

Existing APIs and callers do not migrate. SQLite adds a new asynchronous
module and a CGO-free dependency. Persistent localStorage and sessionStorage
are separate candidates and are not bundled into the SQL API work.

The following implementation checks passed:

- Focused public SQLite tests and race checks for memory and file databases,
  typed values and snapshots, named/positional binding, prepared reuse,
  queue reservation, callback/SQL failure rollback, cancellation while queued
  and executing, Worker use, execution shutdown, and asynchronous disposal.
- Native VFS and host checks for selected Cwd/Root, symlink rejection, memory
  without FS, exact read-only WAL header detection, side-effect-free
  rollback-journal reads, and rollback after SQLite's automatic rollback.
- Existing affected-package unit checks for maintained/public stdlib,
  execution bindings, runtime, and API catalog. SQLite focused tests and
  external consumer wrappers were excluded from the integration rerun because
  they were checked separately. The new reference initially omitted the
  catalog's required explicit export inventory; adding it resolved the sole
  runtime documentation-contract failure in a targeted rerun.
- CLI run/eval, Worker, fixture-agent run_js, and persistence between two
  independent executions. These use local fixtures and no paid providers.
- External public Go consumer with a checkout replacement and a standalone
  module artifact, including the new async module under a no-FS profile.
- Native test compilation for all six release targets. Linux amd64 candidate
  and public consumer also build/run with CGO_ENABLED=0; the other five target
  test binaries record CGO_ENABLED=0. Native execution was on Linux only.
- TypeScript positive/negative fixtures, affected-package vet, Go formatting,
  relative documentation links, and whitespace checks.
- Independent review and one fix-only re-review. The confirmed transaction
  issue was SQLite's INSERT OR ROLLBACK returning to autocommit before a
  callback's later SQL ran. Transaction queues now stop after their first
  failure, while finalization remains possible; native rollback is a no-op
  after an automatic rollback. Regressions cover later submissions and
  already-queued SQL, original error preservation, and connection reuse.
  The re-review reported no remaining actionable findings.

In a Linux amd64 CGO-disabled stripped build, the CLI measured 56,250,528 bytes,
versus 51,159,200 bytes with only the SQLite implementation disabled through a
temporary Go build overlay (identical documentation). That is about 5.1 MB,
or 10%, for SQLite. Both builds include the pending EventSource work. These
are implementation measurements, not a new release or installed executable;
later documentation-only edits can change the byte count slightly.

Evidence, comparison sources, build metadata and the independent module
artifact are under `/var/tmp/noe-sqlite-w4mhlx_e/`. Cross-compiled test binaries
are `/tmp/noe-sqlite-stdlib-{os}-{arch}.test` (with `.exe` on Windows).
No commit, release, installed binary replacement, service restart, noebot
migration, or upstream request was performed as part of this slice.
