# NOE MVP案

NOE（No Opulence Engine）は、小さなJavaScriptスクリプトを実行するためのGo製ランタイム。Denoからの移行先として、自分に必要な処理を手軽に実行できる環境を作る。実装を通じてランタイムの仕組みを学ぶことも目的とする。

最初のMVPは、ローカルのJavaScriptでファイル処理とHTTP APIの取得ができる範囲とする。TypeScript対応とREPLはMVP後に実装する。

実装計画は[インクリメント1](increment-1.md)にまとめる。

No bloat. Just scripts.

## 製品方針

- Goで実装し、JavaScriptエンジンにはgo-quickjsを使う。
- txiki.jsのCLIや基本機能を参考にする。
- 実際に使う機能を優先し、過剰な機能を増やさない。
- npm対応、Node互換、Deno互換は実装しない。MVP後の対応予定にも含めない。
- Web標準への全面的な互換性は、当初は目指さない。必要なAPIのサブセットを実装し、対応範囲を明記する。

## MVPの機能範囲

| 項目 | 範囲 |
| --- | --- |
| CLI | `noe run script.js [args...]`、`noe eval 'コード'`、help、version |
| JavaScript実行 | go-quickjsで実行し、言語機能はエンジンに任せる |
| モジュール | 拡張子を明示したローカルの相対`import` |
| コンソール | `console.log`、`console.warn`、`console.error` |
| 実行環境 | スクリプト引数、環境変数、カレントディレクトリ、終了コード |
| ファイル | テキストファイルの読み書き |
| HTTP | GET・POST、レスポンスのテキスト・JSON取得に必要なサブセット |
| 非同期 | Promise、`async/await`、top-level await、開始したI/Oの完了待ち |
| エラー | エラー位置とスタックの表示、失敗時の非ゼロ終了コード |

実行環境とファイル操作のAPI名・公開方法、HTTP APIの詳細は、[インクリメント1](increment-1.md)と[README](../README.md)に記載する。NodeやDenoのAPIへの互換性を前提にしない。`console`やHTTP APIも、対応を明記した範囲を提供する。

## MVPの受入条件

次の処理をNOE単体で実行できることを、MVPの完成条件とする。

1. `run`でJSファイルを実行し、`eval`で渡したコードを評価できる。helpとversionを表示できる。
2. 引数で指定したJSONファイルを読み、加工して別のファイルに保存できる。
3. HTTP APIにGET・POSTを送り、テキスト・JSONのレスポンスを取得してファイルに保存できる。
4. 別のローカルJSモジュールを相対`import`し、top-level awaitを使って非同期処理を実行できる。開始したI/Oの完了前に終了しない。
5. スクリプトから引数、環境変数、カレントディレクトリを参照し、終了コードを指定できる。通常のログと警告・エラーを標準出力・標準エラーに出せる。
6. 構文エラーや未処理の実行エラーを診断でき、失敗した実行が非ゼロ終了コードになる。

## MVPに含めない機能

- TypeScript実行・型チェック
- REPL
- URLからのモジュール読み込み、import map、パッケージ管理
- 外部コマンド実行
- HTTPサーバー、WebSocket、直接のTCP・UDP操作
- Workers、FFI、DB
- バンドル、スクリプトからの実行ファイル生成
- 組み込みのテストランナー

これらはMVPの対象外であり、すべてを将来実装するという意味ではない。npm対応とNode/Deno互換は、製品方針として実装しない。

## MVP後に実装したい機能

### REPL

最初は、入力したJSの評価、結果表示、変数の保持、エラー後の続行、終了操作を提供する。この範囲は[インクリメント2](increment-2.md)で実装した。

[インクリメント3](increment-3.md)でGoの行編集専用ライブラリを導入し、基本の編集操作とセッション内履歴を提供した。[インクリメント4](increment-4.md)で履歴保存・前回結果・JS補完・複数行入力・中断後の続行を追加した。

### TypeScript

JSへの変換を経て実行する方式を検討する。ソースの読み込みとエンジンでの実行を分け、後から変換処理を追加できる構成にする。変換ライブラリや対応範囲は、TypeScript対応の段階で決める。

READMEのJavaScript/TypeScriptという説明は製品の方向性を示す。MVP公開時には、現在の対応言語がJavaScriptのみであることを明記する。

### 実用スクリプト向けの入出力

[インクリメント5](increment-5.md)で、run/evalの標準入力をテキストとして読む`noe.readStdin()`、ディレクトリ直下の一覧を取得する`noe.readDir(path)`、ファイル情報を取得する`noe.stat(path)`を追加した。JSONをパイプで受け取って加工し、指定ディレクトリのJSONを集計する例を提供する。REPLの標準入力はJS入力として使い続ける。

[インクリメント6](increment-6.md)で、親を含むディレクトリ作成の`noe.mkdir(path)`、名前変更・移動の`noe.rename(from, to)`、ファイル・リンク・空ディレクトリ削除の`noe.remove(path)`を追加した。集計結果の保存から処理済み入力の移動までを行う例を提供する。

### 実行時間の制御

[インクリメント7](increment-7.md)で、run/evalのスクリプト名・コードの前に指定する任意の`--timeout`を追加した。JS評価・開始した非同期I/O・後続JSの完了待ちを時間切れで終了し、コード124を返す。未指定時は上限を設けず、REPLの操作も維持する。

## 参照

- [txiki.js Getting Started](https://txikijs.org/docs/getting-started/)
- [txiki.js CLI Reference](https://txikijs.org/docs/cli/)
- [go-quickjs](https://github.com/go-quickjs/go-quickjs)

ローカルの参照資料は`_refs/`に置く。`_refs/`はGitの追跡対象に含めない。
