# NOE release checkpoints

[README](../../README.md) · [Development guide](../development/README.md) · [Release notes](README.md) · [Release profile](../../.forgejo/release-profile.yml) · [Release workflow](../../.forgejo/workflows/release.yml)

The release contract is stored in `.forgejo/release-profile.yml`. Its `version: 2` is a schema version; the first application release is `v1.0.0`.

The `v1.0.0` evidence below refers to the listed release commits. It predates the [stdlib migration](../archive/increment-12.md) and does not validate or distribute the stdlib migration. Release-specific checks are recorded separately. See the migration record for local checks and the [API reference](../api.md) for current behavior.

## Distribution gate

The Forgejo workflow generated from this profile runs the following on the confirmed `ubuntu-latest` runner when a `v*` tag is pushed:

1. Set up the Go version specified in `go.mod` (currently 1.27.2).
2. Run `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go vet ./...`.
3. Build `./cmd/noe` with CGO disabled, `-trimpath`, and the profile's ldflags. Embed the tag in `noe/internal/cli.Version`.
4. Produce `noe-linux-amd64`, `noe-linux-arm64`, `noe-darwin-amd64`, `noe-darwin-arm64`, `noe-windows-amd64.exe`, and `noe-windows-arm64.exe`, plus `SHA256SUMS`.
5. Upload the binaries and checksums on success using `actions/forgejo-release@v2` and the automatic `forgejo.token`.

The uploader creates a non-draft Release within the existing repository visibility; it does not make a private repository public. Failed gates or builds must leave their output in native Actions logs and must not publish Release assets.

The `v1.0.0`, `v1.1.0`, `v1.2.0`, `v1.3.0`, `v1.3.1`, `v1.3.2`, `v1.3.3`, `v1.4.0`, and `v1.5.0` Releases have been published through this workflow; see their validation records below. A successful cross-build verifies compilation; runtime and terminal behavior on macOS, Windows, and arm64 have not been tested here.

## Reference checks before tagging

Run these commands from the repository root on VM201 (Linux amd64, Go 1.27.2, C toolchain), and record the code revision and results here before tagging:

```sh
CGO_ENABLED=1 go test -race ./internal/... -count=1
go test -tags=e2e ./... -count=1
go vet -tags=e2e ./...
```

These are reference-environment checks, not generated workflow steps. E2E includes the actual CLI, local HTTP and file operations, agents, and cancellation/cleanup. REPL and its PTY checks were removed after v1.8.0; the historical evidence below retains the checks performed for those releases. Race requires a C toolchain. No system-package installation is added to the release workflow to run these checks.

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
