# REPL削除の実装・検証記録

2026-10-19。[削除計画](repl-removal-plan.md)に基づき、実装、対象テスト、独立コードレビュー、その後の総合テストを完了しました。対象は`2d402c3`からの未コミット差分です。

## 実装

REPLコマンド、入力UI、履歴、補完、入力継続判定、await変換、`runtime.Session`と入力間の回復処理を撤去しました。通常の起動にも入っていた補完・inspect・cancel probe、timer/Worker/portのREPL判定用wrapperも外しました。

native I/OはHostのREPL向け集計counterを除去し、実際のAsyncWork・resource registry・WaitGroup・Closeによる所有と回収を維持しました。signal購読の`active`もこのcounterだけの補助flagだったため除去し、購読のclosed guard、broker解除とAsyncWork.Doneは残しました。テストは削除したcounterではなく実runtimeのBusy・物理I/Oの終了・resource解除を確認します。

Session経由でのみ確認していたcatalogの設定snapshot、canonical module cache、画像のroot制約と未対応generation入力は、通常の`Execute`へ移しました。入力間の回復だけを確認するテストは撤去し、未参照Worker/portの実行終了時の所有回収は通常実行で確認します。

CLIは`noe repl`を他の未対応コマンドと同じ終了コード2で拒否します。引数なしhelp、run/eval、docs、agent制御、標準入力と通常のSIGINT取消しは維持します。旧JSONLのrepl.result/repl.errorの閲覧は残します。

REPL専用の現行ガイド・例・端末テストを整理し、qjs入力UIの統合記録はarchiveへ移しました。削除source・旧ガイドの履歴参照はv1.8.0のtagへ向け、当時の検証記録を保持します。内蔵referenceは英文を維持します。

## コードレビュー

独立reviewerがCLI撤去、共有runtimeのawait・interrupt・Close、native I/Oの所有と回収、移行したテスト契約、旧JSONL reader、現行docsとarchiveのリンクを確認しました。P1/P2およびコード上の必須修正はありませんでした。完了メモの古い「計画のみ」表記をP3として受け、検証完了後に更新しました。

総合unitの初回実行では、既存のprocessテスト補助関数が終了途中の`/proc/<pid>/stat`読取りで`ESRCH`を受けて失敗しました。`linuxProcessAlive`で既存の`ENOENT`に加えて`ESRCH`も消滅済みとして扱う最小修正を行いました。製品のprocess処理は変更していません。関連する所有者SIGKILL・起動中取消し・supervisor失敗の3テストを各10回実行して成功し、差分に限定した一回の再レビューでも追加指摘はありませんでした。

## 検証

Linux amd64 / Go 1.27.2で実施しました。機械可読な結果は[repl-removal-results.json](repl-removal-results.json)、実行ログ・検証用script・各buildは`/tmp/noe-repl-removal-20261019-yzglhqqf/`にあります。

| 検証 | 結果 |
| --- | --- |
| 変更対象のruntime・nativeio・CLI単体テスト、apidocs・modulemapテスト、重点E2E | 成功 |
| `CGO_ENABLED=0 go test ./... -count=1` | 成功、39.432秒（上記test helper修正後） |
| `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` | 成功、39.608秒 |
| `CGO_ENABLED=0 go vet ./...` / `go vet -tags=e2e ./...` | 両方成功 |
| `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1` | 成功、59.615秒 |
| MCP SDK 1.32.1 bundleを指定した`TestImportMapMCPProcessIO` | 成功。main・Worker・fixture agentのrun_js、失敗時の回収を確認 |
| CGO不要のLinux/Darwin/Windows × amd64/arm64ビルド | 全6対象成功 |
| Linux amd64のビルド済みCLIによるsmoke | 基本11件、docs・低水準I/O 9件、REPL撤去・取消し等9件に成功 |
| ビルド済みCLIとローカルprovider fixture | agent→run_js、instruction合成、context状態、4種類のhook、終了後counterを確認。ローカル要求2回、hook計6回で成功 |

実CLIでは引数なしhelp・`--help`からREPLが消え、`noe repl`が入力を実行せず終了コード2を返すこと、`NOE_REPL_HISTORY`に指定した既存ファイルが変化しないことを確認しました。run/evalのstdinとtop-level await、import map、未設定認証でのlazy import、子process、Worker docs、TCP/Unixの位置指定I/O、signal購読と配送、timeout 124・SIGINT 1、旧REPLイベントの保存ログ表示も確認しています。作業ディレクトリの`./noe`を検証済みLinux amd64バイナリへ更新しました。

MCP bundleは既存npmキャッシュだけから作成し、実行時はNOEとローカルfixtureを使いました。他のOS・architectureはcross-buildの確認であり、実機動作の確認ではありません。

現行source内のREPL参照は、未対応コマンドのテストと旧JSONL reader・そのテストだけです。現行ガイドに新しいリンク切れはありません。全文書のリンク走査では、今回変更していない画像検証記録の既存`/tmp`画像4件のみ参照先が存在しませんでした。履歴と独立spikeは維持しています。`git diff --check`とGoのformat確認も成功しました。

v1.8.0のタグ・配布物、version値は変更していません。今回の作業ではコミット・公開・実プロバイダ要求は行っていません。
