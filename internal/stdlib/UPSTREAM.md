# NOE standard library snapshot

This is a source snapshot of the go-quickjs standard library, maintained inside
NOE. The JavaScript engine remains the upstream Go dependency. NOE's production
runtime, Workers, and managed task/run_js processes now install this snapshot.
Runtime adoption preserves the imported API surface and configuration; its
wiring lives in internal/runtime and the integration test helpers.

NOE also adds http.go: Install registers `noe:http` using the existing fetch and
serve function values enabled by its configuration. It adds no transport,
constructors, or permissions; the upstream globals and node:http remain available.
The added http_test.go covers this module's registration and capability boundaries.

## Origin and license

- Upstream module: `github.com/go-quickjs/go-quickjs`
- Version: `v0.27.0`
- Commit: `bef3107cc7eeb5e392cdd9a3768f64fcbf0e609c`
- Module checksum: `h1:TVemuGmYmaR+4CCNIVIcHk3dhrqYRq+PGljCkT1EqAE=`
- Source: <https://github.com/go-quickjs/go-quickjs/tree/v0.27.0>
- Import date: 2026-10-19
- License: MIT. `LICENSE` is an unchanged copy of the upstream root license,
  including its copyright notices.

The version, commit and checksum were checked against the Go module cache's
origin metadata, its downloaded module hash and NOE's `go.sum`.

## Source mapping

| Upstream path | Local path | Imported contents |
| --- | --- | --- |
| `stdlib/` | `internal/stdlib/` | All 26 production Go files, 9 Go test files and 3 testdata files listed below |
| `internal/wsproto/` | `internal/stdlib/internal/wsproto/` | `wsproto.go` and `wsproto_test.go`, unchanged |
| `LICENSE` | `internal/stdlib/LICENSE` | Complete upstream license, unchanged |

The Go files include embedded JavaScript source. The testdata includes the
recorded Node path corpus and its generator; tests use the recorded data and do
not require Node.js or regeneration.

Imported files relative to upstream `stdlib/` and this directory:

```text
async_hooks.go
cleanup.go
compress.go
console.go
crypto.go
dns.go
exec.go
extras.go
fetch.go
fs.go
fsstream.go
loop.go
messaging.go
node.go
os.go
path.go
process.go
serve.go
stdlib.go
streams.go
timers.go
vm.go
webapis.go
websocket.go
worker.go
workerscript.go
async_hooks_test.go
broadcast_internal_test.go
dns_test.go
lifetime_test.go
messaging_test.go
path_test.go
stdlib_test.go
vm_test.go
worker_test.go
testdata/path_node.json
testdata/pathcorpus.js
testdata/pathcorpus_node.js
```

## Local changes

1. Eight external-package test files import `noe/internal/stdlib` instead of
   the upstream standard library: `async_hooks_test.go`, `dns_test.go`,
   `lifetime_test.go`, `messaging_test.go`, `path_test.go`, `stdlib_test.go`,
   `vm_test.go` and `worker_test.go`. The internal test package is unchanged.
2. `serve.go` and `websocket.go` import the local `internal/wsproto` snapshot.
   Its protocol implementation and tests are unchanged.
3. `console.go` no longer imports the engine's private `internal/hostaccess`.
   Its `evalInternal` helper calls the public `Runtime.EvalFile(name, src)`.
   The host closure, source names, clone brands and other library code remain
   unchanged. The helper comment describes this adjustment.
4. `eval_internal_test.go` is a NOE test for the public evaluation adapter's
   result, Promise-job processing and error-source behavior.
5. This provenance document is a NOE addition.
6. The Fetch implementation is locally adapted for NOE's event loop. In a
   runtime with a Loop, outgoing ReadableStream bodies are written
   incrementally from Uint8Array chunks instead of being collected in full.
   The Request path preserves one-shot body ownership, and stream bodies are
   not replayed when a 307/308 redirect would require resending them. A final
   response may stop an unfinished upload while leaving the response readable.
   Hosts that install Fetch without a Loop retain the synchronous finite-body
   path; they do not get the same incremental upload behavior. These are NOE
   local changes, not upstream behavior.
   The Go upload bridge lives in fetch_upload.go. streams.go records whether
   a readable stream has been read or cancelled so that releasing a reader
   cannot make a consumed Request body reusable. fetch_upload_test.go and
   runtime/CLI integration tests cover these changes.
7. The HTTP server's receive path is a NOE-local adaptation. It dispatches a
   Web Request before the body reaches EOF and exposes the Go request body as
   a demand-driven ReadableStream with a zero high-water mark and a bounded
   native read buffer. `Serve.MaxBodyBytes` retains its existing behavior:
   zero selects the 32 MiB default, a positive value exposes that prefix with
   normal EOF, and a negative value exposes an empty body. The original
   `Content-Length` is not rewritten. Body reads remain available while a
   returned Response stream is being sent, then outstanding input is stopped
   and cleaned up before the Go handler returns. For HTTP/1, the bridge uses
   public HTTP response/connection controls to avoid waiting for an unread
   body before sending a response and to release blocked reads during
   cleanup; it does not depend on net/http private fields or unsafe access.
   Accepted `Expect: 100-continue` requests receive an explicit interim
   response before body reads, and NOE's response pump waits for final headers
   to flush before requesting a response-stream chunk. Graceful server
   shutdown retains its five-second bound and force-closes remaining
   connections afterward. These are NOE-local changes, not upstream behavior.

8. Worker capability rebinding copies DNS configuration onto the child's own
   Loop. This prevents asynchronous DNS results from being delivered through
   the parent's Loop and keeps the parent's configuration unchanged. A local
   fake-resolver regression test covers ownership without external DNS.

9. Synchronous child commands (`execFileSync` and the shell-based `execSync`)
   now throw an Error carrying `code`, `stdout`, and `stderr`, like the
   asynchronous path. Their success result and error output remain strings,
   and existing error messages, capture limits, and start-failure code
   conventions are unchanged. Runtime shutdown still interrupts execution.
   `exec_test.go` covers success/options, nonzero exits, empty and capped
   output, start refusals/failures, and host-configured timeouts. Runtime
   diagnostic tests also verify that sync failure details reach agent
   tool results and events. This is a NOE-local change, not upstream behavior.

10. Rooted filesystem operations that act on a directory entry now retain
    the final symbolic link instead of operating on its target. This applies
    to lstat, readlink, unlink/rmdir, rm, and both sides of rename. Existing
    checks that reject resolved targets outside the configured root remain
    in place; stat, reads, writes, and unrooted operation retain their prior
    link-following behavior. fs_symlink_test.go covers synchronous and Promise
    APIs, linked parents, target preservation, ReadOnly, and outward-link
    rejection. This fixes entry selection, not the separate nonexistent-path
    confinement issue or filesystem races. It is a NOE-local change.

11. `mkdtempSync` and `fs/promises.mkdtemp` preserve the supplied prefix's
    parent directory. Relative prefixes use the configured filesystem cwd;
    a trailing separator creates a randomly named directory inside that
    directory. The parent is resolved through the shared filesystem backend,
    preserving root confinement and ReadOnly enforcement. Returned paths keep
    the existing host-absolute or root-virtual form. `fs_mkdtemp_test.go`
    covers both APIs, rooted and unrooted prefixes, relative paths, trailing
    separators, missing/non-directory parents, and symlink/write policies.
    This is a NOE-local fix; upstream issue filing is deferred.

12. `Loop.Run` and `Loop.RunUntil` reuse one lazily created Go wait timer per
    call. Previously, each wait created another timer and deferred its Stop
    until the entire call returned, retaining timers during long runs. The
    timer is now stopped after each wait and on every return. JavaScript
    timer registration, ordering, callback execution, cancellation, and loop
    lifetime retain their existing contracts. `loop_timer_test.go` covers
    deadline changes after host wakeups, timer-free waiting, and later runs
    using virtual time, and benchmarks allocation and live-heap growth.
    This is a NOE-local optimization.

13. ReadableStream data and pending-read queues advance a head index instead
    of using Array.shift for each item. Consumed references are cleared
    immediately; queues compact only after at least half their entries have
    been consumed and reset when empty. Close/error detach pending requests
    before settling them without copying the entire array. FIFO order, chunk
    identity, size accounting, backpressure, and terminal states retain their
    existing contracts. `streams_queue_test.go` covers draining/refilling,
    zero-size/undefined chunks, a slow tee reader, and pending reads on close,
    cancellation, and error. This is a NOE-local optimization.

The initial snapshot import did not intentionally change HTTP behavior, body
limits, timeouts, module names, exports, or protocols. Subsequent NOE-local
changes should be recorded here with their rationale and validation. Upstream
updates are selected and reviewed explicitly; this directory is not an
automatic mirror.

## Maintenance notes

NOE maintains this standard library independently. Fixes can be made locally
without waiting for an upstream release. Shared defects and useful fixes may
also be communicated upstream when an opportunity arises.

### Fixed locally: mkdtemp discards the prefix directory (2026-10-30)

Reported on 2026-10-26: both `node:fs.mkdtempSync` and
`node:fs/promises.mkdtemp` call `fsHost.mkdtemp` in `fs.go`. It previously passed
`filepath.Base(prefix) + "*"` to `os.MkdirTemp`, dropping the directory from the
supplied prefix. Without a filesystem root, creation therefore used the OS
temporary directory; with a root, it used the root directory rather than the
requested subdirectory.

Reproduced through NOE's `run_js`, with the prefix's parent directory present:

```text
prefix: /tmp/workflow-cli-prefix-check-3470230562/nested/run-
result: /tmp/run-746059527
```

[Node's contract](https://nodejs.org/api/fs.html#fsmkdtempprefix-options-callback)
appends a random suffix to the supplied prefix, retaining its directory.
The reproduced result violates that contract and places caller artifacts
outside the requested directory.

The same implementation exists in the imported v0.27.0 and in
[upstream v0.29.0 / main as checked on 2026-10-26](https://github.com/go-quickjs/go-quickjs/blob/44ddbaa01bcc4fa431e3e95978a18cb9ee4f21fc/stdlib/fs.go#L509-L526).
Updating the engine dependency alone does not change this local copy.

The NOE-local correction splits the prefix before path resolution, resolves
its parent using the filesystem backend, and appends the random suffix only
to the final name component. Both synchronous and Promise APIs are covered by
regression tests, including filesystem root and write permissions. Upstream
issue filing is deferred; upstream acceptance is not a prerequisite for this
NOE fix.

### Upstream PR candidate: loop wait timer reuse (2026-11-05)

**Status:** implemented and validated locally in NOE; not yet released.
An upstream issue or PR has not been created. This note records a
possible future contribution, as requested by the user.

**Upstream scope and cause.** Both `Loop.Run` and `Loop.RunUntil` originate in
the imported v0.27.0 stdlib. The same per-wait `time.NewTimer` followed by
`defer timer.Stop()` remains in
[upstream v0.32.0 Run](https://github.com/go-quickjs/go-quickjs/blob/v0.32.0/stdlib/loop.go#L231)
and [RunUntil](https://github.com/go-quickjs/go-quickjs/blob/v0.32.0/stdlib/loop.go#L294).
When host work repeatedly wakes a loop with a future JavaScript timer armed,
each wait adds a Go timer and a deferred call. Those references remain live
until the enclosing Run/RunUntil returns, even after individual timers expire.
This produces growing live memory during one long run; it is not a claim of
permanent retention after the call returns. The engine dependency itself is
unchanged by this stdlib correction.

**Proposed contribution.** Reuse one lazily created wait timer local to each
Run/RunUntil call, stop it after each wait, and use one deferred stop for all
return paths. Preserve JavaScript timer ordering, pending-work behavior,
context cancellation, completion-channel handling, and reuse of the loop
across calls. NOE's implementation is in [loop.go](loop.go); its behavior
regression and benchmark are in [loop_timer_test.go](loop_timer_test.go).

This local optimization was measured against NOE commit
`10b90dd4aba92ac1167c598d01ed1a61209b652d`, with go-quickjs v0.32.0 and JIT
disabled. `BenchmarkLoopTimerWake` repeatedly posts host work while a distant
JavaScript timer keeps the loop waiting. It samples live heap before Run or
RunUntil returns, while the old deferred Stop calls still retain their timers.

On Linux amd64, Go 1.27.2, Intel Core i9-13900HX, GOMAXPROCS=1, medians of
three 5,000-post runs were:

| Path | Allocated bytes/post, before -> after | Allocations/post, before -> after | Time/post, before -> after |
| --- | --- | --- | --- |
| Run | 415 -> 104 | 8 -> 4 | 1,342 ns -> 991 ns |
| RunUntil | 415 -> 104 | 8 -> 4 | 1,336 ns -> 1,053 ns |

Additional 1,000- and 10,000-post measurements confirmed that the old live
heap grew by approximately 311 bytes/post, while the new net live-heap change
stayed constant at -3,624 bytes. This is a process-heap delta that includes
reclaimed setup objects, not a timer-only byte count. These measurements
describe this host-wakeup workload, not end-to-end application speed.

The virtual-time regression passed for both Run and RunUntil on the old and
new implementations: host wakeup brings a deadline forward, the loop waits
without timers between callbacks, another timer fires, and a later Run works.
The combined whole-tree unit/local CLI E2E suite passed, including external Go
consumers. Full vet and focused loop/timer/Worker/cancellation/Close race
checks across internal/stdlib, internal/jsexec, and public stdlib also passed.
Raw benchmark and validation logs are retained at
`/tmp/noe-loop-timer-noLO95`. Dependencies, public APIs, and JavaScript timer
queue behavior are unchanged. This work does not publish or install a release.

**Before preparing an upstream PR:** inspect the then-current upstream main
and existing issues/PRs, and reproduce the allocation/retention measurements
using upstream's own stdlib without NOE's Profile/execution-scope extensions.
The figures above measure NOE's maintained copy, not a separately executed
upstream build. Port the fix and focused tests against upstream's actual
source and supported Go versions. At v0.32.0 upstream declares Go 1.24.0,
whereas NOE uses Go 1.27.2 and its new regression uses `testing/synctest.Test`
(added in Go 1.25); adapt that test for the supported minimum rather than
copying it unchanged or silently raising upstream's Go requirement. Current
upstream main, duplicate reports, and Go 1.24 execution have not been checked.

### ReadableStream queue indexing (2026-11-05)

The data queue and pending-read queue previously called Array.shift for every
item. In go-quickjs v0.32.0 that operation moves the remaining elements on
each call, so draining a long queue takes quadratic element-moving work.
Both shift sites are also present in
[upstream v0.32.0 streams.go](https://github.com/go-quickjs/go-quickjs/blob/v0.32.0/stdlib/streams.go).
NOE now advances head indices, clears consumed references immediately, and
compacts after at least half the array has been consumed. Empty queues reset
their indices. Close/error detach the pending-read array before settling
requests, preserving the previous ordering without copying the array first.
Chunk bytes and object identity are preserved.

`BenchmarkStreamsQueue` measures complete public stream operations: buffering
chunks before reading, or queuing read promises before producing chunks. Each
batch verifies FIFO values and EOF. On Linux amd64, Go 1.27.2, Intel Core
i9-13900HX, JIT disabled and GOMAXPROCS=1, medians of three 100 ms runs were:

| Path / chunks per batch | Time/batch, before -> after | Allocated bytes/batch, before -> after |
| --- | --- | --- |
| Buffered / 1 | 16.42 us -> 16.34 us | 10,854 -> 10,774 |
| Buffered / 64 | 197.1 us -> 148.1 us | 68,589 -> 70,129 |
| Buffered / 1,024 | 11.17 ms -> 2.14 ms | 986,768 -> 992,677 |
| Pending reads / 1 | 21.25 us -> 21.27 us | 14,387 -> 14,310 |
| Pending reads / 64 | 299.9 us -> 272.6 us | 176,495 -> 178,097 |
| Pending reads / 1,024 | 12.96 ms -> 4.01 ms | 2,759,681 -> 2,732,228 |

Long-queue elapsed time decreases substantially; single-chunk results remain
approximately level. Compaction adds some reference-array allocation: about
2.2% for buffered batches of 64 and 0.6% for buffered batches of 1,024. This is
not an across-the-board allocation reduction or a measurement of end-to-end
HTTP/file throughput. The baseline stream source matches NOE commit
`10b90dd4aba92ac1167c598d01ed1a61209b652d`; the existing local timer improvement
was preserved throughout.

The new tests cover draining/refilling queues, chunk identity and size
accounting, zero-size/undefined values, a slow tee branch, reader locking,
and close/cancel/error after partially servicing pending reads. Whole-tree
unit/local CLI E2E, including external Go consumers, passed. Full vet,
embedded-JavaScript Biome lint, and focused stream/HTTP/Worker/cancellation/
Close race checks across internal/stdlib, internal/jsexec, and public stdlib
passed. Raw results are retained at `/tmp/noe-stream-queue-FVRPcD`.

The change and benchmark are in [streams.go](streams.go) and
[streams_queue_test.go](streams_queue_test.go). These are NOE-local changes;
current upstream main, duplicate reports, and an upstream-only benchmark have
not been checked. No upstream issue or PR has been submitted.

## Evaluation and debugger difference

Upstream evaluates its own scripts with debugger instrumentation disabled,
using a private callback tied to its engine internals. The snapshot uses the
public evaluation API and follows the runtime's debugger setting instead.
Both paths use the same execution and Promise-job processing in this engine
version. Normal NOE and Worker runtimes use `quickjs.New()` without a debugger;
that is the compatibility scope of this import.

With `quickjs.WithDebugger()`, the snapshot's internal library scripts may be
visible to and instrumented by the debugger. Upstream's behavior of hiding
those scripts is not preserved. Debugger-enabled runtimes are not rejected.
This difference must be considered before offering the library to external
hosts. No engine fork or private-access workaround is included.
