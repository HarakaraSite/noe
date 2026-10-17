# qjs固定版のREPL入力をNOEへ統合

## 採用した構成

go-quickjs v0.25.3の`cmd/qjs`から、入力・行編集・端末制御のソースを`internal/replui`へ取り込む。上流のREPLは`package main`の非公開実装であり、公開パッケージとしてimportできない。今回は上流の改修・採用を前提にせず、必要部分のスナップショットを保守する方針を選んだ。

NOEの`repl`コマンドからこの入力UIを使う。別のqjsプロセスは起動せず、エンジン全体もforkしない。取り込み元、ローカル差分、MITライセンスは[`internal/replui`](../internal/replui/README.md)に記録している。

| 責務 | 実装 |
| --- | --- |
| キー入力、カーソル編集、折り返し、履歴呼び出し、候補の表示 | qjs由来の`internal/replui` |
| Ctrl+R/S検索、複数行履歴の可視表示、UTF-8補完境界、中断キーの維持 | スナップショットへのNOE側の変更 |
| JSONL履歴ファイル、補完要求の配送、継続プロンプト | 既存CLI |
| JS評価、await、宣言と前回値の保持、補完候補の取得 | 既存`runtime.Session` |
| `noe:agent`登録、認証、broker、通信と子処理の回収 | 既存runtime/agentホスト |

入力goroutineはQuickJSへアクセスしない。Tab補完は要求をownerのselectへ送り、ownerが`Session.Complete`を実行する。評価中は端末を通常モードへ戻し、SIGINTを既存の入力単位のcontext取消しへ接続する。端末待ち中の外部SIGINTや補完からの終了でも、CLI終了側から端末を復元できるようraw/restore/Closeを同期した。Windowsで出力モードを変更した場合もCloseで元へ戻す。

`liner`と、そのためだけに使っていた`go-runewidth`・`uax29`へのルートmoduleの依存を削除した。過去の比較スパイクは当時の記録として残す。

## 更新手順

エンジンのバージョンアップ時にもNOE側の改修が必要になる可能性はある。公開パッケージの利用なら不要になる、とは扱わない。

1. go-quickjsの更新内容とNOEが使うruntime/stdlib APIの互換性を確認し、依存バージョンを更新する。
2. 新しい版の`cmd/qjs/lineedit.go`、`terminal*.go`、`lineedit_test.go`を、現在の取り込み元v0.25.3と比較する。
3. 不具合修正や必要な改善を選んで取り込む。NOE側の履歴・検索・Unicode・中断・端末復元の変更を保つ。モジュールキャッシュ内のソースは変更しない。
4. 新しいソースから取り込んだ場合は`internal/replui/README.md`とprovenanceコメントを更新し、ライセンスも照合する。エンジンだけを更新した場合、スナップショットの取り込み元表記は元の版のまま残す。
5. 入力UIのunit/race、PTYの編集・履歴・検索・複数行・補完・中断と端末復元、`noe:agent`のREPL利用、全体E2Eと配布対象ビルドを確認する。

`go.mod`の更新だけでは、取り込んだ入力UIは更新されない。上流のすべてのREPL変更を移す必要もない。NOEが使う部分と修正の必要性に沿って判断する。

## 検証

通常のテストは実プロバイダへ接続せず、agent用のローカルHTTP fixtureを使う。実APIキーの読込みや課金は伴わない。

- 既存のPTYテストで編集、日本語、履歴保存と再実行、複数行、補完、Ctrl+C、EOF、終了コード、端末設定の復元を確認する。
- qjs固有の接続確認として、Ctrl+Rの一致選択と不一致時の非実行、複数行のbracketed paste、Esc/CSI/SS3途中のCtrl+Cを追加した。
- agentの既存PTYテストでは`a.r<Tab>`による補完から`run()`を実行し、複数入力での再利用、provider error後の継続、通信と補完で始まるI/Oの取消しも確認する。
- 入力UIのunitは上流由来の編集テストに加え、Unicode候補の共通部分、カーソル後方のソース維持、元の複数行ソースの保持、検索と読み取りエラーを確認する。
- Linuxのunit/raceで、入力待ち中のCloseによる端末復元と、その後にrawモードへ戻らないことを確認する。

独立レビューで見つかったEsc後の中断キーの取りこぼし、不一致検索による直前の一致コマンドの再実行は修正し、上記の回帰確認を追加した。

2026-10-16、Linux amd64 / Go 1.27.2で次を確認した。

| 確認 | 結果 |
| --- | --- |
| `go test ./...` | 成功 |
| `go test -tags=e2e ./... -count=1` | 修正後の全体unit/E2E成功 |
| `CGO_ENABLED=1 go test -race ./internal/... -count=1` | 成功。レビュー修正後は変更対象の`./internal/replui ./internal/cli`を再実行して成功 |
| `go vet -tags=e2e ./...` | 成功 |
| `CGO_ENABLED=0 go build -o noe ./cmd/noe` | 成功。ローカルの`./noe repl`から利用可能 |
| Linux・Darwin・Windows × amd64・arm64 | 修正後の6対象すべてCGO無効でビルド成功 |
| 40列PTYの画面とカーソル | pyte 0.8.2 / wcwidth 0.2.13でASCII・日本語、折り返し、途中編集の6ケース成功 |
| 独立レビュー | 2件の指摘を修正し、対象を絞った再レビューで両方の解消を確認。追加の対応必須指摘なし |

実行確認はLinux amd64に限る。他OS・arm64はビルド確認であり、実端末の動作を保証する結果ではない。画面確認は指定した文字列とPTYモデルを対象とし、すべての結合文字・絵文字・IME・端末固有の幅や折り返しを確認したものではない。Esc単体の待機タイムアウトは今回追加していない。
