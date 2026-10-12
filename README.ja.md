# NOE — No Opulence Engine

A tiny JavaScript/TypeScript runtime written in pure Go.

No bloat. Just scripts.

この文書は日本語版です。正本は[英語版README](README.md)です。
小さなスクリプトを実行するためのGo製ランタイムです。ファイル処理、HTTP、REPLなど、実際に使う機能を少しずつ実装しています。JavaScriptエンジンには[go-quickjs](https://github.com/go-quickjs/go-quickjs)を使います。

**現在の対応言語はJavaScriptです。TypeScriptは後から追加する予定です。** npm対応・Node/Deno互換は実装しません。Web APIは必要なサブセットを提供します。

[APIリファレンス](docs/api.md) · [REPLガイド](docs/repl.md) · [実行例](examples/README.md) · [開発記録](docs/README.md)

## ビルドと実行

Go 1.27.2以降が必要です。Cのツールチェーンは不要です。以下のコマンドはリポジトリのルートで実行します。

```sh
git clone https://forge.harakara.site/littleisland/noe
cd noe
CGO_ENABLED=0 go build -o noe ./cmd/noe
```

```sh
./noe --version
# noe v1.0.0

./noe run examples/hello.js
# Hello from NOE!

./noe eval 'console.log(1 + 1)'
# 2

./noe repl
```

## すぐに試す

標準入力のJSONを集計します。

```sh
printf '[100,200,300]\n' | ./noe run examples/summarize-stdin.js
# {"count":3,"total":600,"average":200}
```

ディレクトリ内のJSONファイルをまとめて集計します。

```sh
./noe run examples/collect-json.js examples/data/numbers
# ファイルごとの結果と、count: 5、total: 800、average: 160を表示
```

非同期の待機を使い、1秒間隔で3回表示します。

```sh
./noe run examples/sleep-demo.js
```

ファイル加工・レポート保存・HTTPダウンロードなどは[実行例の一覧](examples/README.md)を参照してください。

## HTTPサーバー

小さなHTTPサーバーもJavaScriptで書けます。まず起動します。

```sh
./noe run examples/http-server.js 8080
```

別の端末から試します。

```sh
curl http://127.0.0.1:8080/health
curl -H 'Content-Type: application/json' \
  -d '{"message":"こんにちは"}' http://127.0.0.1:8080/echo
```

起動した端末のCtrl+Cで終了します。デモのポートは省略時8080、0なら空きポートで、起動時に実際のURLを表示します。

`noe.serve(options, handler)`はサーバーハンドルを返します。ハンドラーは応答オブジェクトまたはそのPromiseを返せるため、ファイル読取りや`noe.sleep()`も使えます。契約は[APIリファレンス](docs/api.md#httpサーバー)を参照してください。

## CLI

| コマンド | 動作 |
| --- | --- |
| `noe run <file> [args...]` | ファイルを実行。引数は`noe.args`へ渡す |
| `noe eval <code>` | コードを実行。`noe.args`は空配列 |
| `noe repl` | 対話的にJavaScriptを実行 |
| `noe --help` | 使い方を表示。引数なしでも表示 |
| `noe --version` | バージョンを表示 |

`.js`と`.mjs`をESモジュールとして実行し、Promise・`async/await`・top-level awaitを使えます。ローカルimportはimport元のファイルが基準で、拡張子を明示します。ファイル操作と`eval`内の相対importはカレントディレクトリが基準です。

### 実行時間の上限

`run`・`eval`には任意の上限を指定できます。指定しなければ上限を設けません。

```sh
./noe run --timeout 5s examples/hello.js
./noe eval --timeout=500ms 'await noe.sleep(60000)'
# 終了コード124、標準エラーへ時間切れの診断
```

`--timeout`はスクリプト名・コードの前に指定し、`500ms`・`5s`・`1m`などGoのduration形式の正の値を使います。空・不正・ゼロ・負・重複は使い方エラーです。`--`でオプションを終端できます。スクリプト名の後の引数は、そのまま`noe.args`へ渡します。

エントリソースの読込み後、ランタイム初期化からJS評価・非同期I/O・sleep・サーバー待受・後続JSまでを計測します。同期OS syscallやローカルimportのOS読取りを強制停止する保証はありません。REPLはCtrl+Cで中断します。中断までの出力やファイルの変更は残ります。

## REPL

同じランタイムを使い続けるため、変数・関数は次の入力でも使えます。

```text
> let count = 1
> count += 1
2
> _ + 1
3
> await noe.sleep(100)
> noe.exit()
```

行編集、保存される履歴、Tab補完、複数行入力に対応します。`_`は最後に成功したundefined以外の結果です。通常の入力エラーの後も続行します。

空の入力でCtrl+D、または`noe.exit()`で終了します。対応する端末でCtrl+Cを押すと入力や実行中の処理を中断し、次のプロンプトへ戻ります。REPLで起動したサーバーは稼働中にプロンプトへ進まず、Ctrl+Cで停止します。

REPLの`await`は式、単一変数の初期化・代入に対応します。任意の複数文に含まれる`await`や静的importは提供せず、モジュールは`await import(...)`で読み込みます。操作と対応範囲は[REPLガイド](docs/repl.md)を参照してください。

## API

| 用途 | API |
| --- | --- |
| 出力 | `console.log`・`console.warn`・`console.error` |
| 引数・環境・終了 | `noe.args`・`noe.env.get`・`noe.cwd`・`noe.exit` |
| 標準入力 | `noe.readStdin`（run/evalのみ） |
| テキスト・バイナリ | `noe.readTextFile`・`noe.writeTextFile`・`noe.readFile`・`noe.writeFile` |
| ディレクトリ・ファイル情報・移動 | `noe.readDir`・`noe.stat`・`noe.mkdir`・`noe.rename`・`noe.remove` |
| HTTP | `fetch`・`noe.serve` |
| 待機 | `noe.sleep` |

引数、戻り値、エラー、対応範囲は[APIリファレンス](docs/api.md)にまとめています。

通常終了では、開始したI/O・sleepと後続のJS処理を待ちます。サーバーが稼働中なら実行を継続します。`noe.exit()`は停止を優先し、後続のJSや`catch`・`finally`を実行しません。

## HTTPサブセット

グローバルの`fetch(url, options)`はGET・POST・PUT・PATCH・DELETEに対応します。レスポンスは`status`・`ok`と、繰り返し読める`text()`・`json()`・`bytes()`を提供します。JSONの文字列化とContent-Typeは明示します。4xx・5xxはレスポンス、通信失敗はPromiseのrejectになります。

HTTP bodyは全体をメモリへ保持します。ストリーミング、`Request`・`Response`・`Headers`クラス、URLからのimport、WebSocket、`setTimeout`・`setInterval`は提供しません。

[HTTP APIの詳細](docs/api.md#httpサブセット) · [JSON APIへのリクエスト例](examples/README.md#json-apiへのリクエスト)

## エラーと検証

| run/evalの終了コード | 意味 |
| --- | --- |
| `0` | 成功 |
| `1` | 実行失敗・未処理のPromise rejection・SIGINTによる中断 |
| `2` | CLIの使い方の誤り |
| `124` | `--timeout`による時間切れ |

`noe.exit(code)`で指定した0〜255のコードはそのまま返します。エンジンが提供するエラー位置とスタックを表示します。解決する処理が残っていない未完了のtop-level awaitも診断します。

REPLは回復可能な入力エラーの後も続行し、EOFで0を返します。入力読取り・エンジン内部の続行できない失敗は1、非端末や入力待ちでの外部SIGINTは130です。

```sh
go test ./...
go test -tags=e2e ./... -count=1
```

E2EはCGOを無効にしてCLIをビルドし、別プロセスから実行します。一時ファイルとローカルHTTPサーバーを使い、LinuxではPTYによるREPLの操作も確認します。外部サービスには接続しません。

設計、インクリメントごとの受入条件・検証記録、開発用コマンドは[開発記録](docs/README.md)を参照してください。
