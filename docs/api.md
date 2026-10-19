# NOE APIリファレンス

[READMEへ戻る](../README.md) · [agent起動ガイド](agents.md) · [実行例](../examples/README.md)

バイナリと同じ版の英文API契約は`noe docs [topic [member]]`で参照できます。JS・Worker・agentの`run_js`でも`noe:docs`から同じ本文を取得します（[内蔵docsの使い方](reference.md)）。このガイドはCLI・設定・APIの利用手順を説明します。

NOEは固定したgo-quickjs v0.27.0をJavaScriptエンジンに使い、同版のstdlibを原型とするNOE管理の標準ライブラリを有効にします。Web APIとNode形式APIを利用できます。各APIはNOEが管理する実装に従い、Node.js全体との互換性を保証するものではありません。

JavaScriptに`noe`グローバルや旧`noe.*` APIはありません。NOE固有の入口として、CLIの`run`・`eval`・`docs`・`--timeout`と、組み込みES moduleの`noe:agent`・`noe:decision`・`noe:image`・`noe:auth`があります。これらのagent・decision・image・authモジュールはWorkersでは利用できません。`noe:process`・`noe:io`・`noe:fs-handle`・`noe:sockets`・`noe:signals`・`noe:http`・`noe:docs`はWorkerでも利用できるNOE拡張です。[旧APIからの移行](#旧apiからの移行)と[移行の実装記録](archive/increment-12.md)も参照してください。

## 利用できる機能と権限

Web APIにはURL、Fetch、Request/Response/Headers、Web Streams、タイマー、WebSocket、Worker、cryptoなどがあります。Node形式の組み込みモジュールには、たとえば`assert`、`buffer`、`child_process`、`dns`、`events`、`fs`、`fs/promises`、`http`、`os`、`path`、`process`、`timers`、`timers/promises`、`url`、`util`、`vm`、`worker_threads`、`zlib`があります。暗号機能はWeb Cryptoと`crypto`から利用できます。APIごとの未実装機能や差異は、NOE管理の`internal/stdlib`と、そのバイナリに内蔵したdocsを参照してください。

NOEは次のホスト機能をスクリプトに渡します。

- ローカルのファイルシステム全体の読み書き
- 起動時のプロセス環境変数、標準入力・標準出力・標準エラー、プロセス終了
- 実OSの情報
- DNS問い合わせと任意の宛先へのHTTP・WebSocket通信
- 任意のコマンド実行
- HTTPサーバーの待受
- Workersとメッセージング

Workersにも同じNOE管理の標準ライブラリのホスト機能を渡します。NOE固有moduleでは`noe:process`・`noe:io`・`noe:fs-handle`・`noe:sockets`・`noe:signals`・`noe:http`・`noe:docs`をWorkerで利用でき、`noe:agent`・`noe:decision`・`noe:image`・`noe:auth`は利用できません。NOEはsandboxではありません。スクリプトは起動した利用者と同じOS権限で動作します。

原型であるgo-quickjs v0.27.0のWorker数は既定で同時64個ですが、NOEは`Workers.Max: -1`を指定し、従来どおりWorker数の上限を設けません。これはJavaScriptのWorkerの設定で、agentの委譲数とは別です。

npm、パッケージ導入、HTTP/HTTPS URLからのモジュールimport、暗黙のbare importによる外部パッケージ探索はありません。裸の組み込みモジュール名と対応する`node:`名で標準ライブラリをimportできます。`run/eval --import-map path`で完全一致のbare名をローカルESMに対応付けられ、Worker・agent生成JSへ同じ対応表を継承します。ローカルモジュールは拡張子付きの相対/絶対`.js`・`.mjs`パスとlocal file URLを使えます。詳細は[import mapガイド](import-map.md)を参照してください。相対importはimport元のファイルを基準に解決し、`eval`内ではカレントディレクトリが基準です。QuickJS本来の`std`モジュールは提供しません。

## CLI

| コマンド | 動作 |
| --- | --- |
| `noe run [options] <file> [args...]` | ファイルを実行。引数は`process.argv.slice(2)`で取得 |
| `noe eval [options] <code>` | コードを実行。`process.argv.slice(2)`は空配列 |
| `noe observe --follow <log>` | 保存中のagentイベントを人間向けに表示 |
| `noe agents` | 実行中のagentとcurrent run IDを一覧表示 |
| `noe steer <run-id> <instruction>` | 実行中taskへ追加指示 |
| `noe queue <agent-id> <instruction>` | keepAlive instanceへ次のtaskを予約 |
| `noe interrupt --run <id>` / `--agent <id>` | taskを取消し、またはinstanceを停止 |
| `noe --help` | usageを表示。引数なしでも同じ |
| `noe --version` | versionを表示 |

`run`・`eval`の`--import-map path`、`--observe`、`--events[=path|-]`はソースより前に指定します。`--`でオプション解析を終えます。`.js`・`.mjs`はES modulesとして、Promise・`async/await`・top-level awaitを利用できます。import解決は[import map](import-map.md)、ログの出力先は[イベントガイド](events.md)を参照してください。

### 実行時間の上限

`run`・`eval`は省略時にCLI独自の期限を設けません。必要ならソースより前に`--timeout`を指定します。

```sh
./noe run --timeout 5s examples/basics/hello.js
./noe eval --timeout=500ms 'await new Promise(resolve => setTimeout(resolve, 60000))'
# 終了コード124、stderrへ時間切れの診断
```

`500ms`・`5s`・`1m`など正のGo durationを指定します。空・不正・0・負数・重複指定はusage errorです。時間の計測はentry source読込み後から始まり、runtime初期化、JS評価、非同期I/O、待機、サーバー待受と後続処理を含みます。同期host I/O（`process.stdin.read()`など）やlocal import中のOS読込みの強制停止は保証しません。中断前の出力やファイル変更は残ります。

### 終了コードと診断

| run/evalの終了コード | 意味 |
| --- | --- |
| `0` | 成功 |
| `1` | 実行失敗、未処理のPromise rejection、SIGINTによる中断 |
| `2` | CLI引数の誤り |
| `124` | 実行時間切れ |

`process.exit(code)`で指定したコードでも終了できます。エラーはエンジンが提供する位置とstackを表示します。解決する処理が残っていない未完了のtop-level awaitも実行失敗として診断します。

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

`queue_agent({agentId, instruction, commandId?})`は受領後に`{commandId, status}`を返します。タスクはFIFOで処理され、新しいrun IDを持ちますが、同じkeepAlive instanceに確定済みの会話を引き継ぎます。過去のuser・assistant・tool往復と最終回答のうち、context trimming後も残っている内容が続きます。実行開始時に新しい指示を時系列どおりuser messageとして追加し、元のtask指示・適用済みsteerと同様に以後の切り捨てから保護します。既に切り捨てた履歴は戻りません。trim回数と直近のtrim情報もinstance内で続きます。`starting`・`running`・`idle`の`keepAlive` instanceで受け付けます。終了済みinstanceや`keepAlive:false`のinstanceは受け付けません。同じ`commandId`と同じ指示の再送は再実行せず現在statusを返し、異なる指示での再利用はエラーになります。既定では`commandId`が自動発行されます。

### 設定

各instanceの最初の実行開始時に、共通設定とworkspaceの`instructions/*.md`をファイル名順で読み込みます。
組込み指示 → 共通 → workspace → 明示した`system`の順で追記し、同じinstanceのqueue taskにも固定します。子など新しいinstanceは独立して読み込みます。
探索場所・読込み失敗・子やqueueの扱いは[インストラクションガイド](instructions.md)を参照してください。

`hooks`・`contextBudget`・`contextPolicy`・`executionLimits`の契約、4種のhandlerとhook用操作API、
context snapshotは[hook・実行groupガイド](hooks.md)を参照してください。
`hooks:[]`と`contextBudget:null`は子の継承を解除します。`contextPolicy:null`は自動切り捨てを無効化し、子にも継承されます。
`contextBudget.inputTokens`は固定入力込みの総入力目安（正のsafe integer）で、超過だけで停止しません。自動policyが有効なら古い完了blockを切り捨てる発動目安にも使います。`contextPolicy`は省略または`{}`で有効、`retainRatio`は0より大きく1未満（既定0.1）、任意の`responseReserveTokens`は正のsafe integerです。window不明・明示予算なしでは総入力目安を100万tokensとし、モデル容量とは区別します。
`executionLimits`はrootのみ指定でき、省略項目は無制限、0は受付枠0です。
runのsnapshotに`groupId`・`execution`・`context`が加わります。

`config`は`{provider, model, effort, name?, system?, maxTokens?, maxSteps?, cacheKey?, hooks?, contextBudget?, contextPolicy?, executionLimits?}`です。rootで`provider`・`model`・`effort`を省略すると入力エラーです。子agentではprovider・model・effort・maxTokens・hooks・contextBudget・contextPolicyを省略時に起動元から継承します。その他の項目は自動継承しません。executionLimitsはrootのみ指定でき、子は同じgroupの残量を共有します。`provider`には組込みまたは外部設定で定義したIDを指定します。`model`と`name`は空でない文字列、`system`と`cacheKey`は文字列、`maxTokens`と`maxSteps`は0以上の整数です。`name`の既定値は`agent`、`system`は空文字、`maxTokens`と`maxSteps`は0です。`system`と自動読込みinstructionはinstanceの最初のtaskで構成して固定し、同じinstanceのqueue taskではinstruction fileを再読込みしません。新しいinstanceは独自のsystemと会話を持ちます。`instruction`は空でない文字列で、`keepAlive:true`なら省略できます。`options`は独立した第三引数で、受け取る項目は`keepAlive`のみです。`keepAlive`を`config`へ入れないでください。instructionを省略してoptionsを渡す場合は、第二引数に`undefined`を置きます。たとえば`spawn_agent({name:"worker"}, undefined, {keepAlive:true})`はidleで起動し、queueにtaskが届くまでモデルを呼びません。

`effort`で指定できる値は`"auto"`・`"none"`・`"minimal"`・`"low"`・`"medium"`・`"high"`・`"xhigh"`・`"max"`です。明示的な`"auto"`は推論設定を送らず、provider/modelの既定動作に任せます。他の値はprovider設定の`effortField`（OpenRouterは`reasoning.effort`、OpenCode GoのChatは`reasoning_effort`）へ送り、各タスクで固定します。実際に利用できるエフォートと効果はmodel/providerに従います。NOEはモデル別の対応表による事前拒否や値の自動変更を行わず、APIからエラーが返ればprovider errorとして扱います。[OpenRouterのreasoning仕様](https://openrouter.ai/docs/guides/best-practices/reasoning-tokens)を参照してください。

`maxSteps`はタスクごとのモデル要求回数を制限します。0はNOE側の上限なしです。`maxTokens`はモデル要求ごとの出力上限で、会話全体の累計ではありません。0の場合、NOEはOpenRouterへ`max_tokens`を送りません。どちらの上限も指定しない場合、NOEは独自の上限を追加しません。

`cacheKey`はOpenRouterの`session_id`として送られ、routingやprompt cacheで使われる値です。未指定（`undefined`を含む）の場合は各task runの`runId`を使い、同じtask内の全モデル要求へ同じIDを送ります。明示した`cacheKey`は各taskで再利用されます。`cacheKey: ""`は`session_id`を送らない指定です。queue会話の保持はNOE instanceが行い、cacheKeyは会話を保存・再開せず、キャッシュヒットも保証しません。[OpenRouter session_id仕様](https://openrouter.ai/docs/guides/best-practices/prompt-caching#using-session_id-for-sticky-sessions)を参照してください。

### 戻り値・状態・寿命

run snapshotは`{agentId, parentRunId, name, state, reason, keepAlive, currentRunId, queueLength, lastResult}`です。instance stateは`starting`・`idle`・`running`・`stopping`・`stopped`・`failed`です。起動直後やidleでは`currentRunId`が空です。`lastResult`は最後のtaskの`{runId, commandId, state, answer, error?}`で、まだtaskを実行していなければ`null`です。

`keepAlive`の既定値は`false`です。`false`なら一つのtaskの後に停止し、instructionは必須です。`true`ならtask完了・失敗後も待受を続け、通常のtask失敗後も次のqueue taskを受け付けます。instructionを省略するとidleから開始します。idle中にモデルは呼び出しません。同じinstanceのqueue taskは確定済み会話とtrim状態を続けますが、各taskのrun ID・`maxSteps`・task単位interrupt scopeは独立し、実行runtimeとhook moduleの状態は新しくなります。`run_agent(config, instruction, {keepAlive:true})`はinstance停止まで戻りません。

rootのrun/evalではJS本文が終わってもnative追跡によって待受が続き、現在の終了方法はCtrl+C・timeoutなどです。親run_jsから起動した子はtool本文の終了後も親runに所有されます。

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

捕捉するstdout・stderrは、それぞれ元の出力の先頭32KiBと末尾32KiB、合計64KiBまでを保持します。上限以内の出力は変更しません。超過した場合は途中に`[NOE: output truncated; N bytes omitted]`を挿入し、結果に`stdoutOmittedBytes`・`stderrOmittedBytes`を該当streamについて追加します。UTF-8の切断位置は文字境界へ寄せるため、保持量が数バイト減る場合があります。超過分も読み捨て続けるため、上限到達で子プロセスを停止したり出力pipeを詰まらせたりしません。省略だけで`exitCode`や`reason`は変更しません。内部のmanaged taskプロセスの捕捉出力も同じ制限に従います。

省略分は自動保存しません。完全なログが必要な処理は、実行するJSやコマンド側で既知のファイルへ書き出し、必要な部分だけを読み戻してください。afterToolが受け取る元の結果とJSONLも、この制限後の出力です。hookが独自に生成した置換結果、子プロセス内部のメモリ、通常のトップレベル`run`・`eval`の出力を制限する機能ではありません。

`observe_agent`はlive状態と保存済みJSONLの二つのtargetを持ちます。live用は`observe_agent({agentId})`のみです。ファイル観察は既存の`observe_agent({logPath, runId, cursor?, wait?, includeThinking?, includeOutput?})`を維持します。両方のtargetを同じ呼出しへ混在させられません。ファイル観察の戻り値・cursorは[観察ガイド](observe.md)を参照してください。

### 認証と設定

OpenRouter APIキーをテキストファイルへ保存し、`NOE_OPENROUTER_API_KEY_FILE`でパスを指定します。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key ./noe run examples/agent/agent-summary.js README.md
```

この変数が空または未設定なら、`$XDG_CONFIG_HOME/noe/credentials/openrouter-api-key`を探します。`XDG_CONFIG_HOME`が未設定なら`~/.config/noe/credentials/openrouter-api-key`を使います。ファイル内容の前後の空白は取り除きます。Go側が最初のモデル要求時にファイルを読みます。moduleのimportや初期taskなしのidle起動では読みません。task付きのrun/spawnは受領後にモデル要求を開始します。キー値はモデル履歴・子接続情報・traceへ含めません。NOEはsandboxではなく、実行するJSは同一ユーザーのOS権限でファイルや環境変数へアクセスできます。

`NOE_OPENROUTER_BASE_URL`はOpenRouter APIのbase URLを上書きし、既定値は`https://openrouter.ai/api/v1`です。`NOE_AGENT_TRACE=1`を指定すると、実行メタデータをJSON Linesで標準エラーへ出力します。

```sh
NOE_AGENT_TRACE=1 NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent/agent-summary.js README.md 2>agent-trace.log
```

traceにはrun ID、親run ID、agent名、model、status、duration（nanoseconds）、返された場合はprompt/completion/total/cached token数とcostを記録します。`js_result`にはtoolの終了コードと理由も含みます。prompt、生成code、toolのstdout/stderr、APIキーは記録しません。標準エラーには通常の診断も含まれる場合があります。

通常の`run`・`eval`では`--timeout`が評価全体を制限できます。省略時はCLI独自の期限はありません。

WindowsでTEMPとカレントディレクトリが別volumeの場合、生成ソースをカレントディレクトリ上の一時場所へ書き込み、相対importの基準を保ちます。その場合はカレントディレクトリへ一時ファイルを書き込める必要があります。書込みできない場合は、カレントディレクトリと同じvolumeの書込み可能な場所を`TEMP`に指定してください。

## 子プロセスと標準I/Oストリーム

`noe:process`の`spawn`で子の入出力を実行中から扱い、終了・停止・回収を行えます。`noe:io`はホストのstdin/stdout/stderrのWeb Streamsと取消し対応`pipe`を公開します。通常のJavaScript・run_js・Workerで利用できます。公開契約と例は[プロセス・標準I/Oガイド](process-io.md)を参照してください。stdlibの`execFile`・`process.std*`・`pipeTo`は変更しません。

`noe:fs-handle`の位置指定readAt/writeAt、`noe:sockets`のTCP/Unix connect/listen、`noe:signals`のAsyncIterator購読も使えます。API・方向別終了・OS差・所有と回収は[ファイル・ソケット・シグナル](low-level-io.md)を参照してください。

## 意思決定モデル：`noe:decision`

`import { decide } from "noe:decision"`で、`await decide({provider:"openrouter",model,state,questions})`を呼べます。`state`は文字列・JSON object/array、`questions`は名前付き質問objectです。provider/modelは毎回明示し、結果はproviderの応答JSON全体です。通常JSとagentの`run_js`から利用でき、agent loopは作りません。認証は既存OpenRouter設定を共用し、要求は現在の評価・所有scopeの取消しに従います。使用例、検証・エラー、画像part、観察は[意思決定APIガイド](decision.md)を参照してください。

## 画像の読み込みと生成：`noe:image`

`noe:image`は`read_image(path, {detail?})`と`generate_image({provider, model, prompt, output, parameters?})`を公開します。`read_image`はagentの`run_js` tool call内だけで画像を添付し、通常のトップレベル`run`・`eval`やWorkerでは画像入力できません。`generate_image`は通常JSとagentの`run_js`で利用できます。JavaScriptへ返るのはmetadataで、画像bytesやBase64ではありません。provider/model、対応形式、hook、履歴、エラーの契約は[画像ガイド](agent-images.md)を参照してください。

## 構造化イベントの出力

`run`・`eval`で`--observe`を付けると、stdoutへ人間向けの整形表示を直接出せます。`--events=path`との併用でJSONL保存も行えます。`--events=-`は機械処理用のstdout JSONLとして維持し、同じstdoutを使う`--observe`とは同時指定しません。

観察には人間用CLIの`noe observe`と、親用JS APIの`observe_agent`を利用できます。`observe_agent({agentId})`はlive snapshotをすぐ返します。`observe_agent({logPath, runId, cursor?, wait?, includeThinking?, includeOutput?})`は既存JSONLから状態・差分・確定済みの結果を返し、既定は非待機、`wait: true`ならrunの終端まで待ちます。使い方と戻り値は[観察の入口](observe.md)を参照してください。

`run`・`eval`の`--events`で通常出力とagentの詳細イベントをJSONLへ記録できます。`--events=-`はstdout、`--events=path`はファイルへの追記、`--events`単独は起動時のcwdに`noe-events-日時-ID.jsonl`を新規作成します。通常出力、thinking・summary、途中の発言、最終回答、`run_js`の先頭コメントと成功・失敗をrun ID付きで読めます。オプションはソースより前へ置きます。形式と逐次出力の範囲は[観察ガイド](events.md)を参照してください。

## 実行環境とファイル

`process.argv`は実行ファイル名、スクリプト名、スクリプト引数の順です。スクリプト引数は`process.argv.slice(2)`で取得できます。`process.env`は起動時の環境変数、`process.cwd()`はカレントディレクトリです。`process.exit(code)`でプロセスを終了し、終了コードを指定できます。

標準入力全体は`process.stdin.read()`で読み取れます。この読み取りは同期処理で、最初の呼出しでEOFまで読み込み、2回目以降は`null`を返します。空入力でも`null`です。この同期読取りを`--timeout`で強制停止する保証はありません。標準入力を待つスクリプトでは、パイプ入力や端末からのEOFを使って入力を終えてください。

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

HTTP送受信の推奨入口は`noe:http`です。`import { fetch, serve } from "noe:http"`で、既存globalと同じ関数を利用できます。default exportはありません。`fetch()`はWeb Fetch APIです。`Request`・`Response`・`Headers`を使い、レスポンス本文は`text()`、`json()`、`arrayBuffer()`、`bytes()`、またはReadableStreamで読み取れます。レスポンス本文は一度だけ読み取れます。HTTP 4xx・5xxもResponseとして返るため、必要に応じて`response.ok`を確認してください。

```js
import { fetch } from "noe:http";

const response = await fetch("https://example.com/api/data");
if (!response.ok) throw new Error(`HTTP ${response.status}`);
const value = await response.json();
console.log(value);
```

JSONを送信する例です。

```js
import { fetch } from "noe:http";

const response = await fetch("https://example.com/api/items", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ value: 42 }),
});
console.log(response.status, await response.text());
```

`ReadableStream`を要求bodyに渡すと、`Uint8Array`チャンクを全量収集せず逐次送信できます。bodyは一度だけ使え、途中で応答headersが届くと未完了の送信を止めてResponseを返します。`serve`は受信bodyを読取り上限までbufferingしてからhandlerを呼びます。受信streamingや全二重HTTPの保証はありません。既定値、redirect、abort、closeとresponse本文上限の詳細は`noe docs noe:http`で確認できます。

## HTTPサーバー

`noe:http`の`serve(options, handler)`はWeb Requestを受け取り、Web Responseを返すHTTPサーバーを起動します。`options`では`hostname`と`port`を指定できます。ポートの既定値は0で、OSが空きポートを選びます。返されるサーバーハンドルには`url`、`port`、`hostname`、`close()`があります。サーバーが稼働中は実行を継続します。

```js
import { serve } from "noe:http";

const server = serve({ port: 8080 }, async request => {
  const pathname = new URL(request.url).pathname;
  if (pathname === "/echo" && request.method === "POST") {
    return Response.json(await request.json());
  }
  return new Response("Not Found", { status: 404 });
});
console.log(server.url);
```

リクエスト本文はWeb Request APIで読み、応答は必ずResponseで返します。JSONの応答には`Response.json(value)`を使えます。従来のglobal fetch/serveと`node:http`・`http`のserveも維持しています。Request・Response・StreamsとWebSocketは既存globalを共有します。WebSocketはWebSocket APIと、サーバー側では`serve`を通じて利用できます。

`server.close()`は同期的に受付を停止し、現在のNOE管理のstdlibでは処理中のHTTPリクエスト終了を最大5秒待ちます。停止を求めるリクエスト自身のハンドラーから同期的に呼ぶと、そのハンドラーの完了を待つことになります。応答が完了した後の別の処理から呼んでください。これは旧`noe.serve()`の非同期停止とは異なる動作です。

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
