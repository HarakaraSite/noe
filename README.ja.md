# NOE — No Ordinary Engine

Just JavaScript. Until you need an agent.

A tiny JavaScript runtime written in pure Go.

この文書は日本語版です。正本は[英語版README](README.md)です。
小さなスクリプトと自律agentのタスクを実行するGo製ランタイムです。JavaScriptで処理を書いたり、`run_agent()`へ仕事を頼んでファイル操作・JS実行・修正・回答まで任せたりできます。JavaScriptエンジンには[go-quickjs](https://github.com/go-quickjs/go-quickjs)を使います。

**現在の対応言語はJavaScriptです。** npm・パッケージ管理は対象外で、Node.jsやDenoの完全互換は目標にしていません。固定したgo-quickjs標準ライブラリのWeb APIとNode形式APIを有効にしていますが、各APIは一部実装です。

[APIリファレンス](docs/api.md) · [import map](docs/import-map.md) · [旧APIからの移行](docs/api.md#旧apiからの移行) · [REPLガイド](docs/repl.md) · [実行例](examples/README.md) · [開発記録](docs/README.md)

[v1.5.0リリースノート](docs/releases/v1.5.0.md) · [バイナリのダウンロード](https://forge.harakara.site/littleisland/noe/releases/tag/v1.5.0)

## ビルドと実行

Go 1.27.2以降が必要です。Cのツールチェーンは不要です。以下のコマンドはリポジトリのルートで実行します。

```sh
git clone https://forge.harakara.site/littleisland/noe
cd noe
CGO_ENABLED=0 go build -o noe ./cmd/noe
```

```sh
./noe --version
# noe v1.5.0

./noe run examples/hello.js
# Hello from NOE!

./noe eval 'console.log(1 + 1)'
# 2

./noe repl
```

## すぐに試す

標準入力のJSONを集計します。

```sh
printf '[100,200,300]\n' | ./noe run examples/summarize-stdin.js
# {"count":3,"total":600,"average":200}
```

ディレクトリ内のJSONファイルをまとめて集計します。

```sh
./noe run examples/collect-json.js examples/data/numbers
# ファイルごとの結果と、count: 5、total: 800、average: 160を表示
```

非同期の待機を使い、1秒間隔で3回表示します。

```sh
./noe run examples/sleep-demo.js
```

ファイル加工・レポート保存・HTTPダウンロードなどは[実行例の一覧](examples/README.md)を参照してください。

## ローカルモジュールとimport map

`--import-map`で、自作JSや動作確認済みのESMライブラリに再利用しやすい名前を付けられます。[付属のmap](examples/import-map/import-map.json)は、`app/math`をローカルモジュールへ対応付けています。

```json
{
  "imports": {
    "app/math": "./lib/math.mjs"
  }
}
```

スクリプトからは名前でimportできます。

```js
import { sum } from "app/math";
console.log(sum([1, 2, 3]));
```

```sh
./noe run --import-map examples/import-map/import-map.json examples/import-map/main.js
# 6
./noe eval --import-map examples/import-map/import-map.json 'import {sum} from "app/math"; console.log(sum([1,2,3]))'
# 6
./noe repl --import-map examples/import-map/import-map.json
```

REPLでは`const math = await import("app/math")`で読み、次の入力で`math.sum([1, 2, 3])`を実行します。

相対targetは**mapファイルのあるディレクトリ**が基準です。ライブラリ内部の相対importは、そのライブラリのファイルが基準になります。対応表は起動時に読み、Worker・agent task・各`run_js`・子agentへ継承します。agentには使わせたいaliasとライブラリのAPIを指示してください。読取り済みライブラリは同じruntime内で再利用し、新しい`run_js`では独立してファイルを読みます。

初回は`imports`内の完全一致名に対応します。prefix mapping・`scopes`・HTTP/HTTPS import・npm package探索は未対応です。ESM形式でも、実行環境との互換性は別途確認が必要です。[MCP SDKの利用例](scripts/spikes/process-io-mcp/README.md)では、検証済みSDK bundleと独立したJS transportをmain・Worker・agentから使っています。Node/npm/esbuildが必要なのはbundleの準備時だけです。

解決規則、対応するpath、ソースの寿命は[import mapガイド](docs/import-map.md)を参照してください。

## 自律agent

スクリプトから`noe:agent`をimportして起動します。agentは`run_js`でファイルの読み書き、JavaScriptやコマンドの実行、失敗の修正、子agentへの委譲を行い、人間の承認を待たずに回答まで進みます。認証とprovider通信はGoが担当し、生成したJavaScriptはNOEと同じOS権限で動きます。

### providerを選び、認証を登録する

| `provider` | `model`の例 | `~/.config/noe/credentials/`以下の既定認証ファイル |
| --- | --- | --- |
| `opencode-go` | `deepseek-v4.1-flash`、`gpt-6-luna` | `opencode-go-api-key`（テキスト） |
| `openrouter` | `deepseek/deepseek-v4.1-flash`、`openai/gpt-6-luna` | `openrouter-api-key`（テキスト） |
| `openai-chatgpt` | `gpt-6.1-sol`、`gpt-6-luna` | `openai-chatgpt.json`（ChatGPTサインイン） |
| `openai-api` | `gpt-6.1-sol`、`gpt-6-luna` | `openai-api-key`（テキスト） |

APIキーは対応するテキストファイルへ保存します。別の場所を使う場合は`NOE_OPENCODE_GO_API_KEY_FILE`、`NOE_OPENROUTER_API_KEY_FILE`、`NOE_OPENAI_API_KEY_FILE`で指定できます。ChatGPT認証は`NOE_OPENAI_CHATGPT_CREDENTIALS_FILE`です。`XDG_CONFIG_HOME`があれば、既定の基点は`~/.config/noe/`から`$XDG_CONFIG_HOME/noe/`へ変わります。認証ファイルは最初のモデル要求時に読み込みます。

agentへの共通の指示は`~/.config/noe/instructions/*.md`（XDG指定時はその設定先）、
workspaceの指示は起動時の作業ディレクトリの`.noe/instructions/*.md`へ置けます。
共通→workspace→明示した`system`の順で追記し、各ディレクトリ内はファイル名順です。
各run開始時に読み、同じrunの間は固定します。[インストラクションガイド](docs/instructions.md)を参照してください。

ChatGPTサインインには外部JSヘルパーを配置し、NOEで実行します。

```sh
mkdir -p ~/.config/noe
cp scripts/auth-chatgpt.js ~/.config/noe/auth-chatgpt.js
./noe run ~/.config/noe/auth-chatgpt.js
```

表示されたURLをブラウザで開いてください。バイナリだけを取得した場合は、`cp`の代わりに[v1.5.0のヘルパーをダウンロード](https://forge.harakara.site/littleisland/noe/raw/tag/v1.5.0/scripts/auth-chatgpt.js)します。リモート端末での登録や認証更新は[OpenAIガイド](docs/openai.md)を参照してください。OpenAIは公式OpenAI Go SDKのResponsesを既定とし、OpenRouter・OpenCode GoはOpenRouter Go SDKを使います。Chat CompletionsとResponsesに対応し、Messagesは未対応です。

### 1タスクを実行し、回答を受け取る

次を`task.js`として保存します。

```js
import { run_agent } from "noe:agent";

const snapshot = await run_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
  name: "summary",
}, "README.mdを読み、要点を3点にまとめてください。");

if (snapshot.lastResult?.state !== "completed") {
  throw new Error(snapshot.lastResult?.error || snapshot.reason || "Agent failed");
}
console.log(snapshot.lastResult.answer);
```

```sh
./noe run task.js
# 完了後の回答を表示

./noe run --observe --events=agent.jsonl task.js
# 進捗を表示し、詳細なJSONLも保存
```

ルートagentでは`provider`・`model`・`effort`の明示が必須です。providerを変更するときは、providerごとに異なるmodel IDも合わせて変更します。`effort: "auto"`は推論エフォートをprovider/modelに任せます。他に`none`・`minimal`・`low`・`medium`・`high`・`xhigh`・`max`を指定でき、実際の対応はモデルによります。

`run_agent`はinstanceの停止と後始末が終わるまで待ちます。通常のタスク失敗も`snapshot.lastResult`へ返るため、上の例のようにstateを確認してください。入力不正や起動受付の失敗はPromiseがrejectします。追加設定は次のとおりです。

| 設定 | 意味 |
| --- | --- |
| `name` | 観察用の名前。既定は`agent` |
| `system` | 各タスクの実行中に固定する追加インストラクション。外部ファイルの内容も指定可能 |
| `maxSteps` | タスクごとのモデル呼出し上限。省略・`0`はNOE側の上限なし |
| `maxTokens` | モデル要求1回あたりの出力token上限。省略・`0`は指定なし |
| `cacheKey` | キャッシュ・routing用の固定キー。省略するとタスクの`runId`、空文字はフィールド省略 |
| `hooks` | モデル/tool前後に実行する外部ESM一覧。子は継承、`[]`で解除 |
| `contextBudget` | runごとの入力目安`{inputTokens:200000}`。子は継承、`null`で解除 |
| `executionLimits` | root共有の任意上限。`providerCalls`・`agentStarts`・`queuedTasks`・`hookCalls`。省略項目は無制限、`0`は受付枠0 |

各モデル要求へ、明示入力予算・取得できたcatalog window・直前の実測・次要求の概算を通知します。未知値は未取得のままです。概算はJSON UTF-8 bytes/4による近似で、tokenizerの実測ではありません。通知は会話履歴に蓄積せず、入力目安を超えただけで自動停止・圧縮しません。`--observe`でcontext行を表示し、live/保存観察も同じsnapshotを返します。`executionLimits`は別の共有受付上限で、所有rootと子孫に適用します。[hook・contextガイド](docs/hooks.md)を参照してください。

外部インストラクションは、たとえば`node:fs/promises`の`await readFile("instructions.md", "utf8")`で読み、得た文字列を`system`へ渡します。`AGENTS.md`の自動探索は行いません。cacheKeyは会話履歴の保存やキャッシュヒットを保証するものではありません。OpenRouterでは`session_id`、OpenAIでは`prompt_cache_key`へ送ります。OpenCode Goは実際のrun IDを`x-opencode-session`へ送り、同一タスク内では固定、親子では別IDです。`User-Agent: noe/<version>`も送ります。

全体の実行時間を制限する場合は、スクリプト名の前に`--timeout 5m`を置きます。任意の指示を試す場合は、OpenRouter設定済みの[try-agent.js](examples/try-agent.js)も使えます。

```sh
./noe run --observe examples/try-agent.js 'README.mdを読み、要点をSUMMARY.mdへ保存してください。'
```

### 待たずに起動する・観察する・子へ委譲する

| 関数 | 戻るタイミング・役割 |
| --- | --- |
| `run_agent(config, instruction, options?)` | instance停止後にsnapshotを返す |
| `spawn_agent(config, instruction?, options?)` | タスク完了を待たず、受付後に`{agentId}`を返す |
| `observe_agent({agentId})` | 現在のstate・`currentRunId`・`lastResult`をすぐ返す |
| `queue_agent({agentId, instruction})` | `keepAlive` instanceへ次のタスクを予約する |
| `steer_agent({runId, instruction})` | 実行中タスクの次のモデル判断前に指示を加える |
| `interrupt_agent({runId})` / `interrupt_agent({agentId})` | タスク単位またはinstance全体の停止を受け付ける |

通常JSでも`Promise.all([run_agent(...), run_agent(...)])`で独立したルートagentを並列起動できます。それぞれprovider・model・effortを明示します。実行中に親が観察・追加指示を行う場合は`spawn_agent`を使います。[操作例](examples/agent-control.js)も参照してください。

モデルが生成する`run_js`には6つのライフサイクル関数が事前に束縛されています。子はprovider・model・effort・maxTokens・hooks・contextBudgetを省略すると親から継承します。たとえば親は次のtoolコードを実行できます。

```js
// Summarize the two documents in parallel
const results = await Promise.all([
  run_agent({ name: "english" }, "README.mdを3点に要約してください。"),
  run_agent({ name: "japanese" }, "README.ja.mdを3点に要約してください。"),
]);
console.log(JSON.stringify(results));
```

この省略形は`run_js`内の子向けです。通常のルートスクリプトでは3項目を明示します。子のsystem・name・maxSteps・cacheKey・履歴は継承されません。`run_js`ごとに新しいNOEプロセスが動き、JS変数は持ち越せないため、呼出し間のデータはファイルなどへ保存します。[agent-parallel.js](examples/agent-parallel.js)は親による委譲の例です。

### 次のタスクを待ち受ける

**第三引数**に`{keepAlive: true}`を渡すと、タスク終了後も次の仕事を待ちます。初期指示を省略すれば、モデルを呼ばずidleで起動します。

```js
import { spawn_agent } from "noe:agent";

const { agentId } = await spawn_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
  name: "worker",
}, undefined, { keepAlive: true });
console.log(agentId);
```

JS本文が終わってもNOEプロセスは待受を続けます。別terminalからIDを確認し、仕事を渡せます。

```sh
./noe agents
./noe queue <agent-id> 'README.mdを要約してください'
./noe steer <run-id> '回答は表形式にしてください'
./noe queue <agent-id> '続けてテストしてください'
./noe interrupt --run <run-id>
./noe interrupt --agent <agent-id>
```

`agentId`はinstanceの識別子、`runId`はタスクごとに発行する識別子です。queueは到着順に、新しい会話履歴で実行します。steerは現在の要求やJSを止めず、実行中タスクの履歴へ追加します。タスク単位のinterruptはkeepAlive instanceと他の予約を残し、instance単位のinterruptは全体を止めます。各操作は受付後に戻るため、完了は観察で確認します。

`run_agent(..., {keepAlive: true})`もidle時間を含め、instance停止まで戻りません。REPLでもkeepAlive instanceが残る入力は次のpromptへ進みません。instanceは現在のNOE実行または親runに所有され、所有者の終了・取消し・timeout・Ctrl+Cで回収されます。NOE終了後に独立して動き続ける仕組みではありません。[ライフサイクルと操作の詳細](docs/api.md)も参照してください。

### 進捗の観察とモデルの一覧

`--observe`はproviderが返すthinking・summary、途中の発言、`run_js`の先頭1行コメント、成功失敗、stderr、最終回答を表示します。連続したthinkingをまとめ、発言と回答は表も整形する色なしMarkdown表示にします。通常のスクリプト・toolのstdoutは表示から省きます。

```sh
./noe run --observe --events=agent.jsonl task.js
./noe observe --follow agent.jsonl
./noe run --events=- task.js | jq --unbuffered -c .
./noe repl --events=agent.jsonl
```

`--events`はrun・eval・repl共通です。`--events=-`はstdout、パス指定はファイルへ追記、単独指定は起動時のcwdへ`noe-events-日時-ID.jsonl`を作ります。通常JSの出力、生成JS本文、取得できた例外詳細も記録します。従来の`NOE_AGENT_TRACE=1`によるstderrへのメタデータtraceも使えます。[イベントガイド](docs/events.md)と[観察の入口](docs/observe.md)を参照してください。親は`observe_agent({logPath, runId, ...})`で保存ログも読めます。

`list_models({provider?, kind?})`は外部通信なしで内蔵・外部設定のモデル候補を返します。接続先・header・パラメータ・モデル別API形式・カタログは、`~/.config/noe/config.json`または`NOE_CONFIG_FILE`で上書きできます。[provider設定](docs/providers.md)、[モデルカタログ](docs/model-catalog.md)、[Jevでの選択例](examples/agent-route-model.js)を参照してください。JS内でカタログ未登録のモデルを直接指定することもできます。

REPLから単発で呼ぶ場合は次の形です。

```text
> const am = await import("noe:agent")
> const result = await am.run_agent({provider:"opencode-go", model:"deepseek-v4.1-flash", effort:"auto"}, "README.mdを要約してください")
> console.log(result.lastResult.answer)
```

**v1.2.0からの移行:** `createAgent()`・`agent.run()`は廃止しました。`run_agent`または`spawn_agent`をimportし、ルートのprovider・model・effortを明示して、回答は`lastResult.answer`から取得します。互換aliasはありません。[移行表](docs/api.md#旧apiからの移行)も参照してください。

## 構造化された意思決定

`import { decide } from "noe:decision"`から、`provider`・`model`・`state`・名前付き`questions`を明示して一回のDecisions要求を送れます。Promiseは応答JSON全体を返します。通常JSとagentの`run_js`から利用でき、既存の認証・取消しscopeを共用します。[意思決定APIガイド](docs/decision.md)と[実行例](examples/decision-classify.js)を参照してください。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/decision-classify.js 'Checkout failed.' typesafe/jev-1.14
```

## HTTPサーバー

小さなHTTPサーバーもJavaScriptで書けます。まず起動します。

```sh
./noe run examples/http-server.js 8080
```

別の端末から試します。

```sh
curl http://127.0.0.1:8080/health
curl -H 'Content-Type: application/json' \
  -d '{"message":"こんにちは"}' http://127.0.0.1:8080/echo
```

起動した端末のCtrl+Cで終了します。デモのポートは省略時8080、0なら空きポートで、起動時に実際のURLを表示します。

グローバルの`serve(options, handler)`はWebの`Request`を受け取り、`Response`を返すHTTPサーバーです。`new URL(request.url).pathname`で経路を判定し、JSON応答には`Response.json(value)`を使います。ハンドルにはURLと`close()`があり、詳しくは[APIリファレンス](docs/api.md#httpサーバー)を参照してください。

## CLI

| コマンド | 動作 |
| --- | --- |
| `noe run [options] <file> [args...]` | ファイルを実行。スクリプト引数は`process.argv.slice(2)`で取得 |
| `noe eval [options] <code>` | コードを実行。`process.argv.slice(2)`は空配列 |
| `noe repl [options]` | 対話的にJavaScriptを実行 |
| `noe observe --follow <log>` | イベントを人間向けに整形して表示 |
| `noe agents` | 稼働中agentとrun IDを一覧表示 |
| `noe steer <run-id> <instruction>` | 実行中タスクへ追加指示 |
| `noe queue <agent-id> <instruction>` | keepAlive instanceへ次のタスクを予約 |
| `noe interrupt --run <id>` / `--agent <id>` | タスク取消し・instance停止 |
| `noe --help` | 使い方を表示。引数なしでも表示 |
| `noe --version` | バージョンを表示 |

`.js`と`.mjs`をESモジュールとして実行し、Promise・`async/await`・top-level awaitを使えます。相対ローカルimportはimport元のファイルが基準で、拡張子を明示します。`--import-map path`でローカルESMに名前を付けられます。targetはmapファイル基準で、Worker・agent生成JSへ継承します。[import mapガイド](docs/import-map.md)を参照してください。ファイル操作と`eval`内の相対importはカレントディレクトリが基準です。

### 実行時間の上限

`run`・`eval`には任意の上限を指定できます。指定しなければ上限を設けません。

```sh
./noe run --timeout 5s examples/hello.js
./noe eval --timeout=500ms 'await new Promise(resolve => setTimeout(resolve, 60000))'
# 終了コード124、標準エラーへ時間切れの診断
```

`--timeout`はスクリプト名・コードの前に指定し、`500ms`・`5s`・`1m`などGoのduration形式の正の値を使います。空・不正・ゼロ・負・重複は使い方エラーです。`--`でオプションを終端できます。`process.argv`の先頭2項目は実行ファイル名とソース名で、その後にスクリプト引数が続きます。

エントリソースの読込み後、ランタイム初期化からJS評価・非同期I/O・sleep・サーバー待受・後続JSまでを計測します。`process.stdin.read()`などの同期ホストI/Oや、ローカルimportのOS読取りを強制停止する保証はありません。REPLはCtrl+Cで中断します。中断までの出力やファイルの変更は残ります。

## REPL

同じランタイムを使い続けるため、変数・関数は次の入力でも使えます。

```text
> let count = 1
> count += 1
2
> _ + 1
3
> await new Promise(resolve => setTimeout(resolve, 100))
> process.exit()
```

行編集、保存される履歴、Tab補完、複数行入力に対応します。`_`は最後に成功したundefined以外の結果です。通常の入力エラーの後も続行します。

空の入力でCtrl+D、または`process.exit()`で終了します。対応する端末でCtrl+Cを押すと入力や実行中の処理を中断し、通常は次のプロンプトへ戻ります。stdlibの通信・サーバー・Worker・メッセージポートやNOE拡張の子プロセス・I/Oが残った状態で中断やエラーが起きると、処理を停止してREPLセッションも終了します。タイマーだけなら取り消して続行できます。REPLで起動したサーバーは稼働中にプロンプトへ進まず、Ctrl+Cで停止します。

REPLの`await`は式、単一変数の初期化・代入に対応します。任意の複数文に含まれる`await`や静的importは提供せず、モジュールは`await import(...)`で読み込みます。操作と対応範囲は[REPLガイド](docs/repl.md)を参照してください。

## API

| 用途 | API |
| --- | --- |
| 自律タスク | `noe:agent`の`run_agent`・`spawn_agent`・`observe_agent`・`queue_agent` |
| 子プロセスの逐次I/O・停止 | `noe:process`の`spawn` |
| 標準I/Oのバイトストリーム | `noe:io`の`stdin`・`stdout`・`stderr`・取消し対応`pipe` |
| 構造化された意思決定 | `noe:decision`の`decide` |
| モデルカタログ | `noe:agent`の`list_models` |
| ChatGPT認証登録 | 認証ヘルパーから使う`noe:auth`の`save_chatgpt_credentials` |
| 出力 | `console.log`・`console.warn`・`console.error` |
| 引数・環境・終了 | `process.argv`・`process.env`・`process.cwd()`・`process.exit()` |
| 標準入力 | `process.stdin.read()`、逐次読取りは`noe:io`の`stdin` |
| テキスト・バイナリ | `fs/promises`の`readFile`・`writeFile` |
| ディレクトリ・ファイル情報・移動 | `fs/promises`の`readdir`・`stat`・`lstat`・`mkdir`・`rename`・`rm` |
| HTTP | グローバルの`fetch()`・`serve()` |
| 待機 | `timers/promises`の`setTimeout` |

引数、戻り値、エラー、対応範囲は[APIリファレンス](docs/api.md)にまとめています。

## 組み込みモジュールと利用権限

`fs`・`process`・`os`・`path`・`child_process`・`dns`・`http`・`worker_threads`・`crypto`などのgo-quickjs標準ライブラリをimportできます。timer、Web Streams、WebSocket、Workersも利用できます。NOE独自の`noe:agent`・`noe:decision`・`noe:auth`はmain runtimeで使え、Workerからのimportは未対応です。`noe:process`と`noe:io`は通常JS・agent生成JS・Workerで利用できます。使い方は[プロセス・標準I/Oガイド](docs/process-io.md)を参照してください。JavaScriptのグローバル`noe`はありません。NodeやDenoの完全互換ではなく、npm・パッケージ導入・QuickJS本来の`std`モジュールも提供しません。bare importは組込み名と、明示したimport mapの完全一致entryを解決します。ローカルのソースは相対・絶対pathとローカルfile URLに対応し、`.js`・`.mjs`を明示します。`--import-map path`のtargetはmapファイル基準で、対応表はWorker・agent生成JSへ継承されます。[import mapガイド](docs/import-map.md)と[利用例](examples/import-map/main.js)を参照してください。

スクリプトには、実行中プロセスの環境変数とローカルファイルシステム全体、コマンド実行、DNS・ネットワーク通信、HTTP待受、WebSocket、Workersの権限を渡します。NOEはsandboxではなく、スクリプトは実行ユーザーのOS権限で動作します。

通常終了では、開始したI/O・タイマーと後続のJS処理を待ちます。サーバーが稼働中なら実行を継続します。`process.exit(code)`で実行を停止し、終了コードを指定できます。

## Fetch API

グローバルの`fetch()`はgo-quickjsのWeb APIで、`Request`・`Response`・`Headers`とストリーミングbodyを提供します。レスポンスbodyは一度だけ読み取れます。JSONを送る場合は、文字列化とContent-Type指定を明示します。

HTTP 4xx・5xxもレスポンスとして返し、通信失敗はPromiseのrejectになります。HTTP/HTTPS URLからのモジュールimportには対応しません。HTTPサーバーはグローバルの`serve()`から、WebSocketは標準ライブラリから利用できます。

[HTTP APIの詳細](docs/api.md#fetch) · [JSON APIへのリクエスト例](examples/README.md#json-apiへのリクエスト)

## エラーと検証

| run/evalの終了コード | 意味 |
| --- | --- |
| `0` | 成功 |
| `1` | 実行失敗・未処理のPromise rejection・SIGINTによる中断 |
| `2` | CLIの使い方の誤り |
| `124` | `--timeout`による時間切れ |

`process.exit(code)`で終了コードを指定できます。エンジンが提供するエラー位置とスタックを表示します。解決する処理が残っていない未完了のtop-level awaitも診断します。

REPLは回復可能な入力エラーの後も続行し、EOFで0を返します。入力読取り・エンジン内部の続行できない失敗や、対話端末でstdlibのI/Oが残った状態での入力エラー・中断は1、非端末や入力待ちでの外部SIGINTは130です。

```sh
go test ./...
go test -tags=e2e ./... -count=1
```

[E2Eの構成と実行方法](tests/README.md)も参照してください。通常のE2EはCGOを無効にしてCLIをビルドし、別プロセスから実行します。一時ファイルとローカルHTTPサーバーを使い、LinuxではPTYによるREPLの操作も確認します。外部サービスには接続しません。実OpenRouter検証は別途`agent_live`タグと認証ファイルのパスを明示して実行します。[agent実装・検証記録](docs/increment-13-implementation.md)を参照してください。

設計、インクリメントごとの受入条件・検証記録、開発用コマンドは[開発記録](docs/README.md)を参照してください。

## ライセンス

NOEのライセンスは[MITライセンス](LICENSE)です。

依存する第三者のコード・データには、それぞれのライセンスが適用されます。
