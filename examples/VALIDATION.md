# examplesの動作確認

## AI実行契約変更の候補確認 — 2026-10-23

今回の候補と、そのコミット`af8c2a6`からビルドしたCLIでJS/MJS全51ファイルを実行経路に含めました。library・hook・browser用fileは利用側から呼んでいます。詳細は[AI実行契約の実装記録](../docs/development/ai-execution-implementation.md)と `/var/tmp/noe-oauth-workflow-20261023-165717/examples-coverage.json` を参照してください。

- ローカル入出力・HTTP・process・signal等25件が成功。task-loopのsolution/testは意図的な失敗baselineを維持し、一時コピー内で確認。
- agent 7入口、hook、decision、画像生成・画像読取をローカルprovider fixtureで確認。agent-controlはsteer適用とtask完了を確認してからSIGINTでkeepAliveを終了。最初の単純完了待ちはkeepAliveのためtimeoutになり、成功扱いにしていません。
- agent-serverはHTTP受付→run_jsのdata→回答→正常shutdown、agent-consoleはbackend E2EとChromiumによるUI検査3本が成功。
- Exa libraryはfetch差替えで成功・入力検査・HTTP失敗を確認。実Exa、実画像生成・visionの品質は今回検証していません。
- 追加実行で`agent-openai.js`と`task-loop-jev/workflow.js`も実providerで成功。OAuth自動更新後、専用例は`gpt-6-luna`で437を返しました。全workflowは`gpt-6.1-sol / high`とOpenRouterの`typesafe/jev-1.14`を使い、計画修正1回、コード修正0回、5テスト合格、wait 2回、poll 0回で完了。元のworkflow・要件・テストを変えず、一時コピーだけで計画・実装を修正しています。

examplesの通常経路を先に検証し、その後総合検証で見つかったasync診断frame取得だけを修正しています。更新後は全体E2Eと新契約の実model probeで影響箇所を再確認しています。OAuth例・全workflowの追加実行は修正を含む`af8c2a6`で行い、記録は `/var/tmp/noe-oauth-workflow-20261023-165717/verification.json` にあります。以前の49/51時点の記録は初回検証の範囲です。

## ローカルWebコンソール — 2026-10-23

[agent-console](agent-console/README.md)を追加し、実CLI＋ローカルprovider fixtureとChromium 151で確認しました。idle起動、Queueの会話継続と回答全文保存、Steer・Interrupt・全停止、切断中の監視、再起動後の履歴閲覧、Host・Origin検証、1,000件の表示保持量を超える履歴のスクロールを確認しています。実ブラウザから実NOEを経由して12,014-byteのUnicode回答を表示する経路も成功しました。

独立コードレビューの軽微な1件を修正・再確認し、全体E2Eを含むGoテスト、vet、JS lintが成功しました。サンプルの実provider呼出しや長期連続稼働は検証していません。[実装・検証記録](../docs/development/agent-web-console-implementation.md)を参照してください。

## Go常駐組み込み例の追加確認

[embedding-server](embedding-server/README.md)をLinux amd64 / Go 1.27.2で確認しました。

- `go test ./examples/embedding-server -count=1`、同packageのrace検査と `go vet` が成功。並行リクエストの入出力分離、VMの分離、同期・非同期JSの期限切れ、キャンセル、停止時のVM cleanup完了待ちを確認。
- CGO無効でビルドした実行ファイルに対し、`/health`、`/sum`、SIGTERM後の正常終了（コード0）を確認。
- READMEのテンプレートを別moduleへコピーし、checkoutへのlocal replaceで `go mod tidy` とCGO無効のbuildが成功。
- READMEのsystemd unitは配置先を検証用一時パスへ置換して `systemd-analyze verify` が成功。ユーザーサービスのインストール・起動やログアウト後の継続は実行していません。

長期連続稼働や本番向けの資源制御を確認したものではありません。

## v2互換性確認 — 2026-10-21

`fa43be492122e930e9299089f69f7513c3908031`からビルドしたv2候補で、JS/MJS全41ファイルを実行しました。library・hook・appは利用側から呼び出しています。入力移動やworkflowの書換えは一時コピー内で行い、ソースの互換性不具合は確認されませんでした。

- 実行確認40件、agent-serverのfixtureテスト9件、実Firefox確認1件が成功。
- OpenRouter・OpenCode Go・OpenAI ChatGPTで実agentを実行。モデル選択、Jev判断、親子の並行実行、hook、steer/queue/停止を確認。
- 画像生成・画像読み込み・Exa検索も実サービスで確認。64×64の4色画像を一度「赤一色」と誤認したため、生成したヨット画像で追加確認し、船と海を正しく回答。小画像の誤認原因は特定していません。
- task-loop-jevは意図的に失敗するbaselineから開始し、計画レビュー、同じworkerへの修正キュー、実装、5項目のテスト、コードレビューを完了。
- Goのstdlib/agent組み込みも、NOEに別moduleの小さなHTTP常駐アプリを作らせて実providerと終了処理まで確認しました。

全ファイルの実行経路とログは`/var/tmp/noe-examples-v2-glpym7ir/REPORT.md`と`results.json`、常駐アプリ監視は`/var/tmp/noe-resident-otwho36_/MONITOR_REPORT.md`へ保存しています。[v2リリース準備記録](../docs/releases/release-checkpoints.md#v200-preparation--2026-10-21)に総合検証をまとめています。

## 以前の確認記録

2026-10-19追記：第2グループAPIを含むソースからCLIを再ビルドし、[low-level](low-level/README.md)のindex-roundtrip（TCP/Unix）とwait-signal（SIGTERM）をLinux amd64で実行して成功しました。[実装・総合検証記録](../docs/development/low-level-group2-implementation.md)を参照してください。以下は既存例を整理した時点の記録です。

2026-10-18、Linux amd64、NOE v1.6.1のソースとビルド済みCLIで確認しました。

**現在のNOE APIとの非互換で利用不可と判断した例はありません。** スクリプトは削除せず、用途ごとのフォルダへ移動しています。実行パスは[一覧](README.md)と各フォルダのREADMEを参照してください。以前の`examples/try-agent.js`は`examples/agent/try-agent.js`です。

## 確認した範囲

| フォルダ | 確認内容 | 結果 |
| --- | --- | --- |
| basics | Hello・sleepは既存E2E。REPL用JSは当時実行して出力を確認 | 成功 |
| json | 加工・標準入力・ディレクトリ集計・処理済みファイルの移動。正常系と既存の異常系E2E | 成功 |
| sales-report | 同梱データからJSON・Markdownを一時ディレクトリへ出力し、集計結果を確認 | 成功 |
| http | ローカルHTTP serverでJSON取得・バイナリ取得・更新要求・serveの既存E2E | 成功 |
| process | 常駐子との3往復、JSONL応答、二つの子を通す日本語入りのパイプライン | 成功 |
| import-map | 同梱mapで`app/math`を解決し、`6`を出力 | 成功 |
| agent | 全8エントリをローカルprovider fixtureで実行。並列例は二つの子によるファイル読取り、hook例は移動先moduleの実行、制御例はidle・queue・instance interruptを確認 | 成功 |
| decision | 分類例をローカルDecisions fixtureで実行。Jev選択→agent起動は既存CLIテストでも確認 | 成功 |
| exa | importと、fetchを差し替えた検索・引数検証・HTTPエラーの保持 | 成功 |
| agent-server | 今回はJS・HTML・テストの配置と内容を維持。直前の評価でHTTP統合9件と実Firefoxでの操作を確認済み | 成功（直前の評価） |

移動先を参照する既存テストは、次のコマンドで再実行しています。

```sh
go test ./internal/cli
go test -tags=e2e ./tests -run 'Test(ReadmeExamples|ServeExampleThroughCLI|SummarizeStdinExampleAndPipeline|CollectJSONExample|ProcessJSONExample|HTTPJSONExample|SleepExampleAndUnawaitedCompletionThroughCLI|BinaryHTTPDownloadExample)' -count=1
```

上記に含まれない例は整理時の作業用fixtureで確認しました。README・現行ガイドの相対リンクも確認しています。過去のrelease・設計・検証記録は当時のパスを維持しています。

## 利用条件と未確認の範囲

- agent・decision・Exaの実サービスへは今回接続していません。実際のモデル応答の質、アカウントの利用枠、現在の認証状態はこの確認結果に含みません。OpenAI例のfixtureでは認証もテスト用APIキーへ上書きしており、ChatGPTログインは検証していません。
- `http`のREADMEにある`https://example.com/api/...`や画像URLは説明用です。実在するAPI・ファイルURLへ差し替えます。
- `exa/exa.js`、各`lib/`、`agent/hooks/context.js`は読み込んで使うモジュールです。単体実行で検索・レポート・hook処理が始まる例ではありません。Exaの既定キーパスは`~/.config/noe/credentials/exa-api-key`で、XDG設定は自動探索しません。別の場所なら`apiKeyFile`を指定します。
- `agent-control.js`はモデルへ45秒待機を指示する例で、必ず45秒待つことをコードで保証してはいません。keepAliveにより、task終了後もinstanceの停止までプロセスが続きます。
- `json/process-json.js`は入力ファイルを移動します。READMEどおりサンプルを一時ディレクトリへコピーして使います。

旧`noe.*` APIに依存する例はありませんでした。説明のprovider表記を修正し、`agent-control.js`のsystem指示も現在のqueueによる会話継続に合わせています。
