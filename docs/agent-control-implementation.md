# Agent実行中の操作：実装・検証記録

状態：実装・総合検証・最終レビュー完了。2026-10-17。正本は[実行中の操作計画](agent-control-plan.md)。前提は[起動と待受の実装記録](agent-lifecycle-implementation.md)。

## スライスとレビュー

- S1 steer：inbox・command ID・適用ack・final確定RPCを追加。syntheticなsteer配送はモデル呼び出しに数えず、Go側が実際のprovider呼び出し時にmaxStepsを検証する。agent/runtimeテストとagent race成功。独立レビューのP2「commit後finish前の終了で確定回答がlastResultから失われる」は採用し、修正済み。実NOEでcommitだけを送りfinishなしでprocess終了する回帰fixtureを追加し、確定回答を保持したままfailedとなることをrace付きで確認。
- S2 interrupt：run/instance取消しを分離し、taskのみの取消しではkeepAliveと予約を維持、instance停止では待受・予約・子workを回収。公開API・自己interruptのruntimeテストとfocused race成功。独立レビューのP2「fatal終了が受領済み停止を上書き」「停止受付後のbootstrap run登録」は採用し、受付状態を同じHost.mu区間で確定して修正。observer/cancel gateによる決定的な回帰テストも追加しrace成功。自然終了と停止、stoppingとidle/runningへの遷移も同じ受付境界で整理した。修正後のS2レビューに未解消findingなし。
- S3 人間用入口：既存brokerに別capabilityのcontrol endpointを追加。rootのみ接続情報を登録し、正常終了時に除去。CLIはagents/steer/queue/interruptを提供し、受領応答を表示する。client/CLIの先行レビューとserver接続レビューを実施。P2「HOMEなどが未設定だとagentを使わないevalまで失敗する」は採用し、接続先directory解決を初回broker登録時へ遅延、回帰テストを追加。修正後の追加findingなし。
- S4 文書：README両言語、API・REPL・観察・イベントガイド、モデル向けinstructions、操作exampleを更新。総合検証は完了。最終レビューで公開契約・文書・slice間接続と検証結果の整合を確認し、追加findingなし。

## 検証済み

実プロバイダは呼ばず、ローカルfixtureと実NOEプロセスを使用。

- `go test ./internal/agent ./internal/runtime -count=1`：成功（S1）。
- `go test -race ./internal/agent -count=1`：成功（S1）。
- control client/server・CLI・表示・通常eval回帰のfocused test/race：成功（S3）。
- `go test -tags=e2e ./tests -run '^TestAgentHuman' -count=1 -timeout=70s`：成功。
- 上記human E2Eのrace版：成功。
- `go test -race ./internal/agent -run '^TestCommittedAnswer' -count=1 -timeout=30s`：成功。
- `go test -tags=e2e ./tests -run '^TestAgentHumanRunInterruptReaps' -count=1 -timeout=40s`：成功。

Human E2Eでは別CLI processからhost発見、steer、queue、run interrupt、agent interruptを実行。steerでreasoning/history/cacheを保持し、task取消し後もFIFO予約が実行され、待受停止後にownerのawaitが正常に返ることを確認。REPLは外部interrupt後にpromptへ戻り、次の評価を実行できることを確認した。Linuxでは親runの取消しで、子agent・子run_js・shell・sleepまで回収/停止することをPIDで確認した。

## 総合検証

Linux amd64、Go 1.27.2で実行。Go実行時の`TMPDIR`/`GOTMPDIR`は`/home/agent/.cache/noe-qjs-repl`を指定。

| チェック | 結果 |
| --- | --- |
| `go test -tags=e2e ./... -count=1 -timeout=180s` | 成功（修正後最終実行） |
| `go vet -tags=e2e ./...` | 成功 |
| `go test -race ./internal/... -count=1 -timeout=180s` | 成功 |
| `go test -race -tags=e2e ./tests -run '^TestAgent' -count=1 -timeout=180s` | 成功 |
| provider開始gate修正後の`TestPublicSteerAgent` focused race、`-count=5` | 成功 |
| `CGO_ENABLED=0`、Linux/macOS/Windows各amd64/arm64の6target build | 全成功 |
| 手元の`./noe`再ビルド、`--help`でcontrol各commandを確認 | 成功 |
| `git diff --check` | 成功 |

クロスビルド結果はローカルの`/home/agent/.cache/noe-qjs-repl/noe-control-final-builds-r5dh9ny_/results.json`。Darwin/Windows/arm64のnative実行は行っていない。総合internal raceの後の変更はpublic steerのtest同期だけで、同testのfocused raceと全体通常testを再実行した。

## 検証中に解消した回帰

初回の総合testで、owner取消しの`instance.stopping`通知までcommand受領通知を待ってしまう順序変更を既存テストが検出した。受領通知待ちは明示interruptの場合だけに限定し、owner取消しの既存の通知順を復旧した。修正後の総合testは成功。別の実行ではpublic steerのfixtureがrun登録とprovider呼び出し開始を同一視していたため、steerが最初のモデル要求より前に正しく反映された場合に期待が外れた。provider開始markerを待ってからsteerを送るgateへ修正し、完了と要求通知が同時に到達しても誤判定しないようにした。修正後のfocused raceを5回確認した。

## 受入条件と検証経路

| 契約 | 主な証拠 |
| --- | --- |
| モデル通信中のsteer、final候補のreasoning/cache保持 | `TestSteerDuringProviderRequestPreservesCandidateAndSettings`、`TestPublicSteerAgentControlsAnActiveTask` |
| run_js中のsteer、tool結果との順序と上限維持 | `TestSteerDuringGeneratedJavaScriptPreservesToolOrderAndBudget` |
| command順・再送・scope・取消し | `control_steer_test.go`、control capabilityのserver test |
| taskだけを取消しFIFOを継続 | `TestPublicRunInterruptKeepsKeepAliveAndQueuedTasks` |
| busy/idle instance停止、予約取消し、直近回答保持 | `TestPublicAgentInterruptCancelsBusyTaskAndQueue`、`TestPublicIdleAgentInterruptPreservesLastResult` |
| final確定後のtask interrupt拒否 | `TestPublicRunInterruptRejectsAfterFinalCommit` |
| run_jsからの自己interruptに自己待機がない | `TestPublicRunJSCanInterruptItsOwnTaskWithoutJoining`、`TestPublicRunJSCanInterruptItsOwnInstanceWithoutJoining` |
| 停止とfatal/bootstrapの受付順 | `control_supervision_test.go`、`control_start_test.go` |
| finalだけを確定してprocess終了しても回答を保持 | `TestCommittedAnswerSurvivesMissingFinishReport` |
| 人間用CLI、正常なawait帰還、REPL再利用 | `TestAgentHumanControlCLIWhileEvalRuns`、`TestAgentHumanInterruptReturnsREPLPrompt` |
| 子agent・生成NOE・OS子processの取消し/回収 | `TestAgentHumanRunInterruptReapsNestedProcessGroups` |
| 通常JSの遅延初期化とcapability分離、登録cleanup | `TestOrdinaryEvalDoesNotRequireAgentRuntimeDirectory`、`control_server_test.go` |

## 対象外

実API接続・コミット・push・リリースは行っていない。daemon・永続queue・クラッシュ復旧・親終了後の独立実行は追加しない。
