# OpenCode Go SDK investigation (retired)

The provider investigation is complete. Its prototype transport and tests were
retired in v1.16.0; source before retirement is available in Git at `7b43646`.
The [validation record](../../docs/development/opencode-go-sdk-spike.md) retains
the original observations.

Current provider configuration, session headers, reasoning replay, cancellation,
and SDK transport regressions are tested in `internal/agent/provider` and
`internal/cli`. The opt-in [three-provider cache probe](../../scripts/spikes/provider-cache-probe/README.md)
provides the current SDK comparison. Do not use this historical spike as a
production regression gate.
