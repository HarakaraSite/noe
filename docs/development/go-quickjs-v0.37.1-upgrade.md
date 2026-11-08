# go-quickjs v0.37.1 upgrade — 2026-11-09

Local integration on top of NOE `fc28b2058ffe96ec29c1688a13ff4b4c71378326`.
The user approved adopting upstream standards behavior, retaining disabled JIT,
and correcting NOE bindings where that behavior breaks their contracts.
Publication and installation are not part of this change.

## Dependencies and execution mode

| Dependency | Before | After |
| --- | --- | --- |
| github.com/go-quickjs/go-quickjs | v0.32.0 | v0.37.1 |
| github.com/go-quickjs/go-intl | v0.8.0 | v0.9.0 |

The official engine tag resolves to `42845bad012a67b532b4c93e7f1bbbe6e5c29721`,
with checksum `h1:lpt66w/hQ0h6bUr3KS8Xtt8nKYRRo2fh2JdFr4K/mKE=`.
go-intl resolves to `36bfbee282c3e977cc7542f56598ed5f45ba0b18`, with checksum
`h1:flAmmWD5QL4UfQ/YgeRlUV9UUEnh67ocYw4rlIEIbmo=`. Only these two selected
dependencies change after `go get` and `go mod tidy`. Go remains 1.27.2.

The [upstream comparison](https://github.com/go-quickjs/go-quickjs/compare/v0.32.0...v0.37.1)
leaves public root Go declarations unchanged. Of those production root Go files,
only quickjs.go, program.go and realm.go change, for implementation and comments.
Upstream stdlib production files are unchanged; its two changed files are tests.
NOE retains its own maintained stdlib snapshot and local implementations.
go-intl v0.9.0 changes Temporal parsing signatures, but NOE has no direct calls.

Runtime and Worker construction retain `quickjs.New` without `WithJIT` or
`WithNodeQuirks`. Validation uses empty GOFLAGS and no `quickjs_jit` tag.
All six targets select VM `zzjit_disabled.go` and no internal/jit package.
No new memory-limit setting or engine fork is introduced.

## Behavior and the NOE binding change

The nested-loop bug fixed in
[v0.35.0](https://github.com/go-quickjs/go-quickjs/releases/tag/v0.35.0)
is reproduced in the installed v2.24.0 binary: a nested `for...of` with
`continue` before another `break` test returns `e,e` instead of
`2,3,e,2,3,e`. The new integration test checks the correct result in main,
a Worker, and a repeatedly executed node:vm Script.

The standards changes in
[v0.37.0](https://github.com/go-quickjs/go-quickjs/releases/tag/v0.37.0)
are adopted: Error and fresh DOMException instances inherit stack, iterator
helpers validate closing, out-of-bounds DataView getters return zero, and
Temporal follows the newer annotation and PlainMonthDay rules. Two existing
DOMException test expectations now reflect inherited stack. Reading and cloning
stack traces still works; no Node compatibility option is enabled.

DataView exposed an actual NOE integration regression. On v0.32.0, hashing a
view made out of bounds by shrinking its buffer rejects with TypeError. Merely
updating the dependency made it hash empty bytes instead, since byteOffset and
byteLength now return zero. Web Crypto's BufferSource types do not opt into
[Web IDL AllowResizable](https://webidl.spec.whatwg.org/#AllowResizable).
The shared subtle-method conversion now checks the captured intrinsic resizable
getter and rejects resizable backing explicitly, for ArrayBuffers and views.
It retains rejected-Promise behavior, ordinary fixed-buffer byte spans and valid
empty DataViews. The engine's DataView getters keep their new behavior.

This also closes an older acceptance gap for in-bounds resizable inputs.
Callers using those inputs must copy their intended bytes into a fixed
Uint8Array. The embedded crypto reference documents that migration. The check
is shared by data, raw key material and algorithm BufferSources such as salt.
Tests cover main/Workers, the out-of-bounds case, raw key import, KDF salt,
shadowed resizable properties, fixed empty views and nonzero view offsets.
This slice does not complete the separate general BodyInit/BufferSource work.

## Validation

Evidence: `/var/tmp/noe-quickjs-v0371-4cwggjqx`.

| Check | Result |
| --- | --- |
| Whole-tree non-JIT unit tests | All packages pass using the initial run plus the complete affected stdlib rerun after the binding/expectation corrections |
| Vet including E2E sources | Passed on final code |
| CLI E2E suite | All cases pass using the full run plus the corrected HTTP cases below |
| Focused race checks | Passed for stdlib, jsexec and runtime cancellation, Workers, realms, source maps, diagnostics and Web Crypto |
| External stdlib consumer | Checkout and standalone module-artifact builds/runs pass, including main/Workers; engine v0.37.1 selected |
| External agent consumer | Checkout and standalone module-artifact portable/Go-host builds/runs pass; engine v0.37.1 selected |
| Unmodified jose 6.2.15 | Main and Worker each pass 27 checks; Node interoperability and 46 SDK source fingerprints pass |
| Engine behavior probes | Main and Worker each pass 13 checks for loops, JSON, array isolation, stack/clone, direct iterator closing, DataView and Temporal |
| node:vm timeout and reuse | Passed; timed-out sandbox subsequently produces 42 |
| Production long-turn interruption | CLI 100ms timeout exits 124 at 117ms; Worker termination succeeds and owner subsequently produces 42 |
| Six CGO-free target compilation checks | Passed for linux/darwin/windows, amd64/arm64; JIT disabled on each |
| Module checksum verification | `go mod verify` passed |

The initial E2E failures were two outdated HTTP fixtures, independent of the
engine. Both expected lowercase patch to become PATCH, contradicting the
already approved Request method contract. Both failures reproduce with the
baseline v0.32.0/go-intl v0.8.0 dependencies through an external modfile.
The fixtures now expect caller case for patch; they pass with both dependency
sets. HTTP implementation and the example remain unchanged. The other successful
E2E cases and unit packages are reused rather than rerunning entire suites for
these local corrections.

The initial direct-iterator probe used an Iterator.from wrapper. That wrapper
still ignores a non-callable return in both installed baseline and updated
engines. The final probe checks the release's direct iterator-helper closing
path with Iterator.prototype instead. The unchanged wrapper divergence is
recorded in `iterator-wrapper-baseline.json`; no engine patch is included.

Timing observations are single Linux amd64 runs, not guarantees or performance
benchmarks. The race build disables the engine's production interruption fast
path, so the normal CLI/Worker probes provide separate evidence. Other targets
were compiled but not run natively. No JIT execution or live provider request
was needed. Installed executables remain the verified v2.24.0 assets.
