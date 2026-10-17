# NOE release checkpoints

[README](../README.md) · [Development notes](README.md) · [Release profile](../.forgejo/release-profile.yml) · [Release workflow](../.forgejo/workflows/release.yml)

The release contract is stored in `.forgejo/release-profile.yml`. Its `version: 2` is a schema version; the first application release is `v1.0.0`.

The `v1.0.0` evidence below refers to the listed release commits. It predates the [stdlib migration](increment-12.md) and does not validate or distribute the stdlib migration. Release-specific checks are recorded separately. See the migration record for local checks and the [API reference](api.md) for current behavior.

## Distribution gate

The Forgejo workflow generated from this profile runs the following on the confirmed `ubuntu-latest` runner when a `v*` tag is pushed:

1. Set up the Go version specified in `go.mod` (currently 1.27.2).
2. Run `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go vet ./...`.
3. Build `./cmd/noe` with CGO disabled, `-trimpath`, and the profile's ldflags. Embed the tag in `noe/internal/cli.Version`.
4. Produce `noe-linux-amd64`, `noe-linux-arm64`, `noe-darwin-amd64`, `noe-darwin-arm64`, `noe-windows-amd64.exe`, and `noe-windows-arm64.exe`, plus `SHA256SUMS`.
5. Upload the binaries and checksums on success using `actions/forgejo-release@v2` and the automatic `forgejo.token`.

The uploader creates a non-draft Release within the existing repository visibility; it does not make a private repository public. Failed gates or builds must leave their output in native Actions logs and must not publish Release assets.

The `v1.0.0`, `v1.1.0`, `v1.2.0`, `v1.3.0`, `v1.3.1`, and `v1.3.2` Releases have been published through this workflow; see their validation records below. A successful cross-build verifies compilation; runtime and terminal behavior on macOS, Windows, and arm64 have not been tested here.

## Reference checks before tagging

Run these commands from the repository root on VM201 (Linux amd64, Go 1.27.2, C toolchain, Linux PTY), and record the code revision and results here before tagging:

```sh
CGO_ENABLED=1 go test -race ./internal/... -count=1
go test -tags=e2e ./... -count=1
go vet -tags=e2e ./...
```

These are reference-environment checks, not generated workflow steps. E2E includes the actual CLI, local HTTP and file operations, and PTY REPL interaction. Race requires a C toolchain. No system-package installation is added to the release workflow to run these checks.

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

This release enables the full pinned go-quickjs stdlib and removes the `noe` JavaScript global. Existing scripts require migration; see the [v1.1.0 release notes](releases/v1.1.0.md) and [API migration table](api.md#旧apiからの移行).

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
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.1.0): published, neither draft nor prerelease. Its description matches the committed [release notes](releases/v1.1.0.md), including the removed `noe.*` APIs and migration guidance.

The actual workflow passed the CGO-free test and vet gates, built all six profile targets, and uploaded the six binaries plus `SHA256SUMS`. All seven assets were then downloaded from the Release. Every binary hash matched the published checksum file.

The downloaded Linux amd64 binary returned `noe v1.1.0` for `--version`. An evaluation importing `timers/promises` and waiting on its timer confirmed the removed `noe` global and the available Fetch/server APIs, printing `undefined function function` with exit code 0. The peeled remote tag was checked against the workflow commit SHA.

This verification record is committed after publication. It does not move or replace the `v1.1.0` tag. The local repository-root `./noe` was also rebuilt with version `v1.1.0`.

## v1.2.0 preparation — 2026-10-16

Checked source revision: `55bbbe4f94e1510c47375c21ee0c99e2db140280`. The checks ran against this source immediately before the commit. This preparation entry is documentation-only; the tag includes it without changing the checked application code. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support.

This release adds the OpenRouter-backed `noe:agent` module, generated JavaScript execution, autonomous repair, parallel child agents, file-based credentials, and opt-in execution tracing. README versions, the default CLI version, and version expectations in unit/E2E tests are updated to `v1.2.0`. See the [release notes](releases/v1.2.0.md).

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

The [increment 13 implementation record](increment-13-implementation.md) documents per-slice and whole-feature reviews and real OpenRouter aggregate/repair/parallel acceptance. A further live run of `try-agent.js` edited README.md and reread the result without human intervention: 28.294 seconds, exit 0, seven model calls and seven successful JS executions. Its task, trace, output, before/after README, diff, and verification are under `/tmp/noe-readme-agent-spse4kwn/`. These observations used the explicitly authorized OpenRouter key and `deepseek/deepseek-v4.1-flash`; no further live API call was needed for this version/documentation-only release preparation.

The existing tag-triggered Forgejo workflow remains the distribution gate. Its result and the published assets are checked separately after publication. macOS/Windows/arm64 native execution remains unverified; successful cross-builds are compilation evidence.

## v1.2.0 Release validation — 2026-10-16

- Annotated tag: `v1.2.0`, pointing to `32e031102506b10c40b55f204b49974412fbbaa5`. This contains the checked source revision and the preparation record above. The peeled remote tag matches this workflow commit.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/3): succeeded (API run ID 181, job ID 202).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.2.0): published, neither draft nor prerelease (Release ID 117). Its description matches the committed [release notes](releases/v1.2.0.md).

The actual workflow set up Go 1.27.2, passed the CGO-free unit-test and vet gates, built all six profile targets, and uploaded the binaries plus `SHA256SUMS`. All seven assets were downloaded after publication, and all six binary hashes matched the published checksum file.

The downloaded Linux amd64 binary returned `noe v1.2.0` for `--version`, `2` for an arithmetic evaluation, and `function function function undefined` for lazy `noe:agent` import/create plus Fetch/server and removed-global checks. All three exited with 0 and empty stderr; the agent check did not make a model request. Downloaded artifacts and verification records are under `/tmp/noe-v1.2.0-published-izk96800/`.

This verification entry is committed after publication and does not move or replace the `v1.2.0` tag. The repository-root `./noe` was updated to the verified published Linux amd64 binary. macOS/Windows/arm64 native execution is still unverified.

## v1.3.0 preparation — 2026-10-17

Checked source revision: `0c87d0fbb20fd06b69771ef0509da0058bedfde5`. The checks ran against this application source immediately before the commit; the final source-tree adjustment was removal of a trailing blank line in a validation document. This preparation entry is documentation-only. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support.

This release includes managed agent lifecycle/control and qjs REPL integration since v1.2.0, plus structured decisions, OpenCode Go, OpenAI SDK/ChatGPT credentials, configurable providers and a model catalog, and richer execution diagnostics. Both READMEs now explain credential registration, root startup, results and failures, child inheritance, concurrency, keepAlive, control commands, and observation. The public agent API breaks compatibility with v1.2.0; see the [release notes](releases/v1.3.0.md).

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

Existing live acceptance evidence is recorded in [agent controls](agent-control-openrouter-validation.md), [decisions](decision-openrouter-validation.md), [OpenCode Go/provider configuration](provider-config-validation.md), and [OpenAI](openai-implementation.md). The live ChatGPT tests used refreshed existing credentials; fresh browser registration and the OpenAI API-key route were fixture-tested. No further external model request was made during this release preparation.

The tag-triggered Forgejo workflow remains the distribution gate. Publication and downloaded assets are verified separately after tagging.

## v1.3.0 Release validation — 2026-10-17

- Annotated tag: `v1.3.0`, pointing to `68f1f298b6b5ba95659429b3f613fa665baa37db`. The peeled remote tag matches the successful workflow commit; this includes the checked source and preparation record above.
- [Forgejo release workflow run](https://forge.harakara.site/littleisland/noe/actions/runs/4): succeeded (API run ID 185, job ID 206).
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.3.0): published, neither draft nor prerelease (Release ID 119). Its description matches the committed [release notes](releases/v1.3.0.md).

The actual workflow passed its CGO-free test/vet gates, built all six profile targets, and uploaded the binaries plus `SHA256SUMS`. All seven assets were downloaded after publication; every binary hash matched the published checksum file.

The downloaded Linux amd64 binary passed four smoke checks with exit 0 and empty stderr: `--version` returned `noe v1.3.0`; arithmetic evaluation returned `2`; lazy agent/decision/auth imports, Fetch/serve, removed-global, and OpenAI catalog checks passed; the external ChatGPT helper displayed its help. No model provider request was needed. The helper's versioned download URL was also checked against the committed JavaScript. Downloaded artifacts and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.0-published-82a__6s7/`.

The repository-root `./noe` is now the verified published Linux amd64 binary. This documentation-only verification commit follows publication without moving the tag. Native runtime behavior on macOS, Windows, and arm64 remains unverified.

## v1.3.1 preparation — 2026-10-17

Checked source revision: `1451a6272a9175f90ee3c7b0d9a3608f8a182ea2`. Checks ran against the same application and test source immediately before this commit. The cross-builds preceded a test-only cancellation-fixture adjustment; application code remained identical. This preparation entry is documentation-only. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support.

This release updates go-quickjs from v0.25.3 to v0.27.0. Compatibility was checked through v0.26.0 first, with full unit/CLI/REPL E2E success for both versions using separate modfiles. The qjs-derived five input files and MIT license are byte-for-byte identical upstream across all three versions; NOE keeps the existing source snapshot and provenance. `Workers.Max: -1` preserves the existing unlimited Worker count. See the [release notes](releases/v1.3.1.md).

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
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.3.1): published, neither draft nor prerelease (Release ID 120). Its description matches the committed [release notes](releases/v1.3.1.md).

The workflow passed its CGO-free test/vet gates, built all six profile targets, and uploaded six binaries plus `SHA256SUMS`. All seven assets were downloaded; all six binary checksums matched the published file.

The published Linux amd64 binary passed four smoke checks with exit 0 and empty stderr: version (`noe v1.3.1`), arithmetic (`2`), lazy agent/decision/auth imports with Fetch/serve and the OpenAI catalog, and the external ChatGPT helper's help command. Its embedded Go build information confirms `github.com/go-quickjs/go-quickjs v0.27.0`. The helper's versioned download URL also matches the committed script. No model request was made. Artifacts and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.1-published-kp5497v6/`.

The repository-root `./noe` is the verified published Linux amd64 binary. This documentation-only verification commit follows publication without moving the tag. macOS, Windows, and arm64 native runtime behavior remains unverified.

## v1.3.2 preparation — 2026-10-17

Checked source revision: `d8a58e857292340088fb9f5e2b07512e9bfe3ea1`. The same application, test source, and dependencies were checked immediately before/after this commit; no code change intervened. This preparation entry is documentation-only. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support.

This release updates go-intl, OpenRouter SDK, gjson, pretty, match, ajson, x/sys, and x/term after individual and combined compatibility checking. NOE's runtime implementation and JavaScript API remain unchanged. README links, CLI version, and version assertions are updated to v1.3.2. See the [release notes](releases/v1.3.2.md) and [dependency validation record](dependency-updates.md).

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
- [Release](https://forge.harakara.site/littleisland/noe/releases/tag/v1.3.2): published, neither draft nor prerelease (Release ID 121). Its description matches the committed [release notes](releases/v1.3.2.md).

The workflow passed its CGO-free test/vet gates, built all six profile targets, and uploaded six binaries plus `SHA256SUMS`. All seven assets were downloaded; all six binary checksums matched the published file.

The published Linux amd64 binary passed five smoke checks with exit 0 and empty stderr: version (`noe v1.3.2`), arithmetic (`2`), lazy agent/decision/auth imports, Intl normalization/collation, and the external ChatGPT helper's help command. Its embedded Go build information confirms all eight updated dependency versions and go-quickjs v0.27.0. The helper's versioned download URL also matches the committed script. No model request was made. Artifacts and verification records are under `/home/agent/.cache/noe-qjs-repl/noe-v1.3.2-published-k371cnhz/`.

The repository-root `./noe` is the verified published Linux amd64 binary. This documentation-only verification commit follows publication without moving the tag. macOS, Windows, and arm64 native runtime behavior remains unverified.

## v1.3.3 preparation — 2026-10-17

Checked source revision: `0387e10d8b22cc52beb8385ec182332d76fd5fc8`. Environment: Linux amd64, Go 1.27.2, a C toolchain for race, and Linux PTY support. Subsequent preparation changes only update this record and the observation guides' commands for the relocated example.

This release reorganizes the agent broker, SSE handling, recorded-run observation, and E2E tests. All 107 original implementation declarations were compared before and after splitting, including their bodies and attached comments, and are byte-for-byte unchanged. Existing E2E test names and build tags are retained; live acceptance reports now obtain the SDK version from the tested CLI. Dependency versions remain those of v1.3.2. The example is now `examples/try-agent.js`. See the [release notes](releases/v1.3.3.md) and [E2E test guide](../tests/README.md).

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
