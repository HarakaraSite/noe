# HTTPクライアントとサーバー

[実行例一覧へ戻る](../README.md)

コマンドはリポジトリのルートで、ビルド済みの`./noe`を使って実行します。

Use the standard global `fetch()` for HTTP requests and import the listener helper with `import { serve } from "noe:http"`. The API contract is available through `./noe docs noe:http`. Fetch request bodies and `serve` request bodies can be processed incrementally as `ReadableStream` values of `Uint8Array` chunks. `serve` starts its handler before body EOF and reads the request as JavaScript consumes it.

## ローカルでJSONを往復する

```sh
./noe run examples/http/roundtrip.js
```

Without an external service or credentials, POST JSON to a port-0 loopback server, print `{"value":42}`, and exit after `finally` awaits server shutdown.

## Receive server-sent events

```sh
./noe run examples/http/event-source.js
```

This local example needs no credentials or external service. It receives a
message and a named `done` event, prints `1 hello` and `done completed`, then
closes both the EventSource and the server. EventSource is a global API;
`serve` comes from `noe:http`. Use `./noe docs globals EventSource` for its
reconnection, lifecycle, and authentication contract.

## HTTPダウンロード

URLは利用するAPI・ファイルに置き換えてください。保存先の親ディレクトリは事前に用意します。

```sh
DEMO_DIR=$(mktemp -d)
./noe run --timeout 5s examples/http/fetch-json.js https://example.com/api/data.json "$DEMO_DIR/data.json"
./noe run --timeout 5s examples/http/download-file.js https://example.com/image.png "$DEMO_DIR/image.png"
```

JSON is parsed and saved with formatting, while binary data is saved without converting it to text. The response body from the global `fetch()` is read once. Both examples treat HTTP error statuses as failures.

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

引数なしではport 0のloopback serverへ有限のbinary streamをPOSTし、echoされたbytesを確認して終了します。外部サービスや固定portは使いません。`URL [output]`を渡すと、同じstreamを指定URLへ送り、`output`があればresponse bodyをraw bytesで保存します。保存先の親ディレクトリは事前に用意してください。要求streamは一度だけ送信され、各chunkは`Uint8Array`です。未完了の送信中に最終Responseが返る場合、残りのuploadは停止します。これはfull-duplex通信の例ではありません。

## Streaming received request bodies

```sh
./noe run examples/http/receive-stream.js
```

port 0のloopback serverへ有限のbinary streamを送り、handlerが`request.body.getReader()`からbytesを順次読み取って、次のJSONを表示します。

```json
{"bytes":[0,255,17,34,128,0,255],"byteCount":7,"sum":689}
```

These examples show streamed request-body processing and finite transfers. A successful run alone does not prove that the handler started before body EOF and observed a prefix; verify that ordering with an HTTP client independent of NOE's `fetch`. For receive caps, empty bodies, read errors, cancellation, `Expect: 100-continue`, and request-body lifetime during a response, see `./noe docs noe:http serve`.

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

## HTTPS and WSS server

This source-only addition is newer than v2.16.0. Build a temporary executable
from this checkout for the example; a published v2.16.0 binary does not support
the new listener options.

The example reads PEM text from the two explicit paths with `noe:fs`. Those reads follow the active filesystem Root, Cwd, and ReadOnly settings; supplying TLS credentials does not add filesystem access. The certificate and private key must match. The listener stays active until Ctrl+C or runtime shutdown.

Optionally create a short-lived local test certificate with an external OpenSSL command:

```sh
DEMO_DIR=$(mktemp -d)
CGO_ENABLED=0 go build -o "$DEMO_DIR/noe" ./cmd/noe
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$DEMO_DIR/server-key.pem" \
  -out "$DEMO_DIR/server-cert.pem" \
  -days 1 -subj '/CN=localhost' \
  -addext 'subjectAltName=IP:127.0.0.1,DNS:localhost'
echo "$DEMO_DIR"
"$DEMO_DIR/noe" run examples/http/https-server.js \
  "$DEMO_DIR/server-cert.pem" "$DEMO_DIR/server-key.pem" 8443
```

In another terminal, use the printed directory path as the CA file for HTTPS verification:

```sh
curl --cacert /path/to/printed-directory/server-cert.pem https://127.0.0.1:8443/health
curl --cacert /path/to/printed-directory/server-cert.pem \
  -H 'Content-Type: application/json' \
  -d '{"message":"こんにちは"}' https://127.0.0.1:8443/echo
```

The health endpoint returns `ok`, and the echo endpoint returns the submitted JSON. A WebSocket client that trusts the same certificate can connect to `wss://127.0.0.1:8443/ws`; the server echoes text and binary messages. The example defaults to port 8443, accepts port 0 for an OS-assigned port, and prints its `https://` URL on startup.
