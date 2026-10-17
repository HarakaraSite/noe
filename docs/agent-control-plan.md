# Agent実行中の操作の実装計画

状態：実装・検証・コードレビュー完了。2026-10-17。[起動と待受の計画](agent-lifecycle-plan.md)から分割した後続計画。S1〜S4と最終の統合レビュー・総合検証を完了。[実装・検証記録](agent-control-implementation.md)に指摘・対応・検証結果を記録しています。

本書は、steer・明示的なinterrupt・人間用の操作CLIの計画正本とする。起動API・keepAlive・queue・最小観察・所有者取消しは前提計画が担当する。両計画の依存順は「起動と待受の完了 → 実行中の操作」とし、別々に実装・検証を完了できる単位にする。

## 目的と対象範囲

親agentが子の状態を確認し、実行途中で指示を追加したり、特定のタスク・待受全体を停止できるようにする。人間も別ターミナルから同じGo側管理機構を利用する。

通常実行では入力・承認待ちを作らない。モデルに渡すtoolは `run_js` 一つのままとし、そのJS内から操作APIを呼ぶ。steerは同じrunへの追加指示、queueは独立した次のrunの予約として区別する。

以下は対象外とする。

- 起動APIやkeepAliveの再設計、会話履歴の自動引継ぎ、コンテキスト圧縮。
- daemon、永続queue、クラッシュ復旧、親終了後の独立実行。
- pause / resumeなど別の操作、TUI、ネットワーク越しの管理。
- live観察とファイル観察のcursor統合、詳細イベント再生。
- プロバイダ追加、互換alias、リリース作業。

## 前提と接続点

[前提計画](agent-lifecycle-plan.md)が以下を提供した後に開始する。

- `run_agent` / `spawn_agent` と `keepAlive`、FIFOの `queue_agent`。
- 固定 `agentId` とタスクごとの `runId`、親runによる子の所有。
- instanceの `starting / idle / running / stopping / stopped / failed` 状態。
- taskごとに分離したcontext、親取消し・Ctrl+Cによる回収。
- 完了報告とRunner回収の区別、タスク終端の一度だけの通知。
- `observe_agent({ agentId })` の状態・current run・queue件数・直近結果。
- JSONLでのagent/run/commandの対応付けと、既存のファイル観察。

既存Go managerへ操作受付を追加する。新しい管理daemonや独立したqueue基盤は作らない。基盤に変更が必要なら、この計画の変更として扱い、前提計画に未実装のsteerやcontrol endpointを要求しない。

## JSの操作API

| API | 対象と意味 | 戻る時点 |
| --- | --- | --- |
| `steer_agent({ runId, instruction, commandId? })` | 実行中のタスクへ追加指示 | 受領時 |
| `interrupt_agent({ runId })` | 指定タスクを取消す | 取消し要求の受領時 |
| `interrupt_agent({ agentId })` | 待受と予約を含むagent全体を停止 | 停止要求の受領時 |

interruptの二つのID指定は排他的に検証する。受領応答は後始末完了を意味しない。状態・適用結果・回収完了は観察とイベントから確認する。

親JSの操作はscopeと親子所有関係で認可する。rootの操作でも、誤ったID・終了済み対象・新規受領を停止した対象を識別できる応答を返す。

## steer

指定runのinboxへ指示を受領し、次のモデル要求前にuser入力として履歴へ追加する。進行中のモデル通信・run_jsをその場で書き換えたり停止したりしない。停止が必要ならinterruptを使う。

- runningの対象runに受領し、idleや終了済みrunには受領しない。
- 受領順を保ち、command IDで受領と適用を区別する。
- `commandId` 未指定時は発行する。同一run内の同じIDの再送は重複適用せず、異なる内容でのID再利用はエラーとする。
- 同じrunのsystem・cache ID・回数上限を維持し、steerで上限を初期化しない。
- 既存の完全なassistant messageとreasoningを保持する。
- 受領後にrunが取消された指示も、取消し・未適用を記録し、黙って残さない。

### 最終回答との競合

モデル要求前の確認だけでは、その要求中に受領したsteerが最終回答の後に取り残される。最終回答候補の確定時にもGo側で原子的に確認する。

1. JSは候補をreturnする前に内部の確定RPCを呼ぶ。
2. 保留steerがあれば `{ committed:false, instructions:[...] }` を返し、runを維持する。
3. JSは候補assistant message全体とsteerのuser messageを履歴へ追加し、同じloopを継続する。`reasoning_details` 等を捨てない。
4. 保留がなければ `{ committed:true }` とし、最終回答を一度だけ確定してsteer受付を閉じる。
5. JSは確定成功後に回答を返す。確定RPCの失敗は握り潰さず、failure/cancelのcleanupは冪等にする。

steer受領・task interrupt・final確定を同じ排他区間で順序付ける。先に受領したsteerは処理し、final確定後のsteer・task interruptは終了済みとして返す。

`agent.final` は回答確定時に一度通知する。管理タスクの `agent.completed` 等は前提計画どおりRunner・scope回収後に一度だけ通知する。次タスクの開始とinstance停止待ちの解決も回収後とする。

## interrupt

### タスク単位

`{ runId }` は、そのタスクのモデル通信・生成JS・子scopeを取消す。回収後にcancelledを記録する。

- `keepAlive:true` のinstanceと他の予約は残し、次のFIFO予約またはidleへ進む。
- 単発instanceは対象タスクの回収後に終了する。
- final確定後はtask interruptを終了済みとして返す。確定後の回収中processを含めて停止する場合はinstance停止を利用する。

### instance単位

`{ agentId }` は全体をstoppingにし、新規queue・steerの受領を止める。実行中タスク・未実行予約・子instanceを取消し、待受を終了する。

idle中に停止しても、最後の成功タスクを失敗へ書き換えない。未実行予約には取消し・未実行結果を対応付ける。`run_agent` は回収後にinstanceの終端snapshotを返す。

### 受付と回収の順序

受付handlerは取消し要求の受領で戻り、対象processや自分自身のrequestのjoinをhandler内で待たない。受付停止を確定し、lock外でcancel・scope close・Runnerのjoinを行う。

正常な明示停止ではownerへPromiseのsettleを投稿する。所有者取消し・Loop終了時はJSでのsettle実行をGo cleanupの完了条件にしない。Ctrl+Cと所有者取消しは前提計画どおり所有範囲全体を終了させ、待受継続へ変換しない。

## 人間用CLIと接続

人間は起動済みNOEへ別ターミナルから接続する。agentの起動入口は従来のrun / eval / replのままとする。

```sh
noe agents
noe steer <run-id> '表形式に変更して'
noe queue <agent-id> '続けてテストして'
noe interrupt --run <run-id>
noe interrupt --agent <agent-id>
```

- `noe agents` は到達可能な実行ホストとagentの状態・agent ID・current run IDを表示する。
- queueの実行規則は前提計画と共有し、CLI専用のqueueを作らない。
- IDで対象ホストを解決する。対象なし・接続切断・曖昧な指定は診断を返す。
- 受領と実際の適用・回収完了を表示上も区別する。観察には既存のJSONLと `noe observe` も使える。

既存localhost brokerへcontrol APIを追加する。人間用controlには、子Connectionに渡る既存Bearerとは別のcapabilityを発行する。子tokenをhost全体の操作権限へ昇格させない。

接続先とcontrol capabilityはユーザー専用runtime directoryの接続情報ファイルへ置き、mode 0600とする。認証情報をJSONL・stdoutへ出さない。正常終了では登録を除去し、探索時も接続先の到達性を確認する。これは実行中ホストの発見に限り、daemon化や再起動復旧には広げない。

## 観察と結果

既存agent/run IDに操作command IDを対応付け、受領・適用・取消し・未適用を追跡できるイベントを追加する。最小live snapshotとファイル観察を使い、詳細イベントの新しい再生基盤は設けない。

終端snapshotの `{ agentId, state, reason, lastResult }` を維持し、instanceの停止理由と最後のタスク結果を分ける。受付後の明示interruptは状態・結果として記録し、所有者取消しの場合はJSへの結果配送を保証せず回収を優先する。

## 実装スライス

| Slice | 作業 | 主な受入条件 |
| --- | --- | --- |
| S1 steer | JS・broker入口、inbox、command追跡、モデル要求前の反映、最終回答の確定判定 | 指示順・reasoning・上限を保持し、受領済みsteerがfinal競合で失われない |
| S2 interrupt | run/instanceの二つの対象、取消し受付、回収・終端通知 | taskだけなら待受継続、instanceなら予約を含め終了。handlerとjoinが相互待ちにならない |
| S3 人間用入口 | host発見、別control認証、agents/steer/queue/interrupt CLI、操作表示 | 別プロセスから共通APIを使え、子scope制限を迂回しない |
| S4 文書と総合確認 | instructions・README両言語・API/観察ガイド・examples・CLIガイド、総合テスト | 親agentと人間の両方から操作でき、前提計画の利用が維持される |

依存順はS1→S2→S3→S4。各スライスで関連文書とテストを更新し、最後に全体の整合性を確認する。主な変更対象は `internal/agent/`、runtimeのnative Promise接続、`internal/cli/`、関連tests・利用ガイド。

## 検証と完了条件

fixture provider・ローカルbroker・実NOEプロセスを使う。競合の確認は制御可能なgateを使い、固定sleepに依存しない。

- モデル通信中・run_js実行中にsteerを受領でき、次の判断へ一度だけ適用する。
- final候補とsteerを競合させ、受領済み指示の反映と確定後の受領拒否を確認する。
- 同じrunのreasoning・cache ID・モデル呼出し回数上限を維持する。
- run interruptは生成JS・子scopeを回収し、待受instanceと残りの予約を維持する。
- agent interruptはbusy / idleどちらでも停止し、未実行予約を取消す。
- final・interrupt・所有者取消しを競合させ、終端通知の重複とself-deadlockを起こさない。
- 明示停止でrun_agentへ終端snapshotが返り、Ctrl+C・REPL中断でも回収が成立する。
- 別プロセスCLIでhost発見・観察・各操作を行い、到達不能時にも診断を返す。
- 人間用control capabilityと子tokenを分け、親JSの操作が所有範囲に従う。
- 前提計画の単発起動・待受・queue・既存JSONL観察が引き続き成立する。

変更経路のテスト後に `go test -tags=e2e ./...` と `go vet -tags=e2e ./...` を実行する。並行処理の変更packageと関連E2Eはrace detectorで確認する。OS別process管理を変更した場合は配布対象のクロスビルドを確認し、native実行と区別して記録する。

実プロバイダ確認・公開はfixture検証から分ける。設計レビューの結果は内部RPC・所有関係・回収の設計入力として使い、実装コードと検証結果のレビューで完了を判断する。

[起動と待受の計画](agent-lifecycle-plan.md) · [開発記録へ戻る](README.md)
