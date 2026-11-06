# Source maps in runtime diagnostics

[Development guide](README.md) · [Standard library roadmap](stdlib-roadmap.md)

2026-11-06, based on filesystem-watch commit `10a8f70`. Phase 6 connects the
pinned engine's source-map support to NOE-owned VMs. The user approved enabling
it by default after being told that mapped Error stacks change displayed source
names and positions. Implemented and locally verified; included in [v2.15.0](../releases/v2.15.0.md).

## Behavior and compatibility

Owned `stdlib.NewRuntime` and `stdlib.Execute` enable source maps, covering the
CLI, managed task/run_js executions, and their Workers. Runtime Error stacks
can display original file names, function names, and line/column positions.
Structured diagnostic stacks and Worker error transfer retain that mapping
after the originating VM closes. Native Go causes, exit codes, diagnostic
classification, messages/details, and result budgets retain their contracts.

Maps are linked by `sourceMappingURL`. The engine parses/maps/caches them lazily
when the stack is written; no second source-map parser is maintained in NOE.
No engine fork, new dependency, CLI flag, or JavaScript module is introduced.
The source-map implementation comes from upstream go-quickjs v0.32.0; NOE owns
the selected-filesystem reader and execution/Worker wiring.

| Map or script form | Behavior |
| --- | --- |
| Inline application/json data URL | Base64 and percent-encoded forms work without FS. |
| Local relative sidecar | Read through the selected filesystem; resolve relative to the script file URL, then use the map file URL as the map's base. |
| Local script ID | Canonical absolute paths and local file URLs support sidecars, including volume-free virtual Root paths on Windows. |
| Root and ReadOnly | Initial path/link policy and frozen Cwd apply. ReadOnly permits observation. Physical root paths are not inserted into virtual labels. |
| Remote or opaque script ID | Inline maps work; external sidecars are unsupported. |
| Absolute map URL or protocol-relative target | Unsupported except data URLs; no HTTP or UNC retrieval is added. Native drive-absolute paths on Windows remain subject to the selected FS. |
| Missing, unreadable, malformed, or unsupported map | Preserve generated-code stack positions without replacing the program exception. |
| Original source files | Never fetched; sources/sourceRoot are labels and sourcesContent is map data. |

For example, a generated `dist/bundle.js:3:8` frame may become
`src/original.ts:6:1`, with the original function name. This is the approved
compatibility change for code that compares stack strings. Ordinary code with
no usable map retains its generated positions. `Error.prepareStackTrace` still
receives generated CallSites, matching the engine's behavior. Syntax-error
Position remains the generated position supplied by the parser; NOE does not
infer positions by parsing stack text. TypeScript execution remains a separate
future feature.

## VM and capability boundaries

The owned constructor normalizes the Profile once before creating the VM.
The same prepared environment/backend then feeds Attach, avoiding duplicate
Root resolution and environment snapshots. The map reader has an execution
context and only gets a filesystem view when FS was selected.

Owned Worker construction uses an additive internal `NewWithContext` factory,
receiving the actual child lifetime. Existing explicit `Workers.New` takes
precedence. The worker inherits the frozen filesystem policy; no VM registry
or global mutable map cache is needed. Runtime/owned-scope cleanup cancels map
reads, and Worker context/VM closure ends the worker reader's lifetime.

`stdlib.Attach` preserves caller-owned VM options. An advanced caller enables
`quickjs.WithSourceMaps` when constructing its VM and can delegate to the new
`Bindings.ReadSourceMap(script, reference)` method. The method follows that
view's context/filesystem and refuses closed bindings. See the
[Go embedding setup](../go-embedding.md#source-maps-in-diagnostics).

The reader performs no ambient file read, cwd lookup, module load, or network
request. HTTP module-import authority does not implicitly authorize a sidecar
fetch. Returned map bytes and labels are handled by the engine; reading an
Error stack does not execute source text or fetch its original sources.

## Verification

Validation passed:

- Full affected units for internal jsexec/stdlib/noestdlib/runtime/CLI/agentexec,
  the API catalog, and public stdlib/agent. Separate consumer checks covered
  their external-consumer units rather than run the same checks twice.
- Focused race checks for the reader, owned runtime mapping, public borrowed
  VM setup, and HTTP imports without extra map requests.
- Inline and local sidecar CLI run/eval, Worker error transfer, and event
  recording, including escaped Unicode/space/#/% map filenames. A fixture
  agent received the mapped structured stack from a failed run_js and then
  completed its response. No real provider requests were needed.
- Reader tests for Base64/percent data, subdirectory map bases, nil FS, Root,
  ReadOnly, outward links, unavailable/missing maps, closed views, and context
  cancellation. Runtime tests include invalid-map fallback, custom
  prepareStackTrace, initializer exceptions, and preserved native Go causes.
- Separate public stdlib and agent consumers with both local replacement and
  self-contained module artifacts built and ran; agent ran portable/gohost.
  The stdlib consumer checks mapping without FS and built for all six targets.
- Vet including E2E, builtin `docs("globals", "Error.stack")`, documentation
  links, Go formatting, and whitespace.
- Independent bounded review and one re-review; no actionable findings remain.

Review found that a decoded mixed slash/backslash prefix could be interpreted
as UNC on Windows. The reader now normalizes decoded Windows separators before
rejecting that form, with fake-FS no-read regressions. The Windows test binary
cross-compiled. Native Windows execution was not available; native runtime
checks were Linux amd64, and other release targets received build verification.

Evidence is retained at `/tmp/noe-source-maps-qy4kygxm/results.json`, including
source hashes, full unit/race/E2E/vet logs, consumer artifact manifests and
build logs, and builtin documentation output. After the checks, only the Worker
constructor comment was clarified; token comparison confirms the runtime Go
source is identical to both tested consumer artifacts. No runtime change
followed validation. Installed executables, actual noebot repositories, and
running services were unchanged.

Primary references: the pinned engine's `sourcemaps.go` and
`sourcemaps_test.go`, [ECMA-426](https://tc39.es/ecma426/), and the official
[Node CLI source-map behavior](https://nodejs.org/api/cli.html#--enable-source-maps).
The engine's supported map format and fallback behavior govern this feature;
full Node module/source-map inspection APIs are outside this phase.
