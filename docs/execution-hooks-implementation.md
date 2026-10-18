# 実行グループ・hook・コンテキスト通知の実装記録

正本：[実装計画](execution-hooks-plan.md)。開始日：2026-10-18。

## 進捗

| Slice | 状態 | 検証・review |
| --- | --- | --- |
| S1 所属・原因・集計 | 完了 | focused regression成功。独立reviewのP2を修正、変更箇所のraceと再review成功 |
| S2 共通API・module・所有scope | 完了 | agent/runtime suite・focused race成功、独立reviewで必須指摘なし |
| S3 hook・論理判断 | 完了 | phase/取消し/因果のfixture・race成功、P2 2件を修正し再review成功 |
| S4 コンテキスト通知・表示 | 完了 | 通知/全phase/継承/表示/保存観察とrace成功、P2 2件を修正し再review成功 |
| S5 共有上限・停止 | 完了 | 全上限の原子的受付/連鎖/回収とrace成功、P2を修正し再review成功 |
| S6 資料・総合検証・実接続・全体review | 完了 | オフライン総合・race・vet・6対象ビルド・実接続全6case成功。通常/批判的sourceと最終証拠reviewの必須指摘解消 |

各sliceで変更、実行した確認、review指摘と採否、未確認範囲を追記する。
実接続は許可済みの `opencode-go` / `deepseek-v4.1-flash` を使用する。
既存のローカルバイナリは置換せず、検証用バイナリは一時領域へ作成する。

## S1

Go側のinstance/task/操作の所属と因果情報、観察の有無によらない集計・要求識別を実装する。
後続hookからのspawnでも現在のrunへ所有を束縛できるよう、既存の所有経路との接続を確認する。

変更：root agent IDをgroup IDに使い、子孫・queue・model/decision・run_jsへ所属と原因を伝播。
snapshotにgroup集計を追加し、model responseには観察無効時もrequest IDを返す。
providerのstream callbackは観察無効時には従来どおり渡さない。
現在のtaskからのspawnは`op.task.run`を親とし、bootstrapの祖先情報を変更しない。

確認済み：

```sh
go test ./internal/agent -run '^TestExecution|^TestModelRequestIDAndUsageCountersWithoutObservation$|^TestDecisionActivityUsesCurrentOwnerAndGroup$' -count=1 -timeout=30s
```

group分離・子孫への伝播・queue再送の計数、usageと要求ID、decisionの帰属、
現在runの終了による子work回収、存在しない親scopeからのroot生成拒否を確認した。
通常importが自己instanceへqueueするための認可解決はS2で接続する。

既存のOperation/Host取消し、子instance寿命、停止後のbootstrap受付拒否、
生成JS中のsteerとtool順序のselectionも成功（約2秒）。implementerのcombined regressionは
stream無効・accepted final・decision metadataの保持・FIFOを含め成功した。
独立reviewでもfocused selectionと所有・取消し・maxStepsの回帰をrace付きで確認した。

reviewでP2を1件採用：通常終了の排他区間を分割したため、その隙間のinterrupt受付を
終了処理が上書きし得る。終了状態とactive counterを元のlock中で確定する修正と、
変更箇所に限定した再reviewを行う。他の必須指摘はなかった。

修正済み：非keepAliveの通常終了を`finishLocked`で元の排他区間中に確定し、
イベント通知のみlock外へ出す。実際の`serve()`経路で終端通知を待機させ、
その間のinterrupt拒否、終了理由・active計数・group回収を確認した。
再reviewはコード上の競合窓の除去と、追加・関連テストのrace成功を確認し、
必須追加指摘なし。S2へ進めると判断した。

## S2

登録時のmodule一覧固定、既存import mapによるbinding、各task runtimeでの読込み、
hookごとのAPI scope、通常import版のcurrent runへの認可解決を実装する。
4phaseのloop接続はS3、完全なcontext snapshotと標準通知はS4で行う。

結合確認は`internal/runtime/hooks_runtime_test.go`へ追加した。既存のruntime test helperで
実NOE binaryを一時ビルドし、公開APIからqueueと子agentを起動する。
module一覧の設定だけを読むテストではなく、別プロセスで実際に依存をimportし、
各taskでの読込み回数、認証済みtask失敗報告、子の継承/解除を確認する。

実装した共通factoryはprivate ESMの`noe:agent/api`とし、通常importとhookで同じ
引数検証・RPC facadeを使う。private `noe:agent/hooks`の`withHookAPI`は
Goで作る個別OperationへAPIを束縛し、callback終了時に取消し・request joinを行う。
Goのscope開始応答からprovider/model/effort/maxTokens/hooksを受け取り、子の省略設定を継承する。
受付済みの子instanceはcurrent runに所有され、callback returnだけでは停止しない。
通常importのtask自己instanceへの認可も接続し、独立siblingへのアクセスは許さない。

確認済み（親が実行）：

```sh
go test ./internal/runtime -run '^TestHook(Module|ScopedAPI)' -count=1 -timeout=90s
go test ./internal/agent -run '^TestExecutionTaskScopeManagesOwnInstance$' -count=1 -timeout=20s
```

ともに成功。4種のruntime結合確認にはscope終了後のAPI拒否、callback後の子継続、
task終了時の子回収も含む。実装中に見つけたscopeの継承defaults欠落を修正した。
成功callbackのscope終了失敗は伝播し、callback本体が失敗した場合はその例外を優先する。

実装担当は`go test ./internal/agent ./internal/runtime -count=1 -timeout=180s`、
hook/所有関連のfocused raceを実行し成功。独立reviewでもS2のagentテストとruntime結合
テストをrace付きで確認し、対応必須の指摘なし。S3へ進めると判断した。

## S3

Goの論理判断・hook invocationの識別と記録、JSのモデル/tool loopへの接続を分担する。
steerだけのRPC往復では同じ判断を保持し、外部hookを再実行しない。

接続したprivate RPCは`begin_model_step`、metadata付きhook scope開始/終了、
`tool_result`。Goが判断・要求・tool・hookのIDを関連付け、JSは各moduleを順にawaitする。
providerの元assistant objectは保持し、hookには別のJSON snapshotを渡す。
toolは元引数/実行引数と、実行結果/モデル向け結果を区別する。
afterTool例外でも取得済みの結果を記録してから失敗を返し、並列の残処理を回収する。

親が実行した`go test ./internal/runtime -run '^TestHooks' -count=1 -timeout=100s`は成功。
自己steerと複数module、最終候補への追加指示、異なる完了順の並列tool、
afterTool失敗時の結果保持と取消し、run_js以外の内部fixture tool、
CPUループ/未解決Promiseに待機するhookと子instanceのinterrupt回収を確認した。
module読込み中とhook待機中の二つのsteerを順に適用し、モデル要求1回・before1回で
完了するケースも含む。現時点ではcontext APIと標準通知は未接続。

結合中の修正：hook開始イベントのtype欠落、private taskでの明示run帰属、
不正tool名の従来の修復経路保持、共有execution summaryとraw実行事実のfield分離。
steer配送は適用完了イベント経路と区別し、settled channelを二重closeしない。
取消しは先行させ、hook開始の公開完了後に終端を公開する。

独立reviewで因果追跡のP2を2件採用し修正した。hookの操作scopeはinvocation自身を
原因とし、steerは受付・適用・取消しに同じactivity IDと送信元を保持する。
次判断には直前の応答・完了tool・適用済みsteer、実要求にはbefore hookの原因を接続する。
保持は直近stepと未消費の適用steerに限り、過去のgraph全文は持たない。
agentの関連5テスト、runtimeの因果経路2テストをrace付きで確認し、再reviewで必須指摘なし。

## S4

Go側のrunごとの予算・直前実測・次要求概算、要求限定のruntime情報と標準通知を実装する。
人間表示と保存JSONLのsnapshotも同じ`context.updated`を参照する。

実装：`contextBudget`の継承/解除、runごとのsnapshot、hookのcontext API、
`NOE_CONTEXT_STATUS`で識別する要求限定の末尾system通知を接続した。
JSON UTF-8 bytes/4の概算とproviderの直前実測は別項目にし、未知はnullとする。
元systemと会話履歴は保持し、runtime情報は同keyで置換・次判断でリセットする。
人間表示は短い英語context行、保存ログの復元とlive snapshotも同じ値を返す。

親の結合確認：tool往復・self steer・runtime情報置換/リセット・reasoning保持・
cached実測・最新要求のusage未取得を確認。run/eval/replの実CLIで表示とJSONLを確認した。
agent/observe/import mapのE2E selectionは成功。full runtimeは既存の末尾message前提3箇所だけ
失敗し、通知分を分離したassertionへ修正後、該当ケースを再実行して成功した。
runtime/CLI/observationのcontext・steer・診断に対するfocused raceも成功。
独立reviewではP2を2件採用して修正。`run_js`の接続defaultsに入力予算と明示nullを
引き継ぎ、親→run_js→子の省略継承/明示解除を確認した。保存観察の`tool.finished`は
既定で元結果・モデル向け結果のpayloadを省き、`includeOutput:true`で取得する。
実行状態・IDと元JSONLは保持する。変更箇所のraceと再reviewが成功し、必須追加指摘なし。
全phaseでcontext.getが利用できることも結合確認した。最終full suiteはS6で実行する。

## S5

rootで明示した共有上限をgroup全体の受付に適用し、超過時は既存の所有取消し経路で停止する。
独立したrootへ停止を波及させず、上限理由・受付済みworkの回収を観察できるようにする。

実装：root専用`executionLimits`、4種の原子的な受付、group受付閉鎖と既存取消し、
`execution.limit_reached`と停止receipt、group終端で固定する`elapsedMs`を接続した。
省略項目は無制限、0は受付枠0。子の個別上限指定は拒否し、同groupの残量を共用する。
model/decision、queue再送、hook invocation、dequeue/bootstrapを含む後続workの受付を確認する。

実装担当のagent focused selectionとraceは成功し、全4dimensionの並列exact-N受付、
agent+decisionの共有計数、root起動前の0枠拒否、終端の計数・receipt・elapsed固定を確認した。
親のruntime結合4ケースとraceも成功：孫までの起動連鎖と独立rootの継続、
新runtimeへ跨ぐqueue連鎖とcommandId再送、before/after間のhook上限、
並列run_js中のprovider上限と実PID回収。人間の上限表示もfocused test成功。
独立reviewのP2を1件修正した。受付済み要求が開始イベント中に取消された場合も
adapterへ取消し済みcontextで入り、計数と終端記録を揃える。
model/decisionそれぞれのobserverを待機させて取消す回帰テストで
adapter入口・provider計数・取消し終端が各1回となることを確認し、raceと再review成功。
保存runのgroup集計は最後のrun一致イベント時点であることを文書化した。

## S6

全suite・race・配布対象のCGOなしビルド、ドキュメントと例の整合、許可済み実接続、
通常/批判的な全体reviewを最終候補に対して実施する。
検証成果物：`/tmp/noe-execution-hooks-validation`。
基準commit：`6bd0093527242c894f4b9ecaada2c5d924a86969`と今回の未コミット差分。
Go：`go1.27.2 linux/amd64`。既存`./noe`は置換しない。

最終候補に対して以下が成功した：

```sh
go test ./...
NOE_MCP_SDK_BUNDLE=/tmp/noe-process-mcp.wzVT64/mcp-client.bundle.mjs go test -p 1 -tags=e2e ./...
go vet -tags=e2e ./...
```

`unit-final.log`・`e2e-final.log`・`vet-final.log`に記録。
初回のfull確認ではCLI fixture 2箇所の末尾message前提を修正し、該当ケースと最終fullを再実行した。
instructionsに継承項目・要求限定通知・hook・共有上限の意味を反映した。
README両言語・API/events/observe/hookガイドと外部hook例を更新し、
検討メモの通知項目を完了へ移した。自動圧縮は引き続き未実装。

全体race確認も成功：

```sh
GORACE=atexit_sleep_ms=0 NOE_MCP_SDK_BUNDLE=/tmp/noe-process-mcp.wzVT64/mcp-client.bundle.mjs go test -race -p 1 -tags=e2e ./...
```

`race.log`に記録（runtime約47秒、E2E約24秒）。
通常の全体reviewでは`api.execution.get()`の約束と実装の差分を修正し、
現在hookのactivity/親原因/originを返すようにした。callback eventとの同一性を
実runtimeで検証しrace成功、reviewでも確認済み。未解消の必須指摘なし。
実接続・クロスビルドの証拠確認と批判的全体reviewは、この後の記録を参照。

批判的全体reviewのP2を修正：interrupt受付に対象の生成元ではなく、
要求元のhook/script/humanと固有activity IDを記録する。
再送は新しい受付を作らず、取消し先行・受付channel・回収の既存経路を維持した。
hookからのrun/instance自己停止、人間CLIからのrun/instance停止をrace付きで確認し、
独立再reviewでも成功。通常・批判的なsource reviewの未解消必須指摘はない。
修正後の全体チェックもすべて成功した。`unit-reviewed.log`、`e2e-reviewed.log`、
`race-reviewed.log`、`vet-reviewed.log`へ保存した（vetは`-tags='e2e hooks_live'`）。
Linux/Darwin/Windowsそれぞれamd64/arm64の6対象で`CGO_ENABLED=0 go build -trimpath`が成功。
`builds/results.json`と`build-final.log`に記録し、Linux amd64ではnative起動とhook import例も確認した。
他5対象はクロスビルド確認のみ。通常・批判的reviewerがログとartifactのbuild metadataも確認した。

実接続のL1/L2はHTTP 401となり、直接接続でも`api_key_expired`を確認した。
NOEとhjhの既存keyは同一で、利用者へNOE側keyの更新を依頼済み。
中断した所有processのPID消滅は確認したが、L2の最終snapshot照合は未完了。
L3/L4は未実施。詳細と再実行コマンドは[実接続記録](execution-hooks-opencode-validation.md)。

最終の証拠reviewでlive harnessの照合条件3点を修正した：L1は同じ要求内の
assistant tool callとtool resultを対応させ、応答reasoningは次の同session要求と別に照合する。
L3はrun IDのないinstance終端をagent IDで照合してexecution counterの存在も確認する。
L4は初期task受付を追加queueに数えず、指定command IDだけを数える。
これらはテスト側の修正であり、成功した実接続の代替証拠にはしない。
変更範囲に絞った通常・批判的再reviewはいずれも必須追加指摘なし。
opt-in無効のtag付きtestとvetが成功し、実接続が自動実行されないことも確認した。

利用者のkey更新後、許可済みの実接続全6caseを確認した。
L1は実tool往復と4phase・reasoning/usage/通知、L2は親子孫・queue・session分離、
L3は準備中steerとhook/実process treeの取消し、L4は共有上限停止と独立root継続が成功。
最初のtool準備2試行では長いthinking後にstreamが不完全として拒否され、tool開始に至らなかった。
taskの呼出しJSとhookによる置換を具体化し、実process開始後のinterruptと3PID消滅を確認した。
失敗記録・原因未確定の範囲を残し、成功済みcaseの再実行は行わなかった。
`live-retry.log`の先頭4caseと`live-retry-remaining2.log`の残り2caseが成功証拠。
通常・批判的reviewerもイベント・wire receipt・最終counter・PID消滅を確認し、
必須追加指摘なし。詳細は[実接続記録](execution-hooks-opencode-validation.md)。
最後のharness task文変更後も、opt-in無効のtag付きtestとvet・`git diff --check`が成功
（`harness-final.log`・`vet-harness-final.log`）。製品コードは総合確認後に変更していない。
計画の全sliceを完了とする。commit/push/release・既存binary置換は実施していない。
