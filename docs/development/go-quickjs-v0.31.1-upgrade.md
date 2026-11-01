# go-quickjs v0.31.1 upgrade — 2026-11-01

The local checkout updates the engine from v0.29.0 to v0.31.1 with JIT disabled,
as explicitly requested. Baseline NOE commit: `2ffebde`.
This record concerns source integration and validation, not publication or
replacement of installed executables.

## Dependency and API scope

| Dependency | Before | After |
| --- | --- | --- |
| github.com/go-quickjs/go-quickjs | v0.29.0 | v0.31.1 |
| github.com/go-quickjs/go-intl | v0.8.0 | v0.8.0 |

The selected engine tag points to
`2583b4d1f6de59f3be45f00a2713971c9a86fb88`, with checksum
`h1:tWrA5q3/vTDiYh9MRJPzSpAMPdpwje+uCffD2bpVgJY=`.
Upstream's go.mod is byte-identical between the two versions. NOE retains
Go 1.27.2 and the other selected dependencies; only the engine requirement
and its checksum entries change after go get and tidy.

The [upstream comparison](https://github.com/go-quickjs/go-quickjs/compare/v0.29.0...v0.31.1)
contains 169 commits across 251 files. The public root API signatures used by
NOE are unchanged; value.go, program.go and realm.go are unchanged. No NOE
production adapter change is needed. Upstream stdlib/ is unchanged across
these tags. NOE's maintained internal/stdlib snapshot and its local fixes
retain their existing source and v0.27.0 provenance.

The [v0.31.1 release](https://github.com/go-quickjs/go-quickjs/releases/tag/v0.31.1)
improves interpreter execution of class/property/constant patterns and reports
no API or behavior changes relative to v0.31.0. The intervening
[v0.30.0](https://github.com/go-quickjs/go-quickjs/releases/tag/v0.30.0)
and [v0.31.0](https://github.com/go-quickjs/go-quickjs/releases/tag/v0.31.0)
also change BigInt limits/error formatting, stack-frame naming and constructor
semantics, fix a dictionary-prototype crash and improve cancellation detection.
API stability therefore does not imply identical diagnostics or execution
behavior across the whole v0.29.0-to-v0.31.1 update. Upstream benchmark results
are not a measurement of NOE or noebot performance.

## JIT remains disabled

NOE's runtime and Worker constructors continue to use `quickjs.New()` without
`WithJIT`. No build tag or release workflow is changed. Verification commands
use empty GOFLAGS and omit the `quickjs_jit` tag. The dependency graph for the
native CLI selects VM `zzjit_disabled.go` and no internal/jit packages.
The ordinary production interrupt watcher is selected, rather than its race
build variant. The six-target compilation record also checks that the
disabled JIT file is selected for each target.

## Validation

Evidence directory: `/tmp/noe-quickjs-v0311-zgxw87c3`.

| Check | Result |
| --- | --- |
| Whole-tree non-JIT unit tests | Passed |
| Vet including E2E sources | Passed |
| Local CLI E2E | Original run passed all except the pre-existing instruction-sensitive fixture described below; corrected case passed on both engine versions |
| External local/artifact agent consumer | Passed, portable and gohost modes; engine v0.31.1 selected |
| Ordinary CLI deadline during long loop turns | Passed, 100ms deadline, observed wall time 106.45ms and expected exit 124 |
| Worker termination during long loop turns | Passed; observed termination 0.63ms after the request and owner continued |
| node:vm timeout followed by reuse | Passed, same sandbox produced 42 after catching timeout |
| Focused lifecycle race checks | Passed; four affected runtime/stdlib packages |
| Six CGO-free distribution-target compile checks | Passed; JIT disabled on all six, no distribution artifacts built |

Cancellation probes use ten-million-character numeric coercion loop turns,
which exercise the new interrupt path more directly than a short empty loop.
These are individual Linux amd64 observations, not timing guarantees or
benchmarks. They make no provider requests.

The initial E2E run failed
`TestContextTrimmingQueuedTaskContinuesConversationAndTrimState` because a
third queued task performed an additional trim. A focused baseline run with
v0.29.0 reproduced the same counts and budget values. The fixture loaded
the user's common external instructions despite expecting fixed trim counts
under built-in facts. Its CLI invocation now includes `--no-inst`; the
unchanged assertions pass with both v0.29.0 and v0.31.1. User instruction
files and production instruction behavior are unchanged. The successful
remaining E2E cases are reused instead of repeating the full suite for this
isolated fixture correction.

## Review and limits

Independent review focused on the new asynchronous interruption watcher,
context restoration, owner-thread Close and NOE's cancellation integration.
It found no actionable integration defect. The reviewer identified the need
for a normal-build long-turn cancellation probe, which was then completed.

Upstream intentionally writes scalar VM interrupt budgets from a watcher;
owner-side execution still checks its current context/abort state. The race
build disables that production fast path. A race pass alone therefore does
not validate the new production interruption behavior; the ordinary CLI and
Worker probes provide separate evidence. Review and these tests do not prove
the absence of all engine defects.

No JIT build or execution, live provider calls, broad performance benchmark,
native execution on other OS/architectures, release, push or installation is
part of this integration. Installed executables and running noebot processes
are preserved. Future release preparation can reuse these checks while source
and dependencies remain unchanged.
