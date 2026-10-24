# 責務境界と共有状態の整理計画

## 目的と対象

2026-10-24。現行 HEAD `b34754a` を起点とし、構造調査で挙げた次の1〜4を対応する。

1. `internal/agent/instance.go` を中心とするtaskライフサイクルの共有状態
2. `internal/jsexec/profile.go` の設定・組込み・ローダー・実行寿命の集中
3. `internal/stdlib/worker.go` と `messaging.go` の設定継承・実行・配送の結合
4. `internal/agent/js/task.js` のツール固有処理・hookパイプライン・モデル反復の集中

成功は、公開Go/JS/CLIの利用経路を維持しながら、独立した変更理由を分離し、状態を直接更新する場所と他機能の内部事情を知る場所を減らすこと。行数やファイル数の目標は設けない。単なるファイル移動だけで完了にしない。

対象外：provider/image、CLI events/control、機能追加、公開API変更、設定・入力制約の追加、ロック分割、agentの実行方式変更、Node互換範囲の拡大、外部依存更新、リリース・push・配布済み `./noe` の更新。既存の未追跡イベントログや `__pycache__` は触らず、コミットしない。

## 維持する契約

### agentの受付・状態・公開順序

- `Host.mu` がinstance/run/groupとcommandの共有状態を守る。新しいmutexや独立した状態コピーは導入しない。`*Locked` は保持中にのみ呼ぶ。
- owner/親子scope・実行groupの計数・上限判定、spawn/queue/steer/cancel/stopの受付条件、JSON/status/error、command/run/agentの識別を維持する。
- cancelは対象commandだけを取り消す。waitのrequest取消しは観察の中断でありtask取消しではない。結果は保持され、再取得できる。
- 選択中と待ち行列中と実行中を区別する。別のcommandのfinal commitを理由にpending commandの取消しを拒否しない。
- final commit後はfinish cleanupやrunの削除中も取消しを再開しない。確定済み回答がexecutor終了・finish報告欠落で失われない。
- 観察callback・実行・待機・cancel関数呼出しをロック内へ移動しない。現行の受付→適用/終端→待機解除のchannel/event順を維持する。Host/Client.Closeは進行中の取消し公開と所有資源回収をjoinする。
- 会話・画像・contextは確定済みsnapshotをqueueへ継承する。履歴revision、trim、steer拒否候補、hook/モデルstepの確定境界は変更しない。

### managed runtime / Profile

- 公開 `stdlib` の型alias・関数signature・初期化callback契約を維持する。環境と設定は実行ごとにsnapshotする。stream・filesystem等の借用資源を閉じない。
- Attachとmanaged runtimeの所有範囲を混同しない。初期化の途中失敗もcleanupし、診断はVMの終了前にdetachする。失敗・exit・cancel・default export直列化の順序を維持する。
- 実行ごとのfresh loader、owner-defined/private modulesの優先順と衝突回避、virtual entryの相対解決、rooted FS、既存のcaller loader保存、Prepare一回制約を維持する。
- Workerには独立runtime/loop/loader/initializer/環境を与える。stdinは空、出力の直列化境界と同時起動数は親子で共有する。

### Worker / messaging

- Profile installer経路と従来Install経路の双方を維持する。WorkerのWeb/Node API、eval/module/classic、argv/env、max/ref/unref、例外と終了コードの意味は変更しない。
- capabilityのコピーとloop再バインドは既存の共有/複製粒度を維持する。FS backend、shared worker上限、Serve/WebSocketsの対応、DNS Servers等の可変sliceを特に確認する。
- MessagePortのclone/transfer/closeと所有権移管、BroadcastChannelのruntime終了時の登録解除を維持する。
- Workerのdata/online/error/exitは同じFIFOに流す。parentPort.closeによる切断とWorkerの実終了は別。runtime・initializer cleanupとwh.doneの完了後にexitを通知し、親の終了も実Workerをjoinする。閉じたexecutionへJS通知しない。
- 共通キューを複数に分けたり、通知のための新goroutine/並行配送路を増やしたりしない。

### JS task/tool

- 公開noe:agentのexportとrun_js schema・引数/結果/diagnostics検証・エラー文言を維持する。
- raw call / effective arguments、実行事実 / hook後のモデル提示結果、画像metadata/bytesの所有、hook失敗時のtool_result報告を区別したまま維持する。
- 並列toolの結果と画像は元のcall順へ対応付ける。tool_result確定、履歴checkpoint、次モデル要求の順を変えない。
- 内部ES moduleを分割した場合、Go embed・agentbridgeのmodule定義/登録・task executor・直接module利用の試験を一緒に更新する。新内部moduleは公開agent APIの追加を意味しない。

## スライス

各スライスを「実装→関連テスト→独立コードレビュー→必要な修正と限定再確認」の順で完了し、変更内容と実行証拠を実装記録へ残す。コードレビューのP2以上はそのスライスの完了前に解消する。完了したスライスは個別コミットする。

### S1: command/taskの状態遷移を局所化する（対象1）

対象：`internal/agent/{instance,instance_operations,task,cancel_task,interrupt,broker_steer}.go` と必要な同package内helper/test。group/model/conversation側は呼出し接続以外を書き換えない。

- commandのqueue登録・選択・起動・取消受付・final確定・結果確定を、同じライフサイクル領域の明示的な操作へ寄せる。
- `command` と状態操作を `task_lifecycle.go` 等へ集約し、呼出し側からの関連field直接更新を減らす。instanceのスケジュール判断とtask実行は分ける。
- 既存のロック内処理とロック外処理の境界を保つ。必要な公開/待機処理を返す場合は操作専用の小さな値を使い、汎用action frameworkや新state machineの全面導入はしない。
- final/取消し/結果確定に関する判定・更新の所有者を明確にし、同じ状態を複数の表現で管理しない。

受入：取消し、最終確定、選択と起動の間、停止時のqueued settlement、結果保持の各利用経路が既存と同じ。状態変更箇所を検索して、対象fieldが意図した操作へ集まったことも確認する。

関連確認：`go test ./internal/agent ./agent -count=1`、runtimeのqueue/steer/cancel/task wait/final/所有/上限のfixtureを選択実行。既存の `cancel_task_test.go`、`task_lifecycle_direct_test.go`、`task_wait_direct_test.go`、`s6_review_findings_test.go`、`control_steer_test.go` を契約の証拠にする。新しい遷移境界で未確認の動作があればsleepではなくchannelで固定する回帰試験を追加する。

### S2: Profileの設定・ローダー・実行寿命を分離する（対象2）

対象：`internal/jsexec/profile.go` と同package内の新規 `profile_options.go` / `profile_config.go` / `execution_loader.go` 等、対応するtest。公開 `stdlib/profile.go` は互換性の確認対象。

- 設定型・snapshot/normalize、下位Configへの変換、attach/cleanupを独立した変更単位へ分離する。
- loaderのfactory、defined modules、prepared状態とload関数を専用の内部オブジェクトへまとめ、`Bindings` から操作を委譲する。単に `Bindings` のmethodを別ファイルへ移すだけにしない。
- ローダーがQuickJSの所有goroutineで動く規則とmodules登録/Closeの同期を維持する。閉鎖状態を別コピーで二重管理しない。
- Worker installerは既存lower.WorkerInstaller契約を使い、S3のworker実行層を同時に再設計しない。

受入：Profileを変更するためにローダーや終了処理を読む必要が減る。load/prepared/modulesの更新は内部loader境界へ集まり、公開alias/Attach/Prepare/LoadModule/Closeの利用結果は不変。

関連確認：`go test ./internal/jsexec ./stdlib -count=1`。fresh loader/virtual import/defined module collision/caller loader、環境snapshot/rooted FS、initializer途中失敗、借用runtime/loop/stdio、default result/diagnosticの既存試験を使う。loader境界抽出で新しい接続が生まれた場合のみ、その製品経路に対応する試験を追加する。

### S3: Worker設定継承とVM起動停止を分離する（対象3・前半）

対象：`internal/stdlib/worker.go` と同package内 `worker_config.go` / `worker_runtime.go` 等。必要なら `Config` 定義側に設定専用操作を置く。

- child Configの生成・env/argv継承・capabilityのloop再バインドを設定領域へ寄せる。
- runWorkerのVM生成・installer選択・entry評価・失敗/終了/cleanupを実行領域へ分離する。API登録・親側handle/上限/ref管理はworker本体へ残す。
- Worker側がcapabilityを一つずつ知る箇所を設定側へ集約する。汎用plugin registry等の新機構は導入しない。
- 終了待ちの責務、エラーのJS値からGo dataへの変換時点、port切断とworker終了の区別を変えない。

受入：Worker本体から各capabilityのfield操作とVM評価手順が分離され、両installer経路、nested Worker、共有max、親の終了待ちが成立する。

関連確認：`go test ./internal/stdlib -run 'TestWorker|TestWebWorker|TestWorkers|TestLoop' -count=1`、`go test ./internal/jsexec ./stdlib -run 'Test.*Worker' -count=1`。既存のWorker DNS/Profile/sharing/close lifecycle試験も選択し、設定copyの意味と実終了joinの既知退行を確認する。

### S4: messagingのcodec・broadcastとWorker通知の境界を整理する（対象3・後半）

依存：S3の実装・レビュー完了後。同じ実装担当が引き続き担当する。

対象：`internal/stdlib/messaging.go`、S3のWorker領域、必要な `message_codec.go` / `broadcast.go` と対応test。

- clone/transferのcodec、BroadcastChannelのregistry/登録寿命を配送coreから分離する。
- portEndが具体的workerHandleを直接知る依存を、Worker側が提供する小さな通知境界に置き換える。FIFO、close、drop/終了後の通知処理の意味は保持する。
- Worker通知の解釈はWorker側へ寄せる。共通キューと順序制御はmessaging側に残し、callbackは従来と同じowner loopで呼ぶ。
- 通知境界は実在するonline/error/exit/closeの用途に限定し、公開interfaceや汎用イベントbusは作らない。

受入：messagingの配送coreがworkerHandleの内部状態を直接更新せず、codecとbroadcastは各領域内で変更できる。port.close後もdata→error→exitとinitializer cleanup待ちが維持される。

関連確認：MessageChannel、structuredClone、port transfer/close/drop、scope停止、broadcast登録解除、`TestWorkerLifecycleContinuesAfterParentPortClose`、公開 `TestWorkerPortCloseWaitsForCleanup`。順序/終了を変える境界に既存試験の不足がある場合はchannel制御で追加し、タイミング頼みの大量反復はしない。

### S5: JSツールアダプタ・hookパイプラインをtask loopから分離する（対象4）

対象：`internal/agent/js/task.js`、新しいprivate JS modules、`internal/agent/module.go`、`internal/agentbridge/{bridge,managed}.go`、関連test。

- run_js schema・実行・引数/結果検証をtool adapterへ寄せる。
- hook前後、実行事実/モデル提示結果、画像処理、tool_result記録をtool pipelineへ寄せる。
- `executeTask` は履歴・context・steer・モデル反復・最終確定の調停を担当する。
- helperの重複を増やさず、依存はtask→pipeline→adapter/validationの一方向とする。hook/history/APIの既存境界は保つ。
- task moduleから直接利用されている `executeToolWithHooks` の既存exportは、必要なら互換re-exportで維持する。全新moduleをembed/登録し、profile/task/tool実行時に解決可能にする。

受入：ツール形式や結果の検証を変える際、モデル反復本体を編集しない構造となる。既存のモデル向け結果予算・診断・hook・画像・並列tool・履歴checkpointの意味は不変。

関連確認：変更JSの `biome lint`、`go test ./internal/agent ./internal/agentbridge ./internal/agentexec -count=1`、`internal/runtime` のhooks/phases/output/images/trim/queue/decision/agent fixtureを選択実行。module登録漏れは実executor経路で検出する。

## 依存順と分担

- 計画の通常レビューと批判的レビューを独立に行い、P2以上が両方でなくなるまで修正・再レビューする。本文とレビュー記録だけを先にコミットし、その後に実装を始める。
- S1、S2、S5は担当pathが独立し、並行実装可能。S3はS2が固まった後、S4はS3完了後に行う。同一ファイルを並行編集しない。
- 各担当は自分の差分と関連テストを実行し、親が独立レビューを依頼する。修正は元の担当へ戻し、同じreviewerが再確認する。
- 統合文書 `docs/development/responsibility-boundaries-refactor-implementation.md` と現行構成の説明は親が更新し、担当間で競合させない。

## 総合テストと完了

すべてのスライスのレビュー修正が落ち着いた候補に、親が以下を一回まとめて実行する。失敗は今回の到達経路を確認し、今回の退行と既存別件を分ける。修正後は影響範囲を再確認し、無関係な全メニューの自動再実行はしない。

1. `CGO_ENABLED=0 go test -tags=e2e ./... -count=1`：公開Go、CLI、managed agent、stdlib、外部consumerを含むローカルfixture。live/slow等のopt-in試験は有効化しない。
2. `CGO_ENABLED=1 go test -race ./internal/... ./stdlib/... ./agent/... -count=1`：共有command状態・Worker・messaging・loader/Closeの同期を確認。
3. `go vet -tags=e2e ./...`、変更JSの `biome lint`、`gofmt` と `git diff --check`。
4. 配布対象6種（Linux/Darwin/Windows × amd64/arm64）で `CGO_ENABLED=0 go list -export ./...`。公開Goのalias/内部型とOS別実装のcompileを確認し、リンク/配布はしない。
5. 現行構成文書のpathと責務を更新し、公開export・JS module登録の差分に意図しない変更がないことを確認する。

外部providerへの通信、ベンチマーク、負荷反復、長時間待機は、この挙動維持リファクタの完了条件にはしない。既知の退行に関する既存の決定的試験を再利用する。新しい回帰試験が必要なときは、その未確認の製品動作と利用者への影響経路を実装記録に説明する。

完了条件：対象1〜4の責務境界が上記の受入に達し、各スライスの関連試験・独立コードレビューで未解消P2以上がなく、総合テストが成立すること。実装・検証・残件を記録しコミットする。push/release/binary置換はしない。

## 計画レビュー記録

2026-10-24、実装着手前の同じ初稿に対して独立に実施した。

| review | 対象・観点 | 結論 |
| --- | --- | --- |
| 通常レビュー（gpt-6.1-sol / medium） | 対象1〜4の達成、状態更新経路、分担、受入、関連試験と現行コードの整合 | P2以上0件。必要な計画修正なし |
| 批判的レビュー（gpt-6.1-sol / high） | final/run削除後の取消し、Closeと公開待ち、loader衝突、Worker実終了、JS module接続とtool確定順序、単なる配置移動にならないか | P2以上0件。必要な計画修正なし |

両レビューで共有mutex、イベント/FIFO順、借用資源、公開API、module登録と状態所有の境界を現行実装に照合した。初回で両方のP2以上が0件となったため、追加の計画変更・形式的な反復は不要と判断した。この記録を含む計画を先にコミットし、その後に実装する。
