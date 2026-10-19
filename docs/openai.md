# OpenAI SDKとChatGPTサインイン

OpenAI公式Go SDK `github.com/openai/openai-go/v3 v3.79.0`を利用します。OpenAIのSol・LunaはResponsesを既定とし、同じSDKで互換接続先のChat Completionsも扱います。OpenRouter・OpenCode Goの既定SDKは引き続きOpenRouter SDKです。

## 接続設定

| provider | SDK | API | 認証ファイルの既定値 |
| --- | --- | --- | --- |
| `openai-chatgpt` | OpenAI | Responses | `~/.config/noe/credentials/openai-chatgpt.json` |
| `openai-api` | OpenAI | Responses | `~/.config/noe/credentials/openai-api-key` |

OpenAIを使う通常の例は、利用者の方針に従って`openai-chatgpt`を優先します。利用する認証は起動時のprovider指定で決まります。どちらにも`gpt-6.1-sol`・`gpt-6-luna`をカタログ候補として持ちます。普段のOpenCode Go優先、OpenRouterでのモデル試用という運用も継続できます。

```js
import { run_agent } from "noe:agent";

const result = await run_agent({
  provider: "openai-chatgpt",
  model: "gpt-6-luna", // または gpt-6.1-sol
  effort: "auto",
}, "README.mdを3点に要約してください。");
console.log(result.lastResult?.answer);
```

APIキーを利用するときは`provider: "openai-api"`を指定します。外部JSONによる部分上書きは[provider設定](providers.md)と共通です。

| 環境変数 | 意味 |
| --- | --- |
| `NOE_OPENAI_CHATGPT_CREDENTIALS_FILE` | ChatGPTのJSON credential recordのpath |
| `NOE_OPENAI_API_KEY_FILE` | OpenAI APIキーテキストファイルのpath |

XDG_CONFIG_HOME指定時は`~/.config`を置き換えます。接続先は両方とも`https://api.openai.com/v1/responses`です。ChatGPT経路は公式のChatGPTプラン用OAuthで、`store:false`・`stream:true`とし、トークン更新はGoホストが担当します。[公式の推論手順](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)

`effort:"auto"`はeffortを送らず、モデル既定値を使います。明示effortは`reasoning.effort`へ送信します。組込み設定では`reasoning.summary:"auto"`を要求し、返されたsummaryを既存の観察イベントへ流します。`cacheKey`は`prompt_cache_key`へ渡し、未指定ならrunIdを使います。

## 認証ヘルパー

配布元は[`scripts/auth-chatgpt.js`](../scripts/auth-chatgpt.js)、通常の配置先は`~/.config/noe/auth-chatgpt.js`です。ヘルパーは単体でコピーして使えます。

```sh
mkdir -p ~/.config/noe
cp scripts/auth-chatgpt.js ~/.config/noe/auth-chatgpt.js
./noe run ~/.config/noe/auth-chatgpt.js
```

表示されたURLをブラウザで開きます。ループバックの`/auth/callback`で認証を受け、GoホストがIDトークンのRSA署名・issuer・audience・有効期限・nonceとChatGPTプラン権限を確認して、credential recordを保存します。新規登録時はNOEというアプリ名とホストIDを送り、発行されたclient IDを保持します。再ログインは同じ登録を使います。[公式の登録・サインイン手順](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)

VMやSSH接続先で実行し、ブラウザがVMのループバックへ到達できない場合は、最初のターミナルを待機させたまま、別ターミナルで次を実行します。

```sh
./noe run ~/.config/noe/auth-chatgpt.js complete
```

ブラウザに表示された`http://127.0.0.1:.../auth/callback?...`のURLを貼り付け、Enterの後にCtrl+Dで入力を終えます。戻り先URLを最初のヘルパーへ届け、最初のターミナルへ保存結果が表示されます。

保存先を変えるときは`NOE_OPENAI_CHATGPT_CREDENTIALS_FILE`、または`login`/`complete`の次の引数で指定します。外部configの`oauthFile`だけを変更した場合も、ヘルパーへ同じpathを指定してください。別アカウントを登録するときは別のファイルを指定します。

```sh
./noe run ~/.config/noe/auth-chatgpt.js login /path/to/another-account.json
./noe run ~/.config/noe/auth-chatgpt.js complete /path/to/another-account.json
```

ヘルパーのGo側入口は`noe:auth`の`save_chatgpt_credentials({file,clientId,hostId,nonce,tokens})`です。外部JSで行ったOAuthコード交換の結果を検証・保存し、保存path・email・期限を返します。token値は返しません。このmoduleは通常JSで利用でき、Workersには渡しません。

## 既存credential recordのコピー

hjhの選択中アカウントのJSONは同じ形式なので、NOEの`openai-chatgpt.json`へコピーできます。APIキーはテキストファイルのままコピーします。認証ファイルはソース管理の外で保持します。

ChatGPTのrefresh tokenは更新時に置き換わります。同じ認証セッションを複数のファイルへコピーして両方から更新すると、片方が古くなります。今回の移行検証では利用者の許可に従い、更新済みrecordをhjhにも同期しています。継続して両方を使う場合、各アプリで別セッションを登録するか、同一recordのpathを両方から参照します。NOEは同一ファイルへの更新をプロセス間file lockで直列化します。[公式のトークン更新手順](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions#refreshing-tokens)

## OpenAI互換のローカル接続先

外部JSONへ、SDK・通信形式・認証を指定します。以下のURLとmodel IDは実際のローカルサーバーに合わせて変更します。

```json
{
  "providers": {
    "local": {
      "sdk": "openai",
      "api": "chat-completions",
      "baseURL": "http://127.0.0.1:8080/v1",
      "path": "/chat/completions",
      "auth": "none",
      "reasoningField": "reasoning_content",
      "effortField": "reasoning_effort",
      "models": {
        "my-local-model": {
          "catalog": {"kind": "agent", "description": "Local worker"}
        }
      }
    }
  }
}
```

`run_agent({provider:"local",model:"my-local-model",effort:"auto"}, task)`で利用します。サーバーがキーを要求する場合は`auth:"api-key"`と`keyFile`を指定します。SDKが環境から読むOpenAIキー・organization・project・base URLは、NOEの選択した設定で置き換えます。

## Responsesの履歴と観察

`response.completed`まで読み、完成したoutput itemsを保持します。最終イベントの`output`が空の場合も、途中の`response.output_item.done`から組み立てます。reasoningの暗号化された内容や未知の追加フィールドもraw JSONで保持し、次のモデル要求へtool結果とともに再送します。応答完了後はHTTP EOFを待ちません。

途中のtext・thinking・summaryは既存のJSONLと`--observe`で観察できます。stream開始後のChatGPT利用枠エラーはquotaとして扱います。詳しい確認結果は[実装・検証記録](development/openai-implementation.md)を参照してください。
