# REPL削除計画

2026-10-19。v1.8.0公開後の計画です。計画作成後、利用者より実装・テスト・コードレビューと、その後の総合テストを依頼され、完了しました。結果は[実装・検証記録](repl-removal-implementation.md)を参照してください。

## 目的

日常の実行をファイル編集と`noe run`、短い確認を`noe eval`へ揃え、利用頻度の低いREPL専用実装を撤去します。コマンドを隠すだけでなく、入力UI・履歴・補完・await変換・入力間の状態保持と回復処理も削除します。

`run`・`eval`・Worker・agent・`run_js`の実行、標準入力、イベント観察、取消しと所有resourceの回収を維持します。Zed設定の追加は別候補です。新しい対話コマンド、stdinからのソース実行、汎用Session APIへの改名は行いません。

## 利用者から見た変更

- `noe repl`を削除します。削除後は通常の未対応コマンドとして終了コード2を返し、専用の互換aliasや起動stubは設けません。
- help・現行ガイド・examplesからREPLへの導線を除去します。代替は`noe run script.js`と`noe eval 'code'`です。引数なしの`noe`は従来どおりhelpを表示します。
- REPLだけの変数保持、前回値`_`、Tab補完、複数行prompt、入力履歴、`NOE_REPL_HISTORY`を廃止します。利用者の既存履歴ファイルを自動削除する処理は追加しません。
- CLIのSIGINTは全コマンドで通常のprocess context取消しへ揃えます。REPL用の「入力だけ中断して次promptへ戻る」分岐を除去します。
- 新規の`repl.result`・`repl.error`イベント生成を廃止します。既存JSONLに対する`observe`の表示対応は残し、保存済みログを読み続けられるようにします。

REPLを含むv1.8.0のタグと配布物は変更しません。次回releaseではCLIの削除と移行先を明記します。バージョン決定・コミット・公開は、この計画作成の対象外です。

## 調査で確認した境界

| 対象 | 現在の使われ方 | 方針 |
| --- | --- | --- |
| `internal/cli/repl*.go`・`internal/replui/` | REPLコマンド・入力・端末・履歴・補完 | 専用テストと合わせて削除 |
| `runtime.Session`・`Evaluation`・`TerminationError` | productionではREPL専用。ほかの参照はテスト | APIを残さず削除し、混在テストを整理 |
| `async_input.go`・`syntax.go`・`completion*.go` | REPLのawait変換、入力継続判定、字句名・property補完 | 専用テストと合わせて削除 |
| hostの`inspect`・`completion`・`cancelProbe` | Sessionの値表示と回復・補完だけに利用 | fieldと起動時のJS helper生成を削除 |
| timer/resource追跡wrapper | Session中断後の再利用可否、補完副作用の検出 | REPL専用の追跡とwrapperを削除 |
| `eventLoop.await/drain/interrupt`・`loopFailure` | run/evalのESM・Promise・未処理エラーにも利用 | 実行・診断・取消しの本体を維持 |
| `stdlib.Loop`・`nativeio`・agent Operation | I/O・Worker・agent・子processの所有関係と回収 | runtime終了・context取消しの契約を維持 |
| PTY入力helper・端末テスト | agent/sleep/serveを含めてREPLを起動するテストのみ | helperごと削除。run/eval側の取消しテストは残す |
| `golang.org/x/term` | REPLに加え、`observe_render.go`の端末幅取得 | 依存を維持 |
| `golang.org/x/sys` | host I/O・認証lock・process group管理などにも利用 | 依存を維持 |

ファイル名だけで共有処理と判断しません。`stdlib_lifecycle.go`は現在`stopTimers`とREPL用resource追跡を提供し、利用先はSession・completionです。反対に、stdlib自身のkeepalive・native I/O・Worker終了処理は別の経路であり、削除対象に含めません。

## 実装範囲

### S1：CLIと入力UIの撤去

`internal/cli/cli.go`のusageと`repl`分岐、`internal/cli/repl*.go`、`internal/replui/`を削除します。`cmd/noe/main.go`は`signal.NotifyContext`のREPL例外分岐を除去します。

CLIのimport-map・events・observeテストはREPL caseだけを外し、run/evalのcaseを維持します。`events.go`等のREPL専用コメントも整理します。旧ログを表示する`observe_render.go`の対応は残します。

完了条件は、helpにREPLがなく、`noe repl`が未対応コマンドとして終了し、run/eval/docs/controlコマンドの入口が維持されることです。

### S2：Sessionと回復専用処理の撤去

次のsourceと対応する専用テストを削除します。

- `internal/runtime/session.go`、`session*_test.go`、`agent_session_test.go`
- `internal/runtime/async_input.go`、`syntax.go`
- `internal/runtime/completion.go`、`completion_names.go`、`completion_test.go`
- `internal/runtime/stdlib_lifecycle.go`

`runtime.go`・`stdlib.go`・`eventloop.go`から、補完・値表示helperの初期化、取消しprobe、`drainCancelled`、timer/resource集計、`trackTimers`・`trackResources`・`hasStdlibWork`・`hasResourcesSince`を除去します。Sessionのawait結果を変数へcommitするためだけの`eventLoop.await`のcallback引数も除去します。

`nativeio.Host.Active()`のproduction参照はSession/completionの判定だけです。これに付随するHostの集計counterも削除対象とし、AsyncWork/Ref・WaitGroup・resource registry・Closeによる回収を残します。signal購読自体の`active`等、解放制御に使う個別状態は別物として維持します。集計counterだけに依存するテストは、resourceの回収・処理の完了を確認する形へ合わせます。

`newHost`・`Execute`・`host.run`・module loader・`beginAgentOperation`、stdlibのcontext連携、rootエラー時のloop停止と診断は維持します。REPL削除に不要な一般的なruntime再設計は行いません。

完了条件は、REPL専用helperやwrapperを通常の起動時にも生成しなくなり、残る実行経路が同じ公開API・終了と取消しの契約を満たすことです。

### S3：テストの整理

REPL固有の状態保持・prompt・編集・履歴・補完・入力中断後の回復を検証するテストは削除します。

- `tests/repl*_e2e_test.go`、`tests/agent_repl_terminal_e2e_test.go`
- `tests/sleep_terminal_e2e_test.go`、`tests/serve_terminal_e2e_test.go`、`tests/agent_control_terminal_e2e_test.go`（現在の中身はREPL専用）
- `runREPLInput`、`liveREPL`・`terminalREPL`・capture/PTY helper
- `internal/runtime/low_level_test.go`等のSession専用case、および通常テスト内に混在するREPL case

通常テストに混在するSession利用は、確認したい契約で判断します。単なる「次の入力で回復する」caseは廃止します。lazy import、入力検証、画像のroot制約、catalogの設定snapshot、元のGo error・診断の保持、ファイル操作等の契約をSessionだけで確認している部分は、既存run/evalテストで十分かを確かめ、不足分だけ`Execute`または実CLIのテストへ移します。

重点確認ファイルは`stdlib_test.go`・`catalog_test.go`・`decision_test.go`・`images_test.go`・`diagnostic_test.go`・`filesystem_test.go`・`import_map_test.go`・`agent_observation_test.go`・`process_io_test.go`・`stdin_test.go`です。`tests/low_level_e2e_test.go`の実行modeはREPLだけ外し、run/eval/Worker/run_js/events/observeを残します。

完了条件は、Sessionの代わりになる未使用のtest-only実行基盤を作らず、残る製品機能の検証を保持することです。

### S4：利用説明・内蔵docs・開発記録

両README、docs一覧、API・agent・import map・events・observe・decision・image・process I/Oガイド、examples一覧とbasicsガイドから現行REPLの利用説明を除去します。`examples/basics/repl-demo.js`は専用例なので削除します。

`internal/agent/instructions.md`と内蔵referenceのREPLへの言及も更新します。内蔵docs本文は引き続き英文です。NOE/stdlibのAPI説明、topic/member ID、docs参照方法は維持します。

`docs/repl.md`を現行ガイドから外し、`qjs-repl-integration.md`は保守手順ではなく過去の統合記録としてarchiveへ移します。旧ガイド・削除したsourceへの履歴文書のリンクは、v1.8.0のtag URLへ向けるなど、参照先が残る形へ修正します。過去の実装・検証・releaseの「当時REPLがあった」という記録は書き換えません。

`spikes/repl-lineedit/`の独立moduleと比較結果は、製品の実装・ビルド依存ではない過去の実験として維持し、現行の保守・検証導線から外します。再実行は今回の完了条件に含めません。

開発ガイド・tests README・現行release profileのreference環境表記から、REPLのためだけのPTY要件を除去します。配布targetとテスト・vetのgate、release workflowの動作は維持します。削除メモを完了へ更新し、実装・検証結果は別の記録へ保存します。

## 検証とレビュー

1. CLIのhelp・未対応`repl`・run/evalの標準入力とSIGINT、top-level await・import map・診断を確認する。
2. 既存のrun/eval・Worker・agent/run_jsテストで、タイマー、HTTP/WebSocket、port/Worker、子process・TCP/Unix・signalの生存期間と正常終了・timeout・interrupt時の回収を確認する。各分類で新しいテストを機械的に増やさず、既存の検証で不足する場合だけ追加する。
3. docsのCLI/main/Worker/run_jsとfixture agent、agentの起動・観察・queue・hook・context・imageの既存テストを維持する。プロバイダはローカルfixtureを使う。
4. `go test ./...`、`go test -tags=e2e ./... -count=1`、`go vet -tags=e2e ./...`を実行する。共有runtimeの追跡wrapperと回収判定も整理するため、`CGO_ENABLED=1 go test -race ./internal/... -count=1`を実行する。
5. Linux/Darwin/Windows × amd64/arm64のCGO不要buildを確認する。Linux amd64ではビルドしたCLIのrun/eval/docsと取消しを確認し、他環境はcross-buildの確認として記録する。
6. REPL専用source・production参照の残存とリンクを確認する。旧ログreader、過去の記録、履歴spikeに残る語は意図したものとして区別する。
7. 実装後の独立レビューでは、共有終了処理の削り過ぎと未使用のREPL実装の残存、テストの契約保持、現行説明の整合を確認する。初回30分以内、再レビューは変更箇所と指摘解消だけを15分以内で一回とする。

実装・検証の範囲にコミット・公開、追加の実プロバイダ要求は含めません。
