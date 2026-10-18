# 売上レポート

[実行例一覧へ戻る](../README.md)

コマンドはリポジトリのルートで、ビルド済みの`./noe`を使って実行します。

## 売上レポート

```sh
DEMO_DIR=$(mktemp -d)
REPORT_TITLE='今日の売上' ./noe run examples/sales-report/sales-report.js "$DEMO_DIR/sales-report"
cat "$DEMO_DIR/sales-report.md"
```

既定のサンプル2ファイルを並行して読み、キャンセル済み注文を除いて集計します。JSONとMarkdownを保存し、集計結果も表示します。`REPORT_TITLE`はレポートのタイトルです。

引数なしなら保存先の接頭辞は`/tmp/noe-sales-report`です。入力を指定する場合は、接頭辞の後にファイルを並べます。

```sh
./noe run examples/sales-report/sales-report.js "$DEMO_DIR/custom-report" examples/sales-report/data/sales-day-1.json
```
