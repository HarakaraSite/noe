# NOE 実行例

[READMEへ戻る](../README.md) · [APIリファレンス](../docs/api.md) · [REPLガイド](../docs/repl.md)

以下のコマンドは、ビルド済みの`./noe`を使い、リポジトリのルートで実行します。ファイル操作の相対パスもそのディレクトリが基準です。

## 一覧

| スクリプト | 内容 |
| --- | --- |
| [hello.js](hello.js) | 最小の出力 |
| [transform-json.js](transform-json.js) | ローカルモジュールを使ってJSONを加工・保存 |
| [sales-report.js](sales-report.js) | 複数ファイルを読み、JSONとMarkdownの売上レポートを保存 |
| [summarize-stdin.js](summarize-stdin.js) | 標準入力の数値配列を集計 |
| [collect-json.js](collect-json.js) | ディレクトリ内のJSON数値配列を集計 |
| [process-json.js](process-json.js) | 集計レポートを保存し、処理済み入力を移動 |
| [fetch-json.js](fetch-json.js) | HTTPでJSONを取得・保存 |
| [download-file.js](download-file.js) | HTTPでバイナリを取得・保存 |
| [http-json.js](http-json.js) | JSON APIへのリクエスト |
| [http-server.js](http-server.js) | healthとJSON echoのHTTPサーバー |
| [sleep-demo.js](sleep-demo.js) | 非同期の待機を使った定期表示 |
| [repl-demo.js](repl-demo.js) | REPLへ1行ずつ貼り付けて試す計算例 |

`lib/`は例で使うモジュール、`data/`はサンプル入力です。

## JSONの加工と保存

```sh
DEMO_DIR=$(mktemp -d)
printf '{"value":21}\n' > "$DEMO_DIR/input.json"
./noe run examples/transform-json.js "$DEMO_DIR/input.json" "$DEMO_DIR/output.json"
cat "$DEMO_DIR/output.json"
```

入力のプロパティを保持し、`processed: true`を追加します。保存先の親ディレクトリは事前に用意します。

## 売上レポート

```sh
DEMO_DIR=$(mktemp -d)
REPORT_TITLE='今日の売上' ./noe run examples/sales-report.js "$DEMO_DIR/sales-report"
cat "$DEMO_DIR/sales-report.md"
```

既定のサンプル2ファイルを並行して読み、キャンセル済み注文を除いて集計します。JSONとMarkdownを保存し、集計結果も表示します。`REPORT_TITLE`はレポートのタイトルです。

引数なしなら保存先の接頭辞は`/tmp/noe-sales-report`です。入力を指定する場合は、接頭辞の後にファイルを並べます。

```sh
./noe run examples/sales-report.js "$DEMO_DIR/custom-report" examples/data/sales-day-1.json
```

## 標準入力とディレクトリの集計

```sh
printf '[100,200,300]\n' | ./noe run examples/summarize-stdin.js
# {"count":3,"total":600,"average":200}

./noe run examples/collect-json.js examples/data/numbers
# ファイルごとの結果と、count: 5、total: 800、average: 160を表示
```

入力は有限の数値だけを含むJSON配列です。空配列のaverageはnullです。ディレクトリ集計は直下の通常ファイルで、名前が`.json`で終わるものを名前順に処理します。再帰しません。

出力を保存し、次のスクリプトの標準入力として渡すこともできます。

```sh
DEMO_DIR=$(mktemp -d)
./noe run examples/collect-json.js examples/data/numbers > "$DEMO_DIR/summary.json"
./noe eval 'const report = JSON.parse(await noe.readStdin()); console.log(report.total)' < "$DEMO_DIR/summary.json"
# 800
```

## 処理済みファイルの移動

この例は入力ファイルを移動します。サンプルを作業ディレクトリへコピーして試します。

```sh
DEMO_DIR=$(mktemp -d)
mkdir "$DEMO_DIR/input"
cp examples/data/numbers/*.json "$DEMO_DIR/input/"
./noe run examples/process-json.js "$DEMO_DIR/input" "$DEMO_DIR/output"
cat "$DEMO_DIR/output/report.json"
ls "$DEMO_DIR/output/processed"
```

入力と出力は別のディレクトリを指定します。全入力の解析後、出力ディレクトリを作り、一時レポートを`report.json`へ名前変更し、入力を`processed/`へ移動します。成功すると作業マーカーを削除します。

既存の`report.json.tmp`・`.processing`は上書きします。名前変更の置換可否はOSに従い、Linuxでは同名の通常ファイルを置換します。ファイルシステムをまたぐ移動はコピーで補いません。処理はトランザクションではなく、失敗・中断までの変更や作業ファイルは残ります。

## HTTPダウンロード

URLは利用するAPI・ファイルに置き換えてください。保存先の親ディレクトリは事前に用意します。

```sh
DEMO_DIR=$(mktemp -d)
./noe run --timeout 5s examples/fetch-json.js https://example.com/api/data.json "$DEMO_DIR/data.json"
./noe run --timeout 5s examples/download-file.js https://example.com/image.png "$DEMO_DIR/image.png"
```

JSONは解析して整形保存し、バイナリは文字列に変換せず保存します。どちらの例もHTTPエラーを実行失敗にします。

## JSON APIへのリクエスト

URLとJSONの項目は利用するAPIに合わせて置き換えてください。

```sh
ITEM_URL='https://example.com/api/items/1'
./noe run --timeout 5s examples/http-json.js PUT "$ITEM_URL" '{"name":"日本語","count":1}'
./noe run --timeout 5s examples/http-json.js PATCH "$ITEM_URL" '{"count":2}'
./noe run --timeout 5s examples/http-json.js DELETE "$ITEM_URL"
```

JSON-bodyがある場合は解析して検証し、明示的に文字列化してapplication/jsonで送ります。成功時は`{status, ok, body}`のJSONを表示し、空レスポンスのbodyはnullです。この例はHTTP失敗を診断してコード1で終了します。`fetch`自体は4xx・5xxもレスポンスとして返します。

## HTTPサーバー

一つ目の端末で起動します。

```sh
./noe run examples/http-server.js 8080
```

別の端末から試します。

```sh
curl http://127.0.0.1:8080/health
curl -H 'Content-Type: application/json' \
  -d '{"message":"こんにちは"}' http://127.0.0.1:8080/echo
```

healthは`ok`、echoは送ったJSONを返します。不正JSONは400、その他のパスは404です。ポート省略時は8080、0ならOSが空きポートを選び、起動時に実際のURLを表示します。起動した端末のCtrl+Cで終了します。

## 待機とREPL

```sh
./noe run examples/sleep-demo.js
```

`実行 1`から`実行 3`まで1秒間隔で表示します。待機中もHTTPやファイル処理を進められます。

[repl-demo.js](repl-demo.js)を`./noe repl`へ1行ずつ貼り付けると、変数・関数の保持を試せます。↑で入力を呼び出し、割引率を編集して再実行できます。詳細は[REPLガイド](../docs/repl.md)を参照してください。
