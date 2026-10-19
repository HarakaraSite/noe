# NOE 実行例

[READMEへ戻る](../README.md) · [APIリファレンス](../docs/api.md)

用途ごとのフォルダに、実行用JS・README・補助モジュール・サンプルデータをまとめています。各READMEのコマンドはリポジトリのルートで実行してください。通常のファイル操作の相対パスは作業ディレクトリ、相対importはimport元のファイルが基準です。

| フォルダ | 内容 | 外部サービス |
| --- | --- | --- |
| [basics](basics/README.md) | Hello、待機 | 不要 |
| [json](json/README.md) | JSONの加工、標準入力・ディレクトリ集計、処理済みファイルの移動 | 不要 |
| [sales-report](sales-report/README.md) | JSONとMarkdownの売上レポート | 不要 |
| [http](http/README.md) | JSON・バイナリ取得、JSON APIへのリクエスト、HTTPサーバー | クライアント例は接続先URLが必要 |
| [process](process/README.md) | 常駐子プロセス、JSONL通信、パイプライン | 不要 |
| [low-level](low-level/README.md) | 位置指定ファイルI/O、TCP/Unix通信、OSシグナル | 不要 |
| [import-map](import-map/README.md) | ローカルESMへの名前付け | 不要 |
| [agent](agent/README.md) | 任意タスク、要約、並列委譲、provider選択、hook、steer・queue・interrupt | 各providerの認証 |
| [image](image/README.md) | agentによる画像読取り、JavaScriptからの画像生成 | 読取りはagent用provider認証、生成は対応provider認証と利用料 |
| [decision](decision/README.md) | Jev等による単発の構造化判断 | OpenRouter認証 |
| [exa](exa/README.md) | 再利用できるExa検索ライブラリ | Exa APIキー |
| [agent-server](agent-server/README.md) | HTTP・ブラウザから常駐agentへ依頼・観察・制御 | 各providerの認証 |

任意の指示を試す入口は`agent/try-agent.js`です。

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
