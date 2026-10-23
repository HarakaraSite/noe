# ソケットで要求を受けるJSデーモン

[実行例一覧](../README.md) · [ソケットとsignal](../../docs/low-level-io.md)

独立したNOEプロセスでloopback TCPを待ち受け、JSONLの要求に応答する例です。
`noe:sockets`と`noe:signals`だけで組み立て、Node・npm・モデルは使いません。
SIGTERMを使うためLinux/macOS向けです。Windowsのsignal APIはSIGINTだけに対応しています。

## 起動・要求・停止

repositoryルートで起動します。

```sh
./noe run examples/daemon/server.js 9070
```

`{"event":"ready","pid":12345,"address":{...}}`が出たら、別terminalから要求します。

```sh
./noe run examples/daemon/client.js 9070
./noe run examples/daemon/client.js 9070 '{"id":2,"op":"sum","values":[7,11,13]}'
./noe run examples/daemon/client.js 9070 '{"id":3,"op":"ping"}'
```

既定の要求は`{"id":1,"op":"echo","text":"hello 東京"}`です。
sumは`{"id":2,"ok":true,"result":31}`、pingはサーバーのPIDを返します。
不正な要求は`ok: false`の応答になり、その後も他の要求を処理します。
clientの終了コードは、正常応答なら0、不正要求や接続失敗なら1です。

終了するには、readyに表示されたPIDへSIGTERMを送ります。

```sh
kill -TERM 12345
```

listenerを閉じ、接続中のclientも閉じて処理の回収を待ちます。
`{"event":"stopped","connections":0}`を表示して終了コード0で終わります。
SIGTERM時点の未完了要求は切断される場合があります。
Ctrl+CはNOE CLIの取消しなので、JSによる停止手順を確認する場合はSIGTERMを使います。

## バックグラウンドで動かす

同じshellでPIDを保持すれば、外部から起動・利用・停止できます。

```sh
./noe run examples/daemon/server.js 9070 > /tmp/noe-daemon.log 2>&1 &
daemon_pid=$!
cat /tmp/noe-daemon.log
```

ログのreadyを確認してからclientを実行し、停止します。

```sh
./noe run examples/daemon/client.js 9070 '{"id":1,"op":"echo","text":"background"}'
kill -TERM "$daemon_pid"
wait "$daemon_pid"
cat /tmp/noe-daemon.log
```

端末を閉じた後も動かしたい場合はtmuxやOSのサービス管理を使います。
これはshellから独立したNOEプロセスを起動する例です。
`noe:process.spawn()`の子は作成したruntimeに所有され、親終了後の独立実行にはなりません。

## 通信形式

UTF-8のJSONを改行で区切ります。一つの接続で複数要求を送ることもできます。
受信chunkとJSONの行境界は一致しないため、decoderと未完了行のbufferを接続ごとに保持します。
clientは送信側を閉じてEOFを送り、応答を読み終えてから接続全体を閉じます。

serverのportに`0`を指定するとOSが空きportを選び、readyの`address.port`に表示します。
認証や永続化を持たないローカルの部品例で、待受先は`127.0.0.1`に固定しています。
