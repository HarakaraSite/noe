# hookの例外局所化とtaskへの通知 — 実装計画（2026-10-25）

[議論メモ](hook-isolation-context-api-discussion-20261025.md) · [開発ガイド](README.md) · [現行hook仕様](../hooks.md)

後続指示により、[スライス分割・レビュー手順](hook-failure-isolation-slices-20261025.md)に従って実装・検証・commitまで進める。[実施記録](hook-failure-isolation-implementation-20261025.md)に状態を残す。以下の「今回は計画のみ」は作成時の範囲であり、後続指示前の記録として読む。

## 0. 位置付け・完了する利用動作

利用者の依頼は「hookの例外を局所化し、taskを終了させず、失敗をtaskへ通知したい。計画を立てて」。
本書はその実装計画であり、**今回は計画の保存まで。製品実装・実行時挙動の変更・リリース・commit/pushは行わない。**
調査基準は `74e7056`。同じworkspaceの未コミットの議論メモと参照リンクは保持する。

成功とは、たとえばファイル保存用hookが書込みに失敗しても、失敗をtaskへ通知し、本来のモデル/tool処理と後続hookを継続して、必要な成果を得られること。
hookは検査器ではなく、記録・通知・状態更新・加工など任意の処理を行うものとして扱う。

本計画では、次を採用する。

1. 登録された4種類のhandlerについて、処理例外・reject・置換値の契約違反を各hook単位で局所化する。
2. 失敗を既存の観察イベントだけに閉じず、run所有の内部通知へ登録する。
3. 次の通常のモデル要求へ事実通知として同送する。人間のsteerや修復命令にはしない。
4. 次のモデル要求がない場合も消えないよう、taskの保持結果へ失敗情報を残す。
5. **hook失敗の通知だけを理由にモデル要求を増やさない。** 最終応答後の失敗もtaskを正常完了できる。
6. 取消し、既存の実行上限、scope/Hostの継続不能は通常hookエラーと分ける。

## 1. 範囲と維持する契約

### 今回変更する範囲

- `beforeModelRequest`、`afterModelResponse`、`beforeTool`、`afterTool` の登録済みhandler呼出し。
- handlerの例外と、戻り値・JSON化・画像選択などの検証失敗。
- 後続hookの続行、元の値・成立済みの置換の維持。
- taskへの通知、完了結果への保持、Go/JSの公開型と説明、関連fixture。

### 今回変更しない範囲

- hooks設定自体の形式検証、module解決・読込み・export定義の成立確認。これらの開始時エラーは現行どおりtaskの準備失敗とする。
  今回は登録済みhandlerの実行失敗の局所化を対象とし、登録できなかったmoduleを黙って解除する挙動は同時に追加しない。
- 登録方法、module一覧と順序、継承、taskごとの読込み・寿命。
- hookの完了をawaitする方式、異なるtool callの並行処理。
- 人間の承認、実行見送りAPI、動的hook登録、親モデルへの同期問い合わせ。
- トリムのhook化、公開コンテキスト操作API、汎用の公開task通知API。
- provider通信のretry、contextの予算・trim方針、tool出力予算、権限やsandbox。
- hook失敗の回数を理由にした自動無効化・自動再試行、新しい終了条件・実行上限の追加。

正常なhookは従来どおり動かす。モデル前後の戻り値は引き続き使わず、tool前後は現行の明示的な戻り値でのみ置換する。
通常のtool実行エラーを成功へ変えたり、hookの通知をtool自体の失敗として偽装したりしない。

## 2. hookごとの継続動作

| 失敗したhandler | 継続する処理 | モデル向け通知のタイミング |
| --- | --- | --- |
| beforeModelRequest | 後続hook、最終コンテキスト準備、元のモデル要求 | そのモデル要求 |
| afterModelResponse（tool付き応答） | 後続hook、応答に含まれるtoolの通常実行 | 次の通常モデル要求 |
| beforeTool | 後続hook、直前の有効な引数で対象toolを通常実行 | 次の通常モデル要求 |
| afterTool | 後続hook、直前の有効な結果・画像で結果を確定 | 次の通常モデル要求 |
| afterModelResponse（最終応答） | 後続hook、既存の最終確定ゲート | 追加要求は強制せず、保持結果に通知を残す |

`beforeTool`が失敗しても対象toolを見送らない。hook失敗を実行許可判定に使う設計ではない。
直前の引数自体が不正なら、通常のtool入力エラーとして処理する。

### 置換と副作用

- 成功した先行hookの置換は維持する。失敗したhookの置換だけを不採用とする。
- 次のhookには直前まで成立した値を渡す。イベントsnapshotの直接変更を採用しない現在の契約も維持する。
- `afterTool`では、元の実行結果と、先行hookで成立した結果加工・画像選択を維持する。
- 失敗以前に成立した `setRuntimeInfo()`、ファイル操作、子agent起動等を自動的に巻き戻さない。既存の所有関係・回収規則は維持する。
- 完了済みhookや対象toolを再実行しない。
- 並列toolの一方のhook失敗によって、他方を取り消さない。

## 3. 例外を捕捉する境界

### 3.1 登録handlerの処理と返却値検証だけを回復対象にする

主な変更先は [js/hooks.js](../../internal/agent/js/hooks.js) の `callHooks()` と `withHookAPI()`。
登録単位でhandlerと置換値検証を実行し、通常の失敗なら結果を適用せず次の登録へ進む。

`withHookAPI()` 全体を単純な `catch {}` で包んではいけない。
現在はcallback失敗とscope終了失敗が重なるとcallback例外を優先するため、局所化に合わせて、**scope終了側の継続不能を隠さない内部結果契約**へ整理する。

内部の概念上の結果は、次の3種類に分ける。

- handler成功：値を採用できる。
- handler失敗・継続可能：失敗を記録・通知し、そのhookの値は採用しない。
- 継続不能：取消し・上限・Host/bridgeの障害等を従来の本体終了経路へ伝える。

### 3.2 エラー文字列で分類しない

現行の [bridge](../../internal/agentbridge/bridge.go) はGoエラーをJSのPromise rejectへ渡しており、JSでの文字列照合では分類できない。

- scopeの開始・終了、broker応答の検証はhandler失敗のcatch対象から分離する。
- hook-scoped RPC中のHost内部障害やgroup停止などは、Go側で型・状態に基づいて継続不能として保持し、scope終了結果でも確認する。
- 不正引数などhook側の通常のAPI利用エラーは、handlerが捕捉しなければhook失敗として扱える。
- ownerが生存しているかの確認だけで、単発の内部障害まで回復可能と決めない。
- ownerの取消しや実行上限が成立した場合、それをhookの警告に置き換えてtaskを続けない。

変更はhookの内部呼出し・scopeに必要な範囲に限定し、全公開APIのエラー形式を一括変更しない。

## 4. 失敗記録とtaskへの通知

### 4.1 Hostを通知記録の所有元にする

[broker_hooks.go](../../internal/agent/broker_hooks.go) の `endHookScope` で、通常のhandler失敗を確定したときにrun所有の記録へ追加する。
既存invocation IDを通知の識別子として使い、同じscopeの終了が再度観察されても二重登録しない。
並列toolからの追加もHostの既存同期の下で処理する。

既存の `hook.failed` は維持する。新たにモデルへ渡す通知と、診断用のイベントは別の用途とする。
task通知には、少なくとも次の情報を保持する。

- `invocationId`、`hookIndex`、`module`、`phase`
- `modelStepId`、該当する場合は発生元の `requestId`・`toolCallId`・`toolName`
- printableなエラー説明
- 通知を載せたprovider要求のID（未送付なら未設定）

発生元のrequest IDと、通知を載せたrequest IDを混同しない。
エラーのstack等の詳細は既存イベントに残す。モデルへの通知は発生箇所と失敗内容の事実説明にし、修復を義務付けない。
hookの任意のファイル処理について、実際の副作用をNOEが推測して「一切変更されていない」とは通知しない。

### 4.2 次の通常モデル要求への同送

既存の `NOE_CONTEXT_STATUS` user messageに、hook処理失敗を示す独立した節として追加する案を採用する。
同じ通知messageを、現在と同じ完了blockに保存する。新しいprotected指示や履歴kindは増やさない。

関係する処理：
[context.go](../../internal/agent/context.go)、
[broker_context.go](../../internal/agent/broker_context.go)、
[broker_model.go](../../internal/agent/broker_model.go)、
[js/task.js](../../internal/agent/js/task.js)。

- 送信予定の通知本文を最終コンテキスト準備・概算に含める。prepared digest確定後に会話を勝手に差し替えない。
- `context_get` や準備・観察で通知を消費しない。
- 通知に含めたIDのbatchを内部契約で追跡し、実際のprovider要求のadmissionで、そのbatchだけを送付済みにする。
- steerだけを返して要求を再準備する経路では消費しない。
- 同じinvocationを新規通知として再挿入しない。送付済み通知が通常の会話prefixに残ることとは区別する。
- 送付済みの通知本文は対応応答・tool結果と一緒に、従来どおり会話保存・trimされる。
- pending通知をtrim対象の履歴だけに置かない。provider admission前に消えないようrun側で保持する。
- `beforeModelRequest`は論理判断につき一度という契約を維持する。通知のためにhookをもう一度呼ばない。
- 通知を人間のsteerへ変換しない。`wait_tasks`を起こすsteerとしても扱わない。

ここで「送付済み」は実provider要求へ載せたことを指し、モデルが内容を理解・解決した保証ではない。
通常のprovider要求自体が失敗した場合の終了・retryは、既存の契約に従う。

### 4.3 最終応答直後と、次のモデル要求がない場合

**hook失敗だけを理由に `commit_final` を拒否したり、追加のモデル要求を発生させたりしない。**
毎回失敗する `afterModelResponse` で、通知 → 新応答 → 同hook失敗、というループを作らないため。

最終応答後の失敗もtaskの失敗記録として保持し、最終確定と正常完了を妨げない。
実際のsteerがあれば従来の確定ゲートが次の判断へ戻す。その場合、pendingなhook通知もその通常要求に同送できる。
maxStepsなどを通知のために迂回・追加消費しない。

この契約は次を区別する。

- **全hook失敗をtaskの通知・保持結果へ残すこと**は行う。
- **全hook失敗を、そのtaskのモデルが必ず読んでから終了すること**は保証しない。

### 4.4 taskの保持結果

`TaskOutcome`へ任意の `hookFailures` 配列を追加する計画とする。公開Goには要素型 `HookFailure`、公開JS宣言にも同等の型を用意する。
エラーがなければフィールドを省略し、従来の成功結果のwire shapeを維持する。

- run中に確定した失敗の記録を保持し、providerへの送付状況を区別できるようにする。
- taskの `state`・`answer`・`error` とは分ける。`completed` と `hookFailures` の存在は両立する。
- `run_agent` / `observe_agent` の `lastResult`、`wait_task`、`wait_tasks` のoutcomeへ同じ情報を返す。
- `includeAnswer: false` は引き続き回答本文だけを省略する。hook失敗の通知は確認できる。
- `read_task_result` は回答原文の範囲取得のまま変更しない。回答本文へ警告を追記しない。
- 保持済み記録は完了後に変えず、既存の保持寿命に従う。projectionで保持元を変更しない。
- keepAliveの次taskへ未送付通知を自動移送しない。記録は発生元taskのoutcomeに残し、次taskに同じ失敗を新規通知しない。
- 取消しや別の本体エラーで終了した場合も、すでに確定しているhook失敗は可能な範囲でoutcomeに保持し、本体の終了理由を上書きしない。

無限再判断を避けるための「最終候補を一度だけ再判断させる」案も検討したが、追加要求と専用の延期状態を必要とするため今回は採用しない。

## 5. tool結果・画像・観察との整合

[js/tool_pipeline.js](../../internal/agent/js/tool_pipeline.js) では、回復したafterTool失敗をtask例外として再送出せず、直前の有効な結果・画像で通常の `tool_result` を一度だけ記録する。

**回復済みの失敗を、現行の `toolResultRequest.HookError` にそのまま載せない。**
[broker_tool_result.go](../../internal/agent/broker_tool_result.go) は `HookError` または `modelResultAvailable: false` があるとstep全体の画像を破棄するため、単に既存欄へ警告を足すだけでは画像が失われる。

- 元の `rawArguments` / `rawResult` / `rawImages` と、有効な置換後値を維持する。
- 局所化できたhook失敗でもモデル向けtool結果は利用可能とする。
- 回復した失敗の事実は `hook.failed` とtaskの失敗記録に対応付け、tool成功/失敗の事実と混ぜない。
- Host障害等で本当にモデル結果を確定できない場合の処理は、通常hook失敗の回復と分ける。
- `Promise.all` は通常hook失敗ではrejectしないが、本体の取消し・継続不能は従来どおり伝播させる。
- CLI/JSONLは `hook.failed` を表示・保持しつつ、その後にtaskが `completed` になり得る。失敗イベントを成功に書き換えない。

## 6. 実装単位と変更経路

### A. 共通hook境界と失敗記録

- `internal/agent/js/hooks.js`：handlerと返却値検証の局所化、成立済み値の保持、後続登録の続行。
- `internal/agent/broker_hooks.go`、必要なhook-scoped bridge：scope終了と回復可能性の区別、run記録・pending通知。
- `internal/agent/host.go` 等のrun状態、`instance.go`・`task_lifecycle.go`：失敗記録の保持とoutcomeへの確定。
- `agent/host.go` と `agent/types/noeagentlib.d.ts`：追加の結果型。
- 対象を絞ったfixtureで、通常失敗の局所化と取消し・内部障害の非局所化を確認する。

### B. モデルへの通知と完了時の扱い

- context通知の整形・概算・provider admissionでのbatch消費を接続する。
- 最終応答後に追加要求を強制しないこと、未送付分もoutcomeへ残ることを確認する。
- steerによる再準備、既存の会話checkpointとtrim、keepAlive task間の状態を確認する。

### C. tool経路・利用契約・最終検証

- toolパイプラインの結果・画像・並列処理を整合させる。
- 旧「afterTool失敗でtask失敗・兄弟tool取消し」fixtureを、新しい製品動作の期待へ変更する。
- 利用・内蔵docs、型宣言、必要なイベント説明、実装記録を更新する。
- 実装とレビュー修正が落ち着いた候補で、親が必要な全体検証を一回まとめる。

A〜Cは作業の区切りであり、未完成の通知経路をリリースする段階分割ではない。
担当を工程・ファイルごとに増やす必要はなく、まとまった実装と独立レビューで進める。

## 7. 検証する利用動作

有料providerを使わず、現行のローカルprovider fixtureと実executorで検証する。

| 利用動作 | 主な確認 |
| --- | --- |
| 保存用hookがファイルI/Oで失敗 | taskは継続、hook.failedとtask通知が残る。登録4phaseそれぞれの配送先を確認 |
| 同phaseに複数hook | 一つの例外・不正な戻り値でも後続が呼ばれ、先行の有効な置換と副作用を保持 |
| tool実行後のhook失敗 | ファイル等の作用は一度だけ。元の結果、先行加工、選択済み画像を維持 |
| 並列toolの一方のhook失敗 | 他方は取り消されず完了。全失敗が正しいtool/呼出しに対応し、欠落しない |
| beforeModelRequestで失敗 | その要求に通知が含まれ、概算にも含まれる |
| steerでモデル要求を再準備 | 実provider要求までpendingを保持。新規通知を二重挿入せず、hookも再実行しない |
| 最終応答のたびにafterModelResponseが失敗 | taskはcompleted。通知だけの追加要求なし。未送付分をhookFailuresで確認可能 |
| 完了結果を後から取得 | lastResult/wait_task/wait_tasksとincludeAnswer:falseで通知を保持。read_task_resultの回答原文は不変 |
| keepAliveで次taskを実行 | 送付済み通知の会話保存を維持。旧taskのpendingを新規通知として再送しない。旧outcomeも不変 |
| 通知を含む会話をtrim | 現行の完了block検証・概算・checkpointが成立。未送付通知は削除されない |
| hook待機中の取消し・実行上限・Host障害 | hook警告として正常続行に変えず、既存の取消し・終了・所有物回収を維持 |

単体/統合テストは上記動作に対応する範囲で追加・更新する。現行の正常hook・引数/結果置換・画像選択・hook因果関係の関連テストも使う。
待合せはfixtureの明示的な同期で行い、長いsleepや実ネットワークの失敗待ちを作らない。

実装時は変更箇所のテスト、Go整形、保存JSのBiome lint、公開宣言の型確認、差分確認を行う。
最終候補では全体unit/E2Eと、Host/scope同期に影響するpackageのrace検証をまとめる。既存結果を再利用できる範囲を区別し、無関係な全メニュー・6対象配布ビルド・実provider接続を自動追加しない。

## 8. docs・互換性・完了条件

実装時に [hooks.md](../hooks.md)、[api.md](../api.md)、[events.md](../events.md)、[observe.md](../observe.md)、[内蔵agent docs](../../internal/apidocs/content/noe-agent.md) と公開宣言を確認・更新する。
組込みfacts/policyにhook例外やtask結果の説明があれば整合させる。リリース番号・時期は今回決めない。

変更は意図的な既定挙動の変更であり、従来throwでtaskを止めていた利用者は同じ停止効果を得られなくなる。
互換性を称して黙って変更せず、この点と「beforeTool失敗後も直前の引数で実行する」ことを明記する。
今回、従来の停止動作を切り替える設定や、別の停止APIを新設することは完了条件にしない。

完了条件：

1. 登録4handlerの通常例外・返却値エラーをhook単位で局所化し、後続hookとtaskが継続する。
2. taskへの通知と完了結果への保持が、JSONL保存の有無に依存せず成立する。
3. 最終応答後の失敗でモデル要求のループや、通知だけの追加要求を発生させない。
4. 元の実行事実・成立済みの置換・画像・並列tool・会話の整合と、取消し・上限・所有物回収を維持する。
5. 公開型と利用説明で、変更された例外契約と、通知の「通常要求への同送／完了結果への保持」を区別できる。

計画段階では関連ソースと既存fixtureの読取りのみを行った。製品動作のテストは実装時に行う。

## 9. 計画の確認記録

同日の独立した読取り専用レビューを一回行い、計画修正を要する重要な指摘はなかった。
handler単位の回復境界、provider admissionでの通知消費、最終応答後の終了性、画像保持、会話の完了block構造、task結果の確定経路との接続を確認した。
これは設計・現行コードとの照合結果であり、新しい動作の実行検証済みという意味ではない。
ローカル参照先の存在と文書の空白差分も確認した。製品コード・依存・実行時挙動は未変更。
