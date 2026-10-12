# NOE release checkpoints

[README](../README.md) · [Development notes](README.md) · [Release profile](../.forgejo/release-profile.yml) · [Release workflow](../.forgejo/workflows/release.yml)

The release contract is stored in `.forgejo/release-profile.yml`. Its `version: 2` is a schema version; the first application release is `v1.0.0`.

## Distribution gate

The Forgejo workflow generated from this profile runs the following on the confirmed `ubuntu-latest` runner when a `v*` tag is pushed:

1. Set up the Go version specified in `go.mod` (currently 1.27.2).
2. Run `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go vet ./...`.
3. Build `./cmd/noe` with CGO disabled, `-trimpath`, and the profile's ldflags. Embed the tag in `noe/internal/cli.Version`.
4. Produce `noe-linux-amd64`, `noe-linux-arm64`, `noe-darwin-amd64`, `noe-darwin-arm64`, `noe-windows-amd64.exe`, and `noe-windows-arm64.exe`, plus `SHA256SUMS`.
5. Upload the binaries and checksums on success using `actions/forgejo-release@v2` and the automatic `forgejo.token`.

The uploader creates a non-draft Release within the existing repository visibility; it does not make a private repository public. Failed gates or builds must leave their output in native Actions logs and must not publish Release assets.

The profile and release workflow have been created locally. The workflow has not yet run on Forgejo, and no tag or Release has been created as part of this work. A successful cross-build verifies compilation; runtime and terminal behavior on macOS, Windows, and arm64 have not been tested here.

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

NOE has no Actions runs yet. Go 1.27.2 setup and Release upload in NOE's own workflow remain to be verified when that workflow runs. No secret value is stored in the profile, and no additional PAT secret is configured.

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
