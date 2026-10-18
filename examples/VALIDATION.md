# examplesの動作確認

2026-10-18、Linux amd64、NOE v1.6.1のソースとビルド済みCLIで確認しました。

**現在のNOE APIとの非互換で利用不可と判断した例はありません。** スクリプトは削除せず、用途ごとのフォルダへ移動しています。実行パスは[一覧](README.md)と各フォルダのREADMEを参照してください。以前の`examples/try-agent.js`は`examples/agent/try-agent.js`です。

## 確認した範囲

| フォルダ | 確認内容 | 結果 |
| --- | --- | --- |
| basics | Hello・sleepは既存E2E、REPL用JSは実行して出力を確認 | 成功 |
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
