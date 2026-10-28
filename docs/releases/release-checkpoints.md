# NOE release checkpoints

[README](../../README.md) · [Development guide](../development/README.md) · [Release notes](README.md) · [Release profile](../../.forgejo/release-profile.yml) · [Release workflow](../../.forgejo/workflows/release.yml)

The release contract is stored in `.forgejo/release-profile.yml`. Its `version: 2` is a schema version; the first application release is `v1.0.0`.

The `v1.0.0` evidence below refers to the listed release commits. It predates the [stdlib migration](../archive/increment-12.md) and does not validate or distribute the stdlib migration. Release-specific checks are recorded separately. See the migration record for local checks and the [API reference](../api.md) for current behavior.

## Distribution gate

Release work follows **local changes → tests → release verification → light CI**.
Necessary heavy source tests finish before release preparation. Reuse valid
implementation evidence for unchanged source and dependencies. Release
verification is normally light and expands only for actual affected behavior.
The commands below are a reference toolbox, not a mandatory repeated full suite.

CI checks out the selected tag, builds the six distribution binaries with CGO
disabled, creates SHA256SUMS and uploads them. It does not repeat local source
tests, E2E, race or vet. Manual dispatch can publish an existing selected tag;
the uploader uses the checked-out source commit rather than the workflow's
branch commit.

The uploader creates a non-draft Release within the existing repository visibility; it does not make a private repository public. Failed gates or builds must leave their output in native Actions logs and must not publish Release assets.

The `v1.0.0`, `v1.1.0`, `v1.2.0`, `v1.3.0`, `v1.3.1`, `v1.3.2`, `v1.3.3`, `v1.4.0`, and `v1.5.0` Releases have been published through this workflow; see their validation records below. A successful cross-build verifies compilation; runtime and terminal behavior on macOS, Windows, and arm64 have not been tested here.

## Reference checks before tagging

Use existing implementation results first. Select additional commands below only when actual changes require them; record the checked source and reason. The reference host is VM201 (Linux amd64, Go 1.27.2, C toolchain):

```sh
CGO_ENABLED=1 go test -race ./internal/... ./stdlib/... ./agent/... -count=1
go test -tags=e2e ./... -count=1
go test -tags=slow_integration ./agent -run '^TestGoClientLongModelAndToolWait$' -count=1
go vet -tags=e2e ./...
(
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  GOOS=${target%/*} GOARCH=${target#*/} CGO_ENABLED=0 go list -export ./... >/dev/null || exit
done
)
```

These are reference-environment checks, not generated workflow steps. E2E includes the actual CLI, local HTTP and file operations, agents, and cancellation/cleanup. REPL and its PTY checks were removed after v1.8.0; the historical evidence below retains the checks performed for those releases. Race requires a C toolchain. No system-package installation is added to the release workflow to run these checks.

The six-target check uses `go list -export ./...` to compile every package, including main packages, and their dependencies into Go's build cache without linking or producing distribution binaries. Unlike this check, `go build ./...` still links main packages in temporary storage even when it does not save the results. The loop stops on the first failure and returns its nonzero status; the subshell prevents a pasted command from exiting an interactive shell. This is compilation evidence only, intended to catch platform-specific compile errors before tagging. Linking and distribution binary generation happen in the tag-triggered workflow (see Distribution gate). Do not build the six distribution artifacts in the reference environment.

If the reference environment's temporary directory is small or nearly full, create a directory on a filesystem with space (`mkdir -p /var/tmp/noe-gotmp`) before setting `go env -w GOTMPDIR=/var/tmp/noe-gotmp`. The compile-only check still uses temporary compilation space and the build cache. Linking during normal builds or tests needs more temporary space than the final binary size, and an ENOSPC during linking otherwise looks like a source failure.

Before the first authorized v2 publication, also run the external consumer checks
in the [Go embedding guide](../go-embedding.md). After the actual v2 tag and
module are published, create a clean module outside the checkout, run
`go get forge.harakara.site/littleisland/noe/v2/stdlib@<published-v2-tag>`, and
build and run the example without a local replacement or synthetic proxy. Record
the selected module and engine versions. Local artifact checks do not establish
that the published module can be downloaded.

## v1.0.0 preparation — 2026-10-12

Code revision checked: `cba739ef9f6de6dee4c0b866bf95aefa93a7f634`, before adding the release profile and this document. Local environment: VM201, Linux amd64, Go 1.27.2.

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./...` | Passed |
| `CGO_ENABLED=0 go vet ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | Passed |
| `go test -tags=e2e ./... -count=1` | Passed, including Linux PTY checks |
| `go vet -tags=e2e ./...` | Passed |
| All six target builds with `CGO_ENABLED=0`, `-trimpath`, and `-s -w -X noe/internal/cli.Version=v1.0.0` | Passed |
| Built Linux amd64 binary: `--version` | `noe v1.0.0` |

Forgejo environment checks:

- The instance at `https://forge.harakara.site` reports `16.0.3+gitea-1.22.0`.
- `littleisland/noe` has Actions enabled. Its visible runner is `shirushi-runner`, with the `ubuntu-latest` label.
- The same instance's successful [hayami release run](https://forge.harakara.site/littleisland/hayami/actions/runs/44) and its workflow confirm the checkout/setup-go/release action references and Release context expressions used in the profile.
- Native log retrieval returned HTTP 200 for `/api/v1/repos/littleisland/hayami/actions/runs/133/logs` (ZIP) and `/api/v1/repos/littleisland/hayami/actions/jobs/153/logs` (text).
- Go's [download metadata](https://go.dev/dl/?mode=json&include=all) includes the Go 1.27.2 Linux amd64 archive required by setup-go.

At profile preparation, NOE had no Actions runs, so Go 1.27.2 setup and Release upload in NOE's own workflow were still unverified. No secret value is stored in the profile, and no additional PAT secret is configured.

The static profile defines gates and commands; it does not assert that later revisions have passed. Add a new evidence entry for subsequent releases or changes to the checked code or build configuration.

## Workflow generation — 2026-10-12

`.forgejo/workflows/release.yml` was rendered from the confirmed profile using the `apply-forgejo-go-release-profile` skill template. All placeholders were replaced. Reference checks remain outside the workflow, and the Release upload runs only after successful gates and builds.

| Local workflow validation | Result |
| --- | --- |
| YAML structure and correspondence with the profile | Passed |
| `actionlint` | Passed with only the unsupported `forgejo` context diagnostic excluded |
| Temporary copy with `forgejo.*` replaced by equivalent `github.*` for expression linting | Passed; the actual workflow retains `forgejo.*` |
| `bash -n` for every `run` step | Passed |
| All `run` steps executed in a temporary source copy, with the tag expression resolved to `v1.0.0` | Passed: unit tests, vet, and all six builds |
| Exact artifact list, `SHA256SUMS` verification, and Linux amd64 `--version` | Passed; version is `noe v1.0.0` |

Checkout, setup-go, and Release upload require the actual Forgejo run and were not executed by this local validation. Shellcheck is not installed in the reference environment; shell validation used Bash syntax checks and execution of the generated commands.

## v1.0.0 Release validation — 2026-10-12

- Annotated tag: `v1.0.0`, pointing to `e3f59cb219aca2880ea5db052a6188048282e4a9`.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/1): succeeded (API run ID 179, job ID 200).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.0.0): published, neither draft nor prerelease.

The actual runner set up Go 1.27.2, passed the CGO-free unit tests and vet gate, built all six targets, and uploaded the binaries and `SHA256SUMS` with the automatic workflow token. No additional Release secret was needed.

After publication, all seven assets were downloaded from the Release. The six binary hashes matched the published `SHA256SUMS`. The downloaded Linux amd64 binary returned `noe v1.0.0` for `--version` and `2` for `eval 'console.log(1 + 1)'`, both with exit code 0 and empty stderr. This verifies the distributed Linux amd64 binary in addition to the earlier local and CI builds.

The remote tag was checked against the workflow's commit SHA. This validation record is committed after publication; it does not move or replace the release tag.

## v1.1.0 preparation — 2026-10-16

Checked source revision: `3195e83ae525ace96f6204c0495d74e5c8a2411f`. The checks ran against the same source immediately before that commit. This preparation entry is a documentation-only follow-up; the release tag includes it without changing the checked application code. Environment: Linux amd64, Go 1.27.2, with a C toolchain for race and Linux PTY support.

This release enables the full pinned go-quickjs stdlib and removes the `noe` JavaScript global. Existing scripts require migration; see the [v1.1.0 release notes](v1.1.0.md) and [API migration table](../api.md#旧apiからの移行).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go vet ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | Passed |
| `go test -tags=e2e ./... -count=1` | Passed, including Linux PTY checks |
| `go vet -tags=e2e ./...` | Passed |
| All six profile targets, CGO disabled, `-trimpath`, `-s -w -X noe/internal/cli.Version=v1.1.0` | Built successfully |
| Local `SHA256SUMS` covering all six binaries | All hashes verified |
| Linux amd64 binary `--version` | `noe v1.1.0` |
| Linux amd64 binary: stdlib timer, Fetch/serve presence, removed `noe` global | Passed; `undefined function function` |

The existing tag-triggered Forgejo workflow remains the distribution gate. Its result and the downloaded Release assets are verified separately after publication. Cross-builds verify compilation; runtime and terminal behavior on macOS, Windows, and arm64 were not tested locally.

## v1.1.0 Release validation — 2026-10-16

- Annotated tag: `v1.1.0`, pointing to `a06fe533365cf261b4775292b2f2198c023a460f`. This contains the checked source revision and the preparation record above.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/2): succeeded (API run ID 180, job ID 201).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.1.0): published, neither draft nor prerelease. Its description matches the committed [release notes](v1.1.0.md), including the removed `noe.*` APIs and migration guidance.

The actual workflow passed the CGO-free test and vet gates, built all six profile targets, and uploaded the six binaries plus `SHA256SUMS`. All seven assets were then downloaded from the Release. Every binary hash matched the published checksum file.

The downloaded Linux amd64 binary returned `noe v1.1.0` for `--version`. An evaluation importing `timers/promises` and waiting on its timer confirmed the removed `noe` global and the available Fetch/server APIs, printing `undefined function function` with exit code 0. The peeled remote tag was checked against the workflow commit SHA.

This verification record is committed after publication. It does not move or replace the `v1.1.0` tag. The local repository-root `./noe` was also rebuilt with version `v1.1.0`.

## v1.2.0 preparation — 2026-10-16

Checked source revision: `55bbbe4f94e1510c47375c21ee0c99e2db140280`. The checks ran against this source immediately before the commit. This preparation entry is documentation-only; the tag includes it without changing the checked application code. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support.

This release adds the OpenRouter-backed `noe:agent` module, generated JavaScript execution, autonomous repair, parallel child agents, file-based credentials, and opt-in execution tracing. README versions, the default CLI version, and version expectations in unit/E2E tests are updated to `v1.2.0`. See the [release notes](v1.2.0.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go vet ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | Passed |
| `go test -tags=e2e ./... -count=1` | Passed, including Linux PTY checks |
| `go vet -tags=e2e ./...` | Passed |
| All six profile targets, CGO disabled, `-trimpath`, `-s -w -X noe/internal/cli.Version=v1.2.0` | Built successfully |
| Local `SHA256SUMS` covering all six binaries | All hashes verified |
| Built Linux amd64 binary `--version` | `noe v1.2.0` |
| Built Linux amd64: lazy agent import/create plus Fetch/server presence and removed `noe` global | Passed; `function function function undefined`, no model request |
| README/example/release-note relative links and `git diff --check` | Passed |

Full local command/output records and build artifacts are under `/tmp/noe-v1.2.0-release-rasoucsy/`. The repository-root `./noe` was updated from the checked Linux amd64 binary.

The [increment 13 implementation record](../archive/increment-13-implementation.md) documents per-slice and whole-feature reviews and real OpenRouter aggregate/repair/parallel acceptance. A further live run of `try-agent.js` edited README.md and reread the result without human intervention: 28.294 seconds, exit 0, seven model calls and seven successful JS executions. Its task, trace, output, before/after README, diff, and verification are under `/tmp/noe-readme-agent-spse4kwn/`. These observations used the explicitly authorized OpenRouter key and `deepseek/deepseek-v4.1-flash`; no further live API call was needed for this version/documentation-only release preparation.

The existing tag-triggered Forgejo workflow remains the distribution gate. Its result and the published assets are checked separately after publication. macOS/Windows/arm64 native execution remains unverified; successful cross-builds are compilation evidence.

## v1.2.0 Release validation — 2026-10-16

- Annotated tag: `v1.2.0`, pointing to `32e031102506b10c40b55f204b49974412fbbaa5`. This contains the checked source revision and the preparation record above. The peeled remote tag matches this workflow commit.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/3): succeeded (API run ID 181, job ID 202).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.2.0): published, neither draft nor prerelease (Release ID 117). Its description matches the committed [release notes](v1.2.0.md).

The actual workflow set up Go 1.27.2, passed the CGO-free unit-test and vet gates, built all six profile targets, and uploaded the binaries plus `SHA256SUMS`. All seven assets were downloaded after publication, and all six binary hashes matched the published checksum file.

The downloaded Linux amd64 binary returned `noe v1.2.0` for `--version`, `2` for an arithmetic evaluation, and `function function function undefined` for lazy `noe:agent` import/create plus Fetch/server and removed-global checks. All three exited with 0 and empty stderr; the agent check did not make a model request. Downloaded artifacts and verification records are under `/tmp/noe-v1.2.0-published-izk96800/`.

This verification entry is committed after publication and does not move or replace the `v1.2.0` tag. The repository-root `./noe` was updated to the verified published Linux amd64 binary. macOS/Windows/arm64 native execution is still unverified.

## v1.3.0 preparation — 2026-10-17

Checked source revision: `0c87d0fbb20fd06b69771ef0509da0058bedfde5`. The checks ran against this application source immediately before the commit; the final source-tree adjustment was removal of a trailing blank line in a validation document. This preparation entry is documentation-only. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support.

This release includes managed agent lifecycle/control and qjs REPL integration since v1.2.0, plus structured decisions, OpenCode Go, OpenAI SDK/ChatGPT credentials, configurable providers and a model catalog, and richer execution diagnostics. Both READMEs now explain credential registration, root startup, results and failures, child inheritance, concurrency, keepAlive, control commands, and observation. The public agent API breaks compatibility with v1.2.0; see the [release notes](v1.3.0.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go vet ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | Passed |
| `go test -tags=e2e ./... -count=1` | Passed, including Linux PTY checks |
| `go vet -tags=e2e ./...` | Passed |
| All six profile targets, CGO disabled, `-trimpath`, `-s -w -X noe/internal/cli.Version=v1.3.0` | Built successfully |
| Local `SHA256SUMS` covering all six binaries | All hashes verified |
| Built Linux amd64 `--version` and arithmetic evaluation | `noe v1.3.0` and `2`, exit 0 |
| Lazy agent/decision/auth imports, Fetch/serve, removed global, and OpenAI catalog | Passed without provider requests |
| External ChatGPT helper `help` under the built binary | Passed, exit 0, empty stderr |
| Changed Markdown relative links, JSON files, gofmt, and staged diff whitespace | Passed |

Local command/output records, checksums, and build artifacts are under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.0-release-n3m2z62v/`. The Linux amd64 binary is 47,739,040 bytes with the release flags. Native runtime behavior on other targets remains unverified.

Existing live acceptance evidence is recorded in [agent controls](../development/agent-control-openrouter-validation.md), [decisions](../development/decision-openrouter-validation.md), [OpenCode Go/provider configuration](../development/provider-config-validation.md), and [OpenAI](../development/openai-implementation.md). The live ChatGPT tests used refreshed existing credentials; fresh browser registration and the OpenAI API-key route were fixture-tested. No further external model request was made during this release preparation.

The tag-triggered Forgejo workflow remains the distribution gate. Publication and downloaded assets are verified separately after tagging.

## v1.3.0 Release validation — 2026-10-17

- Annotated tag: `v1.3.0`, pointing to `68f1f298b6b5ba95659429b3f613fa665baa37db`. The peeled remote tag matches the successful workflow commit; this includes the checked source and preparation record above.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/4): succeeded (API run ID 185, job ID 206).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.3.0): published, neither draft nor prerelease (Release ID 119). Its description matches the committed [release notes](v1.3.0.md).

The actual workflow passed its CGO-free test/vet gates, built all six profile targets, and uploaded the binaries plus `SHA256SUMS`. All seven assets were downloaded after publication; every binary hash matched the published checksum file.

The downloaded Linux amd64 binary passed four smoke checks with exit 0 and empty stderr: `--version` returned `noe v1.3.0`; arithmetic evaluation returned `2`; lazy agent/decision/auth imports, Fetch/serve, removed-global, and OpenAI catalog checks passed; the external ChatGPT helper displayed its help. No model provider request was needed. The helper's versioned download URL was also checked against the committed JavaScript. Downloaded artifacts and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.0-published-82a__6s7/`.

The repository-root `./noe` is now the verified published Linux amd64 binary. This documentation-only verification commit follows publication without moving the tag. Native runtime behavior on macOS, Windows, and arm64 remains unverified.

## v1.3.1 preparation — 2026-10-17

Checked source revision: `1451a6272a9175f90ee3c7b0d9a3608f8a182ea2`. Checks ran against the same application and test source immediately before this commit. The cross-builds preceded a test-only cancellation-fixture adjustment; application code remained identical. This preparation entry is documentation-only. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support.

This release updates go-quickjs from v0.25.3 to v0.27.0. Compatibility was checked through v0.26.0 first, with full unit/CLI/REPL E2E success for both versions using separate modfiles. The qjs-derived five input files and MIT license are byte-for-byte identical upstream across all three versions; NOE keeps the existing source snapshot and provenance. `Workers.Max: -1` preserves the existing unlimited Worker count. See the [release notes](v1.3.1.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go vet ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | Passed after the test adjustment below |
| `go test -tags=e2e ./... -count=1` | Passed, including Linux PTY checks |
| `go vet -tags=e2e ./...` | Passed |
| 65 concurrent JavaScript Workers, followed by termination | Passed in unit/E2E and race runs |
| All six profile targets, CGO disabled, `-trimpath`, `-s -w -X noe/internal/cli.Version=v1.3.1` | Built successfully |
| Local `SHA256SUMS` covering all six binaries | All hashes verified |
| Built Linux amd64 `--version` and arithmetic evaluation | `noe v1.3.1` and `2`, exit 0 |
| Lazy agent/decision/auth imports, Fetch/serve, removed global, OpenAI catalog, and auth helper help | Passed without provider requests |
| Built binary dependency information | Contains go-quickjs v0.27.0 |
| Changed Markdown relative links, gofmt, and staged diff whitespace | Passed |

The initial concurrent checks had one race-run test failure: `TestObserveAgentWaitYieldsToJSAndIsCancelledWithEvaluation` reached its 150 ms deadline before its timer fired; no data race was reported. The test passed in isolation. Its deadline included host initialization, although its purpose is to prove observer waiting yields to JavaScript and respects evaluation cancellation. It now cancels from the timer callback once that behavior is observed, with a five-second timeout guarding a blocked loop. After this test-only change, all five checks were rerun and passed. Initial failure logs are retained under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.1-release-bdp3e1ix/first-attempt/`.

Final command/output records and build artifacts are under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.1-release-bdp3e1ix/`. The Linux amd64 binary is 47,960,224 bytes with the release flags. No external model API was called. Publication and downloaded assets are verified separately after tagging; native runtime behavior on other platforms remains unverified.

## v1.3.1 Release validation — 2026-10-17

- Annotated tag: `v1.3.1`, pointing to `b94b275ed6e382d80c20ec6c6a74646ca81ea5f3`. The peeled remote tag matches the successful workflow commit and includes the checked source and preparation record above.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/5): succeeded (API run ID 186, job ID 207).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.3.1): published, neither draft nor prerelease (Release ID 120). Its description matches the committed [release notes](v1.3.1.md).

The workflow passed its CGO-free test/vet gates, built all six profile targets, and uploaded six binaries plus `SHA256SUMS`. All seven assets were downloaded; all six binary checksums matched the published file.

The published Linux amd64 binary passed four smoke checks with exit 0 and empty stderr: version (`noe v1.3.1`), arithmetic (`2`), lazy agent/decision/auth imports with Fetch/serve and the OpenAI catalog, and the external ChatGPT helper's help command. Its embedded Go build information confirms `github.com/go-quickjs/go-quickjs v0.27.0`. The helper's versioned download URL also matches the committed script. No model request was made. Artifacts and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.1-published-kp5497v6/`.

The repository-root `./noe` is the verified published Linux amd64 binary. This documentation-only verification commit follows publication without moving the tag. macOS, Windows, and arm64 native runtime behavior remains unverified.

## v1.3.2 preparation — 2026-10-17

Checked source revision: `d8a58e857292340088fb9f5e2b07512e9bfe3ea1`. The same application, test source, and dependencies were checked immediately before/after this commit; no code change intervened. This preparation entry is documentation-only. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support.

This release updates go-intl, OpenRouter SDK, gjson, pretty, match, ajson, x/sys, and x/term after individual and combined compatibility checking. NOE's runtime implementation and JavaScript API remain unchanged. README links, CLI version, and version assertions are updated to v1.3.2. See the [release notes](v1.3.2.md) and [dependency validation record](../development/dependency-updates.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | Passed, including Linux PTY checks |
| `CGO_ENABLED=0 go vet ./...` | Passed |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | Passed |
| All six profile targets, CGO disabled, `-trimpath`, `-s -w -X noe/internal/cli.Version=v1.3.2` | Built successfully |
| Local `SHA256SUMS` covering all six binaries | All hashes verified |
| Built Linux amd64 `--version` | `noe v1.3.2`, exit 0 |
| Arithmetic, lazy agent/decision/auth imports, Intl normalization/collation, external auth helper help | All passed, empty stderr |
| Embedded dependency information | All eight updated module versions confirmed |
| Changed Markdown relative links, Go formatting, and staged diff whitespace | Passed |

The five checks were run sequentially, followed by the six builds. All passed on this release run, including the startup-dependent timeout test discussed in the dependency compatibility record. The Linux amd64 binary is 47,976,608 bytes with the release flags. No real model request was made. Logs and artifacts are under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.2-release-pqtmecgr/`.

The tag-triggered Forgejo workflow remains the distribution gate; publication and downloaded assets are verified separately. Native runtime execution on macOS, Windows, and arm64 remains unverified.

## v1.3.2 Release validation — 2026-10-17

- Annotated tag: `v1.3.2`, pointing to `3f82e10becdfbad3b2ea48136d44349ba59ba220`. The peeled remote tag matches the successful workflow commit and includes the checked source and preparation record above.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/6): succeeded (API run ID 187, job ID 208).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.3.2): published, neither draft nor prerelease (Release ID 121). Its description matches the committed [release notes](v1.3.2.md).

The workflow passed its CGO-free test/vet gates, built all six profile targets, and uploaded six binaries plus `SHA256SUMS`. All seven assets were downloaded; all six binary checksums matched the published file.

The published Linux amd64 binary passed five smoke checks with exit 0 and empty stderr: version (`noe v1.3.2`), arithmetic (`2`), lazy agent/decision/auth imports, Intl normalization/collation, and the external ChatGPT helper's help command. Its embedded Go build information confirms all eight updated dependency versions and go-quickjs v0.27.0. The helper's versioned download URL also matches the committed script. No model request was made. Artifacts and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.2-published-k371cnhz/`.

The repository-root `./noe` is the verified published Linux amd64 binary. This documentation-only verification commit follows publication without moving the tag. macOS, Windows, and arm64 native runtime behavior remains unverified.

## v1.3.3 preparation — 2026-10-17

Checked source revision: `0387e10d8b22cc52beb8385ec182332d76fd5fc8`. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support. Subsequent preparation changes only update this record and the observation guides' commands for the relocated example.

This release reorganizes the agent broker, SSE handling, recorded-run observation, and E2E tests. All 107 original implementation declarations were compared before and after splitting, including their bodies and attached comments, and are byte-for-byte unchanged. Existing E2E test names and build tags are retained; live acceptance reports now obtain the SDK version from the tested CLI. Dependency versions remain those of v1.3.2. The example is now `examples/try-agent.js`. See the [release notes](v1.3.3.md) and [E2E test guide](../../tests/README.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | Passed, including Linux PTY checks |
| `CGO_ENABLED=0 go vet ./...` | Passed |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | Passed |
| Live-test build with `e2e agent_live decision_live`, `-run '^$'` | Passed; no live cases executed |
| All six profile targets, CGO disabled, `-trimpath`, `-s -w -X noe/internal/cli.Version=v1.3.3` | Built successfully |
| Local `SHA256SUMS` covering all six binaries | All hashes verified |
| Built Linux amd64 version, arithmetic, lazy agent/decision/auth imports, Intl normalization/collation, external auth helper help | All five checks passed, exit 0 and empty stderr |
| Embedded dependency information | Confirms the v1.3.2 module versions and go-quickjs v0.27.0 |
| Changed Markdown relative links, Go formatting, and diff whitespace | Passed |

The five main checks ran sequentially, followed by the six builds. The Linux amd64 binary is 47,976,608 bytes with the release flags. No real model request was made. Logs and build artifacts are under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.3-release-1xqnnkw7/`. The declaration comparison and initial refactoring checks are under `/home/agent/.cache/noe-qjs-repl/noe-source-tidy-ti37tizi/`.

The tag-triggered Forgejo workflow remains the distribution gate; publication and downloaded assets are verified separately. Native runtime execution on macOS, Windows, and arm64 remains unverified.

## v1.3.3 Release validation — 2026-10-17

- Annotated tag: `v1.3.3`, pointing to `4612c8b13cdaf41ac27dd79e8f0a2bd7bf09a104`. The peeled remote tag matches the successful workflow commit and includes the checked source and preparation record above.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/7): succeeded (API run ID 188, job ID 209).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.3.3): published, neither draft nor prerelease (Release ID 122). Its description matches the committed [release notes](v1.3.3.md).

The workflow passed its CGO-free test/vet gates, built all six profile targets, and uploaded six binaries plus `SHA256SUMS`. All seven assets were downloaded; all six binary checksums matched the published file.

The published Linux amd64 binary passed five smoke checks with exit 0 and empty stderr: version (`noe v1.3.3`), arithmetic (`2`), lazy agent/decision/auth imports, Intl normalization/collation, and the external ChatGPT helper's help command. Its embedded Go build information confirms the dependency versions retained from v1.3.2 and go-quickjs v0.27.0. The helper's versioned download URL also matches the committed script. No model request was made. Artifacts and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.3-published-u1f9ufrd/`.

The repository-root `./noe` is the verified published Linux amd64 binary. This documentation-only verification commit follows publication without moving the tag. macOS, Windows, and arm64 native runtime behavior remains unverified.

## v1.4.0 preparation — 2026-10-18

Checked source revision: `9f9e57e906a5981719be5395363b44b33cd70790`. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support. Subsequent preparation changes only add this release record.

This release includes the managed-process and standard-I/O extensions committed after v1.3.3, local ESM path resolution and exact-name import maps, and the reusable JavaScript MCP SDK example. Source, CLI version, version assertions, both READMEs, and helper download links are updated for v1.4.0. Dependency versions remain unchanged from v1.3.3. See the [release notes](v1.4.0.md), [process/I/O implementation record](../development/process-io-implementation.md), and [import map implementation record](../development/local-import-map-implementation.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | Passed, including Linux PTY and the opt-in MCP SDK checks |
| `CGO_ENABLED=0 go vet ./...` | Passed |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | Passed; `GORACE=atexit_sleep_ms=0` removes instrumented child-process exit delays while keeping race detection |
| All six profile targets, CGO disabled, `-trimpath`, `-s -w -X noe/internal/cli.Version=v1.4.0` | Built successfully |
| Local `SHA256SUMS` covering all six binaries | All hashes verified |
| Linux amd64 version, arithmetic, lazy agent/decision/auth imports, Intl, external auth helper help, import-map run/eval, host byte-stream output, owned child process | All nine smoke checks passed, exit 0 and empty stderr |
| Embedded dependency information | Confirms the retained module versions and go-quickjs v0.27.0 |
| Changed Markdown relative links, Go formatting, and diff whitespace | Passed |

The five main checks ran sequentially, followed by the six release builds. The E2E run explicitly supplied `NOE_MCP_SDK_BUNDLE=/tmp/noe-process-mcp.wzVT64/mcp-client.bundle.mjs`, prepared with SDK 1.32.1 and esbuild 0.25.12; it does not call real model providers or run Node. The Linux amd64 binary is 48,222,368 bytes with the release flags. Logs, checksums, and build artifacts are under `/home/agent/.cache/noe-qjs-repl/noe-v1.4.0-release-en13jon_/`.

The tag-triggered Forgejo workflow remains the distribution gate. Publication and downloaded assets are verified separately after tagging. Native runtime execution on macOS, Windows, and arm64 remains unverified.

## v1.4.0 Release validation — 2026-10-18

- Annotated tag: `v1.4.0`, pointing to `ed48c04f5c4a77de3f0d6fd9b11b3242ea9961d0`. The peeled remote tag matches the successful workflow commit and includes the checked source and preparation record above.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/8): succeeded (API run ID 189, job ID 210).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.4.0): published, neither draft nor prerelease (Release ID 123). Its description matches the committed [release notes](v1.4.0.md).

The workflow passed its CGO-free test/vet gates, built all six profile targets, and uploaded six binaries plus `SHA256SUMS`. All seven assets were downloaded; all six binary checksums matched the published file.

The published Linux amd64 binary passed all nine preparation smoke checks: version (`noe v1.4.0`), arithmetic, lazy agent/decision/auth imports, Intl, the external auth helper help command, import-map run/eval, host byte-stream output, and owned child-process execution. Every check returned exit 0 and empty stderr. Its embedded Go build information confirms go-quickjs v0.27.0, the retained dependencies, the release-tag commit, and an unmodified source tree. The helper's versioned download URL also returns the committed script byte-for-byte. No model request was made.

Artifacts and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.4.0-published-a9qi92a7/`; preparation and orchestration records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.4.0-release-en13jon_/`. The repository-root `./noe` is the verified published Linux amd64 binary. This documentation-only verification commit also places the v1.4.0 preparation record after the earlier releases without moving the tag. macOS, Windows, and arm64 native runtime behavior remains unverified.

## v1.5.0 preparation — 2026-10-18

Checked source revision: `fad40a7ffd4f20bbca3b69919e28a816670500e9`. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support. The preparation follow-up adds only this record; application and test source remain the checked revision.

This release adds invocation-scoped agent hooks, execution groups and optional shared admission limits, context budget/usage observation and model notifications, default shared/workspace instruction loading, and the reusable Exa library example. CLI version, version assertions, both READMEs, and helper download links are updated for v1.5.0. Dependencies and the release workflow/profile are unchanged from v1.4.0. See the [release notes](v1.5.0.md), [hooks implementation record](../development/execution-hooks-implementation.md), and [instruction validation record](../development/instructions-implementation.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | Passed, including Linux PTY and the opt-in local MCP SDK checks |
| `CGO_ENABLED=0 go vet ./...` | Passed |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | Passed with `GORACE=atexit_sleep_ms=0` |
| `e2e agent_live decision_live hooks_live`, `-run '^$'` | Compiled; no real-provider cases executed |
| All six profile targets, CGO disabled, `-trimpath`, `-s -w -X noe/internal/cli.Version=v1.5.0` | Built successfully |
| Local `SHA256SUMS` covering all six binaries | All hashes verified |
| Linux amd64 version, arithmetic, lazy agent/decision/auth imports, Intl, auth helper help, import-map run/eval, host byte-stream output, owned child process, and Exa library import | All ten smoke checks passed, exit 0 and empty stderr |
| Linux amd64 feature smoke with local HTTP fixture | Passed: shared/workspace/system order, two model requests with one context notice each, four hook phases, effective tool arguments/result, group counters and active counts zero |
| Embedded dependency information | Confirms retained module versions and go-quickjs v0.27.0 |
| Staged diff whitespace and release documentation/version review | Passed; no required review finding |

The five main checks ran sequentially, followed by six builds. E2E explicitly used `NOE_MCP_SDK_BUNDLE=/tmp/noe-process-mcp.wzVT64/mcp-client.bundle.mjs`, prepared with SDK 1.32.1 and esbuild 0.25.12. The Linux amd64 binary is 48,488,608 bytes with release flags. Logs, checksums, smoke scripts, and artifacts are under `/home/agent/.cache/noe-qjs-repl/noe-v1.5.0-release-1ve_i8ix/`.

Earlier authorized [OpenCode Go live acceptance](../development/execution-hooks-opencode-validation.md) verified six hook/context/control scenarios on the pre-release implementation; failed authentication and stream attempts are retained separately from successful cases. The Exa library passed local mock tests and one authorized real search. This release preparation did not make additional paid requests or read real model credentials. Native macOS, Windows, and arm64 execution remains unverified.

The tag-triggered Forgejo workflow remains the distribution gate. Publication, downloaded assets, and the native installed binary are verified after tagging.

## v1.5.0 Release validation — 2026-10-18

- Annotated tag: `v1.5.0`, pointing to `653e64fe2d99eaac5d6700e7343434038dd87f7c`. The peeled remote tag matches the successful workflow commit and includes the checked source plus preparation record above.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/9): succeeded (API run ID 190, job ID 211).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.5.0): published, neither draft nor prerelease (Release ID 124). Its description matches the committed [release notes](v1.5.0.md).

The workflow passed the CGO-free test/vet gates, built all six profile targets, and uploaded six binaries plus `SHA256SUMS`. All seven assets were downloaded, and every binary checksum matched the published file.

The downloaded Linux amd64 binary passed all ten preparation smoke checks with exit 0 and empty stderr. A separate local-fixture feature smoke verified automatic common/workspace/system instruction composition, two model/tool turns, context notifications and hook runtime information, all four hook phases with transformed tool arguments/results, and final group counters/active work. The published binary's embedded build information confirms the tag commit, an unmodified source tree, and the retained dependency versions. The versioned ChatGPT helper download is byte-for-byte identical to the committed script. No real-provider request was made during these publication checks.

Downloaded binaries and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.5.0-published-vftdg42f/`; release orchestration, local checks, and both feature-smoke records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.5.0-release-1ve_i8ix/`. The repository-root `./noe` is the verified published Linux amd64 binary and reports `noe v1.5.0`.

This documentation-only verification commit follows publication without moving the release tag. Native macOS, Windows, and arm64 execution remains unverified.


## v1.6.0 preparation — 2026-10-18

Checked source revision: `77259bd31384faf9ae73b63a17a190448e4e11b1`. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support. The preparation follow-up adds this record and clarifies two release-note sentences; application and test source remain the checked revision.

This release adds automatic agent context trimming, execution-history budget estimates and notifications, trim observation/cause tracking, inherited context policies, and a confirmed OpenRouter model-window entry. CLI version, assertions, README release/helper links and release notes use v1.6.0. Dependencies and release workflow/profile remain unchanged from v1.5.0. See the [release notes](v1.6.0.md), [implementation record](../development/context-trimming-implementation.md), and [live behavior results](../development/context-trimming-live-results.json).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | Passed, including Linux PTY and opt-in local MCP SDK checks |
| `CGO_ENABLED=0 go vet ./...` | Passed |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | Passed with `GORACE=atexit_sleep_ms=0` |
| `e2e agent_live decision_live hooks_live context_live`, `-run '^$'` | Compiled; no real-provider cases executed |
| All six profile targets, CGO disabled, `-trimpath`, `-s -w -X noe/internal/cli.Version=v1.6.0` | Built successfully |
| Local `SHA256SUMS` covering all six binaries | All hashes verified |
| Linux amd64 version, arithmetic, lazy agent/decision/auth imports, Intl, helper help, import-map run/eval, host streams, owned process and Exa import | All ten smoke checks passed, exit 0 and empty stderr |
| Local-fixture feature smoke | Passed: shared/workspace/system order, two requests, one notice per request, all four hook phases, transformed tool payloads, counters/active work |
| Local-fixture context smoke | Passed: four model requests, two actual trims, protected task and latest complete tool block, stable session, one post-trim notice, live/saved count agreement and causal IDs |
| Embedded dependency information | Confirms retained module versions and go-quickjs v0.27.0 |
| Changed Markdown relative links and diff whitespace | Passed: 171 relative references checked |
| Release version/documentation/workflow review | Passed; no required finding; optional wording clarifications applied |

Checks ran sequentially before the six release builds. E2E reused `NOE_MCP_SDK_BUNDLE=/tmp/noe-process-mcp.wzVT64/mcp-client.bundle.mjs`, prepared with SDK 1.32.1 and esbuild 0.25.12. The local Linux amd64 binary is 48,545,952 bytes with release flags. Logs, checksums, smoke scripts, and build artifacts are under `/home/agent/.cache/noe-qjs-repl/noe-v1.6.0-release-aeeac09q/`.

Earlier authorized OpenCode Go / DeepSeek V4.1 Flash behavior trials and implementation-slice reviews are recorded separately, including the initial failed numeric-ID assertion and clarified successful retry. This release preparation made no additional real-provider calls. Native macOS, Windows, and arm64 execution remains unverified.

The tag-triggered Forgejo workflow remains the distribution gate. Published assets and the installed native binary are checked after tagging.


## v1.6.0 Release validation — 2026-10-18

- Annotated tag: `v1.6.0`, pointing to `883045a2ccfff3c754ad6498440b5e589c16347b`. The peeled remote tag matches the successful workflow commit and includes the checked application source plus preparation record above.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/10): succeeded (API run ID 191, job ID 212).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.6.0): published, neither draft nor prerelease (Release ID 125). Its description matches the committed [release notes](v1.6.0.md).

The workflow passed CGO-free test/vet gates, built all six profile targets and uploaded six binaries plus `SHA256SUMS`. All seven assets were downloaded, and every binary checksum matched the published file.

The downloaded Linux amd64 binary passed all ten preparation smoke checks with exit 0 and empty stderr. Local-fixture feature and context smokes also passed on the downloaded binary: automatic instruction ordering, all four hook phases, transformed tool arguments/results, group counters and active work, then four model requests across two actual trims with the original task and newest whole tool block preserved, live/saved trim-count agreement, stable session and trim causal IDs.

The published binary's embedded build information confirms the tag revision, unmodified source tree, go-quickjs v0.27.0 and retained dependencies. The versioned ChatGPT helper download matches the committed script byte-for-byte. No real-provider request was made during release preparation or publication checks.

Downloaded assets and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.6.0-published-99b2aw6d/`; release orchestration, local gates and smoke records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.6.0-release-aeeac09q/`. The repository-root `./noe` is the verified published Linux amd64 binary and reports `noe v1.6.0` (48,545,952 bytes).

This documentation-only verification commit follows publication without moving the release tag. Native macOS, Windows, and arm64 execution remains unverified.


## v1.6.1 preparation — 2026-10-18

Checked source revision: `c4f7b25f42a302c23745982698bbd25b5ad1cd0b`. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support. The preparation follow-up adds only this record.

This release fixes same-instance queue continuation: retained conversation and final answers survive task boundaries, started queued instructions are protected, system instructions stay frozen, and trim state continues without resurrecting removed blocks. Run IDs, maxSteps, cancellation scopes, hook-module state, and cache/session behavior keep their existing task boundaries. Both READMEs, default CLI version, and version assertions use v1.6.1. Dependencies and release workflow/profile are unchanged. See the [release notes](v1.6.1.md) and [implementation record](../development/queue-continuation-implementation.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | Passed, including Linux PTY and opt-in local MCP SDK checks |
| `CGO_ENABLED=0 go vet ./...` and `go vet -tags=e2e ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` with `GORACE=atexit_sleep_ms=0` | Passed |
| All six CGO-free profile builds with v1.6.1 release flags | Passed; all local SHA256SUMS entries verified |
| Native binary smoke checks | All ten passed, exit 0 and empty stderr |
| Local-fixture hooks/instruction smoke | Passed: two requests, all four hook phases, transformed tool arguments/results and group counters |
| Local-fixture trimming smoke | Passed: four requests, two trims, whole tool blocks, live/saved counts and causal IDs |
| Local-fixture queue smoke | Passed: three requests over two runs, prior tool exchange/final answer/reasoning preserved, pending queue excluded, system frozen despite file change, per-run cache/session boundary unchanged |
| Markdown relative links | All 305 changed-document references resolve |
| Code review and post-review delta | No required finding; bootstrap cancellation regression fixed and verified |

The five main checks ran sequentially, followed by the six builds. E2E reused `NOE_MCP_SDK_BUNDLE=/tmp/noe-process-mcp.wzVT64/mcp-client.bundle.mjs`, prepared with SDK 1.32.1 and esbuild 0.25.12. The local Linux amd64 binary is 48,586,912 bytes with release flags. Embedded build information confirms the checked revision, an unmodified source tree, go-quickjs v0.27.0 and retained dependencies.

Logs, checksums, smoke scripts and build artifacts are under `/home/agent/.cache/noe-qjs-repl/noe-v1.6.1-release-lx0i3r48/`. No real-provider request was made. Native execution on other platforms remains unverified. Tag-triggered publication and downloaded assets are verified separately after tagging.


## v1.6.1 Release validation — 2026-10-18

- Annotated tag: `v1.6.1`, pointing to `d1b93ca4b620ec688cb29175df567d979b51a4c4`. The peeled remote tag matches the successful workflow commit and includes the checked source plus preparation record above.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/11): succeeded (API run ID 192, job ID 213).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.6.1): published, neither draft nor prerelease (Release ID 126). Its description matches the committed [release notes](v1.6.1.md).

The workflow passed CGO-free unit test/vet gates, built all six profile targets, and uploaded six binaries plus SHA256SUMS. All seven assets were downloaded; every binary checksum matched the published manifest.

The downloaded Linux amd64 binary passed all ten native smoke checks with exit 0 and empty stderr. Fixture-backed hooks/instruction, trimming, and queue-continuation smokes also passed: two model/tool requests with all four hook phases; four requests across two trims with live/saved counts and causal IDs; and three requests over two queue runs with a complete tool exchange, previous final answer and reasoning retained. The queue smoke also verified pending-task exclusion, frozen system instructions after a file change, distinct run IDs, and unchanged cache/session boundaries.

Embedded build information confirms the release-tag revision, an unmodified source tree, go-quickjs v0.27.0, and the retained dependencies. The versioned ChatGPT helper download matches the committed script byte-for-byte. No real-provider request was made during release preparation or publication checks.

Downloaded assets and checksum records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.6.1-published-p2dzbaaf/`; orchestration, gate output and feature-smoke records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.6.1-release-lx0i3r48/`. The repository-root `./noe` is the verified published Linux amd64 binary and reports `noe v1.6.1` (48,586,912 bytes).

This documentation-only verification commit follows publication without moving the release tag. Native macOS, Windows and arm64 execution remains unverified.


## v1.7.0 preparation — 2026-10-18

Checked source revision: `d09a365d1416c9a8e9ee1f10046e75f167722aca` (image implementation `9e472e2`, followed by a test-fixture timing correction). The preparation commit adds this record and completes the image entries in both READMEs; application and test source remain the checked revision. Checks and builds used a clean detached checkout, preserving the user's unrelated `agent-server-build.jsonl` outside the release.

This release adds agent image input and independent image generation, including owned snapshots, complete-block conversation/queue/trim integration, hook selection, context estimates, provider configuration/catalog, structured errors and observation. It also includes the model-window/budget clarifications, HTTP agent-server example and example reorganization committed since v1.6.1. See the [release notes](v1.7.0.md), [implementation record](../development/agent-images-implementation.md), and [live checks](../development/agent-images-integration.md). CLI version, version assertions, both README release/helper links, and notes use v1.7.0.

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed on the final checked revision |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | Passed, including Linux PTY and opt-in local MCP SDK checks |
| `CGO_ENABLED=0 go vet ./...` and `go vet -tags=e2e ./...` | Passed |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | Passed after the timing-fixture correction, with `GORACE=atexit_sleep_ms=0` |
| Focused CLI timeout race check, repeated three times | Passed |
| Six CGO-free profile builds, `-trimpath -s -w -X noe/internal/cli.Version=v1.7.0` | Passed; all local SHA256SUMS entries verified |
| Native binary checks | Eleven passed: version, arithmetic, lazy modules, Intl, helper help, import maps, streams, owned process, Exa import, image exports/catalog |
| Hooks/instruction, context and queue fixture smokes | Passed: hook transforms/counters, four requests across two trims, and prior tool/final/reasoning continuation over two queue runs |
| New image fixture smoke | Passed: parent chat→independent OpenAI image endpoint→atomic save→read_image→next chat; original PNG bytes preserved, null result replacement retains the image, correct counters and metadata-only events |
| Embedded dependencies and VCS information | go-quickjs v0.27.0, added x/image v0.47.0, checked source revision and `vcs.modified=false` |
| Markdown references and whitespace | All 394 changed-document local references resolve; `git diff --check` passed |

The initial race run hit the existing CLI timeout fixture before its managed process reached HTTP: the CLI correctly returned 124, but the test assumed startup within 300ms. The fixture now allows 2 seconds before testing cancellation of the deliberately blocked request. No application behavior changed; the focused test, CLI suite, full race suite, full unit suite and E2E suite passed afterward.

Environment: Linux amd64, Go 1.27.2, C toolchain for race, Linux PTY. E2E reused the prepared MCP SDK bundle at `/tmp/noe-process-mcp.wzVT64/mcp-client.bundle.mjs`. The release profile/workflow is unchanged. The local Linux amd64 binary is 49,164,448 bytes with release flags. Records, checksums, logs, fixture servers and builds are under `/home/agent/.cache/noe-qjs-repl/noe-v1.7.0-release-gf9uhahi/`.

The earlier authorized real-provider checks are recorded separately, including Sol's four-quadrant recognition and generation/readback. Release preparation and publication verification use local fixtures, without further provider calls. Native macOS, Windows and arm64 execution remains unverified. Tag-triggered workflow publication and downloaded assets are verified after tagging.


## v1.7.0 Release validation — 2026-10-18

- Annotated tag: `v1.7.0`, pointing to `09f21a8790488a234bb6092b120e2914305e7aba`. The peeled remote tag matches the successful workflow commit.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/13): succeeded (API run ID 194).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.7.0): published, neither draft nor prerelease (Release ID 127). Its description matches the committed [release notes](v1.7.0.md).

The first tag-triggered workflow (run 12, API ID 193) failed the existing process-cleanup fixture: it checked for immediate PID disappearance after agent-group completion. Source inspection and independent review confirmed that the separately supervised process is reaped asynchronously after its owner closes; agent completion does not wait for that final reap. The failed run did not capture the process state, so its exact state is unknown. A 200-run diagnostic did not reproduce a persistent process. The follow-up test commit `0531679` allows two seconds for disappearance, still requiring ENOENT and rejecting a remaining process or zombie; it passed 100 normal and 30 race repetitions. This test-only correction follows the release tag on main and is not included in the tagged source.

The tag was left unchanged. Manually dispatching the release workflow through the Forgejo API against the same `v1.7.0` ref passed all publication gates. The workflow built all six profile targets and uploaded six binaries plus `SHA256SUMS`. All seven assets were downloaded; every binary checksum matched the published manifest.

The downloaded Linux amd64 binary passed all eleven native checks. Four local-fixture smokes also passed: hooks/instruction ordering and transformed tool results; four requests across two context trims; conversation continuation over two queue runs; and parent chat → independent image generation → atomic file save → read_image → next model request. The image check verified the original PNG bytes, metadata-only events, retained image after a null result replacement, and three provider calls split into two chat calls and one image call.

Embedded build information confirms the tag revision, an unmodified source tree, go-quickjs v0.27.0 and x/image v0.47.0. The versioned ChatGPT helper download matches the committed script byte-for-byte. No additional real-provider request was made during release preparation or publication verification.

Downloaded assets and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.7.0-published-d_73t7c5/`; orchestration, gate output, CI diagnostics and fixture-smoke records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.7.0-release-gf9uhahi/`. The repository-root `./noe` is the verified published Linux amd64 binary and reports `noe v1.7.0` (49,164,448 bytes).

The test and documentation follow-ups are pushed to main without moving the release tag. Native macOS, Windows and arm64 execution remains unverified.


## v1.7.1 preparation — 2026-10-18

Checked source revision: `cb0bd185210ccd541dde1de160e81f89c7aa0bce`. The preparation follow-up adds this record only. Checks and builds used a clean detached worktree, excluding the user's unrelated `agent-server-build.jsonl`.

This maintenance release bounds captured agent stdout/stderr to 64 KiB per stream, retains the beginning and end with omission metadata, and reorganizes agent responsibilities without changing public APIs or shared locking. It also includes the asynchronous process-cleanup test correction committed after the v1.7.0 tag. CLI defaults, version assertions, README release/helper links and [release notes](v1.7.1.md) use v1.7.1. Dependencies and the release profile/workflow are unchanged. Slice and overall reviews are recorded in the [implementation checks](../development/agent-refactor-implementation.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | Passed, including Linux PTY and prepared local MCP SDK checks |
| `CGO_ENABLED=0 go vet ./...` and `go vet -tags=e2e ./...` | Passed |
| `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1` | Passed |
| Six CGO-free profile builds with `-trimpath -s -w -X noe/internal/cli.Version=v1.7.1` | Passed; local SHA256SUMS verified |
| Native CLI checks | Eleven passed, including version, lazy modules, import maps, streams, owned process, Exa import and image exports/catalog |
| Hooks/instruction, context, queue and image fixture smokes | Passed: hook transforms/counters, two trims, conversation continuation and independent generation/readback |
| Bounded-output fixture smoke | Passed: about 2 MiB each on stdout/stderr, first/last sections retained within each 64 KiB data budget, exact omitted-byte counts through afterTool, model result and JSONL, successful completion preserved |
| Embedded dependencies and source information | Retained versions, go-quickjs v0.27.0, checked revision and `vcs.modified=false` |
| Changed-document relative links and whitespace | All 235 references resolve; `git diff --check` passed |

All five gates ran sequentially before the six release builds. Environment: Linux amd64, Go 1.27.2, C toolchain for race and Linux PTY. E2E reused `NOE_MCP_SDK_BUNDLE=/tmp/noe-process-mcp.wzVT64/mcp-client.bundle.mjs`. Local Linux amd64 size: 49,180,832 bytes. Logs, fixture scripts, builds and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.7.1-release-mtd4niey/`.

No real-provider request was made. Other platforms have cross-build evidence only. The tag-triggered workflow and downloaded release assets are verified after publication.


## v1.7.1 Release validation — 2026-10-18

- Annotated tag: `v1.7.1`, pointing to `1fe99ed6e15de4b7be999004fb0119b4ecafabc5`. The peeled remote tag matches the successful workflow commit.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/14): succeeded (API run ID 195, job ID 216).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.7.1): published, neither draft nor prerelease (Release ID 128). Its description matches the committed [release notes](v1.7.1.md).

The tag-triggered workflow passed its unit/vet gates, built all six targets and uploaded six binaries plus `SHA256SUMS`. All seven assets were downloaded, and every binary checksum matched the published manifest.

The downloaded Linux amd64 binary passed all eleven native CLI checks and five local-fixture smokes: instruction/hook ordering and transformed tool results; four requests across two context trims; queue conversation continuation; independent image generation and readback; and large-output capture. The output fixture emitted 2,097,170 bytes on each stream, retained the first/last 32 KiB and reported exactly 2,031,634 omitted bytes per stream through the hook, model result, stream events and completion event. Execution still completed successfully.

Embedded build information confirms the release-tag revision, `vcs.modified=false`, go-quickjs v0.27.0 and the retained dependencies. The versioned ChatGPT helper matches the committed script byte-for-byte. No real-provider request was made during release checks.

Downloaded assets and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.7.1-published-o58ng4ux/`; orchestration, gate logs and fixture records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.7.1-release-mtd4niey/`. The repository-root `./noe` is the verified published Linux amd64 binary and reports `noe v1.7.1` (49,180,832 bytes).

This documentation-only follow-up records publication without moving the tag. Native execution on macOS, Windows and arm64 remains unverified.


## v1.8.0 preparation — 2026-10-19

Checked source revision: `ca34c5b89f4666a87d28a2d7baab68567a1326cc`. The preparation follow-up adds this record only. Gates and builds used a clean detached worktree, excluding the unrelated `agent-server-build.jsonl`.

This release adds `noe:fs-handle`, `noe:sockets`, `noe:signals`, and bundled English API docs through the CLI and JavaScript, including main/Worker/run_js integration and agent discovery instructions. The catalog has 40 topics and 98 detailed sections. CLI defaults, version assertions, README release links and [release notes](v1.8.0.md) use v1.8.0. Dependencies and the release profile/workflow are unchanged. Feature checks and independent reviews are recorded in the [low-level I/O record](../development/low-level-group2-implementation.md) and [docs record](../development/builtin-docs-implementation.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | Passed, including Linux PTY, low-level I/O, docs and local provider fixtures |
| `CGO_ENABLED=0 go vet ./...` and `go vet -tags=e2e ./...` | Passed |
| `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1` | Passed |
| Six CGO-free profile builds, `-trimpath -s -w -X noe/internal/cli.Version=v1.8.0` | Passed; all local SHA256SUMS entries verified |
| Existing native CLI smoke checks | Eleven passed: version, arithmetic, lazy modules, Intl, helper help, import maps, streams, owned process, Exa import, image exports/catalog |
| New API smoke checks | Nine passed: configuration-independent docs, aliases, private-topic rejection, CLI/JS/Worker content, positional I/O over TCP/Unix roundtrips, SIGTERM delivery and cleanup |
| Embedded dependencies and VCS information | Retained versions, go-quickjs v0.27.0, checked revision and `vcs.modified=false` |
| Changed-document local links and whitespace | All references resolve; staged diff whitespace checks passed |

All five gates ran sequentially before the six builds. Environment: Linux amd64, Go 1.27.2, C toolchain for race and Linux PTY. The optional external MCP SDK bundle was not present and its opt-in check was not enabled. Its prior implementation evidence remains in the feature record. Local Linux amd64 size: 49,483,936 bytes. Gate logs, checksums, builds, smoke scripts and records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.8.0-release-xf677opo/`.

The already-authorized real-provider docs checks used OpenCode Go / deepseek-v4.1-flash: both natural tasks consulted docs without task-level prompting and completed successfully in nine provider requests total. Those observations are recorded separately; no additional real-provider request was made during release preparation. Other platforms have cross-build evidence only. The tag-triggered workflow and downloaded assets are verified after publication.


## v1.8.0 Release validation — 2026-10-19

- Annotated tag: `v1.8.0`, pointing to `5f09c6efac616fdf5a064f9217ce3d7fb4c654de`. The peeled remote tag matches the successful workflow commit and includes the checked application source plus the preparation record above.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/15): succeeded (API run ID 196, job ID 217).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.8.0): published, neither draft nor prerelease (Release ID 129). Its description matches the committed [release notes](v1.8.0.md).

The tag-triggered workflow passed the CGO-free unit/vet gates, built all six profile targets, and uploaded the six binaries plus `SHA256SUMS`. All seven assets were downloaded; every binary checksum matched the published manifest.

The downloaded Linux amd64 binary passed all eleven existing native CLI smokes and nine new API checks, including the expected private-topic error. The new checks exercised offline docs despite invalid agent configuration, stdlib aliases, matching CLI/JS/Worker reference content, positional file updates through TCP and Unix roundtrips, and actual SIGTERM delivery with subscription cleanup. These publication checks used local fixtures and loopback networking; no further real-provider request was made.

Embedded build information confirms the release-tag revision, `vcs.modified=false`, go-quickjs v0.27.0 and all retained dependency versions. The versioned ChatGPT helper matches the tagged source byte-for-byte. The current agent setup guide links that v1.8.0 helper.

Downloaded assets and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.8.0-published-v2oof0uy/`; gate logs, local builds and new-API smoke records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.8.0-release-xf677opo/`. The repository-root `./noe` is the verified published Linux amd64 binary and reports `noe v1.8.0` (49,483,936 bytes).

This documentation-only verification commit follows publication without moving the release tag. Native execution on macOS, Windows and arm64 remains unverified.


## v1.9.0 preparation — 2026-10-19

Checked source revision: `8a5394ec7cb5ad72f0c22bb7208ae034598f2d65`. This preparation follow-up adds only this record. Gates and builds used a clean detached worktree, excluding the unrelated `agent-server-build.jsonl`.

This release removes the REPL CLI, line editor, history/completion, Session, and recovery/tracking helpers used only by REPL. `run`, `eval`, Workers, agents, `run_js`, docs, cancellation and owned-resource cleanup retain their contracts. Saved REPL JSONL events remain readable. CLI defaults, version assertions, README release/helper links and [release notes](v1.9.0.md) use v1.9.0. Dependencies and distribution targets are unchanged; the reference profile no longer lists the removed REPL's PTY requirement. Independent review and the narrowly scoped process-test helper correction are recorded in the [implementation checks](../development/repl-removal-implementation.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | Passed, 43.338 seconds |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | Passed, 39.840 seconds, including the prepared MCP SDK 1.32.1 bundle and local provider fixtures |
| `CGO_ENABLED=0 go vet ./...` and `go vet -tags=e2e ./...` | Passed |
| `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1` | Passed, 64.403 seconds |
| Six CGO-free profile builds, `-trimpath -s -w -X noe/internal/cli.Version=v1.9.0` | Passed; local checksum manifest generated |
| Native CLI smoke checks | All 29 passed: eleven base checks, nine docs/owned I/O checks, nine REPL removal/cancellation/history/legacy-log checks |
| Local provider fixture | Passed: agent→run_js, instruction ordering, context status, hook transformations and counters; two local model requests, six hook invocations |
| Embedded dependency and source information | Retained versions including go-quickjs v0.27.0, checked revision, `vcs.modified=false` |
| Local document references and whitespace | No new broken links; existing temporary image and archival sibling-repository references recorded separately; `git diff --check` passed |

All five gates ran sequentially before the builds. Environment: Linux amd64, Go 1.27.2 and C toolchain for race. The MCP bundle was prepared from the existing npm cache at `/tmp/noe-repl-removal-20261019-yzglhqqf/mcp-client.bundle.mjs`; its consumers run on NOE. Local Linux amd64 size: 49,303,712 bytes. Gate logs, clean source, builds, checksums and smoke records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.9.0-release-_84uvtgf/`.

No real-provider request was made. Other platforms have cross-build evidence only. The tag-triggered workflow and downloaded release assets are verified after publication.


## v1.9.0 Release validation — 2026-10-19

- Annotated tag: `v1.9.0`, pointing to `5658f9f2384e89cbd9453aff5c452fd0614f71aa`. The peeled remote tag matches the successful workflow commit and includes the checked source plus its preparation record.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/16): succeeded (API run ID 197, job ID 218).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.9.0): published, neither draft nor prerelease (Release ID 130). Its description matches the committed [release notes](v1.9.0.md).

The tag-triggered workflow passed the CGO-free unit/vet gates, built all six profile targets, and uploaded the six binaries plus `SHA256SUMS`. All seven assets were downloaded, and all six binary hashes matched the published manifest.

The downloaded Linux amd64 binary passed all 29 native CLI checks: eleven base checks, nine docs/owned I/O checks, and nine removal/cancellation/history/legacy-log checks. In particular, help omits REPL, `noe repl` exits with code 2 without executing stdin, run/eval keep their stdin and top-level await behavior, timeout returns 124, SIGINT cancels with code 1, and saved REPL events remain readable. TCP/Unix positional I/O, Worker docs, and SIGTERM subscription cleanup also passed. The local provider fixture passed with two requests and six hook invocations, verifying agent→run_js, instruction ordering, context status, result transformation, and final execution counters.

Embedded build information confirms the release-tag revision, `vcs.modified=false`, go-quickjs v0.27.0 and all retained dependency versions. The versioned ChatGPT helper matches the tagged source byte-for-byte. No real-provider request was made during release preparation or publication verification.

Downloaded assets and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.9.0-published-856ro7oq/`; gate logs, clean source, local builds and publication smoke records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.9.0-release-_84uvtgf/`. The repository-root `./noe` is the verified published Linux amd64 binary and reports `noe v1.9.0` (49,303,712 bytes).

This documentation-only verification commit follows publication without moving the release tag. Native execution on macOS, Windows and arm64 remains unverified.


## v1.10.0 preparation — 2026-10-19

Checked source revision: `e1eac6c6a6bd54d5ff553e0b4d451e3e94f9c0be`. This preparation follow-up adds only this record. Gates and builds used a clean detached worktree, excluding the unrelated `agent-server-build.jsonl`.

This release imports and adopts NOE's maintained stdlib snapshot, based on go-quickjs v0.27.0, across main, Workers, agent tasks and `run_js`. The engine dependency and JavaScript API contracts remain unchanged. Bundled English docs distinguish the maintained implementation from its origin. The release also includes replacement of the `x/term` terminal-width dependency. CLI defaults, version assertions, README release/helper links and [release notes](v1.10.0.md) use v1.10.0. The distribution profile and workflow are unchanged.

The first preparation attempt at `659af38` passed unit/E2E/vet but failed `TestNetConnectTCPAndUnixHalfClose/unix` during race testing with `busy=true`; no data-race warning was emitted. Source inspection found that AsyncWork.Complete posts its result before marking the producer done, permitting an immediate idle assertion to run too early. The test now joins the native producers before retaining the same `rt.Busy()` assertion. Production code, close behavior, expected values and skips are unchanged. The exact first failure's interleaving is not established by the log alone. The narrow follow-up review found no P0/P1/P2/P3 issues; targeted race execution passed 100 repetitions. The final gates below were rerun after this test-only correction. See the [review record](../development/stdlib-step2-runtime-review.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1 -timeout=180s` | Passed, 39.849 seconds |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1 -timeout=180s` | Passed, 40.027 seconds |
| `CGO_ENABLED=0 go vet ./...` | Passed, 0.17 seconds |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | Passed, 0.19 seconds |
| `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1 -timeout=300s` | Passed, 56.89 seconds |
| Six CGO-free profile builds, `-trimpath -s -w -X noe/internal/cli.Version=v1.10.0` | Passed; all local SHA256SUMS entries verified |
| Native CLI checks | All 29 passed: eleven base checks, nine docs/owned I/O checks, nine REPL-removal/cancellation/legacy-log checks |
| Fixture-agent smoke | Passed: agent→run_js, instruction/context/hooks/counters; two local requests and six hook invocations |
| Four-mode API inventory | Main, Node-style Worker, nested Worker and Web Worker exactly match the feature-validation inventory |
| Embedded dependencies and source information | Retained dependency versions, go-quickjs v0.27.0, no embedded x/term, checked revision and `vcs.modified=false` |
| Changed-document local links and whitespace | All checked local references resolve; staged whitespace checks passed |

All five final gates ran sequentially before the six builds. Environment: Linux amd64, Go 1.27.2 and C toolchain for race. E2E reused the prepared MCP SDK 1.32.1 bundle at `/tmp/noe-repl-removal-20261019-yzglhqqf/mcp-client.bundle.mjs`. Local Linux amd64 size: 49,291,424 bytes. Initial failure logs are under `/tmp/noe-v1.10.0-release-npfmrpt0/attempt1/`; targeted before/after runs, final gate logs, clean source, builds, hashes and smoke records are under `/tmp/noe-v1.10.0-release-npfmrpt0/`.

Before the version update, the maintained-stdlib source also passed independent code review, per-mode API comparison, unit/race/E2E/vet and six-target compilation. An authorized real OpenCode Go / `deepseek-v4.1-flash` agent completed the basic file-read/aggregate/write/verify/final-answer flow in 10.368 seconds, with three provider requests and two successful `run_js` calls. That separate observation is retained in the [implementation record](../development/stdlib-step2-runtime-implementation.md). Release preparation did not repeat real-provider calls.

Native execution was performed on Linux amd64; other targets have cross-build evidence. The tag-triggered workflow and downloaded release assets are checked separately after publication.


## v1.10.0 Release validation — 2026-10-19

- Annotated tag: `v1.10.0`, pointing to `a28b0537a9024cc0fb5da7f16753fa7e21ea0a1b`. The peeled remote tag matches the successful workflow commit and includes the checked source plus its preparation record.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/17): succeeded (API run ID 198, job ID 219).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.10.0): published, neither draft nor prerelease (Release ID 131). Its description matches the committed [release notes](v1.10.0.md).

The tag-triggered workflow passed the CGO-free unit/vet gates, built all six profile targets and uploaded the binaries plus `SHA256SUMS`. All seven assets were downloaded, and all six binary hashes matched the published manifest.

The downloaded Linux amd64 binary passed all 29 native CLI checks and the fixture-agent smoke (two local model requests, six hook invocations). Its main, Node-style Worker, nested Worker and Web Worker inventories exactly match the maintained-stdlib feature baseline. Checks cover version, arithmetic, imports, Intl, streams and process ownership, offline docs and aliases, positional I/O over TCP/Unix, signal delivery, timeout/cancellation, removed REPL behavior and legacy log rendering. No further real-provider calls were made during publication checks.

Embedded build information confirms the release-tag revision, `vcs.modified=false`, go-quickjs v0.27.0, all expected dependency versions and the absence of an embedded x/term dependency. The versioned ChatGPT helper matches the tagged source byte-for-byte.

Downloaded assets and verification records are under `/tmp/noe-v1.10.0-published-kdm90dws/`; gate logs, clean source, local builds, initial failure evidence and smoke records are under `/tmp/noe-v1.10.0-release-npfmrpt0/`. The repository-root `./noe` is the verified published Linux amd64 binary and reports `noe v1.10.0` (49,291,424 bytes).

This documentation-only verification commit follows publication without moving the release tag. Native execution on macOS, Windows and arm64 remains unverified.


## v1.11.0 preparation — 2026-10-19

Checked source revision: `378ed576d5c6d9f4801b345f3a728bc6aecd5e13`. Gates and profile builds used a clean detached worktree, excluding the unrelated `agent-server-build.jsonl`. This preparation follow-up adds only this record; the release tag includes it without changing the checked application or test source.

This release adds the `noe:http` entry point and incremental request uploads through the shared fetch implementation. It includes one-shot Request ownership, backpressure, abort/timeout and runtime cleanup, early-response preservation, English bundled docs, and a finite upload example. Existing entry points, finite-body replay, response streaming and read caps remain available. Incoming serve bodies still buffer up to the existing read cap; general full-duplex HTTP is not promised. See the [release notes](v1.11.0.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1 -timeout=180s` | Passed, 44.833 seconds |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1 -timeout=180s` | Passed, 41.48 seconds, including the prepared MCP SDK bundle |
| `CGO_ENABLED=0 go vet ./...` | Passed, 0.686 seconds |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | Passed, 0.719 seconds |
| `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1 -timeout=300s` | Passed, 67.228 seconds |
| Six CGO-free profile builds, `-trimpath -s -w -X noe/internal/cli.Version=v1.11.0` | Passed; all local SHA256SUMS entries verified |
| Existing native CLI checks | All 29 passed: base APIs, docs/owned IO, REPL removal, cancellation and legacy-log rendering |
| Fixture-agent smoke | Passed: agent→run_js, instruction/context/hooks/counters; two local requests, six hook invocations |
| Four-mode API inventory | Main, Node-style Worker, nested Worker and Web Worker match the feature baseline; 41 topics, 100 details, 26 Node-style modules |
| HTTP upload and docs | Prefix→ACK→remainder/EOF through a transferred Request; wire length unknown and chunked, response bytes correct, CLI/JS docs identical; finite upload example passed |
| Embedded dependencies and source information | Retained versions including go-quickjs v0.27.0, no x/term, checked revision and `vcs.modified=false` |

All five gates ran before the builds. Environment: Linux amd64, Go 1.27.2 and C toolchain for race. E2E reused the prepared MCP SDK 1.32.1 bundle at `/tmp/noe-repl-removal-20261019-yzglhqqf/mcp-client.bundle.mjs`. Local Linux amd64 size: 49,332,384 bytes. Logs, clean source, builds, checksums, scripts and smoke records are under `/tmp/noe-v1.11.0-release-2_dsnb0k/`.

Before the version update, the [HTTP entry-point](../development/stdlib-step3-http-implementation.md) and [upload-streaming](../development/stdlib-step4-upload-implementation.md) changes passed independent code review, API comparisons, unit/race/E2E/vet and six-target compilation. The authorized real-provider upload task completed on retry after concurrent POST/ACK instructions were clarified; its initial failure and harness correction are retained in the implementation record. No additional real-provider request was made for release preparation.

Native execution was performed on Linux amd64; other targets have compilation evidence. The tag-triggered workflow and downloaded release assets are verified separately after publication.


## v1.11.0 Release validation — 2026-10-19

- Annotated tag: `v1.11.0`, pointing to `689dd1b36b749be692568b266c2dd7209793cab9`. The peeled remote tag matches the successful workflow commit and includes the checked source plus its preparation record.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/18): succeeded (API run ID 199, job ID 220).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.11.0): published, neither draft nor prerelease (Release ID 132). Its description matches the committed [release notes](v1.11.0.md).

The tag-triggered workflow passed the CGO-free unit/vet gates, built all six profile targets and uploaded the binaries plus `SHA256SUMS`. All seven assets were downloaded, and all six binary hashes matched the published manifest.

The downloaded Linux amd64 binary passed all 29 native CLI checks and the fixture-agent smoke (two local model requests, six hook invocations). Its HTTP upload smoke also passed: a fresh Go receiver acknowledged the prefix before producer EOF, the transferred Request sent all seven bytes, CLI and JavaScript docs matched, and the finite upload example completed. Its main, Node-style Worker, nested Worker and Web Worker inventories exactly match the maintained-stdlib feature baseline. Checks cover version, arithmetic, imports, Intl, streams and process ownership, offline docs and aliases, positional I/O over TCP/Unix, signal delivery, timeout/cancellation, removed REPL behavior and legacy log rendering. No further real-provider calls were made during publication checks.

Embedded build information confirms the release-tag revision, `vcs.modified=false`, go-quickjs v0.27.0, all expected dependency versions and the absence of an embedded x/term dependency. The versioned ChatGPT helper matches the tagged source byte-for-byte.

Downloaded assets and verification records are under `/tmp/noe-v1.11.0-published-0yndwi_x/`; gate logs, clean source, local builds and smoke records are under `/tmp/noe-v1.11.0-release-2_dsnb0k/`. The repository-root `./noe` is the verified published Linux amd64 binary and reports `noe v1.11.0` (49,332,384 bytes).

This documentation-only verification commit follows publication without moving the release tag. Native execution on macOS, Windows and arm64 remains unverified.


## v1.12.0 preparation — 2026-10-19

Checked source revision: `65319265f65a4397771c37391204be829b5c606d`. The five gates and profile builds used a clean detached worktree; the unrelated `agent-server-build.jsonl` was excluded. This preparation entry is a documentation-only follow-up and changes no checked application or test source.

This release streams incoming HTTP bodies through the shared serve implementation, dispatching after headers and reading on demand. It includes bounded reads, input/response lifetime separation, early responses, Expect ordering, graceful/forced shutdown, English docs and a finite receive example. Existing entry points, read-cap behavior, outgoing streaming and WebSocket integration remain. General full-duplex HTTP is not guaranteed. See the [release notes](v1.12.0.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1 -timeout=180s` | Passed, 45.621 seconds |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1 -timeout=180s` | Passed, 41.499 seconds |
| `CGO_ENABLED=0 go vet ./...` | Passed, 0.755 seconds |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | Passed, 0.724 seconds |
| `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1 -timeout=300s` | Passed, 61.002 seconds (second attempt; see below) |
| Six CGO-free profile builds, `-trimpath -s -w -X noe/internal/cli.Version=v1.12.0` | Passed; all SHA256SUMS entries verified |
| Existing native CLI checks | All 29 passed: base APIs, docs/owned IO, REPL removal, cancellation and legacy logs |
| Fixture-agent smoke | Passed: agent/run_js, instructions, context, hooks and counters; two local model requests and six hook invocations |
| Four-mode inventory | Main, Node-style Worker, nested Worker and Web Worker match the feature baseline; 26 Node modules, 41 topics and 100 details |
| HTTP upload smoke | Transferred Request and prefix→ACK→remainder/EOF, binary bytes, matching CLI/JS docs and finite upload example passed |
| HTTP receive smoke | Independent client withholds remainder/EOF until handler ACK; seven bytes/sum689/HTTP200, matching CLI/JS docs, finite receive example and listener rebind passed |
| Build/source/dependencies | Checked revision, vcs.modified=false, pinned go-quickjs v0.27.0 and unchanged dependencies; no x/term |

The first full race attempt failed an unchanged nativeio test's immediate `runtime.Busy()` assertion (`TestNetListenTCPAddressAcceptAndIndependentClose`); it did not report a data race. The nativeio code and its tested initialization path have no changes in this release. Twenty focused race repetitions passed, then the same full race command passed on the unchanged checked commit. The failed log and first-attempt checks are retained as race-attempt1.log and checks-attempt1.json alongside race-retry.json. No test was skipped or relaxed and no product source changed to obtain the pass.

Environment: Linux amd64, Go 1.27.2, C toolchain for race. The short disk-backed Go temporary directory `/var/tmp/n12-xn9k3118` avoids the feature run's tmpfs capacity and Unix-socket path-length issues. E2E reused the prepared MCP SDK 1.32.1 bundle. Logs, clean source, local binaries, checksums and scripts are under `/home/agent/.cache/noe-v1.12.0-release-kgnfqe53`. Native execution on other targets was not performed.

Before the version update, the [feature record](../development/stdlib-step5-receive-implementation.md) passed independent code review with no remaining P0–P3, full unit/race/E2E/vet and six-target compilation. Its real OpenCode Go / deepseek-v4.1-flash receive task succeeded in one basic task, spontaneously consulting docs, receiving before EOF, saving the result, closing the listener and answering. The feature's reviewed 14-file manifest is unchanged. No additional real-provider request was made for this release preparation.

The tag-triggered workflow and downloaded release assets are verified separately after publication.


## v1.12.0 Release validation — 2026-10-19

- Annotated tag: `v1.12.0`, pointing to `61a3be28e2c250a4c88b5eef7960f73b0adf697d`. The peeled remote tag matches the successful workflow commit and includes the checked source plus the preparation record.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/19): succeeded (API run ID 200, job ID 221).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.12.0): published, neither draft nor prerelease (Release ID 133). Its description matches the committed [release notes](v1.12.0.md).

The workflow passed its CGO-free unit/vet gates, built all six targets, and uploaded six binaries plus SHA256SUMS. All seven assets were downloaded; all six binary hashes matched the manifest.

The downloaded Linux amd64 binary passed all 29 retained native CLI checks and the fixture-agent smoke (two local model requests, six hook invocations). Both HTTP smokes passed: outgoing transferred-Request streaming and incoming demand-driven receiving, with independent prefix acknowledgements before producer EOF, correct binary results, matching CLI/JavaScript docs, and finite upload/receive examples. The receive listener was released and rebound. Main, Node-style Worker, nested Worker, and Web Worker inventories exactly match the maintained feature baseline. No additional real-provider request was made during publication.

Embedded build information confirms the tagged revision, vcs.modified=false, pinned go-quickjs v0.27.0, unchanged dependency versions, and no x/term. The versioned ChatGPT helper matches the tagged source byte-for-byte. Downloaded assets and verification records are under `/home/agent/.cache/noe-v1.12.0-published-7txm_pzw`; gates, clean source, local builds and publication scripts are under `/home/agent/.cache/noe-v1.12.0-release-kgnfqe53`. The repository-root `./noe` was updated to the verified published Linux amd64 binary and reports `noe v1.12.0` (49361056 bytes).

This documentation-only verification commit follows publication without moving the release tag. Native execution on macOS, Windows and arm64 remains unverified.


## v1.13.0 preparation — 2026-10-19

Checked source revision: `def64e1f165a82858dfb03c634d3d35a92393bf0`. All five gates and profile builds used a clean detached worktree. The user's unrelated `agent-server-build.jsonl` was not copied, read, or staged. This preparation record is a documentation-only follow-up; the release tag includes it without changing checked application or test source.

This release contains Step6's internal standard-library composition, shared output ownership, per-Worker DNS loop binding, installation-error cleanup, and an English internal Go embedding example. Step7 is planned and independently reviewed, but remains unimplemented. The module remains `noe`, the library packages remain internal, and public `/v2` imports are not provided. The user's policy is to use v1.13.0 daily, fix small bugs, and decide on v2.0.0 after confidence from use. See the [release notes](v1.13.0.md).

| Check | Result |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1 -timeout=180s` | Passed, 41.642 seconds |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1 -timeout=180s` | Passed, 41.449 seconds |
| `CGO_ENABLED=0 go vet ./...` | Passed, 0.182 seconds |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | Passed, 0.202 seconds |
| `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1 -timeout=300s` | Passed, 61.247 seconds |
| Six CGO-free profile builds, `-trimpath -s -w -X noe/internal/cli.Version=v1.13.0` | Passed; SHA256SUMS entries verified |
| Native CLI checks | All 29 passed: base APIs, docs/owned IO, REPL removal, cancellation and legacy logs |
| Fixture agent | Passed: instructions, context, hooks, run_js and counters; two local model responses and six hook invocations |
| Four-mode inventory | Main, Node Worker, nested Worker and Web Worker match the Step6 baseline; 119/119/119/129 globals, 26 Node modules, 41 topics and 100 details |
| HTTP upload/receive | Independent prefix/ACK barriers, binary bytes, source ownership, CLI/JS docs, finite examples and listener rebind passed |
| Internal Go host example | Passed: docs lookup and borrowed IO pipe without CLI/agent imports |
| Build/source/dependencies | Checked revision, vcs.modified=false, pinned go-quickjs v0.27.0, unchanged dependencies and no x/term |

The initial gate run on `7497144de7659c76126cb8dcba73bd1a2a872cac` passed unit/E2E/vet but failed TestNetListenUnixPrePublishReplacementSurvives/file at its immediate Busy assertion, without a data-race report. The engine posts its Promise callback before ending the corresponding AsyncWork, so Promise completion alone did not join the native producer. A test-only correction adds a bounded Host.wg barrier before the unchanged Busy assertion in that test and the previously affected TCP close test. Independent limited review reported no P0–P3; twenty focused race repetitions passed. The final five gates use the corrected checked commit. Initial logs and checks-attempt1.json are retained; no production behavior or engine code was changed or test skipped.

Environment: Linux amd64, Go 1.27.2, C toolchain for race. Short disk-backed temporary path: `/var/tmp/n13-xtnwdxg2`. E2E reused the prepared MCP SDK bundle. Native execution on other targets was not performed. Logs, clean source, six local binaries, checksums and scripts are under `/home/agent/.cache/noe-v1.13.0-release-5gy52pum`.

The [Step6 verification record](../development/stdlib-step6-composition-implementation.md) already passed independent code review and limited re-review with no remaining P0/P1/P2, full unit/race/E2E/vet, six-target builds and test compilation. Its real OpenCode Go / deepseek-v4.1-flash basic task used ten model requests across an initial incomplete attempt and a successful retry that explicitly named noe:http. Both attempts are recorded; the successful retry spontaneously used docs, verified a loopback response, saved the result, closed its listener and answered. No additional real-provider request was made during release preparation.

The socket-test correction was independently reviewed by `/root/stdlib_step6_code_review` with P0/P1/P2/P3 all zero; its bounded completion wait leaves Busy, replacement identity and communication assertions in place. The review and focused-repeat evidence are saved as socket-review.json and socket-completion-race.log.

The tag-triggered workflow and downloaded release assets are verified separately after publication. This release does not claim that daily-use stabilization or Step7 implementation has finished.


## v1.13.0 Release validation — 2026-10-19

- Annotated tag: `v1.13.0`, pointing to `667b586b22b8dc5e42aa74f494e49625bf1bad5a`. The peeled remote tag matches the successful workflow commit and contains the checked source plus the preparation record.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/20): succeeded (API run ID 201).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.13.0): published, neither draft nor prerelease (Release ID 134). Its body matches the committed [English notes](v1.13.0.md).

The actual runner passed unit tests and vet, built all six targets and uploaded the six binaries plus SHA256SUMS. All seven assets were downloaded and every binary hash matched the published checksums. The downloaded Linux amd64 binary passed the native CLI checks, fixture agent/run_js/hooks, four-mode API inventory, independent HTTP upload and receive barriers, docs equality, finite examples and listener cleanup. No model-provider request was made for publication verification.

Build information confirms the tagged revision with vcs.modified=false, go-quickjs v0.27.0, unchanged dependency versions and no x/term. The versioned ChatGPT helper matches the tagged source. Published downloads and verification records are under `/home/agent/.cache/noe-v1.13.0-published-c5qpqpe8`; source, gates, local builds and publication scripts are under `/home/agent/.cache/noe-v1.13.0-release-5gy52pum`.

The repository-root `./noe` was updated from the verified published Linux amd64 binary and reports `noe v1.13.0` (49373344 bytes). Native execution on other targets remains unverified. This documentation-only follow-up does not move or replace the release tag. Daily-use stabilization before v2.0.0 remains future work, and Step7 remains planned and unimplemented.


## v1.14.0 preparation — 2026-10-19

Checked source revision: `932b474ba883a7e2947afc2a72a7df6f168b7329`. Gates and six profile builds used a clean detached worktree; unrelated `.gitignore`, `.zed/`, `agent-server-build.jsonl`, `_refs/`, and the repository-root distribution binary were excluded. This preparation entry is documentation-only and changes no checked application or test source.

This release includes Steps 7.1 and 7.2: canonical `/v2` source module, public `stdlib` and `agent` Go packages, shared internal agent bridge, explicit separate NOE execution and context-bound lifetimes, external consumer tests and English guides/examples. The executable remains on the v1 line. A v1.14.0 tag is not a valid v2 Go module version; development embedding uses the documented source checkout/local replacement until an authorized v2 publication. Daily-use stabilization and the v2 release remain future work. See the [English release notes](v1.14.0.md).

| Check | Result |
| --- | --- |
| `go test ./... -count=1 -timeout=300s` | Passed, 46.09 seconds |
| `go test -tags=e2e ./... -count=1 -timeout=300s` | Passed, 42.664 seconds |
| `go vet ./...` | Passed, 0.913 seconds |
| `go vet -tags=e2e ./...` | Passed, 0.949 seconds |
| `go test -race ./internal/... ./stdlib/... ./agent/... -count=1 -timeout=300s` | Passed, 70.249 seconds |
| Six CGO-free profile builds | Passed, `-trimpath -s -w -X forge.harakara.site/littleisland/noe/v2/internal/cli.Version=v1.14.0`; SHA256SUMS verified |
| Native Linux CLI checks | All 29 passed; fixture agent ran two local requests and six hooks |
| Four-mode inventory | Identical baseline: globals 119/119/119/129, 26 Node modules, 41 topics and 100 details |
| HTTP upload/receive | Prefix/ACK before EOF, binary bytes, CLI/JS docs equality, finite examples and listener rebind passed |
| Go embedding and external agent | Generic example, agent example and outside consumer reference passed with the v1.14.0 executable |
| Build/source/dependencies | Checked revision, vcs.modified=false, go-quickjs v0.27.0, unchanged dependency versions and no x/term |

The first E2E attempt failed only because `TestEvalAndCLICommands/version` still expected `noe v1.13.0`. That expectation was corrected to v1.14.0 in a test-only commit, and the full E2E gate passed. The failed log/checks and exact one-line diff are retained as e2e-attempt1.log, checks-attempt1.json and version-test-correction.diff. The initial unit result and native/embedding smokes were retained from `ceab45505bf6977144199c3f16eea3a65573e199` because the sole subsequent change was that E2E expectation; application and default-unit source are identical. Builds were regenerated at the corrected checked revision and their clean build information and checksums reverified. No test was weakened or skipped.

Before release preparation, [Step7.1](../development/stdlib-step7-library-implementation.md) and [Step7.2](../development/stdlib-step72-agent-implementation.md) passed independent review (final P0–P3 all zero), full unit/race/E2E/vet, outside local-replace and synthetic no-replace artifact consumers, and six-target compilation. The generic stdlib closure remains 257 packages with no agent/provider SDK dependency. Step7.2's actual external Go consumer used OpenCode Go / deepseek-v4.1-flash / effort auto, three model requests and two run_js calls to save and reread count3/total31, then verify task completion and a final answer. No additional real-provider requests were made for this release.

Environment: Linux amd64, Go 1.27.2 and C toolchain for race. Go temporary directory: `/var/tmp/n14`. E2E reused the prepared MCP SDK bundle. Raw logs, clean source, six binaries, checksums and scripts: `/home/agent/.cache/noe-v1.14.0-release-bcw68s4d`. Native execution on other targets is unverified. The tag-triggered workflow and downloaded assets are verified separately after publication.


## v1.14.0 Release validation — 2026-10-19

- Annotated tag: `v1.14.0`, pointing to `d086281c691b541daf929f507bfca0e4ef81eeca`. The peeled remote tag matches the successful workflow commit and includes the checked code plus its preparation record.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/21): succeeded (API run ID 202).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.14.0): published, neither draft nor prerelease (Release ID 135). Its body matches the committed [English notes](v1.14.0.md).

The actual runner passed its CGO-free unit/vet gates and built all six profile targets. All seven published assets were downloaded; all six binary hashes matched SHA256SUMS. The downloaded Linux amd64 binary reports `noe v1.14.0` and passed all 29 native CLI checks, fixture agent/run_js/hooks, the unchanged four-mode API inventory, HTTP upload/receive prefix-before-EOF barriers, docs equality, finite examples and listener rebind. The external Go consumer reference also passed against this downloaded binary, including real child NOE execution, provider/decision/image injection, queue/observe/interrupt, active-request Close cleanup and Auth fixtures. No additional model-provider request was made during publication.

Build information confirms the tagged revision with vcs.modified=false, go-quickjs v0.27.0, unchanged dependency versions and no x/term. The versioned ChatGPT helper matches the tagged source. Published downloads are under `/home/agent/.cache/noe-v1.14.0-published-6oyku759`; raw checks, local builds and publication records are under `/home/agent/.cache/noe-v1.14.0-release-bcw68s4d`. The existing repository-root binary was preserved. Runtime behavior on non-Linux targets remains unverified.

This documentation-only follow-up does not move the release tag. The CLI release stays on v1; there is no v2 tag or claim of a downloadable v2 Go module. Daily use and small bug fixes precede the decision to publish v2.0.0.


## v1.14.1 preparation — 2026-10-20

Checked source revision: `1a137150b2f298c415afa2ae3cb03bf5cede4ab6`. Gates and all six profile builds used a clean detached worktree. The unrelated `agent-server-build.jsonl` and ignored local files were excluded. This preparation entry and the release-note validation paragraph are documentation-only follow-ups and change no checked application or test source.

This patch adds fixed built-in agent-provider retries (three attempts, 2-second and 4-second waits), preserves cancellation and committed history, avoids replaying hooks/tools, and keeps retry-specific events disabled. It also includes the typed two-line observer labels, consistent short control IDs with `agents --full`, concise context percentages, and one terminal context update per model request. Complete but invalid model responses are distinguished from stream transport failures so output-limit/refusal responses are not treated as retryable disconnections. See the [release notes](v1.14.1.md).

| Check | Result |
| --- | --- |
| `go test ./... -count=1 -timeout=300s` | Passed, 46.228 seconds |
| `go test -tags=e2e ./... -count=1 -timeout=300s` | Passed, 50.505 seconds |
| `go vet ./...` | Passed, 0.93 seconds |
| `go vet -tags=e2e ./...` | Passed, 0.928 seconds |
| `go test -race ./internal/... ./stdlib/... ./agent/... -count=1 -timeout=300s` | Passed, 69.954 seconds |
| Six CGO-free profile builds with `-trimpath`, stripped symbols and version `v1.14.1` | Passed; all local SHA256SUMS entries verified |
| Linux amd64 binary | Reports `noe v1.14.1`; build info matches the checked revision with `vcs.modified=false` |
| Built binary with local HTTP fixtures | Passed: one completed tool, HTTP 503 then truncated SSE then recovery, identical retry inputs, two logical requests/four HTTP attempts, six hook calls, no extra model/context/retry events |
| Shared retry and adapter fixtures | Passed: attempts at 0/2/6 seconds, exhaustion, permanent errors, cancellation/deadline during wait, isolated response state across both SDKs and Chat/Responses APIs |

The first E2E gate used `/var/tmp/n141-vkkqk2vc` as its temporary directory and failed only the Unix-socket observation case because its generated socket pathname exceeded the OS limit of 107 bytes. The same source and assertions passed with the shorter temporary directory `/tmp/n141`. The initial log and checks-attempt1.json are preserved; no application or test source was changed to bypass the failure.

Environment: Linux amd64, Go 1.27.2 and a C toolchain. E2E used the prepared MCP SDK bundle. Logs, clean checkout, local binaries, checksums and verification scripts are under `/home/agent/.cache/noe-v1.14.1-release-uegu4t7l`. No real model-provider request was made. Cross-builds establish compilation only; runtime behavior on macOS, Windows and arm64 remains unverified. The tag-triggered workflow and downloaded Release assets are verified separately after publication.


## v1.14.1 Release validation — 2026-10-20

- Annotated tag: `v1.14.1`, pointing to `877cea99604dc8dae2e8a87cd444c6538f9a320e`. The peeled remote tag matches the successful workflow commit and includes the checked code plus its documentation-only preparation record.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/22): succeeded (API run ID 203).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.14.1): published, neither draft nor prerelease (Release ID 136). Its body matches the committed [English notes](v1.14.1.md).

The workflow passed the CGO-free unit-test and vet gates, built all six targets and uploaded six binaries plus SHA256SUMS. All seven assets were downloaded, and all six binary hashes matched the published checksums. The downloaded Linux amd64 binary reports `noe v1.14.1`, passed arithmetic, `agents --full` help and two-line context percentage checks, and recovered from a fixture HTTP 503 followed by a truncated SSE stream. Its retry input stayed identical, one completed tool ran once, six hook invocations were retained, two logical requests produced four HTTP attempts, and no intermediate model/context/retry events were added. Previously emitted partial thinking remained in JSONL. No real model-provider request was made.

Build information matches the tagged revision with `vcs.modified=false` and go-quickjs v0.27.0. The verified published Linux amd64 binary was installed to both the repository-root `./noe` and `~/.local/bin/noe`; their bytes match the downloaded asset. Downloads and verification are under `/home/agent/.cache/noe-v1.14.1-published-9881qqfr`, with pre-release evidence and publication scripts under `/home/agent/.cache/noe-v1.14.1-release-uegu4t7l`. Other targets have compilation evidence, not native execution validation.

This documentation-only follow-up does not move or replace the release tag. The release remains on the v1 executable line and does not publish a v2 Go module.

## v1.15.0 preparation — 2026-10-20

Application revision checked: `5680bd689aa0ed252b7234fe3fb563a3200007ac`. The corrected test-only revision is `e413cb67aff00111f9c39dfcdcb44d18c3635aac`; its sole source difference is the native socket-abort test synchronization described below. The release tag includes these changes plus this documentation-only preparation record.

This release adds `wait_task({agentId, commandId})`, retained task outcomes, initial-task command IDs in spawn receipts, the `.noe/events/` automatic log directory, and the Jev task-loop example. Queue acceptance/FIFO, human control commands, and observation output are preserved. The executable remains on the v1 line; no v2 Go module is published. See the [release notes](v1.15.0.md).

| Check | Result |
| --- | --- |
| `go test -p 2 -tags=e2e ./... -count=1` | Passed, 101.80 seconds; includes all default packages and the prepared local MCP SDK bundle |
| `CGO_ENABLED=1 go test -race -p 2 ./internal/... ./stdlib/... -count=1` | All packages except nativeio passed on the initial run; the corrected nativeio package passed its full race rerun below |
| `CGO_ENABLED=1 go test -race ./internal/nativeio -count=1` | Passed, 6.31 seconds, at the corrected test revision |
| Corrected `TestNetConnectValidationAndAbort` with race, `-count=20` | Passed |
| `go vet -tags=e2e ./...` | Passed |
| Six targets with `CGO_ENABLED=0 go list -export ./...` | Passed; compile-only, no local distribution artifacts |
| Native CGO-free binary | Reports `noe v1.15.0`; clean build information matches the application revision |
| Native binary with a local HTTP fixture | Passed: initial and queued task waits, concurrent waits, repeated retained result, exactly two model requests, no wait-specific events, bundled API docs, arithmetic, and bare `--events` saving under `.noe/events/` |

The initial race run caught an existing test synchronization error in `TestNetConnectValidationAndAbort`: Promise settlement can precede the producer returning from AsyncWork.Complete. The test checked Runtime.Busy before joining that producer. It now waits for the native host's producer wait group, following the existing adjacent test's pattern, before keeping the same retained-work assertion. Production code is unchanged. The initial failed log and all unaffected package passes are retained as `race-attempt1.log/json`; the corrected nativeio package and the targeted repeated race check passed. No assertion was removed or weakened.

The earlier real-provider workflow experiments completed before this release preparation. No new model-provider requests were made for the release checks. The Jev example's initial failing implementation is intentional and documented; copied corrected output passed its five acceptance tests.

Environment: Linux amd64, Go 1.27.2 and C toolchain. Clean checkout, checks, native binary, and publication scripts are under `/var/tmp/noe-v1.15.0-release-erkqfcd9`. Other targets have compilation evidence, not native runtime validation. The tag-triggered workflow and downloaded Release assets are verified separately after publication.

## v1.15.0 Release validation — 2026-10-20

- Annotated tag: `v1.15.0`, pointing to `f381b42299b7772597070f7067b54dd9f6eb7fe5`. The peeled remote tag matches the successful workflow commit.
- [Release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/23): succeeded (API run ID 204, job ID 225).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.15.0): published, neither draft nor prerelease (Release ID 137). Its body matches the committed [English notes](v1.15.0.md).

The workflow passed its CGO-free unit-test and vet gates, built all six profile targets, and uploaded six binaries plus SHA256SUMS. All seven assets were downloaded; all six binary hashes matched the published checksums.

The downloaded Linux amd64 binary reports `noe v1.15.0` and passed arithmetic, bundled `wait_task` docs, and the local provider fixture covering initial/queued task IDs, concurrent task waits, repeated retained outcomes, exactly two model requests, no wait-specific events, and automatic event saving under `.noe/events/`. No external model-provider request was made.

Build information matches the tagged revision with `vcs.modified=false` and go-quickjs v0.27.0. The verified published Linux amd64 binary was installed to both repository-root `./noe` and `~/.local/bin/noe`; their bytes match the downloaded asset. Downloads, checksums, build information, fixture output, and publication records are under `/var/tmp/noe-v1.15.0-release-erkqfcd9`. Other targets have compilation evidence, not native execution validation. This record does not move or replace the release tag.


## v1.15.1 preparation — 2026-10-20

Checked source revision: `b997b082452b085013fdd649f1d670eae0fb0af8`, in a clean detached worktree. The release tag includes this source plus a documentation-only preparation record. The unrelated untracked `.noe/` and `agent-server-build.jsonl` were excluded.

This patch retains synchronous command failure details, isolates test agents from inherited connections, and expands run_js instructions and bundled recipes. See the [release notes](v1.15.1.md). Dependencies are unchanged and the executable remains on the v1 release line.

| Check | Result |
| --- | --- |
| CGO-free full unit suite | Passed, 70.4 seconds |
| Race: internal, stdlib and public agent packages | Passed, 178.15 seconds |
| E2E package with the prepared MCP SDK bundle; other packages passed in the preceding full-suite attempts | Passed, 48.31 seconds |
| Vet with E2E build tags | Passed, 7.32 seconds |
| CGO-free vet | Passed, 7.08 seconds |
| All six profile targets, CGO-free link-free compilation | Passed, 17.74 seconds |
| Native CGO-free binary | Reports `noe v1.15.1`; clean build information matches the checked revision |
| Native fixture checks | Passed: execFileSync/execSync failure code, stdout and stderr; unchanged success return; all six run_js recipes; initial/queued task waits and retained results; exactly two fixture model requests; no wait events; automatic event path; arithmetic |

The initial E2E attempt passed all packages except the Unix/observe socket case, whose generated pathname exceeded the OS limit of 107 bytes under `/var/tmp/n151-udm7sc87`. A second attempt changing only TMPDIR still used the original long pathname. The unchanged E2E package passed with both TMPDIR and GOTMPDIR set to `/var/tmp/n151`; all other packages passed in both full-suite attempts. Both failed logs and results are retained as `e2e-attempt1.log/json` and `e2e-attempt2.log/json`; no source or assertion was changed to bypass the failure. All test gates inherited an intentionally invalid `NOE_AGENT_CONNECTION`, verifying test-suite isolation while managed fixture children still worked.

Environment: Linux amd64, Go 1.27.2 and C toolchain for race. Evidence, clean checkout, native binary and publication scripts: `/var/tmp/noe-v1.15.1-release-xrvhlgw6`. No external model-provider request was made. Other targets have compilation evidence only. Workflow publication and downloaded assets are verified separately after tagging.


## v1.15.1 Release validation — 2026-10-20

- Annotated tag: `v1.15.1`, pointing to `1341d0820bfe0c8ddd36b5f8041240b3a74113ed`. The peeled remote tag matches the successful workflow commit.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/24): succeeded (API run ID 205).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.15.1): published, neither draft nor prerelease (Release ID 138). Its body matches the committed [English notes](v1.15.1.md).

The workflow passed its CGO-free unit and vet gates, built all six profile targets, and uploaded six binaries plus SHA256SUMS. All seven assets were downloaded; all six binary hashes matched the published checksums.

The downloaded Linux amd64 binary reports `noe v1.15.1`. Both execFileSync and execSync retained exit code 7, stdout and stderr on failure, and successful execution still returned a string. All six run_js recipe lookups, initial and queued task waits, repeated retained outcomes, exactly two local fixture model requests, automatic event saving, arithmetic and the absence of wait-specific events passed. No external model-provider request was made.

Build information matches the tagged revision with vcs.modified=false and go-quickjs v0.27.0. The verified published binary was installed to repository-root `./noe` and `~/.local/bin/noe`; both match the downloaded bytes. Evidence and backups: `/var/tmp/noe-v1.15.1-release-xrvhlgw6`. Other targets have compilation evidence only. This documentation-only follow-up does not move the release tag.


## v1.15.2 preparation — 2026-10-21

Application revision checked: `3d17b3b3aa59d36be22313fdc5dc5713b6cd8ab2`, in a clean detached worktree. The test-only follow-up `f7dec0078139faab8fc935b856779d240a9b4ab0` updates the E2E version expectation from v1.15.1 to v1.15.2. The release tag includes both revisions and this documentation-only preparation record. Unrelated untracked `.noe/` files and `agent-server-build.jsonl` are excluded.

This patch fixes saved tool-output reuse, provider HTTP header merging, stream signal cleanup, and rooted-filesystem symbolic-link entry selection. It also clarifies agent guidance and current Go embedding documentation. See the [release notes](v1.15.2.md). Dependencies are unchanged; the executable remains on the v1 release line and does not publish a v2 Go module.

| Check | Result |
| --- | --- |
| CGO-free full unit suite | Passed, 64.52 seconds |
| Race: internal, stdlib and public agent packages | Passed, 188.96 seconds |
| Corrected E2E package, including MCP integration; other packages passed in the full-suite attempt | Passed, 51.79 seconds |
| Vet with E2E build tags | Passed, 2.69 seconds |
| CGO-free vet | Passed, 0.93 seconds |
| All six profile targets, CGO-free link-free compilation | Passed, 11.32 seconds |
| Native CGO-free binary | Reports `noe v1.15.2`; build information matches the checked application revision with `vcs.modified=false` |
| Native fixture checks | Passed: synchronous command errors and unchanged success return, all six run_js recipes, initial/queued task waits, retained outcomes, exactly two fixture model requests, automatic event saving, no wait-specific events, bundled docs and arithmetic |
| Changed Go formatting, streams.js Biome lint and committed diff check | Passed |

The first full E2E suite failed only because its version-display expectation still said v1.15.1 while the binary correctly reported v1.15.2. All other packages passed. The single expectation was corrected without changing application source or weakening the assertion; the full E2E package then passed from a separate clean worktree. The initial failed result and log are retained as `e2e.json` and `e2e.log`, with the corrected result in `e2e-corrected.json/log`. No full-suite rerun was needed for this test-only correction.

The earlier small-improvement validation had intermittent native network-test Busy assertions, as recorded in the [implementation record](../development/small-improvements-20261020.md). This release race run passed once without changing those tests or networking code. It does not establish that the previously observed intermittency is fixed.

The host shutdown interrupted preparation after the application commit and validation worktree creation, before push or tagging. The persistent worktree and scripts were recovered. The MCP SDK bundle formerly under /tmp was rebuilt offline from the local npm cache using the same pinned SDK 1.32.1 and esbuild 0.25.12; it is preserved under this release evidence directory. Test gates use an intentionally invalid inherited NOE_AGENT_CONNECTION, and short TMPDIR/GOTMPDIR paths avoid the known Unix-socket pathname limit.

Environment: Linux amd64, Go 1.27.2 and C toolchain for race. Evidence, clean worktrees, native binary, MCP bundle and publication scripts: `/var/tmp/noe-v1.15.2-release-8j6osrl8`. No external model-provider request was made. Other targets have compilation evidence only. Workflow publication and downloaded assets are verified separately after tagging.


## v1.15.2 Release validation — 2026-10-21

- Annotated tag: `v1.15.2`, pointing to `f66a751ef3938a302d1bed28fcf5826d907b5518`. The peeled remote tag matches the successful workflow commit.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/25): succeeded (API run ID 206).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.15.2): published, neither draft nor prerelease (Release ID 139). Its body matches the committed [English notes](v1.15.2.md).

The workflow passed its CGO-free unit and vet gates, built all six profile targets, and uploaded six binaries plus SHA256SUMS. All seven assets were downloaded; all six binary hashes matched the published checksums.

The downloaded Linux amd64 binary reports `noe v1.15.2`. Both execFileSync and execSync retained exit code 7, stdout and stderr on failure, and successful execution still returned a string. All six run_js recipe lookups, initial and queued task waits, repeated retained outcomes, exactly two local fixture model requests, automatic event saving, arithmetic and the absence of wait-specific events passed. No external model-provider request was made.

Build information matches the tagged revision with vcs.modified=false and go-quickjs v0.27.0. The verified published binary was installed to repository-root `./noe` and `~/.local/bin/noe`; both match the downloaded bytes. Evidence and backup: `/var/tmp/noe-v1.15.2-release-8j6osrl8`. Other targets have compilation evidence only. This documentation-only follow-up does not move or replace the release tag.


## v1.16.0 preparation — 2026-10-21

Checked source revision: `5b820bbca869fb40663329789f1e9f06c3f3cd65`, in a clean detached worktree. The release tag includes this source plus this documentation-only preparation record. Unrelated untracked `.noe/` files and `agent-server-build.jsonl` are excluded.

This release combines stable per-instance sessions, explicit redelegation admission, retained context-status history, ranged task results and ChatGPT cache affinity with repository-wide test cleanup. See the [release notes](v1.16.0.md). Provider dependencies and agent lifetimes are unchanged; delegation workflow guidance remains external and no v2 Go module is published.

| Check | Result |
| --- | --- |
| CGO-free full unit suite | Passed, 33.62 seconds |
| Race: internal, stdlib and public agent packages | Passed, 142.06 seconds |
| Full E2E suite, including prepared MCP SDK bundle | Passed, 80.32 seconds |
| Long RPC slow integration gate | Passed, 35.05 seconds |
| Vet with E2E tags | Passed, 0.90 seconds |
| CGO-free vet | Passed, 0.86 seconds |
| All six profile targets, CGO-free link-free compilation | Passed, 21.78 seconds |
| Opt-in live probe compilation, no provider requests | Passed, 0.31 seconds |
| Provider-cache SDK probe build | Passed, 4.02 seconds |
| Native CGO-free binary | Reports `noe v1.16.0`; build info matches the checked source with `vcs.modified=false` and go-quickjs v0.27.0 |
| Native local HTTP fixture | Passed: two queued tasks keep one session ID and distinct run IDs, metadata-only waits, retained full and ranged results, exactly two model requests, automatic event saving, synchronous command errors, six run_js recipes and arithmetic |
| Independent cleanup review and bounded re-review | Passed after restoring the second final assistant → third queued instruction assertion; targeted queue test also passed |

The review covered all 43 changed files against the preceding cache-fix revision. It found one test-coverage regression in the ordered queue-history assertion; commit `5b820bb` restores that check, and the complete final suite above includes the correction. Earlier successful unit/vet/probe checks are retained separately as before-review evidence. No product behavior was changed to accommodate a failing test. Stale test fixtures now match retained status history, stable sessions and explicit delegation permission; the context-policy fixture still distinguishes inherited 0.2 from default 0.1 trimming.

Retired low-level and OpenCode investigation implementations are removed from the active suite, with historical findings and source revision preserved. Repeated CLI builds are shared, single-attempt classification avoids duplicated backoff waits, startup/RSS measurements are benchmarks, and the long RPC test remains an explicit pre-release gate. Test/probe cleanup removes a net 1,919 lines.

Environment: Linux amd64, Go 1.27.2 and C toolchain for race. Checks inherited an intentionally invalid `NOE_AGENT_CONNECTION`; both TMPDIR and GOTMPDIR use a short path to avoid the known Unix-socket pathname limit. MCP uses the same offline-prepared SDK 1.32.1 bundle from v1.15.2. Evidence, clean checkout, native binary and publication scripts: `/var/tmp/noe-v1.16.0-release-20261021`. No external model-provider request was made for these release gates. Other targets have compilation evidence only. Workflow publication and downloaded assets are verified separately after tagging.


## v1.16.0 Release validation — 2026-10-21

- Annotated tag: `v1.16.0`, pointing to `55e16eb73eafd227cdca9ffbe878e85bc97b7dab`. The peeled remote tag matches the successful workflow commit; its only change from the checked source is the documentation-only preparation record.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/26): succeeded (API run ID 207, job ID 228).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.16.0): published, neither draft nor prerelease (Release ID 140). Its body matches the committed [English notes](v1.16.0.md).

The workflow passed its CGO-free unit and vet gates, built all six profile targets, and uploaded six binaries plus SHA256SUMS. All seven assets were downloaded; all six binary hashes matched the published checksums.

The downloaded Linux amd64 binary reports `noe v1.16.0`. The local HTTP fixture confirmed that two queued tasks keep the same session ID and separate run IDs, metadata-only waits omit answers, retained answers can be fetched whole or in byte ranges, repeated waits preserve the initial result, and exactly two model requests occur. Automatic event saving, the absence of wait-specific events, synchronous command failure details, all six run_js recipes, bundled docs and arithmetic also passed. No external model-provider request was made.

Build information matches the tagged revision with `vcs.modified=false` and go-quickjs v0.27.0. The verified published Linux amd64 binary was installed to repository-root `./noe` and `~/.local/bin/noe`; both match the downloaded bytes. Evidence and backup: `/var/tmp/noe-v1.16.0-release-20261021`. Other targets have compilation evidence only. This documentation-only follow-up does not move or replace the release tag.


## v2.0.0 preparation — 2026-10-21

At completion of local preparation, publication was pending. No v2.0.0 tag,
push, Release upload, or installed-binary replacement was performed in that stage. The release target is the
CLI and the first stable `forge.harakara.site/littleisland/noe/v2` module tag.
See the [release notes](v2.0.0.md) and [migration guide](../v2-migration.md).

Checked base revision: `fa43be492122e930e9299089f69f7513c3908031`, plus the
local v2.0.0 version/test updates, release definitions and documentation, and
the test-only native producer join described below. The default CLI version,
unit and E2E expectations are v2.0.0. Engine and provider dependencies are unchanged.
`/var/tmp/noe-v2-release-prep-w7neo8yb/inputs.json` records the exact tested code/profile hashes; the final
changed-file manifest and patch are kept in the same evidence directory.

The release profile and active reference commands now include public
`stdlib/...` and `agent/...` in race checking and replace the removed long-RPC
test with `TestGoClientLongModelAndToolWait` in `./agent`. Before correction,
the stale command returned success with `[no tests to run]`; the corrected
verbose gate actually executed all four model/tool wait and cancellation cases.
The tag-triggered workflow's unit/vet/build/upload behavior remains unchanged,
including all six targets and the v2 ldflags path.

| Preparation check | Result |
| --- | --- |
| CGO-free full unit suite | PASS, 17.504s |
| Race: internal, stdlib and public agent packages | PASS, 82.917s after the test-only correction below |
| Full E2E with prepared MCP SDK bundle | PASS, 49.901s |
| Sustained Go Client waits and cancellation | PASS, 30.669s; model/tool normal cases each waited 30s, both cancellation cases passed |
| CGO-free vet with E2E tags | PASS, 0.366s |
| Six profile targets, CGO-free link-free compilation | All PASS, 7.030s total |
| External stdlib consumer | local replace and isolated module artifact build/run PASS; six consumer cross-builds PASS |
| External model/auth/log/Go Host consumer | both portable and Go Host modes PASS with local replace and isolated module artifact |
| Native release-flags build | Reports `noe v2.0.0`; arithmetic and lazy agent/model/log bindings PASS without provider calls |
| Profile/workflow/docs | YAML parsed, current check names/paths and shell syntax verified; actionlint PASS excluding its unsupported Forgejo context; equivalent temporary GitHub-context copy also PASS; local links and diff checks PASS |

The first full race attempt failed `TestNetStartSignalStopsOnlyPendingDial`
with `closed connection retained active work: true`, without a DATA RACE warning.
The test awaited Promise settlement and then immediately checked Runtime.Busy.
As in previously corrected adjacent socket tests, AsyncWork.Complete can post
its Promise callback before the producer's final Done. The test now uses the
existing bounded `waitNetOperations` barrier before the same Busy assertion.
Communication, late-abort, and retained-work assertions remain intact; no
production networking code, skips, or expected values changed. The exact
first-attempt scheduling cannot be established from its log alone. Thirty
focused race repetitions passed, followed by the complete final gates above.
`attempt1-race.log`, `attempt1-results.json` and `nativeio-focused-race.log`
retain the failure and follow-up. This does not claim to eliminate every
previously recorded intermittent socket assertion.

Earlier live validation on the checked base, whose functional implementation
is unchanged by this preparation:

- All 41 JS/MJS example files were exercised through 40 execution checks,
  9 HTTP server fixture tests and one real Firefox check. OpenRouter,
  OpenCode Go, ChatGPT, image generation/reading, Exa and task-loop reviews
  used actual services. Full file mapping and results:
  `/var/tmp/noe-examples-v2-glpym7ir/REPORT.md` and `results.json`.
- A single NOE builder agent created a separate-module Go HTTP daemon using
  stdlib, model.Service and Host/Client directly, without a NOE executable
  dependency. Build, three Go tests, repeated real-agent calculations,
  health under load, deadlines/disconnect recovery and live-tool shutdown
  passed. SIGINT joined agent/tool plus eval in 0.032s; the observer's
  SIGTERM check exited normally in 0.153s. Evidence:
  `/var/tmp/noe-resident-otwho36_/MONITOR_REPORT.md` and app logs.
- The supplemental daemon test initially assumed live stdout notification.
  Its failed evidence was retained and its marker-based retry passed.
  A separate local-provider comparison of old 1.16.0 and v2 showed both
  deliver captured stdout after the tool ends; this was not a v2 regression.
- A 64x64 quadrant image was once described incorrectly by the model; a
  generated sailboat image was described correctly on follow-up. The small
  image misrecognition cause is unconfirmed and no image transport regression
  was established. Long-term continuous operation was not tested.

Environment: Linux amd64, Go 1.27.2; C toolchain for race. Other platforms have
compilation evidence only. New preparation gates use fixtures and make no real
provider calls. The MCP bundle is the existing offline SDK 1.32.1/esbuild 0.25.12
artifact. A short TMPDIR/GOTMPDIR avoids Unix pathname limits. Detailed logs,
commands, results, native binary and independent consumer artifacts:
`/var/tmp/noe-v2-release-prep-w7neo8yb`. The consumer's v2.0.0-dev.0 is a local artifact-test version;
it is not a downloaded published release.

Before tagging, commit the prepared changes and identify that commit in the
publication record. At authorized publication, update the preparation labels
in the READMEs/release index/notes, create and push the v2.0.0 tag, then verify
the workflow, Release body and all downloaded assets/checksums. Also create a
clean module outside this checkout and fetch/build/run the published
`forge.harakara.site/littleisland/noe/v2/stdlib@v2.0.0` and agent example without
local replace or the synthetic proxy. Record selected module/engine versions;
only that post-publication check establishes real module availability.


## v2.0.0 publication authorization and focused test review — 2026-10-21

The user explicitly authorized release of v2.0.0 after reviewing preparation
and requesting an independent critical examination of test integrity. The
review found no mandatory finding or release blocker in its focused scope.
It confirmed that the native producer wait does not close/cancel resources,
that real OS process cleanup tests remain despite removal of obsolete
per-VM PID checks, and that cancellation/queue/result/control assertions have
replacement coverage through public Go or actual CLI execution.

Two temporary overlays deliberately broke socket keepalive release and
changed task cancellation into instance-wide cancellation. Existing tests
failed on Busy=true and the incorrectly cancelled remaining queue,
respectively. An independent focused race run passed 30 repetitions. This
was a focused review of changed/deleted tests and major replacements, not an
exhaustive one-to-one audit of every test. No source changed during review;
524 source hashes were preserved. Report and copied negative-control logs:
`/var/tmp/noe-v2-release-prep-w7neo8yb/test-integrity-review/`.

The tag includes the already tested preparation and documentation finalization.
Publication, workflow execution, actual module download and downloaded-binary
verification are recorded separately below after they complete.


## v2.0.0 Release validation — 2026-10-21

- Annotated tag: `v2.0.0`, pointing to `0dc3cbf7a57062d07439536cc70b83640e82c2a1`. The peeled remote tag matches the successful workflow commit.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/27): succeeded (API run ID 208, job ID 229).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.0.0): published, neither draft nor prerelease (Release ID 141). Its body matches the committed [English notes](v2.0.0.md).

The actual workflow passed the CGO-free unit and vet gates, built Linux/macOS/
Windows binaries for amd64 and arm64 with the v2 ldflags path, and uploaded all
six binaries plus SHA256SUMS. All seven assets were downloaded, and all six
binary hashes matched the published checksum file.

The downloaded Linux amd64 binary reports `noe v2.0.0`. Build information
matches the tagged commit with `vcs.modified=false`, `CGO_ENABLED=0` and
`go-quickjs v0.27.0`. Native local-fixture smoke checks passed: two queue tasks
keep one session ID and separate run IDs, metadata-only waits omit answers,
retained full/ranged reads and repeated waits preserve both task outcomes,
exactly two local model requests occur, automatic events contain no wait-only
events, synchronous command failures retain stdout/stderr and exit 7,
all six run_js recipe lookups and arithmetic succeed. The instance is stopped
using the v2 `stop_agent` API. These publication checks made no real model calls.

The first stable Go module was also verified from two clean modules outside
the checkout. The actual commands fetched:

```sh
go get forge.harakara.site/littleisland/noe/v2/stdlib@v2.0.0
go get forge.harakara.site/littleisland/noe/v2/agent@v2.0.0
```

Neither module uses local replace or the synthetic artifact proxy. Both
examples built and ran, `go mod verify` passed, and selected versions are
`forge.harakara.site/littleisland/noe/v2 v2.0.0` and
`github.com/go-quickjs/go-quickjs v0.27.0`. The agent example returned
`The total is 31.` using its deterministic provider. Downloaded module files
`go.mod`, `agent/host.go`, `stdlib/profile.go` and `internal/cli/cli.go` match
the tagged source bytes. This establishes published-module retrieval in
addition to the earlier local/artifact consumer tests.

The verified published Linux amd64 binary was atomically installed to
repository-root `./noe` and `~/.local/bin/noe`; both match the downloaded bytes
and report v2.0.0. Previous binaries were backed up before replacement.
Evidence, downloaded assets, source/version checks, smoke logs and backups:
`/var/tmp/noe-v2.0.0-release-jmsxcwez`. Other targets have build/checksum evidence only;
native macOS/Windows/arm64 execution and long-term continuous operation were
not tested. This documentation-only follow-up does not move or replace the
v2.0.0 release tag.


## v2.0.1 preparation — 2026-10-21

Checked source revision: `37b55fd38ce2d89ab20116a4751c72db7c4d6510` in a clean detached
worktree. The release tag adds this documentation-only evidence record without
changing the tested application or test source. The unrelated active agent
JSONL under `.noe/events/` is excluded.

This patch displays human CLI queue/steer instructions in observation output
and adds the resident Go stdlib HTTP host example. CLI/JS/Go call signatures
and return values, the Go toolchain requirement, engine and provider dependencies
are unchanged. Accepted command events now include instruction text; subsequent
states omit it. See the [release notes](v2.0.1.md).

| Pre-tag check | Result |
| --- | --- |
| Full race: internal, stdlib and public agent packages | PASS, 89.389s |
| CGO-free full unit/E2E suite (`go test -tags=e2e ./... -count=1`) | PASS, 49.882s |
| Sustained Go Client model/tool waits and cancellation | PASS, 30.758s; all four cases actually executed |
| CGO-free vet including E2E tags | PASS, 1.090s |
| All six profile targets, CGO-free link-free compilation | PASS, 13.864s; no local distribution builds |
| Native release-flags binary | Reports `noe v2.0.1`; arithmetic succeeds, clean build metadata matches the checked commit |
| Focused implementation checks | CLI queue/steer text appears while the provider is blocked; plain multiline text, ID-only later states, hidden initial queue and live/saved equivalence PASS |
| Resident Go host | Request/VM isolation, deadlines, cancellation and shutdown cleanup tests PASS; built-binary HTTP/SIGTERM smoke, copy-out module build and systemd unit syntax checks PASS |
| Independent implementation review | No concrete findings in the observe change; the embedding example's null-element input issue was corrected and re-reviewed before its commit |

The full release gates above passed on their first attempt. Focused change
checks also passed before release preparation, including targeted race and vet.
The systemd example was syntax-checked, not installed as a user service.
Long-term continuous operation and native execution on non-Linux-amd64 targets
were not tested. No external model-provider calls were made for this patch's
implementation or release checks.

Environment: Linux amd64, Go 1.27.2, C toolchain for race. Short TMPDIR/GOTMPDIR
paths avoid Unix-socket pathname limits. Commands, per-gate logs, timings,
clean source worktree and the native binary are retained at
`/var/tmp/noe-v2.0.1-release-0lJV8Q`. Publication, published-module retrieval and downloaded
asset verification are recorded separately after tagging.


## v2.0.1 Release validation — 2026-10-21

- Annotated tag: `v2.0.1`, pointing to `e18524cb10582191b19e410b3aea2fbfbc2a001e`. The peeled remote tag matches the successful workflow commit; its only change from the checked source is the preparation evidence record.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/28): succeeded (API run ID 209, job ID 230).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.0.1): published, neither draft nor prerelease (Release ID 142). Its body matches the committed [English notes](v2.0.1.md).

The workflow passed its CGO-free unit-test and vet gates, built all six profile
targets, and uploaded the binaries plus SHA256SUMS. All seven assets were
downloaded; every binary hash matched the published checksum file.

The downloaded Linux amd64 binary reports `noe v2.0.1`. Build information
matches the tagged revision with `vcs.modified=false`, `CGO_ENABLED=0` and
go-quickjs v0.27.0. The actual human-control CLI E2E test also passed against
the downloaded binary: a TestMain-only temporary overlay selected that binary
instead of rebuilding the CLI, while preserving the tagged test's assertions.
It verified live queue/steer instruction visibility while the provider was
blocked, literal multiline text, ID-only later states, hidden initial queue,
live/saved-log equivalence, conversation continuity, interruption and cleanup.

The native fixture smoke also passed stable queue sessions, separate run IDs,
metadata-only waits, retained/ranged results, synchronous command failure
details, bundled docs, six run_js recipes and arithmetic. No real model-provider
requests were made.

Two clean modules outside the checkout fetched the actual published
`stdlib@v2.0.1` and `agent@v2.0.1`, without local replacement or a synthetic
proxy. Both examples built and ran, and `go mod verify` passed. Selected versions
are `forge.harakara.site/littleisland/noe/v2 v2.0.1` and
`github.com/go-quickjs/go-quickjs v0.27.0`. The deterministic agent returned
`The total is 31.`.

The verified downloaded Linux amd64 binary was atomically installed to
repository-root `./noe` and `~/.local/bin/noe` after backing up both previous
executables. Both installations match the asset bytes and report v2.0.1.
Evidence, all downloaded assets, build information, smoke/overlay logs,
published-module consumers and backups: `/var/tmp/noe-v2.0.1-release-0lJV8Q`.
Native execution on macOS, Windows and arm64, actual systemd user-service
installation, and long-term continuous operation remain unverified. This
documentation-only follow-up does not move or replace the release tag.

## v2.1.0 preparation — 2026-10-22

The release adopts go-quickjs v0.28.0 from `fdfadb39dc0f0afa293701e97de6b44931b7ccc3`.
Normal and critical plan/code reviews, including re-review of the existing
Linux procfs test correction, have no unresolved P2-or-higher findings. The
[adoption record](../development/go-quickjs-v0.28.0-plan.md) preserves the first
race failure, its reproduction with the previous engine and the corrected
test's positive/negative controls.

The full profile checks completed for the adoption source are reused:

| Reference-host gate | Result |
| --- | --- |
| Full CGO-free unit/E2E suite | PASS, 46.437s |
| Full internal/stdlib/agent race | PASS, 82.874s |
| Sustained Go Client model/tool waits and cancellation | PASS, 30.766s; all four cases ran |
| Vet including E2E tags | PASS, 1.369s |
| All six distribution targets, CGO-free compilation | PASS, 26.985s; compile only |
| External stdlib/agent consumers, local and synthetic artifact | PASS; both select engine v0.28.0 |

Adoption evidence is retained at `/var/tmp/noe-q280-XrXP1S`. Release source
`c74d7e31d200176b6e3ad91c6add5fbd7ee89628` changes only the CLI version constant
and its two expected strings among runtime/test/dependency files. A byte-level
comparison confirms all other runtime code and dependencies match the adoption
source. Later tag-preparation changes are documentation only. The existing
stdlib source origin stays v0.27.0, JIT stays disabled, and other dependencies
and Go 1.27.2 are unchanged.

The release source was checked again in a clean detached worktree:

| Release-specific check | Result |
| --- | --- |
| CLI package unit tests | PASS, 6.615s |
| Real CLI version/control/managed run_js E2E cases | PASS, 2.427s |
| Vet including E2E tags | PASS, 1.423s |
| CGO-free release-flags native build | PASS, 0.770s; v2.1.0, engine v0.28.0, checked commit, clean build metadata |
| BigInt/object/Worker/primitive-delete CLI smoke | PASS, 0.065s |
| Local agent fixture, stable queue session, ranged results, command diagnostics and docs | PASS, 0.667s |

Commands, logs, source comparison, clean worktree and build information are in
`/var/tmp/noe-v2.1.0-release-WnXLpO`. The current embedding guides and example
instructions select the v2.1.0 published module; their local placeholder remains
unchanged. Publication and downloaded-asset checks are recorded after tagging.
Native execution is Linux amd64 only; other targets have compilation evidence.
No real provider calls or JIT execution/performance tests were performed.

## v2.1.0 Release validation — 2026-10-22

- Annotated tag: `v2.1.0`, pointing to `d5fc7ee54b06090b7803f1dbb504482b4365d0b6`. Its runtime/test/dependency source matches the checked release source; the additional preparation commit changes only documentation.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/29): succeeded (API run ID 210, job ID 231).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.1.0): published, neither draft nor prerelease (Release ID 143). Its body matches the tagged [English notes](v2.1.0.md).

The workflow passed the CGO-free unit-test and vet gates, linked all six
distribution binaries and uploaded them with SHA256SUMS. All seven assets were
downloaded, and all six binary hashes matched the published checksum file.

The downloaded Linux amd64 binary reports `noe v2.1.0`; build information
contains the tag commit, `vcs.modified=false`, `CGO_ENABLED=0` and go-quickjs
v0.28.0 with the expected checksum. It has no JIT build tag. The downloaded
binary passed the BigInt/object/Worker/primitive-delete smoke.

A temporary TestMain-only overlay selected the downloaded binary for the
tagged CLI E2E assertions instead of rebuilding the CLI. Version/CLI commands,
live human queue/steer observation and saved replay, low-level managed run_js,
and agent file-reading/tool-history cases all passed. Native local-fixture
smoke also passed stable queue sessions, separate run IDs, metadata-only waits,
retained/ranged results, synchronous command failure details, bundled docs and
the six run_js recipes. No paid model-provider requests were made.

Two clean external modules fetched the actual published stdlib and agent
packages at v2.1.0 without local replacement or a synthetic proxy. Both built,
ran and passed `go mod verify`; MVS selected go-quickjs v0.28.0. The deterministic
agent returned `The total is 31.`. These are publication checks, separate from
the adoption's local/synthetic-artifact consumer validation.

The verified downloaded Linux amd64 asset was atomically installed to
repository-root `./noe` and `~/.local/bin/noe`, after backing up both previous
executables. Both installed files match the asset bytes and report v2.1.0.
Linux asset/installation SHA256:
`f82f68c787da88e7a4689cf3cd075971a764d4be2f3f2c045a973a8187f72817`.

Evidence, all downloaded assets, workflow logs, module consumers, native and
artifact-E2E logs, build information and backups are retained at
`/var/tmp/noe-v2.1.0-release-WnXLpO`. Other five targets have build/hash
verification but no native execution on this host. JIT behavior and workload
performance were not tested. This documentation-only follow-up does not move
or replace the release tag; the unrelated active JSONL logs remain excluded.

## v2.1.1 preparation — 2026-10-23

Implementation commit: `aa108dd` (agent processing simplification and Worker
cleanup joining). Checked release source: `20273af`, including the v2.1.1 CLI
version and release notes. All checks below ran in its clean detached worktree.
The tag-preparation evidence commit changes documentation only.

| Reference-host gate | Result |
| --- | --- |
| Full CGO-free unit/E2E suite | PASS, 68.367s |
| Full internal/stdlib/agent race | PASS, 101.285s |
| Sustained Go Client model/tool waits and cancellation | PASS, 40.416s |
| Vet including E2E tags | PASS, 1.934s |
| Six distribution targets, CGO-free compilation | PASS; compile only |
| External stdlib consumer, local replace and module artifact | PASS, 21.234s |
| External agent consumer, local replace and module artifact | PASS, 29.618s; portable and gohost modes |
| Native release-flags build | PASS, 2.142s; v2.1.1, clean checked revision, CGO disabled |
| Engine/Worker CLI smoke | PASS, 0.064s |
| Local agent fixture, queue continuity, retained/ranged answers, command diagnostics and docs | PASS, 0.667s |
| Go module verification | PASS |

The processing and Worker changes passed independent code review. The Worker
regression tests failed with the previous Worker implementation restored by
overlay and passed after the fix. The formerly failing external stdlib artifact
also passed 100 runs with eight concurrent processes; see the
[implementation record](../development/processing-simplification.md).

Go 1.27.2 and go-quickjs v0.28.0 are unchanged. JIT remains disabled. The
maintained stdlib source origin is still v0.27.0. No paid provider calls were
made. Native execution is Linux amd64 only; the other targets have compilation
evidence before tagging and will be linked by the release workflow.

Reference logs, clean source, build information and consumer artifacts:
`/var/tmp/noe-v2.1.1-release-4Q1KN0`. The tag-triggered workflow remains the
distribution gate. Published binaries, checksums and the actual public Go
module are verified separately after publication.

## v2.1.1 Release validation — 2026-10-23

- Annotated tag: `v2.1.1`, pointing to `9349720947186865c7c77dd7d0ca4c1186b7e615`. The peeled remote tag matches the workflow revision; its only difference from the checked source is the preparation evidence document.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/30): succeeded (API run ID 211, job ID 232).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.1.1): published, neither draft nor prerelease (Release ID 144). Its body matches the tagged [English release notes](v2.1.1.md).

The workflow passed the CGO-free unit-test and vet gates, built all six
distribution binaries and uploaded them with SHA256SUMS. All seven assets were
downloaded, and all six binary hashes matched the published checksum file.

The downloaded Linux amd64 binary reports `noe v2.1.1`, the tagged revision,
`vcs.modified=false`, `CGO_ENABLED=0` and go-quickjs v0.28.0 without JIT. Its
SHA256 is `80d3a80034696d4c8ae588b706232aaf6ebcbc5bb01b921e7b1e81407026adde`.
The engine/Worker smoke and native fixture passed against this downloaded
binary. A TestMain-only overlay also ran the checked CLI E2E assertions against
the downloaded binary: version/commands, live queue/steer and saved playback,
managed run_js, file reading and tool history all passed.

Clean modules outside the checkout fetched `stdlib@v2.1.1` and `agent@v2.1.1`
from the actual published module, with no local replacement or synthetic proxy.
Both examples built, ran and passed `go mod verify`; MVS selected go-quickjs
v0.28.0. The deterministic agent returned `The total is 31.`.

The full stdlib Worker consumer also built and ran against the actual published
module. Its initializer-cleanup, Worker and borrowed-I/O assertions passed in
all 100 repeated runs with eight concurrent processes. No assertion was relaxed
to accommodate the formerly failing case.

All assets, workflow log, build information, native/CLI logs, published-module
consumers and repeated Worker results are retained at
`/var/tmp/noe-v2.1.1-release-4Q1KN0`. No paid provider calls were made. Other five
targets have build/hash verification but no native execution on this host.
This documentation-only publication record does not move the release tag.

## Rollback to v2.1.1 — 2026-10-23

The user requested rollback to the last stable 2.1.x release and explicitly
approved deletion of the unauthorized v2.2.0 and managed-engine public tags.

- Application, dependency, test and example source is restored to v2.1.1.
  577 product files were compared byte-for-byte with the v2.1.1 tag; no
  mismatches remain. The source snapshot uses the post-publication v2.1.1
  documentation commit `1c78569`, whose application source matches the tag.
- go.mod again requires upstream `github.com/go-quickjs/go-quickjs v0.28.0`.
  The managed fork source, new execution contract and new module dependency
  are removed from the active main tree.
- Both repository-root noe and ~/.local/bin/noe already contain the published
  v2.1.1 binary. Each reports v2.1.1 and matches published SHA256
  `80d3a80034696d4c8ae588b706232aaf6ebcbc5bb01b921e7b1e81407026adde`.
- The v2.2.0 Release was changed to draft, withdrawing it from public Releases.
  The user approved deleting tags `v2.2.0` and
  `third_party/go-quickjs/v0.28.1-noe.1`; the rollback push removes those refs.
  Already downloaded Go proxy content cannot be recalled by deleting Git tags.
- AGENTS.md and the corrected light-CI policy are retained. User event logs
  were hashed before restoration and preserved unchanged. No heavy suite or
  CI test rerun is needed for restoration of the exact previously validated
  stable source and binaries. No new release version is published.

Rollback evidence: `/var/tmp/noe-rollback-v211-20261023-175657`.
The rollback is a new commit; prior Git history is preserved.

## v2.3.0 preparation — 2026-10-24

The user authorized v2.3.0 publication after agreeing to skip the withdrawn
v2.2.0 number. Reusing that number could confuse downloaded artifacts and
Go proxy caches. Implementation and integrated validation are complete at
`1d6ab38`; see the [implementation record](../development/ai-execution-contract-implementation.md).
The checked release source is `2834fba`, which adds the version, release notes
and current-version migration links. The tag-preparation commit changes this
evidence document only.

Existing S2/S3/S4/T results are reused for unchanged product logic and
dependencies: affected unit/race tests, remaining unit tests, ordinary CLI E2E,
declaration fixtures, external stdlib/agent consumers through local and module
artifact paths, and the deterministic docs/static-check/run_js/repair flow.
The agent-server explicit-import correction and its three cleanup tests are
included. Optional MCP SDK bundle, real-provider and long-wait suites were not
newly run; no paid provider requests were made.

| Release verification | Result |
| --- | --- |
| Updated CLI version subtest | PASS, `CGO_ENABLED=0 go test ./internal/cli -run '^TestCLI/version$' -count=1`, 2.292s |
| Clean native release-flags build | PASS, `2834fba`, `vcs.modified=false`, CGO disabled |
| Version, arithmetic, embedded result contract and agent declaration path | PASS against the native build |
| Go module verification | PASS; dependencies unchanged from v2.1.1 |
| Relative release/guide links | PASS, 120 links |
| Source whitespace/diff | PASS |

The first docs smoke assertion used lowercase `diagnostics` where the selected
member starts with `Diagnostics`. Its successful saved output was checked
against the actual text; no product fix or build repeat was needed.

Go 1.27.2 and official upstream go-quickjs v0.28.0 remain unchanged; no engine
fork, dependency update or JIT is introduced. No full unit/E2E/race/vet or
six-target local build is repeated for the version change. The unchanged light
CI builds all six distribution binaries, creates SHA256SUMS and uploads them.
Native build execution is Linux amd64 only. Actual public artifacts and module
retrieval are checked after publication.

Evidence and detached checked source:
`/var/tmp/noe-v2.3.0-release-u241ahed`. User event logs remain untouched.

## v2.3.0 Release validation — 2026-10-24

- Annotated tag `v2.3.0` points to `8e7c82ad980d2bf51a11b1615a28e01cc625ca4f`.
  The peeled remote tag matches the workflow revision. Its only difference
  from checked source `2834fba` is the preparation evidence above.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/34)
  succeeded (API run ID 215, job ID 236). It built the six binaries, generated
  checksums and uploaded them; the saved log contains no repeated source-test
  or vet command.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.3.0)
  is published, neither draft nor prerelease (Release ID 147). Its body matches
  the tagged [English release notes](v2.3.0.md). v2.2.0 is not reused.

All seven assets were downloaded and all six binary hashes matched SHA256SUMS.
The Linux amd64 binary reports `noe v2.3.0`, the tagged revision,
`vcs.modified=false`, `CGO_ENABLED=0` and official go-quickjs v0.28.0, without
JIT. Its SHA256 is
`50de71b27353db9b2d721c7a41a9d4eb558eb2c906d08d932ac185d0af502f00`.

Against the downloaded binary, a local fixture verified embedded docs,
explicit imports, an execution failure with diagnostics separated from empty
stderr, a corrected async default result, actual stdout, next-model-request
delivery and JSONL retention. Static-tool/declaration source tests were not
repeated for this artifact check.

Clean external modules fetched `stdlib@v2.3.0` and `agent@v2.3.0` through the
actual published module without local replacement or a synthetic proxy. Both
examples built and ran, and both passed `go mod verify`; the deterministic
agent returned `The total is 31.`. MVS selected official go-quickjs v0.28.0.
Both declaration files in the fetched module match the tagged source.
An external stdlib probe also confirmed absent/null/Promise data, detached
structured diagnostics, native Go causes and serialization failure without
replaying side effects.

Evidence is retained at `/var/tmp/noe-v2.3.0-release-u241ahed`, including
`published-verification.json`, `public-module-verification.json`, the workflow
log, downloaded assets, module consumers and result/diagnostic fixture output.
No paid provider calls were made. Other five platforms have CI build and hash
evidence, without native execution here.

This publication record does not move the release tag. Repository-root `noe`
and `/home/agent/.local/bin/noe` are not replaced; the installed version remains
v2.1.1. User event logs remain unchanged. Publication is complete; deployment
is a separate next action if requested.

## v2.3.0 local installation — 2026-10-24

After publication, the user explicitly requested installation. The verified
published Linux amd64 asset replaced repository-root `./noe` and
`/home/agent/.local/bin/noe` using temporary files and atomic renames. Both
report `noe v2.3.0`, match published SHA256
`50de71b27353db9b2d721c7a41a9d4eb558eb2c906d08d932ac185d0af502f00`, and the
PATH command resolves to the updated user binary. Previous binaries are saved
at `/var/tmp/noe-v2.3.0-release-u241ahed/installation-dip_3ak0`; the installation
record is `/var/tmp/noe-v2.3.0-release-u241ahed/installation.json`.
No source test suite was repeated for installing the verified artifact.

## v2.4.0 preparation — 2026-10-24

The user authorized committing and publishing the completed token-efficiency
changes. Implementation commit `0f0f519` contains the compact API member docs,
3 KiB model-facing text budget, per-call output pragma, and openai-chatgpt raw
JavaScript transport. The release also includes the previously committed
standalone observe cancellation fix and MCP/daemon examples since v2.3.0.
See the [implementation and review record](../development/token-efficiency-implementation.md)
and [English release notes](v2.4.0.md).

Checked release source: `f1b99211afa4071cde8655d94ace38c1fb5b55c6`.
The tag-preparation follow-up changes this evidence document only.
Implementation full unit, affected runtime/provider/E2E, vet and real
openai-chatgpt results are reused. Normal and critical plan/implementation
reviews have no unresolved Blocker/P1/P2. Dependencies, public Go execution
contracts, release profile and workflow are unchanged.

| Release verification | Result |
| --- | --- |
| CLI version subtest | PASS, `CGO_ENABLED=0 go test ./internal/cli -run '^TestCLI/version$' -count=1` |
| Clean native release-flags build | PASS, checked source above, `vcs.modified=false`, CGO disabled |
| Native version/arithmetic/embedded budget and pragma docs | PASS, `noe v2.4.0` |
| Native local diagnostic-to-repair fixture | PASS, original diagnostics, structured data and next-request delivery |
| Current release/guide relative links and source diff | PASS |
| Dependencies and existing event logs | Unchanged |

Evidence is retained at `/var/tmp/noe-v2.4.0-release-x4rfnar6`, including a
clean detached source checkout and `preparation-verification.json`.
No full unit/E2E/race/vet or six-target local distribution build is repeated
for the version update. The existing light CI builds six binaries, creates
SHA256SUMS and publishes the immutable tag. Native execution here is Linux
amd64; other platforms receive CI build and checksum verification.
Published artifacts and actual public-module retrieval are checked separately
after publication. Installation of the new binary is a separate action.

## v2.4.0 Release validation — 2026-10-24

- Annotated tag `v2.4.0` points to
  `a067a22164440d2e6f30cbc7c150bf042f0c21b3`. The peeled remote tag matches
  the workflow revision. Its difference from checked source `f1b9921` is the
  preparation evidence above only.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/35)
  succeeded (API run ID 216, job ID 237). The saved log contains no repeated
  source-test or vet command.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.4.0)
  is published, neither draft nor prerelease (Release ID 148). Its description
  matches the tagged English release notes.

All seven assets were downloaded; all six binary hashes matched SHA256SUMS.
The downloaded Linux amd64 binary reports `noe v2.4.0`, the tagged revision,
`vcs.modified=false`, `CGO_ENABLED=0` and official go-quickjs v0.28.0 without
JIT. Its SHA256 is
`6515de3dd63334c908107a6b890c981676dd0df16a7aed405f04b949666c7997`.

Local fixtures against that binary verified embedded docs, separated failure
diagnostics, corrected structured results, next-request delivery and JSONL
retention. An oversized successful result reached the next model request as
an explicit 3 KiB preview, while a subsequent call with a 12 KiB pragma
returned its complete 9,000-character data. Original captured stdout/data
remained in `tool.finished.rawResult`.

The initial artifact-only assertion looked for stdout in `run_js.finished`,
which records completion/data/diagnostics rather than the stdout text. The
fixture was corrected to inspect `tool.finished.rawResult`; both artifact
cases then passed. No application change, tag movement or workflow rebuild
was required. Initial and corrected evidence are retained.

Clean external modules fetched `stdlib@v2.4.0` and `agent@v2.4.0` from the
actual published module, with no local replacement or synthetic proxy.
Both examples built, ran and passed `go mod verify`; the deterministic agent
returned `The total is 31.`. MVS selected go-quickjs v0.28.0, and both
declaration files match the tagged source.

Evidence is retained at `/var/tmp/noe-v2.4.0-release-x4rfnar6`, including
published assets, workflow log, artifact fixtures, module consumers,
`published-verification.json` and `public-module-verification.json`.
No external model calls were needed for release/artifact verification.
Other five platforms have CI build/hash evidence without native execution
here. Existing event logs are unchanged.

This documentation-only publication record does not move the release tag.
Repository-root `./noe` and `/home/agent/.local/bin/noe` remain the previously
installed v2.3.0 binaries; this request publishes v2.4.0 without installing it.

## v2.5.0 preparation — 2026-10-24

The user authorized committing and publishing the completed output-budget and
workspace file-helper changes. The latest remote release was v2.4.0; v2.5.0 is
the next feature release. Implementation commit `d451052` contains the unified
stream/final JSON budget (64 KiB default, 256 KiB maximum), full-envelope byte
accounting, and external `ls`, `read_file`, `write_file`, `edit_file` helpers.
It also includes the already committed search/map libraries, agent-server and
JSONL analysis examples since v2.4.0. See [release notes](v2.5.0.md).

Checked release source: `0d9c13576d333b3044c838a8dfb1af3741b24f9f`.
The tag-preparation follow-up changes this evidence document only. Dependencies,
release profile and workflow are unchanged.

Implementation evidence is reused for unchanged source: affected Go tests in
`internal/agent`, `internal/agentexec`, `internal/apidocs`, `internal/runtime`;
CLI output/hook/event and docs E2E; 34 file-helper filesystem checks; and Biome
lint of the changed JavaScript. These passed before release preparation.
The checks cover expanded console capture, encoded JSON including metadata and
escaping at the 1/64/128/256 KiB budgets, invalid-budget rejection before
execution, Unicode, exact replacements and failed edits leaving files unchanged.

| Release verification | Result |
| --- | --- |
| CLI version subtest | PASS, `go test ./internal/cli -run '^TestCLI/version$' -count=1` |
| Clean native release-flags build | PASS, checked source above, CGO disabled, `vcs.modified=false` |
| Native version/arithmetic/embedded budget docs | PASS, `noe v2.5.0`; `noe:run_js results` has the current contract |
| Current release/README/helper guide relative links | PASS |
| Dependencies and existing event logs | Unchanged |

Evidence is retained at `/var/tmp/noe-v2.5.0-release-j4edg63t`, including the
clean detached source, native build information and preparation verification.
The initial smoke assertion read the topic overview; it was corrected to read
the `results` detail, where the output contract is documented. No source change
was needed. No full source suite, race, six-target local distribution build or
paid model call was repeated for release preparation. The existing light CI
builds six binaries, creates SHA256SUMS and publishes the immutable tag.
Published artifacts and module availability are checked after publication.
Installation is separate from this publication request.

## v2.5.0 Release validation — 2026-10-24

- Annotated tag `v2.5.0` points to
  `1ba92915c21b9b5efc3d1804edd9b1fb617af83f`. The remote peeled tag matches
  the workflow revision; its difference from checked source `0d9c135` is
  preparation evidence only.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/36)
  succeeded (API run ID 217, job ID 238). The saved native log has no repeated
  source test or vet command.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.5.0)
  is published, neither draft nor prerelease (Release ID 149). Its description
  matches the tagged English release notes.

All seven assets were downloaded, and all six binary hashes matched SHA256SUMS.
The Linux amd64 binary reports `noe v2.5.0`, the tagged source revision,
`vcs.modified=false`, `CGO_ENABLED=0` and official go-quickjs v0.28.0. Its SHA256
is `316b0ec2fb7195face6ee1d2b2b4bcb1816aafbedbe28d227564ca008b108979`.

Deterministic local-provider fixtures against the downloaded binary verified
failure diagnostics, repaired structured results, next-request delivery and
JSONL retention. Default-budget overflow reached the model within 65,536 bytes.
A 131,072-byte pragma captured and delivered 90,001 stdout bytes completely,
demonstrating that capture grows past the previous fixed 64 KiB ceiling.
Heavily escaped oversized data stayed within the maximum 262,144-byte result
budget. A 262,145-byte setting produced a setup failure without writing its
marker file. All 34 external file-helper checks also passed with that binary.

Clean external modules fetched `stdlib@v2.5.0` and `agent@v2.5.0` from the actual
published module without local replacement or a synthetic proxy. Both examples
built, ran and passed `go mod verify`. The agent returned `The total is 31.`;
MVS selected official go-quickjs v0.28.0, and both declaration files match the
tagged source. No paid provider calls were made.

Evidence is retained at `/var/tmp/noe-v2.5.0-release-j4edg63t`, including the
workflow log, published assets, contract-flow requests/events, helper output,
consumer builds, `published-verification.json` and
`public-module-verification.json`. Other five platforms have CI build/hash
evidence without native execution here.

This publication record does not move the release tag. Repository-root `./noe`,
the PATH binary and running NOE processes are unchanged; installation remains
a separate action. Existing event logs remain untouched.

## v2.5.1 preparation — 2026-10-25

The user authorized committing and releasing the completed responsibility-boundary
refactoring and release-independent version tests. The latest remote release was
v2.5.0; this is a maintenance patch with no public API or dependency change.
The pending upstream-issue reports and parent-wait proposal were committed as
documentation only in `519f1a7`; no proposed interruptible-wait behavior is included.
See the [release notes](v2.5.1.md).

Checked clean source: `e4b4c2332acc47e6e9322865e135aa72e600337d`.
Its only production-source difference from the tested implementation/test head
`66343d1` is the default CLI Version changing from v2.5.0 to v2.5.1.
The tag-preparation follow-up changes this evidence document only.

Implementation evidence is reused rather than repeating source suites:
the five slices passed related tests and independent reviews; normal and critical
plan reviews and every code review had no unresolved P2-or-higher finding.
Full local unit/E2E execution reported only the pre-existing fixed-version test
failure. Commit `66343d1` removes that release-number dependency, after which
the affected CLI package and entire E2E package passed. The production source
and dependencies did not change with the test correction. Full race, vet,
changed-JS lint, formatting and six-target compilation had already passed.
Evidence: [implementation record](../development/responsibility-boundaries-refactor-implementation.md),
`/var/tmp/noe-boundaries-KVDans/final/` and
`/var/tmp/noe-version-tests-uI2vzf/`.

| Release verification | Result |
| --- | --- |
| Clean native release-flags build | PASS, CGO disabled, checked source above, vcs.modified=false |
| Native version and arithmetic | PASS, noe v2.5.1 and 2, exact stdout and empty stderr |
| Repository Exa stub fixture against the candidate | PASS, 43 checks, no real API request |
| Updated release/README relative links | PASS, 11 references resolve |
| Production delta since implementation verification | Version string only; go.mod/go.sum and public facades unchanged |

Evidence is retained at `/var/tmp/noe-v2.5.1-release-8ZOiqB`, including the
clean detached worktree, native binary/build metadata and preparation verification.
No full source suite, race, long wait or local six-binary distribution build was
repeated for release preparation. Existing lightweight CI builds the six profile
targets, creates SHA256SUMS and publishes the immutable tag. Artifact download,
checksums, release body and actual published module retrieval are checked after
publication. No paid model call or installed-binary replacement is part of this
release request. Existing event logs and caches are excluded.

## v2.5.1 Release validation — 2026-10-25

- Annotated tag `v2.5.1` points to
  `8bf7ea4640993f49a9eacb151b4588bfc106fd4d`. The remote peeled tag matches
  the successful workflow revision. Its difference from checked clean source
  `e4b4c23` is the preparation evidence document only.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/37)
  succeeded (API run ID 218, job ID 239), using the unchanged lightweight
  build/checksum/upload workflow.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.5.1)
  is published, neither draft nor prerelease (Release ID 150). Its description
  matches the tagged English release notes.

All seven assets were downloaded; all six binary hashes match SHA256SUMS.
The Linux amd64 binary reports `noe v2.5.1`, the tagged source revision,
`vcs.modified=false`, `CGO_ENABLED=0` and official go-quickjs v0.28.0.
Its SHA256 is `85389792da12c11678b1d834c8a5933a4e81553801b91cb3743c518d126a2fb6`.

The downloaded-binary local-provider contract fixture passed result/diagnostic
delivery, next-request history and JSONL retention, including the private task
and tool modules affected by the refactoring. Budget fixtures also passed.
These checks make no paid model-provider calls.

Clean modules outside the checkout fetched
`forge.harakara.site/littleisland/noe/v2/stdlib@v2.5.1` and
`forge.harakara.site/littleisland/noe/v2/agent@v2.5.1` from the actual public
module, without local replace or a synthetic proxy. Both examples built,
ran and passed `go mod verify`. The agent returned `The total is 31.`;
MVS selected official go-quickjs v0.28.0. Both public type-declaration files
match the tagged source.

Evidence is retained at `/var/tmp/noe-v2.5.1-release-8ZOiqB`, including the
workflow metadata, seven downloaded assets, contract-flow requests/events,
consumer builds, `published-verification.json` and
`public-module-verification.json`. Other five platforms have CI build/hash
evidence without native execution here.

This documentation-only follow-up does not move or replace the release tag.
Repository-root `./noe`, the PATH binary and running processes are unchanged;
installation remains separate. Existing event logs and caches are untouched.

## v2.6.0 preparation — 2026-10-25

The user authorized commit, push, release and replacement of the locally used
binaries after the steer-aware wait implementation was complete. The latest
remote tag was v2.5.1; this additive public API is a minor feature release.
Implementation commit `f01351ee544f9822ab5230f0b298f243fa463da0` contains
grouped waiting, its Go/JS/typed/hook connections, declarations, docs and tests.
See the [implementation record](../development/steer-aware-wait-implementation.md)
and [English release notes](v2.6.0.md).

Checked clean source: `c2d6121ecdec5e0a6f5dd1a9bd6c1289f958d017`.
Its only production-source difference from the verified implementation is the
default CLI Version changing from v2.5.1 to v2.6.0. The tag-preparation follow-up
changes this evidence document only.

Implementation evidence is reused: full unit/E2E, related-package race tests,
changed JavaScript lint, public declaration checking, formatting and diff checks
passed. Independent code review has no unresolved P2-or-higher finding.
A real executor with controlled model fixtures held two children unfinished and
verified trusted-host and script-origin steer, wait/tool completion, delivery to
the next parent model request and subsequent explicit child cancel/steer/result
retrieval. No child was cancelled merely to return from the wait.
Logs: `/var/tmp/noe-steer-aware-wait-Ekbh7s`.

| Release verification | Result |
| --- | --- |
| Clean native release-flags build | PASS, CGO disabled, checked source above, vcs.modified=false |
| Candidate version and arithmetic | PASS, noe v2.6.0 and 2, exact stdout and empty stderr |
| Bundled new/legacy wait exports and grouped-wait docs/model recipe | PASS, no model request |
| Current README/release relative links | PASS, 106 references resolve |
| Production delta since implementation validation | Version string only; dependencies and release workflow unchanged |

Evidence is retained at `/var/tmp/noe-v2.6.0-release-T7lygk`, including the
clean detached source, native build metadata and preparation-verification.json.
No full source suite, race, long wait or local six-binary distribution build is
repeated for release preparation. The unchanged lightweight CI builds six
binaries, creates SHA256SUMS and publishes the immutable tag. Published artifacts
and actual module retrieval will be checked before installing the verified Linux
amd64 asset with backups and atomic replacement. Other targets have CI build/hash
verification, not native execution on this host. No paid model-provider request
is required. Unrelated model-catalog edits, active events and caches are excluded.

## v2.6.0 Release validation and local installation — 2026-10-25

- Annotated tag `v2.6.0` points to
  `9ae801f145b232e718b1fff74e3bcff9bfb1dfd2`. Its peeled remote tag matches
  the successful workflow revision; its difference from checked source
  `c2d6121` is the preparation evidence document only.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/38)
  succeeded (API run ID 219, job ID 240), using the unchanged lightweight
  build/checksum/upload workflow.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.6.0)
  is published, neither draft nor prerelease (Release ID 151). Its description
  matches the tagged [English release notes](v2.6.0.md).

All seven assets were downloaded; all six binary hashes match SHA256SUMS.
The Linux amd64 binary reports `noe v2.6.0`, the tagged source revision,
`vcs.modified=false`, `CGO_ENABLED=0` and official go-quickjs v0.28.0 without
JIT. Its SHA256 is
`c6f171ca9d7c4476f3699d58c5a4aafa9735ea9f790db93895727c0cfb193c85`.

Against the downloaded binary, arithmetic and bundled new/legacy wait exports,
grouped-wait member docs and the parent-wait model recipe passed. A deterministic
local-provider fixture also passed failure diagnostics, repaired structured
results, next-request delivery and JSONL retention. The unrelated budget fixture
was not repeated. No paid model-provider request was made.

Clean external modules fetched `stdlib@v2.6.0` and `agent@v2.6.0` from the
actual published module, without local replacement or a synthetic proxy.
Both built, ran and passed `go mod verify`; MVS selected official go-quickjs
v0.28.0. Both public declaration files match the tagged source. The agent
consumer additionally exercised the new public `Client.WaitTasks` on a completed
task with includeAnswer=false, checked the ordered metadata-only result, and
confirmed the legacy WaitTask could still retrieve its retained answer.

The user explicitly authorized local installation in the same request.
The verified published Linux asset atomically replaced both repository-root
`/home/agent/projects/noe/noe` and `/home/agent/.local/bin/noe`.
Both match the published SHA256 and report `noe v2.6.0`; PATH resolves to the
updated user binary. The previous v2.5.1 executables are backed up at
`/var/tmp/noe-v2.6.0-release-T7lygk/installation-tvx98f1a`.
Existing running processes are not restarted or interrupted.

Evidence is retained at `/var/tmp/noe-v2.6.0-release-T7lygk`, including workflow
metadata, seven published assets, native build metadata, artifact fixture logs,
module consumers, published-verification.json, public-module-verification.json
and installation.json. Other five platforms have CI build/hash verification
without native execution here. Successful implementation source tests were not
repeated for publication or installation.

This documentation-only follow-up does not move or replace the release tag.
The unrelated existing model-catalog edit, events and caches remain excluded
from these commits and preserved in the working tree.

## v2.7.0 preparation — 2026-10-26

The user authorized release of the completed hook-failure isolation changes.
The latest remote release and tag were v2.6.0. Implementation commit
`ab5c0a1dbcf8c3dddc9087d67304155fa87b371e` contains the complete feature,
public Go/JS contracts, docs and tests. See the
[implementation record](../development/hook-failure-isolation-implementation-20261025.md)
and [English release notes](v2.7.0.md). Both READMEs and the release notes
explicitly warn that handler throw no longer stops a task and beforeTool
failure does not skip the tool.

Checked clean release source: `5fac70acbf4303e3514589363880f0a71ab3daca`.
Its only production-source difference from the validated implementation is the
default CLI Version changing from v2.6.0 to v2.7.0; the remaining changes are
release notes and links. This preparation follow-up changes this evidence
document only. Dependencies, feature source/tests, release profile and workflow
are unchanged.

Existing implementation results are reused: full unit and local E2E suites,
race checks for internal/agent, internal/agentbridge, internal/runtime and agent,
vet, changed-JavaScript lint, declaration/fixture types, Go formatting and diff
checks all passed. Normal and critical reviews have no unresolved P2-or-higher
finding. The original machine-readable report remains at
`/var/tmp/noe-hook-isolation-final-1792941850015/report.json`.

| Release verification | Result |
| --- | --- |
| Clean native release-flags build | PASS, CGO disabled, checked source above, vcs.modified=false |
| Candidate version and arithmetic | PASS, noe v2.7.0 and 2, exact stdout and empty stderr |
| Candidate hook recovery through actual CLI | PASS, one local provider request, completed outcome, one delivered before-model failure and one retained final-response failure |
| Metadata-only and original-answer retrieval without event file | PASS, both hook records preserved, answer unchanged, no additional model request |
| Current README/release relative links | PASS, 115 references resolve |
| Production delta and whitespace | Version string only; unchanged dependencies/feature/workflow; diff check passed |

Evidence is retained at `/var/tmp/noe-v2.7.0-release-eXLsEk`, including clean
detached source, native build metadata, candidate-hook requests/results and
preparation-verification.json. No paid model call, full source-suite rerun,
race rerun, long wait or local six-binary distribution build was needed.
The unchanged lightweight CI builds all six profile targets, creates
SHA256SUMS and publishes the immutable tag. Actual artifacts and published Go
module retrieval are checked after publication. Native execution here is
Linux amd64 only.

This request is publication, not local installation. Repository-root noe,
the PATH binary, running processes, user event logs and caches remain
unchanged and excluded from release commits.

## v2.7.0 Release validation — 2026-10-26

- Annotated tag `v2.7.0` points to
  `c9d70b1753235960d4ae8e2dbee12d2dd409fa21`. The peeled remote tag matches
  the successful workflow revision; its difference from checked source
  `5fac70a` is the preparation evidence document only.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/39)
  succeeded (API run ID 220), using the unchanged lightweight
  build/checksum/upload workflow.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.7.0)
  is published, neither draft nor prerelease (Release ID 152).
  Its description matches the tagged [English release notes](v2.7.0.md).

All seven assets were downloaded. All six binary hashes match SHA256SUMS.
The downloaded Linux amd64 binary reports `noe v2.7.0`, the tagged source
revision, `vcs.modified=false`, `CGO_ENABLED=0` and official go-quickjs
v0.28.0. Its SHA256 is
`8c3762993afc7c7858a0d98f3a7e41a434568d09275351b4336a780b2ce25858`.

Against that actual distributed binary, arithmetic and the deterministic
hook CLI fixture passed. One ordinary model request carried the before-model
hook failure, the task completed, and the final-response hook failure remained
in the retained outcome without an extra request. Metadata-only retrieval
preserved both failures, original-answer retrieval was unchanged and no event
file was created. No paid model-provider call was made.

Clean external modules fetched `stdlib@v2.7.0` and `agent@v2.7.0` from the
actual published module, without a local replacement or synthetic proxy.
Both examples built, ran and passed `go mod verify`; MVS selected official
go-quickjs v0.28.0. Both public declaration files match the tagged source.
The agent consumer additionally compiled and round-tripped the new public
`HookFailure`/`TaskOutcome.HookFailures` wire fields and retrieved a normal
retained result without its answer. It returned `The total is 31.` and
`PUBLISHED_HOOK_API_PASS`.

Evidence is retained at `/var/tmp/noe-v2.7.0-release-eXLsEk`, including workflow
metadata/log archive, seven downloaded assets, build information, published
hook requests/results, public-module consumers, published-verification.json and
public-module-verification.json. Other five targets have CI build/hash
verification without native execution on this host. Successful source test
suites were not repeated for publication.

This documentation-only follow-up does not move or replace the release tag.
Repository-root noe, the PATH binary and running processes are unchanged;
installation remains separate. Existing event logs and caches are preserved
and excluded from these commits.

## v2.7.1 preparation — 2026-10-26

The user authorized commit and release of the completed go-quickjs v0.29.0
upgrade and direct named re-export restoration. Latest remote release: v2.7.0.
Implementation commit `591b7621c6e6495be6286a2148b58bc3a575ed08` contains
the dependency update, regression test, direct re-export and current docs.
See the [impact and implementation record](../development/go-quickjs-v0.29.0-impact.md)
and [English release notes](v2.7.1.md).

Checked clean release source: `a7b3dfddfba157374b21ad4523f24e4e839ac720`.
Its only production-source difference from the verified implementation is
the CLI Version changing from v2.7.0 to v2.7.1. Other changes are release
notes/current-version links and the follow-up record. This preparation
commit changes this evidence document only.

Successful implementation results are reused: dependency-candidate full unit
and local E2E, public Go consumer fixtures and six-target compilation; after
the direct re-export change, affected executor/runtime unit tests, actual CLI
hook run/eval routes, four-package runtime/Worker race checks, related vet,
lint, formatting and diff checks. The applied module graph exactly matches
the dependency candidate. Independent review accepted implementation and test
changes; its documentation finding was corrected. Evidence:
`/var/tmp/noe-q290-impact-PqTX81`.

| Release verification | Result |
| --- | --- |
| Clean native release-flags build | PASS, CGO disabled, checked revision above, vcs.modified=false |
| Native version/arithmetic | PASS, noe v2.7.1 and 2 |
| NOE module regressions | PASS, block declarations/shadowing yield 7/7; node:path named re-export resolves |
| Intl/date changes | PASS, corrected Telugu year, historical Abidjan seconds and Korean locale fallback |
| Normal task/direct re-export path | PASS, real local-provider CLI completes; hook notification/retention and original answer remain valid |
| Embedded dependencies | go-quickjs v0.29.0 and go-intl v0.8.0 confirmed; go mod verify passes |
| Current release/README/Go-guide links | PASS, 125 relative references resolve |

Release evidence and clean detached source:
`/var/tmp/noe-v2.7.1-release-EI1leu`. All other selected dependencies, Go 1.27.2,
NOE-managed stdlib origin v0.27.0, no-JIT configuration and lightweight release
workflow are unchanged. The latter builds six binaries, creates SHA256SUMS
and uploads the immutable tag; no local full-suite/race/long-wait rerun or
six-binary distribution build is repeated for release preparation.

Published assets, checksums and actual public-module retrieval are verified
after publication. Native execution here is Linux amd64 only; no paid model
call is made. Local installation and process restarts are not authorized by
this release request. Repository-root/PATH binaries, user event logs and
caches remain untouched and excluded from commits.

## v2.7.1 Release validation — 2026-10-26

- Annotated tag `v2.7.1` points to
  `e4e297a32faac7f19bb8094fa436760072163352`. Its peeled remote tag matches
  the successful workflow revision. Its difference from checked source
  `a7b3dfd` is the preparation evidence document only.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/40)
  succeeded (API run ID 221) with the unchanged lightweight workflow.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.7.1)
  is published, neither draft nor prerelease (Release ID 153).
  Its body matches the tagged [English release notes](v2.7.1.md).

All seven assets were downloaded; all six binary hashes match SHA256SUMS.
The Linux amd64 asset reports `noe v2.7.1`, the tagged source revision,
`vcs.modified=false`, `CGO_ENABLED=0`, go-quickjs v0.29.0 and go-intl v0.8.0.
Its SHA256 is
`5f60d2eb4f040383d14e4ec0c08c558a97f49ab169ce36f61fa739b8a255b4cf`.

Against the actual downloaded binary, arithmetic, module block declarations
and shadowing, node:path direct named re-export, corrected Intl year/locale
fallback and historical timezone seconds all passed. A deterministic
local-provider task also completed normally through the restored task
re-export, retained the before/final hook failures and returned the unchanged
original answer without an event file or extra model request.

Clean external modules fetched `stdlib@v2.7.1` and `agent@v2.7.1` from the
actual published module without a local replacement or synthetic proxy.
Both built, ran and passed `go mod verify`. MVS selected go-quickjs v0.29.0
and go-intl v0.8.0 in both consumers, and both public declaration files match
the tagged source. The agent returned `The total is 31.` and passed public
HookFailure wire/retained-result checks.

Evidence is retained at `/var/tmp/noe-v2.7.1-release-EI1leu`: workflow
metadata/log archive, published assets, build metadata, engine/hook probe
outputs, module consumers, published-verification.json and
public-module-verification.json. No paid model calls were made. Other five
targets have build/hash verification without native execution on this host.
Valid implementation source-test results were reused, not repeated for tagging.

This documentation-only follow-up does not move or replace the release tag.
Repository-root noe, the PATH binary and running processes are unchanged;
installation remains separate. User event logs and caches are preserved and
excluded from these commits.

## v2.8.0 preparation — 2026-10-26

The user authorized release of the completed HTTP/HTTPS ESM URL import feature.
Latest remote release is v2.7.1. Feature commit
`c1f4690ae864063b44c210db49eb8cc92378622c` contains the four implementation
slices, tests, independent code reviews and usage documentation.
See the [implementation evidence](../development/url-import-implementation.md)
and [English release notes](v2.8.0.md).

Checked clean release source:
`4260b6a9bd0f513bc1f3f32fcc76a326a35c997e`.
Its only production-source change from the verified feature is the CLI
Version string changing from v2.7.1 to v2.8.0. Other changes are current-version
links, release notes and source-only wording. This preparation follow-up
changes this evidence document only.

Implementation results are reused: four slice-specific test/review passes,
the full unit/local-E2E execution with one obsolete docs phrase assertion
corrected and its affected package rechecked, whole-tree vet, five affected
package race passes, external Go consumers, saved-JS lint and live public
lodash-es export use through direct and alias/dynamic imports. There is no
remaining implementation finding. Source suites, race, long-client waits and
six distribution artifact builds are not repeated for publication.

| Release verification | Result |
| --- | --- |
| CLI configured-version test | PASS |
| Clean native release-flags build | PASS, CGO disabled, checked revision above, vcs.modified=false |
| Native version/arithmetic | PASS, noe v2.8.0 and 2 |
| URL artifact fixture | PASS, extensionless redirect to .data module, relative .txt dependency, text/plain/JS MIME, static/re-export/dynamic aliases and canonical namespace |
| Independent runtime source | PASS, fixture changes 42 to 43 and new run/eval sees new source while each namespace evaluates once |
| Embedded dependencies / go mod verify | go-quickjs v0.29.0, go-intl v0.8.0, all modules verified |
| Current README/release/Go-guide links and diff checks | PASS, 278 relative references resolve |

Evidence and clean detached source:
`/var/tmp/noe-v2.8.0-release-cXWw0u`.
Go 1.27.2, all selected dependencies, NOE-managed stdlib origin v0.27.0,
the no-JIT configuration and lightweight workflow are unchanged.
CI builds the six distribution targets, creates SHA256SUMS and uploads the
immutable tag, without repeating local source tests. Published assets and
actual public-module retrieval are checked after publication.

Native execution here is Linux amd64; other five targets receive CI
build/checksum verification, not native execution. No paid model-provider
call is made. This request authorizes publication, not installation:
repository-root/PATH binaries and running processes remain unchanged.
Existing user event logs and caches are preserved and excluded from commits.

## v2.8.0 Release validation — 2026-10-26

- Annotated tag `v2.8.0` points to
  `9aa185018773c30ee0e10082f7e12b8759299a9e`. The peeled remote tag matches the successful
  workflow revision. Its difference from checked source `4260b6a` is the
  preparation evidence document only.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/41) succeeded (API run ID
  222) with the unchanged lightweight build/checksum/upload job.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.8.0) is published, neither draft nor prerelease
  (Release ID 154). Its body matches the tagged English
  [release notes](v2.8.0.md).

All seven assets were downloaded. All six binary hashes match SHA256SUMS.
The downloaded Linux amd64 asset reports `noe v2.8.0`, the tagged source
revision, `vcs.modified=false`, `CGO_ENABLED=0`, go-quickjs v0.29.0 and
go-intl v0.8.0. Its SHA256 is
`a1ef5c013aacb812779425aef2ef6ae5b200f03c62e5acd0b85effe2a9f5e600`.

Against the actual distributed binary, version/arithmetic and the focused
URL fixture passed: extensionless URL redirects to a .data module, relative
.txt dependencies, text/plain and JavaScript MIME, static/re-export/dynamic
imports, alias/final-URL namespace identity, and fresh source in separate
run/eval runtimes. The downloaded binary also fetched public
lodash-es@4.17.21 from esm.sh and chunk returned `[[1,2],[3,4],[5]]`
with exit 0 and empty stderr.

Clean external Go modules fetched stdlib@v2.8.0 and agent@v2.8.0 from the
actual published module, without local replacements or synthetic proxies.
Both consumer fixtures built, ran and passed go mod verify. The stdlib
consumer exercises the new explicit URL provider, public capability helper,
independent-runtime source and fetch/FS separation. MVS selected go-quickjs
v0.29.0 and go-intl v0.8.0. The downloaded loader/HTTP/Host source files and both
public declaration files match the tagged source. No paid model-provider
request was made.

Evidence is retained at `/var/tmp/noe-v2.8.0-release-cXWw0u`: workflow metadata/log archive,
seven downloaded assets, build metadata, URL fixture results, live public-ESM
output, public-module consumers, published-verification.json and
public-module-verification.json. Native execution is Linux amd64; other five
targets have CI build/hash verification without native execution on this host.
Successful implementation suites were reused rather than rerun for tagging.

This documentation-only follow-up does not move or replace the release tag.
Repository-root and PATH binary hashes match their pre-release baseline;
running processes are unchanged and installation remains separate. Existing
event logs and caches are preserved and excluded from these commits.

## v2.8.1 preparation — 2026-10-26

The user authorized release and local installation of the completed
import.meta.url compatibility correction. Latest remote release: v2.8.0.
Implementation commit `b8dad18e9500db6cb201c6dcd120cd3da1a75883` contains
the runtime metadata hook, public/CLI/agent/Worker tests and documentation,
plus the external find helper, ls default-page update, URL-import example
and esbuild investigation requested in the same work. esbuild is not added
as a dependency and TypeScript is not implemented.

Checked clean release source:
`4ba2e8ec9c6dc07845828ab43e2346338d8b7df4`.
Its only production-Go-source difference from the verified implementation
is the CLI Version changing from v2.8.0 to v2.8.1. Other changes are release
notes and current-version links. This preparation follow-up changes only
this evidence document. Go dependencies and release workflow are unchanged.

Implementation evidence is reused: the full local unit suite, focused
CLI/agent URL-import E2E, public local/HTTP/HTTPS/Worker/embedding metadata
paths, encoding unit tests and independent review passed. The external
find helper passed 46 fd/find/fallback/error/limit checks, the file helper
passed 37 checks, and the small external-library example ran with lint
success. See the [metadata record](../development/import-meta-url-implementation.md)
and [release notes](v2.8.1.md). No full suite, race, long wait or six-binary
local distribution build is repeated for release preparation.

| Release verification | Result |
| --- | --- |
| Configured CLI version test | PASS |
| Clean native release-flags build | PASS, CGO disabled, checked source above, vcs.modified=false |
| Candidate version/arithmetic | PASS, noe v2.8.1 and 2 |
| Candidate metadata fixture | PASS, encoded local module path, module-relative asset read, redirected remote URL/query/fragment and asset fetch, file/eval Worker, CLI virtual entry |
| Current release/README/guide links | PASS, 138 relative references resolve |
| Source/formatting/dependency delta | Version only in production Go; unchanged dependencies and workflow |

Evidence and detached source:
`/var/tmp/noe-v2.8.1-release-161635626`.
This includes native build information, candidate fixture output, reused
implementation-validation.json and implementation-review.md, and
preparation-verification.json.

The unchanged lightweight CI builds six distribution targets, creates
SHA256SUMS and publishes the immutable tag. Published checksums, native
metadata behavior and actual module retrieval will be checked before
installing the verified Linux amd64 asset with backups and atomic
replacement. Native runtime verification here is Linux amd64; the other
targets have build/hash verification, not native execution. Existing
running processes will not be restarted. User logs, caches and the
Git-excluded reference checkouts are preserved and excluded from commits.

## v2.8.1 Release validation and local installation — 2026-10-26

- Annotated tag `v2.8.1` points to
  `416a9c8d12faad061069f4021a3fb593f00cf8e5`. The remote peeled tag
  matches the successful workflow revision. Its difference from checked
  source `4ba2e8e` is the preparation evidence document only.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/42)
  succeeded (API run ID 223) using the unchanged lightweight job.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.8.1)
  is published, neither draft nor prerelease (Release ID 155).
  Its body matches the tagged [English release notes](v2.8.1.md).

All seven assets were downloaded; all six binary hashes match SHA256SUMS.
The downloaded Linux amd64 asset reports `noe v2.8.1`, the tagged source
revision, `vcs.modified=false`, `CGO_ENABLED=0`, go-quickjs v0.29.0 and
go-intl v0.8.0. Its SHA256 is
`4d7a3fdab0a5e17273ab2800cfde5f39f9b1940bab9db70ffd90b060e2219bf6`.

The actual distributed binary passed version/arithmetic and the metadata
fixture: encoded local module paths, module-relative asset read, remote
redirect/query/fragment identity, remote asset fetch, file/eval Workers
and the synthetic CLI eval URL. No paid model-provider request was made.

Clean external modules fetched `stdlib@v2.8.1` and `agent@v2.8.1` from
the actual public module, without local replacements or a synthetic proxy.
Both consumer examples built, ran and passed `go mod verify`. The stdlib
consumer additionally checked the new virtual-entry import.meta.url and
relative asset URL, printing `PUBLISHED_IMPORT_META_PASS`.
MVS selected official go-quickjs v0.29.0 and go-intl v0.8.0. Metadata/loader/
Host source files and both public declaration files match the tagged source.

The user's request also authorized local installation. The verified
published Linux amd64 asset atomically replaced both
`/home/agent/projects/noe/noe` and `/home/agent/.local/bin/noe`.
Both hashes match the published checksum and both report `noe v2.8.1`.
PATH resolves to the updated user binary. Previous executables are backed
up at `/var/tmp/noe-v2.8.1-release-161635626/installation-ly5qadev`.
Existing running processes are not restarted or interrupted; they pick up
the new implementation after restart.

Evidence is retained at `/var/tmp/noe-v2.8.1-release-161635626`, including workflow metadata/logs,
seven downloaded assets, native build information, metadata fixture results,
public-module consumers, published-verification.json,
public-module-verification.json and installation.json.
Other five targets have CI build/hash evidence without native execution here.
Previously successful source tests were reused rather than rerun for release.

This documentation-only follow-up does not move or replace the release tag.
User event logs, caches and Git-excluded reference checkouts remain preserved.

## v2.8.2 preparation — 2026-10-26

The user authorized committing and releasing the completed maintenance work.
Latest published release is v2.8.1. Implementation commit
`2cbb7beed0af523e3f364f27db00eb61aa2f6c20` removes obsolete image I/O and
unused internal wrappers, preserves live image-output safety tests, avoids
duplicate context normalization, and adds image-only rejection of
non-regular files. The release also includes the earlier Staticcheck
cleanup commit `f2a49db`.

Checked clean release source:
`9cff990b5bdd3087e6080e10c796fd910faa849a`.
Its only production-source difference from the verified implementation is
the CLI Version changing from v2.8.1 to v2.8.2. Dependencies and release
workflow are unchanged. This preparation follow-up changes only this
evidence document.

Implementation evidence is reused:

- The cleanup passed `go test ./... -count=1` and independent review.
- The subsequent FIFO fix passed `go test ./internal/modelbinding ./internal/agent ./agent/model ./internal/filesystem -count=1`
  and managed runtime `TestImage*` tests.
- The image-binding tests cover selected read-only/rooted filesystems,
  regular images, symlinks, non-regular inputs and cancellation.
- The FIFO test rejects without a writer. A temporary Go overlay restoring
  the previous reader fails that same test by blocking in open/read.
- Development Staticcheck (8371db75e9da), changed-JavaScript Biome lint,
  gofmt and diff checks passed. No unreviewed broad commonization was added.

| Lightweight release verification | Result |
| --- | --- |
| Configured CLI version test | PASS |
| Current README/release relative links | PASS, 120 references resolve |
| Clean native release-flags build | PASS, CGO disabled, checked source above, vcs.modified=false |
| Candidate version/arithmetic | PASS, noe v2.8.2 and 2 |
| Engine dependencies | Unchanged: go-quickjs v0.29.0, go-intl v0.8.0 |
| Source delta after implementation | CLI version and release documentation only |

No full suite, race, long-duration check or six-binary local distribution
build was repeated for release preparation. The unchanged lightweight CI
will build six distribution targets, create SHA256SUMS and publish the
immutable tag. Published assets, native smoke behavior and actual Go-module
retrieval are verified after publication. Native execution is Linux amd64;
other targets receive build/hash verification, not native execution here.

The image guard is a pre-read type check, not atomic protection against
concurrent path replacement. Generic filesystem APIs remain unchanged.
See the [release notes](v2.8.2.md).

Evidence and detached source:
`/tmp/noe-v2.8.2-release-3336898421`.
This request authorizes publication, not local installation; repository-root
and PATH executables and existing processes remain unchanged. Existing user
logs/caches and the separately modified development instruction file are
preserved and excluded from release commits.

## v2.8.2 Release validation — 2026-10-26

- Annotated tag `v2.8.2` points to
  `3cd4d996e31df553206839924005bf001e92a61f`. The remote peeled tag and
  successful workflow revision agree. Its difference from checked source
  `9cff990b` is the preparation evidence document only.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/43)
  succeeded (API run ID 224) using the unchanged lightweight
  build/checksum/upload job.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.8.2)
  is published, neither draft nor prerelease (Release ID 156).
  Its body matches the tagged [English release notes](v2.8.2.md).

All seven assets were downloaded. All six binary hashes match SHA256SUMS.
The actual distributed Linux amd64 binary passed version/arithmetic smoke
checks, reporting `noe v2.8.2` and `2`, the tagged source revision,
`vcs.modified=false`, `CGO_ENABLED=0`, go-quickjs v0.29.0 and go-intl
v0.8.0. Its SHA256 is
`4fc82d25a5794304b0c7b5e5037c4e24b17fce229dbdd84d3666cd543133f160`.

Clean external modules fetched `stdlib@v2.8.2` and `agent@v2.8.2`
from the actual published Go module, without local replacements or a
synthetic proxy. Both consumers built and passed `go mod verify`;
the stdlib consumer and the agent portable/gohost modes ran successfully.
MVS selected official go-quickjs v0.29.0 and go-intl v0.8.0.

The downloaded module's image binding/codec and agent API/task module
sources match the checked source, including the image-only regular-file
guard and single normalization. Both public declaration files also match.
The implementation's FIFO regression and managed image behavior evidence
are reused; source suites were not repeated merely for publication.
No paid model-provider request was made.

Evidence is retained at `/tmp/noe-v2.8.2-release-3336898421`, including the
clean native candidate, seven published assets, build metadata,
published-verification.json, public-module-verification.json and both
external consumers. Native runtime verification is Linux amd64; other five
targets have CI build/hash verification without native execution here.

This documentation-only follow-up does not move or replace the release tag.
Local installation was not requested: repository-root/PATH executables and
running processes remain unchanged. Existing user logs, caches and the
separately edited development instruction file remain preserved and
excluded from these commits.

## v2.8.3 preparation — 2026-10-26

The user authorized release of the instruction changes made in another session.
Implementation commit `1b84390db3414bd4b7e9805fd4a53f25e262bc1d` separates
runtime/API facts from editable behavioral guidance, removes the CLI's embedded
policy injection, and updates external guidance, notices and related tests.

Checked clean release source:
`122272c32983419445c1e8c903cc592a06d16044`.
Only CLI version/release documentation changed after the checked implementation.
Dependencies and the six-target lightweight release workflow are unchanged.
This preparation follow-up changes only this evidence document.

Implementation verification passed:
- `go test ./internal/agent ./internal/noeprofile ./internal/runtime ./internal/apidocs ./internal/agentexec -count=1`.
- CLI instruction-file run/eval E2E:
  `go test -tags=e2e ./tests -run '^TestAgentInstructionFilesRunAndEval$' -count=1`.
- Instruction absence, replacement/removal, instance snapshots, child loading,
  no-comment run_js data, and retained context-history behavior are covered.
- Development Staticcheck, changed-JavaScript Biome lint, gofmt and diff checks.
- No remaining live references to the removed internal Policy function/file.

Light release verification passed the configured version test, 302 current
documentation links, and a clean CGO-free native release-flags build.
The candidate reports `noe v2.8.3`, evaluates arithmetic to `2`, and its
`noe docs noe:run_js run_js` detail contains the updated optional-comment
contract. Provenance is the checked source with `vcs.modified=false`,
go-quickjs v0.29.0 and go-intl v0.8.0.

No unrelated full suite, race, long-duration check, or six-binary local
distribution build was repeated. CI builds/checksums/uploads the immutable
tag without duplicating local source tests. Published assets and public-module
availability are checked afterward. Native execution is Linux amd64; other
targets receive build/hash verification.

Evidence and clean source: `/tmp/noe-v2.8.3-release-23267156`.
[Release notes](v2.8.3.md) explain the intended CLI guidance change and the
unchanged public Go host policy option. External files are not installed or
recreated by the binary. Local installation and process restarts are not
part of this request. User event logs and caches are preserved/excluded.

## v2.8.3 Release validation — 2026-10-26

- Annotated tag `v2.8.3` points to
  `4e527ffe7dc489bf53cd3359e2408a4cf8e5e89a`, matching the successful
  workflow revision. Only preparation evidence differs from checked source
  `122272c3`.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/44)
  succeeded (API run ID 225) using the unchanged lightweight job.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.8.3)
  is published, neither draft nor prerelease (Release ID 157), with a body
  matching the tagged [English release notes](v2.8.3.md).

All seven assets were downloaded and all six binary hashes match SHA256SUMS.
The actual distributed Linux amd64 binary reports `noe v2.8.3`, evaluates
arithmetic to `2`, and its detailed run_js documentation contains the updated
optional-comment contract, not the former default-policy wording.
Provenance is the tagged source with `vcs.modified=false`,
`CGO_ENABLED=0`, go-quickjs v0.29.0 and go-intl v0.8.0.
Its SHA256 is
`07099800b5f94d0cc3357dccaf0f43efa7a5177e379bde96c9546194816b4886`.

Clean external modules fetched `stdlib@v2.8.3` and `agent@v2.8.3`
without local replacements or a synthetic proxy. The stdlib consumer and
agent portable/gohost modes built and ran; both modules passed
`go mod verify`. Selected engine/intl versions match those above.
Published runtime facts, run_js description, instruction-reader/CLI
composition and embedded reference sources match the checked source.
The removed internal policy.md is absent from the published module.

Implementation instruction-composition and CLI run/eval evidence is reused;
source suites were not repeated for publication. No paid model-provider
request was made. Evidence is retained at
`/tmp/noe-v2.8.3-release-23267156`, including seven published assets,
workflow/release metadata, candidate and published verification records,
embedded contract output and public-module consumers.

Native runtime verification is Linux amd64; other targets have CI build/hash
verification, not native execution here. This documentation-only follow-up
does not move the release tag. Local executable installation/process restarts
were not requested and were not performed. External instruction files remain
user-controlled; the binary does not install or restore them. Existing user
logs and caches are preserved and excluded from commits.

## v2.9.0 preparation — 2026-10-27

The user authorized release after committing the streamlined development
workflow and completing an isolated smoke task. Latest published release:
v2.8.3. The new minor version includes the portable agent-led workflow and
the five core cleanup/efficiency slices. See [release notes](v2.9.0.md).

Checked clean candidate source:
`bcdb28699040db64e1cab3d8bf70b34707a9a8d5`.
Its only production-Go difference from implementation `b362011` is the
CLI version string. Dependencies, Go 1.27.2, go-quickjs v0.29.0, go-intl
v0.8.0, no-JIT configuration and lightweight release workflow are unchanged.
This preparation follow-up changes only this evidence document.

Implementation evidence is reused:
- Five ordered core slice commits `038bfcc`, `54f904b`, `fcdbd4e`,
  `6e0d6fe` and `3050fdc` passed focused checks and independent reviews.
  Coordinated whole-tree unit tests and vet passed, as did targeted
  cancellation race checks. Windows command-environment checks were
  cross-compiled and reviewed, not executed natively.
- Workflow simplification `b362011` passed offline configuration/termination,
  copied-package/no-config and help checks, JavaScript lint/type checks and
  source/diff checks. Decision configuration/defaults were removed; only
  role settings remain.
- A real one-slice isolated display-label task completed independent
  normal/critical plan reviews, Terra implementation with focused tests,
  independent code review, a local commit and a verified push to a local
  bare remote. Wall time was 408,330 ms; 2,141 recorded events contain zero
  decision events. This is not a controlled speed comparison.
- The bounded thinking-fragment benchmark in the release notes is reused;
  it demonstrates local prefix-copy reduction, not end-to-end speedup.

| Lightweight release verification | Result |
| --- | --- |
| Configured CLI version test | PASS |
| Clean CGO-free native release-flags build | PASS, checked revision above, vcs.modified=false |
| Candidate version/arithmetic | PASS, noe v2.9.0 and 2 |
| Candidate workflow offline contract/configuration/termination checks | PASS, no provider calls |
| Current README/release relative links | PASS, 127 references resolve |
| Formatting/diff and production/dependency delta checks | PASS |
| Unrelated tracked edits and local executable hashes | Unchanged and excluded |

Evidence and clean detached source:
`/tmp/noe-v2.9.0-release-2166384416`.
Core command evidence and the real workflow smoke result are copied into
its reused-evidence directory. No full suite, E2E, race, long wait, repeated
paid task or six-binary local distribution build was rerun for release.
CI builds the six profile targets, generates SHA256SUMS and uploads the
immutable tag without duplicating local source tests. Published assets
and actual Go-module retrieval are checked after publication.

Native execution here is Linux amd64; the other five targets receive
build/hash verification. This request authorizes publication, not local
installation or process restarts. Repository-root/PATH binaries, user
logs/caches and unrelated worktree changes are preserved.

## v2.9.0 Release validation — 2026-10-27

- Annotated tag `v2.9.0` points to
  `9aee19f73819ed9a46020dafefbc20f3be6e836c`. The peeled remote tag and
  successful workflow revision agree. Only this preparation evidence
  document differs from checked source `bcdb286`.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/45)
  succeeded (API run ID 226) using the unchanged lightweight
  six-build/checksum/upload job.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.9.0)
  is published, neither draft nor prerelease (Release ID 158). Its body
  matches the tagged [English release notes](v2.9.0.md).

All seven assets were downloaded; all six binary hashes match SHA256SUMS.
The actual distributed Linux amd64 binary reports `noe v2.9.0`, the tagged
source revision, `vcs.modified=false`, `CGO_ENABLED=0`, go-quickjs v0.29.0
and go-intl v0.8.0. Its SHA256 is
`25991ff74bbf24966c99499ee21cdeb812ab19ecfb23f24d2a6ae8e8787312d0`.
Version/arithmetic and the copied workflow's offline configuration,
instruction and native-termination contract checks pass under this asset.

Clean external modules fetched `stdlib@v2.9.0` and `agent@v2.9.0` from the
actual published module with GOPROXY=direct, without local replacements or
a synthetic proxy. Both built and passed `go mod verify`; the stdlib
consumer and the agent portable/gohost modes ran successfully.
MVS selected official go-quickjs v0.29.0 and go-intl v0.8.0.
The downloaded core source files for all five cleanup areas and both
public declaration files match the checked source.

The first consumer-verification helper used an incorrect Intl module path
and failed its metadata query after a successful module fetch. The helper
was corrected to github.com/go-quickjs/go-intl, completed steps were reused,
and all final consumer checks passed. This was a verification-script typo,
not a product failure; its initial result is retained with the final evidence.

Evidence is retained at `/tmp/noe-v2.9.0-release-2166384416`, including
clean source/candidate, reused implementation and real-workflow smoke
evidence, native Actions metadata, published release metadata, all seven
assets, checksums/build information and public-module consumer logs/results.
No paid model-provider calls or full source-suite reruns were made during
release preparation/publication. Native execution here is Linux amd64;
the other five targets have CI build/hash evidence, not native execution.

This documentation-only follow-up does not move or replace the release tag.
Local installation/process restarts were not requested and were not
performed. Repository-root and PATH executable hashes and the original
unrelated worktree status remain unchanged; user files/logs/caches are
preserved and excluded.

## v2.10.0 preparation — 2026-10-28

The user authorized committing the short instruction-option names, release,
and local package placement. Latest published release: v2.9.0.
Implementation `c4fa378` makes `--inst DIR` / `--inst=DIR` and
`--no-inst` canonical, retaining the original long compatibility aliases.
This release also contains directory-selection `d20d883`, its help-test
correction `63aa502`, and the previously committed Terra catalog metadata.
See [release notes](v2.10.0.md).

Checked clean release source:
`d9602b39077ff0a47c3f4e059e1cfb1661ce7f8d`.
Its only production-source difference from verified implementation
`c4fa378` is the CLI Version string. Go dependencies and the unchanged
six-target lightweight release workflow are unchanged. This preparation
follow-up adds only this evidence section.

Valid implementation evidence is reused:
- The original directory-selection feature passed independent plan/code
  reviews and deterministic CLI/runtime/source-reader checks. Its single
  overall unit run passed all packages except a stale help-test assertion;
  after the test-only correction, the complete CLI package passed.
  Affected CLI/runtime/profile vet passed.
- Short names, old aliases, mixed ordering/conflicts, empty selection,
  actual run/eval composition, lazy behavior and script boundaries passed
  the complete CLI package (`go test ./internal/cli -count=1`, 3.972s).
  CLI vet, gofmt and diff checks passed.

| Lightweight release verification | Result |
| --- | --- |
| Configured CLI version test | PASS |
| Current README/release/instruction relative links | PASS, 138 references |
| Clean CGO-free native release-flags build | PASS, checked revision above, vcs.modified=false |
| Candidate version/help/arithmetic | PASS, noe v2.10.0 and 2 |
| Native run/eval local-provider fixture | PASS, default/ordered replacement/explicit empty/old aliases and cwd |
| Native usage and script boundaries | PASS, missing value/conflict errors and unchanged arguments |
| Dependencies and release workflow | Unchanged |

The candidate fixture makes only deterministic loopback provider requests;
it makes no paid or external model-provider request. No full suite, race,
long wait or six-binary local distribution build is repeated for release.
CI builds six targets, creates SHA256SUMS and uploads the immutable tag.
Published checksums, native instruction-option behavior and actual public
Go-module retrieval will be verified after publication.

Evidence and clean detached source: `/tmp/noe-v2.10.0-release-ymFznQ`.
The user also authorized local package placement: install only the verified
published Linux amd64 asset into repository-root `./noe` and
`/home/agent/.local/bin/noe`, with backups and atomic replacement.
Both currently report v2.8.1. Existing running processes will not be restarted.
Native runtime verification is Linux amd64; other five targets have
CI build/hash verification, not native execution here.
The three pre-existing local discussion/run memos, user logs and caches
are preserved and excluded from commits.

## v2.10.0 Release validation and package placement — 2026-10-28

- Annotated tag `v2.10.0` points to
  `8637edcd17d91e7737777fd8a5c6ee490aaf9809`, matching the peeled remote tag and
  successful native workflow revision. Only preparation evidence differs
  from checked clean source `d9602b39077ff0a47c3f4e059e1cfb1661ce7f8d`.
- [Release workflow](https://forge.harakara.site/littleisland/noe/actions/runs/46)
  succeeded (API run ID 227) using the unchanged lightweight
  six-target build/checksum/upload job.
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.10.0)
  is published, neither draft nor prerelease (Release ID 159).
  Its body matches the committed English [release notes](v2.10.0.md).

All seven assets were downloaded and all six binary hashes match SHA256SUMS.
The actual distributed Linux amd64 asset reports `noe v2.10.0`, the tagged
source revision, `vcs.modified=false`, `CGO_ENABLED=0`, go-quickjs v0.29.0
and go-intl v0.8.0. Its SHA256 is
`4659e68efce9a33a82979f88d76d03a5ae4bfe28c279fd8c1c0d423912c86aaa`.

The published binary passed version/help/arithmetic and the deterministic
local-provider fixture: both run/eval, default sources, ordered explicit
replacement and cwd, explicit-empty selection, old aliases, usage errors,
and unchanged script arguments. Built-in facts and explicit system text
remain. These fixture requests are loopback-only; no paid model-provider
request was made.

Clean external modules fetched stdlib@v2.10.0 and agent@v2.10.0 from the
actual published module with GOPROXY=direct, no local replacements or
synthetic proxy. Both built and passed `go mod verify`; the stdlib consumer
and agent portable/gohost modes ran. MVS selected official go-quickjs
v0.29.0 and go-intl v0.8.0. Published instruction-reader, managed-host,
CLI-option, catalog and both public declaration files match the checked source.

The verified published asset atomically replaced both
`/home/agent/projects/noe/noe` and `/home/agent/.local/bin/noe`.
Both hashes match the published checksum and both report `noe v2.10.0`.
PATH resolves to the updated user binary. Installed invocation checks for
`--inst` and `--no-inst` passed. Previous v2.8.1 executables are backed up at
`/tmp/noe-v2.10.0-release-ymFznQ/installation`.
Existing running processes were not restarted or interrupted.

Evidence is retained at `/tmp/noe-v2.10.0-release-ymFznQ`, including
candidate/source evidence, native Actions and Release metadata, all seven
assets, published checksums/build info, CLI fixture results, actual
external module consumers and installation.json. Other five targets have
CI build/hash verification, not native execution here. Previously valid
source suites were reused rather than repeated for publication.

This documentation-only follow-up does not move or replace the release tag.
The three pre-existing local memos remain unchanged and untracked;
user instruction files, logs and caches are preserved.
