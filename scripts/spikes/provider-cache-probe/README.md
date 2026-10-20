# SDK provider cache probe

NOEが使用するOpenAI Go SDKとOpenRouter Go SDKへ単発要求を送る。既定4ケースは既存の薄いprovider adapterを通し、選択式の`openrouter-responses`はSDKの型と標準encoderを直接使う。agent/task runtime、子agent、tool実行、履歴の追加は使わない。

```sh
go build -o /tmp/noe-provider-cache-probe ./scripts/spikes/provider-cache-probe
/tmp/noe-provider-cache-probe \
  -out /absolute/path/to/new-artifacts \
  -credential-dir /home/agent/.config/noe/credentials
```

認証は指定したNOEディレクトリの次のファイルだけを使う。環境変数の別providerキーやhjhの保存先へfallbackしない。値は表示・保存しない。

- ChatGPT: `openai-chatgpt.json`。通常のNOE OAuth managerが更新も担当する。
- OpenCode Go: `opencode-go-api-key`。
- OpenRouter: `openrouter-api-key`。

既定で4ケースを順に実行し、各ケースで同じコードreview入力を3回送る。要求間は完了後2秒。provider・model・effort・本文・session IDをケース内で固定し、NOE側の`RunID`だけ変える。API呼出しは計12回で、adapter内の再試行が発生した場合はHTTP要求数が増える。SDK自身の自動retryは無効。ケースの要求が失敗した場合は、それ以上そのケースを繰り返さない。

| case | SDK | model | API | 識別子 |
| --- | --- | --- | --- | --- |
| `chatgpt` | OpenAI | `gpt-6.1-sol` | Responses | ヘッダー`session_id`、本文`prompt_cache_key` |
| `opencode-chat` | OpenRouter | `deepseek-v4.1-flash` | Chat Completions | ヘッダー`x-opencode-session` |
| `opencode-responses` | OpenRouter | `gpt-6-luna` | Responses | ヘッダー`x-opencode-session` |
| `openrouter` | OpenRouter | `deepseek/deepseek-v4.1-flash` | Chat Completions | 本文`session_id` |
| `openrouter-responses`（選択時） | OpenRouter | `deepseek/deepseek-v4.1-flash` | Responses | ヘッダー`x-session-id` |

`-cases opencode-chat,opencode-responses`のように範囲を選べる。全ケースのeffortは`high`、出力上限は2,048 tokens。ChatGPTのprofileには`sessionHeader: session_id`を設定する。調査当初はprobeだけの追加だったが、その後NOEの組込み設定にも同じ修正を適用し、既定設定からの実接続試験でも改善を確認した。詳細は[検証記録](../../../docs/development/agent-efficiency-observation.md)を参照。probeの実行自体は本体設定を書き換えない。

`-cases openrouter-responses`はhjhが使うAPI形式と、修正候補の`sessionHeader: x-session-id`を組み合わせた確認用。hjh本体や設定を変更せず、NOEのキーとSDKを使う。このケースはSDKの`ResponsesRequest`を直接構築し、`instructions`にsystem、文字列の`input`にuserを渡す。未使用の`tools`は省略する。SDKの既定値の生成時エラーを避けるため、NOEの既存adapterと同様に`service_tier: auto`を明示する。SDK自身のretryは無効。

`results.json`にSDK version、要求の生成経路、認証ファイルのpath（値は含まない）、HTTP送信直前の本文SHA-256・識別子・User-Agent、HTTP status、SSEの生usageとadapterまたはSDKが返したusageを保存する。観測時に本文を再marshalせず、SDKが生成した内容をそのままupstreamへ送る。記録のための中継サーバーは使わず、本文・認証ヘッダーはartifactへ保存しない。

cached tokensが未報告の場合は0と区別する。比較する率は`cached tokens / input tokens`で、初回と再送を分けて読む。長い固定入力の再送に対する小規模観測であり、実作業の会話全体で同じ率になるとは扱わない。

## 2026-10-21の実行結果

すべてNOEの認証情報を使用し、既定4ケースの計12要求がHTTP 200で完了した。OpenAI SDK v3.79.0、OpenRouter SDK v0.9.61。各ケースで実送信本文hash・セッション識別子を維持し、生のSSE usageとadapterの返却usageが全件一致した。

| case | 各回の入力tokens | 1回目のcached | 2・3回目のcached | 再送時の率 |
| --- | ---: | ---: | ---: | ---: |
| ChatGPT Responses | 6,287 | 0 | 各6,144 | 97.73% |
| OpenCode Go Chat | 6,570 | 0 | 各6,528 | 99.36% |
| OpenCode Go Responses | 6,286 | 0 | 各6,283 | 99.95% |
| OpenRouter Chat | 6,575 | 0 | 各6,144 | 93.44% |

生データ・集計は`/var/tmp/noe-three-provider-sdk-cache-20261021/{results,summary}.json`。各認証ファイルのpathは結果に記録し、キーやOAuth tokenの値は記録していない。

追加のOpenRouter Responsesケースは最初、NOE adapterが生成した本文でHTTP 400、`code:invalid_prompt`、`message:Invalid Responses API request`となった。エラー詳細取得の再確認を含め2要求で、cache usageは取得できなかった。結果は`/var/tmp/noe-three-provider-sdk-cache-responses-error-20261021/results.json`に保存した。

利用者の再実行依頼を受け、上記のSDK標準encoder経由で同じmodel・NOEキーを使って3要求を再送し、すべてHTTP 200で完了した。

| 回 | 入力tokens | cached tokens | ヒット率 |
| --- | ---: | ---: | ---: |
| 1 | 6,572 | 0 | 0.00% |
| 2 | 6,572 | 6,144 | 93.49% |
| 3 | 6,572 | 6,144 | 93.49% |

3要求の本文hash・`x-session-id`は一致し、生のSSE usageとSDKの返却usageも全件一致した。結果は`/var/tmp/noe-openrouter-responses-sdk-native-tier-20261021/{results,summary}.json`。再実行の準備中にSDKの要求生成で失敗した2試行はHTTP送信0件だった。

前回の本文には`tools: null`が含まれ、今回は未使用toolsの省略に加えsystem/inputの形式も変えている。この再実行だけでは400の原因を単一フィールドに確定しない。製品のencoderやhjhの設定を変更する修正は行っていない。

## `tools: null`の原因切り分け（退役済み診断モード）

追加の確定調査では、NOE adapterの元の本文に戻し、HTTP送信直前に`tools`だけを変えた4要求を比較した。認証は引き続きNOEのOpenRouterキー。model・入力・effort・セッションヘッダーを固定し、system/inputの形式もNOEの元の形式を維持した。この専用診断モードは調査後に退役し、現行helpから`-diagnose-tools-null`を削除した。以下は実行時の記録で、現行版では再実行できない。

```sh
/tmp/noe-provider-cache-probe \
  -out /absolute/path/to/new-diagnosis-artifacts \
  -credential-dir /home/agent/.config/noe/credentials \
  -diagnose-tools-null
```

| `tools` | HTTP | 結果 |
| --- | ---: | --- |
| `null`（現行encoderの元の値） | 400 | `invalid_prompt: Invalid Responses API request` |
| フィールド省略 | 200 | 完了 |
| `[]` | 200 | 完了 |
| `run_js`関数定義入り配列 | 200 | 完了 |

4要求の`tools`を除く本文のSHA-256はすべて`455f6789fec9ca6de8b892ff0bd2a110704d651a90c024937cdc86e91f9b198b`で一致し、セッションヘッダーも同一だった。したがって、観測したOpenRouter Responsesの400は`tools: null`が原因と確定する。省略だけでNOEの元のsystem/input形式のまま成功し、この要求ではsystem/input形式の変更は不要だった。

この切り分けでは、OpenRouter Responsesへ送った要求の400は`tools: null`が原因と実証できた。現行NOEの標準agent loopは常に`tools: [runJSTool]`を渡すため、この条件に該当しない。従って標準agent経路に対する製品修正は不要で、診断用4ケースも退役した。製品側のencoderは変更していない。

証拠は`/var/tmp/noe-openrouter-tools-null-isolation-20261021/{results,summary}.json`。当時の診断ではHTTP計4要求を送り、400の対照群は再試行されなかった。失敗する対照群を含んでいたため、その診断実行の終了コードは1だった。
