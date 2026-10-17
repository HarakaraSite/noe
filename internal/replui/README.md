# qjs-derived REPL input

Source: `github.com/go-quickjs/go-quickjs v0.25.3`, `cmd/qjs/lineedit.go`,
`terminal.go`, `terminal_other.go`, `terminal_windows.go`, and the editor tests
from `lineedit_test.go`. The upstream MIT license is retained in `LICENSE`.

These five source files and the license were compared with v0.26.0 and
v0.27.0 on 2026-10-17 and are byte-for-byte unchanged upstream. NOE's engine
dependency is now v0.27.0; the snapshot provenance remains v0.25.3.

This is a source snapshot, not a fork of the engine or an external qjs process.
The upstream REPL is private to `package main`, so the reusable input portion
is adapted here. JavaScript evaluation, await handling, completion candidates,
agent operations, cancellation, and persisted history remain owned by NOE.

Local adaptations:

- Package and a small `Input` interface for the CLI.
- Terminal restoration errors are returned to the host; raw/restore/Close are
  synchronized and Windows output-mode changes are restored on Close.
- Completion replacement uses rune boundaries and accepts the host's complete
  identifier-chain candidates.
- Ctrl+R/S history search preserves NOE's existing terminal operation. An
  unmatched edited query clears the selection rather than running a stale match.
- Embedded newlines in recalled logical history are rendered visibly while
  the original multiline JavaScript remains intact.
- Ctrl+C and Ctrl+D remain effective inside an unfinished ESC/CSI/SS3 sequence.

When updating the pinned engine, compare these files against its `cmd/qjs`
sources and run NOE's input, history, completion, terminal, and agent REPL
checks. No changes are made to the Go module cache.
