# End-to-end tests

These tests exercise a built NOE CLI. Package-level tests live beside their implementation under `internal/`.

```sh
go test -tags=e2e ./tests -count=1
```

The suite builds the CLI once with CGO disabled. Ordinary E2E tests use temporary files and local HTTP servers; they do not call real providers. Linux-only terminal tests use a PTY.

- `main_test.go`: shared CLI build and cleanup.
- `cli_helpers_test.go`: CLI execution, stream capture, environment, and file helpers.
- `cli_e2e_test.go`, `io_*`, `filesystem_*`, `binary_io_*`, `http_*`, `serve_*`, `sleep_*`, and `stdlib_*`: CLI and standard-library behavior.
- `repl_*`: piped REPL behavior and terminal editing, history, multiline input, completion, and interruption. Files containing `terminal` require Linux.
- `agent_*`, `observe_*`, and `decision_*`: agents, observation, control, and decisions. Agent model responses are served by local fixtures in `agent_e2e_test.go`.
- `fixtures/`: static inputs for live acceptance tests.

Live OpenRouter tests require additional build tags and explicit credential-file configuration. Run them only when real provider requests are intended:

```sh
# Agent cases; NOE_AGENT_LIVE_RESULTS optionally retains a JSON report.
NOE_OPENROUTER_API_KEY_FILE=/path/to/key \
  go test -tags='e2e agent_live' ./tests -run '^TestAgentOpenRouterLive$' -count=1

# Decision cases; the output directory retains reports and events.
NOE_OPENROUTER_API_KEY_FILE=/path/to/key NOE_DECISION_LIVE_DIR=/path/to/results \
  go test -tags='e2e decision_live' ./tests -run '^TestDecisionOpenRouterLive$' -count=1
```

`live_helpers_test.go` reads dependency versions from the built CLI for these reports.

Hook/context/execution-limit acceptance uses OpenCode Go and requires both the build tag and explicit opt-in. Create the artifact directory first; the suite retains scripts, events, and sanitized request metadata there. It does not save API keys or authorization headers.

```sh
NOE_AGENT_HOOKS_LIVE=1 NOE_AGENT_HOOKS_LIVE_DIR=/path/to/existing/results \
NOE_OPENCODE_GO_API_KEY_FILE=/path/to/key \
  go test -v -failfast -tags='e2e hooks_live' ./tests -run '^TestHooksOpenCodeLive$' -count=1 -timeout=30m
```

These cases use only `opencode-go` / `deepseek-v4.1-flash`. See the [validation record](../docs/execution-hooks-opencode-validation.md) for current acceptance results.

`process_io*_e2e_test.go`は新しい`noe:process`・`noe:io`を実CLIで確認します。開いたままの入力、40 MiBの転送、観察出力、Worker終了、fixture providerからの生成run_jsのinterrupt/timeout/SIGINTと子・孫回収を含みます。実providerへの接続は不要です。公式MCP SDKの任意の適合確認は[scripts/spikes/process-io-mcp](../scripts/spikes/process-io-mcp/README.md)へ分離しています。
