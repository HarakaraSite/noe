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
| `noe.readStdin()` | run/evalの標準入力をEOFまで読む`Promise<string>`。繰り返し呼出しは同じPromiseを返す。REPLでは提供しない |
| `noe.readTextFile(path)` | テキストを読み込む`Promise<string>` |
| `noe.writeTextFile(path, text)` | テキストを上書き保存する`Promise<void>`。親ディレクトリは事前に用意する |
| `noe.readDir(path)` | 名前順の直下一覧を返すPromise。各要素は`{name, isFile, isDirectory, isSymlink}`。nameはベース名。再帰せず、リンク自身の種別を返す |
| `noe.stat(path)` | `{size, modifiedAt, isFile, isDirectory}`を返すPromise。sizeはバイト数、modifiedAtはUTCのRFC3339文字列（小数秒を含み得る）。リンク先を調べる |
| `noe.mkdir(path)` | 親も含めてディレクトリを作る`Promise<void>`。既存ディレクトリは成功。作成時は0755にOSのumaskを適用 |
| `noe.rename(from, to)` | ファイル・ディレクトリ・リンク自身を名前変更する`Promise<void>`。移動先の親は事前に作る。Linuxでは既存の通常ファイルを置換 |
| `noe.remove(path)` | ファイル・リンク自身・空ディレクトリを削除する`Promise<void>`。存在しないパス・空でないディレクトリはreject。再帰削除しない |

ファイル操作の相対パスはコマンドを実行したディレクトリが基準です。ローカルimportはimport元のファイルが基準で、`.js`か`.mjs`の拡張子を明示します。`eval`内の相対importはカレントディレクトリが基準です。

`readStdin`は改行を保持し、空入力は空文字を返します。EOFまで待つので、対話端末からデータを渡す場合はEOFを入力してください。スクリプトのコードは従来どおりファイルや`eval`引数で指定します。`readDir`の空ディレクトリは空配列となり、ファイル・ディレクトリの読取り失敗はPromiseのrejectになります。

名前変更・削除はリンク先に作用せず、リンク自身を扱います。renameの置換可否はOSに従い、ファイルシステムをまたぐ移動はコピーで補わずrejectします。操作が失敗すればrejectし、依存する操作は`await`で順序を指定します。中断・明示終了でも実行済みのファイル変更は残ります。

```js
import { transform } from "./lib/transform.js";

const input = JSON.parse(await noe.readTextFile(noe.args[0]));
await noe.writeTextFile(noe.args[1], JSON.stringify(transform(input)));
```

Promise、`async/await`、top-level awaitを利用できます。通常終了では、開始した標準入力・ファイル・HTTP処理と後続のJS処理を待ちます。`run`・`eval`では、解決する処理が残っていない未完了のtop-level awaitは失敗として終了します。REPLでは診断後に次の入力へ進みます。`noe.exit`は停止を優先し、後続のJSや`catch`・`finally`を実行しません。

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

E2EはCGOを無効にしてCLIをビルドし、別プロセスから実行します。標準入力のJSON加工、ディレクトリ内JSONの集計、レポート保存と処理済み入力の移動、ディレクトリ作成・名前変更・削除、ファイル情報、ファイルの加工、ローカルモジュール、引数・環境変数、HTTPのGET・POST、非同期完了、エラーと明示終了を確認します。REPLでは連続入力、状態保持、エラー後の続行、EOF、入力待ち・評価中・HTTP待ちの割込みも確認します。LinuxではPTYで基本の編集、日本語、再起動後の履歴、複数行、前回結果、Tab補完、実際のCtrl+Cキーによる中断後の続行と、終了後の端末設定の復元を確認します。履歴は一時ディレクトリを使い、HTTPテストにはローカルサーバーを使います。外部サービスには接続しません。

[MVP案](docs/mvp.md)、[インクリメント1](docs/increment-1.md)、[インクリメント2](docs/increment-2.md)、[インクリメント3](docs/increment-3.md)、[インクリメント4](docs/increment-4.md)、[インクリメント5](docs/increment-5.md)、[インクリメント6](docs/increment-6.md)に範囲と受入条件を記載しています。
