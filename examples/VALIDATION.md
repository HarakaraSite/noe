# examplesの動作確認

## MCP・JSデーモン例 — 2026-10-24

[MCP](mcp/README.md)と[デーモン](daemon/README.md)をLinux amd64 / NOE v2.3.0で確認した。
公式SDKの固定依存からのbundle、ローカルHTTP/JSON/SSEでの初期化・一覧・並列tool呼出し、
Exaエントリのローカルfixture、実Exa公式サーバーの匿名での一覧取得が成功した。
実Exaの検索toolは呼んでいない。キーのheaderはダミーkeyによるfixtureで確認した。

デーモンはJSONLの分割・複数要求・並行接続・不正要求後の継続、CLI client、
接続中のSIGTERM、接続とlistenerの解放を確認した。新規JSのBiome lintも成功。
Node/npmはSDKの準備だけに使い、実行には使っていない。
詳細と未確認範囲は[実装・検証記録](../docs/development/mcp-daemon-examples.md)を参照。

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
