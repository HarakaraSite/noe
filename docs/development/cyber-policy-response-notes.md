# サイバーセキュリティ制限の応答処理メモ

記録日: 2026-10-23。v2.1.1の調査結果と、その後の対応記録。利用者の指示により、以下の分類・詳細伝達・再試行抑止を実装した。公開・配置は未実施。

## 確認した事象

`/home/agent/projects/kaguyajs`で動く別のCodexで、画面に「This content can’t be shown」とDaybreakの案内が繰り返し表示された。

その親セッションの保存ログでは、2026-10-23 JSTに5件の`task_complete`に次の情報があった。直近は12:56:16。

- `last_agent_message: null`
- `error.codex_error_info: "cyber_policy"`
- エラー本文は、サイバーセキュリティ上のリスク判定とDaybreakの案内。

モデルの通常の回答文ではなく、Codexが`cyber_policy`に分類したエラーでターンが終了している。このログから元のHTTPステータスや生のSSEイベント形式までは確定できない。

元ログ（実施環境のローカル記録）:
`/home/agent/.codex/sessions/2026/10/21/rollout-2026-10-21T20-26-19-01a15df1-e77a-78b2-880a-c6e66d71148a_01a15e36-4e77-7472-9697-354049cec39f.jsonl`

OpenAIの[Cybersecurity checks](https://developers.openai.com/api/docs/guides/safety-checks/cybersecurity)は、APIの`cyber_policy`エラーとストリーム途中にエラーが返る場合を説明している。[Responsesのイベント仕様](https://developers.openai.com/api/reference/resources/responses/streaming-events)では、refusalのイベントも別に定義されている。画面の案内だけから受信形式を決め付けない。

## NOE v2.1.1での再現結果

配置済みバイナリへローカルHTTPサーバーから疑似応答を返した。実プロバイダへの要求は行っていない。

| 疑似応答 | タスクの結果 | 試行回数・保持情報 |
| --- | --- | --- |
| completed応答の明示的な`refusal` | failed、`model refused the request` | 1回。拒否本文とrequest IDを`lastResult.errorDetails`に保持 |
| `response.failed`と`cyber_policy` | failed、`invalid provider response` | 1回。具体的な理由が結果へ残らない |
| HTTP 403と`cyber_policy` | failed、`authentication failed (HTTP 403)` | 1回。認証失敗と区別できない |
| HTTP 200のSSE `error`と`cyber_policy` | failed、`invalid or incomplete response stream (HTTP 200)` | 合計3回。不完全な通信として再試行してしまう |
| 同じ案内文を通常の回答テキストで返す | completed、その文章が最終回答 | 1回。本文だけから拒否と推測する処理はない |

`run_agent()`は通常のタスク失敗もsnapshotとして返す。呼び出し元が`lastResult.state`を確認して例外等にしなければ、外側のJSの正常終了とタスク失敗は両立する。

probeと結果（実施環境の一時記録）:
`/var/tmp/noe-refusal-probe-xo7ngo/probe.py`、同ディレクトリの`results.json`とケース別JSONL。

## 採用した対応

1. HTTPエラー、`response.failed`、SSE `error`の各入口で、明示された`cyber_policy`を認証・通信・一般的な応答不正と区別する。
2. 明示されたポリシー拒否を通信障害として自動再試行しない。途中まで受信したtool callも実行しない。
3. タスクの失敗状態、分類、上流のエラーコード、利用者向け理由、取得できたrequest IDを結果とイベントへ渡す。既存の資格情報等の扱いと整合させ、未知の自由文だけから拒否を推測しない。
4. completedのrefusalを既に処理できる経路と、今回のエラー経路を混同しない。分類追加時は、実データまたは公式仕様で確認した形式と既存のローカルprobeを使って検証する。

入口は`internal/agent/provider/openai.go`の`nativeStreamFailure`、`errors.go`の`classifyHTTPError`、`retry.go`の再試行判定。Responsesの組立て・観察処理、OpenRouter SDK側の同種エラー経路、brokerからタスク結果への詳細伝達も確認対象とする。

NOE側で行うのは、上流の拒否を正しく分類・表示して終了するための対応。Daybreakの権限や上流制限自体をNOEが変更するものではない。

## 実装と修正後の検証

- 受信済みのHTTPエラー本文とSSEイベントから、明示された`cyber_policy`を共通処理で検出する。SDKの型定義が新しいコードを解釈できず、一般的なdecode/streamエラーに変換した場合も同じ分類へ戻す。正常応答では追加のエラー解析をしない。
- providerの分類は`policy`、詳細の`code`と`providerCode`は`cyber_policy`。既存の再試行対象に含めず、タスクをfailedとして終了する。取消しをポリシー拒否へ置き換えない。
- 既存のエラー詳細欄へ、上流の理由・HTTPステータス・取得できた上流の`x-request-id`を保存する。資格情報等は既存のmetadataサニタイズを使う。`model.failed`・`agent.failed`と、`lastResult`・`wait_task`に伝わる。イベント直下のNOE request IDと、詳細内の上流request IDは区別する。
- 通常の案内文・未知のエラー文からは拒否を推定しない。completed応答のrefusal処理も従来どおりとする。

`internal/agent/provider/policy_test.go`はOpenAI・OpenRouter両SDKのResponses/Chat経路で、HTTP 403・429とSSEの各エラーを確認する。いずれも要求は1回で、理由・上流request IDを保持し、応答に混じった資格情報を除去する。

`tests/agent_policy_e2e_test.go`は実CLIを使い、両SDKそれぞれでHTTP拒否・response.failed・SSE errorを確認する。失敗前に受信したtool itemは実行されず、回答も成功扱いせず、タスク結果とmodel/agentイベントに詳細が届く。commandイベントは状態の通知を維持する。

検証にはローカルfixtureのみを使用。実プロバイダの制限発生を意図的に誘発する試験は行っていない。修正後の振る舞いは[providerガイド](../providers.md#サイバーセキュリティ制限)と[イベント仕様](../events.md)にも反映した。

最終コードに対して次を実行し、すべて成功した。

- `go test -tags=e2e ./... -count=1`（tests package 55.771秒。新規のpolicy検証とagent-consoleの既存E2Eを含む）
- `CGO_ENABLED=1 go test -race ./internal/agent/provider ./internal/agent -count=1`
- `go vet -tags=e2e ./...`
- `git diff --check`
