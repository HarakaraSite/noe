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
