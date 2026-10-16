# NOE APIリファレンス

[READMEへ戻る](../README.md) · [REPLガイド](repl.md) · [実行例](../examples/README.md)

NOEはJavaScriptの実行環境として、固定したgo-quickjs v0.25.3の標準ライブラリを有効にします。標準ライブラリが持つWeb APIとNode形式APIを利用できます。各APIはgo-quickjsによる実装で、Node.js全体との互換性を保証するものではありません。

JavaScriptの`noe`グローバルと`noe.*` APIはありません。NOE固有で残るのはCLIの`run`・`eval`・`repl`・`--timeout`と、REPLの履歴・補完・前回結果`_`です。[旧APIからの移行](#旧apiからの移行)と[移行の実装記録](increment-12.md)も参照してください。

## 利用できる機能と権限

Web APIにはURL、Fetch、Request/Response/Headers、Web Streams、タイマー、WebSocket、Worker、cryptoなどがあります。Node形式の組み込みモジュールには、たとえば`assert`、`buffer`、`child_process`、`dns`、`events`、`fs`、`fs/promises`、`http`、`os`、`path`、`process`、`timers`、`timers/promises`、`url`、`util`、`vm`、`worker_threads`、`zlib`があります。暗号機能はWeb Cryptoと`crypto`から利用できます。APIごとの未実装機能や差異は、go-quickjsの実装に従います。

NOEは次のホスト機能をスクリプトに渡します。

- ローカルのファイルシステム全体の読み書き
- 起動時のプロセス環境変数、標準入力・標準出力・標準エラー、プロセス終了
- 実OSの情報
- DNS問い合わせと任意の宛先へのHTTP・WebSocket通信
- 任意のコマンド実行
- HTTPサーバーの待受
- Workersとメッセージング

Workersにも同じホスト機能を渡します。NOEはsandboxではありません。スクリプトは起動した利用者と同じOS権限で動作します。

npm、パッケージ導入、URLからのモジュールimport、bare importによる外部パッケージ解決はありません。裸の組み込みモジュール名と対応する`node:`名で標準ライブラリをimportできます。ローカルモジュールは拡張子付きの相対`.js`・`.mjs`パスを使います。import先はimport元のファイルを基準に解決し、`eval`内ではカレントディレクトリが基準です。QuickJS本来の`std`モジュールは提供しません。

## 実行環境とファイル

`process.argv`は実行ファイル名、スクリプト名、スクリプト引数の順です。スクリプト引数は`process.argv.slice(2)`で取得できます。`process.env`は起動時の環境変数、`process.cwd()`はカレントディレクトリです。`process.exit(code)`でプロセスを終了し、終了コードを指定できます。

標準入力全体は`process.stdin.read()`で読み取れます。この読み取りは同期処理で、最初の呼出しでEOFまで読み込み、2回目以降は`null`を返します。空入力でも`null`です。REPLの標準入力はソース入力用なので、`process.stdin.read()`にはデータを渡さず`null`になります。この同期読取りを`--timeout`で強制停止する保証はありません。標準入力を待つスクリプトでは、パイプ入力や端末からのEOFを使って入力を終えてください。

ファイルのPromise APIは`fs/promises`からimportできます。

| API | 動作 |
| --- | --- |
| `readFile(path, "utf8")` | UTF-8テキストを文字列として読む |
| `readFile(path)` | 全バイトをUint8Arrayとして読む |
| `writeFile(path, data)` | 文字列またはバイト列を書き込む |
| `readdir(path)` | 直下の名前を文字列の配列で返す |
| `stat(path)`・`lstat(path)` | `size`、`mode`、`mtimeMs`、`mtime`と、`isFile()`・`isDirectory()`などを返す。`stat`はリンク先、`lstat`はリンク自身を調べる |
| `mkdir(path, options)` | `{ recursive: true }`などの標準オプションを受け付ける |
| `rename(from, to)` | 名前を変更または移動する |
| `rm(path, options)` | `{ recursive: true, force: true }`などの標準オプションを受け付ける |

`readdir`の結果は名前だけで、Direntオブジェクトではありません。`stat`はシンボリックリンクの参照先、`lstat`はリンク自身を調べます。パスの解決はファイル操作時のカレントディレクトリが基準です。

```js
import { readFile, writeFile } from "fs/promises";

const inputPath = process.argv[2];
const outputPath = process.argv[3];
const value = JSON.parse(await readFile(inputPath, "utf8"));
value.processed = true;
await writeFile(outputPath, JSON.stringify(value, null, 2) + "\n");
```

## 非同期処理

Promise、`async/await`、top-level awaitを利用できます。タイマーPromiseは次のように使います。

```js
import { setTimeout as sleep } from "timers/promises";

await sleep(1000);
console.log("done");
```

通常の完了では、開始した非同期処理と後続のJavaScriptを待ちます。解決する処理が残っていない未完了のtop-level awaitは、`run`・`eval`で実行失敗になります。実行期限は`--timeout`で設定できます。

## Fetch

グローバルの`fetch()`はWeb Fetch APIです。`Request`・`Response`・`Headers`を使い、レスポンス本文は`text()`、`json()`、`arrayBuffer()`、`bytes()`、またはReadableStreamで読み取れます。レスポンス本文は一度だけ読み取れます。HTTP 4xx・5xxもResponseとして返るため、必要に応じて`response.ok`を確認してください。

```js
const response = await fetch("https://example.com/api/data");
if (!response.ok) throw new Error(`HTTP ${response.status}`);
const value = await response.json();
console.log(value);
```

JSONを送信する例です。

```js
const response = await fetch("https://example.com/api/items", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ value: 42 }),
});
console.log(response.status, await response.text());
```

## HTTPサーバー

グローバルの`serve(options, handler)`はWeb Requestを受け取り、Web Responseを返すHTTPサーバーを起動します。`options`では`hostname`と`port`を指定できます。ポートの既定値は0で、OSが空きポートを選びます。返されるサーバーハンドルには`url`、`port`、`hostname`、`close()`があります。サーバーが稼働中は実行を継続します。

```js
const server = serve({ port: 8080 }, async request => {
  const pathname = new URL(request.url).pathname;
  if (pathname === "/echo" && request.method === "POST") {
    return Response.json(await request.json());
  }
  return new Response("Not Found", { status: 404 });
});
console.log(server.url);
```

リクエスト本文はWeb Request APIで読み、応答は必ずResponseで返します。JSONの応答には`Response.json(value)`を使えます。HTTPモジュールから`serve`をimportすることもできます。WebSocketはWebSocket APIと、サーバー側では`serve`を通じて利用できます。

`server.close()`は同期的に受付を停止し、go-quickjs v0.25.3では処理中のHTTPリクエスト終了を最大5秒待ちます。停止を求めるリクエスト自身のハンドラーから同期的に呼ぶと、そのハンドラーの完了を待つことになります。応答が完了した後の別の処理から呼んでください。これは旧`noe.serve()`の非同期停止とは異なる動作です。

## REPLでの非同期処理

REPLでは変数や関数を入力間で保持します。タイマーだけが残った入力をCtrl+Cで中断した場合はタイマーを取り消して続行できます。Fetch、サーバー、Worker、メッセージポートなど、ランタイム全体に結び付いた標準ライブラリ処理が残った状態で中断または入力エラーが起きると、ランタイムを閉じてREPLセッションを終了します。入力単位の停止後に既存の関数やimportを安全に再利用できないためです。詳しくは[REPLガイド](repl.md)を参照してください。

## 旧APIからの移行

以前のNOE固有APIは提供しません。既存スクリプトは次の標準APIへ移行してください。

| 以前のAPI | 移行先 |
| --- | --- |
| `noe.args` | `process.argv.slice(2)` |
| `noe.env.get(name)` | `process.env[name]` |
| `noe.cwd()` | `process.cwd()` |
| `noe.exit(code)` | `process.exit(code)` |
| `noe.readStdin()` | `process.stdin.read()` |
| `noe.readTextFile(path)` | `fs/promises`の`readFile(path, "utf8")` |
| `noe.writeTextFile(path, text)` | `fs/promises`の`writeFile(path, text)` |
| `noe.readFile(path)`・`noe.writeFile(path, data)` | `fs/promises`の`readFile`・`writeFile` |
| `noe.readDir(path)` | `readdir(path)`で名前を取得し、`lstat()`で各項目を判定 |
| `noe.stat(path)` | `stat(path)` |
| `noe.mkdir`・`noe.rename`・`noe.remove` | `fs/promises`の`mkdir`・`rename`・`rm` |
| `noe.sleep(ms)` | `timers/promises`の`setTimeout(ms)` |
| `noe.fetch(url, options)` | グローバルの`fetch(url, options)` |
| `noe.serve(options, handler)` | グローバルの`serve(options, handler)` |

標準APIでは、FetchのResponse本文は一度だけ読み取れます。`process.stdin.read()`はPromiseではなく、入力全体を同期的に一度だけ読みます。`readdir`は名前の配列を返し、`stat`と`lstat`は標準ライブラリの情報オブジェクトを返します。`readdir`で得た各項目の種別を知るには、名前をpathに連結して`lstat`または`stat`を呼びます。オプションや戻り値を標準APIに合わせて更新してください。
