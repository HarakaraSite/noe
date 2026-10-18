# NOE APIリファレンス

[READMEへ戻る](../README.md) · [REPLガイド](repl.md) · [実行例](../examples/README.md)

NOEはJavaScriptの実行環境として、固定したgo-quickjs v0.27.0の標準ライブラリを有効にします。標準ライブラリが持つWeb APIとNode形式APIを利用できます。各APIはgo-quickjsによる実装で、Node.js全体との互換性を保証するものではありません。

JavaScriptに`noe`グローバルや旧`noe.*` APIはありません。NOE固有の入口として、CLIの`run`・`eval`・`repl`・`--timeout`、REPLの履歴・補完・前回結果`_`、および組み込みES moduleの`noe:agent`・`noe:decision`・`noe:auth`があります。これらのagent・decision・authモジュールはWorkersでは利用できません。`noe:process`・`noe:io`はWorkerでも利用できるNOE拡張です。[旧APIからの移行](#旧apiからの移行)と[移行の実装記録](increment-12.md)も参照してください。

## 利用できる機能と権限

Web APIにはURL、Fetch、Request/Response/Headers、Web Streams、タイマー、WebSocket、Worker、cryptoなどがあります。Node形式の組み込みモジュールには、たとえば`assert`、`buffer`、`child_process`、`dns`、`events`、`fs`、`fs/promises`、`http`、`os`、`path`、`process`、`timers`、`timers/promises`、`url`、`util`、`vm`、`worker_threads`、`zlib`があります。暗号機能はWeb Cryptoと`crypto`から利用できます。APIごとの未実装機能や差異は、go-quickjsの実装に従います。

NOEは次のホスト機能をスクリプトに渡します。

- ローカルのファイルシステム全体の読み書き
- 起動時のプロセス環境変数、標準入力・標準出力・標準エラー、プロセス終了
- 実OSの情報
- DNS問い合わせと任意の宛先へのHTTP・WebSocket通信
- 任意のコマンド実行
- HTTPサーバーの待受
- Workersとメッセージング

Workersにも同じgo-quickjs標準ライブラリのホスト機能を渡します（`noe:agent`を除く）。NOEはsandboxではありません。スクリプトは起動した利用者と同じOS権限で動作します。

go-quickjs v0.27.0のWorker数は既定で同時64個ですが、NOEは`Workers.Max: -1`を指定し、従来どおりWorker数の上限を設けません。これはJavaScriptのWorkerの設定で、agentの委譲数とは別です。

npm、パッケージ導入、HTTP/HTTPS URLからのモジュールimport、暗黙のbare importによる外部パッケージ探索はありません。裸の組み込みモジュール名と対応する`node:`名で標準ライブラリをimportできます。`run/eval/repl --import-map path`で完全一致のbare名をローカルESMに対応付けられ、Worker・agent生成JSへ同じ対応表を継承します。ローカルモジュールは拡張子付きの相対/絶対`.js`・`.mjs`パスとlocal file URLを使えます。詳細は[import mapガイド](import-map.md)を参照してください。相対importはimport元のファイルを基準に解決し、`eval`内ではカレントディレクトリが基準です。QuickJS本来の`std`モジュールは提供しません。

## 自律agent（OpenRouter）

OpenAIのSol・LunaはResponsesを使います。[OpenAIガイド](openai.md)にChatGPTサインイン・APIキー・認証ヘルパーの手順があります。

`list_models({provider?, kind?})`は、認証や外部API要求なしで内蔵デフォルトと外部上書きを統合した候補一覧を返します。詳細とJevでの選択例は[モデルカタログ](model-catalog.md)を参照してください。

`noe:agent`は`run_agent`・`spawn_agent`・`observe_agent`・`queue_agent`・`steer_agent`・`interrupt_agent`・`list_models`を公開します。互換aliasはなく、`createAgent()`と`agent.run()`は利用できません。通常のJavaScriptからは必要な関数をimportします。ルートagentでは`provider`・`model`・`effort`を明示します。組込みproviderは`openrouter`・`opencode-go`・`openai-chatgpt`・`openai-api`で、外部設定で互換providerを追加できます。接続先・パラメータ・モデル別のAPI形式は[provider設定](providers.md)を参照してください。

```js
import { run_agent } from "noe:agent";

const snapshot = await run_agent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "summary",
  system: "日本語で簡潔に回答してください。",
  maxTokens: 1024,
  maxSteps: 6,
  cacheKey: "noe-summary-v1",
}, "README.mdを読んで要点を3点にまとめてください。");

if (snapshot.lastResult?.state === "completed") {
  console.log(snapshot.lastResult.answer);
}
```

`run_agent(config, instruction?, options?)`はagent instanceが停止し、後始末を終えてからsnapshotを返します。起動APIに`wait` optionはありません。既定の`keepAlive:false`では1タスクで停止します。通常のタスク失敗はrejectではなく、`state:"stopped"`・`reason:"task_failed"`と`lastResult.state:"failed"`で返ります。入力や起動受付に失敗した場合はPromiseがrejectします。監督基盤の致命的な失敗は`state:"failed"`と理由をsnapshotへ記録します。

`spawn_agent(config, instruction?, options?)`は起動と初期タスクの受領後に`{agentId}`を返します。最初のモデル応答やタスク完了は待ちません。`observe_agent({agentId})`はlive snapshotを直ちに返します。待受instanceへ追加の仕事を渡す場合は、`keepAlive:true`で起動して`queue_agent`を呼びます。

```js
import { spawn_agent, observe_agent, queue_agent } from "noe:agent";

const worker = await spawn_agent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "worker",
}, "README.mdの要点をまとめてください。", { keepAlive: true });

const queued = await queue_agent({
  agentId: worker.agentId,
  instruction: "README.ja.mdも読み、違いを簡潔にまとめてください。",
});

let snapshot;
while (true) {
  snapshot = await observe_agent({ agentId: worker.agentId });
  if (snapshot.lastResult?.commandId === queued.commandId && snapshot.state === "idle") break;
  if (snapshot.state === "stopped" || snapshot.state === "failed") break;
  await new Promise(resolve => setTimeout(resolve, 100));
}

console.log(snapshot.lastResult);
```

`queue_agent({agentId, instruction, commandId?})`は受領後に`{commandId, status}`を返します。タスクはFIFOで処理され、各タスクは新しい履歴とrun IDを持ちます。`starting`・`running`・`idle`の`keepAlive` instanceで受け付けます。終了済みinstanceや`keepAlive:false`のinstanceは受け付けません。同じ`commandId`と同じ指示の再送は再実行せず現在statusを返し、異なる指示での再利用はエラーになります。既定では`commandId`が自動発行されます。

### 設定

各task run開始時に、共通設定とworkspaceの`instructions/*.md`をファイル名順で読み込みます。
組込み指示 → 共通 → workspace → 明示した`system`の順で追記し、run中は固定します。
探索場所・読込み失敗・子やqueueの扱いは[インストラクションガイド](instructions.md)を参照してください。

`hooks`・`contextBudget`・`executionLimits`の契約、4種のhandlerとhook用操作API、
context snapshotは[hook・実行groupガイド](hooks.md)を参照してください。
`hooks:[]`と`contextBudget:null`は子の継承を解除します。
`contextBudget.inputTokens`は正のsafe integerの入力目安で、超過だけで停止しません。
`executionLimits`はrootのみ指定でき、省略項目は無制限、0は受付枠0です。
runのsnapshotに`groupId`・`execution`・`context`が加わります。

`config`は`{provider, model, effort, name?, system?, maxTokens?, maxSteps?, cacheKey?, hooks?, contextBudget?, executionLimits?}`です。rootで`provider`・`model`・`effort`を省略すると入力エラーです。子agentではprovider・model・effort・maxTokens・hooks・contextBudgetを省略時に起動元から継承します。その他の項目は自動継承しません。executionLimitsはrootのみ指定でき、子は同じgroupの残量を共有します。`provider`には組込みまたは外部設定で定義したIDを指定します。`model`と`name`は空でない文字列、`system`と`cacheKey`は文字列、`maxTokens`と`maxSteps`は0以上の整数です。`name`の既定値は`agent`、`system`は空文字、`maxTokens`と`maxSteps`は0です。`system`に渡した文字列は各タスクの実行中に固定されます。`instruction`は空でない文字列で、`keepAlive:true`なら省略できます。`options`は独立した第三引数で、受け取る項目は`keepAlive`のみです。`keepAlive`を`config`へ入れないでください。instructionを省略してoptionsを渡す場合は、第二引数に`undefined`を置きます。たとえば`spawn_agent({name:"worker"}, undefined, {keepAlive:true})`はidleで起動し、queueにtaskが届くまでモデルを呼びません。

`effort`で指定できる値は`"auto"`・`"none"`・`"minimal"`・`"low"`・`"medium"`・`"high"`・`"xhigh"`・`"max"`です。明示的な`"auto"`は推論設定を送らず、provider/modelの既定動作に任せます。他の値はprovider設定の`effortField`（OpenRouterは`reasoning.effort`、OpenCode GoのChatは`reasoning_effort`）へ送り、各タスクで固定します。実際に利用できるエフォートと効果はmodel/providerに従います。NOEはモデル別の対応表による事前拒否や値の自動変更を行わず、APIからエラーが返ればprovider errorとして扱います。[OpenRouterのreasoning仕様](https://openrouter.ai/docs/guides/best-practices/reasoning-tokens)を参照してください。

`maxSteps`はタスクごとのモデル要求回数を制限します。0はNOE側の上限なしです。`maxTokens`はモデル要求ごとの出力上限で、会話全体の累計ではありません。0の場合、NOEはOpenRouterへ`max_tokens`を送りません。どちらの上限も指定しない場合、NOEは独自の上限を追加しません。

`cacheKey`はOpenRouterの`session_id`として送られ、routingやprompt cacheで使われる値です。未指定（`undefined`を含む）の場合は各task runの`runId`を使い、同じtask内の全モデル要求へ同じIDを送ります。明示した`cacheKey`は各taskで再利用されます。`cacheKey: ""`は`session_id`を送らない指定です。キャッシュヒットは保証せず、会話履歴を保存・再開する機能でもありません。[OpenRouter session_id仕様](https://openrouter.ai/docs/guides/best-practices/prompt-caching#using-session_id-for-sticky-sessions)を参照してください。

### 戻り値・状態・寿命

run snapshotは`{agentId, parentRunId, name, state, reason, keepAlive, currentRunId, queueLength, lastResult}`です。instance stateは`starting`・`idle`・`running`・`stopping`・`stopped`・`failed`です。起動直後やidleでは`currentRunId`が空です。`lastResult`は最後のtaskの`{runId, commandId, state, answer, error?}`で、まだtaskを実行していなければ`null`です。

`keepAlive`の既定値は`false`です。`false`なら一つのtaskの後に停止し、instructionは必須です。`true`ならtask完了・失敗後も待受を続け、通常のtask失敗後も次のqueue taskを受け付けます。instructionを省略するとidleから開始します。idle中にモデルは呼び出しません。`run_agent(config, instruction, {keepAlive:true})`はinstance停止まで戻りません。

rootのrun/evalではJS本文が終わってもnative追跡によって待受が続き、現在の終了方法はCtrl+C・timeoutなどです。REPLでもkeepAlive instanceが残る入力は次のpromptへ進みません。親run_jsから起動した子はtool本文の終了後も親runに所有されます。

待受instanceは、その所有者のNOE runに結び付いています。親runの終了・取消し、timeout、Ctrl+Cなどで回収され、NOE終了後も独立して続くdaemonにはなりません。

### 実行中taskへの指示と停止

`steer_agent({runId, instruction, commandId?})`は実行中taskへ追加指示を受領します。同じrunの履歴へ次のモデル判断前にuser messageとして加わります。進行中のモデル要求や`run_js`を中断・書換えしません。runが終了していれば受け付けません。

```js
import { steer_agent } from "noe:agent";

async function addInstructionToActiveTask(runId) {
  const receipt = await steer_agent({
    runId,
    instruction: "結果を箇条書きにまとめてください。",
  });
  console.log(receipt.commandId);
}
```

steerの受領は適用完了を意味しません。`commandId`を省略すると自動発行し、返されたIDでJSONLの`command.accepted`・`command.applied`・`command.cancelled`を追跡します。イベントの`operation`は`steer`です。同じrunで同じ`commandId`・指示を再送すると重複適用せず現在の状態を返し、異なる指示でIDを再利用するとエラーです。モデルが最終回答候補を確定する直前にも受領済みsteerを確認します。確定より先に受け付けた指示は最終回答の前に適用し、確定後の指示は終了済みとして拒否します。別runとして後の仕事を始める`queue_agent`とは使い分けてください。

`interrupt_agent`は、指定した対象への停止要求を受領すると戻ります。後始末完了を待つAPIではありません。

```js
import { interrupt_agent } from "noe:agent";

async function stopTask(runId) {
  await interrupt_agent({ runId });
}
async function stopInstance(agentId) {
  await interrupt_agent({ agentId });
}
```

`runId`と`agentId`のどちらか一方を指定します。`{runId}`はtask・生成JS・そのtaskが所有する子workを取消します。`keepAlive:true` instanceと他の予約は残り、次の予約またはidle状態へ進みます。`{agentId}`はidle中でも受け付け、instanceを停止して実行中task・未実行queue・子instanceを回収します。idle時は最後のtask結果を書き換えません。JSONLには`interrupt.accepted`が記録され、`target`は`run`または`agent`です。停止要求の受付後、`observe_agent({agentId})`でstateと最終結果を確認してください。単発instanceのtaskだけをinterruptした場合、snapshotの`reason`は`task_interrupted`になります。明示的なinstance interrupt後はsnapshotの`reason`が`interrupted`になり、`run_agent`は回収後に終端snapshotを返します。

JavaScriptからのsteer・interruptは呼出元が所有するscope内で認可されます。所有していないrunやagentのIDを指定すると受付エラーになります。別terminalのCLIはローカルhost用のcontrol接続から操作し、JSへ渡す子scope用tokenを使いません。

### 別ターミナルからの操作

人間は同じユーザー環境の別ターミナルから稼働中のNOEへ接続できます。

```sh
noe agents
noe steer <run-id> '結果を表形式にしてください'
noe queue <agent-id> '続けてテストしてください'
noe interrupt --run <run-id>
noe interrupt --agent <agent-id>
```

`noe agents`は到達可能なhostのactive instanceとagent ID・current run ID・state・queue件数・nameを表示します。終了済みinstanceは一覧に含めません。各操作は受付結果を返し、task完了や回収を待ちません。`steer`には実行中のrun ID、`queue`とinstance停止にはagent IDを使います。IDは完全なIDか、候補が一つに絞れるprefixを使えます。要求後の進行確認には`noe observe`・`--observe`・JSONLを使ってください。

Control接続はローカルで稼働中のNOEに限られ、daemonや再起動後のagent管理には使えません。接続先を共有する必要がある場合は、agentを実行するNOEとCLIで同じ`NOE_AGENT_RUNTIME_DIR`または`XDG_RUNTIME_DIR`を使ってください。新規作成するruntime directoryはmode `0700`、既存directoryのmodeは変更しません。接続情報fileとsecret fileは常にmode `0600`で保護します。`NOE_AGENT_RUNTIME_DIR`に既存directoryを指定してもchmodは行いません。接続探索はcontrol要求に限り2秒で打ち切り、モデル要求のtimeoutは変更しません。

### run_jsからの利用

モデル向けの`run_js`では6つのagent関数が事前に束縛されています。importや再宣言をせず、そのまま呼び出してください。`run_js`呼出しごとに新しいNOEプロセスが動き、通常のJavaScript変数は呼出し間で保持されません。子はprovider・model・effort・maxTokensを省略時に親runから継承し、name・system・maxSteps・cacheKey・履歴は継承しません。

`observe_agent`はlive状態と保存済みJSONLの二つのtargetを持ちます。live用は`observe_agent({agentId})`のみです。ファイル観察は既存の`observe_agent({logPath, runId, cursor?, wait?, includeThinking?, includeOutput?})`を維持します。両方のtargetを同じ呼出しへ混在させられません。ファイル観察の戻り値・cursorは[観察ガイド](observe.md)を参照してください。

### 認証と設定

OpenRouter APIキーをテキストファイルへ保存し、`NOE_OPENROUTER_API_KEY_FILE`でパスを指定します。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key ./noe run examples/agent-summary.js README.md
```

この変数が空または未設定なら、`$XDG_CONFIG_HOME/noe/credentials/openrouter-api-key`を探します。`XDG_CONFIG_HOME`が未設定なら`~/.config/noe/credentials/openrouter-api-key`を使います。ファイル内容の前後の空白は取り除きます。Go側が最初のモデル要求時にファイルを読みます。moduleのimportや初期taskなしのidle起動では読みません。task付きのrun/spawnは受領後にモデル要求を開始します。キー値はモデル履歴・子接続情報・traceへ含めません。NOEはsandboxではなく、実行するJSは同一ユーザーのOS権限でファイルや環境変数へアクセスできます。

`NOE_OPENROUTER_BASE_URL`はOpenRouter APIのbase URLを上書きし、既定値は`https://openrouter.ai/api/v1`です。`NOE_AGENT_TRACE=1`を指定すると、実行メタデータをJSON Linesで標準エラーへ出力します。

```sh
NOE_AGENT_TRACE=1 NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent-summary.js README.md 2>agent-trace.log
```

traceにはrun ID、親run ID、agent名、model、status、duration（nanoseconds）、返された場合はprompt/completion/total/cached token数とcostを記録します。`js_result`にはtoolの終了コードと理由も含みます。prompt、生成code、toolのstdout/stderr、APIキーは記録しません。標準エラーには通常の診断も含まれる場合があります。

通常の`run`・`eval`では`--timeout`が評価全体を制限できます。省略時はCLI独自の期限はありません。REPLでは通信中のCtrl+Cで要求を取り消し、稼働中のI/Oを残さないようREPL sessionを終了します。詳しくは[REPLガイド](repl.md)を参照してください。

WindowsでTEMPとカレントディレクトリが別volumeの場合、生成ソースをカレントディレクトリ上の一時場所へ書き込み、相対importの基準を保ちます。その場合はカレントディレクトリへ一時ファイルを書き込める必要があります。書込みできない場合は、カレントディレクトリと同じvolumeの書込み可能な場所を`TEMP`に指定してください。

## 子プロセスと標準I/Oストリーム

`noe:process`の`spawn`で子の入出力を実行中から扱い、終了・停止・回収を行えます。`noe:io`はホストのstdin/stdout/stderrのWeb Streamsと取消し対応`pipe`を公開します。通常JS・run_js・Workerで利用でき、REPLでは子の操作とcloseまでを同一入力で行います。公開契約と例は[プロセス・標準I/Oガイド](process-io.md)を参照してください。stdlibの`execFile`・`process.std*`・`pipeTo`は変更しません。

## 意思決定モデル：`noe:decision`

`import { decide } from "noe:decision"`で、`await decide({provider:"openrouter",model,state,questions})`を呼べます。`state`は文字列・JSON object/array、`questions`は名前付き質問objectです。provider/modelは毎回明示し、結果はproviderの応答JSON全体です。通常JSとagentの`run_js`から利用でき、agent loopは作りません。認証は既存OpenRouter設定を共用し、要求は現在の評価・所有scopeの取消しに従います。使用例、検証・エラー、画像part、REPL、観察は[意思決定APIガイド](decision.md)を参照してください。

## 構造化イベントの出力

`run`・`eval`・`repl`で`--observe`を付けると、stdoutへ人間向けの整形表示を直接出せます。`--events=path`との併用でJSONL保存も行えます。`--events=-`は機械処理用のstdout JSONLとして維持し、同じstdoutを使う`--observe`とは同時指定しません。

観察には人間用CLIの`noe observe`と、親用JS APIの`observe_agent`を利用できます。`observe_agent({agentId})`はlive snapshotをすぐ返します。`observe_agent({logPath, runId, cursor?, wait?, includeThinking?, includeOutput?})`は既存JSONLから状態・差分・確定済みの結果を返し、既定は非待機、`wait: true`ならrunの終端まで待ちます。使い方と戻り値は[観察の入口](observe.md)を参照してください。

`run`・`eval`・`repl`共通の`--events`で通常出力とagentの詳細イベントをJSONLへ記録できます。`--events=-`はstdout、`--events=path`はファイルへの追記、`--events`単独は起動時のcwdに`noe-events-日時-ID.jsonl`を新規作成します。通常出力、thinking・summary、途中の発言、最終回答、`run_js`の先頭コメントと成功・失敗をrun ID付きで読めます。オプションはソースより前へ置きます。形式、REPLの扱い、逐次出力の範囲は[観察ガイド](events.md)を参照してください。

## 実行環境とファイル

`process.argv`は実行ファイル名、スクリプト名、スクリプト引数の順です。スクリプト引数は`process.argv.slice(2)`で取得できます。`process.env`は起動時の環境変数、`process.cwd()`はカレントディレクトリです。`process.exit(code)`でプロセスを終了し、終了コードを指定できます。

標準入力全体は`process.stdin.read()`で読み取れます。この読み取りは同期処理で、最初の呼出しでEOFまで読み込み、2回目以降は`null`を返します。空入力でも`null`です。REPLの標準入力はソース入力用なので、`process.stdin.read()`にはデータを渡さず`null`になります。この同期読取りを`--timeout`で強制停止する保証はありません。標準入力を待つスクリプトでは、パイプ入力や端末からのEOFを使って入力を終えてください。

ファイルのPromise APIは`fs/promises`からimportできます。

| API | 動作 |
| --- | --- |
| `readFile(path, "utf8")` | UTF-8テキストを文字列として読む |
| `readFile(path)` | 全バイトをUint8Arrayとして読む |
| `writeFile(path, data)` | 文字列またはバイト列を書き込む |
| `readdir(path)` | 直下の名前を文字列の配列で返す |
| `stat(path)`・`lstat(path)` | `size`、`mode`、`mtimeMs`、`mtime`と、`isFile()`・`isDirectory()`などを返す。`stat`はリンク先、`lstat`はリンク自身を調べる |
| `mkdir(path, options)` | `{ recursive: true }`などの標準オプションを受け付ける |
| `rename(from, to)` | 名前を変更または移動する |
| `rm(path, options)` | `{ recursive: true, force: true }`などの標準オプションを受け付ける |

`readdir`の結果は名前だけで、Direntオブジェクトではありません。`stat`はシンボリックリンクの参照先、`lstat`はリンク自身を調べます。パスの解決はファイル操作時のカレントディレクトリが基準です。

```js
import { readFile, writeFile } from "fs/promises";

const inputPath = process.argv[2];
const outputPath = process.argv[3];
const value = JSON.parse(await readFile(inputPath, "utf8"));
value.processed = true;
await writeFile(outputPath, JSON.stringify(value, null, 2) + "\n");
```

## 非同期処理

Promise、`async/await`、top-level awaitを利用できます。タイマーPromiseは次のように使います。

```js
import { setTimeout as sleep } from "timers/promises";

await sleep(1000);
console.log("done");
```

通常の完了では、開始した非同期処理と後続のJavaScriptを待ちます。解決する処理が残っていない未完了のtop-level awaitは、`run`・`eval`で実行失敗になります。実行期限は`--timeout`で設定できます。

## Fetch

グローバルの`fetch()`はWeb Fetch APIです。`Request`・`Response`・`Headers`を使い、レスポンス本文は`text()`、`json()`、`arrayBuffer()`、`bytes()`、またはReadableStreamで読み取れます。レスポンス本文は一度だけ読み取れます。HTTP 4xx・5xxもResponseとして返るため、必要に応じて`response.ok`を確認してください。

```js
const response = await fetch("https://example.com/api/data");
if (!response.ok) throw new Error(`HTTP ${response.status}`);
const value = await response.json();
console.log(value);
```

JSONを送信する例です。

```js
const response = await fetch("https://example.com/api/items", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ value: 42 }),
});
console.log(response.status, await response.text());
```

## HTTPサーバー

グローバルの`serve(options, handler)`はWeb Requestを受け取り、Web Responseを返すHTTPサーバーを起動します。`options`では`hostname`と`port`を指定できます。ポートの既定値は0で、OSが空きポートを選びます。返されるサーバーハンドルには`url`、`port`、`hostname`、`close()`があります。サーバーが稼働中は実行を継続します。

```js
const server = serve({ port: 8080 }, async request => {
  const pathname = new URL(request.url).pathname;
  if (pathname === "/echo" && request.method === "POST") {
    return Response.json(await request.json());
  }
  return new Response("Not Found", { status: 404 });
});
console.log(server.url);
```

リクエスト本文はWeb Request APIで読み、応答は必ずResponseで返します。JSONの応答には`Response.json(value)`を使えます。HTTPモジュールから`serve`をimportすることもできます。WebSocketはWebSocket APIと、サーバー側では`serve`を通じて利用できます。

`server.close()`は同期的に受付を停止し、go-quickjs v0.25.3では処理中のHTTPリクエスト終了を最大5秒待ちます。停止を求めるリクエスト自身のハンドラーから同期的に呼ぶと、そのハンドラーの完了を待つことになります。応答が完了した後の別の処理から呼んでください。これは旧`noe.serve()`の非同期停止とは異なる動作です。

## REPLでの非同期処理

REPLでは変数や関数を入力間で保持します。タイマーだけが残った入力をCtrl+Cで中断した場合はタイマーを取り消して続行できます。Fetch、サーバー、Worker、メッセージポートなど、ランタイム全体に結び付いた標準ライブラリ処理が残った状態で中断または入力エラーが起きると、ランタイムを閉じてREPLセッションを終了します。入力単位の停止後に既存の関数やimportを安全に再利用できないためです。詳しくは[REPLガイド](repl.md)を参照してください。

## 旧APIからの移行

以前のNOE固有APIは提供しません。`createAgent()`・`agent.run()`にも互換aliasはありません。旧agent APIでは`maxSteps`と`cacheKey`を`run()`側へ渡していましたが、新APIでは`config`へまとめます。`run_agent()`は文字列ではなくsnapshotを返すため、回答は`lastResult.answer`から取得してください。

```js
// 以前
const agent = createAgent({ provider, model, effort, maxTokens });
const previousAnswer = await agent.run(instruction, { maxSteps, cacheKey });

// 現在
import { run_agent } from "noe:agent";
const snapshot = await run_agent({
  provider, model, effort, maxTokens, maxSteps, cacheKey,
}, instruction);
const answer = snapshot.lastResult?.answer;
```

その他の旧NOE固有APIは提供しません。既存スクリプトは次の標準APIへ移行してください。

| 以前のAPI | 移行先 |
| --- | --- |
| `noe.args` | `process.argv.slice(2)` |
| `noe.env.get(name)` | `process.env[name]` |
| `noe.cwd()` | `process.cwd()` |
| `noe.exit(code)` | `process.exit(code)` |
| `noe.readStdin()` | `process.stdin.read()` |
| `noe.readTextFile(path)` | `fs/promises`の`readFile(path, "utf8")` |
| `noe.writeTextFile(path, text)` | `fs/promises`の`writeFile(path, text)` |
| `noe.readFile(path)`・`noe.writeFile(path, data)` | `fs/promises`の`readFile`・`writeFile` |
| `noe.readDir(path)` | `readdir(path)`で名前を取得し、`lstat()`で各項目を判定 |
| `noe.stat(path)` | `stat(path)` |
| `noe.mkdir`・`noe.rename`・`noe.remove` | `fs/promises`の`mkdir`・`rename`・`rm` |
| `noe.sleep(ms)` | `timers/promises`の`setTimeout(ms)` |
| `noe.fetch(url, options)` | グローバルの`fetch(url, options)` |
| `noe.serve(options, handler)` | グローバルの`serve(options, handler)` |

標準APIでは、FetchのResponse本文は一度だけ読み取れます。`process.stdin.read()`はPromiseではなく、入力全体を同期的に一度だけ読みます。`readdir`は名前の配列を返し、`stat`と`lstat`は標準ライブラリの情報オブジェクトを返します。`readdir`で得た各項目の種別を知るには、名前をpathに連結して`lstat`または`stat`を呼びます。オプションや戻り値を標準APIに合わせて更新してください。
