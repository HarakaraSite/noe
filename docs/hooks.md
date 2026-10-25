# agent hookと実行状況

現行のhook APIとコンテキスト通知の使い方。初期導入時の[実装記録](development/execution-hooks-implementation.md)・[計画](development/execution-hooks-plan.md)は当時の検証履歴であり、例外の扱いは以下の現行契約を参照してください。

## 登録

`run_agent` / `spawn_agent`のconfigに`hooks: ["./hooks.js"]`を指定する。
一覧は登録時に固定し、相対パスはホストのcwd、bare名はimport mapから解決する。
hook moduleからの相対importはそのmoduleの場所を基準にする。
子は省略時に一覧を継承し、明示配列で置換、`[]`で解除する。
module本文と依存moduleは各task開始時に読み込み、そのtask中は再読込みしない。
同じkeepAlive instanceの次taskも、新しいruntimeとmodule状態を使う。会話履歴と最初のtaskで構成したsystem instructionはinstance側に引き継ぐ。

## handler

ESMの名前付きexportで次の関数を定義する（asyncも可）。必要なものだけexportできる。
同じphaseのmoduleは登録順にawaitし、異なるtool callは並行して処理できる。

```js
export async function beforeModelRequest(event, api) {
  const context = await api.context.get();
  await api.context.setRuntimeInfo({key: "project", text: "Use the project conventions."});
}

export async function afterModelResponse(event, api) {
  // event.response: message、finishReason、usage、requestId。
}

export async function beforeTool(event, api) {
  // undefinedは変更なし。置換する場合は同じtoolの引数契約を満たす。
  return {arguments: event.arguments};
}

export async function afterTool(event, api) {
  // モデルへ渡す結果の置換。元の成功/失敗は観察記録に残る。
  return {result: event.result};
}
```

`run_js`が画像を読んだ場合、`afterTool`は`event.images`でそのtool callの画像metadataを受け取ります。画像bytesは含みません。既存の`undefined`と`{result}`は画像候補を保ち、添付を選ぶときは`{result, imageIds}`を返します。

```js
export async function afterTool(event) {
  if (event.toolName !== "run_js") return;
  const imageIds = event.images.slice(0, 1).map(image => image.imageId);
  return {result: event.result, imageIds};
}
```

`imageIds`は現在の`event.images`から選ぶID配列です。`[]`は全候補を除外し、部分選択後も候補の元順序を保ちます。重複、未知・他toolのID、前のhookが除外したIDの再追加はエラーになります。通常のtool `result`の選択とは独立しています。

モデル前後handlerの戻り値は使わず、今回の状態情報は`setRuntimeInfo`で設定する。
tool handlerは`undefined`で変更なし、上記objectで置換する。
tool名・call IDは変更できない。tool完了後の置換は実際の終了状態を書き換えない。
登録済みhandlerの通常の例外・reject・返却値検証エラーは、その呼出しだけの失敗として扱う。後続hookと本体は直前の有効値で続行する。詳細は[失敗の局所化と通知](#失敗の局所化と通知)を参照。

`beforeTool`の`rawArguments`はモデルが返した原文です。JSON toolではJSON文字列、openai-chatgptの生JS toolではJSソース文字列になります。`arguments`はどちらも解析・正規化した`{code: string}`です。
解析できない場合は`null`を渡し、hookが置換しなければ通常のtool入力エラーとして扱う。
`run_js`の置換引数は`{code: string}`。`afterTool`の置換結果はJSONにできる値で、
文字列や要点だけのobjectへまとめることもできる。
handlerへ渡すJSON snapshotの直接変更は、元のprovider応答や他handlerへ反映しない。
変更を採用するには上記の戻り値を使う。

共通eventは`phase`、module/hook識別、`invocationId` / `activityId`、
`groupId`、`agentId`、`runId`、`parentRunId`、`role`、`agentDepth`、
`modelStepId`と原因情報を持つ。対象に応じて`requestId`、`response`、
`toolCallId`、`toolName`、`arguments`、`result`を持つ。
`role`はroot/child、`agentDepth`はrootが0、子が1、孫が2となる。
snapshotは取得時点の情報であり、hook実行中に新しいsteerが届く場合がある。

## 失敗の局所化と通知

**以前の「handlerのthrowでtaskを止める」挙動からの意図的な変更です。hookは検査・承認や実行見送りの機構ではありません。**

| 失敗したhandler | 継続する処理 | 失敗通知を載せる通常モデル要求 |
| --- | --- | --- |
| `beforeModelRequest` | 後続hook、コンテキスト最終準備、元のモデル要求 | その要求 |
| `afterModelResponse`（tool付き応答） | 後続hook、応答内toolの実行 | 次の通常要求 |
| `beforeTool` | 後続hook、直前の有効な引数で元のtoolを実行 | 次の通常要求 |
| `afterTool` | 後続hook、直前の有効な結果・画像で確定 | 次の通常要求 |
| `afterModelResponse`（最終応答） | 後続hook、既存の最終確定処理 | 次の通常要求がある場合だけ |

- 捕捉対象は登録handler本体と返却値検証です。ファイルI/O・コマンドの失敗、JSON化できない置換値、不正な引数・画像選択なども、handlerから外へ出た場合はそのhookの失敗になります。handler自身が捕捉して処理した例外を追加のhook失敗とは推定しません。
- 失敗したhookの未成立の置換だけを不採用とし、先行hookの有効な引数・結果・画像選択を保ちます。`beforeTool`失敗でtoolをskipしません。先行置換がなければ元の引数で実行し、その引数自体が不正なら通常のtool入力エラーです。
- 完了したファイル変更、`setRuntimeInfo`、子agent起動等の副作用を巻き戻しません。子の所有・回収規則も変えません。完了済みhookやtoolの自動再実行はせず、並列toolの他方をhook失敗だけで取り消しません。
- hook設定の形式、module解決・読込み・export定義の準備失敗は従来どおり開始時エラーです。取消し、既存実行上限、scope/Hostの継続不能も通常のhook失敗には変換せず、本体の終了経路へ伝えます。scope終了障害はhandler例外と重なっても隠しません。

### モデルへの同送と保持結果

失敗は`hook.failed`イベントに残し、run所有の記録から既存の`NOE_CONTEXT_STATUS` user message内の独立した事実通知として同送します。JSONL保存・人間向け観察が無効でも動作します。人間のsteerや修復命令ではなく、`wait_tasks`を起こすsteerにもなりません。

通知本文も最終概算・準備に含め、実provider要求の受付時にその本文に含まれたinvocationだけを送付済みにします。`api.context.get()`・観察・準備では消費せず、steer-onlyの再準備でも保持します。同じ失敗を新規通知として二重挿入しません。送付済み本文は対応する応答・tool結果と同じ通常blockで保存・trimされ、未送付記録はtrim対象の履歴だけには置きません。

**hook失敗だけを理由に最終確定を拒否したり、モデル要求を追加したりしません。** 最終応答後など、次の通常要求がなければモデルが通知を読む保証はありません。その場合も失敗はtaskの保持結果に残ります。実際のsteerで次の判断へ進む場合は、その通常要求へ同送します。

`TaskOutcome.hookFailures`は回復した失敗の任意配列で、失敗がなければ省略します。`state: "completed"`と両立し、`answer`・本体の`error`とは別です。`run_agent` / `observe_agent`の`lastResult`、`wait_task`、`wait_tasks`のoutcomeで取得できます。

| HookFailureのfield | 意味 |
| --- | --- |
| `invocationId` | `hook.failed`と対応する呼出しID |
| `hookIndex` / `module` / `phase` | 登録一覧の0始まりindex、解決済みmodule、4phaseのいずれか |
| `modelStepId` | 発生した論理判断 |
| `requestId?` | 発生元のprovider要求。モデル前hookでは未設定 |
| `toolCallId?` / `toolName?` | tool hookの場合の発生元 |
| `error` | printableなエラー説明。stack等の詳細は診断イベントを参照 |
| `notifiedRequestId?` | 通知を載せたprovider要求ID。未設定は未送付。発生元`requestId`とは別 |

「送付済み」は要求へ載せたことを表し、モデルが理解・解決した保証ではありません。要求自体が失敗した場合も、その受付済み要求IDを保持します。

`includeAnswer: false`でも`hookFailures`は取得できます。`read_task_result`は回答原文の範囲取得のままで、警告を回答へ追記しません。確定後のoutcomeは後続taskにより書き換えません。同じkeepAlive instanceの次queue taskは通常の会話prefixを継続しますが、旧taskの未送付失敗を新規通知として自動移送しません。取消しや別の本体エラーで終わった場合も、すでに確定した回復済み失敗は可能な範囲で元taskのoutcomeに残します。

```js
import { run_agent } from "noe:agent";

const snapshot = await run_agent({
  provider: "openrouter", model: "provider/model-id", effort: "auto",
  hooks: ["./hooks.js"],
}, "依頼本文");
const outcome = snapshot.lastResult;
console.log(outcome?.state, outcome?.answer);
for (const failure of outcome?.hookFailures ?? []) {
  console.log(failure.phase, failure.error,
    failure.notifiedRequestId ?? "未送付");
}
```

エラー説明にはpath・コマンド出力・利用者の値などが含まれ得ます。hookの診断ログだけでなく、モデルへの通知・保持結果にも含まれるため、外部送信や保存・共有に適した内容か確認してください。自動redaction機能ではありません。

## hook用API

`api.agents`は`run_agent`、`spawn_agent`、`observe_agent`、`queue_agent`、`wait_task`、`wait_tasks`、`read_task_result`、
`steer_agent`、`cancel_task`、`stop_agent`、`list_models`を持つ。
引数・戻り値・受付と完了の区別は[通常のagent API](api.md)と同じ。
各hookの呼出元へ束縛され、同時実行中の別hookの原因情報と混ざらない。
通常の`noe:agent` importも利用でき、現在のtaskに所有されるが、正確なhook原因が
必要な操作は渡された`api.agents`を使う。

`await api.context.get()`は現在のrunの予算・直前実測・次要求の概算を取得する。
`await api.execution.get()`は所有group・原因・集計・任意上限を取得する。
group summaryの`groupId`・`counters`・`limits`・`elapsedMs`・任意`usage`/`stop`に加え、
現在handlerの`activityId`・`causedBy`・`origin`を返す。これらはcallback eventと同じinvocationを指す。

`contextBudget: {inputTokens: 200000}`は固定入力を含む総入力の目安を明示する設定。
子で省略すると継承し、`null`で解除する。実行履歴だけの予算とは別の尺度としてsnapshotに残る。
`contextPolicy`は省略または`{}`なら自動切り捨てを有効にし、子では省略時に継承する。
`contextPolicy: null`は切り捨てを無効化する設定で、context通知は続ける。
`retainRatio`は`0 < ratio < 1`で指定し、既定値は`0.1`。任意の`responseReserveTokens`は正のsafe integerで、window由来の予算計算にだけ使い、providerの出力上限は変更しない。
正の`maxTokens`も応答予約として扱い、両方指定した場合は大きい方を使う。どちらもなければcatalogの`maxOutputTokens`が利用可能な場合に使う。
windowが不明で明示予算もなければ、NOEは総入力目安として1,000,000 tokensを使うが、モデルwindowの値とは扱わない。
同じinstanceの次queue taskにもpolicy設定を保ち、trim後の会話履歴・trim回数・直近のtrim情報も続ける。
各taskのrun IDと実行runtimeは新しく、hook moduleも再読込みする。新しいagent instanceは独立したsystem・会話・trim状態を持つ。
catalogのwindowは別項目で、未知なら未取得のまま。既知windowの代わりにfallback値を使ってcapacityを保証しない。

windowと応答予約から予算を計算する場合、次の記号を使う。

| 記号 | 意味 |
| --- | --- |
| `W` | catalogで確認できたモデルwindow |
| `F` | 保持するsystem・元task・適用済みsteer・tool定義の概算 |
| `N` | 今回だけのruntime情報とcontext通知の概算 |
| `R` | 応答用token予約 |

`W`と`R`が既知の場合、window由来の実行履歴予算は
`max(0, floor((W - F - N - R) × 0.9))`。window由来の総入力目安は`F + N + 実行履歴予算`で、
明示`contextBudget.inputTokens`を`I`とすると、実効総入力目安は`min(I, window由来の総入力目安)`。
`I`がなければwindow由来の総入力目安をそのまま使い、実効総入力目安から`F`と`N`を引いて実行履歴予算を得る。
どちらの場合も、実行履歴予算は`max(0, 実効総入力目安 - F - N)`。
`W`は既知でも`R`が不明なら、総入力目安を`min(明示予算または1,000,000, floor(W × 0.9))`とし、
そこから`F`と`N`を引いて実行履歴予算を出す。この場合も実行履歴予算は`max(0, 総入力目安 - F - N)`で、
`responseReserveUnknown: true`をsnapshotに残す。
`W`自体が不明ならwindow由来の計算はせず、明示予算または1,000,000-token fallbackを総入力目安にする。

context snapshotの形は次のとおり（数値は例）。

```js
{
  runId: "...", modelStepId: "...",
  budget: {inputTokens: 200000},
  modelWindow: {tokens: null, source: null},
  previous: {
    requestId: "...", status: "completed",
    promptTokens: 12500, cachedTokens: 10000, measuredAt: "..."
  },
  estimate: {
    tokens: 13000, method: "json_utf8_bytes/4", stage: "preparing",
    modelStepId: "...", requestId: null, estimatedAt: "..."
  },
  executionContext: {
    policy: "auto", budgetSource: "input_budget",
    fixedTokens: 12000, transientTokens: 300,
    responseReserveTokens: null, responseReserveUnknown: true,
    estimatedTokens: 700, budgetTokens: 187700,
    totalInputBudgetTokens: 200000, usedPercent: 0.37,
    nearBudget: false, budgetExceeded: false, targetUnreachable: false,
    trimCount: 1,
    lastTrim: {id: "...", beforeTokens: 905000, afterTokens: 700, removedBlocks: 12}
  },
  remainingTokens: 187000, usedPercent: 6.5, updatedAt: "..."
}
```

`executionContext.budgetTokens`は可変の実行履歴に対する予算、
`totalInputBudgetTokens`は固定入力を含む総入力目安。既存の`budget.inputTokens`は後者の尺度である。
`budgetSource`は`model_window`・`input_budget`・`combined`・`fallback_window_cap`・`fallback`のいずれか。
切り捨て後は再計算したsnapshotが要求へ送られる。`context.trimmed`には前後の概算、削除block数、理由、source、
`activityId`・`causedBy`・`origin`が記録される。削除したtool本文やreasoning本文はsnapshotやtrim eventから復元しない。

明示予算・window・実測の未知値は`null`。最初の要求前は`previous: null`。
実測が返らない要求や失敗を、前要求の数値で補わない。
promptにはcached分も含む。概算はmessages/toolsのJSON UTF-8 byte量を4で割る近似で、
providerのtokenizerやadapter変換後のwire量とは一致しない。
`preparing`は判断準備時、`sent`は実要求の送信時を示す。
応答後のsnapshotは最新の送信概算を`sent`として保持し、次の判断を準備すると置き換える。
これらの内部更新ごとに観察イベントは発火しません。`context.updated`は実際のモデル要求の
正常終了・失敗・取消し時に1回だけ発火します。hookの`api.context.get()`とlive snapshotは、
通知の有無にかかわらず各時点の最新状態を返します。実際に適用した切り捨ては
`context.trimmed`として別途通知します。

画像を含む場合、`estimate.imageTokens`は画像分の概算を別に示し、`estimate.method`は
`json_utf8_bytes/4+image_32px_patches_x1.2_fallback`になります。画像の推定はtokenizerや課金額の実測ではなく、`low` detailでは最大辺512pxに収めた寸法、それ以外では原寸を使います。式の詳細は[画像ガイド](agent-images.md)を参照してください。

標準通知は外部hookなしでもモデル要求へ新しいuser messageとして一つ添える。
`Execution context: ~使用量 / 予算 tokens`の分母が、実行履歴の計算済み予算と自動切り捨て有効時の閾値。
`Input budget`は計算済みの総入力予算、`Explicit input budget`は手動指定した予算、`Model context window`はcatalogの値を表す。window不明でも計算済み予算を通知する。
`source=fallback`はNOEの既定予算を示し、モデルの実際のwindowを示さない。この区別を組込みinstructionと毎回の通知にも記載する。
通知は対応するassistant応答・tool結果と同じ完了blockに保持し、過去の通知を書き換えない。自動trim時もそのblock単位で削除する。固定instructionには追加しない。
各runのprovider使用量は独立して計測し、同じinstanceの次queue taskも新しいrun IDを持つ。会話のcontext trim countはinstance内で続く。

`await api.context.setRuntimeInfo({key, text})`は`beforeModelRequest`で利用できる。
同じkeyは置換し、今回の論理判断の新しい通知に添える。その通知は応答後に履歴へ保持され、過去の通知にある同じkeyの内容は書き換えない。
hook後に新しいsteerが届いて要求を再準備しても、外部handlerは再実行せず、
設定済み情報を保持して組込みの予算情報だけを再計算する。

## 発火と停止

`beforeModelRequest`は一つの論理判断につき一度。steer配送だけのRPC往復や
同じ準備済み要求の通信retryで再実行しない。
`afterModelResponse`は実際に完了したモデル応答につき一度であり、最終回答の確定とは別。
afterから追加されたsteerは最終確定前の確認で扱う。

`beforeTool` / `afterTool`は個々のtool callに対応する。並列実行しても履歴への
結果追加はモデルが返したcall順。現在のtoolは`run_js`。

`afterTool`はモデル向け予算を適用する前の結果を受け取ります。加工後のJSON全体（metadata・JSONエスケープを含む）に既定64 KiBの予算を適用し、超過時は不完全なpreviewへ変換します。outputBytesはstream捕捉にも連動し、最大256 KiBです。詳細は[実行結果](execution-results.md)を参照してください。

`tool.finished`には元の`rawArguments` / `rawResult`、実行用の`arguments`と
モデル向け`result`を分けて記録する。`run_js`の画像候補は`rawImages`、hook選択後は`images`へ
metadataのみを記録する。加工値が元結果と異なり、preview化で失われる場合だけ`hookResult`にも制限前の加工値を保存する。回復した`afterTool`失敗は直前の有効な結果・画像で通常の`tool.finished`を確定し、`modelResultAvailable: true`を保つため、`hookError`を立てて画像を破棄しない。失敗の事実は`hook.failed`と`hookFailures`で区別する。継続不能により結果を確定できない場合の`modelResultAvailable: false` / `hookError`や、過去ログの同項目とは別である。hookが明示的に除外した画像は`images`に含めない。
既存の`run_js.started` / `run_js.finished`は、実際の実行ソースと成功・失敗を示す。

interruptはhandlerの完了を待たずに取消しを開始する。終了しないJSやPromiseも
所有するtask runtimeの取消し対象となる。強制取消し時はafter handlerの完走を保証しない。
handlerがspawnしたagentの寿命は現在のrunに属し、handlerのreturnだけでは停止しない。

## 所属と集計

一つのroot agentと所有下の子・孫・予約は同じ`groupId`を持つ。
rootの`agentId`が`groupId`であり、同じNOE内の独立した親agentは別groupになる。
同じkeepAlive instanceの次taskも同じgroupで、run IDは新しくなる。
会話履歴は前taskの最終回答を含む確定済み・trim後の内容を引き継ぐ。
取消しは所有関係に従い、原因の参照を辿って別groupへ波及させない。

JSONLとlive snapshotの`execution`は、group IDと`counters`、取得できた累積`usage`を持つ。
counterは`providerCalls`、`agentProviderCalls`、`decisionProviderCalls`、`agentStarts`、
`queuedTasks`、`hookCalls`、`activeAgents`、`activeTasks`。agentStartsはrootを含み、queuedTasksは
明示的なqueue受付だけを数える。観察を無効にしても集計する。
取得できなかったusage値は0として補わず、省略する。
個々の要求のコンテキスト使用量は、group累計のusageとは別に扱う。

## 任意の共有上限

rootのconfigに`executionLimits`を指定すると、子孫・hook・次queue taskも同じ残量を使う。
子の起動configからの上書きはできない。設定例の数値は利用者が選ぶ値で、既定ではない。

```js
executionLimits: {providerCalls: 100, agentStarts: 8, queuedTasks: 16, hookCalls: 1000}
```

省略項目は無制限、明示した`0`は受付枠0。`maxSteps:0`が無制限である契約とは別。
providerCallsはagentのGenerateと所有下のdecisionのDecideを共用し、同じGenerate内のHTTP retryは数えない。組込みagent生成は最大3試行・2秒／4秒待機で再送し、hookとtoolは再実行しない。詳細は[providerガイド](providers.md#モデル要求の再試行)を参照。
開始後の失敗・取消しで枠を戻さない。queueの同commandId再送は二重計数しない。
N件目は受け付け、その次の受付でgroupを閉じ、rootと所有workを取消す。
上限に達した瞬間に成功したN件目を停止する方式ではない。

`execution`には`limits`、`elapsedMs`、上限停止時の`stop`が加わる。
`stop`は`{reason:"execution_limit",limitName,limit,used,source}`で、原因のactivity/originを参照できる。
停止理由は通常のinterruptと区別し、別groupへ取消しを波及させない。
`execution.limit_reached`は上限の種類と使用数を記録する。
受付や停止開始のイベントは回収完了を意味しないため、instanceの終端とactive counterを確認する。
agentStartsが0のrootは実行資源の作成前に受付エラーとなる。
