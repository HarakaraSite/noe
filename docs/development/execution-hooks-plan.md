# 実行グループ・共通操作API・hook・コンテキスト通知の計画

状態：2026-10-18、S1–S6の実装・検証・レビューを完了。総合テスト・6対象ビルドと、key更新後の実接続全6caseの受入を確認した。進捗・検証結果は[実装記録](execution-hooks-implementation.md)、公開hook契約は[hookガイド](../hooks.md)へ記録する。本書が今回の実装計画である。

利用者は、総合テストと、`opencode-go` / `deepseek-v4.1-flash`による実プロバイダテストを許可した。本書の作成時点では接続しない。実装認可、commit・push・releaseは今回の計画作成と区別する。

## 目的と受入条件

長い自律タスクで、agent自身と人間がコンテキスト予算・使用量を把握できるようにする。その土台として、agent・hook・queueが生む仕事の所属と原因を追跡し、既存の制御操作を共用する。

- agentは次の判断で入力予算・直前要求の実測・次要求の概算を読める。通知自体は履歴へ蓄積しない。
- 人間は`--observe`、親や外部スクリプトはJSONLと既存の観察APIから、同じ情報と関連IDを確認できる。
- 外部JSでモデル前後・tool前後のhookを定義できる。共通操作APIからsteer・interrupt・queue・spawn等を呼べる。
- steer配送による再準備で同じhookを重複実行せず、準備中に届いた指示を取りこぼさない。
- 子孫・hook・queueを所有ルートごとに集計でき、任意に設定した共有上限を適用できる。独立した親agentの実行量・取消しは分離する。
- hookが終了しなくても、既存のtask/instance取消しで実行と所有workを回収できる。
- 各スライスで実装・対応するテスト・コードレビューを完了し、最後に総合テスト、実プロバイダテスト、全体レビューを行う。

## 範囲と分担

Goは既存の`Host`・`Operation`・agent instance・task runの状態、所属、受付、集計、取消しを所有する。JSは外部hookの処理内容を定義する。既存の制御handlerやschedulerを新しいイベントbusで作り直さない。

OSプロセス数・CPU・メモリ等の資源制限は、利用環境のOS機構を使う。cgroup管理の組込み、OS全プロセスの因果追跡、任意の外部コマンドや子NOEへの設定注入は含めない。NOE管理のtask/run_jsと`noe:process`の既存取消し・回収は維持する。

自動圧縮・要約、任意の会話履歴置換、外部hookによる任意イベントの発火、制御イベント全種のhook公開、汎用tool登録、独立agent実行、会話の永続化、stdlib変更、releaseは対象外。tool hookは将来の追加toolにも使える契約にするが、今回新しいモデル用toolを追加する必要はない。

## 現行経路と変更の入口

| 現行の所有者・経路 | 今回接続する内容 |
| --- | --- |
| `internal/agent/js/agent.js`の公開関数 → RPC | 共通操作APIのfacade、configの正規化・継承 |
| `internal/agent/lifecycle.go`のinstance/task/queue | 所有ルート、予約の所属と原因、共有集計 |
| `internal/agent/js/task.js`のモデル/tool loop | 論理判断、hook、要求限定の通知 |
| `broker_model.go`・`decision.go`のprovider呼出し | 常時の要求識別、実行量の受付、usageとの対応 |
| `broker_steer.go`・`control_interrupt.go`・`control_server.go` | 現行の適用・最終確定・取消しを共用 |
| `runner.go`・`internal/runtime/agent.go` | task内hook実行、現在runへの操作元の束縛、module解決 |
| `internal/events`・`internal/observation`・`internal/cli/observe*` | 関連ID、予算・集計・停止理由の記録と表示 |

公開JSと人間CLIは既に同じGoの制御handlerを使う。今回はこの実処理を再利用し、hookからも呼べるようにする。変更対象のファイル分割は実装時に既存の責務に合わせ、新しい巨大managerへ集約しない。

## 実行グループと因果関係

### 所属・取消し・予算の単位

一つのroot agent instanceと、その所有下の子孫・task・予約・hookを同じgroupとする。`groupId`はrootの`agentId`を再利用する。同じNOEで独立した親A/Bを起動すれば別groupになる。既存`executionId`はCLI起動全体を示すため、groupとは区別する。

keepAliveで処理する次task、人間が同じinstanceへ送ったqueueも同じgroupに属する。groupは指示文一件ではなく所有ルートの寿命単位である。別の寿命・共有予算には別root instanceを起動する。既存instanceへ別groupの仕事を混在させるschedulerは追加しない。

queueの所属・計数先は対象instanceのgroup、原因は送信元のtool/hook/外部操作。権限上許される別groupへの操作があってもgroupを合併しない。原因の矢印を辿って他groupを停止しない。

NOE管理のagent/hook/run_jsから呼ぶ`noe:decision`はそのgroupの集計へ含める。agentと無関係な通常JSのdecision利用にroot agent groupを自動生成する変更は含めず、既存の評価scope・CLI timeoutで扱う。

### 共通情報

- `groupId`：所有ルート。
- `activityId`：Goが発行する個々の実行・操作のID。既存のrun/tool/command/request IDは元の意味で維持する。
- `causedBy`：この仕事を生んだactivity・イベントの参照。並列toolの合流等では複数の参照を持てる。
- `origin`：script / model / tool / hook / human等の出所。判明しているrun・tool・hookの識別情報を添える。
- 既存の所有scope：寿命・取消し・認可を決める。接続tokenやcapability値を公開する識別子にはしない。

情報と集計は観察の有無によらずGoで保持する。JSがgroupIdを申告して所属替えするAPIにはしない。Goに過去の因果グラフ全文を保持せず、active状態・予約・短い集計を保持し、過去の詳細は既存JSONLへ記録する。

hookごとに束縛したAPIを渡し、並列hookの呼出元をグローバルな`currentHook`変数から推測しない。通常import版の操作で正確なhook原因を取得できない場合はtask/run由来と記録し、推測したhook名を付けない。

## 公開設定と共通API

### configの追加案

```js
const result = await run_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
  hooks: ["./hooks.js"],
  contextBudget: { inputTokens: 200000 },
  // 任意。値は利用例であり既定値ではない。
  executionLimits: {
    providerCalls: 100,
    agentStarts: 8,
    queuedTasks: 16,
    hookCalls: 1000,
  },
}, "task");
```

| 設定 | root | 子・次task |
| --- | --- | --- |
| `hooks` | 省略時は外部moduleなし | 子で省略すると起動元の一覧を継承。明示配列で置換、`[]`で解除。次taskはinstanceの設定を使う |
| `contextBudget` | 省略時は明示入力予算なし | 子で省略すると継承。明示objectで置換、`null`で明示予算を解除。各runで独立に測定する |
| `executionLimits` | rootでのみ共有上限を指定 | 子は同じgroupの残量を共有し、上書きしない。子の起動設定には別上限を指定しない |

`contextBudget`の初期項目は`inputTokens`。モデルwindowは既存catalogの`contextWindow`があれば参照し、取得できない場合は未取得とする。`maxOutputTokens`を実際の応答予約量と見なさない。今回、未知の応答余裕から有効入力上限を自動算出しない。

`executionLimits`は省略項目が無制限、明示した0は受付枠0として扱う。従来の`maxSteps: 0`が無制限である契約とは異なることを記述する。`maxSteps`は引き続きrunごとのGenerate回数上限であり、共有上限の代わりにはしない。

### 共通操作API

`steer_agent()`・`interrupt_agent()`・`queue_agent()`・`spawn_agent()`等と、人間CLI、hookへ渡す`api.agents`を同じGo実処理へ接続する。制御操作の受付と、適用・終了・回収完了を混同しない。明示的な`run_agent()`等の待機操作は現在の待機契約を維持する。

hook用APIの初期構成：

- `api.agents`：既存agent操作のfacade。
- `api.context.get()`：当該run/論理判断の予算・直前実測・現時点の概算と、その時点の識別情報。
- `api.context.setRuntimeInfo({key, text})`：`beforeModelRequest`で今回の要求へ添えるキー付き情報を設定。同一keyは置換し、元の会話履歴には追加しない。
- `api.execution.get()`：現在のgroup・原因情報・集計・任意上限を取得。

任意の履歴全体を置換するAPIは初回に公開しない。上記の正確な戻り値はS1/S2でdocsへ定義し、実装開始時に読み手が利用できる最小の型例を併記する。既存scopeで許される対象を共通API化によって拡大しない。

### レビュー指摘を反映する所有元の解決

task bootstrapの`Operation.parentRunID`は現在run自身ではなく起動元の親runを指す。これを無調整でspawn元にすると、子taskのhookが起動したagentは兄弟扱いとなり、子taskの取消し後に残り得る。

- 渡すhook APIには、current runを親とする操作scopeをGoで束縛し、現在runの`childScopes`へ登録する。既存run_jsと同じ所有経路を利用する。
- 通常の`import {spawn_agent} from "noe:agent"`等も共通Go実処理で現在runへ解決する。managed taskの操作元は`op.task.run`から、run_jsの生成scopeは従来の`parentRunID`から解決する。decisionの所属・帰属も同様に扱う。
- task bootstrap本体の親run情報を書き換えない。
- hookがspawnしたagentは現在runの`instances`に属し、hook invocation後も現在runの寿命で継続できる。hook用API scopeの終了と、spawn済みagentの寿命を混同しない。

## hookの登録・実行契約

### moduleと寿命

`hooks`は外部ESMの順序付き配列とする。Goで登録時に既存import mapとホストcwdから絶対的なbindingへ正規化する。taskの一時ソース位置を相対解決の基準にしない。module内の相対importはそのファイル位置を基準にする。

module一覧はinstance登録時に固定し、本文は各managed task runtimeの開始時にESMとして読み込む。同じtask内ではmodule状態を保持し、run中の再ロードはしない。次task・子taskは新しいruntimeで読み込む。登録関数やclosureをJSON化して別プロセスへ渡さない。

外部hookはmanaged taskのJS runtime内で実行し、既存Runnerによる取消し・回収の対象にする。Goの制御handlerやlock内で外部hookを待たない。同じphaseでは登録順にawaitし、別tool callのhandler同士は並行し得る。

```js
export async function beforeModelRequest(event, api) {
  // 状態の参照、要求限定の情報設定、共通操作APIの利用。
}
export async function afterModelResponse(event, api) {
  // 当該応答のusage等を参照する。最終回答の確定とは別。
}
export async function beforeTool(event, api) {
  // event.toolName / event.arguments。戻り値で引数を置換できる。
}
export async function afterTool(event, api) {
  // 実行事実を参照し、モデルへ返す結果を置換できる。
}
```

初期の外部handlerは上記4種。制御のaccepted/applied等は既存観察イベントとして記録し、それだけで外部hookを起動しない。将来のイベント追加時に発火契約を定義する。

### 論理判断と発火単位

run内に`modelStepId`を持たせ、RPCの往復やwhileの周回とは区別する。Goで識別・状態を管理し、task loopは現在の判断を保持する。

1. 論理判断を開始し、受領済みsteerを元履歴へ適用する。
2. `beforeModelRequest`を登録順に一度ずつ実行する。
3. hook待機中などに受領した新steerは、既存brokerの送信境界で取り込む。steers-only応答では同じ判断のまま履歴を更新し、外部hookは再実行しない。
4. 最新履歴とキー付き動的情報から要求を組み立て、組込みの予算情報を再計算する。ここは副作用のない内部処理であり、外部hookを呼び直さない。
5. brokerがpending確認を通過した時点で要求を確定し、providerへ進む。その後に受領したsteerは次判断または最終確定前の確認で扱う。
6. 完全なモデル応答を得た場合、`afterModelResponse`を応答につき一度実行する。通信失敗には成功応答を捏造せず、失敗イベントを記録する。
7. tool batchなら各callの`beforeTool → 実行 → afterTool`を並列処理する。結果の履歴追加はモデルのcall順とし、batch処理後に次判断へ進む。
8. 最終候補ならafter hookの後に既存`commit_final`でpendingを再確認する。差戻し時は候補assistant・指示を保持し、新しい判断へ進む。同じ応答のafterは繰り返さない。確定回答は既存`agent.final`で観察する。

同じ準備済み要求の通信retryを行う場合も、before/afterを新たに発火する理由にはしない。本計画でprovider retry機能を新設する必要はない。

hook入力にはphase、hook/module識別、invocation/activity ID、groupId、runId、modelStepId、該当するrequestId/toolCallId、role・agentDepth・原因情報を含める。callbackのsnapshotは取得時点を示し、その後に到着するsteerで古くなり得ることを明示する。

tool前の置換引数は同じtoolの引数契約を満たすものとし、tool名/call IDは変更しない。tool後のモデル向け結果と、元の実行成功・失敗・exitCode等を分けて保持・観察する。別toolを追加しても同じhook経路を通せる形にする。

### 失敗・取消し・連鎖

hook例外時は`hook.failed`と取得済みの実行事実を残し、そのtaskを失敗終了する。hookや完了済みtoolを自動再実行しない。並列batchに残る処理は既存task取消しで回収する。開始済みAPI呼出しも所有scopeで追跡する。

interruptはhook実行列の外でcancelを先行する。強制取消し時に外部JSのafter hookが必ず完走する保証は置かず、Goがcancelledを記録する。自己interruptも同じ契約で、受付後のhook完走は保証しない。

同じhookを子孫へ継承して新しい仕事を作る連鎖は、単なるイベント重複とは別。一般に任意のJS方針の停止は保証せず、所属・原因・実行量の観察と任意上限で管理する。既定の孫agent禁止やhook階層禁止は追加しない。

## コンテキスト予算・使用量の通知

runごとに以下を扱う。

- 明示入力予算、既存catalogから取得できるモデルwindowと出所。
- 直前要求の実測入力tokensと対応するrequest ID。
- 次要求の入力概算、算出方式・時点、予算に対する残量・使用率。

未知はnull/未取得とし、0やwindow全量を既定予算として作らない。累積の利用token数、応答上限`maxTokens`、group共有予算とは分ける。キャッシュ済み入力もコンテキスト使用量へ含める。decisionへの独立した入力は親のコンテキストへ加えない。

初期概算は、送信予定のmessages/tools等のJSONのUTF-8 byte量に基づく方式とし、係数・framingの扱いを実装時の小さなestimatorに明記する。正確なprovider token数や、adapter変換後のwire全体と一致する値とは主張しない。予算超過の概算を理由に、この機能だけで自動停止・圧縮は行わない。

現在のmodel requestIdは観察有効時のみ作られ、JS応答に返らないため、常時の識別とusageとの対応付け・JSへの返却を接続する。実測がない応答・失敗は、以前の値を現在要求の実測として表示しない。

標準通知は利用者の`hooks.js`なしでも使える組込み処理とする。不変instructionには通知の意味を説明し、具体値は要求限定の動的情報として一度だけ添える。通知メッセージを元履歴へappendせず、再準備では最新値へ置き換える。任意外部hookを再発火して表示値を更新する方式にはしない。

`--observe`では英語ラベルの短いcontext行を表示し、実測/概算を明示する。JSONLへ同じ値と関連IDを記録し、保存済みログとliveのどちらでも観察できるようにする。`observe_agent`のsnapshot/差分でも必要な所属・予算情報を取得できるようにする。

## 共有上限と停止

初期の集計はprovider要求数（agent/decisionの内訳）、agent起動数、queue task受付数、外部hook invocation数、active agent/task数、取得できるusage・費用、経過時間。

`providerCalls`はNOEがadapterの`Generate`/`Decide`へ進んだ回数。SDK内部のHTTP retryや未知の課金を実測として推測しない。失敗・取消しでも呼出し開始後の枠は戻さない。

`agentStarts`はroot自身を含むinstanceの起動受付数。`queuedTasks`は`queue_agent`で受け付けた追加task数で、spawnの初期taskを重複計数しない。`hookCalls`は外部handlerのinvocationで、内部の要求再計算・標準表示を数えない。既存commandIdの再送で新規仕事を作らなければ追加計数しない。

Goで上限確認・枠の確保・新規操作登録を一体に扱い、並列受付でも残量を共有する。上限NのN個目は受け付け、次を受け付けようとした際に新規実行を開始せず、groupの受付を閉じてroot取消しへ進む。取消し・イベント通知・回収待ちはlock外で行う。

上限による停止理由を保持するため、既存のinstance停止の実処理を共用し、単に公開interrupt handlerへ転送して理由を`interrupted`へ上書きしない。停止によって外部hookを新たに起動しない。root起動前に上限へ達する場合は、実行資源を作らず受付エラーを返す。

手動のgroup停止には既存`interrupt_agent({agentId: rootAgentId})` / `noe interrupt --agent`を使う。runだけの停止は従来どおり対象taskとその子work、instance停止はqueueを含む所有範囲を対象にする。新しいchain専用CLIは作らない。費用・tokenの概算に基づく共有hard limitは初回に含めない。既存CLIの`--timeout`は引き続き利用できる。

## 実装スライス

各スライスは、対応する実装とfocused testを行い、独立したコードレビューの指摘を修正してから次へ進む。reviewでは具体的な利用経路と受入条件を確認し、test件数・制限の多さを目標にしない。

| スライス | 実装の責任・主な対象 | テスト・完了条件 |
| --- | --- | --- |
| **S1：所属・原因・集計の基盤** | Goのgroup/activity/原因情報、instance/task/queue/decisionへの伝播、観察に依存しない要求IDと計数。主に`internal/agent`のHost/Operation/lifecycle/model/decisionとevents | 独立root A/Bの分離、子・孫・keepAliveの次taskの所属、queueの送信元と所属先の区別、観察なしの計数、元のmaxSteps/取消し契約を確認。管理状態を新しいschedulerへ重複保持しないことをレビュー |
| **S2：共通API・module・所有scope** | hook用facade、config/hook pathの正規化と継承、managed taskでのESM読込み、current runへの操作元の束縛。主にJS agent/task、Go runtime/runner/lifecycle | map alias/相対moduleとその依存解決、子の継承/解除、task内状態と次taskの再ロード。渡されたapiと通常import版の両方で、子hookが作った孫を子runのinterruptで回収できることを確認 |
| **S3：4種のhookと論理判断** | モデル前後・一般tool前後の発火、modelStepId、steer再準備、raw/effective値、例外と取消し | 自己steer・複数module経由でも同じ判断のbeforeが一度、新steerは反映。afterからのsteerとcommit_final、並列toolのcall対応/履歴順、hook例外でtoolを再実行しないこと、終了しないhookも停止・回収できることを確認 |
| **S4：標準コンテキスト通知・表示** | contextBudget、測定snapshot、要求限定runtime info、組込み通知、JSONL/observe/observe_agent | 直前実測と次概算の対応、未知値、cache分、provider reasoning/tool往復の保持、通知が履歴へ増えないこと、hook後のsteerで内部通知だけ再計算すること、run/eval/replの観察を確認 |
| **S5：任意共有上限・停止** | root executionLimits、共有受付・計数、group受付閉鎖、既存取消しへの接続、停止理由 | 並列の子孫・queue・hookから同じ残量を使用、再送の二重計数なし、上限後に新workを開始しない、実行中・未開始予約を回収し別rootを継続。深さだけでは捕捉できない次task連鎖も総量で停止できることを確認 |
| **S6：資料・総合検証・実接続・全体レビュー** | README両言語、API/events/observeガイド、instructions、examples、検討メモと実装記録。総合fixture/E2E・実provider harnessを接続 | 下記の総合テスト・実接続を実施し、全体レビューの指摘を解消。実行コマンド・成果物・未確認範囲を実装記録へ保存 |

S2のmodule runnerの小さな呼出確認と、S3の実際のloop接続は分ける。公開hook名・戻り値・snapshotの型はS2で文書化し、S3で後付けの別契約にしない。各スライスの進捗・review結果・残課題は、実装時に`docs/development/execution-hooks-implementation.md`へ一元記録する。

## 総合的なテスト

### fixtureと実NOEプロセスによる結合テスト

モデルの選択に依存しないfixtureで次の利用経路を検証する。

- **所有と因果**：同一CLI内の独立root A/B、Aの子・孫、hookからのspawn、通常import版からのspawn、同instanceのqueueを組み合わせる。JSONLの関連IDと実際の取消し対象が一致する。
- **発火と指示**：モデル前hookが自己steerし、別hookや外部からも準備中にsteerする。providerへ到達し、beforeの副作用は一度、指示は順序どおり入る。最終候補へのafterからのsteerは確定前に適用される。
- **並列toolと結果**：複数callを異なる完了順で実行。hookの識別・raw/effective値・履歴順が対応し、片方のhook失敗で完了済みtoolを再実行しない。現行run_js以外の内部fixture toolでも共通経路を確認し、公開tool登録機能は追加しない。
- **継続と総量**：子孫を作る連鎖と、queueで次taskを作る連鎖の両方を、設定した共有上限で停止する。commandId再送で仕事・枠が重複せず、別rootは進行する。
- **停止・回収**：hookのJSループ、未解決Promise、モデル通信、run_js、所有子プロセスの各実行地点でinterruptまたはCLI取消しを行う。受付だけで成功扱いせず、task/instance終端、子work・プロセス回収、control登録の除去まで確認する。
- **予算と履歴**：複数のモデル/tool往復、reasoning/provider拡張、steer、queueを通し、通知が蓄積せず不変指示が保持される。直前要求ID・usage・次概算の対応、未知値、独立run、cached tokensを照合する。
- **既存利用**：hookなしのrun/eval/repl、既存のagent操作・JSONL・人間表示、import map、Workerの従来の利用、process/ioの終了契約を確認する。Workerからのnoe:agentを今回有効化する変更は含めない。

最終候補に対し、`go test ./...`、`go test -tags=e2e ./...`、`go vet -tags=e2e ./...`を実行する。共有状態を変更したpackageと関連E2Eはrace detectorで確認する。Linux/Darwin/Windowsのamd64/arm64、CGOなしの配布対象ビルドを確認し、native実行とクロスビルドを区別する。各sliceの途中でfull suiteを繰り返すのではなく、focused確認後に最終候補で総合確認する。

### 実プロバイダでの総合テスト（許可済み）

接続条件：

- providerは**`opencode-go`**、modelは**`deepseek-v4.1-flash`**。組込みChat Completions経路と既存Go側認証を使う。effortは明示的な`auto`を基本にする。
- 既存NOEのcredential探索を使う。必要なら許可済みのhjhの同provider key fileを`NOE_OPENCODE_GO_API_KEY_FILE`で参照する。キーの内容をJS・ログ・計画へ書かない。
- 既存の`User-Agent: noe/<version>`と`x-opencode-session`を維持する。同run内の往復でrunIdが固定し、子・孫・次queue taskはそれぞれ別runIdを持つ。
- 今回の許可を他providerやJevの実接続へ拡大しない。decisionの共有計数はfixtureで確認する。Chat以外の既存adapterへの共通処理の回帰もfixtureで確認する。

| ケース | 実施内容 | 実接続で確認すること |
| --- | --- | --- |
| **L1：通知とtool往復** | 標準通知・外部hook付きで、一時ファイルを読み`run_js`で簡単な計算をして回答する | 実usage、実際のreasoning/tool replayと通知の共存、モデル前後/tool前後の発火、回答と計算結果、JSONLと人間表示の対応 |
| **L2：親子・孫・queue** | JS側の明示的な制御とhookのrole条件で、有限の親子・孫を起動し、keepAliveへ追加taskを予約する | 同groupと原因の追跡、各runの独立usage/session、module継承、通常import版を含むcurrent runへの所有帰属、結果取得 |
| **L3：steerとinterrupt** | 実行状況をイベントで確認して追加指示を送り、別の実行ではhook/toolの待機中に停止する | 指示の適用、同判断のhook非重複、最終確定、受付後の実際の回収。テスト用のready通知・hookでタイミングを制御する |
| **L4：共有上限と独立root** | 独立A/Bを起動し、Aでafter hook等から継続を要求して明示共有上限へ到達させる。Bは小さなtaskを完了する | 実providerへ進んだ回数と集計、Aの上限停止と受付閉鎖、Bへの非波及、終端理由と予約・子workの回収 |

無限のagent生成や大きな入力を実providerへ投入する必要はない。連鎖の再現・多量の並列受付・未知usage等はfixtureで確定し、実接続は各ケース数回程度の判断で成立する有限の課題にする。harnessには明示的な実行上限とtimeoutを設定するが、製品の既定値へ持ち込まない。実行待ちや同期は固定sleepだけに頼らず、イベント・ready file・受付receiptで合わせる。

モデルが期待したtoolや回答を選ばない場合は、制御処理の不具合と区別し、イベント・成果物・providerエラーを確認して必要なケースだけ再試行する。実接続で未達の利用経路をfixture成功だけで完了扱いしない。

実接続harnessは通常suiteやCIで自動課金しないopt-inとし、例として`NOE_AGENT_HOOKS_LIVE=1`で有効にする。具体的なtest名と実行コマンドはS6で確定して実装記録へ記載する。今回の許可済みprovider/modelの範囲で実行し、同じ許可を再確認しない。

成果物は一時作業directoryへまとめる。使用したbinaryのversion/commit、設定、fixture/hook/JS、実行コマンド、JSONL、可読ログ、生成ファイル、要求別usage・集計、受付と適用・終端の照合、取消し後のプロセス確認を残す。`docs/development/execution-hooks-opencode-validation.md`へ結果の要点と保存先を記録し、資格情報は保存しない。

## reviewと完了判定

- 各sliceのレビューは目的・変更範囲・具体的な利用経路を示して依頼し、指摘の採否と修正結果を実装記録へ残す。
- S6では全体のコード・API・文書・総合テスト・実接続結果をレビューする。通常の整合確認に加え、発火連鎖・取消し・共有予算の具体的な反例を確認する。
- 再レビューは変更箇所と既存findingの解消を中心とする。新しい重大な経路がなければ探索・full testを無制限に繰り返さない。
- 受入条件、各slice、総合確認、許可済み実接続、全体レビューの必須指摘が完了したときに計画完了とする。既存バイナリの置換・commit・push・releaseは本計画の完了条件に含めない。

## 参照と構想レビューの反映

- [コンテキスト管理・予算通知の検討メモ](../future-candidates.md)
- [現行agent API](../api.md)、[観察イベント](../events.md)、[人間向け観察](../observe.md)、[provider設定](../providers.md)、[モデルカタログ](../model-catalog.md)
- [既存の起動と待受](agent-lifecycle-plan.md)、[制御操作](agent-control-plan.md)、[import map](local-import-map-plan.md)、[process/io](process-io-plan.md)
- hjhの`internal/jsruntime/bootstrap.js`と`docs/design/request-context.md`：tool/hookへ共通APIを渡す構造、要求単位の投影。
- henji-harnessの`v0/hooks/hook_runner.ts`：登録順のawait。`_refs`のPi/DeepSeek Harness/OpenCode：操作APIとイベント、queueと取消し、要求前の変換。固定snapshotの位置付けは参照側`_refs/README.md`に従う。

通常・批判的レビューで確認した、steers-only往復での再発火、hook中の新steer、afterと最終確定、並列tool、hook取消し、managed taskの所有scope、import版APIの帰属を本書へ反映した。参照側の履歴DB、plugin framework、budget式や自動切詰め規則をNOEの要件として一括採用しない。
