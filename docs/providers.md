# Provider設定

NOEは組込みの[defaults.json](../internal/agent/provider/defaults.json)を土台に、外部JSONで指定された項目だけを上書きします。設定なしでOpenRouter・OpenCode Go・OpenAIの接続定義を利用でき、接続先・モデル別のAPI形式・ヘッダ・送信パラメータをGoの編集なしで変更できます。API形式の変換処理はGoにあります。

## 読込みと上書き

既定の外部ファイルは`$XDG_CONFIG_HOME/noe/config.json`、XDG未設定時は`~/.config/noe/config.json`です。`NOE_CONFIG_FILE=/path/to/config.json`で別のファイルを指定できます。既定ファイルがなければ組込みだけで動作します。明示指定したファイルが読めない場合や不正なJSONの場合は、agent利用時にエラーになります。

設定はGoホストが最初にagent/decisionを利用するときに読み込み、そのホストでは固定します。moduleのimportや通常のJSだけでは読み込みません。認証ファイルは最初のモデル要求時に読みます。

objectは再帰的に上書きし、配列・文字列・数値・真偽値は置換、`null`はそのキーの削除です。変更しない値を外部ファイルへ複製する必要はありません。provider内の`models`では、model IDごとの部分上書きができます。

モデル選択用の説明や能力は、同じ`models.<model>.catalog`へ置きます。`list_models()`による取得とJevへの受渡しは[モデルカタログ](model-catalog.md)を参照してください。カタログも同じホスト設定として固定し、最初の`list_models()`でも読込みが発生します。

```json
{
  "providers": {
    "opencode-go": {
      "keyFile": "/home/me/.config/hjh/credentials/opencode-go-api-key",
      "models": {
        "deepseek-v4.1-flash": {
          "parameters": {"temperature": 0.2}
        }
      }
    }
  }
}
```

## 組込みprovider

| provider | API | キーファイルの既定path | 上書き用環境変数 |
| --- | --- | --- | --- |
| `openrouter` | Chat Completions | `~/.config/noe/credentials/openrouter-api-key` | `NOE_OPENROUTER_API_KEY_FILE` |
| `opencode-go` | モデル別にChat Completions / Responses | `~/.config/noe/credentials/opencode-go-api-key` | `NOE_OPENCODE_GO_API_KEY_FILE` |
| `openai-chatgpt` | Responses / OpenAI SDK | `~/.config/noe/credentials/openai-chatgpt.json` | `NOE_OPENAI_CHATGPT_CREDENTIALS_FILE` |
| `openai-api` | Responses / OpenAI SDK | `~/.config/noe/credentials/openai-api-key` | `NOE_OPENAI_API_KEY_FILE` |

ChatGPTのJSONはOAuth credential recordです。APIキーのテキストファイルとは形式が異なります。ヘルパーの配置・登録・既存hjh資格情報の扱いは[OpenAIガイド](openai.md)を参照してください。

上表の`~/.config`はXDG_CONFIG_HOME指定時にはその値に変わります。キーは改行付きでもよいテキストファイルへ保存します。キーの値をJSや設定JSONへ書く必要はありません。キーpath用環境変数は、設定の`keyFile`より優先します。

既存の`NOE_OPENROUTER_BASE_URL`も維持します。OpenRouterの接続先を上書きし、Decisionsのserver URLも従来どおり末尾の`/api/v1`を除いて導きます。外部JSONで接続先を変える場合、通常生成の`baseURL`とDecisionsの`decisionServerURL`は別項目です。

## OpenCode Go

```js
import { run_agent } from "noe:agent";

const snapshot = await run_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
}, "run_jsで19 * 23を計算してください。");
console.log(snapshot.lastResult.answer);
```

```sh
NOE_OPENCODE_GO_API_KEY_FILE=/path/to/opencode-go-api-key \
  ./noe run --observe examples/agent/agent-opencode-go.js

NOE_OPENCODE_GO_API_KEY_FILE=/path/to/opencode-go-api-key \
  ./noe run --observe examples/agent/agent-opencode-go.js gpt-6-luna
```

OpenCode Goのmodel IDには`deepseek/`や`opencode-go/`のprefixを付けません。既定ではChat Completionsを使い、組込みモデル表の`gpt-6-luna`等はResponsesを使います。**Messages形式は未対応です。** 新しいモデルの対応形式は[OpenCode公式資料](https://opencode.ai/v2/docs/console/go/)で確認し、必要に応じて`models`へ設定を追加します。

Go側で`User-Agent: noe/<version>`を付与し、`x-opencode-session`には実際の`runId`を送ります。同じrunのモデル往復では固定し、親・子・次のqueue taskでは別IDです。OpenRouter用の`cacheKey`/`session_id`とは別の設定で、`cacheKey`を空にしてもOpenCode Goの会話ヘッダは送り続けます。

Chatの思考はprovider設定の`reasoningField`に従って連結・保持・再送します。OpenCode Goの既定は`reasoning_content`です。Responsesではtool callやreasoningを含むnative output itemsを履歴へ保持し、次の要求のinputへ再送します。providerが返したthinking・summary・回答の断片は既存の観察イベントへ流れます。

## モデル要求の再試行

組込みproviderのagentモデル要求は、通信障害・HTTP 429・5xx・不完全なstreamで自動再試行します。最大3試行で、1回目の失敗後に2秒、2回目の失敗後に4秒待機します。認証失敗・残高不足・入力不正・取消しは再試行しません。待機中もinterrupt・timeout・所有元の終了による取消しが有効です。回数・待機時間の設定項目はありません。

各試行は同じ入力を送り、完了済みのtool・hookは再実行しません。失敗した試行の部分応答は会話履歴へ追加せず、成功した試行の完全な応答だけを返します。既に表示・JSONLへ出たthinking等の断片は残り、再試行による重複を許容します。途中まで受信したtool callは実行しません。

retry専用のobserve表示・JSONLイベントは追加せず、`model.started`・`context.updated`・`model.finished` / `model.failed`は論理要求ごとです。`maxSteps`・`providerCalls`も論理要求単位で、内部のHTTP試行回数や課金量を示すものではありません。usageは最終的に取得できた成功応答の値を保持し、失敗した試行の不明なusageは推測しません。

SDK側の暗黙のretryは無効のまま、NOE側で上記の再試行だけを行います。対象はOpenRouter・OpenCode Go・OpenAI等の組込みagent生成経路です。単発decision・画像生成・Goホストから注入した独自providerの再試行方針は変更しません。

## 設定項目

| 項目 | 意味 |
| --- | --- |
| `sdk` | `openrouter`（省略時）または`openai` |
| `auth` | `api-key`（省略時）、`chatgpt-oauth`、`none`。後二つはOpenAI SDKで利用 |
| `oauthFile` | ChatGPTのJSON credential recordのpath |
| `oauthFileEnv` | `oauthFile`を上書きする環境変数の名前 |
| `api` | `chat-completions`または`responses` |
| `baseURL` | APIのbase URL。HTTP/HTTPS |
| `path` | base URLに加えるpath。例：`/chat/completions`、`/responses` |
| `keyFile` | APIキーファイルのpath |
| `keyFileEnv` | キーファイルpathを上書きする環境変数の名前 |
| `headers` | 静的なHTTPヘッダ |
| `parameters` | 静的なJSONリクエストパラメータ |
| `sessionHeader` | 値にrunIdを入れるヘッダ名 |
| `sessionField` | 値にcacheKeyを入れるJSONフィールド。dotで階層指定可 |
| `reasoningField` | Chat応答の思考フィールド名 |
| `effortField` | 明示effortの送信先。例：`reasoning.effort`、`reasoning_effort` |
| `decisionServerURL` | OpenRouter Decisions操作のserver URL。operationが`/api/alpha/decisions`を加える |
| `imageGeneration` | 画像生成用endpointの`baseURL`・`path`とprovider既定の`parameters` |
| `models` | model IDごとの項目上書き |

モデルentryの`catalog`は選択用metadataとして通信設定から分離し、送信パラメータには含めません。

`${configDir}`はXDGに従ったNOE設定directory、`${userAgent}`は`noe/<version>`へ展開します。前者は`keyFile`・`oauthFile`、後者は`headers`で使えます。キー自体の環境変数展開や任意コード実行は行いません。

静的`parameters`を土台に、実行時のmodel・messages/input・tools・streamを設定します。明示した`maxTokens`と`effort`が対応するパラメータより優先します。`effort: "auto"`は実行時のeffort指定を加えず、設定されたprovider既定値に任せます。APIやモデルが受け取るパラメータの違いは設定側で合わせてください。

Responsesでは全履歴を送る方式を使い、`store: false`にします。既定で`include: ["reasoning.encrypted_content"]`を要求し、返されたreasoning itemsをtool callと一緒に次のinputへ渡します。独自の会話履歴をサーバーへ保存するAPIは提供しません。

## 画像生成endpoint

組込みdefaultsは`openrouter`に`imageGeneration: {baseURL: "https://openrouter.ai/api/v1", path: "/images"}`、`openai-api`に`imageGeneration: {baseURL: "https://api.openai.com/v1", path: "/images/generations"}`を設定しています。`baseURL`はHTTP/HTTPS URL、`path`は`/`で始まるendpoint pathです。通常のchat/Responses endpointとは独立した画像生成経路です。`parameters`にはprovider既定の画像生成optionを置けます。

外部JSONは組込みdefaultsへ再帰的にmergeされるため、画像生成optionだけの差分を指定できます。

```json
{
  "providers": {
    "openrouter": {
      "imageGeneration": {
        "parameters": {"quality": "low"}
      }
    }
  }
}
```

呼び出しごとの`generate_image({..., parameters})`はこれらのprovider既定optionを上書きします。初期対応の組込みproviderはOpenRouterとOpenAI API key (`openai-api`)です。ChatGPT OAuth (`openai-chatgpt`)とOpenCode Go (`opencode-go`)の画像生成は未対応です。非対応providerから別経路へ自動fallbackしません。画像生成にはprovider認証が必要で、利用料が発生する場合があります。引数と保存動作は[画像ガイド](agent-images.md)を参照してください。

## 独自providerとモデル

同じ通信形式のproviderを外部設定へ追加できます。

```json
{
  "providers": {
    "my-compatible-provider": {
      "api": "chat-completions",
      "baseURL": "https://example.com/v1",
      "path": "/chat/completions",
      "keyFile": "${configDir}/credentials/my-provider-api-key",
      "reasoningField": "reasoning_content",
      "effortField": "reasoning_effort"
    },
    "opencode-go": {
      "models": {
        "new-responses-model": {
          "api": "responses",
          "path": "/responses",
          "effortField": "reasoning.effort"
        }
      }
    }
  }
}
```

呼出し時は`provider: "my-compatible-provider"`を指定します。利用できる範囲はOpenRouter Go SDKとNOEの各API adapterが扱える互換形式です。新しい独自API形式を追加するにはGo側の対応が必要です。

`noe:decision`は引き続き`provider: "openrouter"`の専用APIです。agentのprovider設定を自動継承しません。
