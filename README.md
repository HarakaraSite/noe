# NOE — No Opulence Engine

A tiny JavaScript/TypeScript runtime written in pure Go.

No bloat. Just scripts.

インクリメント1はJavaScriptのみ対応しています。TypeScriptとREPLは後から追加する予定です。npm対応、Node/Deno互換は実装しません。

## ビルドと実行

Go 1.24以降が必要です。Cのツールチェーンは不要です。

```sh
CGO_ENABLED=0 go build -o noe ./cmd/noe
./noe --help
./noe --version
./noe eval 'console.log(1 + 1)'
./noe run examples/hello.js
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

Promise、`async/await`、top-level awaitを利用できます。通常終了では、開始したファイル・HTTP処理と後続のJS処理を待ちます。解決する処理が残っていない未完了のtop-level awaitは失敗として終了します。`noe.exit`は停止を優先し、後続のJSや`catch`・`finally`を実行しません。

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

成功は終了コード0、実行失敗は1、CLIの使い方の誤りは2です。`noe.exit`で指定したコードはそのまま返します。未処理のPromise rejectionも実行失敗になります。

エンジンが提供するエラー位置とスタックを表示します。文字列などを直接throwした場合は、取得できるメッセージに実行元の名前を添えて表示します。

```sh
go test ./...
go test -tags=e2e ./tests -v
```

E2EはCGOを無効にしてCLIをビルドし、別プロセスから実行します。ファイルの加工、ローカルモジュール、引数・環境変数、HTTPのGET・POST、非同期完了、エラーと明示終了を確認します。HTTPテストにはローカルサーバーを使い、外部サービスには接続しません。

[MVP案](docs/mvp.md)と[インクリメント1の実装計画](docs/increment-1.md)に範囲と受入条件を記載しています。
