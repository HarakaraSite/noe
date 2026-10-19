# NOE standard library snapshot

This is a source snapshot of the go-quickjs standard library, maintained inside
NOE. The JavaScript engine remains the upstream Go dependency. This package is
currently tested independently; NOE's production runtime still installs the
upstream standard library. Runtime migration is a later step.

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

No HTTP behavior, body limit, timeout, module name, export or protocol behavior
is intentionally changed in this import. Future changes should be recorded
here with their rationale and validation. Upstream updates are selected and
reviewed explicitly; this directory is not an automatic mirror.

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
