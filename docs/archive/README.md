# 初期開発と検討の履歴

[ドキュメント一覧](../README.md) · [現在の開発ガイド](../development/README.md) · [次の機能候補](../future-candidates.md)

ここはMVPから初期agent loopまでの計画・レビュー・検証と、過去の検討メモの保存先です。旧`noe.*`、`createAgent`・`agent.run()`、当時の依存バージョン・中断動作などを含みます。現在のAPIや最新ソースの検証結果を示す文書ではありません。

## MVPと初期インクリメント

| 計画・記録 | 当時のテーマ |
| --- | --- |
| [MVP](mvp.md) | 最初の製品方針・対象範囲 |
| [1](increment-1.md) | CLI、JavaScript実行、ローカルモジュール、ファイルとHTTP |
| [2](increment-2.md) | REPL、状態保持、エラー回復、非同期入力 |
| [3](increment-3.md) | 行編集ライブラリの選定と導入 |
| [4](increment-4.md) | 履歴、前回結果、補完、複数行入力 |
| [5](increment-5.md) | 標準入力、ディレクトリ一覧、ファイル情報 |
| [6](increment-6.md) | ディレクトリ作成、名前変更、削除 |
| [7](increment-7.md) | 実行時間の上限 |
| [8](increment-8.md) | HTTPメソッドと文字列body |
| [9](increment-9.md) | バイナリ入出力 |
| [10](increment-10.md) | イベントループと旧HTTPサーバーAPI |
| [11](increment-11.md) | 旧sleep API |
| [12](increment-12.md) | go-quickjs stdlibへ統一し、旧`noe.*`を削除 |
| [13](increment-13.md) | 最初のOpenRouter agent loop・生成JS・親子agent |

現在の公開agent APIはその後の[起動・待受の変更](../development/agent-lifecycle-implementation.md)で破壊的に変わっています。旧APIの利用例をそのまま使わず、[agent起動ガイド](../agents.md)・[APIリファレンス](../api.md)を参照してください。

## 初期レビュー・検証結果

- [REPL行編集の比較スパイク](repl-lineedit-spike.md)
- [インクリメント3・4の計画レビュー](increment-3-4-review.md)
- インクリメント13：[計画レビュー](increment-13-review.md)・[実装と検証](increment-13-implementation.md)・[ローカル検証JSON](increment-13-validation.json)・[OpenRouter検証JSON](increment-13-openrouter-results.json)

現在のREPLの入力UIは[qjs由来ソースの統合・保守手順](../development/qjs-repl-integration.md)を参照してください。

## 検討メモ

[整理前の候補・検討履歴](future-candidates-history.md)を保存しています。現在の完了区分と残件は[次の機能候補](../future-candidates.md)を参照してください。

文書のリンクは現在の配置へ更新しています。本文中の旧API、コマンド例、実行環境、JSONの証跡や一時ファイルのパスは、当時の記録として残しています。
