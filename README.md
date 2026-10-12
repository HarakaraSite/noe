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

```sh
printf '{"value":21}\n' > /tmp/noe-input.json
./noe run examples/transform-json.js /tmp/noe-input.json /tmp/noe-output.json
cat /tmp/noe-output.json
```

HTTPで取得したJSONを保存する例です。URLには利用するAPIを指定してください。

```sh
./noe run examples/fetch-json.js https://example.com/api/data.json /tmp/noe-data.json
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

非同期の初期化は右辺を待ってから変数を宣言します。同名の`const`・`let`が既にあっても、右辺のI/Oを実行した後に重複宣言のエラーになります。非同期の分割代入・複数変数宣言・複数文の変換、静的`import`・`export`、複数行入力、JS補完は提供しません。モジュールには例のように動的`import`を使います。

各入力で開始したファイル・HTTP処理と後続のJS処理を待ってから、次の入力へ進みます。パスは起動時のカレントディレクトリが基準です。REPLの`noe.args`は空配列です。

対応する端末では、入力の途中を編集したり、前の入力を履歴から呼び出したりできます。履歴は現在のREPLセッション内だけに保持し、ディスクへ保存しません。

| キー | 操作 |
| --- | --- |
| ←・→、Ctrl+B・F | 文字ごとに移動 |
| Home・End、Ctrl+A・E | 行の先頭・末尾へ移動 |
| Backspace・Delete | 文字を削除 |
| Alt+B・F、Ctrl+←・→ | 単語ごとに移動 |
| Ctrl+W、Ctrl+U・K | 前の単語、行の先頭まで、行末までを削除 |
| ↑・↓、Ctrl+P・N | 履歴を呼び出す。修正してEnterで再実行できる |
| Ctrl+R・S | 履歴を検索 |
| Ctrl+L | 画面を消去 |

空の入力でCtrl+D（EOF）、または`noe.exit()`で終了します。Ctrl+Cは編集中・履歴検索中・JS評価中・HTTP待ちのいずれでもセッション全体を終了し、コード130を返します。

パイプやリダイレクトした入出力、端末制御に対応しない環境では、通常の行読み取りを使います。パイプ入力でも`> `を表示し、改行のない最終入力も評価します。引数なしの`./noe`はhelpを表示します。

## API

| API | 動作 |
| --- | --- |
| `console.log(...values)` | 標準出力に表示 |
| `console.warn(...values)`・`console.error(...values)` | 標準エラーに表示 |
| `noe.args` | スクリプト引数の配列。実行ファイル名とスクリプト名は含まない |
| `noe.env.get(name)` | 環境変数の文字列。未設定なら`undefined` |
| `noe.cwd()` | カレントディレクトリの絶対パス |
| `noe.exit(code = 0)` | 実行を即座に終了。コードは0〜255の整数 |
| `noe.readTextFile(path)` | テキストを読み込む`Promise<string>` |
| `noe.writeTextFile(path, text)` | テキストを上書き保存する`Promise<void>`。親ディレクトリは事前に用意する |

ファイル操作の相対パスはコマンドを実行したディレクトリが基準です。ローカルimportはimport元のファイルが基準で、`.js`か`.mjs`の拡張子を明示します。`eval`内の相対importはカレントディレクトリが基準です。

```js
import { transform } from "./lib/transform.js";

const input = JSON.parse(await noe.readTextFile(noe.args[0]));
await noe.writeTextFile(noe.args[1], JSON.stringify(transform(input)));
```

Promise、`async/await`、top-level awaitを利用できます。通常終了では、開始したファイル・HTTP処理と後続のJS処理を待ちます。`run`・`eval`では、解決する処理が残っていない未完了のtop-level awaitは失敗として終了します。REPLでは診断後に次の入力へ進みます。`noe.exit`は停止を優先し、後続のJSや`catch`・`finally`を実行しません。

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

- URLは文字列。メソッドはGET・POSTで、省略時はGETです。
- オプションは`method`・`headers`・`body`。ヘッダーは文字列値のオブジェクト、bodyはPOSTで送る文字列です。未対応の入力はエラーになります。
- レスポンスは`status`、`ok`、`text()`、`json()`を提供します。`ok`は200〜299で`true`です。
- `text()`と`json()`はPromiseを返します。bodyを一括取得して保持するため、繰り返し呼べます。
- 4xx・5xxはレスポンスとして返します。通信失敗やJSON解析失敗はPromiseのrejectになります。
- HTTP通信はGoの標準クライアントを使います。プロキシ環境変数とリダイレクト処理はその動作に従います。

ストリーム、`Request`・`Response`・`Headers`クラス、URLからのimport、npmパッケージ、Node/Deno APIは提供しません。

## エラーと検証

`run`・`eval`では成功は終了コード0、実行失敗は1、CLIの使い方の誤りは2です。`noe.exit`で指定したコードはそのまま返します。未処理のPromise rejectionも実行失敗になります。

REPLは回復可能な入力エラーの後も続行し、EOFで0を返します。入力読み取り失敗やエンジン内部エラーなど、続行できない失敗は1で終了します。

エンジンが提供するエラー位置とスタックを表示します。文字列などを直接throwした場合は、取得できるメッセージに実行元の名前を添えて表示します。

```sh
go test ./...
go test -tags=e2e ./tests -v
```

E2EはCGOを無効にしてCLIをビルドし、別プロセスから実行します。ファイルの加工、ローカルモジュール、引数・環境変数、HTTPのGET・POST、非同期完了、エラーと明示終了を確認します。REPLでは連続入力、状態保持、エラー後の続行、EOF、入力待ち・評価中・HTTP待ちの割込みも確認します。LinuxではPTYで基本の編集、日本語、履歴、実際のCtrl+Cキー、終了後の端末設定の復元を確認します。HTTPテストにはローカルサーバーを使い、外部サービスには接続しません。

[MVP案](docs/mvp.md)、[インクリメント1](docs/increment-1.md)、[インクリメント2](docs/increment-2.md)、[インクリメント3](docs/increment-3.md)に範囲と受入条件を記載しています。次は[インクリメント4](docs/increment-4.md)でREPLの操作性を改善します。
