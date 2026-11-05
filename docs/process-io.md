# プロセスと標準I/Oストリーム

実行バイナリと同じ版のAPI契約は`noe docs node:child_process spawn`・`noe docs noe:io`で参照できます。概要に表示されるmember名を第2引数へ渡すと詳細を取得します（[内蔵docs](reference.md)）。

The NOE-specific `spawn` export from `node:child_process`, together with `noe:io`, is available to run/eval, agent `run_js`, and Workers. `spawn` resolves its Promise after startup and exposes byte-oriented Web Streams of `Uint8Array`. It does not add Node.js EventEmitter or Node streams APIs. Existing `exec`, `execFile`, `spawnSync`, `process.std*`, and the `noe:io` facade remain available.

## 子を起動して通信する

```js
import { spawn } from "node:child_process";

const child = await spawn("some-command", ["--jsonl"], {
  cwd: ".",
  stdin: "pipe", stdout: "pipe", stderr: "inherit",
});
try {
  const writer = child.stdin.getWriter();
  await writer.write(new TextEncoder().encode('{"id":1}\n'));
  await writer.close(); // EOFを伝える。子の終了を仮定しない
  const answer = await new Response(child.stdout).text();
  const status = await child.exited;
  console.log(answer, status);
} finally {
  await child.close();
}
```

`spawn(command, args = [], options = {})`は起動成功後にハンドルを返すPromiseです。シェルを暗黙には使いません。必要なら`spawn("sh", ["-c", script])`と明示します。

| 項目 | 動作 |
| --- | --- |
| cwd | 省略時は呼出し元のcwd。相対指定も同じcwdから解決 |
| env | If `Profile.Environment.Env` is unset, the host-configured default is used. Omitting spawn's `env` inherits the selected environment. An explicit `env` object replaces the whole environment; entries with `null` values are omitted. |
| stdin / stdout / stderr | 各`pipe`・`inherit`・`ignore`。既定はすべてpipe |
| child.pid | 対象コマンドのPID。内部監督のPIDではない |
| child.stdin | `WritableStream<Uint8Array>`。pipe以外はnull。closeでEOF、abortで書込み端を閉じる |
| child.stdout / child.stderr | `ReadableStream<Uint8Array>`。pipe以外はnull。cancelでその読取り端を閉じる |
| child.exited | `Promise<{code: number\|null, signal: string\|null}>`。非ゼロ終了もresolveする。起動失敗はspawnがreject、監督機構の失敗はexitedがreject |
| child.kill(signal = "SIGTERM") | 所有するprocess group/Jobへ停止要求。終了待ちは別途exitedで行う |
| child.close() | 冪等な強制回収。所有するプロセス・pipeと内部監督を回収する。未読出力は捨ててよい。inheritの転送エラーも報告する |

終了と出力EOFは別です。終了直前の出力を読む場合もstdout/stderrをEOFまで読みます。大量出力は両方を並行して読むか、inherit/ignoreを指定してください。未読pipeが満杯になれば子は待ちます。Go側で全文を収集せず、execFileのキャプチャ上限も適用しません。

inheritは呼出し元のホストI/Oを使い、観察時のwriterも通ります。ignoreはstdinにEOFを送り、stdout/stderrを破棄します。POSIXではSIGTERM等をgroupへ送り、WindowsではSIGTERM/SIGKILLともJobを強制終了します。Windowsの終了値を架空のPOSIX signalへ変換しません。自ら別group/session等へ離脱したプロセスを管理するAPIではありません。

子と内部監督が生存・回収中の間、NOEの実行は続きます。run_jsの所有者が強制終了された場合も、POSIXでは所有者pipeのEOFを監督が検知して対象groupを停止します。この所有関係のためPOSIXではspawnごとにGo製監督1プロセスが追加されます。親終了後の独立実行機能ではありません。

## ホスト標準I/Oとpipe

```js
import { stdin, stdout, pipe } from "noe:io";
await pipe(stdin, stdout, { preventClose: true });
```

stdinはread要求から逐次読み、EOFを待たずにバイト列を返します。importだけでは読取りを始めません。stdout/stderrへのwriteは非同期で、実際の書込み完了を待ちます。文字列にはTextEncoder/TextDecoderを使います。JSONLの例は[stdio-jsonl.js](../examples/process/stdio-jsonl.js)です。

ホストのstdin/stdout/stderrは借用品です。ストリームのclose/abort/cancelやNOEの終了で元のI/Oを閉じません。既存のconsoleとprocess.stdin.read()/stdout.write()も使えます。同じstdinを新旧APIから並行して消費する使い方には読取り順の保証がありません。

`pipe(source, destination, { signal, preventClose = false } = {})`はEOFまで転送し、既定では宛先をcloseします。preventCloseは正常EOF時にだけ宛先を開いたままにします。エラー・abortではreader.cancelとwriter.abortを開始して待機中のI/Oにも取消しを伝え、原因でrejectします。子のkillは行わないのでfinallyでchild.close()を呼べます。

固定stdlibの`.pipeTo(..., {signal})`はデータ待ち中のabortを直ちに処理しないため、取消しが必要な接続はこのpipeを使います。既存のpipeToの実装は変更していません。

通常実行とeventsファイルのmirrorはバイト列を流します。`--events=-`や`--observe`ではstdoutが観察用に使われ、出力は文字列イベントとして扱います。任意バイナリの転送に観察stdoutを使わないでください。

## Worker

Workers register the same `node:child_process.spawn` extension. Their stdin is empty and output is inherited from the host; terminating a Worker reclaims children it started. If `Profile.Environment.Env` is set, that environment is used by default; otherwise the child inherits the host-configured default. An explicit spawn `env` replaces the whole environment.

```js
await (async () => {
  const { spawn } = await import("node:child_process");
  const c = await spawn("some-command", [], { stdin: "ignore", stderr: "inherit" });
  try { return await new Response(c.stdout).text(); }
  finally { await c.close(); }
})()
```

[実行例](../examples/README.md) · [実装記録・OS別検証](development/process-io-implementation.md)
