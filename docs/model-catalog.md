# モデルカタログ

モデルの選択情報は、provider設定と同じJSONの`providers.<provider>.models.<model>.catalog`に置きます。[組込みdefaults.json](../internal/agent/provider/defaults.json)を土台に、`~/.config/noe/config.json`または`NOE_CONFIG_FILE`で指定項目だけを上書きします。読込み時点・マージ規則は[provider設定](providers.md)と共通です。

```json
{
  "providers": {
    "opencode-go": {
      "models": {
        "deepseek-v4.1-flash": {
          "catalog": {
            "kind": "agent",
            "description": "普段のコード修正・調査に優先して使う候補",
            "tags": ["coding", "routine"]
          }
        },
        "gpt-6-luna": {
          "catalog": {
            "description": "短い分類・軽量なタスクの候補"
          }
        }
      }
    }
  }
}
```

この例では`gpt-6-luna`の既定のResponses経路も保持します。通信設定の`api`・`path`・`parameters`などは`catalog`の外側に置きます。`catalog`は要求bodyへ送信せず、認証・接続設定もカタログAPIへ含めません。

## JavaScriptからの取得

```js
import { list_models } from "noe:agent";

const all = await list_models();
const agents = await list_models({ provider: "opencode-go", kind: "agent" });
const decisions = await list_models({ kind: "decision" });
const imageModels = await list_models({ kind: "image" });
console.log(JSON.stringify(agents, null, 2));
console.log(JSON.stringify(imageModels, null, 2));
```

返却値は`[{provider, model, kind, description?, tags?, capabilities?, contextWindow?, maxOutputTokens?, efforts?}, ...]`です。provider、model IDの順で並び、該当なしは`[]`、不明なprovider指定はエラーです。`provider`・`kind`はどちらも省略できます。通常JSとagentの`run_js`で利用でき、`run_js`では`await import("noe:agent")`から取得します。

取得にAPIキーや外部API要求は不要です。子プロセスからも親ホストの固定済みカタログを参照します。取得結果をJS側で変更してもホストのカタログは変わりません。

## 選択情報の項目

| 項目 | 意味 |
| --- | --- |
| `kind` | `agent`・`decision`・`image`。省略時は`agent`。`image`は画像生成候補 |
| `description` | 用途・得意分野・選択方針の説明 |
| `tags` | 呼出し側で絞り込むための文字列配列。上書き時は配列全体を置換 |
| `capabilities` | `tools`・`vision`・`reasoning`など、能力名と真偽値のmap |
| `contextWindow` | モデルのcontext windowのtoken数。省略または0なら不明 |
| `maxOutputTokens` | モデルの最大出力token数。省略または0なら不明 |
| `efforts` | 選択候補として記録するeffortの配列 |

能力の項目がない場合は不明です。`false`は未対応という記録です。モデル能力とNOEの入力対応は別で、モデルに`vision: true`を設定してもagentへ画像を渡す機能は追加されません。既知の`contextWindow`は自動context管理で実行履歴の入力予算を計算する際に使われます。window由来の予算計算では、`maxOutputTokens`は正の`maxTokens`と`contextPolicy.responseReserveTokens`のどちらも指定されていない場合に応答予約として使われます。これらのcatalog値はproviderへの出力上限を設定せず、呼出しを制限しません。windowが不明で明示`contextBudget.inputTokens`もない場合の1,000,000-token fallbackは、モデルcapacityを表しません。`efforts`と`capabilities`は選択用の情報です。ルートagent起動時は引き続きprovider・model・effortを明示します。

カタログは利用者が選んだ候補一覧であり、プロバイダの全モデル一覧や利用可能性の保証ではありません。`catalog`がない通信設定だけのモデルは一覧へ出ませんが、直接指定して実行できます。`"catalog": null`で一覧から外し、`"モデルID": null`でそのモデルの通信上書きも含めて削除します。新しいモデルの追加も同じJSONで行えます。

組込み候補はOpenRouterのDeepSeek V4.1 Flash・GPT-6 Lunaと3種類の意思決定モデル、OpenCode GoのDeepSeek V4.1 Flash・GPT-6 Luna、OpenAIのChatGPT/APIキー経路それぞれのgpt-6.1-sol・gpt-6-lunaです。画像生成候補としてOpenRouterの`openai/gpt-image-2.5-flare`とOpenAI APIキー経路の`gpt-image-2.5-flare`も`kind: "image"`で一覧できます。生成モデルのIDと初期説明は[DeepSeekのモデルページ](https://openrouter.ai/deepseek/deepseek-v4.1-flash)・[GPT-6 Lunaのモデルページ](https://openrouter.ai/openai/gpt-6-luna)を2026-10-17に確認したものです。意思決定モデルとOpenCode Go経路の実接続範囲は[Decisions検証](development/decision-openrouter-validation.md)・[provider検証](development/provider-config-validation.md)を参照してください。価格や性能の自動更新は行いません。説明・タグは自身の評価と選択方針に合わせて上書きしてください。

画像の利用例と生成providerの制約は[画像ガイド](agent-images.md)を参照してください。`kind: "image"`はcatalogで選択する候補の分類であり、認証やproviderの機能対応を保証するものではありません。

## context metadataの設定値

2026-10-18確認。単位はtokens。`—`はカタログの`maxOutputTokens`を未設定としている項目。

| provider | model | contextWindow | maxOutputTokens | 根拠 |
| --- | --- | ---: | ---: | --- |
| openrouter | deepseek/deepseek-v4.1-flash | 1,048,576 | — | [モデルページ](https://openrouter.ai/deepseek/deepseek-v4.1-flash)・[モデルAPI](https://openrouter.ai/api/v1/models) |
| openrouter | openai/gpt-6-luna | 1,050,000 | 128,000 | [モデルページ](https://openrouter.ai/openai/gpt-6-luna)・モデルAPI |
| openrouter | typesafe/jev-1.14 | 262,144 | — | [モデルページ](https://openrouter.ai/typesafe/jev-1.14)・[endpoint API](https://openrouter.ai/api/v1/models/typesafe/jev-1.14/endpoints) |
| openrouter | perplexity/pplx-decider-v1.2 | 262,144 | — | [モデルページ](https://openrouter.ai/perplexity/pplx-decider-v1.2)・[endpoint API](https://openrouter.ai/api/v1/models/perplexity/pplx-decider-v1.2/endpoints) |
| openrouter | cloudflare/clef | 65,536 | — | [モデルページ](https://openrouter.ai/cloudflare/clef)・[endpoint API](https://openrouter.ai/api/v1/models/cloudflare/clef/endpoints) |
| opencode-go | deepseek-v4.1-flash | 1,048,576 | — | 利用者指定により対応するOpenRouterモデルから採用 |
| opencode-go | gpt-6-luna | 1,050,000 | 128,000 | 利用者指定により対応するOpenRouterモデルから採用 |
| openai-chatgpt | gpt-6.1-sol | 872,000 | 128,000 | アカウントのモデルAPI最大window・[公式モデル仕様](https://developers.openai.com/api/docs/models/gpt-6.1-sol) |
| openai-chatgpt | gpt-6-luna | 872,000 | 128,000 | アカウントのモデルAPI最大window・[公式モデル仕様](https://developers.openai.com/api/docs/models/gpt-6-luna) |
| openai-api | gpt-6.1-sol | 1,050,000 | 128,000 | [公式モデル仕様](https://developers.openai.com/api/docs/models/gpt-6.1-sol) |
| openai-api | gpt-6-luna | 1,050,000 | 128,000 | [公式モデル仕様](https://developers.openai.com/api/docs/models/gpt-6-luna) |

**ChatGPT経路:** [公式のモデル取得手順](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)に従い、NOEで使用する認証の`GET https://api.openai.com/v1/models`を確認した。Solは`context_window: 500000`、Lunaは`context_window: 272000`、両方とも`max_context_window: 872000`だった。NOEは長い入力に使う最大値を採用し、最大出力の予約には公式モデル仕様の128,000を使う。アカウントの取得値は全アカウント共通の保証ではなく、利用者の取得値に合わせて外部JSONで上書きできる。モデル要求bodyは変更していない。

**OpenCode Go:** [公式Goドキュメント](https://opencode.ai/v2/docs/console/go/)でwindow・最大出力を確認できなかったため、今回の利用者指定で対応するOpenRouterの設定値を採用した。OpenCode Go経路で実測したcapacityを表すものではない。

**DeepSeekの出力予約:** [endpoint API](https://openrouter.ai/api/v1/models/deepseek/deepseek-v4.1-flash/endpoints)の最大出力は提供元ごとに異なり、取得例では131,072〜943,718 tokens。モデルページのFAQには1,048,576 completion tokensともある。単一の`maxOutputTokens`としては未設定を維持する。現行実装はこの値を出力予約にも使うため、943,718を設定すると1M windowでも実行履歴予算が約9万tokensへ減る。出力予約を明示したい場合は`contextPolicy.responseReserveTokens`、または実際の出力上限を指定する`maxTokens`を利用できる。

**意思決定モデル:** `kind: decision`の3件はwindowだけを記録し、生成モデル用の出力予約は設定しない。`noe:decision`へのstateは引き続き切り捨て・圧縮せず、一度の要求として渡す。ClefのWorkers AI経路が長いtext stateの先頭約2,000 tokensだけを扱う制約は、window値とは別に[意思決定ガイド](decision.md)を参照すること。

## Jevによる起動前の選択

[agent-route-model.js](../examples/agent/agent-route-model.js)は、次の順で処理します。

1. `list_models({provider, kind:"agent"})`から候補を取得。
2. 説明をJevの`choice`の`criteria`へ渡す。
3. 返却されたキーで元の候補を引く。
4. 選ばれたprovider・modelと明示した`effort:"auto"`で親agentを起動。

```sh
./noe run --observe examples/agent/agent-route-model.js openrouter 'README.mdを3点に要約して'
```

Jev用のOpenRouterキーと、選択先providerのキーが必要です。呼出し元のJSで候補の絞込み・effortのプリセットを追加できます。NOEが全agentの起動前にJevを強制的に呼ぶ仕組みではありません。

## 今後のSDK追加

カタログの識別子は`provider`と`model`の組です。OpenAI SDKを追加済みで、認証別の`openai-chatgpt`・`openai-api`も同じモデル情報と`list_models`を利用します。将来のAnthropic系SDKも同じ構造を利用できます。SDKやChat・Responses・Messagesの変換処理はGoの通信adapterが担当します。Anthropic系SDKとMessages対応は未実装です。[OpenAIの利用方法](openai.md)も参照してください。

## 検証

外部差分のマージ・削除・固定読込み・通信設定との分離、認証なしの通常JS、Jevでの候補選択から起動・`run_js`で同じカタログを参照する経路をローカルfixtureで確認しています。このカタログ選択例の有料実接続は未実施です。
