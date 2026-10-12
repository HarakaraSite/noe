# NOE — No Opulence Engine

A tiny JavaScript/TypeScript runtime written in pure Go.

No bloat. Just scripts.

現在はJavaScriptに対応し、ファイル実行・`eval`・REPLを提供します。TypeScriptは後から追加する予定です。npm対応、Node/Deno互換は実装しません。

## ビルドと実行

Go 1.27.2以降が必要です。Cのツールチェーンは不要です。

```sh
CGO_ENABLED=0 go build -o noe ./cmd/noe
./noe --help
./noe --version
./noe eval 'console.log(1 + 1)'
./noe run examples/hello.js
./noe repl
```

`.js`と`.mjs`をESモジュールとして実行します。`run`ではファイルの後ろに指定した引数を、そのままスクリプトに渡します。`eval`の引数配列は空です。

run/evalには任意の実行時間上限を指定できます。指定しなければ上限を設けません。

```sh
./noe run --timeout 5s examples/hello.js
./noe eval --timeout=500ms 'console.log("started"); while (true) {}'
# 終了コード124、標準エラーへ時間切れの診断
```

`--timeout`はスクリプト名・コードの前に指定します。`500ms`・`5s`・`1m`などGoのduration形式の正の値を使い、空・不正・ゼロ・負・重複は使い方エラーです。`--`でオプションを終端できます。runのスクリプト名の後にある引数は、timeoutオプションに見えるものもそのまま`noe.args`へ渡します。

エントリソースの読込み後、ランタイム初期化からJS評価・開始した非同期I/O・後続JSの完了待ちまでを計測します。JSの無限ループ、HTTP待ち、標準入力のEOF待ちも対象です。同期OS syscallやローカルimportのOS読取りを強制停止する保証はありません。REPLにはこのオプションを追加せず、Ctrl+Cによる中断を使います。中断までの出力やファイルの変更は残ります。

```sh
printf '{"value":21}\n' > /tmp/noe-input.json
./noe run examples/transform-json.js /tmp/noe-input.json /tmp/noe-output.json
cat /tmp/noe-output.json
```

HTTPで取得したJSONを保存する例です。URLには利用するAPIを指定してください。

```sh
./noe run examples/fetch-json.js https://example.com/api/data.json /tmp/noe-data.json
```

画像・PDFなどのバイナリをそのまま保存する例です。URLには取得するファイルを指定し、保存先の親ディレクトリを事前に用意します。

```sh
./noe run --timeout 10s examples/download-file.js https://example.com/image.png /tmp/noe-image.png
```

複数のJSONを並行して読み、キャンセル分を除外して商品別に売上を集計し、JSONとMarkdownを並行保存するデモです。

```sh
REPORT_TITLE='週末の売上レポート' ./noe run examples/sales-report.js
cat /tmp/noe-sales-report.md
```

引数で出力先のプレフィックスと入力ファイルを変更できます。

```sh
./noe run examples/sales-report.js /tmp/my-report examples/data/sales-day-1.json
```

標準入力のJSON配列を集計し、JSONを標準出力へ返す例です。

```sh
printf '[100,200,300]\n' | ./noe run examples/summarize-stdin.js
# {"count":3,"total":600,"average":200}
printf '日本語\n' | ./noe eval 'console.log(await noe.readStdin())'
```

ディレクトリ直下の数値配列の`.json`ファイルを並行して読み、ファイル別のバイト数・件数・合計と、全体の件数・合計・平均をJSONで出力できます。サブディレクトリやシンボリックリンクは集計に含めません。

```sh
./noe run examples/collect-json.js examples/data/numbers
# 全体で5件、合計800、平均160
./noe run examples/collect-json.js examples/data/numbers > /tmp/noe-summary.json
./noe eval 'const report = JSON.parse(await noe.readStdin()); console.log(report.total)' < /tmp/noe-summary.json
```

集計結果を保存して入力を処理済みディレクトリへ移す例もあります。試すための入力を作業ディレクトリへコピーして実行します。

```sh
NOE_DEMO_DIR=$(mktemp -d)
mkdir "$NOE_DEMO_DIR/input"
cp examples/data/numbers/*.json "$NOE_DEMO_DIR/input/"
./noe run examples/process-json.js "$NOE_DEMO_DIR/input" "$NOE_DEMO_DIR/output"
cat "$NOE_DEMO_DIR/output/report.json"
ls "$NOE_DEMO_DIR/output/processed"
```

`process-json.js`は直下の通常`.json`の数値配列をすべて解析し、出力ディレクトリを作成して`report.json.tmp`を書き、`report.json`へ名前変更します。入力ファイルは出力先の`processed`へ移し、作業markerの`.processing`を削除して、集計JSONを標準出力へ返します。サブディレクトリとリンクは処理しません。入力と出力には別ディレクトリを指定してください。

既存の`report.json.tmp`・`.processing`を上書きし、正常完了時に`.processing`を削除します。Linuxでは同名のレポート・処理済みファイルも置換します。途中の失敗で保存・移動済みの変更は残り、作業用ファイルも残る場合があります。

## REPL

`./noe repl`で起動し、1行ずつJSを試せます。同じランタイムを使い続けるため、変数・関数・オブジェクトは次の入力でも使えます。

```text
> let count = 1
> count += 1
2
> const text = await noe.readTextFile("examples/data/sales-day-1.json")
> const orders = JSON.parse(text)
> orders.length
4
> const sales = await import("./examples/lib/sales.js")
> sales.summarizeSales(orders).totalRevenue
3100
> noe.exit()
```

式の結果を表示し、`undefined`は省略します。文字列の結果には引用符が付きます。通常のJSエラーや未処理のPromise rejectionは標準エラーに表示して続行し、実行済みの変更を巻き戻しません。

非同期入力は`await expression`、`const name = await expression`・`let`・`var`の単一変数初期化、`name = await expression`に対応します。`const`は再代入できません。通常のPromise値を暗黙に結果値へ変換しないため、解決結果を使う場合は明示的に`await`してください。

非同期の初期化は右辺を待ってから変数を宣言します。同名の`const`・`let`が既にあっても、右辺のI/Oを実行した後に重複宣言のエラーになります。非同期の分割代入・複数変数宣言・任意の複数文の変換、静的`import`・`export`は提供しません。モジュールには例のように動的`import`を使います。対応するawait式・単一変数の初期化や代入は、改行を含んでも使えます。

各入力で開始したファイル・HTTP処理と後続のJS処理を待ってから、次の入力へ進みます。パスは起動時のカレントディレクトリが基準です。REPLの`noe.args`は空配列です。

未完の関数・オブジェクト・括弧などを入力すると、`... `で続きを待ち、確定した入力をまとめて評価します。確定した構文エラーは診断して次の入力へ進みます。

```text
> function twice(n) {
...   return n * 2;
... }
> twice(21)
42
> _ + 1
43
```

`_`は最後に成功したundefined以外の結果です。最初はundefinedで、エラー・中断・undefinedの結果では更新しません。オブジェクトは参照を保持します。

対応する端末では、入力の途中を編集したり、前の入力を履歴から呼び出したりできます。履歴は次の起動でも使え、複数行の入力も1件として保存します。既定の保存先はユーザー設定ディレクトリの`noe/repl_history.jsonl`です。Linuxでは`$XDG_CONFIG_HOME`、未設定なら`~/.config`の下になります。メモリ内の履歴は直近1000件で、保存ファイルへは追記します。

`NOE_REPL_HISTORY`で保存先を変更できます。空文字を指定すると、保存・読込みを無効にし、そのセッション内の履歴だけを使います。読込み・保存に失敗すると標準エラーへ通知し、REPLを続けます。

```sh
NOE_REPL_HISTORY='' ./noe repl
NOE_REPL_HISTORY=/tmp/my-noe-history.jsonl ./noe repl
```

| キー | 操作 |
| --- | --- |
| ←・→、Ctrl+B・F | 文字ごとに移動 |
| Home・End、Ctrl+A・E | 行の先頭・末尾へ移動 |
| Backspace・Delete | 文字を削除 |
| Alt+B・F、Ctrl+←・→ | 単語ごとに移動 |
| Ctrl+W、Ctrl+U・K | 前の単語、行の先頭まで、行末までを削除 |
| ↑・↓、Ctrl+P・N | 履歴を呼び出す。修正してEnterで再実行できる |
| Ctrl+R・S | 履歴を検索 |
| Tab | キーワード・グローバル名・成功した入力の宣言名・オブジェクトのプロパティを補完 |
| Ctrl+L | 画面を消去 |

`noe.re`や`console.lo`を入力してTabを押すと、候補を補完できます。成功した入力のトップレベルの`let`・`const`宣言は、配列・オブジェクトの分割代入も含めて候補へ追加します。関数や`var`はグローバル名から取得します。失敗した入力内の部分宣言や、識別子のエスケープ表記の収集には対応しません。識別子とドットの列を対象とし、関数呼出しや添字を含む式は対象外です。プロパティのgetterは呼びません。Proxyのtrapが候補取得を妨げる場合は、その補完を中断して通常入力へ戻ります。

空の入力でCtrl+D（EOF）、または`noe.exit()`で終了します。未完の入力を保持したままEOFになると、診断を表示して終了します。対応する端末でのCtrl+Cは、編集中・履歴検索中・複数行入力中の入力を破棄し、JS評価中・HTTP待ちならその入力を中断して、`Interrupted`を表示して次のプロンプトへ戻ります。変数・関数と中断前の変更は残り、ファイルの変更は巻き戻しません。

パイプやリダイレクトした入出力、端末制御に対応しない環境では、通常の行読み取りを使い、履歴ファイルを扱いません。パイプ入力でも`> `や継続時の`... `を表示し、改行のない最終入力も評価します。非端末のSIGINTと入力待ちでの外部SIGINTはコード130で終了します。対話端末で評価中のSIGINTは入力中断として扱います。引数なしの`./noe`はhelpを表示します。

## API

| API | 動作 |
| --- | --- |
| `console.log(...values)` | 標準出力に表示 |
| `console.warn(...values)`・`console.error(...values)` | 標準エラーに表示 |
| `noe.args` | スクリプト引数の配列。実行ファイル名とスクリプト名は含まない |
| `noe.env.get(name)` | 環境変数の文字列。未設定なら`undefined` |
| `noe.cwd()` | カレントディレクトリの絶対パス |
| `noe.exit(code = 0)` | 実行を即座に終了。コードは0〜255の整数 |
| `noe.sleep(ms)` | 指定ミリ秒を待つ`Promise<void>`。msは0〜9223372036854の整数のnumber。timeout・REPL中断・明示終了で待機を取り消す |
| `noe.readStdin()` | run/evalの標準入力をEOFまで読む`Promise<string>`。繰り返し呼出しは同じPromiseを返す。REPLでは提供しない |
| `noe.readTextFile(path)` | テキストを読み込む`Promise<string>` |
| `noe.writeTextFile(path, text)` | テキストを上書き保存する`Promise<void>`。親ディレクトリは事前に用意する |
| `noe.readFile(path)` | 全バイトを読み込む`Promise<Uint8Array>`。空ファイルは長さ0、呼出しごとに独立した配列 |
| `noe.writeFile(path, bytes)` | Uint8Arrayのビュー範囲を上書き保存する`Promise<void>`。呼出し時の内容をコピーし、後の変更・detachに影響されない。親ディレクトリは事前に用意する |
| `noe.readDir(path)` | 名前順の直下一覧を返すPromise。各要素は`{name, isFile, isDirectory, isSymlink}`。nameはベース名。再帰せず、リンク自身の種別を返す |
| `noe.stat(path)` | `{size, modifiedAt, isFile, isDirectory}`を返すPromise。sizeはバイト数、modifiedAtはUTCのRFC3339文字列（小数秒を含み得る）。リンク先を調べる |
| `noe.mkdir(path)` | 親も含めてディレクトリを作る`Promise<void>`。既存ディレクトリは成功。作成時は0755にOSのumaskを適用 |
| `noe.rename(from, to)` | ファイル・ディレクトリ・リンク自身を名前変更する`Promise<void>`。移動先の親は事前に作る。Linuxでは既存の通常ファイルを置換 |
| `noe.remove(path)` | ファイル・リンク自身・空ディレクトリを削除する`Promise<void>`。存在しないパス・空でないディレクトリはreject。再帰削除しない |
| `noe.serve(options, handler)` | HTTPサーバーを起動する。optionsはhostname・port。同期的にサーバーハンドルを返し、稼働中は実行を継続する |

ファイル操作の相対パスはコマンドを実行したディレクトリが基準です。ローカルimportはimport元のファイルが基準で、`.js`か`.mjs`の拡張子を明示します。`eval`内の相対importはカレントディレクトリが基準です。

`readStdin`は改行を保持し、空入力は空文字を返します。EOFまで待つので、対話端末からデータを渡す場合はEOFを入力してください。スクリプトのコードは従来どおりファイルや`eval`引数で指定します。`readDir`の空ディレクトリは空配列となり、ファイル・ディレクトリの読取り失敗はPromiseのrejectになります。

名前変更・削除はリンク先に作用せず、リンク自身を扱います。renameの置換可否はOSに従い、ファイルシステムをまたぐ移動はコピーで補わずrejectします。操作が失敗すればrejectし、依存する操作は`await`で順序を指定します。中断・明示終了でも実行済みのファイル変更は残ります。

バイナリは文字列への変換を通さず、NULや非UTF-8のバイトも保持します。`writeFile`はUint8Arrayとその派生クラスを受け付け、部分配列はその範囲だけを保存します。空のUint8Arrayは空ファイルを書きます。文字列・普通の配列・ArrayBuffer・DataView・他のTypedArray・Proxy・detach済みのバッファはrejectし、型が合わなければファイルを開きません。

```js
const bytes = await noe.readFile("input.bin");
await noe.writeFile("copy.bin", bytes);
await noe.writeFile("prefix.bin", bytes.subarray(0, 16));
```

```js
import { transform } from "./lib/transform.js";

const input = JSON.parse(await noe.readTextFile(noe.args[0]));
await noe.writeTextFile(noe.args[1], JSON.stringify(transform(input)));
```

Promise、`async/await`、top-level awaitを利用できます。通常終了では、開始した標準入力・ファイル・HTTP処理・sleepと後続のJS処理を待ちます。`run`・`eval`では、解決する処理が残っていない未完了のtop-level awaitは失敗として終了します。REPLでは診断後に次の入力へ進みます。`noe.exit`は停止を優先し、後続のJSや`catch`・`finally`を実行しません。

`await noe.sleep(ms)`で、他の処理を止めずに待機できます。0もPromiseを返し、解決結果はundefinedです。引数の暗黙変換はせず、number以外はTypeError、負数・小数・NaN・Infinity・上限超過はRangeErrorでrejectします。指定した時間より早くは完了せず、JSやOSの処理状況によって後になる場合があります。

```js
for (let i = 0; i < 3; i++) {
  console.log(`実行 ${i + 1}`);
  if (i < 2) await noe.sleep(1000);
}
```

同じ例を`./noe run examples/sleep-demo.js`で試せます。

## HTTPサブセット

`fetch`は必要な範囲だけを提供し、Web標準全体への互換性を目指しません。

```js
const response = await fetch("https://example.com/api/data", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ value: 42 }),
});
console.log(response.status, response.ok);
console.log(await response.json());
```

- URLは文字列。メソッドはGET・POST・PUT・PATCH・DELETEで、省略時はGETです。メソッドの文字列は大文字へ正規化します。
- オプションは`method`・`headers`・`body`。ヘッダーは文字列値のオブジェクトです。bodyはPOST・PUT・PATCH・DELETEで省略可能な文字列で、空文字も送れます。省略・undefinedは空のbodyとして扱います。GETはundefined以外のbody指定を受け付けません。未対応の入力はエラーになります。
- JSONは`JSON.stringify`で文字列にし、Content-Typeは明示します。オブジェクトの暗黙のJSON化やContent-Typeの自動設定は行いません。
- レスポンスは`status`、`ok`、`text()`、`json()`、`bytes()`を提供します。`ok`は200〜299で`true`です。
- `text()`と`json()`はPromiseを返します。bodyを一括取得して保持するため、繰り返し呼べます。
- `bytes()`は全bodyを`Promise<Uint8Array>`で返します。毎回独立したコピーとなり、配列を変更・detachしても後のbytes/text/jsonは変わりません。
- 4xx・5xxはレスポンスとして返します。通信失敗やJSON解析失敗はPromiseのrejectになります。
- HTTP通信はGoの標準クライアントを使います。プロキシ環境変数とリダイレクト処理はその動作に従います。

204などの空レスポンスは`text()`で空文字、`bytes()`で長さ0のUint8Arrayを返し、`json()`はJSON解析エラーになります。

```js
const response = await fetch("https://example.com/image.png");
if (!response.ok) throw new Error(`HTTP ${response.status}`);
await noe.writeFile("image.png", await response.bytes());
```

JSON APIの更新・削除には`examples/http-json.js`を使えます。URLとJSONの項目は利用するAPIに合わせて指定してください。

```sh
ITEM_URL='https://example.com/api/items/1'
./noe run --timeout 5s examples/http-json.js PUT "$ITEM_URL" '{"name":"日本語","count":1}'
./noe run --timeout 5s examples/http-json.js PATCH "$ITEM_URL" '{"count":2}'
./noe run --timeout 5s examples/http-json.js DELETE "$ITEM_URL"
```

JSON-bodyがある場合、この例はJSONとして検証してから明示的に文字列化し、application/jsonで送ります。成功時は`{status, ok, body}`のJSONをstdoutへ返し、空レスポンスのbodyはnullにします。この例ではHTTP失敗を診断してコード1で終了します。fetch自体は4xx・5xxもレスポンスとして返します。

ストリーム、`Request`・`Response`・`Headers`クラス、URLからのimport、npmパッケージ、Node/Deno APIは提供しません。

## HTTPサーバー

GoのHTTPサーバーとNOE共通イベントループを使い、小さなサーバーをJSで書けます。ハンドラーは独自のリクエストオブジェクトを受け取り、応答オブジェクトまたはそのPromiseを返します。

```js
const server = noe.serve({ port: 8080 }, async request => {
  if (request.method === "POST" && request.path === "/echo") {
    return {
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(await request.json()),
    };
  }
  return { body: "Hello from NOE\n" };
});
console.log(server.url);
```

optionsは`hostname`・`port`のみ。hostnameは空でない文字列で、既定値は`127.0.0.1`です。portは0〜65535の整数で、既定値の0ではOSが空きポートを選びます。待受に失敗するとthrowします。ハンドルの`hostname`・`port`は実際の待受アドレス、`url`はそのHTTP URLです。複数サーバーを起動できます。

リクエストは`method`・`url`・`path`・`headers`を持ちます。urlはqueryを含む絶対URL、pathはGoのURL.Pathによるデコード済みのパスで、queryを含みません。headersは小文字名をキー、文字列配列を値とし、Hostと同名ヘッダーの値も保持します。`text()`・`json()`・`bytes()`はPromiseを返し、全bodyを繰り返し読めます。bytesは毎回独立したUint8Arrayで、変更やdetachは後の読取りに影響しません。空bodyのjsonは解析エラーです。

応答は`{ status, headers, body }`。statusは省略時200、200〜599の整数です。headersは文字列または文字列配列を値とするオブジェクト、bodyは文字列かUint8Arrayで、省略・undefinedなら空です。バイナリはビュー範囲をコピーしてGoへ渡します。JSONは明示的に文字列化し、Content-Typeを指定します。HEAD・204・304ではbodyを送信しません。

ハンドラーのthrow・reject・不正な応答は500とstderrの診断になり、サーバーは受付を続けます。クライアントが切断した場合、その接続への応答を破棄し、他のJSハンドラーを割り込みません。開始済みの処理と変更は巻き戻しません。

`server.close()`は新規受付を停止し、listenerを解放してundefinedを返します。受付済みの処理と応答の完了は共通ループが待ちます。繰り返し呼べて、ハンドラー内からcloseして応答を返すこともできます。GoのI/Oは並行しますが、JSは一つのgoroutineで実行するため、同期JSの長い処理は他のハンドラーを待たせます。

`--timeout`は待受を含む実行全体に適用します。Ctrl+Cや`noe.exit()`、アプリ全体の実行エラーではサーバーと接続を閉じます。REPLで起動した場合はサーバー稼働中に次のプロンプトへ進まず、Ctrl+Cで停止して次の入力へ戻ります。

デモを起動し、別の端末から試せます。

```sh
./noe run examples/http-server.js 8080
```

```sh
curl http://127.0.0.1:8080/health
curl -H 'Content-Type: application/json' \
  -d '{"message":"こんにちは"}' http://127.0.0.1:8080/echo
```

デモのportは省略時8080、0なら空きポートです。起動時に実際のURLを表示します。不正JSONは400、他のパスは404。起動した端末のCtrl+Cで終了します。

HTTP bodyは全体をメモリへ保持します。ストリーミング、TLSの公開設定、WebSocket、setTimeout・setInterval、リクエスト単位のJS強制中断は提供しません。

## エラーと検証

`run`・`eval`では成功は終了コード0、実行失敗は1、CLIの使い方の誤りは2、設定した`--timeout`の時間切れは124です。`noe.exit`で指定したコードはそのまま返します。未処理のPromise rejectionも実行失敗になります。SIGINTによるrun/evalの中断は従来のコード1です。

REPLは回復可能な入力エラーの後も続行し、EOFで0を返します。入力読み取り失敗やエンジン内部エラーなど、続行できない失敗は1で終了します。

エンジンが提供するエラー位置とスタックを表示します。文字列などを直接throwした場合は、取得できるメッセージに実行元の名前を添えて表示します。

```sh
go test ./...
go test -tags=e2e ./tests -v
```

E2EはCGOを無効にしてCLIをビルドし、別プロセスから実行します。標準入力のJSON加工、ディレクトリ内JSONの集計、レポート保存と処理済み入力の移動、ディレクトリ作成・名前変更・削除、ファイル情報、ファイルの加工、バイナリの保存・部分ビュー・ダウンロード、ローカルモジュール、引数・環境変数、HTTPのGET・POST・PUT・PATCH・DELETE、非同期完了、エラーと明示終了、JS・HTTP・stdin待ちのtimeoutを確認します。HTTPサーバーでは実リクエスト、async並行処理、応答完了、停止と再bind、複数待受、クライアント切断を確認します。REPLでは連続入力、状態保持、エラー後の続行、EOF、入力待ち・評価中・HTTP待ちの割込みも確認します。LinuxではPTYで基本の編集、日本語、再起動後の履歴、複数行、前回結果、Tab補完、サーバー待受中を含む実際のCtrl+Cキーによる中断後の続行と、終了後の端末設定の復元を確認します。履歴は一時ディレクトリを使い、HTTPテストにはローカルサーバーを使います。外部サービスには接続しません。

[MVP案](docs/mvp.md)、[インクリメント1](docs/increment-1.md)、[インクリメント2](docs/increment-2.md)、[インクリメント3](docs/increment-3.md)、[インクリメント4](docs/increment-4.md)、[インクリメント5](docs/increment-5.md)、[インクリメント6](docs/increment-6.md)、[インクリメント7](docs/increment-7.md)、[インクリメント8](docs/increment-8.md)、[インクリメント9](docs/increment-9.md)に範囲と受入条件を記載しています。

[インクリメント10](docs/increment-10.md)に、NOE共通イベントループと簡易HTTPサーバーの範囲・受入条件・検証記録を記載しています。

[インクリメント11](docs/increment-11.md)に、noe.sleepの契約・実装・検証記録を記載しています。
