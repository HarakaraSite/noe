# NOE APIリファレンス

[READMEへ戻る](../README.md) · [REPLガイド](repl.md) · [実行例](../examples/README.md)

NOEはJavaScriptの実行環境として、固定したgo-quickjs v0.25.3の標準ライブラリを有効にします。標準ライブラリが持つWeb APIとNode形式APIを利用できます。各APIはgo-quickjsによる実装で、Node.js全体との互換性を保証するものではありません。

JavaScriptに`noe`グローバルや旧`noe.*` APIはありません。NOE固有の入口として、CLIの`run`・`eval`・`repl`・`--timeout`、REPLの履歴・補完・前回結果`_`、および組み込みES moduleの`noe:agent`があります。`noe:agent`はWorkersでは利用できません。[旧APIからの移行](#旧apiからの移行)と[移行の実装記録](increment-12.md)も参照してください。

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

Workersにも同じgo-quickjs標準ライブラリのホスト機能を渡します（`noe:agent`を除く）。NOEはsandboxではありません。スクリプトは起動した利用者と同じOS権限で動作します。

npm、パッケージ導入、URLからのモジュールimport、bare importによる外部パッケージ解決はありません。裸の組み込みモジュール名と対応する`node:`名で標準ライブラリをimportできます。ローカルモジュールは拡張子付きの相対`.js`・`.mjs`パスを使います。import先はimport元のファイルを基準に解決し、`eval`内ではカレントディレクトリが基準です。QuickJS本来の`std`モジュールは提供しません。

## 自律agent（OpenRouter）

`noe:agent`から`createAgent()`をimportできます。ルートagentにはmodel名が必要で、providerは省略時`openrouter`です。現在利用できるproviderはOpenRouterだけです。 Go側の通信には[OpenRouter公式Go SDK v0.9.59](https://github.com/OpenRouterTeam/go-sdk/tree/v0.9.59)を使います。

```js
import { createAgent } from "noe:agent";

const summary = createAgent({
  model: "deepseek/deepseek-v4.1-flash",
  name: "summary",
  system: "日本語で簡潔に回答してください。",
  maxTokens: 1024,
});

const answer = await summary.run("README.mdを読んで要点を3点にまとめてください。", {
  cacheKey: "noe-summary-v1",
  maxSteps: 6,
});
console.log(answer);
```

`createAgent(options)`は`{provider?, model, name?, system?, maxTokens?}`を受け取ります。`provider`は`openrouter`だけです。`model`は空でない文字列、`name`は空でない文字列、`system`は文字列、`maxTokens`は0以上の整数です。`name`の既定値は`agent`、`system`の既定値は空文字、`maxTokens`の既定値は0です。`system`はagentの標準system instructionへ追加されます。0の場合、NOEはOpenRouterへ`max_tokens`を送りません。

`agent.run(instruction, options)`はPromiseを返し、成功時の値は最終回答の文字列です。標準出力へ自動では表示しません。`instruction`は空でない文字列です。`options`は`{cacheKey?, maxSteps?}`で、`cacheKey`は任意の文字列、`maxSteps`は0以上の整数で既定値は0です。`maxSteps`はこのrunのモデル要求回数を制限し、tool回数や子agentを含む合計回数は制限しません。0はNOE側の上限なしを意味します。`maxTokens`はモデル要求ごとの出力上限で、会話全体の累計ではありません。どちらの上限も指定しない場合にNOEは独自の上限を追加しません。

`cacheKey`はOpenRouterの`session_id`として送られ、routingやprompt cacheで使われる値です。未指定（`undefined`を含む）の場合は、そのrun用に生成した`runId`を使い、同じrun内の全モデル要求へ同じIDを送ります。子agentにも個別のIDを自動付与します。同じagent objectの再利用や`Promise.all`でもrunごとに異なるIDになり、親のIDは継承しません。自動IDはtraceの`runId`と対応します。

明示した`cacheKey`はそのまま使用するため、同じrouting用IDを再利用したい場合は指定できます。`cacheKey: ""`は従来どおり`session_id`を省略します。キャッシュヒットは保証せず、会話履歴を保存・再開する機能でもありません。各`run()`は独立した履歴を持ちます。OpenRouterの[session_id仕様](https://openrouter.ai/docs/guides/best-practices/prompt-caching#using-session_id-for-sticky-sessions)を参照してください。

生成JavaScriptのtoolは`run_js`一つです。各呼出しは新しいNOEプロセスで実行し、同じカレントディレクトリと標準ライブラリを使います。グローバル変数はtool呼出し間で保持せず、生成ソースは実行後に削除します。生成コードでは`createAgent`が事前に利用可能です。子agentがprovider、model、`maxTokens`を省略すると、そのコードを起動したrunの値を継承します。明示した値は上書きされ、`maxTokens: 0`も有効な上書きです。子の`name`、`system`、会話履歴は親から継承しません。

生成ソースは一時moduleに保存されるため、相対importはそのソース位置を基準にします。カレントディレクトリのmoduleを読み込む場合は、`process.argv[1]`（実行中のソース）の場所からcwdへの相対prefixを作り、dynamic importします。

```js
const { relative, dirname, sep } = await import("node:path");
const cwdPrefix = relative(dirname(process.argv[1]), process.cwd()).split(sep).join("/");
const helper = await import((cwdPrefix ? cwdPrefix + "/" : "./") + "helper.js");
```

生成JSの標準出力・標準エラー・終了コードは`{stdout, stderr, exitCode, reason}`のtool結果としてモデルへ返ります。`reason`は`completed`、`failed`、`timeout`、`cancelled`のいずれかです。モデルは失敗を受けてコードを修正できます。`run()`は認証、残高・利用枠、レート制限、通信、provider応答の不備、モデル拒否、token上限到達、空の最終回答、`maxSteps`到達でrejectします。provider SDKを含め、暗黙の再試行はしません。モデルが質問を最終回答として返しても、人間の入力待ちには入らず、その文字列を返します。

### 認証と設定

OpenRouter APIキーをテキストファイルへ保存し、`NOE_OPENROUTER_API_KEY_FILE`でパスを指定します。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key ./noe run examples/agent-summary.js README.md
```

この変数が空または未設定なら、`$XDG_CONFIG_HOME/noe/credentials/openrouter-api-key`を探します。`XDG_CONFIG_HOME`が未設定なら`~/.config/noe/credentials/openrouter-api-key`を使います。ファイル内容の前後の空白は取り除きます。Go側が最初のモデル要求時にファイルを読み、importや`createAgent()`では読みません。キー値はモデル履歴・子接続情報・traceへ含めません。NOEはsandboxではなく、実行するJSは同一ユーザーのOS権限でファイルや環境変数へアクセスできます。

`NOE_OPENROUTER_BASE_URL`はOpenRouter APIのbase URLを上書きし、既定値は`https://openrouter.ai/api/v1`です。`NOE_AGENT_TRACE=1`を指定すると、実行メタデータをJSON Linesで標準エラーへ出力します。

```sh
NOE_AGENT_TRACE=1 NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent-summary.js README.md 2>agent-trace.log
```

traceにはrun ID、親run ID、agent名、model、status、duration（nanoseconds）、返された場合はprompt/completion/total/cached token数とcostを記録します。`js_result`にはtoolの終了コードと理由も含みます。prompt、生成code、toolのstdout/stderr、APIキーは記録しません。標準エラーには通常の診断も含まれる場合があります。

通常の`run`・`eval`では`--timeout`が評価全体を制限できます。省略時はCLI独自の期限はありません。REPLでは通信中のCtrl+Cで要求を取り消し、稼働中のI/Oを残さないようREPL sessionを終了します。詳しくは[REPLガイド](repl.md)を参照してください。

WindowsでTEMPとカレントディレクトリが別volumeの場合、生成ソースをカレントディレクトリ上の一時場所へ書き込み、相対importの基準を保ちます。その場合はカレントディレクトリへ一時ファイルを書き込める必要があります。書込みできない場合は、カレントディレクトリと同じvolumeの書込み可能な場所を`TEMP`に指定してください。

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
