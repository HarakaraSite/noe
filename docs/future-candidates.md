# NOE 次の機能候補

2026-10-19、v1.7.1後のソース・テスト・利用ガイド・実装記録と照合して整理しました。
`<完了>`は合意した範囲を実装済み、`<一部完了>`は実装済み部分と残る候補がある状態、`<未完了>`は検討・未着手を表します。未完了は実装の決定や優先順位を意味しません。

現在の仕様は各利用ガイドを参照してください。実装前の案・旧API・当時の判断は[検討履歴](archive/future-candidates-history.md)へ保存しました。履歴にある未実装記述を現在の残件とは扱いません。

## 検討を続ける候補

### <完了> 内蔵docsとagent初期説明の整備 — 2026-10-19

実行バイナリと同じ版のAPI説明を内蔵し、`noe docs`とJSの`noe:docs`から必要な説明を参照できます。[計画](development/builtin-docs-plan.md)に沿って英文Markdown本文とGo catalogを共通の正本にし、stdlib全26対応moduleの一覧・概要、よく使うAPIの詳細とNode.jsとの差分を収録しました（[実装・検証記録](development/builtin-docs-implementation.md)）。実装テスト・コードレビュー後、実プロバイダへdocsへの誘導を含まない2課題を与え、両方で自発参照と課題完了を確認しました。

初期instructionsへNOEとは何か、agentがNOEに管理され、`run_js`が毎回新しいNOEプロセスで実行されること、docsの調べ方を加えました。system I/Oの詳細は参照先の整備後に内蔵docsへ移しました。約4,700 tokensという変更前の概算だけを理由に削減目標は設けていません。

### <未完了> 辞書参照APIの必要性 — 2026-10-18

専用APIが必要かから検討します。まず既存の`readFile`、JSONなら`JSON.parse`とJSのキー参照で足りるか確認し、共通処理は通常のJSライブラリにする方法も比較します。

具体的な用途・辞書の規模・参照頻度に照らし、必要部分の取得や検索で不足がある場合に専用APIを検討します。形式・配置先・API名・Go/JSの実装方式は未決定です。

<a id="一部完了-汎用の低レベルapi拡張と優先順位--2026-10-18"></a>

### <一部完了> 汎用の低レベルAPI拡張と優先順位 — 2026-10-18

目的は、MCP、LSP、常駐ワーカー、監視・対話スクリプトなどをJSで組み立てるための汎用機能です。NOEの拡張として検討し、stdlibの変更や上流採用を前提にしません。外部ESM・シェル・OSコマンド・ライブラリのNOE向け改修も選択肢です。既存APIとJS・CLIで足りる処理をGo必須とは扱いません。

| グループ | 現在の状態 | 残る検討 |
| --- | --- | --- |
| 1：プロセスと標準I/O | **完了**。`noe:process`の子プロセスハンドルとstdin/stdout/stderr、`noe:io`のホスト標準I/Oストリーム | [公開契約](process-io.md)・[実装記録](development/process-io-implementation.md)を参照 |
| 2：具体的な用途に合わせて追加 | **完了**。`noe:fs-handle`の位置指定I/O、`noe:sockets`のTCP・Unix接続/待受、`noe:signals`のOSシグナル購読 | [公開契約](low-level-io.md)・[実装と総合検証](development/low-level-group2-implementation.md)。stat/truncate/sync等の追加操作は後続候補 |
| 3：後続候補 | **未完了**。ファイル変更通知、端末操作 | ポーリングやCLIと比較。TTY判定・サイズ・raw mode・PTYは別々に評価 |

`execFile`は既に非同期、ファイルの逐次I/Oも既存stdlibで利用可能です。第1グループの追加価値は実行中の入出力・終了観察・停止をハンドルで扱えることです。Ctrl+Cの取消しは既にあり、JSへのOSシグナル配送は別候補です。元QuickJSのfd番号・`dup`・readiness callbackの再現は要件にしません。

### <一部完了> MCPクライアントの利用整備 — 2026-10-18

**JSで外部MCPツールを呼ぶ経路は検証済みです。** 公式SDKのClientをESMへbundleし、`noe:process`を使う独自JS stdio transportを組み合わせた[スパイク](../scripts/spikes/process-io-mcp/README.md)があります。main・Worker・fixture agentの`run_js`で、初期化、tool一覧、並行呼出し、要求と応答の対応付け、大きなUTF-8応答、異常終了と子の回収を確認しました（[import mapの検証記録](development/local-import-map-implementation.md)）。

Node/npm/esbuildはbundleの事前作成に使い、実行時はNOEがJSを動かします。専用のGo製MCP APIは追加していません。通常利用向けの配布・設定・接続先管理をどう整えるかは未決定です。スパイクがあることと、製品として組込みMCPクライアントを提供していることは区別します。

### <未完了> MCPとデーモンのexample作成 — 2026-10-19

利用者の作業メモとして、次の実行例を`examples/`へ追加する。

- MCP：既存のMCPクライアントスパイクを参考に、利用例と起動手順を整える。クライアント/サーバーの役割・通信方式の詳細は作成時に決める。
- デーモン：`noe:sockets`の待受・要求処理と`noe:signals`による終了を使う常駐処理の例を作る。独立したNOEプロセスとして外部から起動・停止する手順も示す。

### <未完了> NOEへのMCPサーバー機能の組込み — 2026-10-17

**優先度は低い。** NOEで実行するJSがtoolを公開し、別アプリ・実行環境のagentから利用できるようにする候補です。同じVM内の別アプリでも別マシンでもよく、自作の集計・レポート、Exa検索ライブラリ、NOE agentへの仕事の委譲などを想定します。

NOE内の親子agentは既存の起動・観察・操作APIで連携でき、この用途にMCPサーバーは必須ではありません。外部へ公開したい具体的な処理が出てから必要性を再検討します。

サーバーSDKの採用、通信方式、起動・公開方法は未決定です。スパイクの`server.js`はクライアント検証用fixtureであり、製品のMCPサーバー機能ではありません。クライアントSDKの検証済み範囲を、そのままサーバーSDKの互換性確認とは扱いません。可能ならJSで実装します。

### <未完了> REPLの削除 — 2026-10-19

利用者の作業メモ：REPLを削除し、日常利用は`noe run`とZedの編集・タスク実行を中心にする。利用頻度に対して、REPL固有の入力UI・履歴・補完・await変換・session回復の保守負担が大きい。

削除時は`repl`コマンドと専用実装・テスト・資料を整理する。run/eval・agentでも使うイベントループと非同期I/Oの回収処理は、依存関係を確認してREPL固有部分から切り分ける。

### <未完了> ZedでのNOEスクリプト開発環境の整備 — 2026-10-17

SSH先のJSを編集し、NOE APIの補完・実行・観察・追加指示まで行える環境を整える候補です。NOE用`.d.ts`、`jsconfig.json`、リモート側の`.zed/tasks.json`と利用ガイドは未整備です。

型定義は本家QuickJSの`std`・`os`、go-quickjs stdlib、Node.jsを混同せず、採用版の実際の契約に合わせます。実行コードをTypeScriptにする必要はありません。まず他のエディターでも再利用できる設定・型を用意し、Zed専用拡張は必要になってから検討します。

利用者はリモートターミナルとtmuxを使う方針です。ファイルに書いた指示は現在もシェル経由で送れます。専用の`--file`・stdin入力指定はありません。

```sh
noe steer RUN_ID "$(cat instruction.md)"
noe queue AGENT_ID "$(cat next-task.md)"
```

### <一部完了> go-quickjs stdlibとNode.jsのAPI差異の説明整備 — 2026-10-17

現在の組込み[モデル向けinstructions](../internal/agent/instructions.md)は、NOEの実行環境と版が一致する内蔵docsの調べ方を説明します。stdlib全26 moduleの概要・export一覧と、優先APIの詳細を英文で収録しました。[参照方法](reference.md)と[実装・検証記録](development/builtin-docs-implementation.md)を参照してください。

`execFile`のPromise、fsのWeb Streams、Node形式Workerのtype option、spawnSyncの起動失敗時statusなどの差異も該当topicに説明しました。全APIの個別詳細や全差異の列挙は完了条件にせず、実際の誤用や利用用途を基に残る説明を追加します。

### <未完了> go-quickjsへの上流PR案：同期コマンドの失敗情報保持 — 2026-10-17

採用版v0.27.0の`stdlib/exec.go`でも、`execFileSync`は失敗時に捕捉したstdout・stderrをErrorへ付けずに返します。非同期`execFile`は`code`・`stdout`・`stderr`をErrorへ付けます。NOEの未処理例外の詳細保持だけでは、上流が失った出力は復元できません。

上流PR候補は、同期経路でも失敗情報を保持する改善です。正常時の契約を維持し、失敗時の終了コードと両出力を取得するテストを添える案です。`execSync`等への適用範囲は提案時に確認します。NOE側での上流修正・PR作成・提出は未実施で、このメモは提出許可ではありません。

### <一部完了> Web検索と検索APIの認証ラッパー — 2026-10-17

**ExaのJSライブラリと利用案内は実装済みです。** [examples/exa/](../examples/exa/README.md)の`exa.js`は`search(query, options)`を公開し、検索結果のJSONと失敗情報を返します。既定の`~/.config/noe/credentials/exa-api-key`または明示した`apiKeyFile`から、JS側でキーを読みます。このサンプルの既定パスは固定で、NOEホストのXDG設定解決と同じではありません。

既知URLは`run_js`の`fetch`で取得でき、agentは配置したライブラリやimport map・instructionsを通じてExaを利用できます。緊急に組込みWeb検索を作る必要はないという方針を維持します。

検索APIの認証をGoホストへ隠す組込みラッパーは未実装です。現在のExa JSサンプルと区別し、専用API・provider・配置方法の採用は必要性が出てから検討します。

### <未完了> Worker内からの`noe:agent`利用

Workerのloaderは`noe:agent`を受け付けません。通常JSからのagent起動は既に非同期で、並行起動や親子の委譲のためにWorker対応を足す必要はありません。必要ならWorkerからmainへメッセージを送り、mainでagentを呼ぶ構成を取れます。

重い計算・解析の途中から直接agentを呼ぶ用途で中継が実際の不便になった時に検討します。module登録だけでなく、broker・認証への接続、Worker終了時の取消しと所有関係も決めます。`noe:process`・`noe:io`のWorker対応や、MCPクライアントのWorker検証とは別の残件です。

### <未完了> 呼び出し元の終了後も続く独立agent実行

`keepAlive:true`は同じNOE内の待受であり、呼出し元の終了後も続く独立実行ではありません。現在の子agentは親runに所有され、親終了・取消しで回収します。

独立したNOEプロセスが自分の認証・broker・loop・回収を持ち、起動元は終了できる構成を候補にします。端末から離れて続けるだけなら、既存のOSのバックグラウンド実行・tmuxも利用できます。組込みAPIの必要性、ログ・結果・停止方法を検討し、会話の永続化・daemon・再起動後の復旧まで一括で必須にはしません。

### <未完了> Anthropic系SDK・Messages形式の追加 — 2026-10-17

OpenAI SDKは追加済みですが、Anthropic系SDK・Messages形式のadapterは未実装です。OpenCode Goの初期対応は利用者がChat Completions・Responsesに限定したため、Messagesはその未完了扱いにせず別候補にします。

追加時もprovider/modelとカタログは共用し、形式変換と認証はGoの通信adapterで扱う方針です。SDK選択と実装範囲は未決定です。

### <未完了> HTTP URLからのimportとimport mapの拡張

ローカルの完全一致import mapは実装済みです。HTTP/HTTPS URLの直接import、永続ダウンロードcache、prefix mapping、scopes、npmのパッケージ探索は提供していません。必要性は個別に判断し、ローカルmapの完了条件へ追加しません。

ESMでもgo-quickjsでの動作保証にはならないため、自作JSと動作確認済みライブラリを再利用する方針です。対応済みの範囲は[import mapガイド](import-map.md)を参照してください。

### <未完了> 観察イベントのSQLite保存

JSONLを検索しやすくする外部保存先の候補です。採用は未決定です。現在のリアルタイム表示・親向け観察APIはSQLiteなしで利用できます。

## 実装済みとして扱う項目

### <完了> 親agentによる子agentの監督・追加指示

現在の入口は`noe:agent`の関数です。旧`agent.run()`・`createAgent`や、`spawn_agent({wait:true})`という検討案を現行APIとして使いません。

| 操作 | 現行の契約 |
| --- | --- |
| `run_agent(config, instruction, options?)` | instance停止まで待ち、最終snapshotを返す |
| `spawn_agent(config, instruction?, options?)` | 開始後に`{agentId}`を返す。`options.keepAlive:true`ならtask後も待受 |
| `observe_agent({agentId})` | live snapshotを即時取得。完了結果も含む。liveに`wait`・cursorは指定しない |
| `observe_agent({logPath, runId, cursor?, ...})` | 保存JSONLから状態・差分・結果を取得。こちらは`wait`も指定可能 |
| `steer_agent({runId, instruction, ...})` | 実行中taskへ追加指示を受領。次のモデル判断前に反映 |
| `interrupt_agent({runId})` / `interrupt_agent({agentId})` | task単位の取消し、またはinstanceと予約の停止 |
| `queue_agent({agentId, instruction, ...})` | 待受instanceへ次taskを予約 |

人間用の`noe agents`・`steer`・`interrupt`・`queue`も実装済みです。子は一回の`run_js`終了後も親runに所有され、後続toolから観察・操作できます。親の完了・取消しで回収されます。[API](api.md)・[起動ガイド](agents.md)・[control実装記録](development/agent-control-implementation.md)。

同じinstanceのqueueは**前taskの最終回答を含む、確定済み・trim後の会話を継続**します。queue指示はそのtask開始時に保護されたuser messageとして追加し、run IDと実行runtimeは新しくします。新しいagent instanceへの履歴共有や、プロセス終了後の再開は意味しません。[queue会話継続の記録](development/queue-continuation-implementation.md)。

<a id="user-instructions"></a>

### <完了> ユーザー共通インストラクションの既定読込み — 2026-10-18

組込みinstruction → 共通設定の`instructions/*.md` → workspaceの`.noe/instructions/*.md` → 明示した`system`の順に追記します。共通側は`XDG_CONFIG_HOME`、空・未設定なら`~/.config`を使います。各ディレクトリ内はファイル名順で、同名でも両方を読みます。

各instanceの最初のtaskで本文を固定し、同じinstanceのqueueでは再読込みしません。hook moduleのtaskごとの再読込みとは異なります。workspaceはNOE起動時のcwdで、祖先・Git root・`AGENTS.md`は自動探索しません。

任意の複数ファイルもJSで読み、本文を結合して`system`へ渡せます。固定本文は切り捨て対象外です。詳細は[instructionsガイド](instructions.md)。

### <完了> 簡易的な実行コンテキスト管理と予算通知

各モデル要求直前に使用量／予算と切り捨て方針を通知し、予算へ達したら古い完了blockを削除します。切り捨て後は再推定した情報と履歴を送信します。固定指示・元task・適用済みsteer・開始したqueue指示を保持し、最新の完全blockも保護します。

既定の`retainRatio`は0.1ですが、最新blockの保護で保持量が10%を超える場合も許容します。モデルによる要約・保存専用turn・必須checkpointは追加せず、必要な情報のファイル保存は任意です。`noe:decision`の単発要求は切り捨てません。

windowと応答予約が既知なら、固定入力と応答予約を差し引いた残りの約90%を実行履歴予算とします。未知値がある場合の計算は[hook・contextガイド](hooks.md)を参照してください。window不明かつ明示予算なしでは総入力目安1,000,000 tokensを使い、モデルcapacityの保証とは扱いません。window不明でも計算済み予算を表示します。

同じinstanceのqueueでは会話とtrim状態を継続します。モデル・人間・JSONL/live snapshotへの通知、hookのcontext APIも実装済みです。[実装記録](development/context-trimming-implementation.md)・[観察ガイド](observe.md)。

### <完了> hookと処理の連鎖の追跡・管理

`beforeModelRequest`・`afterModelResponse`・`beforeTool`・`afterTool`と、hook内からのagent操作を実装済みです。agent・hook・queueを共通のgroup／原因情報で追跡し、任意の`executionLimits`を所有下で共有します。既定で新しい回数上限を課す仕組みではありません。[hookガイド](hooks.md)・[実装記録](development/execution-hooks-implementation.md)。

### <完了> agentの実行状況の観察

人間用は`--observe`とGoの`noe observe`、親用は`observe_agent`です。JSONLには時刻・ID、モデル要求とtoolの開始／終了、thinking・summary、発言・最終回答、実行JS全文、取得できる未処理例外の詳細を記録できます。traceだけでは取得できなかった情報を`--events`で保存できます。

人間向け表示は英語を標準とし、連続するthinkingをまとめ、Markdownを整形します。`run_js`は先頭コメントと成功失敗を表示し、そのコマンド出力やJS全文を通常表示へ流しません。生のJSONLとは表示を分けます。[イベントガイド](events.md)・[観察ガイド](observe.md)。

<a id="外部jsによるリアルタイム表示--2026-10-17"></a>

#### <完了> 外部JS案から親用・人間用の観察口へ — 2026-10-17

外部の観察JSそのものは同梱していません。目的だった逐次表示と親の観察はGo実装で提供済みです。`run/eval/repl`で共通の入口を使えます。

```sh
./noe run --observe --events=agent.jsonl examples/agent/try-agent.js '任意の指示'
./noe run --events=- examples/agent/try-agent.js '任意の指示' | ./noe observe
```

`--observe`だけではファイルを作りません。`--events=path`、またはcwdへ自動命名する単独の`--events`で記録します。通常JSの出力はJSONLでは出力イベントとして区別し、NOE自体の診断はstderrへ出します。SQLite保存は別候補です。

### <完了> 意思決定モデルの呼び出し

`noe:decision`の`decide`からOpenRouterのDecisions APIを呼べます。Jev 1.14・Perplexity Decider V1.2・Clefと、親agentの`run_js`からの呼出しを確認済みです。通常の生成・agent loopとは別経路で、認証と要求管理はホストに寄せています。[利用ガイド](decision.md)・[実接続記録](development/decision-openrouter-validation.md)。

#### <一部完了> 応用例：Jevによる起動設定プリセットの選択 — 2026-10-17

[agent-route-model.js](../examples/agent/agent-route-model.js)は実装済みです。`list_models`の候補と説明をJevに渡し、返却キーからprovider/modelを選んで親agentを起動します。専用ルーターモデルや本体の自動ルーティング機能は不要です。

ローカルfixtureで一連の経路を確認済みです。現在の例は`effort:"auto"`固定で、effortを含むプリセット選択は残る応用候補です。このサンプル自体の有料実接続は未確認であり、`decide`そのものの実接続済み範囲とは区別します。[モデルカタログ](model-catalog.md)。

<a id="完了-agentのvision対応--2026-10-17"></a>

### <完了> agentのVision対応 — 2026-10-17

`noe:image`の`read_image`と独立した`generate_image`を実装済みです。ローカル画像を読み、固定したbytesをモデルへ添付します。queue履歴・block単位のtrim・hookの添付選択・画像イベントにも対応します。通常JSからの直接生成も可能です。

生成はOpenRouterとOpenAI APIキー経路に対応します。ChatGPTプラン／OpenCode Goでの画像生成、画像編集・mask・複数枚生成・入力URLは今回の対応範囲外です。モデルの認識精度とNOEの入力経路は区別します。[画像ガイド](agent-images.md)・[実接続記録](development/agent-images-integration.md)。

### <完了> モデルの推論エフォートと設定継承

ルート起動ではprovider・model・effortを必須とし、`auto`も明示できます。子では省略項目を親から継承し、明示指定で上書きします。queueごとのeffort上書きはありません。reasoningの履歴保持も対応済みです。[APIリファレンス](api.md)。

### <完了> OpenCode Go APIの組込み — 2026-10-17

合意したChat Completions・Responsesの範囲を実装済みです。モデルごとの経路、reasoningとtool履歴、認証、取消し、組込みデフォルト＋外部JSON上書きに対応します。`User-Agent: noe/<version>`と、同じrunの間は固定した`x-opencode-session`を送り、親子・次queue taskは別run IDを使います。[provider設定](providers.md)・[実装記録](development/provider-config-validation.md)。

通常起動の実接続と親子・tool往復・取消しのfixture検証を記録しています。利用枠超過時の実応答は未確認です。Messagesは別の将来候補です。

### <完了> OpenAI SDKとローカル互換接続の設定経路 — 2026-10-17

公式OpenAI SDK、`openai-chatgpt`・`openai-api`のResponses、互換接続のChat Completions、外部JSのChatGPT認証ヘルパーを実装済みです。外部JSONでbaseURL等を上書きし、認証なしの互換接続を設定する経路もあります。個々のローカルLLMでの動作確認は別途必要です。[OpenAIガイド](openai.md)・[実装記録](development/openai-implementation.md)。

### <完了> ローカルESM・import map

ローカルの完全一致bare名をESMへ対応付け、main・Worker・agent生成JSで同じmapを利用できます。ライブラリやmapの配置場所は任意で、自動探索はしません。`~/.config/noe/lib/`も利用者側の配置例であり、予約パスではありません。[利用ガイド](import-map.md)・[検証記録](development/local-import-map-implementation.md)。

### <完了> 自作REPLの必要性とqjsの再利用

固定版go-quickjsのqjs由来の入力・行編集ソースを取り込み、評価先はNOEのruntimeを使います。`noe repl`でstdlibとNOE moduleを対話的に試せます。依存更新だけでは取り込み済みUIソースは変わらないため、上流差分を確認して追随します。[REPLガイド](repl.md)・[統合記録](development/qjs-repl-integration.md)。

### <完了> 実行JS・未処理例外の記録、run_jsの捕捉出力上限

JS全文と利用可能な例外詳細をイベントへ保持します。`run_js`のstdout・stderrはそれぞれ先頭32KiB＋末尾32KiBの計64KiBまで保持し、省略量を示します。JSONLにも省略後の値を記録し、省略部分を自動で完全保存する仕様ではありません。全文が必要な処理はJS側でファイルへ保存できます。[イベントガイド](events.md)・[runner実装](../internal/agent/runner/output.go)。

実行JSは引き続きNOE利用者のOS権限で動きます。cwdはworkspaceへのアクセス制限ではありません。隔離が必要ならVM等で動かすという利用方針で、今回NOE独自のworkspace権限制御は追加していません。

### <完了> internal/agentの責務整理

provider、runner、観察、instanceとtask、JS側の公開API／loop／履歴／hookなどへ責務を整理しました。[構成ガイド](development/agent-architecture.md)・[実装記録](development/agent-refactor-implementation.md)を参照してください。今後の変更場所も構成ガイドを入口にします。

## モデル・接続先の選択方針

普段はサブスクのOpenCode Goを優先し、OpenRouterは様々なモデルの試用と意思決定モデルに使います。よく使う順はDeepSeek V4.1 Flash、MiMo-V2.6-Flash、GPT 6 Luna、GLM 5.3で、effortは用途で変えます。OpenAIではSol・LunaのResponsesを使い、ChatGPTサインインをAPIキーより優先します。

カタログは組込みJSON＋外部JSON上書きと`list_models`を実装済みです。通常生成・意思決定モデルのwindow等も記録しており、実行時の予算計算に使います。画像生成モデルのwindowは未設定です。値の根拠・未知値・OpenCode GoへOpenRouter値を採用した経緯は[モデルカタログ](model-catalog.md)を参照してください。

MiMo・GLMの組込み候補、優先度項目、providerをまたいだ自動選択は未実装です。カタログは許可リストではなく、直接JSへprovider/modelを書いたり、外部JSONへ候補を追加したりできます。これらの選択方針を自動適用する本体機能を新しく決定したものではありません。

## 維持する製品方針

外部コマンド、ファイル・HTTPのストリーミング、暗号・ハッシュは既にstdlibから利用できます。v1.0.0時点の未対応候補とは区別します。TypeScriptは後回し、npm・パッケージ管理とNode.js/Denoの完全互換は対象外です。元QuickJSの`std`・`os`の提供を前提にしません。

[開発記録へ戻る](README.md) · [検討履歴](archive/future-candidates-history.md) · [現在のAPI](api.md)
