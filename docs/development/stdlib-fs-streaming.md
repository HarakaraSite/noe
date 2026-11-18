# Bounded-memory filesystem operations

Recorded 2026-11-18. The user selected copyFile memory reduction and directory
iteration after comparison with txiki.js. This is an additive local stdlib
implementation, with verification and bounded review complete. Installed
executables remain v2.29.0; no commit or release is
included in this slice. Web Crypto and byte streams/BYOB were initially
[notes only](stdlib-roadmap.md#deferred-candidates--2026-11-18); the user later
selected the separate [HMAC/JWK follow-up](stdlib-webcrypto.md#hmac-generation-and-jwk--2026-11-18).
BYOB remains deferred.

## Copying

The former copyFile implementation read the entire source before writing the
destination. Both copyFile and copyFileSync now share a bounded-memory helper:

- Read a fixed-size prefix before touching the destination, preserving initial
  source-error behavior, including directory-read failure.
- Open the destination without truncation, then compare the two opened file
  identities. A same-file alias leaves contents intact, including hard links
  and resolved symbolic links.
- Truncate regular destinations after that comparison, write the prefix, and
  use Go's native copying where available with a 32 KiB fallback buffer.
  Special writable targets retain their operating-system behavior.
- Preserve existing destination permissions; create with mode 0644 subject
  to umask. Both entry points retain the shared Root/Cwd/ReadOnly resolution
  and filesystem error encoding.

Copying remains non-atomic. A mid-copy I/O failure may leave a partial target;
concurrent changes to source contents are not a snapshot. There is no new
copy option or cancellation API.

The focused native allocation probe copies a 64 MiB regular file and records
33,656 allocated Go bytes in both normal and race runs on this host. This is
memory evidence for that fixture, not a platform-independent throughput claim.
The previous whole-file allocation exceeds the probe's 8 MiB ceiling.

## Directory iteration

`noe:fs.iterateDir(path, {batchSize = 128, signal} = {})` immediately returns
a lazy DirectoryIterator of Dirent values. It opens asynchronously on the first
next request and reads at most one bounded batch at a time using os.File.ReadDir.
Consumed entries are removed from that batch. No global sort or recursive walk
is performed. Existing readdir/readdirSync keep sorted-array behavior.

The JavaScript iterator owns serialization, its current batch, and signal
observation. Concurrent next calls are serialized. Native cursors reuse the
existing fileHost/fileHandle for worker admission, native close-before-join,
descriptor ownership, runtime cleanup, and execution-scope cleanup. Closing
the shared loop context now also closes idle descriptors through the same
Go-only host shutdown path.

EOF, return, break, throw, close, async disposal, cancellation, read failure,
and owner shutdown close native resources. Explicit close stops future yields,
settles in-flight next calls as done, and waits for admitted work and any late
open to release its descriptor. AbortSignal cancellation delivers the exact
reason to a pending or first subsequent next call, once; later reads are done.
The existing signal-combination primitive validates genuine signals and ignores
synthetic abort events. A pre-aborted iterator opens nothing.

Each Dirent uses the same constructor and predicates as existing listings;
entry symlinks are not followed. Read-only profiles can iterate. Paths resolve
with the shared Root/Cwd policy, including Worker profiles. Entries are in
filesystem order and may reflect concurrent changes. batchSize bounds entry
count, not filename bytes or OS buffers. An iterator cannot continue yielding
an old buffered batch after its execution owner has ended. Arbitrary OS file
calls may take time to finish during cleanup.

No migration is required for existing callers. Public Go function signatures
and dependencies are unchanged. The [API reference](../../internal/apidocs/content/noe-fs.md#iteratedir),
TypeScript declarations, embedded export inventory, independent Go consumer,
and [example](../../examples/low-level/list-directory.js) describe the addition.

## Verification

Focused native and JavaScript fixtures exercise batch bounds, lazy open,
Dirent identity/types, concurrent next, EOF/break/disposal, explicit close
during opening/reading, exact abort reasons, fake/synthetic signals, ordinary
errors, Root/Cwd/ReadOnly behavior, owner cleanup, and copied-file aliases/modes.
The public consumer exercises directory iteration and copying in main and
Worker runtimes. Final whole-tree and external-artifact verification is
recorded in the [session handoff](stdlib-handoff.md#current-continuation--filesystem-streaming).

## Reference

txiki.js was inspected at c13adc667e3e7761ff4d0c12a6ea34efceb4e1ea. Its
[native filesystem adapter](https://github.com/saghul/txiki.js/blob/c13adc667e3e7761ff4d0c12a6ea34efceb4e1ea/src/mod_fs.c)
uses native copying and directory handles; those patterns inform this slice.
NOE retains its pure-Go implementation and its existing resource/policy model.

## Verification

Whole-tree unit/E2E, race checks for internal/stdlib and public stdlib, vet,
TypeScript positive/negative fixtures, and JS/TS lint pass. A temporary CLI
runs the directory example and exposes the new embedded member references.
Checkout and standalone-artifact Go consumers exercise copying and directory
iteration in main and Worker runtimes without publication or new dependencies.

Focused native fixtures check batch bounds, EOF/read-error cleanup, idle owner
teardown, interruption of an in-flight read, and disposal of a late open.
JavaScript fixtures check lazy opening, Dirent identity/type, concurrent next,
early break, async disposal, pending explicit close, real/pre/synthetic aborts,
large safe-integer batch input, and Root/Cwd/read-only behavior. Copy tests
cover overwrite/create modes, same-file aliases, initial failures, special
targets, public async/sync entry points, and bounded allocation.

An independent bounded review covered both paths and the shared host lifetime
change, reporting no actionable findings. Evidence and final source hashes:
`/var/tmp/noe-fs-streaming-u5ir24u_`. No source/dependency changes follow the
recorded broad checks; final edits update documentation only. Installed
executables and running services remain unchanged.
