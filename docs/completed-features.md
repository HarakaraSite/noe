# NOE 完了した機能と実装記録

[次の機能候補](future-candidates.md) · [ドキュメント一覧](README.md) · [開発ガイド](development/README.md)

`future-candidates.md`にあった完了23項目を、2026-10-24に本文と参照を維持して分離しました。
未完了・一部完了の候補は[次の機能候補](future-candidates.md)を参照してください。
以下は合意した範囲の実装・検証記録です。現在のAPI契約は各利用ガイドで確認してください。

<a id="completed"></a>

## 完了（合意範囲を実装済み）

### <完了> Go側での再委譲の制限 — 2026-10-21

利用者の追加依頼で、モデルへの指示に加えてGo側の起動受付で再委譲を制限した。起動時の第三引数`options.allowDelegation`はrootでは省略時true、子では省略時false。親が明示的にtrueを指定して起動した子だけが次の階層を起動でき、許可は次の子へ自動継承しない。`spawn_agent`・`run_agent`・hookに共通の`Host.spawn`で起動元を検査する。拒否は当該要求だけをrejectし、group停止や起動枠の消費はしない。[API](api.md#設定)。実装・関連テストを完了し、v1.16.0で公開済み。Linux amd64の公開配布物は常用バイナリへ配置済みで、他対象はビルド・checksum確認まで。[リリース記録](releases/release-checkpoints.md)。

現在の`executionLimits.agentStarts`による累計起動数制限とは別の制御とする。任意の最大階層・同時起動数の設定は追加していない。子を親タスクに所有させる寿命も維持し、親のqueue task間での長期再利用へは拡張しない。

発端は`noe-events-20261020-224654-b7d639559fb5e94d.jsonl`の確認。親が直接16体、子が孫を1体起動していた。孫`modulemap-review`は、子`inspect-cli-docs`の担当範囲の一部である`internal/modulemap`の仕様照合・不具合候補調査を行ったが、子への依頼文には再委譲の明示許可がなかった。

照合根拠：[`Host.spawn`](../internal/agent/instance_operations.go)が起動枠の予約前に権限を確認。[`delegation_test.go`](../internal/agent/delegation_test.go)が明示許可・非継承・拒否時のgroup維持を扱う。

### <完了> x/term依存の整理 — 2026-10-19

REPL削除後に残ったobserve表示の端末幅取得を、既存の`x/sys`を使う小さなOS別helperへ置換しました。Linux/macOSは`IoctlGetWinsize`、Windowsは`GetConsoleScreenBufferInfo`の可視window幅を使います。`COLUMNS`→端末幅→80桁の選択を維持し、NOEの`go.mod`・`go.sum`と6配布対象のCLIビルド依存から`x/term`を除去しました。go-quickjs自身の`go.mod`にある間接的なmodule graph上の記載（v0.40.0）は残ります。

CLI単体テスト、観察とCLI入口の重点E2E、`go vet ./internal/cli`が成功しました。LinuxのPTYで実際の端末幅、環境変数の優先、0幅のfallbackを検証し、通常ファイル・bufferでもfallbackを確認しています。6配布対象のCGO不要ビルドとCLIテストのcross-compileも成功しました。他環境は実機確認ではありません。自己レビューではfdの非所有、Windowsの可視windowとbuffer幅の区別、失敗時のfallback、他依存versionの維持を確認しました。ビルド記録は`/tmp/noe-term-removal-20261019-mqhozkrm/checks.json`にあります。

照合根拠：[`go.mod`](../go.mod)・[`terminal_width_unix.go`](../internal/cli/terminal_width_unix.go)・[`terminal_width_windows.go`](../internal/cli/terminal_width_windows.go)と[`terminal_width_test.go`](../internal/cli/terminal_width_test.go)。

### <完了> モデル要求のretry追加 — 2026-10-20

組込みproviderのagentモデル要求に、最大3試行・2回目まで2秒・3回目まで4秒の固定retryを追加した。通信障害・429・5xx・切断や終端欠落による不完全なstreamを再試行し、認証失敗・残高不足・入力不正・取消しは再試行しない。待機中も既存の取消しに従う。完結した明示的な失敗応答やrefusal・出力制限を一律に再試行するものではない。

同じ入力をprovider内部で再送し、完了済みtool・hookは再実行しない。失敗した応答は会話履歴へ追加せず、成功した試行の完全な応答だけを採用する。表示・JSONLへ既に流れたthinking等は残し、重複を許容する。利用者の追加方針に従い、回数・待機時間の設定項目やretry専用の観察イベントは追加していない。`maxSteps`・`providerCalls`は従来の論理要求単位で、内部再送を別要求として数えない。decision・画像生成・利用者が注入した独自providerにはこのretryを追加しない。[providerガイド](providers.md#モデル要求の再試行)。

照合根拠：[`provider/retry.go`](../internal/agent/provider/retry.go)・[`retry_test.go`](../internal/agent/provider/retry_test.go)と[`provider_retry_e2e_test.go`](../tests/provider_retry_e2e_test.go)。

### <完了> 内蔵docsとagent初期説明の整備 — 2026-10-19

実行バイナリと同じ版のAPI説明を内蔵し、`noe docs`とJSの`noe:docs`から必要な説明を参照できます。[計画](development/builtin-docs-plan.md)に沿って英文Markdown本文とGo catalogを共通の正本にし、Node-styleの全26対応moduleの一覧・概要、よく使うAPIの詳細とNode.jsとの差分を収録しました（[実装・検証記録](development/builtin-docs-implementation.md)）。実装テスト・コードレビュー後、実プロバイダへdocsへの誘導を含まない2課題を与え、両方で自発参照と課題完了を確認しました。

初期instructionsへNOEとは何か、agentがNOEに管理され、`run_js`が毎回新しいNOEプロセスで実行されること、docsの調べ方を加えました。system I/Oの詳細は参照先の整備後に内蔵docsへ移しました。約4,700 tokensという変更前の概算だけを理由に削減目標は設けていません。

2026-10-20、利用者の確認により「辞書参照APIの必要性 — 2026-10-18」をこの完了項目へ統合しました。目的は、agentがNOEのAPI仕様を辞書のように必要な部分だけ参照できることです。`noe docs [topic [member]]`と`noe:docs`で実現済みです。任意の辞書ファイルを検索する汎用APIは、この項目の残件には含めません。[使い方](reference.md)・[当時の検討メモ](archive/future-candidates-history.md#未完了-辞書参照apiの必要性--2026-10-18)を参照してください。

照合根拠：[`internal/apidocs/catalog.go`](../internal/apidocs/catalog.go)・[`facts.md`](../internal/agent/facts.md)と[`docs_test.go`](../internal/runtime/docs_test.go)。

### <完了> REPLの削除 — 2026-10-19

日常利用をファイル編集と`noe run`、短い確認を`noe eval`へ揃え、REPLを削除しました。CLI、入力UI、履歴、補完、await変換、Sessionと入力間の回復専用処理・テスト・現行資料を撤去しました。

共有の実行・取消し・I/O回収、Worker・agent/run_js、docsと旧JSONLの閲覧は維持しています。[計画](development/repl-removal-plan.md)に基づき、実装・テスト・独立コードレビュー、その後の全E2E・race・vet・6対象ビルド・実CLI総合テストを完了しました。[実装・検証記録](development/repl-removal-implementation.md)を参照してください。

`noe repl`は未対応コマンドとして終了コード2を返します。既存履歴ファイルは削除しません。v1.8.0のタグ・配布物は変更せず、Zedの開発環境整備は別候補です。

照合根拠：[`internal/cli/cli.go`](../internal/cli/cli.go)にREPL入口はなく、[`cli_test.go`](../internal/cli/cli_test.go)でhelpからの除去と`repl`の終了コード2を確認するテストがある。

### <完了> NOE管理stdlibの同期コマンド失敗情報保持 — 2026-10-21

利用者の実装依頼に基づき、`execFileSync`・`execSync`の失敗時にもErrorへ`code`・`stdout`・`stderr`を付ける変更とテスト・docsを追加済みです。正常時の文字列戻り値と既存メッセージは維持します。[APIの説明](../internal/apidocs/content/stdlib-child_process.md)。v1.15.1でリリースし、Linux amd64の常用バイナリへの配置と配布物の検証も完了しました。[リリース記録](releases/release-checkpoints.md)。

照合根拠：[`internal/stdlib/exec.go`](../internal/stdlib/exec.go)の同期経路と[`exec_test.go`](../internal/stdlib/exec_test.go)の失敗情報・正常時互換テスト。

### <完了> 親agentによる子agentの監督・追加指示

現在の入口は`noe:agent`の関数です。旧`agent.run()`・`createAgent`や、`spawn_agent({wait:true})`という検討案を現行APIとして使いません。

| 操作 | 現行の契約 |
| --- | --- |
| `run_agent(config, instruction, options?)` | instance停止まで待ち、最終snapshotを返す |
| `spawn_agent(config, instruction?, options?)` | 開始後に`{agentId, commandId?}`を返す。`options.keepAlive:true`ならtask後も待受 |
| `observe_agent({agentId, includeAnswer?})` | live snapshotを即時取得。既定で完了結果も含む。`includeAnswer:false`で回答を省略。liveに`wait`・cursorは指定しない |
| `observe_agent({logPath, runId, cursor?, ...})` | 保存JSONLから状態・差分・結果を取得。こちらは`wait`も指定可能 |
| `steer_agent({runId, instruction, ...})` | 実行中taskへ追加指示を受領。次のモデル判断前に反映 |
| `cancel_task({agentId, commandId})` / `stop_agent({agentId})` | 受理済みcommand一件の取消し、またはinstanceと残るqueueの停止。旧`interrupt_agent`はv2で置換 |
| `queue_agent({agentId, instruction, ...})` | 待受instanceへ次taskを予約 |
| `wait_task({agentId, commandId, includeAnswer?})` | 初期・queue taskの個別結果を待つ。再取得可能。`includeAnswer:false`で回答を省略し`answerBytes`を取得 |
| `read_task_result({agentId, commandId, offset?, maxBytes?})` | 完了したtaskの保持済み原文をUTF-8 byte範囲で取得。`nextOffset`から続行 |

人間用の`noe agents`・`steer`・`interrupt`・`queue`も実装済みです。子は一回の`run_js`終了後も親runに所有され、後続toolから観察・操作できます。親の完了・取消しで回収されます。[API](api.md)・[起動ガイド](agents.md)・[control実装記録](development/agent-control-implementation.md)。

同じinstanceのqueueは**前taskの最終回答を含む、確定済み・trim後の会話を継続**します。queue指示はそのtask開始時に保護されたuser messageとして追加し、run IDと実行runtimeは新しくします。新しいagent instanceへの履歴共有や、プロセス終了後の再開は意味しません。[queue会話継続の記録](development/queue-continuation-implementation.md)。

照合根拠：[`js/api.js`](../internal/agent/js/api.js)・[`internal/cli/agent_control.go`](../internal/cli/agent_control.go)と[`queue_continuation_test.go`](../internal/runtime/queue_continuation_test.go)。

<a id="user-instructions"></a>

### <完了> ユーザー共通インストラクションの既定読込み — 2026-10-18

組込みinstruction → 共通設定の`instructions/*.md` → workspaceの`.noe/instructions/*.md` → 明示した`system`の順に追記します。共通側は`XDG_CONFIG_HOME`、空・未設定なら`~/.config`を使います。各ディレクトリ内はファイル名順で、同名でも両方を読みます。

各instanceの最初のtaskで本文を固定し、同じinstanceのqueueでは再読込みしません。hook moduleのtaskごとの再読込みとは異なります。workspaceはNOE起動時のcwdで、祖先・Git root・`AGENTS.md`は自動探索しません。

任意の複数ファイルもJSで読み、本文を結合して`system`へ渡せます。固定本文は切り捨て対象外です。詳細は[instructionsガイド](instructions.md)。

照合根拠：[`NOE instruction source`](../internal/noeprofile/instructions.go)・[`conversation.go`](../internal/agent/conversation.go)と[`agent_instructions_test.go`](../internal/runtime/agent_instructions_test.go)。

### <完了> 簡易的な実行コンテキスト管理と予算通知

各モデル要求直前に使用量／予算と切り捨て方針を通知し、予算へ達したら古い完了blockを削除します。切り捨て後は再推定した情報と履歴を送信します。固定指示・元task・適用済みsteer・開始したqueue指示を保持し、最新の完全blockも保護します。

既定の`retainRatio`は0.1ですが、最新blockの保護で保持量が10%を超える場合も許容します。モデルによる要約・保存専用turn・必須checkpointは追加せず、必要な情報のファイル保存は任意です。`noe:decision`の単発要求は切り捨てません。

windowと応答予約が既知なら、固定入力と応答予約を差し引いた残りの約90%を実行履歴予算とします。未知値がある場合の計算は[hook・contextガイド](hooks.md)を参照してください。window不明かつ明示予算なしでは総入力目安1,000,000 tokensを使い、モデルcapacityの保証とは扱いません。window不明でも計算済み予算を表示します。

同じinstanceのqueueでは会話とtrim状態を継続します。`NOE_CONTEXT_STATUS`と要求時のruntime情報も、対応する応答・tool結果と同じblockで履歴へ保持し、trimで一緒に除去します。モデル・人間・JSONL/live snapshotへの通知、hookのcontext APIも実装済みです。[実装記録](development/context-trimming-implementation.md)・[観察ガイド](observe.md)。

照合根拠：[`context_policy.go`](../internal/agent/context_policy.go)・[`js/history.js`](../internal/agent/js/history.js)と[`context_trimming_test.go`](../internal/runtime/context_trimming_test.go)。

### <完了> hookと処理の連鎖の追跡・管理

`beforeModelRequest`・`afterModelResponse`・`beforeTool`・`afterTool`と、hook内からのagent操作を実装済みです。agent・hook・queueを共通のgroup／原因情報で追跡し、任意の`executionLimits`を所有下で共有します。既定で新しい回数上限を課す仕組みではありません。[hookガイド](hooks.md)・[実装記録](development/execution-hooks-implementation.md)。

照合根拠：[`broker_hooks.go`](../internal/agent/broker_hooks.go)・[`execution.go`](../internal/agent/execution.go)と[`hooks_limits_test.go`](../internal/runtime/hooks_limits_test.go)。

### <完了> agentの実行状況の観察

人間用は`--observe`とGoの`noe observe`、親用は`observe_agent`です。JSONLには時刻・ID、モデル要求とtoolの開始／終了、thinking・summary、発言・最終回答、実行JS全文、取得できる未処理例外の詳細を記録できます。traceだけでは取得できなかった情報を`--events`で保存できます。

人間向け表示は英語を標準とし、連続するthinkingをまとめ、Markdownを整形します。`run_js`は先頭コメントと成功失敗を表示し、そのコマンド出力やJS全文を通常表示へ流しません。生のJSONLとは表示を分けます。[イベントガイド](events.md)・[観察ガイド](observe.md)。

照合根拠：[`log binding`](../internal/logbinding/binding.go)・[`internal/cli/observe.go`](../internal/cli/observe.go)と[`agent_observation_test.go`](../internal/runtime/agent_observation_test.go)・[`events_test.go`](../internal/cli/events_test.go)。

<a id="外部jsによるリアルタイム表示--2026-10-17"></a>

#### <完了> 外部JS案から親用・人間用の観察口へ — 2026-10-17

外部の観察JSそのものは同梱していません。目的だった逐次表示と親の観察はGo実装で提供済みです。`run`・`eval`で入口を使えます。

```sh
./noe run --observe --events=agent.jsonl examples/agent/try-agent.js '任意の指示'
./noe run --events=- examples/agent/try-agent.js '任意の指示' | ./noe observe
```

`--observe`だけではファイルを作りません。`--events=path`、または起動cwdの`.noe/events/`へ自動命名する単独の`--events`で記録します。通常JSの出力はJSONLでは出力イベントとして区別し、NOE自体の診断はstderrへ出します。SQLite保存は別候補です。

### <完了> 意思決定モデルの呼び出し

`noe:decision`の`decide`からOpenRouterのDecisions APIを呼べます。Jev 1.14・Perplexity Decider V1.2・Clefと、親agentの`run_js`からの呼出しを確認済みです。通常の生成・agent loopとは別経路で、認証と要求管理はホストに寄せています。[利用ガイド](decision.md)・[実接続記録](development/decision-openrouter-validation.md)。

照合根拠：[`provider/decision.go`](../internal/agent/provider/decision.go)と[`decision_agent_test.go`](../internal/runtime/decision_agent_test.go)。

<a id="完了-agentのvision対応--2026-10-17"></a>

### <完了> agentのVision対応 — 2026-10-17

`noe:image`の`read_image`と独立した`generate_image`を実装済みです。ローカル画像を読み、固定したbytesをモデルへ添付します。queue履歴・block単位のtrim・hookの添付選択・画像イベントにも対応します。通常JSからの直接生成も可能です。

生成はOpenRouterとOpenAI APIキー経路に対応します。ChatGPTプラン／OpenCode Goを画像生成先providerにすること、画像編集・mask・複数枚生成・入力URLは今回の対応範囲外です。ChatGPT／OpenCode Goのagentが別の対応providerを明示して生成することは可能です。モデルの認識精度とNOEの入力経路は区別します。[画像ガイド](agent-images.md)・[実接続記録](development/agent-images-integration.md)。

照合根拠：[`images.go`](../internal/agent/images.go)・[`provider/image.go`](../internal/agent/provider/image.go)と[`images_test.go`](../internal/runtime/images_test.go)。

### <完了> モデルの推論エフォートと設定継承

ルート起動ではprovider・model・effortを必須とし、`auto`も明示できます。子では省略項目を親から継承し、明示指定で上書きします。queueごとのeffort上書きはありません。reasoningの履歴保持も対応済みです。[APIリファレンス](api.md)。

照合根拠：[`js/api.js`](../internal/agent/js/api.js)の`configFor`と[`agent_configuration_test.go`](../internal/runtime/agent_configuration_test.go)・[`effort_test.go`](../internal/agent/effort_test.go)。

### <完了> OpenCode Go APIの組込み — 2026-10-17

合意したChat Completions・Responsesの範囲を実装済みです。モデルごとの経路、reasoningとtool履歴、認証、取消し、組込みデフォルト＋外部JSON上書きに対応します。`User-Agent: noe/<version>`と、同じinstanceのqueue task間でも固定した`x-opencode-session`にagentIdを送り、親子は別のセッションIDを使います。taskごとのrunIdは別途発行します。[provider設定](providers.md)・[実装記録](development/provider-config-validation.md)。

通常起動の実接続と親子・tool往復・取消しのfixture検証を記録しています。利用枠超過時の実応答は未確認です。Messagesは別の将来候補です。

照合根拠：[`provider/defaults.json`](../internal/agent/provider/defaults.json)・[`configured.go`](../internal/agent/provider/configured.go)と[`provider_config_test.go`](../internal/cli/provider_config_test.go)。

### <完了> OpenAI SDKとローカル互換接続の設定経路 — 2026-10-17

公式OpenAI SDK、`openai-chatgpt`・`openai-api`のResponses、互換接続のChat Completions、外部JSのChatGPT認証ヘルパーを実装済みです。外部JSONでbaseURL等を上書きし、認証なしの互換接続を設定する経路もあります。個々のローカルLLMでの動作確認は別途必要です。[OpenAIガイド](openai.md)・[実装記録](development/openai-implementation.md)。

照合根拠：[`provider/openai.go`](../internal/agent/provider/openai.go)と[`openai_provider_test.go`](../internal/cli/openai_provider_test.go)・[`provider/openai_test.go`](../internal/agent/provider/openai_test.go)。

### <完了> ローカルESM・import map

ローカルの完全一致bare名をESMへ対応付け、main・Worker・agent生成JSで同じmapを利用できます。ライブラリやmapの配置場所は任意で、自動探索はしません。`~/.config/noe/lib/`も利用者側の配置例であり、予約パスではありません。[利用ガイド](import-map.md)・[検証記録](development/local-import-map-implementation.md)。

照合根拠：[`modulemap/loader.go`](../internal/modulemap/loader.go)・[`loader_test.go`](../internal/modulemap/loader_test.go)と[`import_map_test.go`](../internal/runtime/import_map_test.go)。

### <完了> 実行JS・未処理例外の記録、run_jsの捕捉出力上限

JS全文と利用可能な例外詳細をイベントへ保持します。`run_js`のstdout・stderrはそれぞれ先頭32KiB＋末尾32KiBの計64KiBまで保持し、省略量を示します。JSONLにも省略後の値を記録し、省略部分を自動で完全保存する仕様ではありません。全文が必要な処理はJS側でファイルへ保存できます。[イベントガイド](events.md)・[capture実装](../internal/agentexec/output.go)。

実行JSは引き続きNOE利用者のOS権限で動きます。cwdはworkspaceへのアクセス制限ではありません。隔離が必要ならVM等で動かすという利用方針で、今回NOE独自のworkspace権限制御は追加していません。Goから利用する候補と制約は、後日追記の[隔離作業環境の検討メモ](future-candidates.md#microvm)を参照してください。採用・実装は未決定です。

照合根拠：[`broker_tools.go`](../internal/agent/broker_tools.go)・[`agentexec/output.go`](../internal/agentexec/output.go)と[`agent_output_e2e_test.go`](../tests/agent_output_e2e_test.go)。

### <完了> internal/agentの責務整理

公開Go APIの`agent.Host`／`Client`、内部の所有・instance/task管理、JS側の公開API／loop／履歴／hook、provider接続、同一プロセス内の実行層へ責務を分けています。現行の変更場所は[構成ガイド](development/agent-architecture.md)を入口にします。[旧配置整理の実装記録](development/agent-refactor-implementation.md)は当時の構成の履歴であり、現在のrunnerやHTTP brokerの存在を示すものではありません。

照合根拠：[`agent/host.go`](../agent/host.go)、[`internal/agent`](../internal/agent/)のprovider・instance/task・JS各層、[`internal/agentexec/executor.go`](../internal/agentexec/executor.go)と、公開接続を共用する[`internal/agentbridge/bridge.go`](../internal/agentbridge/bridge.go)。

### <完了> agentのアプリ内runtime実行 — 2026-10-20

2026-10-21、利用者が破壊的API変更を含む分離案と[11スライスの実装計画](development/v2-agent-runtime-implementation-plan.md)を承認し、各sliceの実装・対象試験・独立review・修正を実施しました。公開`agent.Host`/`Client`が同じGoプロセス内のfreshなstdlib runtimeでtask/toolを動かし、別NOE実行ファイルは不要です。旧`agent.Install`・内部HTTP RPC・Connection・別NOE Runnerは撤去しました。この構成はv2.0.0で公開済みで、v2.4.0のCLI・公開Go moduleでも利用できます。[移行ガイド](v2-migration.md)・[公開検証記録](releases/release-checkpoints.md#v200-release-validation--2026-10-21)。

stdlibはProfile・VM/Loop・loader・共通FS/I/Oと回収、agentは会話・session・所有・queue・hook・model/auth/log、NOEは既定場所・環境選択・Policy・instruction directory・人間用control・event保存を担当します。model/auth/logはHostなしでも利用できます。現行の利用契約は[Go agent組込みガイド](go-agent-embedding.md)と[stdlib組込みガイド](go-embedding.md)、内部の配置は[構成ガイド](development/agent-architecture.md)を参照してください。v2移行時の設計・責務分離の判断は[API/移行契約](development/v2-api-redesign.md)と[責務台帳](development/v2-responsibility-boundaries.md)、当時の進捗・検証・review採否は[実装記録](development/v2-implementation.md)に残します。

初期の別process方式を選択可能にする案、8slice、内部RPC維持は採用しませんでした。承認後の新構成を[Go agent組込みガイド](go-agent-embedding.md)と[stdlib組込みガイド](go-embedding.md)へ反映しました。

照合根拠：[`agent/host.go`](../agent/host.go)・[`agentexec`](../internal/agentexec/executor.go)・[`noeprofile`](../internal/noeprofile/profile.go)と、実Go Host・外部moduleの[`consumer検証`](../agent/consumer_test.go)。

### <完了> ZedでのNOEスクリプト開発環境の整備 — 2026-10-24

<a id="一部完了-zedでのnoeスクリプト開発環境の整備--2026-10-17"></a>

workspaceの`.zed/jsconfig.json`は、v2.3.0の`stdlib/types/noestdlib.d.ts`と`agent/types/noeagentlib.d.ts`を直接参照する。v1.15.1の独自`.zed/noe.d.ts`を削除し、DOM型への依存も外した。runnerの設定型は正式な`NOEAgent.AgentConfig`へ移行。既存のprovider/model/effort、指示ファイルの読込み、keepAlive待受、実行タスクは維持する。

[Zedの設定・再利用ガイド](zed.md)に、型宣言の設定、JSDoc、新しいcheckoutへのrunner配置、指示ファイル用task、型検査と別workspaceでの再利用を記載した。`.zed/`はGit管理外のlocal設定で、cloneしただけで配布されるものではない。利用ガイドを追跡対象として残す。

利用者のリモートターミナル・tmuxによる運用方針は維持する。ファイルの指示をシェルから`noe steer RUN_ID "$(cat instruction.md)"`や`noe queue AGENT_ID "$(cat next-task.md)"`で送る方法も引き続き利用できる。これらのCLIへ専用の`--file`・stdin入力指定を追加する作業は含めない。

TypeScript 5.9.3で既存runnerとガイドから作った単発/keepAlive runnerの型検査・help表示が成功。新APIの正常例を受け付け、不正な`includeAnswer`とDOM APIの利用を検出した。読み込んだ型にDOM・`@types/node`・旧宣言がないこと、既存tasks/promptsとrunnerの実行ロジックを維持したことも確認した。記録と旧設定のbackupは`/var/tmp/noe-zed-types-ljjhhrq9/results.json`と同ディレクトリ。Zed GUIでの補完表示と実providerへの接続は未確認で、Go全体suiteは再実行していない。実行コードのTypeScript化やZed専用拡張は今回の範囲に含めない。

### <完了> MCPとデーモンのexample作成 — 2026-10-24

[MCP利用例](../examples/mcp/README.md)は、公式SDKを事前bundleし、NOE上のJSからExa公式Streamable HTTPサーバーへ接続する。匿名のtool一覧取得、明示的な検索、任意のkeyファイルを使うheader認証の手順を整えた。ローカルのJSON/SSE fixtureでも外部サービスなしに試せる。既存のstdioスパイクは維持する。

[デーモン例](../examples/daemon/README.md)は、`noe:sockets`でTCP/JSONL要求を受け、`noe:signals`のSIGTERMでlistener・接続・処理を回収する。外部shellから独立したNOEプロセスを起動・利用・停止する手順も記載した。

Linux amd64でローカルMCPの初期化/一覧/並列呼出し、Exaエントリのfixture、実Exaの初期化と一覧、デーモンの分割/並行/不正要求・SIGTERMを確認。実Exaの検索や有料要求は行っていない。[実装・検証記録](development/mcp-daemon-examples.md)を参照。Go製品コード・go.mod/go.sum・上流は変更していない。

<a id="noe-stdlib"></a>
<a id="一部完了-noe版stdlibの実装--2026-10-19"></a>

### <完了> NOE版stdlibの実装・公開 — 2026-10-24

「go-quickjsエンジン＋NOE管理のライブラリ群＋それらを組み立てるNOE実行コマンド」という構成を実装済みです。ステップ1〜7.2の取り込み・API組立・Go公開入口に加え、v2で同一プロセス内のagent実行へ移行し、`/v2` Go moduleを公開しました。v1系での常用判断とv2公開を、現在の未完了作業として残しません。[v2.0.0](releases/v2.0.0.md)・[v2.4.0の公開検証](releases/release-checkpoints.md#v240-release-validation--2026-10-24)。

公開`forge.harakara.site/littleisland/noe/v2/stdlib`は汎用runtime・Profile・VM/Loop・loader・I/Oと回収を提供し、公開`agent`はHost／Clientとagent機能を追加します。同じrepository/root moduleの入口であり、別repositoryへの分離や別NOE実行ファイルは不要です。task／toolは同じGoプロセス内の新しいstdlib runtimeで実行します。[stdlib組込み](go-embedding.md)・[agent組込み](go-agent-embedding.md)。

エンジンは正式go-quickjs v0.29.0を固定し、汎用APIはv0.27.0のstdlibを原型とするNOE管理実装です。エンジン依存とstdlibの出自を区別し、上流stdlibの全面追随を前提にしません。`internal/noestdlib`が汎用APIを組み立て、`internal/noeprofile`と`internal/runtime`がNOE固有の設定・実行経路を接続します。

通常のHTTP用途と要求・応答のストリーミングは`noe:http`を推奨入口とし、既存の`fetch`・`serve`・`node:http`も維持します。Web型・Streams・WebSocket・loopは共通実装を利用します。後続の低レベルAPI拡張は[別候補](future-candidates.md#一部完了-汎用の低レベルapi拡張と優先順位--2026-10-18)です。

公開名は実装の出自と分けて考える。Web標準の型・共通機能は標準名を維持し、NOEが設計するAPIは`noe:`を正式な入口にする。`node:fs`は当面維持し、NOEとして仕様を整える段階で`noe:fs`を正式名、`node:fs`を同じ実装を参照する互換エイリアスにする案を持つ。将来は`noe:fs-handle`を`noe:fs`へ統合することも検討し、ハンドルの表現・読み書き・closeの契約と旧入口の互換性を整理する。エイリアスなら振る舞いも共通になり、互換動作に差が必要な部分だけ別途扱う。一律の改名は行わず、具体的な範囲・時期は未決定。

上記のAPI改名・統合案は未採用の後続候補であり、実装・公開済みのstdlibを未完了へ戻す条件ではありません。

照合根拠：[`stdlib/stdlib.go`](../stdlib/stdlib.go)・[`agent/host.go`](../agent/host.go)・[`internal/noestdlib`](../internal/noestdlib/noestdlib.go)・[`internal/agentexec/executor.go`](../internal/agentexec/executor.go)。外部consumerのコードは[`stdlib/consumer_test.go`](../stdlib/consumer_test.go)・[`agent/consumer_test.go`](../agent/consumer_test.go)、実際の公開module検証は[リリース記録](releases/release-checkpoints.md)にあります。

#### v1系での段階記録（履歴）

以下は2026-10-19〜21の段階案と当時の検証結果です。「v2公開は後続」「次のステップへ残す」等は当時の状態であり、現在の未完了事項や実行方式ではありません。元の計画・実装・検証記録は変更せず残します。現在の構成・公開状況は上記の要約と利用ガイドを参照してください。

利用者の追加方針（2026-10-19）：まずv1.13.0を常用し、見つかった小さなバグを修正して問題なさそうと判断できてからv2.0.0にする。公開済みv1.13.0はステップ6までの実装とステップ7の計画を含む。その後の追加依頼で7.1と7.2の実装・テスト・コードレビュー・総合テストを完了した。v2公開や常用確認完了は今回の実装完了と区別する。

利用者と共有した長期的な節目は、他のGoアプリにも組み込めるNOEを目指すv2.0.0。途中は1.xで完成した単位ごとにリリースできる進め方とし、最終形を一度に実装しない。各段階の具体的なversion・時期は未決定。

段階案（2026-10-19）：

| 段階 | 変更内容 | その段階の完了の目安 |
| --- | --- | --- |
| 1：stdlibの取り込み（完了） | 固定版のソース・テスト・ライセンスと由来を取り込み、非公開packageへの依存を整理した。取り込み時点ではNOE本体は従来版を維持した | 単独/race/総合テスト・6対象cross-compileが成功、独立した通常コードレビュー完了 |
| 2：NOEの利用先の切替（完了） | stdlibの導入、loop、Workerを取り込んだ版へ揃えた。機能追加・API名変更は含まない | 4モードのAPI比較、関連/全体/race/E2E、6対象のビルドと統合test compileが成功。独立した通常コードレビュー完了 |
| 3：`noe:http`の入口（完了） | 既存と同一の`fetch`・`serve`を公開し、英文の内蔵docsと利用例を整備した | 各Worker/agent、旧API比較、総合テスト・6対象compile・実providerのHTTP往復が成功、独立コードレビューと再レビュー完了 |
| 4：要求送信のストリーミング（完了） | `ReadableStream`を全量収集せず送信し、Request所有権・背圧・取消し・早期応答を整えた | EOF前到達、各Worker/agent、全体/race/E2E・6対象compile・実provider再試が成功。独立コードレビューと再レビュー完了 |
| 5：要求受信のストリーミング（完了） | headers後にhandlerへ渡し、需要に応じて本文を逐次読取り。早期応答・Expect・終了回収を整えた | EOF前dispatch/受信、各Worker/agent、全体/race/E2E・6対象compile・実provider基本フローが成功。独立コードレビューと再レビュー完了 |
| 6：NOE独自APIの導入方法の整理（完了） | 内部の組立packageでstdlib/nativeio/docs・設定複製・共有出力・Workerを接続し、NOE自身も切替 | CLI/agent非依存のGo例、API比較、全体/race/E2E・6対象compile・実provider限定再試が成功。独立コードレビューと再レビュー完了 |
| 7：外部向けGoライブラリの提供（7.1・7.2実装/検証完了） | 正規/v2 module・公開stdlib/agent package・設定・外部consumerを整備。NOE自身も共通bridgeを利用し、明示NOE processとlifetimeを接続 | 別Go moduleから汎用APIとagentを選択して組み込める。v2公開・常用の安定性判断は別途 |

実装済み段階の進め方として、最初の作業単位は第1・第2段階までとし、「NOEが管理するstdlibで従来どおり動く」を一つの到達点にする。切替では既存テスト、HTTP・WebSocket・Worker・終了処理、配布対象のビルドとdocsの由来・版情報を確認する。その後、第3～第5段階をHTTP整備のまとまりとして進める。既存APIの契約とNOE側の拡張を整理し、Web型・loop・HTTP・WebSocketの共通実装を活用する。全二重通信や詳細なtimeout設定は、基本的な要求ストリーミングとは分けて仕様を決める。

ステップ1は[固定版stdlibの取り込み計画](development/stdlib-step1-import-plan.md)に沿って3スライスを実装し、[独立した通常の計画/コードレビュー](development/stdlib-step1-import-review.md)を完了した。いずれもP2以上の指摘は0件。[実装・検証記録](development/stdlib-step1-import-implementation.md)と[構造化結果](development/stdlib-step1-import-results.json)を保存している。取り込んだ41ファイルのうち30件が原本と一致し、11件の適合変更とローカル追加2件に限定した。NOE本体の切替は含めず、上流stdlibを使うCLIの依存境界と既存runtime・配布バイナリの維持も確認した。

ステップ2は[runtime切替計画](development/stdlib-step2-runtime-plan.md)の4スライスを実装し、[独立した通常の計画/コードレビュー](development/stdlib-step2-runtime-review.md)を完了した。いずれもP2以上の指摘は0件。productionと統合helperをローカル版へ揃え、内蔵英文docsと現行ガイドの出自説明を更新した。変更前後の4モードAPI inventoryは完全一致し、取り込み版を使うCLI・Worker・agent・`run_js`の総合検証も成功した。追加のOpenCode Go実接続でも、agent起動・`run_js`によるJSON集計と保存・最終回答の基本フローを確認した。[実装・検証記録](development/stdlib-step2-runtime-implementation.md)と[構造化結果](development/stdlib-step2-runtime-results.json)を保存している。

ステップ3は[noe:http入口計画](development/stdlib-step3-http-plan.md)の3スライスを実装し、[独立した通常の計画/コードレビューと再レビュー](development/stdlib-step3-http-review.md)を完了した。コードレビューのdocsに関するP2を解消し、最終P0〜P3は0件。既存と同一のfetch/serveを公開し、Web型・WebSocket・loop・終了処理を共有する。設定・4モード・旧API比較、英文docsと例、関連/総合テスト、6対象のbuild/compileが成功した。OpenCode Go実接続の1課題でもdocsを自発参照し、新入口でのHTTP往復・保存・close・最終回答を確認した。[実装・検証記録](development/stdlib-step3-http-implementation.md)と[構造化結果](development/stdlib-step3-http-results.json)を保存している。要求送信・受信のstreamingはステップ4・5へ残す。

ステップ4は[要求送信streaming計画](development/stdlib-step4-upload-plan.md)の4スライスを実装し、[独立した通常の計画/コードレビューと再レビュー](development/stdlib-step4-upload-review.md)を完了した。コードレビューで見つかったpre-abort時の本文lockのP2を解消し、最終未解消P0〜P3は0件。EOF前到達、Request所有権・背圧・取消し・redirect・早期応答、英文docsと各実行経路、全体/race/E2E・6対象compileが成功した。OpenCode Go実接続も課題の並行手順を明確化した再試で送信・保存・最終回答を確認し、docsを自発参照した。[実装・検証記録](development/stdlib-step4-upload-implementation.md)と[構造化結果](development/stdlib-step4-upload-results.json)に初回失敗も保存している。serveの受信buffering改善はステップ5へ残す。

ステップ5は[要求受信streaming計画](development/stdlib-step5-receive-plan.md)の4スライスを実装し、[独立した通常の計画/コードレビューと再レビュー](development/stdlib-step5-receive-review.md)を完了。Expect tokenのP2と需要計数のP3を解消し、終了テストの総合確認後の修正も限定再確認済みで、最終未解消P0〜P3は0件。EOF前dispatch/受信、背圧・早期応答・取消し/終了、英文docs/例と各実行経路、全体/race/E2E・6対象compileが成功した。OpenCode Go実接続1課題でもdocsを自発参照し、受信・保存・close・最終回答を初回で確認した。[実装・検証記録](development/stdlib-step5-receive-implementation.md)と[構造化結果](development/stdlib-step5-receive-results.json)へ環境上の失敗と復旧も保存している。API登録・資源管理境界は次のステップ6で整理する。

ステップ6は[API導入と所有関係の整理計画](development/stdlib-step6-composition-plan.md)の4スライスを実装し、[独立した通常の計画/コードレビューと限定再レビュー](development/stdlib-step6-composition-review.md)を完了。計画のProcess前提不足とコードのwriter動的比較のP2を解消し、最終未解消P0/P1/P2は0件。CLI/agent非依存の内部組立package・Worker/DNSと資源所有・NOE切替・英文Go例を整え、API/CLI比較、全体unit/race/E2E/vet・6対象build/compileが成功した。実プロバイダの同じ基本課題は初回のNode API仮定で未完了後、noe:http指定の限定再試でdocs自発参照・HTTP往復・保存・終了・最終回答を確認した。[実装・検証記録](development/stdlib-step6-composition-implementation.md)と[構造化結果](development/stdlib-step6-composition-results.json)に両試行を保存。公開Go package化はステップ7へ残す。

ステップ7は[外部Goライブラリ提供計画](development/stdlib-step7-library-plan.md)を作成し、[独立した通常の計画レビュー](development/stdlib-step7-library-review.md)を完了。初回のP0〜P3は0件。7.1をmodule移行・公開facade・外部consumer/artifact・NOE切替/英文ガイド・総合確認の5スライスに分割し、7.2のagent接続は汎用公開の成立後に別計画とする。その後の追加依頼で7.1の5スライスを実装し、独立コードレビューのP2以上は0件、全体unit/race/E2E/vet・外部consumer/local artifact・6対象compile・実プロバイダ基本フローが成功。[実装・検証記録](development/stdlib-step7-library-implementation.md)と[構造化結果](development/stdlib-step7-library-results.json)へ保存。7.2は[agent外部接続計画](development/stdlib-step72-agent-plan.md)を6スライスで作成し、[独立した通常レビューと限定再レビュー](development/stdlib-step72-agent-review.md)を完了。公開設定にregistryで無視されるProfileを含めたP2を解消し、最終P0〜P3は0件。利用者の追加依頼により7.2の6スライスも実装・検証完了。独立コードレビューでlive consumerの完了判定に関するP2を修正し、限定再レビューでP0〜P3は0件。全体unit/race/E2E/vet・外部consumer/local artifact・6対象compile・実provider基本フローが成功。[実装記録](development/stdlib-step72-agent-implementation.md)と[構造化結果](development/stdlib-step72-agent-results.json)に保存した。v2公開・常用の安定性判断は未実施。

## 置き換えられた判断（履歴）

### <履歴・完了> 自作REPLの必要性とqjsの再利用

v1.8.0時点では固定版go-quickjsのqjs由来の入力・行編集ソースを取り込み、評価先にNOEのruntimeを使っていました。当時の利用説明は[タグ付きv1.8.0のREPLガイド](https://forge.harakara.site/littleisland/noe/src/tag/v1.8.0/docs/repl.md)、統合経緯は[archive記録](archive/qjs-repl-integration.md)に残しています。2026-10-19の削除計画でこの決定は置き換えられ、現行のREPL利用・保守対象ではありません。[削除計画](development/repl-removal-plan.md)。

<a id="inventory"></a>

## 分類・照合の記録

以下は移動元に記載していた棚卸しの経緯です。今回の分離で全機能を再検証したものではありません。

2026-10-21にソース（照合開始時 `0f07f2f`）・関連テスト・利用ガイド・v1.16.0までのリリース記録を棚卸しし、その後の実装を追記しています。
2026-10-24に `1bad24a` を基準として、公開Go API、agentの同一プロセス内実行、stdlibの公開状況、実行結果と出力予算を再照合しました。今回は関連コード・利用ガイド・v2.4.0までのリリース記録の選択的な確認であり、全候補や外部サービスの最新状況を再調査したものではありません。
`<完了>`は合意した範囲を実装済み、`<一部完了>`は実装済み部分と残る候補がある状態、`<未完了>`は検討・未着手を表します。未完了は実装の決定や優先順位を意味しません。

上記の棚卸し時点では、テストコードと既存の検証記録を読みましたが、テストの再実行・実プロバイダー接続・新しいリリースは行いませんでした。同日のv2実装と検証は、該当完了項目と実装記録へ別途反映しています。2026-10-24の再照合も文書整理であり、製品コード変更・テスト再実行・実プロバイダー接続・リリースは行っていません。本文の過去の検証結果と、今回確認したコードの存在は区別します。実装済みの範囲、公開状況、保留中の拡張も分けて記載します。

現在の仕様は各利用ガイドを参照してください。実装前の案・旧API・当時の判断は[検討履歴](archive/future-candidates-history.md)へ保存しました。履歴にある未実装記述を現在の残件とは扱いません。

第3階層の候補見出し単位で集計し、Jevの応用例を1件として含めます。観察機能の内訳である「外部JS案」は別件に数えません。旧REPLの判断は履歴1件として集計外です。主な更新は、Zed環境を未完了から一部完了へ変更、NOE側の同期コマンド改善を上流PRから分離、再委譲制限と結果取得のv1.16.0公開状況の反映です。

2026-10-21の追加依頼で[TypeScript対応の検討メモ](future-candidates.md#typescript)を未完了候補へ追記し、件数へ反映しました。2026-10-24にZedの型宣言追随と再利用ガイドを完了し、一部完了から完了へ移しました。同日の再照合で、v2公開済みの[NOE版stdlib](#noe-stdlib)も完了へ移し、旧実行方式を現行の残件から除きました。
