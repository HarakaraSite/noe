# HTTPクライアントとサーバー

[実行例一覧へ戻る](../README.md)

コマンドはリポジトリのルートで、ビルド済みの`./noe`を使って実行します。

HTTPの入口は`import { fetch, serve } from "noe:http"`です。既存globalの同名関数と同じ実装を使います。API契約は`./noe docs noe:http`で確認できます。`fetch`は`Uint8Array`を返す`ReadableStream`の要求bodyを逐次送信できます。`serve`の受信bodyは読取り上限までbufferingしてからhandlerへ渡します。

## ローカルでJSONを往復する

```sh
./noe run examples/http/roundtrip.js
```

外部サービスや認証なしで、port 0のloopback serverへJSONをPOSTし、`{"value":42}`を表示して終了します。応答の消費後にfinallyでserverをcloseします。

## HTTPダウンロード

URLは利用するAPI・ファイルに置き換えてください。保存先の親ディレクトリは事前に用意します。

```sh
DEMO_DIR=$(mktemp -d)
./noe run --timeout 5s examples/http/fetch-json.js https://example.com/api/data.json "$DEMO_DIR/data.json"
./noe run --timeout 5s examples/http/download-file.js https://example.com/image.png "$DEMO_DIR/image.png"
```

JSONは解析して整形保存し、バイナリは文字列に変換せず保存します。`noe:http`の`fetch()`のレスポンスbodyは一度だけ読み取ります。どちらの例もHTTPエラーを実行失敗にします。

## JSON APIへのリクエスト

URLとJSONの項目は利用するAPIに合わせて置き換えてください。

```sh
ITEM_URL='https://example.com/api/items/1'
./noe run --timeout 5s examples/http/http-json.js PUT "$ITEM_URL" '{"name":"日本語","count":1}'
./noe run --timeout 5s examples/http/http-json.js PATCH "$ITEM_URL" '{"count":2}'
./noe run --timeout 5s examples/http/http-json.js DELETE "$ITEM_URL"
```

JSON-bodyがある場合は解析して検証し、明示的に文字列化してapplication/jsonで送ります。成功時は`{status, ok, body}`のJSONを表示し、空レスポンスのbodyはnullです。この例はHTTP失敗を診断してコード1で終了します。`fetch`自体は4xx・5xxもレスポンスとして返します。レスポンスbodyは`text()`で一度だけ読み取ります。

## Streaming request body

```sh
./noe run examples/http/upload-stream.js
```

引数なしではport 0のloopback serverへ有限のbinary streamをPOSTし、echoされたbytesを確認して終了します。外部サービスや固定portは使いません。`URL [output]`を渡すと、同じstreamを指定URLへ送り、`output`があればresponse bodyをraw bytesで保存します。保存先の親ディレクトリは事前に用意してください。要求streamは一度だけ送信され、各chunkは`Uint8Array`です。未完了の送信中に最終Responseが返る場合、残りのuploadは停止します。これはfull-duplex通信の例ではありません。`serve`は受信bodyをEOFまで読んでからhandlerを呼ぶため、loopbackの例は送信先がEOFより前にprefixを受け取った証拠にはなりません。

## HTTPサーバー

一つ目の端末で起動します。

```sh
./noe run examples/http/http-server.js 8080
```

別の端末から試します。

```sh
curl http://127.0.0.1:8080/health
curl -H 'Content-Type: application/json' \
  -d '{"message":"こんにちは"}' http://127.0.0.1:8080/echo
```

healthは`ok`、echoは送ったJSONを`Response.json()`で返します。経路は`new URL(request.url).pathname`で取得します。不正JSONは400、その他のパスは404です。ポート省略時は8080、0ならOSが空きポートを選び、起動時に実際のURLを表示します。起動した端末のCtrl+Cで終了します。
