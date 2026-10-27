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

The initial snapshot import did not intentionally change HTTP behavior, body
limits, timeouts, module names, exports, or protocols. Subsequent NOE-local
changes should be recorded here with their rationale and validation. Upstream
updates are selected and reviewed explicitly; this directory is not an
automatic mirror.

## Maintenance notes

NOE maintains this standard library independently. Fixes can be made locally
without waiting for an upstream release. Shared defects and useful fixes may
also be communicated upstream when an opportunity arises.

### Open: mkdtemp discards the prefix directory (2026-10-26)

Both `node:fs.mkdtempSync` and `node:fs/promises.mkdtemp` call `fsHost.mkdtemp`
in `fs.go`. It passes `filepath.Base(prefix) + "*"` to `os.MkdirTemp`, dropping
the directory from the supplied prefix. Without `FSConfig.Root`, creation
therefore uses the OS temporary directory; with a root, it uses the root
directory rather than the requested subdirectory.

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

Local correction is pending. Preserve the prefix directory while honoring
NOE's filesystem root and write permissions, and cover both synchronous and
Promise APIs. Share the reproduction or fix upstream when convenient;
upstream acceptance is not a prerequisite for a NOE fix.

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
