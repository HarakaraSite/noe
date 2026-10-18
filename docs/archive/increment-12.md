# NOE インクリメント12 stdlibへの統一

> **初期開発の履歴です。** 旧APIや当時の対象範囲・検証結果を含みます。現在の使い方は[利用ガイド一覧](../README.md)、記録の一覧は[archive](README.md)を参照してください。

[開発記録](../README.md) · [現在のAPI](../api.md) · [旧APIからの移行](../api.md#旧apiからの移行)

JavaScript APIの互換範囲を、固定したgo-quickjs v0.25.3の標準ライブラリに揃えた。重複するNOE独自実装を削除し、実行例・テスト・利用ガイドも標準ライブラリへ移行した。Go 1.27.2とCGO不要の通常ビルドを維持する。

## 公開API

JavaScriptの`noe`グローバルは提供しない。残る`noe.*` APIも、旧実装への互換エイリアスもない。

| 用途 | 現在のAPI |
| --- | --- |
| 引数・環境変数・cwd・終了 | `process.argv`・`process.env`・`process.cwd()`・`process.exit()` |
| 標準入力 | `process.stdin.read()` |
| ファイルとディレクトリ | `fs`・`fs/promises` |
| タイマー | グローバルの`setTimeout`・`setInterval`、`timers/promises` |
| HTTPクライアント・サーバー | グローバルの`fetch`・`serve`、Web Request/Response/Headers |
| その他のホスト機能 | `child_process`・`dns`・`os`・WebSocket・Workersなど |

全標準ライブラリを組み込み、ファイルシステム全体、プロセス環境、通信、HTTP待受、コマンド実行、Workersを実行ユーザーのOS権限で利用できるようにした。npm・パッケージ管理やNode.js/Denoの完全互換は提供しない。QuickJS本来の`import "std"`とも別のAPIである。

NOE固有で残る機能は、CLIの`run`・`eval`・`repl`・`--timeout`、ローカルESモジュールの読込み、REPLの行編集・履歴・補完・複数行入力・前回結果`_`など。これらのために`noe`名前空間を公開することはない。

## 実装の変更

- `internal/runtime/stdlib.go`で`stdlib.Install`と全ホスト機能の設定、Worker用のローカルモジュール読込みを行う。
- 独自のファイル・Fetch・HTTPサーバー・sleep・標準入力処理と、それらの非同期ソース管理を削除した。実行は`stdlib.Loop`とエンジンのジョブキューを使う。
- `internal/runtime/eventloop.go`、`stdlib_lifecycle.go`、`session.go`は、エラー検出、実行期限、REPLの中断とリソース終了を接続する。
- 補完中のProxy trapが処理を開始した場合は、タイマーを取り消し、終了できない新しいstdlib I/Oが残る場合はセッションを閉じる。元からある未参照リソースと、補完中に作成・終了したリソースは区別する。

## 移行時の動作差

以前のAPI名だけを置き換えると動かない箇所がある。引数と戻り値も[移行表](../api.md#旧apiからの移行)に従って変更する。

- Fetchの本文は一度だけ消費する。再利用する場合は読み取った値をスクリプト側で保持する。
- `serve`のハンドラーはWeb Requestを受け取り、Responseを返す。独自の`request.path`や`{body: ...}`応答は使わない。同期的な`server.close()`の完了待ちは[現行ガイド](../api.md#httpサーバー)を参照する。
- `process.stdin.read()`は同期的にEOFまで読み、取得済み・空入力では`null`を返す。REPLではstdinをソース入力用に予約する。旧APIのPromise共有や入力単位の非同期キャンセルは提供しない。
- `readdir`は名前の配列を返す。種別は`stat`または`lstat`の`isFile()`などで調べる。実行例のシンボリックリンク除外には`lstat`を使う。
- タイマーだけが残ったREPL入力は中断後に続行できる。stdlibのI/Oやメッセージング資源が残った状態で中断・入力エラーが発生した場合は、ランタイムを閉じてセッションを終了する。

## 検証記録

移行時のローカル変更に対し、Linux amd64・Go 1.27.2で次を確認した。過去のリリースの検証結果とは別の記録であり、新しい変更に対する継続的な合格を示すものではない。

| 確認 | 結果 |
| --- | --- |
| `CGO_ENABLED=0 go test -tags=e2e ./... -timeout 120s` | 成功。CLI、ファイル・HTTP・標準入力の実行例、REPLのPTY操作を含む |
| `CGO_ENABLED=0 go vet -tags=e2e ./...` | 成功 |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1 -timeout 120s` | 成功 |
| `CGO_ENABLED=0 go build -trimpath -o ./noe ./cmd/noe` | 成功 |
| 再ビルドしたCLIで`typeof noe`と`Object.hasOwn(globalThis, "noe")`を確認 | `"undefined"`と`false` |
| 標準ライブラリのファイル・タイマー・Fetch・serve・WebSocket・コマンド・Worker、REPLの補完・中断・終了 | テストと実行確認に成功 |

移行の実装・検証段階ではローカルの`./noe`を再ビルドした。これは既存の`v1.0.0`タグや配布済みReleaseを更新したという意味ではない。v1.1.0での配布内容は[リリースノート](../releases/v1.1.0.md)を参照する。リリース時の検証と配布は[リリース手順](../releases/release-checkpoints.md)に従う。
