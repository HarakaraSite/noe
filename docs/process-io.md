# プロセスと標準I/Oストリーム

Use `noe docs noe:process spawn` and `noe docs noe:process stdin` for the API contract in the matching executable. Pass a member name shown in a topic overview as the second argument to open its details ([embedded reference](reference.md)).

`noe:process` combines captured commands, live children, host standard I/O, and process metadata for run/eval, agent `run_js`, and Workers when their host capabilities are selected. `spawn` resolves its Promise after startup and exposes byte-oriented Web Streams of `Uint8Array`. Host standard I/O uses the same Web Stream types. NOE does not add Node.js EventEmitter or Node streams compatibility for standard I/O. The former `noe:io` module and the synchronous `process.stdin.read()`, `process.stdout.write()`, `process.stderr.write()`, and `isTTY` facades are removed.

The current source removes `node:child_process`, `child_process`, `node:process`, and `process` imports without adapters. Command and stream named imports move directly to `noe:process`. Replace `import process from "node:process"` with `import {process} from "noe:process"`; the metadata/control object remains identical to global `process`. The default export is now a namespace of all selected named exports, with metadata under `.process`. `ProcessInfo`, `Stdio`, and `Commands` stay independently selectable. Function arguments, command results, stream ownership, and global process behavior are retained. This migration is unreleased; see the [implementation and migration record](development/stdlib-noe-process.md).

## 子を起動して通信する

```js
import { spawn } from "noe:process";

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

## Host standard I/O

```js
import { stdin, stdout } from "noe:process";

const text = await new Response(stdin).text();
const writer = stdout.getWriter();
try {
  await writer.write(new TextEncoder().encode(text));
} finally {
  writer.releaseLock();
}
```

`stdin` is a demand-driven `ReadableStream<Uint8Array>`; importing it does not start a read. `stdout` and `stderr` are `WritableStream<Uint8Array>` values. Use `Response` or `TextDecoder` to read text, and encode text with `TextEncoder` before writing. The JSONL example is [stdio-jsonl.js](../examples/process/stdio-jsonl.js).

The host's input and output endpoints are borrowed. Closing, aborting, or canceling a Web Stream does not close the caller's underlying reader or writer. A stream can have only one active consumer; do not read the same stdin concurrently through multiple readers.

Use the standard Web Streams `pipeTo` method to connect streams:

```js
await child.stdout.pipeTo(stdout, { preventClose: true });
```

`pipeTo` closes its destination at normal EOF unless `preventClose` is set. With default cancellation options, an `AbortSignal` cancels the source and aborts the destination, including pending I/O, then rejects the pipe promise with the abort reason. `preventCancel` and `preventAbort` suppress cleanup for their corresponding side. A pipe does not terminate a child process; call `child.close()` in `finally` when the child is owned by the current runtime.

通常実行とeventsファイルのmirrorはバイト列を流します。`--events=-`や`--observe`ではstdoutが観察用に使われ、出力は文字列イベントとして扱います。任意バイナリの転送に観察stdoutを使わないでください。

## Worker

Workers register the same `noe:process.spawn` extension. When standard I/O is selected, their `noe:process` stdin reaches immediate EOF and stdout/stderr use the configured host destinations; terminating a Worker reclaims children it started. If `Profile.Environment.Env` is set, that environment is used by default; otherwise the child inherits the host-configured default. An explicit spawn `env` replaces the whole environment.

```js
await (async () => {
  const { spawn } = await import("noe:process");
  const c = await spawn("some-command", [], { stdin: "ignore", stderr: "inherit" });
  try { return await new Response(c.stdout).text(); }
  finally { await c.close(); }
})()
```

[実行例](../examples/README.md) · [実装記録・OS別検証](development/process-io-implementation.md)
