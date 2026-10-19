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

These cases use only `opencode-go` / `deepseek-v4.1-flash`. See the [validation record](../docs/development/execution-hooks-opencode-validation.md) for current acceptance results.

Context trimming has a separate opt-in behavior test. Use a new artifact directory for every trial; existing case directories are rejected so a previous report cannot affect the result.

```sh
NOE_AGENT_CONTEXT_LIVE=1 NOE_AGENT_CONTEXT_LIVE_DIR=/path/to/fresh/results \
NOE_OPENCODE_GO_API_KEY_FILE=/path/to/key \
  go test -v -tags='e2e context_live' ./tests -run '^TestContextOpenCodeLive$' -count=1 -timeout=14m
```

It compares retain ratios 0.1/0.2 and disabled trimming with controlled tool-output padding, then runs a natural task without padding. Each task has a 12-request / 180-second experiment bound. See the [implementation and behavior record](../docs/development/context-trimming-implementation.md); optional memory-file creation is not a success condition.

`process_io*_e2e_test.go`は新しい`noe:process`・`noe:io`を実CLIで確認します。開いたままの入力、40 MiBの転送、観察出力、Worker終了、fixture providerからの生成run_jsのinterrupt/timeout/SIGINTと子・孫回収を含みます。実providerへの接続は不要です。公式MCP SDKの任意の適合確認は[scripts/spikes/process-io-mcp](../scripts/spikes/process-io-mcp/README.md)へ分離しています。

`low_level*_e2e_test.go`は`noe:fs-handle`・`noe:sockets`・`noe:signals`の公開APIを実CLIで確認します。位置指定I/OとTCP/Unixの並行往復、送信FIN後の応答、run/eval・REPL・Worker・run_js・JSONL/observe、fixture agent、待受とI/Oのtimeout/SIGINT、Worker終了、signal解除後の既定動作、通常closeと強制終了後のUnix pathnameを扱います。OSシグナルは専用の子プロセスへ送ります。

`docs_e2e_test.go`は移動したバイナリ単体の内蔵docsと`run_js`の本文一致を確認します。自然な課題からの自発参照は、明示opt-inの実プロバイダ試験です。課題本文にdocsの指示は含めず、初期instructionsからの参照と成果物を確認します。

```sh
NOE_AGENT_DOCS_LIVE=1 NOE_AGENT_DOCS_LIVE_DIR=/path/to/fresh/results \
NOE_OPENCODE_GO_API_KEY_FILE=/path/to/key \
  go test -v -tags='e2e docs_live' ./tests -run '^TestDocsOpenCodeLive$' -count=1 -timeout=7m
```

OpenCode Go / `deepseek-v4.1-flash`を使い、1課題あたり最大10要求・175秒で確認します。[実装・検証記録](../docs/development/builtin-docs-implementation.md)を参照してください。
