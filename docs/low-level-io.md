# ファイルハンドル・ソケット・OSシグナル

実行バイナリと同じ版のAPI契約は`noe docs noe:fs-handle`・`noe docs noe:sockets`・`noe docs noe:signals`で参照できます。概要に表示されるmember名を第2引数へ渡すと詳細を取得します（[内蔵docs](reference.md)）。

このガイドのAPIはv1.8.0以降で利用できます。

`noe:fs-handle`、`noe:sockets`、`noe:signals`はNOEの拡張です。run/eval、agentのrun_js、Workerで使えます。現在のOSユーザー権限で動きます。stdlibの`node:fs`、HTTP、プロセスAPIも引き続き使えます。

## 開いたファイルの一部分を読み書きする

```js
import { open } from "noe:fs-handle";
const file = await open("./index.bin", { mode: "r+" });
try {
  const { data, bytesRead, eof } = await file.readAt(32, 16);
  console.log(bytesRead, eof);
  await file.writeAt(32, data);
} finally {
  await file.close();
}
```

| 操作 | 動作 |
| --- | --- |
| `open(path, {mode="r"}={})` | Promiseでhandleを返す。`r`は既存fileの読取り、`r+`は読書き。作成・truncate・appendを行わない。相対pathはruntimeのcwd基準 |
| `file.readAt(position, length)` | byte位置から読み、`{data:Uint8Array, bytesRead, eof}`を返すPromise。短いreadとEOFを同時に保持。長さ0は空bytesで成功しEOFを探索しない |
| `file.writeAt(position, bytes)` | Uint8Arrayを呼出し時にcopyし、成功時は全長の書込み数を返すPromise。対象外bytesを変更せず、fileを縮めない |
| `file.close()` | Promise。冪等。新規I/Oを拒否し、開始済みのGo処理が終わるまで追跡する |

position/lengthは非負のsafe integerで、Goへ変換でき、加算がoverflowしない値を指定します。空/NUL入りpathは入力エラーです。OSエラーには取得できる`code`が付きます。EOF以外の部分read失敗はerrorの`data`・`bytesRead`、部分write失敗は`bytesWritten`を保持してrejectします。完了済みの書込みはrollbackしません。

開いた通常fileだけでは実行を維持しません。未完了I/Oは実行を維持し、runtime終了時に所有handleを回収します。REPLでは開いたhandleを後続入力でも使えます。デバイス・FIFO・NFS等でblockするOS file I/Oの即時取消しは保証しません。新規作成・metadataはstdlibを使えます。共有seek位置、fd公開、lock、sync/durabilityの契約はありません。

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

`connect(options)`と`listen(options)`はPromiseです。TCPは`{network:"tcp",host,port,signal?}`、Unixは`{network:"unix",path,signal?}`を指定します。host/pathは必須。TCP portはconnectで1〜65535、listenで0〜65535で、0はOS割当てです。DNS・IPv4/IPv6はGo netの解決に従います。Unix相対pathはruntimeのcwd基準です。TLS、UDP、abstract namespace、接続retryは提供しません。Unix socketの可用性は実行OSに依存します。

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

SIGINTは既存CLI/REPLのCtrl+C取消しと共存し、JS購読が取消しを消費することはありません。SIGINT後のasync cleanup完走は保証しません。SIGTERMは購読中のOS既定終了を抑止し、自動でprocess.exitしません。終了したいJSはresourceとsubscriptionをcloseしてloopを終えます。

WorkerだけがSIGTERMを購読していてもNOEプロセス全体の既定終了が抑止されます。Worker内のprocess.exitはそのWorkerだけを終了します。プロセス全体を終える場合は通常のメッセージ等でmainへ伝え、mainも所有resourceをcloseします。

購読単独で実行を維持します。明示close、runtime終了、Worker terminateで購読とGo参照を解除し、JSのhandler完了を待ちません。

## REPLと観察出力

REPLは現行の入力内drain方式です。socketとsignalは待受・通信・解除まで同じ入力のasync関数内で行います。所有resourceを残したエラー・中断ではsessionを終了して回収します。

socketの生bytesはJSONLイベントへ自動転記しません。consoleや`noe:io`へ明示出力したときに既存の観察writerを通ります。

[プロセスと標準I/O](process-io.md) · [例](../examples/low-level/README.md) · [実装・検証記録](development/low-level-group2-implementation.md)
