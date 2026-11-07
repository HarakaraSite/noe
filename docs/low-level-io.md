# File handles, sockets, and OS signals

Use `noe docs noe:fs open` and `noe docs noe:fs FileHandle.read` for the file handle contract. Use `noe docs noe:sockets` and `noe docs noe:signals` for the NOE socket and signal APIs. Member names from a topic overview select detailed entries in the embedded reference ([guide](reference.md)).

The `connect`, `listen`, and `noe:signals` APIs in this guide are available from v1.8.0. UDP datagrams are a source-only addition after v2.17.0; see below.

`noe:sockets` and `noe:signals` are NOE extensions available to run/eval, agent `run_js`, and Workers. They use the operating-system permissions of the NOE process. The `noe:fs`, HTTP, and process APIs are available through their selected capabilities.

## Open files

`open(path, {flags:"r"})` from `noe:fs` returns a FileHandle; creation and append flags are also supported. `read(length)` returns a Uint8Array and `write(data)` returns a byte count. Use `readAt(position, length)` and `writeAt(position, data)` for positional I/O without moving the cursor. `truncate` rejects negative lengths. Close handles explicitly or use `await using`. The complete contract and errors are documented in the embedded `noe:fs` reference. Open handles do not keep an evaluation alive; runtime cleanup closes remaining handles.

## TCP・Unixソケットで通信する

```js
import { connect } from "noe:sockets";
const connection = await connect({ network: "tcp", host: "127.0.0.1", port: 9000 });
try {
  const writer = connection.writable.getWriter();
  await writer.write(new TextEncoder().encode("request\n"));
  await writer.close(); // 送信FIN。受信方向は使える
  console.log(await new Response(connection.readable).text());
} finally {
  await connection.close();
}
```

`connect(options)`と`listen(options)`はPromiseです。TCPは`{network:"tcp",host,port,signal?}`、Unixは`{network:"unix",path,signal?}`を指定します。host/pathは必須。TCP portはconnectで1〜65535、listenで0〜65535で、0はOS割当てです。DNS・IPv4/IPv6はGo netの解決に従います。Unix相対pathはruntimeのcwd基準です。UDP datagram sockets are available through `bind()` below; abstract namespace and connection retries are not provided. Unix socketの可用性は実行OSに依存します。

Raw TLS sockets are available in source after v2.17.0; the released v2.17.0 binary does not include them. See [Raw TLS sockets](#raw-tls-sockets) below.

開始用`signal`はdial/listen完了までの取消しに使い、成功後は購読解除します。成功後はhandleのcloseかruntime終了で停止します。既にabort済みのsignal、不正port、空/NUL入りpathは開始前にrejectします。

```js
import { listen } from "noe:sockets";
const listener = await listen({ network: "tcp", host: "127.0.0.1", port: 0 });
console.log(listener.address); // 割当てportを含むsnapshot
try {
  const connection = await listener.accept();
  try {
    const request = await new Response(connection.readable).text();
    const writer = connection.writable.getWriter();
    await writer.write(new TextEncoder().encode(request.toUpperCase()));
    await writer.close();
  } finally { await connection.close(); }
} finally { await listener.close(); }
```

`listener.address`はTCPのhost/portまたはUnixのpathを返します。accept要求ごとに接続を受理し、複数pending acceptにはそれぞれ一つの接続を返します。listenerのcloseは新規acceptを拒否し、pending acceptをrejectします。受理済みconnectionは開いたままで、所有者が別にcloseします。

connectionの`readable`/`writable`はUint8ArrayのWeb Streamsです。readは需要に応じて実行し、writeはOS書込み完了を待ってbackpressureを保ちます。socketにはrun_js出力の64KiB捕捉上限を適用しません。

| 操作・状態 | 受信方向 | 送信方向 |
| --- | --- | --- |
| peerからEOF | 残bytesを返して終了 | 維持 |
| `reader.cancel()` | pending readを解除して終了 | 維持 |
| `writer.close()` | 維持 | pending write完了後にFIN |
| `writer.abort(reason)` | 維持 | pending writeを中断し終了 |
| `connection.close()` / runtime終了 | pending I/Oを解除してclose | 同左 |

両方向が終了しても、finallyでconnection.closeを呼びます。listenerとconnectionはopen中に実行を維持します。importだけでは待受を始めません。runtime終了時の回収はJSのPromise配送を待ちません。

Unixのbind前に既存pathを削除しません。親directory内のprivateな一時directoryへbindしてidentityを記録し、既存先を上書きしないhard linkで要求pathへ公開します。filesystem/OSのsocket hard link対応が必要で、未対応時はOSエラーを返します。長いpathでは短いsymlink経由のbindも使うため、その場合はsymlink対応も必要です。`listener.address.path`は要求pathのcwd解決後の値です。

通常closeでは作成したsocketのidentityを照合してpathnameを片付け、既に別file/別socketへ置換されていれば削除しません。identity照合とremoveはOSのatomicな比較削除ではないため、アプリは同じpathの置換とcloseを協調させます。SIGKILL等の強制終了後はpathnameや公開途中の一時directoryが残り得ます。再起動時はアプリ所有directoryの状態を確認し、必要ならJS/CLIで片付けます。

## UDP datagram sockets

UDP datagrams are available in source after v2.17.0; the released and
installed v2.17.0 binary does not include `bind()`. Build a temporary CLI
from this checkout to use the [local UDP round-trip example](../examples/low-level/README.md#udp-datagram-round-trip):

```sh
demo_dir="$(mktemp -d)"
trap 'rm -rf "$demo_dir"' EXIT
CGO_ENABLED=0 go build -o "$demo_dir/noe" ./cmd/noe
"$demo_dir/noe" run examples/low-level/udp-roundtrip.js
```

`bind({network: "udp", host, port, signal?})` returns an owned datagram
socket. The host must be nonempty and the port must be 0–65535; port `0`
requests an operating-system-selected port. `socket.address` reports the
bound endpoint. Send one `Uint8Array` per `send(data, address, {signal}?)`
call; the destination port must be 1–65535. A successful send resolves to
the number of bytes accepted by the local operating system, not a delivery
acknowledgement. Empty datagrams are valid. `receive({signal}?)` returns one
complete packet as `{data, remoteAddress}`; it does not expose streams or EOF.

Bind cancellation applies only until the bind completes. Send and receive
cancellation affects only that operation and preserves its exact JavaScript
reason; it can discard an already received packet or follow a send already
accepted by the operating system. A 65,536-byte receive buffer accommodates
an ordinary full UDP datagram. Receive errors reject; a full buffer rejects
with RangeError/ERR_UDP_MESSAGE_TOO_LARGE rather than exposing a possible prefix.
Operating-system routing and packet-size errors reject, with no
additional application send limit. UDP does not guarantee delivery, order,
duplicate suppression, or retries. Close the socket explicitly or use
`await using`; runtime shutdown and Worker termination also reclaim it.
`Modules.RawSockets` is the existing capability in both main runtimes and
Workers; no separate UDP grant is added.

## Raw TLS sockets

The `network: "tls"` variant uses the same `Connection`, `Listener`, and
Web Streams as TCP. `connect()` returns only after the TLS handshake succeeds
and verifies the server chain and hostname. It uses system roots by default;
`serverName` selects the SNI and verification name, otherwise the connection
host is used. A nonempty `ca` PEM string replaces system roots for that
connection. Optional client `cert` and `key` values must be a matching PEM
pair. JavaScript has no certificate-verification bypass.

TLS listeners accept `cert` and `key` PEM strings, unless the Go host supplies
the server configuration. Values are PEM data, not paths; read credential
files explicitly with `noe:fs`. A TLS listener's address is
`{network: "tls", host, port}`. Start `accept()` before awaiting the client
connection so that a server TLS handshake can proceed. A failed handshake
rejects one `accept()` and leaves the listener available. Closing the listener
cancels pending handshakes and undelivered accepts; connections already
returned by `accept()` have separate ownership.

Closing a TLS writer sends `close_notify` and leaves reads available, so a
client can send TLS EOF and then read a server response. Aborting a writer
interrupts writes without a graceful TLS alert. Cancelling a reader stops local
read delivery; pending native TLS protocol work is joined by full close.
Full close or asynchronous disposal closes the transport
promptly. See the [TLS round-trip example](../examples/low-level/README.md)
and the embedded [socket reference](../internal/apidocs/content/noe-sockets.md).

For a Go host, `Modules.RawSockets` still selects `noe:sockets`.
`Modules.RawSocketsTLS` configures the client/server TLS policy but does not
select the capability; see [Go embedding](go-embedding.md#raw-tls-sockets).
TLS configuration remains independent from Fetch, HTTP/HTTPS, and WebSockets,
and does not grant filesystem access.

## OSシグナルを受け取る

```js
import { subscribe } from "noe:signals";
const signals = subscribe("SIGHUP");
try {
  for await (const name of signals) {
    console.log(name);
    break; // return()が購読を解除
  }
} finally { await signals.close(); }
```

`subscribe(name)`は要求したsignalを登録してsubscriptionを同期的に返します。未対応名と捕捉不能なSIGKILL/SIGSTOPは`code:"ERR_UNSUPPORTED_SIGNAL"`を持つerrorを同期throwします。

| OS | 対応名 |
| --- | --- |
| Linux / Darwin | SIGHUP、SIGINT、SIGTERM、SIGUSR1、SIGUSR2 |
| Windows | SIGINT |

subscriptionはAsyncIteratorです。`next()`は`{value:signalName,done:false}`のPromise、close後は`{done:true}`を返します。複数pending nextへFIFOで配送します。`close()`は冪等なPromiseで、未解決nextをdoneで解決します。`return()`もcloseへ接続し、for-awaitのbreakで解除します。

OS/Go側で通知がまとめられたり落ちたりするため、送信回数と受信回数の一致は保証しません。next待機がないときは同名通知を一つにまとめ、無制限には蓄積しません。受信後のJS処理と例外は通常のPromise/未処理例外の規則に従います。

OS受信はprocess-wide、購読はruntime単位です。mainとWorkerは同じsignalも異なるsignalも購読できます。一方のclose/terminateは他方の購読を解除しません。最後のNOE購読をcloseするとNOE自身のGo登録だけをStopします。他のGo登録がなければOS既定動作に戻ります。

SIGINTはCLIのCtrl+C取消しと共存し、JS購読が取消しを消費することはありません。SIGINT後のasync cleanup完走は保証しません。SIGTERMは購読中のOS既定終了を抑止し、自動でprocess.exitしません。終了したいJSはresourceとsubscriptionをcloseしてloopを終えます。

WorkerだけがSIGTERMを購読していてもNOEプロセス全体の既定終了が抑止されます。Worker内のprocess.exitはそのWorkerだけを終了します。プロセス全体を終える場合は通常のメッセージ等でmainへ伝え、mainも所有resourceをcloseします。

購読単独で実行を維持します。明示close、runtime終了、Worker terminateで購読とGo参照を解除し、JSのhandler完了を待ちません。

## 観察出力

Raw socket bytes are not copied into JSONL events automatically. Explicit writes through `console` or the `noe:process` stdout/stderr Web Streams use the existing observation writer.

[プロセスと標準I/O](process-io.md) · [例](../examples/low-level/README.md) · [実装・検証記録](development/low-level-group2-implementation.md)
