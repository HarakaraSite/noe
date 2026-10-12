# NOE インクリメント6 ファイル操作の拡充

インクリメント5の集計から、結果の保存・処理済み入力の移動までNOEで完結できるようにする。Go 1.27.2と既存go-quickjsを使い、新しい依存は追加しない。状態は実装・テスト・レビュー・総合E2E完了。

## APIの契約

run/eval/REPLに次のAPIを提供する。すべて`Promise<void>`で成功はundefined、OSエラーはreject。相対パスは起動時のcwdが基準で、絶対パスと日本語名も扱う。

| API | 動作 |
| --- | --- |
| `noe.mkdir(path)` | 親も含めてディレクトリを作成する。既存ディレクトリは成功。新規ディレクトリの権限は0755からOSのumaskを適用。途中や対象が通常ファイルならreject |
| `noe.rename(from, to)` | ファイル・ディレクトリ・リンク自身をOSの名前変更で移動する。移動先の親は事前に用意する。Linuxでは移動先の通常ファイルを置換する。ファイルシステムをまたぐ移動はコピーで補わずOSエラーを返す。リンク先は移動しない |
| `noe.remove(path)` | ファイル、シンボリックリンク自身、空ディレクトリを削除する。存在しないパスと空でないディレクトリはreject。再帰削除は提供しない。リンク先は削除しない |

Goのos.MkdirAll、os.Rename、os.Removeの動作に従う。renameの置換可否などのOS差異は、そのOSのエラーとして扱う。通常終了は開始した操作と後続のJS処理を待つ。依存する操作はawaitで順序を指定する。中断・明示終了で実行済みの変更を巻き戻さず、実行中のOS syscallを強制停止する保証も追加しない。

## スライスと受入条件

| スライス | 範囲 | 完了条件 |
| --- | --- | --- |
| 1 | 3つのAPIとランタイムテスト | mkdirの親作成・既存・ファイル衝突、renameの内容保持・置換・ディレクトリ・リンク・失敗、removeのファイル・空ディレクトリ・リンク・存在しないパス・空でないディレクトリを確認。Promise<void>、相対・絶対・日本語、未awaitの完了待ち、失敗後のREPL回復、開始前キャンセルを確認 |
| 2 | 実用例・README・実バイナリE2E | 数値配列JSONを集計し、出力ディレクトリを作り、一時ファイルからレポートへrenameし、入力を処理済みディレクトリへ移す。removeで作業用ファイルを削除する。stdoutは集計JSON、診断はstderr。実バイナリのAPI成功・失敗とREPL続行を確認 |

計画および各スライスを通常／批判的レビューする。削除対象・rename置換の契約、所有goroutine、Promise<void>、キャンセル時の変更の説明、例で使う実際のパスを独立確認する。初回15分、再レビューは変更箇所・既存findingに限って一回15分以内。findingの採否・最終検証は親agentが担当する。

## 実装と検証

既存AsyncWorkを使い、パス解決・JS操作はランタイム所有goroutine、OS操作はworkerで行う。操作の前後でcontextを確認する。voidはJS側のasync wrapperで統一し、nilをnullとして公開しない。既存readDir/stat/read/writeやstdin/REPLの動作を変えない。

例は`examples/process-json.js <input-directory> <output-directory>`。直下の通常`.json`の数値配列を名前順で読み、`{files,count,total,average}`を集計する。すべて解析してから、出力ディレクトリとその下の`processed`を作る。report.json.tmpを書いてreport.jsonへrenameし、入力をprocessedへ移す。出力ディレクトリの`.processing`を作業markerとしてwriteし、正常完了時にremoveして、最後に集計JSONをstdoutへ出す。既存の`.processing`も上書きし、正常完了時に削除する。report.json.tmpも上書きする。入力は移動するため、READMEのデモではmktempで作った作業ディレクトリへfixtureをコピーして使う。入力と出力は別ディレクトリを指定する。

この例は複数ファイル操作をまとめたトランザクションではない。失敗までに完了した保存・移動は残り、同名のレポート・処理済みファイルはLinuxの通常のrenameで置換する。出力途中で失敗した場合の自動削除やロールバックは実装しない。

テストはすべて一時ディレクトリで行い、repositoryのfixtureを移動・削除しない。各スライスの対象テスト、CGO無効ビルド、runtime/CLIのrace検査、go vet、最後に`go test -tags=e2e ./... -count=1 -v`を実行する。直下のnoeを最新ソースでビルドする。

## 対象外

TypeScript、名称変更、npm・Node/Deno互換、Web全面互換、再帰削除、コピー、権限変更、パスユーティリティ、監視、外部コマンド、HTTP拡充、実行時間上限は含めない。

## 実装・レビュー記録

| 段階 | 結果 |
| --- | --- |
| 計画 | 通常／批判的レビューでP2指摘1件。作業markerの実パスと既存同名時の扱いが未確定だったため、`.processing`を上書きし正常完了時に削除すること、report.json.tmpを上書きすることを明記。解消確認済み |
| スライス1 | 3 APIとvoid wrapperを実装。親作成・既存ディレクトリ・ファイル衝突、内容保持・既存ファイル置換・ディレクトリ移動・リンク自身の移動と削除・リンク先保持・dangling link・非再帰削除・各OSエラーを確認。相対・絶対・日本語、undefined成功、REPL回復、未await完了待ち、開始前キャンセルも成功。通常／批判的レビューで必須指摘なし |
| スライス2 | process-json.js・README・実バイナリE2Eを追加。レポート保存と置換、入力移動と置換、対象外入力の保持、作業ファイル除去、空入力・不正データ・引数不足・REPL回復を確認。通常／批判的レビューで必須指摘なし |
| 途中失敗 | 2件目の移動先を既存ディレクトリにしてrenameを失敗させ、保存済みレポート、1件目の移動、2件目の入力、markerと既存移動先が残ること、stdout空・stderr診断・終了コード1を確認。追加テストの限定再レビューで指摘なし |
| 総合E2E | 全スライスのレビュー後、既存のCLI/API/REPL/端末操作を含む全E2Eが成功 |

検証環境はGo 1.27.2、Linux amd64。担当範囲と最終検証の結果は以下のとおり。

```sh
go test ./internal/runtime -count=1
CGO_ENABLED=0 go build ./...
go test -tags=e2e ./tests -run '^Test(FileMutation|ProcessJSON)' -count=1 -v
CGO_ENABLED=1 go test -race ./internal/... -count=1
go test -tags=e2e ./... -count=1 -v
go vet -tags=e2e ./...
CGO_ENABLED=0 go build -o noe ./cmd/noe
git diff --check
```

追加の実バイナリE2Eは一時Go overlayでTestMainをraceビルドへ切り替えても成功し、raceを検出しなかった（途中失敗テスト追加前の4テスト）。通常の総合E2Eと直下のnoeはCGO無効でビルドする。overlayとログは作業用一時ファイルに置き、repositoryへ追加しない。

READMEのデモと同じfixtureの一時コピーで、5件・合計800・平均160、report.jsonの保存、processedへの2ファイル移動、作業用ファイル除去を確認した。repositoryのfixtureは移動・削除していない。実装上の未解消指摘はない。

## 参照

- [製品方針・MVP](mvp.md)
- [インクリメント5](increment-5.md)
- [現在のAPI](../README.md#api)
