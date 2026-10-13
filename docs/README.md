# NOE 開発記録

[READMEへ戻る](../README.md) · [APIリファレンス](api.md) · [REPLガイド](repl.md) · [実行例](../examples/README.md)

現在の利用方法はREADMEと各ガイドを参照してください。以下は設計・受入条件・実装と検証の記録です。[MVP案](mvp.md)の対象外は初期段階の範囲で、その後の追加機能は各インクリメントに記録しています。

## インクリメント

| 計画・記録 | テーマ |
| --- | --- |
| [1](increment-1.md) | CLI、JavaScript実行、ローカルモジュール、ファイルとHTTP |
| [2](increment-2.md) | REPL、状態保持、エラー回復、非同期入力 |
| [3](increment-3.md) | 行編集ライブラリの選定と導入 |
| [4](increment-4.md) | 履歴、前回結果、Tab補完、複数行入力 |
| [5](increment-5.md) | 標準入力、ディレクトリ一覧、ファイル情報 |
| [6](increment-6.md) | ディレクトリ作成、名前変更、削除 |
| [7](increment-7.md) | 実行時間の上限 |
| [8](increment-8.md) | HTTPメソッドと文字列body |
| [9](increment-9.md) | バイナリ入出力 |
| [10](increment-10.md) | 共通イベントループと簡易HTTPサーバー |
| [11](increment-11.md) | `noe.sleep` |

行編集については[比較スパイク](repl-lineedit-spike.md)と[インクリメント3・4の計画レビュー](increment-3-4-review.md)もあります。

## 今後の候補

[次の機能候補](future-candidates.md)に、外部コマンド・ストリーミング・import map・HTTP URLからのimportの優先順案と、暗号・ハッシュの検討保留を記録しています。

## ビルドと検証

Go 1.27.2以降を使います。現在の検証環境はLinux amd64です。通常のビルドはCGO不要で、race detectorの実行にはCツールチェーンが必要です。以下はリポジトリのルートで実行します。

```sh
CGO_ENABLED=0 go build -o noe ./cmd/noe
go test ./...
go test -tags=e2e ./... -count=1 -v
CGO_ENABLED=1 go test -race ./internal/... -count=1
go vet -tags=e2e ./...
```

E2Eは実際のCLIを別プロセスから起動し、出力・終了コード・ファイルの内容・HTTP応答を確認します。履歴とファイルは一時ディレクトリ、HTTPはローカルサーバーを使い、外部サービスに接続しません。LinuxのPTYテストは編集、日本語、履歴、複数行、補完、Ctrl+C中断、終了後の端末設定の復元を確認します。

検証結果は各インクリメントに記録しています。race版CLIでのHTTPサーバー検証手順は[インクリメント10](increment-10.md#実装とレビュー記録)にあります。

## リリース

[リリースプロファイル](../.forgejo/release-profile.yml)に配布対象とビルド・品質確認の契約を記載し、[リリースワークフロー](../.forgejo/workflows/release.yml)で`v*`タグのpush時にビルド・公開します。通常テストとvetはForgejoの配布ゲート、raceとPTYを含むE2EはVM201でのリリース前確認です。手順と確認記録は[リリースチェックポイント](release-checkpoints.md)を参照してください。

## 参照と方針

[txiki.js](https://txikijs.org/)の小さなランタイムという方向を参考にし、エンジンには[go-quickjs](https://github.com/go-quickjs/go-quickjs)を使います。NOEの公開APIと共通イベントループはNOE側で実装し、エンジンの実行機能と分けています。

参照資料・ソースを置く`_refs/`はローカル専用の未追跡ディレクトリです。ビルド・テストの依存には含めません。

npm対応・Node/Deno互換は実装しません。Web標準は必要なサブセットにとどめ、TypeScript対応は後から追加する予定です。
