# OpenRouter SDK → OpenCode Go spike

Use the existing pinned OpenRouter Go SDK (`v0.9.59`) against OpenCode Go without adding SDK dependencies or changing production provider selection. Results and limitations: [validation record](../../docs/development/opencode-go-sdk-spike.md).

Local verification (no credentials or network):

```sh
go test ./spikes/opencode-go -count=1
```

Explicit live verification (consumes the OpenCode Go subscription allowance):

```sh
CGO_ENABLED=0 go build -o noe ./cmd/noe
NOE_OPENCODE_SPIKE_KEY_FILE=/path/to/opencode-go-api-key \
NOE_OPENCODE_SPIKE_DIR=/path/to/new-artifacts-directory \
  go test ./spikes/opencode-go -run '^TestLive$' -count=1 -v
```

The four cases make five provider requests: Chat non-streaming, a streamed Chat tool cycle using NOE's existing `provider.Generate` and actual `noe eval`, Responses non-streaming, and Responses streaming. The SDK's retries are disabled. Each case has a 75-second context deadline.

The transport sets `User-Agent: noe/<version>` and `x-opencode-session` from the per-request run context. A local test checks parent → child → parent IDs. The live tool cycle keeps one ID across both requests. This spike assigns a logical run ID to each case; it does not launch a managed parent/child instance. Production integration must wire the real NOE run ID to the same context boundary.

Artifacts include request JSON, response JSON/SSE, and selected non-secret headers. Authorization values are never recorded. The key is read from the explicitly supplied path, not copied into the repository. No external requests are made unless the key-file environment variable is set.

Responses requests explicitly set `ServiceTier: auto`: the pinned SDK fails locally while encoding its default if that optional field is unset. This workaround is confined to the spike. The existing Chat adapter also needs `reasoning_content` handling before it can preserve and observe OpenCode Go's thinking correctly; a successful simple tool cycle alone does not validate full reasoning replay.
