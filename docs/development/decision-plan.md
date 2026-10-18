# 意思決定APIの実装計画

状態：実装・テスト・コードレビュー・実プロバイダ確認完了。2026-10-17。結果と指摘の対応は[実装記録](decision-implementation.md)、3モデルの確認結果は[実接続記録](decision-openrouter-validation.md)を参照。

本書を`noe:decision`の初期実装の計画正本とする。[検討メモ](../future-candidates.md)と`../henji/docs/design/decision-cli.md`を設計入力とし、通常のagentとは別のJS APIを提供する。利用者は実プロバイダテストを許可済み。対象モデルは`typesafe/jev-1.14`・`perplexity/pplx-decider-v1.2`・`cloudflare/clef`の3つとし、追加指示に従い`typesafe/jev-router`は対象から外す。

## 目的と範囲

通常のJSとagentの`run_js`から、一つの判断対象に対する複数の質問を一回のDecisions要求で評価し、構造化した結果を後続のJS処理に使えるようにする。分類、処理先の選択、成果物の評価などを想定する。

- 公開入口は`noe:decision`の`decide`。通常の`run_agent`・`spawn_agent`とは入出力と実行処理を分ける。
- providerは最初にOpenRouterのみ対応し、採用中の公式Go SDK v0.9.59を使う。
- 認証・通信・要求の所有と取消しはGoホストで扱い、既存のOpenRouter接続設定とoperation/brokerを共用する。
- 文字列・構造化JSONの`state`、名前付き`questions`、完全な応答JSONを扱う。
- 画像等のprovider-nativeなpartもJSONとして渡せる構造にする。画像ファイルを読む・base64化する・縮小する専用APIは初期実装へ追加しない。画像の実接続確認は小さなfixtureをJSで読み込む形にする。
- 判断の反復、結果に応じた分岐、複数対象への並列要求は呼出し元のJSが組み立てる。

agent instance、queue、会話履歴、共通system instructions、推論effort、コンテキスト圧縮は意思決定要求へ持ち込まない。意思決定専用のCLI、Worker対応、他provider、daemon、モデルの自動選択、リリース・commit・pushは今回の対象外とする。

## 確認済みの外部仕様と実装上の注意

2026-10-17、公式資料・公開Models APIとローカルの固定版SDKを確認した。

| 項目 | 確認結果と実装方針 |
| --- | --- |
| 専用API | `POST https://openrouter.ai/api/alpha/decisions`。`model`・`state`・名前付き`questions`を送る。[公式API仕様](https://openrouter.ai/docs/api/api-reference/alphadecisions/submit-a-decisions-request) |
| SDK | v0.9.59に`client.Alpha.Decisions.Create(ctx, components.DecisionsRequest, ...)`がある。新しいSDK依存は不要。[固定版の実装](https://github.com/OpenRouterTeam/go-sdk/blob/v0.9.59/decisions.go) |
| 接続先 | Decisions操作は通常のChat用base URLとは別にoperation固有serverを使う。テスト接続先は`operations.WithServerURL`でoriginを指定し、`/api/alpha/decisions`への要求をfixtureで検証する。Chat用の`/api/v1`をそのまま連結しない |
| JSON保持 | SDKのrequest/response型だけで往復すると未知フィールドや数値表現が失われうる。既存Chat実装を参考に、SDK呼出し用の型と、送信・返却の正本となるraw JSONを分ける |
| 質問 | 初期の利用例・試験は`noul`・`choice`・`score`。質問objectの未知フィールドは保持し、質問の意味やモデル別制約はAPIに委ねる |
| モデル | 公開Models APIの`output_modalities=decisions`で、指定した3モデルの出力が`decisions`、入力がtext/imageであることを確認。[Models API](https://openrouter.ai/api/v1/models?output_modalities=decisions) |
| Clef | Workers AI経路では長いtext stateが先頭約2,000 tokensで切り捨てられ、画像もサイズ上の制約がある。初期試験は短い入力・小画像を使う。[マルチモーダル仕様](https://openrouter.ai/docs/guides/community/multimodal-decisions) |

実装時にも仕様とモデルの提供状態を確認する。上流の型や応答に差があれば、fixtureで再現し、どの差をadapterで扱うか記録する。指定モデルの失敗を別モデルへの置換で成功扱いにしない。

## JS APIと入出力契約

```js
import { decide } from "noe:decision";

const result = await decide({
  provider: "openrouter",
  model: "typesafe/jev-1.14",
  state: {
    ticket: "支払い完了後にエラー画面になりました。",
  },
  questions: {
    team: {
      type: "choice",
      instructions: "Which team should handle this ticket?",
      criteria: {
        payments: "Payment or checkout issues",
        account: "Account or login issues",
      },
    },
  },
});

console.log(result.answers.team);
```

- `decide(options)`はPromiseを返し、正常応答のJSON object全体をresolveする。`answers`だけを抜き出したり、agent snapshotで包んだりしない。
- `provider`と`model`は毎回明示する。初期providerは`openrouter`のみ。親agentのmodel・effort・system・cacheKeyを暗黙に継承しない。
- `state`は空でない文字列、またはJSON object/array。値を自動で文章化・切詰め・要約しない。JSONとして表現できない入力は送信前にrejectする。
- `questions`は空でない名前付きobjectで、各質問はobject。未知の質問フィールドやネストしたJSONを送信時に保持する。空入力・不正な形はJSとGoの境界で検証する。
- モデルIDの固定allowlistは作らない。モデル・質問形式の提供可否はAPIの結果として扱う。
- `state`内の画像partはOpenRouterの形式を使う。ファイルパスやremote URLをNOEが自動で画像へ変換する契約にはしない。
- 初期公開optionは上記4項目に絞る。`effort`・`system`・`maxSteps`・`keepAlive`等は受け付けず、未知のトップレベルoptionは入力エラーにする。provider固有の追加設定は必要が生じた時に別途設計する。
- 1要求の単位で完結し、自動再試行・別モデルへのfallback・再生成は追加しない。独立要求は`Promise.all`で並列実行できる。

Go/RPC内では応答を`json.RawMessage`で保持し、最後にJSでJSONとして解析する。未知フィールドは保持するが、JS objectにした後の数値精度は通常のJavaScriptに従う。巨大整数等の字句表現まで完全に保持する公開APIとはしない。

不正・空・途中で切れたJSON、HTTP/API失敗、取消しはrejectする。`answers`はobjectとして確認し、質問キーとの対応をfixtureと実接続で検証する。正常応答内のrefusalや低い確信度は、呼出し元が判断できるよう元のJSONに残す。既存providerのエラー分類・HTTP statusの扱いを利用し、エラーを通常の判断結果へ変換しない。

## Go・runtime・brokerへの接続

| 層 | 主な責務・変更対象 |
| --- | --- |
| `internal/decision/`（新規） | JS module source、要求契約・検証、意思決定用provider interface。agent loopには依存しない |
| `internal/agent/provider/` | OpenRouter adapterに独立した`Decide`処理を追加。同じclient・credential読込みを利用し、既存の`Generate`契約は維持する |
| `internal/agent/` | Hostに意思決定providerの注入口を追加し、`decide` RPCを既存Operationで追跡。agent providerだけを持つ既存fixtureへ新メソッドの実装を強制しない |
| `internal/runtime/` | `noe:decision`と非公開native入口を登録し、現在のevaluationのOperationへ接続。Promiseのsettleはruntime owner上で行う |
| `internal/cli/`・`internal/observation/` | 意思決定イベントの可読表示と親向けの差分取得 |

依存方向は「意思決定の契約 → provider adapter → Host/broker → runtime」を基本とし、`internal/decision`と`internal/agent`の循環importを作らない。SDKの型はadapter内に閉じる。

### 認証とJSON保持

OpenRouterのキー探索順と`NOE_OPENROUTER_API_KEY_FILE`を共用する。module importだけではキーを読まず、最初の要求時に遅延読込みする。通常のJSと子NOEで挙動を揃え、子は既存Connection経由で親brokerへ要求し、別の認証経路を作らない。

Decisions用の要求・応答状態をChatのSSE状態と分ける。SDK呼出しには公式の`Alpha.Decisions.Create`を使い、adapterのHTTP境界で元の要求JSONを送信し、元の応答JSONを取得する。SDK型への変換結果を正本に戻さない。固定版SDKが通常の成功応答を処理できること、未知フィールドが欠落しないこと、並列要求間で本文・状態が混線しないことをS1で確かめる。型と実APIに処理不能な不一致があれば、その差を明記して計画を更新し、黙ってSDK利用を取りやめない。

### 寿命と取消し

- 通常JSの要求は、その`run`・`eval`・REPL入力のOperationに所有させる。awaitの有無にかかわらず、既存のevent loopで進行中のnative workを追跡する。
- `run_js`からは、そのtoolの子scope経由で親ホストの意思決定providerを呼ぶ。親runの終了・interrupt、子scope終了、timeout、Ctrl+CでHTTP要求を取消し、bodyをcloseして要求処理を回収する。
- 取消しと応答完了が競合してもPromiseと終端イベントを二重に確定しない。scope closeのjoinを自分自身のhandler内で待つ構造にしない。
- 完了済み要求の状態を次のREPL入力へ持ち越さない。import済み関数は呼出し時点のOperationを参照する。

## 観察

`decision.started`・`decision.completed`・`decision.failed`・`decision.cancelled`を既存のJSONLへ追加する。要求ごとの`requestId`、provider、要求したモデルの`decisionModel`、所要時間、取得できるusage・応答モデルの`responseModel`・HTTP statusを記録する。終端は一要求につき一度とする。

agentの`run_js`内から呼ばれた場合は、ホストが所有scopeから`runId`・agent情報・`toolCallId`を関連付ける。通常JSからの直接要求にはagent/run IDを新しく作らない。APIキーやbroker capabilityは既存どおりログへ渡さない。

共通の`model`は親agentの設定モデルを示す項目として維持し、意思決定の要求・応答モデルで上書きしない。通常JSからの直接要求では共通の`model`を省略し、`decisionModel`・`responseModel`で識別する。既存のファイル観察はイベントの`model`を親snapshotの`Model`へ反映するため、この二つの専用項目はsnapshotの`Model`へ反映せず、差分イベントに保持する。

人間向け表示は開始と終了・失敗を短く表示し、親向けのファイル観察は意思決定イベントを差分へ含める。親runの実行段階は引き続き`run_js`として扱い、意思決定完了をagent全体の完了と混同しない。JSONLのmetadataとJSへ返す完全な判断JSONを区別する。実接続の比較用には、テスト成果物として完全な応答JSONも保存する。

## 実装スライス

| Slice | 作業 | テスト・受入条件 |
| --- | --- | --- |
| S1 provider | 意思決定のGo契約、OpenRouter SDK adapter、既存認証の共用、raw JSONの送受信 | ローカルHTTPでpath・auth・本文・未知フィールド・応答保持・エラー・並列・取消しを確認。SDKが実装方針に適合する |
| S2 JS入口 | `noe:decision`、native接続、`decide` RPC、Operationでの追跡 | `run`・`eval`・REPLから利用でき、並列要求、入力エラー、遅延認証、入力ごとの再利用・取消しが成立する |
| S3 agent内利用と観察 | 子Connection、scope/run/toolの関連付け、イベント、表示、親向け観察、instructionsへの使用例 | fixtureの親agent→run_js→decide→判断結果による分岐→最終回答が動く。親interruptとscope回収が成立する。親のモデルと`run_js`の実行段階を維持し、意思決定の要求・応答モデルを差分で識別できる |
| S4 利用文書と総合確認 | API/観察ガイド、README両言語、examples、検討メモ、fixture E2E | APIと例が一致し、通常agent・既存ログ・REPLに回帰がない |
| S5 実接続と最終確認 | 下記3モデルの実接続、結果記録、全体コードレビュー | モデルごとの結果・差・未確認事項を記録し、要件と実装・テストの整合を確認する |

依存順はS1→S2→S3→S4→S5。各スライスは実装→関連テスト→コードレビュー→必要な修正の順で進める。局所チェックが通った後に総合テストを行い、実接続後に全体のレビューと完了判定を行う。

## ローカル検証

fixtureと実NOEプロセスで、利用者の実行経路を確認する。新しいテストのために実プロバイダの応答を固定値とみなさない。

- 文字列・構造化object/arrayと名前付き複数質問が、その内容を保って要求へ入る。
- SDKの既知型にない質問フィールド・応答フィールドが失われず、数値をGoで再エンコードしない。
- `noul`・`choice`・`score`と未知の応答フィールド、正常なrefusalを返せる。
- 空・不正入力は送信前にrejectし、認証失敗、4xx/5xx、不正JSON、途中切断は失敗として返る。
- 並列要求の応答・イベント・所有scopeが対応し、認証を共有しても要求間で本文が混ざらない。
- native要求中のタイマーが進み、run/eval/REPL・親interruptで取消しと回収が成立する。
- モデル向けtoolは`run_js`一つのまま、`await import("noe:decision")`から利用できる。
- JSONLと人間向け表示、親の差分観察で要求と結果を識別できる。
- 親agentとは異なる意思決定モデルの開始・完了イベントを読んでも、親snapshotの`Model`と実行中の`run_js`段階を維持する。cursor以降の差分には`decisionModel`・取得できる`responseModel`が残り、通常JSからの直接要求には親モデルを割り当てない。

全体では`go test ./...`、`go test -tags=e2e ./...`、`go vet -tags=e2e ./...`を実行する。並行処理を変更したpackageはrace detectorでも確認する。通常のテストで実接続や課金を発生させず、実接続は専用tag・明示コマンドへ分離する。

## 許可済みの実プロバイダテスト

| モデル | テスト経路 | 確認すること |
| --- | --- | --- |
| `typesafe/jev-1.14` | `noe:decision` → SDK → Decisions | 複数質問、構造化state、画像part、完全な応答とusage |
| `perplexity/pplx-decider-v1.2` | 同上 | 同じ公開APIと入力形式で結果を得られること |
| `cloudflare/clef` | 同上 | 同じ公開APIでの結果、小さな画像・短いstateでの動作 |

最初は各モデルに次の3ケースを一回ずつ、計9要求を送る。fixture試験が通ってから行う。

1. 短い文章と`noul`・`choice`・`score`の3質問を一要求で送る。
2. 名前・属性を持つ構造化JSONの`state`を送り、複数の属性を質問する。
3. 共通仕様に収まる小さな自作画像を`state`の配列へ入れ、画像について質問する。Jevの画像寸法条件とClefのサイズ条件を満たすfixtureを用意する。

さらに、既存の`deepseek/deepseek-v4.1-flash`を親agentとして一度起動し、`run_js`からJev 1.14へ判断を依頼し、実際の結果を使って回答する経路を確認する。親は`effort:"auto"`と小さな`maxSteps`を明示する。実際にDecisions要求が発生したことをログで確かめる。

認証は既存の登録キーと設定を使う。要求数、要求したmodel・応答model、応答JSON、usage/cost、所要時間、JSONL、終了状態を保存する。これらのモデルは同じ回答・確率・provider名を返すとは限らず、固定の確率や完全一致を成功条件にしない。構造、質問との対応、値の範囲、分岐への利用を主に確認し、明白な入力に対する判断の妥当性は別に記録する。

外部障害やモデル固有の非対応は、ローカル実装の失敗と区別して記録する。失敗時は原因を調べて必要なケースだけを再実行し、同じ入力を理由なく繰り返さない。成功が確認できないモデルを確認済みにせず、残っている検証と判断材料を最終報告へ明記する。利用者が許可した上記実接続のために、改めて接続許可を求めない。

## 完了条件と記録

通常JSと親agentの両経路で意思決定結果を利用でき、認証・SDK・JSON保持・取消し・観察が上記契約を満たすこと。関連テストと全体レビューを終え、3モデルの実接続結果と実際の費用・制約を記録する。

実装・レビューの記録は`docs/development/decision-implementation.md`、実接続の記録は`docs/development/decision-openrouter-validation.md`へまとめる。完了時に本書の状態と[検討メモ](../future-candidates.md)の分類を更新する。実接続はローカル検証とコードレビューの後に実施する。

[開発記録へ戻る](../README.md) · [現在のAPI](../api.md)
