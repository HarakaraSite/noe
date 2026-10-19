# agentの認証・起動・観察

[READMEへ戻る](../README.md) · [APIリファレンス](api.md) · [agentの実行例](../examples/agent/README.md)

NOEのagentは、指示を受けて`run_js`でファイル操作・JavaScriptやコマンドの実行・失敗の修正を行い、最終回答を返します。認証とprovider通信はGoが担当します。生成したJSはNOEを起動したユーザーのOS権限で動き、人間の承認を待たずに処理を進めます。

以下はビルド済みの`./noe`を使い、リポジトリのルートから実行する手順です。

## 1. providerと認証を準備する

利用するproviderを一つ選びます。model IDはproviderによって異なるため、`provider`と`model`を組み合わせて指定します。

| `provider` | `model`の例 | `~/.config/noe/credentials/`以下の認証ファイル |
| --- | --- | --- |
| `opencode-go` | `deepseek-v4.1-flash`、`gpt-6-luna` | `opencode-go-api-key`（APIキーのテキスト） |
| `openrouter` | `deepseek/deepseek-v4.1-flash`、`openai/gpt-6-luna` | `openrouter-api-key`（APIキーのテキスト） |
| `openai-chatgpt` | `gpt-6.1-sol`、`gpt-6-luna` | `openai-chatgpt.json`（ChatGPTサインイン） |
| `openai-api` | `gpt-6.1-sol`、`gpt-6-luna` | `openai-api-key`（APIキーのテキスト） |

APIキーは対応するファイルへ保存します。`XDG_CONFIG_HOME`が設定されている場合、基点は`~/.config/noe/`から`$XDG_CONFIG_HOME/noe/`へ変わります。別の場所を使う場合は次の環境変数を指定します。

| provider | 認証ファイルを指定する環境変数 |
| --- | --- |
| OpenCode Go | `NOE_OPENCODE_GO_API_KEY_FILE` |
| OpenRouter | `NOE_OPENROUTER_API_KEY_FILE` |
| ChatGPT | `NOE_OPENAI_CHATGPT_CREDENTIALS_FILE` |
| OpenAI API | `NOE_OPENAI_API_KEY_FILE` |

認証ファイルは最初のモデル要求時に読み込みます。moduleのimportだけではproviderへ接続しません。接続先や送信パラメータを変える場合は[provider設定](providers.md)、候補モデルは[モデルカタログ](model-catalog.md)を参照してください。

### ChatGPTサインインを使う場合

APIキーの代わりに外部JSヘルパーで登録します。

```sh
noe_config_dir="${XDG_CONFIG_HOME:-$HOME/.config}/noe"
mkdir -p "$noe_config_dir"
cp scripts/auth-chatgpt.js "$noe_config_dir/auth-chatgpt.js"
./noe run "$noe_config_dir/auth-chatgpt.js"
```

表示されたURLをブラウザで開いて登録します。バイナリだけを取得した場合は、`cp`の代わりに[v1.12.0のヘルパー](https://forge.harakara.site/littleisland/noe/raw/tag/v1.12.0/scripts/auth-chatgpt.js)を取得します。リモート端末での登録、認証更新、APIキーとの違いは[OpenAIガイド](openai.md)を参照してください。

## 2. 一つのタスクを実行する

次を`task.js`として保存します。

```js
import { run_agent } from "noe:agent";

const snapshot = await run_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
  name: "summary",
}, "README.mdを読んで、日本語で3点に要約してください。");

if (snapshot.lastResult?.state !== "completed") {
  throw new Error(snapshot.lastResult?.error || snapshot.reason || "Agent failed");
}
console.log(snapshot.lastResult.answer);
```

```sh
./noe run task.js
```

root agentには`provider`・`model`・`effort`が必須です。`effort: "auto"`はprovider/modelの既定に任せます。別providerを使う場合は、最初の表に従って`provider`と`model`を両方変更します。

`run_agent()`はinstanceの停止と後始末を待ち、snapshotを返します。通常のtask失敗は`lastResult.state`と`lastResult.error`へ入るため、例のように状態を確認します。入力不備や起動受付の失敗はPromiseをrejectします。結果の状態やオプションの一覧は[APIリファレンス](api.md#設定)を参照してください。

## 3. 実行状況とログを見る

```sh
# 進捗と最終回答を画面へ表示
./noe run --observe task.js

# 画面表示に加え、詳細イベントをJSONLへ保存
./noe run --observe --events=agent.jsonl task.js

# 別ターミナルから保存中のログを追う
./noe observe --follow agent.jsonl

# JSONLを別プログラムへ渡す
./noe run --events=- task.js | jq --unbuffered -c .
```

`--observe`はthinking・summary（providerが返す場合）、途中の発言、`run_js`の先頭コメント・成功失敗、stderr、最終回答を表示します。通常のスクリプト/toolのstdoutはこの表示から省きます。表を含むMarkdownは色なしで整形します。

`run`・`eval`は`--events`でagentイベントを記録できます。パス指定は追記、単独指定は起動時のcwdに日時・ID付きのログを作り、`--events=-`はstdoutをJSONLにします。同じstdoutを使う`--observe`と`--events=-`は併用しません。従来の`NOE_AGENT_TRACE=1`はstderrへメタデータtraceを出します。

捕捉する`run_js`のstdout・stderrは各64KiB（先頭32KiB＋末尾32KiB）までです。超過分は省略マーカーとバイト数を残し、自動保存はしません。完全なログが必要ならJSやコマンドからファイルへ書き出します。イベント内容と親からの観察は[JSONLイベント](events.md)・[観察の入口](observe.md)を参照してください。

## 4. 指示をファイルから渡す

既存の[try-agent.js](../examples/agent/try-agent.js)なら、agent起動用のJSを毎回書かずに指示を渡せます。

```sh
./noe run --observe examples/agent/try-agent.js \
  --provider opencode-go --model deepseek-v4.1-flash --effort auto \
  'README.mdを読み、要約をSUMMARY.mdへ保存してください。'

./noe run --observe examples/agent/try-agent.js \
  --provider opencode-go --model deepseek-v4.1-flash --effort auto \
  --task-file /path/to/task.txt
```

`--task-file`はUTF-8のtask指示です。相対パスは起動時の作業ディレクトリ基準です。`try-agent.js`の引数を省略したときの設定はOpenRouter・DeepSeek V4.1 Flash・effort maxで、上の例は明示的に上書きしています。

instanceに固定する追加指示は`~/.config/noe/instructions/*.md`（XDG指定時はその設定先）とworkspaceの`.noe/instructions/*.md`から読み込みます。組込み→共通→workspace→明示した`system`の順に追記し、各ディレクトリ内はファイル名順です。同じinstanceのqueue taskでは再読込みしません。任意のファイルもJSで読んだ文字列を`system`に渡せます。`AGENTS.md`の自動探索は行いません。詳しくは[インストラクションガイド](instructions.md)を参照してください。

## 次の使い方

| 内容 | 参照先 |
| --- | --- |
| `maxSteps`・`maxTokens`・effort・cacheKeyと結果の状態 | [agent APIの設定](api.md#設定) |
| 待たずに起動・並列委譲・queue・steer・interrupt | [ライフサイクルAPI](api.md#自律agentopenrouter) · [起動と制御の実行例](../examples/agent/README.md) |
| `--timeout`やCLIの終了コード | [CLIリファレンス](api.md#cli) |
| hook・コンテキスト通知と切り捨て・共有実行上限 | [hookとcontext](hooks.md) |
| `read_image`・`generate_image` | [画像ガイド](agent-images.md) · [画像の実行例](../examples/image/README.md) |
| Jev等への構造化判断の依頼 | [意思決定ガイド](decision.md) |
| モデル候補とルーティング | [モデルカタログ](model-catalog.md) · [Jevによる選択例](../examples/agent/agent-route-model.js) |

子agentは親の指定を一部継承しますが、独立した会話を持ちます。queueは同じkeepAlive instanceの確定済み会話を継続します。待受instanceもNOEや所有する親runの終了・取消しで回収され、独立したdaemonにはなりません。具体的な引数・継承・寿命の規則は上のAPIリファレンスへ集約しています。

旧`createAgent()`・`agent.run()`から移る場合は[API移行ガイド](api.md#旧apiからの移行)を参照してください。
