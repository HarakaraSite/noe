# NOE ドキュメント

[README](../README.md) · [実行例](../examples/README.md) · [開発ガイド](development/README.md) · [リリース](releases/README.md)

現在の使い方は以下の利用ガイドを参照してください。計画・検証記録と初期の開発履歴は別ディレクトリに分けています。

## 利用ガイド

| 目的 | 文書 |
| --- | --- |
| 認証を登録し、agentを起動する | [agent起動ガイド](agents.md) |
| バイナリと同じ版のAPI説明を調べる | [内蔵docs](reference.md) |
| 生成したJSを、環境にあるBiome／tscで確認する | [既存ツールによるJS検査](javascript-checks.md) |
| Zedで正式な型宣言を参照し、指示ファイルを実行する | [Zedの設定](zed.md) |
| run_jsのdata・診断・明示importへ移行する | [実行結果と診断](execution-results.md) |
| API引数・CLIオプション・実行制御・終了コードを調べる | [APIリファレンス](api.md) |
| 自作JS・検証済みESMを再利用する | [import map](import-map.md) · [HTTP/HTTPS URL import](url-import.md) |
| 接続先・認証・要求形式を設定する | [provider設定](providers.md) |
| ChatGPTサインイン・OpenAI API・ローカル互換接続を使う | [OpenAI](openai.md) |
| モデル候補・window・選択情報を管理する | [モデルカタログ](model-catalog.md) |
| 共通・workspaceの指示や任意のsystemを渡す | [インストラクション](instructions.md) |
| hook・コンテキスト予算・切り捨てを設定する | [hookとcontext](hooks.md) |
| agentの様子・状態・結果を読む | [観察](observe.md) |
| JSONLイベントを保存・処理する | [イベント](events.md) |
| 画像を読み、画像を生成する | [画像](agent-images.md) |
| Jevなどで構造化された判断を得る | [意思決定](decision.md) |
| 外部Goアプリへ汎用APIを組み込む | [stdlib](go-embedding.md) · [agent](go-agent-embedding.md)（英文） |
| 子プロセスの逐次I/O・ホスト標準I/Oを使う | [プロセスとストリーム](process-io.md) |
| ファイルの位置指定I/O・TCP/Unix通信・OSシグナルを使う | [ファイル・ソケット・シグナル](low-level-io.md) |

v2への更新は[v2移行ガイド](v2-migration.md)、さらに以前のスクリプトは[API移行表](api.md#旧apiからの移行)を参照してください。廃止した`noe.*`や旧agent APIの例は、履歴文書に残っています。

## 開発・検討・履歴

| 目的 | 入口 |
| --- | --- |
| NOEをビルド・検証し、実装の構成を知る | [開発ガイド](development/README.md)・[agentの構成](development/agent-architecture.md) |
| 機能ごとの計画・レビュー・検証を追う | [開発記録の一覧](development/README.md#機能別の計画と検証記録) |
| 未着手・一部完了の候補を検討する | [次の機能候補](future-candidates.md) |
| 完了した機能の実装・検証を確認する | [完了項目の記録](completed-features.md) |
| トークン効率改善の実装と測定を追う | [計画](development/token-efficiency-plan.md)・[実装・検証](development/token-efficiency-implementation.md) |
| txiki.jsとの機能差と改善の着眼点を調べる | [比較調査（2026-10-24）](development/txikijs-comparison-20261024.md) |
| MVP・旧API・初期インクリメントを調べる | [初期開発の履歴](archive/README.md) |
| 候補の過去の議論・旧API案を調べる | [検討メモの履歴](archive/future-candidates-history.md) |
| バージョンごとの変更とリリース手順を調べる | [リリース一覧](releases/README.md)・[チェックポイント](releases/release-checkpoints.md) |

新しい利用説明は`docs/`、開発向けの設計・検証は`docs/development/`へ置きます。`docs/archive/`は初期記録・旧案の保存先です。過去の記録は当時の状態を示し、現在のAPI契約や最新ソースの検証結果を代わりに示すものではありません。
