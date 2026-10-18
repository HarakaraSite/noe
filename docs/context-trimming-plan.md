# 実行コンテキストの切り捨て・使用量通知の実装計画

状態：2026-10-18、利用者が実装・テスト・レビュー・実プロバイダ確認を承認し、全スライスの実装・確認を完了。
実装前の基準はv1.5.0（HEAD `4cc43f2`）。実装結果は[実装記録](context-trimming-implementation.md)へ残す。
要件の履歴は[検討メモ](future-candidates.md)、現行APIは[hook・contextガイド](hooks.md)と[インストラクションガイド](instructions.md)。

## 目的と合意

長い自律taskを、毎回すべての実行履歴を送り続けずに継続できるようにする。
モデルは予算と切り捨て方針を把握し、有用な場合だけ自らファイルへ情報を保存できる。

- **切り捨て対象は`noe:agent`の履歴を持つモデル要求だけ。Jevなど`noe:decision`の単発要求は対象外。** 親がdecisionを呼ぶ場合もdecisionへのstateを加工しない。その結果が親のtool履歴へ入った後は、親の通常の履歴として扱う。
- 不変インストラクション、元のタスク、適用済みの追加指示を保持する。
- tool call・対応する全結果・reasoningなどを分断せず、古い完了済みのまとまりから落とす。
- 要約モデル、保存専用の追加モデル要求、必須の退避作業は設けない。保存しないモデルも正常に継続できる。
- 毎要求の直前に推定使用量／予算を通知する。切り捨て発生時は**切り捨て後の履歴と再計算した通知**を送る。
- window既知時は、固定入力と応答用予約を引いた残りの約90%を実行履歴の目安にする。
- **window不明・予算未指定時のNOE既定入力予算は1,000,000 tokens。** 利用者は100,000案を小さすぎるとして退け、1,000,000案を採用した。これは運用上の入力予算であり、モデルのwindowが100万だと推定・保証するものではない。
- 発動後は実行履歴を切り捨て前の約1/10まで減らし、再び増える余地を作る。まず10%を既定とし、実験で20%も比較できるようにする。

## 現行実装との接続

| 現行 | 今回の変更 |
| --- | --- |
| `js/task.js`の配列へsystem、初期user、assistant/tool、steerを追記 | 保護するentryと削除可能な完了blockを明示して管理する |
| `context_prepare`で概算、`context_get`で要求限定通知 | hook後にも最終準備を行い、必要なら削除planを作り、適用後に通知を確定する |
| `contextBudget.inputTokens`は入力全体の目安 | 意味・既存snapshotの尺度を維持し、実行履歴だけの有効予算を別項目で扱う |
| `model`でpending steerがあればproviderを呼ばず返す | steerを保護entryへ追加して再準備。外部before hookは再実行しない |
| reasoning/provider拡張を含むassistant objectを保持 | 残すobjectを再構成・要約せず、そのまま残す |
| `context.updated`、live/saved snapshot、人間表示 | 有効予算・切り捨ての結果も同じsnapshotへ接続する |

モデルカタログには`contextWindow`・`maxOutputTokens`を表す型があるが、現行組込みJSONは値を持たない。
値を推測で埋めず、確認できたモデルだけprovider別の公式仕様に基づいて追加する。
同じモデル名でもprovider側で提供される上限を優先し、外部JSONでの上書きを維持する。
カタログを取得するための有料要求や、実行時の自動モデル探索は追加しない。

## 設定と予算

既存の`contextBudget.inputTokens`は**固定入力込みの総入力予算**のままにする。
これを「実行履歴だけの予算」へ読み替えない。`contextBudget:null`も既存どおり明示予算の解除であり、自動切り捨ての無効化とは区別する。

追加する設定案：

```js
contextPolicy: {
  retainRatio: 0.1,
  responseReserveTokens: 8192, // 任意。未指定時は下記の既存設定・カタログから解決
}
```

- `contextPolicy`省略は自動管理。空objectも既定方針を使う。
- `contextPolicy:null`は切り捨てを無効化し、通知と既存の目安表示を続ける。比較実験にも使う。
- `retainRatio`は`0 < ratio < 1`、既定0.1。token数の引数は正のsafe integerとする。
- `responseReserveTokens`は予算計算用であり、それだけでproviderの出力上限を変更しない。
- 子は明示したpolicy（nullを含む）を省略時に継承する。実効予算や親の履歴は継承せず、子のモデル・固定入力で再計算する。
- 同じinstanceの次queue taskは設定を保持し、履歴・trim回数は新runでリセットする。
- `contextBudget`を指定したrunは、policyが有効ならその入力目安を切り捨ての発動に使う。v1.5.0の「通知のみ」を選ぶには`contextPolicy:null`とすることを移行説明へ明記する。
- 最初の90%の余裕率、80%の注意表示は内部の名前付き定数とし、今回必要のない公開optionを増やさない。

### 計算の定義

```text
W = カタログで確認できたモデルwindow（不明ならnull）
F = 保護するsystem・元タスク・適用済みsteerとtool定義の推定量
N = 今回限定のruntime情報とcontext通知の推定量
R = 応答用予約
E = 削除可能な実行履歴の推定量
```

Rは、明示policy値と正の`maxTokens`が両方ある場合は大きい方、片方ならその値を使う。
両方未指定ならカタログの正の`maxOutputTokens`を使い、なければ未知として保持する。
`maxTokens:0`を「出力予約0」「実出力0」へ読み替えない。reasoningを含む実応答のtoken制約は各adapterの現行契約に従う。

WとRが既知なら：

```text
window由来の実行予算 = max(0, floor((W - F - N - R) × 0.9))
window由来の総入力目安 = F + N + window由来の実行予算
```

総入力目安の選択：

| 状況 | 有効な総入力目安 |
| --- | --- |
| 明示`inputTokens`あり、W/R既知 | 明示値とwindow由来の総入力目安の小さい方 |
| 明示値なし、W/R既知 | window由来の総入力目安 |
| W不明 | 明示`inputTokens`、なければNOE既定1,000,000 |
| W既知・R不明 | 明示`inputTokens`、なければNOE既定1,000,000を出発点に、`floor(W × 0.9)`以下にする。R不明の暫定方針と表示する |

```text
実行予算 B = max(0, 有効な総入力目安 - F - N)
実行使用率 = E / B（Bが0なら除算せず状態で表す）
発動 = E > 0 かつ E >= B
保持の目標 = 切り捨て前のE × retainRatio
```

未知時の既定値を使った場合も`modelWindow.tokens`はnullのままにし、予算のsourceを明示する。
既知のwindowが小さい場合を、100万のfallbackで上書きしない。
固定入力込みの現在の`estimate.tokens`・`budget.inputTokens`・`usedPercent`と、E/Bを混同しない。

概算は既存の`json_utf8_bytes/4`を使う。cached tokensも入力であり、概算や予算から引かない。
前回実測は前回のrequest IDへ紐付け、今回の切り捨て後の推定へ流用しない。
数値はprovider tokenizerやadapter変換後のwireとの完全一致を主張しない。
通知自身の長さは既存の有限回の再計算を再利用し、境界付近で切り捨てを無限反復しない。

## 履歴の構造と切り捨て

### 保護するentry

- 組込み・共通・workspace・明示systemを結合した元のsystem全文。
- 元のタスクuserメッセージ。
- 一度適用したsteer全文。command IDで二重追加を防ぎ、元のroleと適用順を保持する。
- 現在有効なtool定義。実行履歴の削除で定義を書き換えない。

保護を文字列の内容やroleだけで推測せず、task loopがentryを追加する地点で分類する。
後からuser roleをすべてsystemへ昇格させたり、モデルに「重要か」を選別させたりしない。
追加指示を意味的に失効させる仕組みは今回追加せず、適用済みsteerはすべて保持する。

### 削除可能なblock

1回のassistant応答と、そのtool callに対応する**全tool結果**を一つのblockにする。
並列toolの完了順ではなく、元のtool call順に結果を保持する。
reasoning・署名・provider固有field・native response itemもassistant objectの一部として扱う。
toolのない最終候補が追加steerによって未確定となった場合、そのassistant候補は単独の完了blockにする。

古いblockから削除し、原則として目標量以下になる最長の直近suffixを残す。
少なくとも直近1個の完了blockは残す。モデル/toolの実行途中、未完了callの一部だけを削除しない。
保護entryは元の相対順を維持し、残したblockとの時系列を壊さない。

直近blockが単独で大きすぎる場合や、固定入力だけで目安を超える場合は、目標量を満たせないことがある。
この場合もsystem・追加指示・toolの組を壊さない。削れる過去blockだけ落とし、`budgetExceeded`/
`targetUnreachable`を通知して既存のprovider要求へ進む。予算を新しいtask停止上限にはしない。
削除対象がない時に同じ履歴へ切り捨てを繰り返さず、providerが拒否した場合は通常の失敗として報告する。
巨大なtool結果の部分切断・自動ファイル化は今回の範囲外。

削除済みblockはJSの保持履歴からも外す。全文を裏の配列やGoのrun stateへ蓄積しない。
元の観察イベント・JSONLは編集せず、既存の出力保存方針を維持する。ログなしで削除した履歴の復元は保証しない。

## 要求前の順序とhook・steer

```text
前の応答と全tool結果を確定
→ begin_model_stepで保留steerを受取り、保護entryへ適用
→ contextを仮準備
→ 外部beforeModelRequestを1回だけ実行
→ hookのruntime情報を含めて最終入力を推定
→ 必要なら切り捨てplanを作る
→ JSで削減候補を準備（この時点では元履歴を変更しない）
→ ホストで候補revision・digestを検証し、要求用履歴と適用記録を確定
→ JSの保持履歴を確定した候補へ置換
→ 削減後の使用量／予算と今回の切り捨て情報を通知へ添える
→ model要求（pending steerなら未送信で戻り、同stepを再準備）
→ 実際にproviderへ送信したrequest IDと送信概算を記録
```

切り捨ては外部hookをもう一度呼ぶ理由にしない。steer-only往復もmaxSteps/providerCallsを消費しない。
準備中に届いた新しいsteerは既存の配送経路で追加し、同stepの最終準備・予算計算をやり直す。
既に削除した履歴を古い配列から復活させない。runtime情報は同stepでは保持し、次stepでリセットする。

今回の切り捨て通知と原因IDは、最初の実provider送信まで保留する。`context_get`・再準備・
steer-only応答では消費しない。同step内で複数回削減した場合は未送信の削減をまとめて通知し、
原因IDを重複なく引き継ぎ、実provider要求の受付時に消費する。送信前にinterruptされた場合も、
実際に削除済みの`context.trimmed`イベントは事実として残す。

Goが予算解決・概算・削除plan・観察snapshotを担当し、JSが実際の履歴とblockを所有する。
既存`context_prepare`の拡張とprivateな適用確認経路で接続する。planにはrun内の安定block IDを使い、
JSが準備した削減候補をホストが要求用履歴として確定した時だけ`context.trimmed`を記録する。
JSは成功応答後に保持履歴を同期的に候補へ置換し、それまでモデル要求を行わない。
ホスト確定前の取消しでは元履歴が未変更、確定後の取消しでは削減済みの要求用snapshotと事実が残る。
これはS3レビューで確認した「JS削除直後の取消しによる記録欠落」を避ける実装時の順序修正である。
Goに残すのは適用待ちplanのID・期待revision/digestと集計量だけにする。
無効なplan・古いrevisionは適用せず、同じplanの再送を二重の切り捨てにしない。
適用後の確認を終えるまでproviderへ送らない。取消し時は既存scopeとrequest joinで回収する。
履歴の任意置換APIをhookやモデルへ公開しない。decision handlerにはこの経路を接続しない。

## モデル通知と保存

有効なpolicyでは、最初の要求から「古い実行履歴が自動的に落ちる」方針を知らせる。
不変インストラクションに短い一般説明、毎要求の`NOE_CONTEXT_STATUS`に推定値を置く。

```text
Execution context: ~720,000 / 900,000 tokens (80%).
Older completed history may be discarded at the budget threshold.
Save information only if useful for the task; saving is optional.
```

80%以上で注意文を添えるが、保存をtaskや完了条件にしない。保存しない選択も正常とする。
長いメモ作成、全履歴の退避、保存専用turnを促さない。NOEがtmpファイルを自動作成することもない。
モデルが任意に保存する例では、保存先をtask/systemで既知の場所にしておけば再参照できる。
保存先の追跡API・自動リンク抽出・固定checkpointファイル・自動削除は今回追加しない。

切り捨てが発生した準備では、再推定後の値と事実を追加する。

```text
Execution context: ~88,000 / 900,000 tokens (9.8%).
Context trimmed: ~905,000 -> ~88,000 tokens; older execution history was removed.
```

大きなtool結果で一度に閾値を超えると、事前保存の機会がない場合もある。
そのため保存成功を保証せず、保存を待って切り捨てを延期する処理も入れない。
通知は要求限定で毎回置換し、会話履歴へ蓄積しない。
現行通知の「履歴を変更しない」という説明はpolicy有効時には使わず、自動削除の方針へ更新する。
明示した入力予算とfallback/計算で得た有効予算を同じものとして表示せず、sourceと両者の尺度を明記する。

## snapshotと観察

既存context snapshotへ`executionContext`を追加する案とする。既存fieldは尺度を維持する。

```js
executionContext: {
  policy: "auto", // またはdisabled
  budgetSource: "model_window", // input_budget / combined / fallback等
  fixedTokens: 12000,
  transientTokens: 300,
  responseReserveTokens: 8192, // 未知はnull
  estimatedTokens: 88000,
  budgetTokens: 900000,
  usedPercent: 9.8,
  nearBudget: false,
  budgetExceeded: false,
  targetUnreachable: false,
  trimCount: 1,
  lastTrim: { id: "...", beforeTokens: 905000, afterTokens: 88000, removedBlocks: 12 }
}
```

値は形式を示す例であり、特定モデルのcapacityではない。
`lastTrim`は最後の事実をsnapshotに保持し、今回の通知に出す切り捨ての事実とは区別する。
今回通知する情報は実provider送信まで保留し、snapshotの参照やsteer-only往復では消費しない。
既存のprevious usageは別requestの実測として保持する。親・子・queueの値を合算してrun使用量にしない。

`context.trimmed`はrun/modelStepと固有activity ID、前後の概算・削除block数・理由・予算sourceを持つ。
実際の適用後に一度だけ記録し、その後のモデル要求の原因へ接続する。本文・reasoning全文はイベントへ追加しない。
`--observe`/`noe observe`では英語の短い使用量／予算行と、発生時だけ前後の削減行を表示する。
live/saved `observe_agent`も同じcontext snapshotを返す。保存ログから消した本文を推測復元しない。

## 実装スライス

各sliceで実装・focused test・独立コードレビューを行い、必須指摘を解消して次へ進む。

| Slice | 主な作業 | 完了確認 |
| --- | --- | --- |
| **S1：policyと予算計算** | Config/JS検証/defaults/Connectionへpolicyを接続。既存input予算と可変予算を分離。W/R/source解決とmetadata確認 | 既知/未知window、100万fallback、明示予算、R不明、固定入力過大、継承/null、モデル変更の再計算、rounding/数値境界 |
| **S2：履歴の単位と保護** | private JS履歴manager、初期指示・steer・応答・並列toolの追加経路を統合。純粋な削除plan適用 | 同名/複数steerの順序、command再送、並列tool順序、全provider field保持、直近block保護、削除した本文を保持しないこと |
| **S3：要求直前の切り捨て** | context準備の最終段階、plan/適用確認、閾値・直近suffix・provider送信を接続 | 完全block削除、複数回発動、hook後の増量、steer-only再準備と非重複、古いplan/再送、取消し、削減不能時の進行 |
| **S4：通知と観察** | executionContext snapshot、任意保存の案内、前後の通知、context.trimmed・cause・人間/live/saved表示 | 送信payloadが削減後、通知1件、非蓄積、前回実測との区別、source/未知/予算超過、保存なしでも進行、再観察の一致 |
| **S5：総合回帰と資料** | README/API/hooks/instructions/events/observe・モデル向け説明・examples・検討メモ・実装記録 | 全unit/E2E/vet、関連race、CGOなし6target build、nativeとcross区別、既存hookなし・policy無効化・run/eval/repl・親子queue・decision回帰 |
| **S6：実モデルの挙動評価** | 小さめの明示予算で有限の長いtaskを実行し、保存判断と継続を観察。全体reviewと結果記録 | 下記評価の成功/失敗/未確認を区別。保存の有無を合否にせず、作業成果と重複・所要時間を確認 |

対象の主な配置は`internal/agent/{context.go,broker_context.go,broker_model*.go,lifecycle.go,agent.go,js/task.js,js/api.js,js/hooks.js}`、private履歴helper、`internal/runtime/agent.go`、CLI/observationと対応テスト。
既存のdecision共有計数・認証・通信・取消しは維持する。stdlib、公開tool追加、provider API形式の新規対応は範囲外。

## 総合テスト

- **入力と通知**：固定入力・tool定義・runtime情報込みで閾値を判定。削減前の使用量を削減後として送らない。estimated/measured/cachedの尺度を混同しない。
- **履歴の正しさ**：並列call、reasoning/署名/native response item、候補回答へのafter steer、tool失敗とhook失敗を通し、残したblockと追加指示を保持する。Chat/Responses双方の既存adapter fixtureを使う。
- **発火の連鎖**：hookからのself steer、外部steer、切り捨ての重複・同step再準備、複数root、親子・孫・次queue taskを組み合わせる。hookCalls/providerCallsを余分に増やさない。
- **削減不能**：固定入力だけで予算到達、単独の巨大block、追加指示の急増、全部分が保護対象のケースで、無限loop・不完全tool履歴・隠れた要約を起こさない。
- **decision除外**：親runが切り捨てを経験していても、`noe:decision`へ渡したstate/questionsは元のまま。単発decisionにcontext準備やtrimを挟まず、通常agentの追加要求も行わない。
- **停止・保存観察**：plan生成/適用確認/モデル/tool実行の各地点でinterruptし、所有workとpending planを回収する。context.trimmedの受付と実際の履歴削減、live/saved snapshotを照合する。
- **メモリ**：削除前の全履歴を保持する配列・closure・Go stateを残さない。メモリ解放後のRSS値そのものを固定した合否条件にはしない。

最終候補で`go test ./...`、`go test -tags=e2e ./...`、`go vet -tags=e2e ./...`を行う。
共有stateと履歴更新・取消しの関係は関連packageとE2Eをrace付きで確認する。
配布6targetのCGOなしbuildを確認し、Linux amd64のnative動作と他targetのcross-buildを区別する。
各sliceでfull suiteを無制限に繰り返さず、focused確認後に総合確認する。

## 実モデル評価の計画

候補は既存の`opencode-go` / `deepseek-v4.1-flash`、effortはauto。通常agentの実要求だけを使い、Jev・他provider・実Exa検索は不要。
この文書の作成時点では実行しない。実装後の評価工程として、接続対象・要求上限・明示予算をレビュー可能な形で記録して実施する。
通常suiteとCIでは接続しないopt-in harnessに分け、候補として追加tag`e2e context_live`と
`NOE_AGENT_CONTEXT_LIVE=1`の両方で有効化する。使用model/providerを固定し、taskごとの
maxSteps/providerCallsは12、timeoutは180秒を初期の実験上限にする。これは製品の既定値ではない。

- 一時workspaceに小さな複数ファイルと簡単な加工taskを用意する。taskは元の指示を読み返せば作業を継続でき、隠れた初期情報の暗記や必須メモを成功条件にしない。
- 既定の100万ではなく、固定入力より大きい小さめの明示input予算を設定し、通常のtool往復で2回程度の切り捨てを観察する。応答予約とmaxTokensも明示する。
- before/after hookのfixtureで出力量を揃える制御ケースと、短い自然なtaskを区別する。制御で到達した結果を「モデルが自発的に選んだ」と報告しない。
- 保存を命令しない同じ注意文で、何を保存するか/保存しないか、切り捨て後の再読込み、作業の重複と最終成果を観察する。
- policy無効・retainRatio 0.1/0.2の比較は各1試行を基本とする。maxSteps・共有providerCalls・timeoutを明示した有限harnessにし、製品既定の制限にはしない。
- 成功基準は履歴の構造・通知と実際の送信・taskの継続と成果。保存ファイルができたこと、cache hit率、削減率だけを成功基準にしない。
- 保存に費やしたtool回数と時間、総要求数、入力/キャッシュ/出力usage、再作業、最終成果を記録する。実測usageのない失敗要求は推定費用で穴埋めしない。

JSONL・可読ログ・設定・fixture/hook/実行JS・生成物と結果を一時directoryへ保存し、キーや認証headerは保存しない。
モデルが保存しない、早すぎる/遅すぎる保存をする、保存が逆に時間を使う結果も、そのまま評価結果にする。
providerエラーや応答上限で未達の試行を機構成功と扱わず、必要なケースのみ再試行して記録を分ける。

## 完了判定と対象外

全sliceの実装・テスト・レビュー、総合確認と実モデル評価の結果を実装記録へ残す。
モデルの任意保存が常に成功することは完了条件にしない。意味的な要約、必須checkpoint、履歴の永続セッション化、
完全な履歴復元、tool結果の部分切断、decision入力圧縮は今回の範囲外。
commit/push/release・既存`./noe`置換は実装計画の完了条件とは別に扱う。

## 計画レビュー

2026-10-18、独立reviewerが計画と既存のcontext・model・steer・hook関連経路を確認した。
予算計算、100万fallback、履歴の保護単位、plan適用確認、hook非再実行、子・queueの設定継承、
decision除外について必須修正の指摘はなし。任意の明確化として、切り捨て通知と原因IDを
最初の実provider送信まで保持する規約を追記した。
計画レビュー時点では製品テスト・実モデル接続は行っていない。後続の実装・検証結果は実装記録へ分離した。
