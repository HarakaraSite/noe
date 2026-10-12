# NOE インクリメント5 実用スクリプト向け入出力

インクリメント5では、標準入力のテキスト読み取り、ディレクトリ列挙、ファイル情報取得を追加する。JSONをパイプで渡して加工したり、指定ディレクトリのJSONをまとめて集計したりできる範囲に絞る。前提は[インクリメント4](increment-4.md)の完了。状態は実装・テスト・レビュー・総合E2E完了。

## APIと操作の契約

| API | 動作 |
| --- | --- |
| `noe.readStdin()` | run/evalで提供。最初の呼出しで標準入力をEOFまで読み、`Promise<string>`を返す。改行を保持し、空入力は空文字。以降の呼出しは同じPromiseを返すので、並行呼出しで入力を取り合わない |
| `noe.readDir(path)` | `Promise<Array<{name, isFile, isDirectory, isSymlink}>>`。指定ディレクトリ直下の名前を名前順で返す。nameはベース名、各種別はboolean。再帰しない。リンク自身はisSymlinkで示し、リンク先を調べない。空ディレクトリは空配列 |
| `noe.stat(path)` | `Promise<{size, modifiedAt, isFile, isDirectory}>`。sizeはバイト数、modifiedAtはUTCのRFC3339形式の文字列（小数秒を含み得る）。種別はboolean。シンボリックリンクはリンク先を調べる。存在しないパスなどのOSエラーはreject |

readDir/statは既存のファイルAPIと同じく、相対パスを起動時のカレントディレクトリから解決する。絶対パスと日本語のファイル名も扱う。入力失敗はrejectし、未処理の失敗は通常の診断・終了コード1となる。

`noe.readStdin`はREPLに追加しない。REPLのstdinはJS入力として使い、既存のパイプREPLも維持する。run/evalのコードは従来どおりファイル・CLI引数から与え、標準入力はそのスクリプトのデータとして使う。`noe run -`などstdinからのコード読み込みは今回追加しない。

標準入力は借用したReaderであり、ランタイムが勝手に閉じない。CLIのキャンセル・明示終了は待機中のstdin読み取りがあっても戻る。既存AsyncWorkのキャンセルと遅延completion抑止を使い、任意ReaderのRead syscallを強制停止する保証へは広げない。読取りが後で終わっても、閉じたランタイムやキャンセル済み入力へJSの値を戻さない。通常終了は開始した読取りを待つ。

## スライスと受入条件

| スライス | 実装 | 完了条件 |
| --- | --- | --- |
| 1 | Runtime Optionsへstdinを渡し、run/evalのreadStdinを接続 | CLIへ渡したJSONを読む。改行・空入力・改行なし最終文字を保持する。同じPromiseを返して同時呼出しでも内容が変わらない。読取りエラー、未awaitの完了待ち、キャンセル・明示終了を確認。REPLへ公開せず既存入力を消費しない |
| 2 | readDir/statを既存AsyncWorkへ接続 | 名前順の直下一覧、各種別、空ディレクトリ、日本語、相対・絶対パス、バイト数、更新日時、リンクの区別を確認。OSエラーはrejectし、REPLでもawaitして続行できる |
| 3 | 例・README・総合E2E | パイプJSON加工と、引数のディレクトリ内JSONの集計を実バイナリで行う。標準出力だけで次のコマンドへ渡せる。診断は標準エラー。既存CLI/API/REPLの全E2Eが成功する |

各スライスで実装・目的に対応するテスト・レビューを行い、必須指摘を解消する。stdinのPromise共有とキャンセルの所有を独立レビューに回し、readDir/statと実用例・E2Eは対象契約に限定して確認する。レビュー初回は15分、再確認は一回15分以内を基本とする。

## 実装方針と検証

Go 1.27.2、Linux amd64、go-quickjs v0.25.3と既存stdlibサブセットを維持する。新しい依存は追加しない。JSの値・Promiseを操作するのはランタイム所有goroutineだけとし、Go側の読取りとメタデータはworkerで行って既存AsyncWorkから返す。

スライスの区切りでGoテストとCGO無効のビルドを行う。stdinの並行呼出し・キャンセルにCLI/runtimeのrace検査を行う。データと履歴は一時ディレクトリを使い、外部サービスを使わない。最後に全スライスレビュー後の`go test -tags=e2e ./... -count=1 -v`を実行し、直下の`noe`を最新ソースでビルドする。

例は`examples/summarize-stdin.js`と`examples/collect-json.js`に置く。前者は数値配列をJSONで読み、件数・合計・平均をJSONで出力する。後者は引数ディレクトリ直下の通常の`.json`ファイル（各ファイルは数値配列）を読み、ファイル名・バイト数・件数・合計と全体集計をJSONで出力する。ログの追加でJSON出力を汚さない。

## 対象外

名称変更、TypeScript、npm・Node/Deno互換、Web全面互換、バイナリ・ストリームAPI、再帰列挙・glob、監視、書込み・削除APIの追加、外部コマンド、sleep、実行時間上限のCLI追加、REPL補助コマンドは今回含めない。

## 実装・レビュー記録

| スライス | 結果 |
| --- | --- |
| 1 | stdinをCLIからRuntime Optionsへ渡し、共有Promiseで読む。テキスト保持、空入力、nil Reader、読取りエラーと同じ失敗の共有、未awaitの完了待ち、実際にブロックしたReaderのキャンセル・明示終了、借用Readerを閉じないこと、REPLで非公開を確認。通常／批判的レビューで必須指摘なし |
| 2 | readDir/statを実装。通常のSession経由で名前順・直下一覧・空配列、日本語・相対・絶対パス、リンク自身とリンク先の違い、バイト数・UTC日時・OSエラーreject・REPL回復を確認。通常／批判的レビューで必須指摘なし |
| 3 | 両サンプル・fixture・README・実バイナリE2Eを追加。JSON出力を次のevalへ渡す処理、並行集計、空データ・引数不足・不正データ、開いたstdinでのSIGINTとexit、メタデータとパイプREPLを確認。通常／批判的レビューで必須指摘なし |

最初の総合E2Eで、既存Tab補完テストが`noe.re`からreadTextFileへ一意に補完される前提に依存していたことが判明した。readDir追加後は候補が複数になるため、テストの入力を`noe.readT`に更新し、新APIの`readD`・`st`の補完から実際にAPIを呼ぶ確認も追加した。補完の実装は変更していない。対象テストの成功と変更箇所の再レビュー後、総合E2Eを再実行して成功した。

最終検証環境はGo 1.27.2、Linux amd64。以下を確認した。

```sh
go test ./... -count=1
CGO_ENABLED=1 go test -race ./internal/... -count=1
go test -tags=e2e ./... -count=1 -v
go vet -tags=e2e ./...
CGO_ENABLED=0 go build -o noe ./cmd/noe
git diff --check
```

stdinの実バイナリE2Eは、一時的なGo overlayでTestMainのCLIビルドを`go build -race`・CGO有効に置き換えて実行し、race検出なしで成功した。通常の全E2Eと手元の`noe`はCGO無効でビルドする。overlayと検証ログは作業用の一時ファイルであり、repositoryへ追加しない。

任意ReaderのブロックしたReadそのものを強制停止する保証は追加していない。CLI・ランタイムの終了後にEOFや呼出し元のCloseで読み取りが終わっても、JSへ結果を戻さない。実装上の未解消指摘はない。

## 参照

- [MVP案](mvp.md)
- [インクリメント4](increment-4.md)
- [現在のAPI](../README.md#api)
