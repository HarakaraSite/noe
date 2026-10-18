# Agent images: provider spike

This is an opt-in investigation, not a public NOE image API. It uses the existing SDK versions and registered NOE credentials. Normal tests skip live requests.

```sh
mkdir -p /tmp/noe-image-spike
NOE_IMAGE_SPIKE_LIVE=1 NOE_IMAGE_SPIKE_DIR=/tmp/noe-image-spike \
  go test -v ./spikes/agent-images -run '^TestImageProviderSpike$' -count=1 -timeout=15m
```

`NOE_IMAGE_SPIKE_CASES` can select comma-separated cases to avoid repeating successful or paid requests. The default cases are:

- `vision-openrouter`: Chat Completions, `openai/gpt-6-luna`.
- `vision-opencode-go`: Responses, `gpt-6-luna`.
- `vision-openai-api`: Responses, `gpt-6-luna`, API key.
- `vision-chatgpt`: Responses, `gpt-6-luna`, ChatGPT sign-in.
- `image-openrouter`: dedicated Images API, `openai/gpt-image-2.5-flare`.
- `image-openai-api`: dedicated Images API, `gpt-image-2.5-flare`, API key.
- `image-chatgpt-direct`: probe the dedicated Images API with the ChatGPT token. This endpoint is not documented as supported by the ChatGPT plan flow.
- `image-chatgpt-responses`: supported public Responses endpoint, `gpt-6.1-sol` calling the `gpt-image-2.5-flare` image-generation tool, ChatGPT sign-in.

The vision fixture is a generated PNG with red on the left and blue on the right. Generation asks for one simple red-circle icon, low quality, 1024×1024. There are no automatic application retries or alternate-model fallbacks. Each case has a 180-second timeout.

Vision defaults to 4096 output tokens; `NOE_IMAGE_SPIKE_MAX_TOKENS=512` reproduces the smaller initial trial bound. `imageMatch` separately records whether the model answered red then blue. API completion alone does not establish visual correctness.

The artifact directory contains `input.png`, generated images and `report.json`. Reports contain endpoint, HTTP status, model, timing, text, usage and image dimensions, without authentication headers or Base64 image bodies. `success` in each report is the probe outcome; a passing Go test only means all selected probes were recorded. A failed provider probe does not become a successful API validation merely because the harness completed.

Credentials follow the existing NOE paths. The direct image probes use `XDG_CONFIG_HOME/noe/credentials` (or `~/.config/noe/credentials`); the vision registry uses NOE's normal provider configuration. If ChatGPT refresh rotates an existing shared refresh token, the matching hjh record is synchronized under its lock, using the previously authorized synchronization policy. Other accounts are not changed.

See the [plan](../../docs/agent-images-plan.md) and [spike findings](../../docs/agent-images-spike.md).

## 本体の実接続とwire診断

`integration.js`は公開APIを使う本体確認script、`wire_test.go`はHTTP実送信直前の画像hashとnative keysを照合する任意の診断です。結果は[実接続記録](../../docs/agent-images-integration.md)を参照。通常の`go test`では実providerを呼びません。wire診断を使うには、実画像`input-followup.png`のある`NOE_IMAGE_WIRE_DIR`と本体binaryの`NOE_IMAGE_WIRE_EXECUTABLE`を指定し、`NOE_IMAGE_WIRE_LIVE=1 go test ./spikes/agent-images -run TestImageWireLiveDiagnostic -count=1 -v`を実行します。診断結果は同directoryへ出力します。`NOE_IMAGE_WIRE_AGENT_ONLY=1 NOE_IMAGE_WIRE_AGENT_MODEL=gpt-6.1-sol`は直接比較を省いてSol agentだけを確認します。認証はNOEの既存設定を利用し、model要求数に応じた利用料が発生する場合があります。
