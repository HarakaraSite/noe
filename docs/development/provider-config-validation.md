# Provider設定とOpenCode Goの実装・検証

2026-10-17、Linux amd64 / Go 1.27.2。利用者の指定に従い、OpenCode GoのChat Completions・Responsesに対応しました。Messagesは今回の対象外です。[計画](provider-config-plan.md)・[設定ガイド](../providers.md)・[実接続結果JSON](provider-config-openrouter-sdk-results.json)。

## 実装

- 組込みの`defaults.json`をGoへembedし、外部の`config.json`で指定項目だけを上書き。providerとモデルごとに接続先・path・headers・parameters・reasoning/effortの対応を変更できます。objectは再帰merge、配列・scalarは置換、nullはキー削除です。
- `NOE_CONFIG_FILE`またはXDGのNOE設定directoryを使用。設定はホストで遅延読込みして固定し、キーは要求時に読込みます。既存OpenRouter用の環境変数とfixture注入を維持しました。
- `run_agent`・`spawn_agent`から設定済みprovider IDを解決します。親子で同じproviderを継承でき、明示的な切替えも可能です。通常のJS・run/eval/replは同じGoホストを使います。
- OpenCode Goには`User-Agent: noe/<version>`と実際の`runId`による`x-opencode-session`を付与。同じrunの往復で固定し、cacheKeyとは分離しました。
- Chatの`reasoning_content`等を全断片連結し、観察と次要求の履歴へ保持。Responsesはnativeなreasoning/function call/message itemsを保持し、tool結果と合わせて再送します。`store:false`とencrypted reasoningのincludeを既定にしています。
- Responsesのinput/output/cache usageも既存の共通観察フィールドへ正規化。SDKのResponses要求は`service_tier:auto`を明示して固定版のシリアライズ問題を回避します。
- 新しいSDKは追加していません。OpenRouter Go SDK v0.9.59を流用します。同じ接続定義のChatとDecisionsはSDK clientと認証cacheを共用します。`noe:decision`のproviderは引き続きOpenRouterのみです。

## ローカル検証

通常unit・全体E2E・vet、agent/provider/CLIのraceを実行しました。

主な確認点：

- 部分上書き・モデル別設定・未知のprovider・明示ファイルの読込み失敗・ホスト内の固定。
- 認証の遅延読込み、キーpathの環境変数優先、未設定の別providerがOpenRouterキーへfallbackしないこと。
- JSONのrawな会話・拡張フィールド・大きい整数を保ち、実行時パラメータを反映すること。
- 実際のCLIで親→子→親を実行し、ヘッダ値が実run IDと一致し、親の2要求は同一・子は別IDになること。
- Chatの思考断片が最後の空断片で消えず、次のHTTP要求とJSONL thinkingへ全文が渡ること。
- Responsesのencrypted reasoningとfunction callを次要求へ保持し、summary/回答を観察できること。
- Responsesの終端イベントでEOFを待たずに戻り、通信中の取消しでbodyを1回だけcloseすること。
- moduleのimportや通常JSだけでは設定ファイルを要求しないこと。

## 実プロバイダ確認

利用者の許可したhjhのキーを`NOE_OPENCODE_GO_API_KEY_FILE`で参照し、通常の`noe run`と組込みagent APIから実行しました。外部の部分上書き設定も指定しています。キーはコピー・表示していません。

| API / model | effort | 課題 | 結果 | model要求 / JS実行 | 所要時間 |
| --- | --- | --- | --- | --- | --- |
| Chat / `deepseek-v4.1-flash` | auto | JSで19×23 | `437` | 2 / 1 | 2,848ms |
| Responses / `gpt-6-luna` | auto | JSで17×29 | `493` | 2 / 1 | 2,456ms |
| Responses / `gpt-6-luna` | high | JSで31×37 | `1147` | 2 / 1 | 2,458ms |

計6要求。いずれもJS tool実行後のモデル継続と最終回答まで成功し、stderrは空です。Responses highは、native usageの正規化とstateless reasoning設定を加えた後に、明示effortの別ケースとして確認しました。成功済みのautoケースを再送していません。

今回の小さい実課題からは可視のthinking断片は返りませんでした。思考の断片保持・encrypted reasoningの再送・summary表示はローカルfixtureで確認した範囲です。初回のResponses auto実行ではusageのtotalだけが共通観察へ載り、その後input/output/cacheの正規化を修正。high実行でそれらの数値を確認しています。費用の数値はAPIから報告されていないため金額は算定していません。OpenCode Goの契約枠を消費します。

実接続のJS・外部設定・JSONL・stdout/stderr・summaryと検証log：

```text
/home/agent/.cache/noe-qjs-repl/noe-provider-config-iu76pmf1
```

再現用の[実行例](../../examples/agent/agent-opencode-go.js)を追加しました。

```sh
NOE_OPENCODE_GO_API_KEY_FILE=/path/to/opencode-go-api-key \
  ./noe run --observe examples/agent/agent-opencode-go.js deepseek-v4.1-flash
NOE_OPENCODE_GO_API_KEY_FILE=/path/to/opencode-go-api-key \
  ./noe run --observe examples/agent/agent-opencode-go.js gpt-6-luna
```

Messages・実プロバイダでの長時間処理やquota到達は未確認。外部設定で独自providerを追加する場合も、各APIの互換性は接続先に依存します。commit・push・releaseは行っていません。
