# NOE APIリファレンス

[READMEへ戻る](../README.md) · [REPLガイド](repl.md) · [実行例](../examples/README.md)

NOEが公開するAPIと対応範囲です。現在はJavaScriptに対応し、npm・Node/Deno互換は実装しません。HTTPは独自オブジェクトを使うサブセットです。

## 組み込みAPI

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

たとえば[transform-json.js](../examples/transform-json.js)は、同じディレクトリの`lib/transform.js`を読み込みます。

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

`Request`・`Response`・`Headers`クラス、ストリーミング、URLからのimportは提供しません。

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

HTTPリクエストの実行例は[http-json.js](../examples/README.md#json-apiへのリクエスト)にまとめています。

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

ワイルドカードアドレスでbindした場合、接続に使うホストは利用者が選びます。HTTP bodyは全体をメモリへ保持します。ストリーミング、TLSの公開設定、WebSocket、リクエスト単位のJS強制中断は提供しません。

起動とcurlの手順は[HTTPサーバーの実行例](../examples/README.md#httpサーバー)を参照してください。
