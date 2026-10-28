# NOE 実行例

[READMEへ戻る](../README.md) · [APIリファレンス](../docs/api.md)

用途ごとのフォルダに、実行用JS・README・補助モジュール・サンプルデータをまとめています。各READMEのコマンドはリポジトリのルートで実行してください。通常のファイル操作の相対パスは作業ディレクトリ、相対importはimport元のファイルが基準です。

| フォルダ | 内容 | 外部サービス |
| --- | --- | --- |
| [basics](basics/README.md) | Hello、待機 | 不要 |
| [embedding](embedding/README.md) | CLI・agentに依存しないGoホストへの公開stdlib API導入 | 不要 |
| [embedding-server](embedding-server/README.md) | Goホストを常駐させ、リクエストごとにJS実行・停止処理・systemdユーザーサービス化 | 不要 |
| [agent-embedding](agent-embedding/README.md) | 同じGoプロセス内にHost/Clientとagent実行を組み込む | 決定的なproviderを使う通常例は不要 |
| [log-analysis](log-analysis/README.md) | JSONLイベントログのtoken・cache・tool出力超過をファイル別に比較 | 不要 |
| [json](json/README.md) | JSONの加工、標準入力・ディレクトリ集計、処理済みファイルの移動 | 不要 |
| [sales-report](sales-report/README.md) | JSONとMarkdownの売上レポート | 不要 |
| [http](http/README.md) | noe:httpでのローカル往復、stream upload、JSON・バイナリ取得、HTTPサーバー | ローカル例は不要、外部APIの例は接続先URLが必要 |
| [process](process/README.md) | 常駐子プロセス、JSONL通信、パイプライン | 不要 |
| [mcp](mcp/README.md) | 公式SDKでExa MCPへ接続、ローカルHTTP/SSE往復 | ローカル例は不要、Exa例は外部接続 |
| [daemon](daemon/README.md) | JSでTCP待受、JSONL要求、SIGTERM終了と外部からの起動停止 | 不要 |
| [low-level](low-level/README.md) | 位置指定ファイルI/O、TCP/Unix通信、OSシグナル | 不要 |
| [import-map](import-map/README.md) | ローカルESMへの名前付け | 不要 |
| [url-import](url-import/README.md) | 小さな時間変換ライブラリ、公開ESMの直接・dynamic・URL alias import | jsDelivrまたはesm.shへのHTTPS接続 |
| [agent](agent/README.md) | 任意タスク、要約、並列委譲、provider選択、hook、steer・queue・interrupt | 各providerの認証 |
| [image](image/README.md) | agentによる画像読取り、JavaScriptからの画像生成 | 読取りはagent用provider認証、生成は対応provider認証と利用料 |
| [decision](decision/README.md) | Jev等による単発の構造化判断 | OpenRouter認証 |
| [exa](exa/README.md) | 再利用できるExa検索ライブラリ | Exa APIキー |
| [noebot](noebot/README.md) | Thin consumer of the separately installed private noebot application (Web, Telegram, jobs, notes, schedules) | NOE provider credentials; Telegram optional |
| [agent-server](agent-server/README.md) | HTTP・ブラウザから常駐agentへ依頼・観察・制御 | 各providerの認証 |

任意の指示を試す入口は`agent/try-agent.js`です。

For settled requirements through slice implementation, reviews, local commits and one final push, use the [portable development workflow](../.noe/lib/workflow/README.md). Version 1 does not use a Decision API or perform releases.

計画・レビュー・修正・実装・テストをJavaScriptで組み立てる例は、[Jevをゲートに使うタスクループ](agent/task-loop-jev/README.md)を参照してください。`queue_agent`と`wait_task`で作業担当を制御します。

```sh
./noe run --observe examples/agent/try-agent.js 'README.mdを3点に要約して'
# ChatGPT経由のSolに、ファイルから指示を渡す場合
./noe run --observe --events=agent-server-build.jsonl examples/agent/try-agent.js \
  --provider openai-chatgpt --model gpt-6.1-sol --effort high \
  --task-file examples/agent-server/build-task.md
```

HTTPサーバーを作らせた[指示文](agent-server/build-task.md)と、生成された[サーバー](agent-server/README.md)も同じフォルダに置いています。

画像例は`./noe run examples/image/read.js ./tmp/screenshot.png`でagentに画像を見せ、`./noe run examples/image/generate.js`で画像を生成します。利用できるprovider/modelや認証条件は[画像例のREADME](image/README.md)を参照してください。

[動作確認結果と利用条件](VALIDATION.md)を参照してください。HTTPの`example.com`は説明用URLなので、実際のAPI・ファイルURLへ置き換えます。Exaは単体実行では検索せず、`search`をimportして呼び出すライブラリです。
