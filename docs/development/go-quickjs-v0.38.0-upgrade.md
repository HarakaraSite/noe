# go-quickjs v0.38.0 upgrade — 2026-11-17

Local integration on top of NOE `ae631a9dd28272b7883cd45f1d35357b02b03446`.
The user requested adopting v0.38.0 and correcting NOE's Windows `os.EOL`
behavior. Existing OpenAI Fast-mode documentation changes are retained.

Published in [NOE v2.27.0](../releases/v2.27.0.md), with both local executables
installed after publication verification. The
[release checkpoint](../releases/release-checkpoints.md#v2270-publication-and-installation--2026-11-17)
records the immutable tag, downloaded assets, published consumers and installation.

## Dependencies and scope

| Dependency | Before | After |
| --- | --- | --- |
| github.com/go-quickjs/go-quickjs | v0.37.1 | v0.38.0 |
| github.com/go-quickjs/go-intl | v0.9.0 | v0.9.0 |

The official engine tag resolves to
`440db6bd2016e682ec4033c10c600609aa8bd02f`, with checksum
`h1:B/4UgWAdZj7Y+B6587zUW8Esq5JiyQdDHbzb9n833+k=`.
Only the engine requirement and its checksums change after `go get` and
`go mod tidy`. Go remains 1.27.2; the upstream go.mod is identical between
v0.37.1 and v0.38.0.

The [upstream release](https://github.com/go-quickjs/go-quickjs/releases/tag/v0.38.0)
includes compiler/parser fixes and interpreter improvements to object spread,
sparse arrays, short string concatenation, and `instanceof`. Intermediate
[v0.37.2](https://github.com/go-quickjs/go-quickjs/releases/tag/v0.37.2) and
[v0.37.3](https://github.com/go-quickjs/go-quickjs/releases/tag/v0.37.3)
are included in this update. No NOE performance claim is made from upstream
benchmarks. The release still uses the stack interpreter; the announced register
interpreter belongs to a later upstream release.

Comparing production root Go files in the two module artifacts finds only a
`WithDebugger` comment change in quickjs.go. Exported Go declarations are
unchanged. Among upstream stdlib production files, only os.go changes. NOE
continues to maintain its own stdlib and module loader; it does not replace
them with upstream stdlib or the `qjs` CLI's module detection.

Runtime and Worker construction keep JIT disabled, without `WithJIT`,
`WithDebugger`, or `WithNodeQuirks`. No engine fork or compatibility adapter is
introduced.

## Windows EOL contract

NOE's `os.EOL` previously returned LF on every platform. It now returns CRLF
(`"\r\n"`) on Windows and LF (`"\n"`) elsewhere, selected from `runtime.GOOS`.
The same value is exported by both `os` and `node:os`, through named and default
exports. Host options, module availability, and the other OS values are unchanged.

This adopts the behavior described by
[Node's os.EOL contract](https://nodejs.org/api/os.html#oseol) and upstream
v0.38.0, using a small change in NOE's existing implementation. It does not
normalize filesystem or stream output automatically.

Windows callers that used `os.EOL` to force LF should instead write an explicit
`"\n"`. No imports or signatures change, and Linux/macOS retain LF. The updated
[built-in reference](../../internal/apidocs/content/stdlib-os.md#eol) and public
TypeScript comments describe this behavior. Repository examples and hidden JS
libraries had no `os.EOL` consumers to migrate; the existing OS test now checks
the actual host expectation and all four export forms.

## Engine behavior checks

`TestEngineCompilerFixes` exercises the fixes through NOE's main runtime,
Worker modules, and a `node:vm` Script executed repeatedly in one realm.
The same expressions were also checked against Node v24.21.0.

An external modfile selects v0.37.1 without changing the checkout dependency;
all eight cases fail under that control. The v0.38.0 cases pass through all
three NOE execution modes.

| Case | v0.37.1 control | Expected and v0.38.0 |
| --- | --- | --- |
| Captured block binding on `continue` | `2,2,2` | `0,1,2` |
| Nested `finally` on `continue` | `a,d,a,d` | `a,b,c,d,a,b,c,d` |
| Completion value across `finally` | `9` | `3` |
| Computed object-rest key | Key evaluated twice | Key evaluated once |
| Optional-chain delete | Property remains | Property removed |
| Multiple labels on a loop | SyntaxError | Loop runs three times |
| Unlabelled break inside a labelled block | Guard detects repeated loop | Loop ends after one iteration |
| Async function after a line break in an array | SyntaxError | Function accepted |

The labelled-break fixture has a bounded guard so the old bug cannot hang the
control run. The older nested-loop integration regression remains covered.

## Verification

Evidence directory: `/var/tmp/noe-quickjs-v0380-kjt3ylch`.

| Check | Result |
| --- | --- |
| Focused engine and OS checks | Passed; compiler cases run in main, Workers, and repeated node:vm Script execution; both OS aliases and named/default exports agree |
| v0.37.1 control and Node reference | All eight engine cases fail against the old engine; all eight expected results match Node v24.21.0 |
| Whole-tree unit and E2E | `GOFLAGS= CGO_ENABLED=0 go test -tags=e2e ./... -count=1 -timeout=10m` passed |
| Vet including E2E sources | `GOFLAGS= go vet -tags=e2e ./...` passed |
| Focused race checks | Passed in internal/stdlib, internal/jsexec, and internal/runtime for engine, OS, realms, Workers, CPU cancellation, and persistent-agent cancellation/queues |
| External stdlib consumer | Checkout and independent module-artifact builds and executions passed; engine v0.38.0 selected |
| External agent consumer | Checkout and independent module-artifact portable/Go-host builds and executions passed; engine v0.38.0 selected |
| Public dependency resolution | Both artifact consumers resolve the published engine without replacements or an engine fork |
| Six CGO-free compilation targets | Passed for Linux, macOS, and Windows on amd64/arm64; each selects `zzjit_disabled.go` and no internal/jit package |
| Development executable | Built successfully; `go version -m` records v0.38.0 and CGO disabled |
| Module integrity | `go mod verify` passed |

The external consumers use `scripts/check-stdlib-consumer.py`, once for stdlib
and once with `--agent`. The six-target check uses `go list -deps -export`
over all NOE packages; it compiles without linking six distribution binaries.
Logs, artifact manifests, the Node reference expressions, the old-engine
control, and compilation results are retained in the evidence directory.

Linux amd64 was executed natively. Windows and macOS were compiled, not run;
the Windows EOL branch still needs a native Windows run for runtime confirmation.
No JIT execution, live model-provider call, long-wait check, or repeated release
test suite was needed for this integration. Reuse the completed verification
while the relevant source and dependencies match.

## Retained state

The source and published v2.27.0 assets select v0.38.0. The development binary
and implementation evidence are retained in the evidence directory. Both
`/home/agent/.local/bin/noe` and `/home/agent/projects/noe/noe` contain the verified
v2.27.0 Linux amd64 release asset. Previous v2.26.1 executables are retained at
`/var/tmp/noe-v2.27.0-install-d1tx5vaw`. Publication and installation are complete;
running services and noebot were not changed.
