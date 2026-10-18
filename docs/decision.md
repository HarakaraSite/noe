# 構造化された意思決定：`noe:decision`

`decide`はOpenRouterのDecisions APIへ一度要求し、分類・スコア・確率等を含む応答JSON全体を返します。通常のJSとagentの`run_js`から利用できます。agent instance・tool loop・queueは作りません。認証は[agentと同じ登録キー](api.md#認証と設定)をGoホストが読み込みます。

```js
import { decide } from "noe:decision";

const result = await decide({
  provider: "openrouter",
  model: "typesafe/jev-1.14",
  state: { ticket: "支払い完了後にエラー画面になりました。" },
  questions: {
    team: {
      type: "choice",
      instructions: "Which team should handle this ticket?",
      criteria: { payments: "Payment issues", account: "Account issues" },
    },
  },
});
const team = result.answers.team;
console.log(team.choice, team.probabilities, team.confidence);
```

## 引数と結果

`decide({provider, model, state, questions})`はPromiseを返します。4項目は毎回明示し、親agentから設定を継承しません。

| 項目 | 契約 |
| --- | --- |
| `provider` | 現在は`"openrouter"` |
| `model` | 空でないモデルID。固定のモデル一覧による制限はしない |
| `state` | 空でない文字列、JSON object、JSON array。文章化・要約・切詰めはしない |
| `questions` | 空でない名前付きobject。各値は質問object。未知の質問フィールドも送信する |

未知のトップレベルoption、循環参照、undefined・関数・BigInt・非有限数等のJSONデータにできない値は送信前にrejectします。`effort`・`system`・`keepAlive`・`maxSteps`はこのAPIのoptionではありません。

戻り値はproviderの応答JSON object全体です。`answers`、返された`model`・`provider`・`usage`等と未知の応答フィールドを保持します。Go/RPC内ではraw JSONを保持しますが、JS objectへ解析した後の数値精度は通常のJavaScriptに従います。

質問の代表例は`noul`（yesの確率）、`choice`（選択肢と各確率）、`score`（順序付き尺度）です。質問形式・モデルごとの対応は[OpenRouterのAPI仕様](https://openrouter.ai/docs/api/api-reference/alphadecisions/submit-a-decisions-request)を参照してください。正常応答のrefusalや低い確信度は返却JSONに残り、呼出し元が扱います。エラーや途中で切れた応答はPromiseをrejectします。自動再試行・fallback・再生成はしません。

## 実行例とREPL

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run --events=decision.jsonl examples/decision/decision-classify.js \
  'Checkout fails after payment.' typesafe/jev-1.14
```

認証ファイルは最初の要求時に読みます。importだけでは認証読込み・通信を行いません。`NOE_OPENROUTER_BASE_URL`を指定すると、末尾の`/api/v1`を除いたURLをDecisions操作のserverとして使い、`/api/alpha/decisions`を付けます。通常の生成要求と送信先のパスが異なります。

```text
> const dm = await import("noe:decision")
> const options = {provider:"openrouter",model:"typesafe/jev-1.14",state:"Checkout failed.",questions:{team:{type:"choice",instructions:"Choose a team",criteria:{payments:"Payment issues",account:"Account issues"}}}}
> const result = await dm.decide(options)
> result.answers.team
```

複数の独立要求は`Promise.all`で並列化できます。実行中の要求はそのJS評価に所有され、未awaitでも終了前に追跡されます。timeout・Ctrl+C・親runのinterruptでHTTP要求を取消し、回収します。REPLの通信中断は既存のnative I/Oと同様にsessionを終了させます。

## agentからの利用と画像

モデルへ渡すtoolは`run_js`一つです。そのJS内で`await import("noe:decision")`して`decide`を呼び、結果に応じてJSで分岐するか、結果を表示して親モデルへ返します。意思決定用provider/modelは明示し、agent用の共通instructionsやeffortは送信しません。

画像等はprovider-nativeなJSON partとして渡せます。例えば`state`を`["画像についての文脈", {type:"image_url",image_url:{url:"data:image/png;base64,..."}}]`にします。ファイル読込みやbase64化は呼出し元のJSで行います。NOEによる画像の自動取得・縮小はありません。形式・寸法・サイズは[公式マルチモーダル仕様](https://openrouter.ai/docs/guides/community/multimodal-decisions)に従います。ClefのWorkers AI経路は長いtext stateの先頭約2,000 tokensだけを読むため、長い入力の評価ではその制約も確認してください。

## 観察

JSONLには`decision.started`・`decision.completed`・`decision.failed`・`decision.cancelled`が入り、`--observe`または`noe observe`で短く表示できます。`requestId`で要求を識別し、`decisionModel`が要求モデル、取得できる`responseModel`が実応答モデルです。完了時には取得できるusage・HTTP status・所要時間も付きます。

`run_js`内からの要求には親run・toolのIDが付きます。共通の`model`は親agentの設定モデルを保持し、観察の段階も`run_js`のままです。通常JSからの直接要求にagent/run IDを新設しません。親のファイル観察では意思決定イベントを差分として受け取れます。通常stdoutの結果は人間向け`--observe`表示では省略するため、判断JSONを見る場合は通常出力または保存したstdoutイベントを確認してください。

[APIリファレンス](api.md) · [実装計画](development/decision-plan.md)
