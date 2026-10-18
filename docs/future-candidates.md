# NOE 次の機能候補

将来の機能を検討するためのメモです。v1.2.0利用後のagent関連の質問と、v1.0.0時点の候補を記録しています。起動・待受・queue・live観察は[起動と待受の計画](agent-lifecycle-plan.md)に沿って実装済みです。steer・明示interrupt・人間向けagent操作CLIも[実行中の操作計画](agent-control-plan.md)に沿って実装・検証済みです（[実装記録](agent-control-implementation.md)）。現行APIの利用方法は[APIリファレンス](api.md)を参照してください。以下の日付付き記述は実装前の候補・設計経緯や当時の旧API案であり、現行仕様の代わりにはなりません。

## v1.2.0利用後のagent検討メモ — 2026-10-16

観察・追加指示の主な利用者は親agentを想定する。「親が子を起動→実行状況を確認→必要なら子へ追加指示→結果を受け取る」を自律的に行える形を検討する。人間も同じ経路を使えるが、人間の操作を前提にはしない。

### <一部完了> 汎用の低レベルAPI拡張と優先順位 — 2026-10-18

**目的：プロセス・I/Oなどの汎用機能をNOEの拡張として提供し、その上でMCP、LSP、常駐ワーカー、監視・対話スクリプトをJSで組み立てられるようにする。** Go側はプロセスやバイト列の入出力を担当し、プロトコルの解釈や要求・応答の対応付けはJS側に置く。第1グループは`noe:process`と`noe:io`として実装した。公開契約は[利用ガイド](process-io.md)を参照。

まずNOEの拡張として検討し、go-quickjs上流への提案・採用を前提にしない。外部ESM、シェル・OSコマンド、外部ライブラリのNOE向け改修も利用できる。既存APIとJS・CLIで実現できる処理をGo必須とは扱わず、汎用APIを追加する価値と実装範囲を比較する。

元のC製QuickJSの`std`・`os`と、NOEが利用するgo-quickjs v0.27.0のstdlibを比較し、レビュアーによる必要性・重複・実装範囲の確認を反映した優先案：

| 優先順位 | 候補 | 用途と追加する価値 |
| --- | --- | --- |
| **1：まず取り組む** | **子プロセスの操作ハンドル・実行中の入出力ストリーム、ホスト標準I/Oのストリーム化** | 子のstdinへ継続的に書き、stdout・stderrを逐次読み、終了待ち・停止を操作する。MCPクライアント、LSP、常駐ワーカー、ビルド監視に共通して使える。ホスト標準入力の逐次読取りは、JS製stdioサーバーやJSONLフィルターにも有用。 |
| **2：具体的な用途に合わせて追加** | **ファイルハンドル、OSシグナル受信、TCP・Unixドメインソケット** | 同じファイルを開いたままランダムアクセス・部分更新する処理、JSで記述する終了処理・設定再読込み、任意プロトコルやローカルIPCとの通信。それぞれ独立して検討・追加できる。 |
| **3：後続候補** | **ファイル変更通知、端末操作** | 自動再実行・変更監視、対話的なJSツール。変更監視はJSのポーリングや外部CLIとも比較する。TTY判定・サイズ取得、raw mode、子プロセス用PTYは別機能として評価する。 |

既存機能との違いと、レビューで補正した点：

- `execFile()`自体は既に非同期である。追加価値は、実行中の入出力と終了観察・停止を一つのハンドルで扱えること。
- ファイルの逐次I/Oも`createReadStream`・`createWriteStream`で利用でき、読取りには開始・終了位置の指定もある。ファイルハンドルは、同じハンドルでの位置指定・部分更新・継続利用を補う候補。
- OSシグナル受信は子プロセス停止の前提ではない。現在もCtrl+Cによる実行取消しはあるため、JSハンドラーへの配送は第2グループの別機能とする。
- 公開APIはハンドルとWeb Streamsを中心に検討する。元QuickJSのfd番号・`dup`・`dup2`・readiness callbackをそのまま公開要件にはしない。JSONLの行分割やMCPの解釈はJS側で実装できる。

第1グループでは、開始失敗、終了Promise、stdinを閉じた際のEOF、出力EOFとプロセス終了の関係、停止対象を公開契約として整理した。ホスト標準I/Oは借用品で、REPLのstdinはソース入力専用のため、子プロセスが所有するpipeと分けて、読取りの取消し・close・入力の所有者を実装した。シグナル受信を追加する際は、既存のCtrl+Cによるcontext取消しとJSへの配送の関係を整理する。

第1グループは[実装計画](process-io-plan.md)に従って実装・レビュー・検証を行った。[実装記録](process-io-implementation.md)を参照。第2・第3グループは未実装で、全項目の実装を決定したものではない。2026-10-18の追加指定により、今回の計画ではstdlibのAPI拡張・置換・修正を範囲外とし、NOE拡張と既存NOE機能への接続に限定する。

### <未完了> NOEへのMCPサーバー機能の組込み — 2026-10-17

**検討事項：NOEでMCPサーバーを動かし、外部のMCPクライアントからNOE上の処理を呼び出せるようにする。可能ならJavaScriptで実装する。** スクリプトでtoolの定義と処理を記述し、通常のJS処理やagentの機能を公開する利用例を候補とする。

まず、既存のMCP SDKをgo-quickjs上で利用できるか、NOEの入出力・HTTP機能で必要な通信を扱えるかを確認する。JSだけで不足する部分はGoホストで補う構成も候補とする。通信方式、JS API、起動方法、公開する機能の範囲は今後検討する。

現時点は検討メモで、実装・SDK選定・プロトコル互換性の確認は未実施。

### <未完了> ZedでのNOEスクリプト開発環境の整備 — 2026-10-17

**目的：ZedからSSHでVM上のJavaScriptを編集し、NOE固有APIの補完・実行・agentの観察と追加指示まで行える開発環境を整える。** 実行するスクリプトは通常のJavaScriptのままとする。

整備するもの：

- **型定義**：`noe:agent`・`noe:decision`などのNOE固有APIと、NOEが提供するgo-quickjs stdlib・グローバル関数を`.d.ts`で記述する。設定項目、戻り値、説明を補完やホバーで確認できるようにする。量に応じて`agent.d.ts`・`decision.d.ts`・`stdlib.d.ts`などへ分ける。
- **実環境に合ったstdlibの定義**：本家QuickJSの`std`・`os`とgo-quickjs stdlibを混同せず、採用版で実際に使えるAPIを定義する。Node.js用の型をそのまま流用して完全互換に見せず、たとえば`execFile`がPromiseを返す点も反映する。`console`・`fetch`・`serve`なども対象候補とする。
- **JavaScriptプロジェクトの設定例**：リモート側のスクリプト用ディレクトリに`jsconfig.json`と型定義を置き、ZedのJavaScript言語サーバーに読み込ませる。型定義のために実行コードをTypeScriptへ変更する必要はない。配布・配置方法は整備時に決め、他のエディターからも再利用できる形にする。
- **Zedの実行タスク**：同じリモート側プロジェクトの`.zed/tasks.json`に、編集中のJSを保存し、プロジェクトルートをcwdとして`noe run --observe`で実行する設定例を用意する。必要なら`--events=path`も併用し、読みやすい進行表示と元のJSONL保存を両立させる。
- **ターミナルとtmuxの使い方**：利用者はリモートターミナルから既存のtmuxを使う方針。Macでは`⌘⇧P`から`workspace: new center terminal`を選ぶと、ターミナルをファイルと同じ中央のタブで開ける。そこでtmuxに接続し、NOEの実行・観察を行う。Codex CLIを使う場合もVM側のtmuxから起動する。
- **ファイルに書いた追加指示**：ZedでMarkdownを編集・保存し、同じVMのターミナルから`noe steer RUN_ID "$(cat instruction.md)"`、`noe queue AGENT_ID "$(cat next-task.md)"`で送る運用例を用意する。steerは実行中taskのrun ID、queueは`keepAlive:true`のagent IDを指定する。現行CLIに専用の`--file`やstdin入力指定はなく、この例はシェルがファイルを読む。

まず型定義・`jsconfig.json`・Zedタスク設定と短い利用ガイドを用意する案とし、Zed専用拡張の作成は必要になった段階で検討する。現時点は検討メモで、これらの設定や型定義の作成・導入は未実施。

参照：[ZedのJavaScript/TypeScript言語サーバー](https://zed.dev/docs/languages/typescript)、[SSHでのリモート開発](https://zed.dev/docs/remote-development)、[Tasks](https://zed.dev/docs/tasks)、[Center Terminal](https://zed.dev/docs/terminal#terminal-panel-vs-center-terminal)。

### <一部完了> go-quickjs stdlibとNode.jsのAPI差異の説明整備 — 2026-10-17

README編集の実試行で、agentがNode.jsでは一般的な`promisify(execFile)`を使い、NOEの実行環境とAPI契約が合わない事例があった。採用中のgo-quickjs v0.25.3では、`node:child_process`の`execFile`は最初からPromiseを返し、`const {stdout, stderr} = await execFile(command, args)`で結果を受け取る。callback形式を前提に`promisify`する必要はない。この差はJavaScript言語やQuickJS-NG本体ではなく、go-quickjsのstdlibが提供するAPIの差である。

確認した上流資料では、`docs/stdlib.md`は提供関数の一覧、[CommandsのAPI説明](https://pkg.go.dev/github.com/go-quickjs/go-quickjs/stdlib#Commands)は直接`await execFile(...)`する例を示している。ただし、Node.jsのcallback形式との非互換や`promisify`が不要であることを明示する説明は見当たらなかった。使用例と実装から違いを読み取る必要がある。

整備候補は、NOEの利用者向け資料・モデル向けinstructionsに実行環境と代表的なAPI差異、正しい短い使用例を示すこと、およびgo-quickjs側のJS API資料・Node.js互換性の説明を補うこと。差異をすべて列挙することを前提にせず、実際に誤用が起きた箇所から必要な説明を検討する。現行instructionsには`execFile (Promise)`の記載があるが、今回の誤用を避けるには説明が足りるか見直す余地がある。このメモでは文書整備や上流への提案の実施までは決めない。

### <未完了> go-quickjsへの上流PR案：同期コマンドの失敗情報保持 — 2026-10-17

go-quickjs v0.25.3の`execFileSync`はコマンドのstdout・stderrを内部で捕捉するが、失敗時にはそれらをErrorへ保持せず返すため、NOE側で未処理例外の詳細を保存しても出力を復元できない。非同期の`execFile`は、失敗時のErrorに`code`・`stdout`・`stderr`を既に付けている。

上流PRの候補として、`execFileSync`も失敗時のErrorへ`code`・`stdout`・`stderr`を付ける改善を検討する。正常時の戻り値・呼出し方と既存のエラーメッセージを維持し、失敗するコマンドの終了コードと両出力を取得できるテストを添える。`execSync`等の同期経路への適用範囲は、PR作成時に実装を確認して決める。NOE固有の回避策ではなく、stdlib利用者全体の原因調査を助ける改善として提案する。現段階は検討メモのみで、上流の実装修正・PR作成・提出は行わない。

### <未完了> Web検索と検索APIの認証ラッパー — 2026-10-17

現在、NOEのagentに専用のWeb検索APIはない。既知のURLは`run_js`内の`fetch()`で取得でき、外部の検索APIも呼出しコードと認証を用意すれば利用できる。検索API向けの組込みラッパーと認証管理は未実装。

検討する構成は、検索APIへの認証と通信をGoホスト側で扱い、`run_js`から使える薄いJSラッパーを提供する形。モデルに公開するtoolは`run_js`一つのままとし、モデルがAPIキーや検索provider固有の認証手順を直接組み立てなくて済むようにする。APIキーは利用者が所定の場所へ登録し、ホストが読み取って要求へ付与する。

検索結果はタイトル・URL・要旨などを構造化して返し、agentが必要なページを選んで取得・確認できるようにする。失敗もモデルが理解して次の処理を判断できる形で返す。検索provider、JS API名、引数・戻り値、キーの保存先は実装時に検討する。このメモでは実装・API・providerの採用は決めない。

**追加メモ — 2026-10-17：Exaを使ったWeb検索サンプルを作る。** NOEから検索クエリを送り、検索結果を取得・表示する利用例を用意する。

### <一部完了> OpenCode Go APIの組込み — 2026-10-17

**検討事項：OpenCode Go（`opencode-go`）をNOEのagent用provider候補に加える。** APIの全体像が分かりにくいため、利用可能な形式と接続条件を確認してから設計する。ここでのGoはサービス名。

2026-10-17の[公式Goドキュメント](https://opencode.ai/v2/docs/console/go/)では、契約後にAPIキーを取得でき、外部のcoding agentから利用するためのAPIも公開されている。モデルによってAPI形式が異なる。

| 形式 | 公式に掲載された送信先 |
| --- | --- |
| OpenAI互換Chat Completions | `https://opencode.ai/inference/go/openai/v1/chat/completions` |
| Responses | `https://opencode.ai/inference/go/openai/v1/responses` |
| Anthropic Messages | `https://opencode.ai/inference/go/anthropic/v1/messages` |

モデル一覧は`https://opencode.ai/inference/go/v1/models`。公式のDeepSeek V4.1 Flashのmodel IDは`deepseek-v4.1-flash`で、Chat Completions経路に掲載されている。旧ドキュメントや検索結果には`/zen/go/v1/...`も残っているため、実装時には公式の現行送信先を再確認する。

公式はcoding agentとしての通常の通信、クライアント自身を示すUser-Agent、会話ごとに安定した`x-opencode-session`を送るよう案内している。NOE側でrun ID・cacheKey等との対応を決める。APIキーの読込みと付与はGoホストで扱う構成を候補とする。

当初はAPIの確認のみ。2026-10-17、利用者の許可でhjh側のキーを使い、既存のOpenRouter Go SDKをOpenCode Goへ接続する[スパイク](opencode-go-sdk-spike.md)を実施した。Chat通常応答・NOEのJS tool往復・Responses通常応答・streamingが成功。`User-Agent: noe/<version>`と同じrunで固定した`x-opencode-session`を付与し、親子別IDもローカルで確認した。

その後、利用者の指示でChat・Responsesに範囲を絞り、本体providerと[組込みデフォルト＋外部JSON上書き](providers.md)を実装した。OpenCode Goの`reasoning_content`の連結・履歴保持・観察、Responsesのnative reasoning/tool履歴、実run IDのヘッダへの接続に対応。親子の別ID・tool往復・取消しはローカルfixture、Chat・Responsesの通常agent起動は実接続で確認した。[実装・検証記録](provider-config-validation.md)。Messages形式と利用枠に達したときの実応答の確認は今後の候補とする。

### <未完了> Worker内からの`noe:agent`利用

**質問：Workerからagentを呼べるようにする必要性はあるか。**

現状はmain runtimeでのみ対応し、Workerからの`noe:agent` importは未対応。`run_agent()`自体は非同期で、複数runは`Promise.all`で並行実行できる。生成JSも呼出しごとに別のNOEプロセスで動くため、通常の親子agentやファイル編集のためにWorker対応を追加する必要はない。

必要性が生じる例は、Workerで重い計算やファイル解析を行う既存の処理があり、その途中から直接agentを呼びたい場合。現状でもWorkerからmainへメッセージを送り、main側でagentを呼んで結果を返す構成は取れる。

当面は現在の構成を使い、mainへの中継が実際の不便になった時に検討する。対応する場合は、Workerのmodule登録、認証・brokerへの接続、Worker終了時のrun取消し・回収をどこが所有するかを決める。Workerの並行実行と、モデルが判断する子agentへの委譲は別の仕組みとして扱う。

### <完了> 親agentによる子agentの監督

**現行実装：** `run_agent`・`spawn_agent`・`observe_agent`・`queue_agent`で単発task、待受instance、live snapshot、FIFO queueを利用できます。同じinstance内のqueueは新しいrun IDを持ちますが、前taskの最終回答を含むtrim後の確定済み会話から続きます。開始したqueue指示はuser messageとして保護し、trim回数もinstance内で続けます。`steer_agent`・`interrupt_agent`と人間向けCLIから実行中の指示追加・取消しも利用できます。instanceは親runに所有されます。引数・戻り値・寿命は[APIリファレンス](api.md#自律agentopenrouter)を参照してください。以下は実装前の検討履歴であり、候補表と当時の未実装記述は現行API契約ではありません。

**主なユースケース：親agentが子agentを起動し、処理中の様子を確認して追加指示する。**

`run_agent(...)`は結果が必要な単発taskに使い、instance停止まで待ちます。子の完了前に状態を見て次の判断をしたい場合は、`spawn_agent(...)`でIDを取得し、後続の`run_js`から`observe_agent({agentId})`を呼びます。追加taskは`queue_agent(...)`で投入できます。JSのPromise並行処理と、親モデルへ途中経過を返して判断させる流れは分けて考えます。

必要な操作の候補は「開始してIDを返す」「状態・新しいイベントを取得する」「追加指示を送る」「最終結果を取得する」。親は各操作を既存のrun_jsから呼び、モデルが確認結果を受け取って次の操作を選べる。モデルへ公開するtoolをrun_js一つとする方針は維持できる。操作名・公開APIは未決定。

利用者の追加意見を受け、**共通の実行管理APIを核にして、親agent用のJSラッパーと人間用のCLIを用意する構成**を主案にする。Go側の既存brokerを拡張し、実行状態・イベント列・指示の受け口・最終結果を一か所で管理する案。親と人間が同じ実行IDに対して同じ状態・イベントを取得できるようにする。

| 操作の候補 | 用途 |
| --- | --- |
| spawn_agent | agentを開始し、既定では完了を待たずrun IDと状態を返す。`wait: true`なら最終結果まで受け取る。通常JSから親runを、親のrun_jsから子runを起動できる形にする |
| observe_agent | run IDを指定し、現在の状態・差分イベント・確定済みの最終回答や失敗情報を取得する。`wait: true`で完了まで待つ |
| send | 対象runへ追加指示を投入し、指示IDと受領状態を返す |

`spawn_agent`・`observe_agent`を検討上の名称として採用し、`send`等は仮称とする。具体的な引数や公開箇所は未確定。親はrun_js内でJSラッパーを呼び、構造化した戻り値をモデルへ返す。人間は状態表示やイベント追跡のCLIから同じAPIを呼ぶ。利用可能な実行の一覧や接続先の発見、既存のbroker接続情報・実行scopeとの対応も設計時に決める。

状態確認と最後の回答の取得は`observe_agent`に統合する。既定では現在取得できる情報を返し、完了済みなら最終回答も含める。`wait: true`なら完了まで待って最終結果を返す。親に他の仕事がある間は非待機で観察し、結果が必要になって他の仕事がなければ待機することで、完了したかを繰り返し問い合わせる必要をなくす。別の`collect_agent`は設けない方針。観察は既存JSONLを読むGo実装とJS入口を先行して追加した（[使い方](observe.md)）。`spawn_agent`と親による子の途中監督は未実装。

`spawn_agent`も起動時に完了待ちを選べる方針を採用する。`wait`の既定は`false`で、起動後にrun IDと状態を返す。`wait: true`では起動から完了待ちまでを一度の呼出しで行い、run ID・終了状態・最終回答または失敗情報を返す。この場合、最終結果のために改めて`observe_agent`を呼ぶ必要はない。非待機で起動した後、後続の`observe_agent({runId, wait: true})`で待機へ切り替えることもできる。

| 呼出し | 戻るタイミング・受け取る情報 |
| --- | --- |
| spawn_agent（wait省略またはfalse） | 起動後にrun IDと状態を返す |
| spawn_agent（wait:true） | 完了後にrun ID・終了状態・最終回答または失敗情報を返す |
| observe_agent（wait省略またはfalse） | 現時点の状態・差分を返し、完了済みなら最終結果も含める |
| observe_agent（wait:true） | 完了まで待って最終結果を返す。完了済みならその結果を返す |

ここでいう同期・非同期は「呼出し側がagent完了まで待つか」の違いを指す。JS APIとしてはどちらもPromiseを返す非同期関数を想定し、runtimeの他の処理を同期的にブロックする意味ではない。引数全体の構成や正確な戻り値schemaは設計時に決める。

2026-10-17、利用者の指示でhjhの現行source（HEAD `77e33c4`）を確認し、仮称の`status_agent`・`result_agent`を、hjhの`agent_status`・`collect_agent`に合わせた。hjhでは`spawn_agent`は起動後に戻り、`agent_status`は完了を待たず状態・報告・nextCursorを取得し、`collect_agent`は子の終了と結果の保存を待って最終結果を返す。JS側の別名は`api.agents.spawn`・`api.agents.status`・`api.agents.collect`。参照：[help](../../hjh/internal/extensions/assets/child-agents-help.md)、[tool定義](../../hjh/internal/extensions/assets/config.json)、[JS入口](../../hjh/internal/jsruntime/bootstrap.js)、[Go側の呼出し](../../hjh/internal/jsruntime/agent_control.go)。

hjhを確認した後、NOEでは状態と結果を一つの入口へまとめ、待機をオプションで選ぶ案を利用者が採用した。`agent_status`・`collect_agent`はhjhの参照名として残し、NOEの採用名は`observe_agent`とする。

通常のJSからも同じ`spawn_agent`で親agentを起動できる形にする。親子という固定のagent種別を設けず、呼出し元に親runがあるかで関係を決める。通常JSから親A・親Bを起動した場合は別々のrun IDを持ち、どちらのparentRunIdも空になる。親Aのrun_jsから起動した子はparentRunIdに親Aのrun IDを持つ。親のない起動ではprovider・model・effortを明示し、親runからの起動では省略項目を親から継承する現在の設定方針を引き継ぐ。NOE終了後も継続する独立実行は別の検討項目とする。

生成JSは毎回別プロセスで、現在はその終了時に子scopeも回収する。この用途では、子の実行を一回のrun_jsより長く保持し、後続のrun_jsからIDで参照できる所有者が必要。親run側のbroker等が子の状態・寿命を管理する構成を検討する。単にawaitを外してJSを終了するだけでは成立しない。

まず親runが子を所有し、親の終了・取消しで子を回収する構成で監督できる。親終了後も子が続く完全な独立実行は、次項の別の寿命の選択肢として扱う。開始が戻っただけで子完了と扱わず、実行中/完了/失敗と結果を区別する。

### <未完了> 呼び出し元の終了後も続く独立agent実行

**質問：起動したagentを独立したプロセスとして動かし、呼び出し元のNOEが終了しても処理を続けられるか。**

実現可能だが、現在の`run_agent()`には独立実行APIはない。現在のrun・生成JS・子agentは親の実行scopeと取消しに結び付く。子agentのモデル通信も親brokerに依存するため、子プロセスだけを切り離しても親終了後の継続は成立しない。

想定する構成は、呼び出し元が専用のNOEプロセスを独立起動し、そのNOEが自身の認証・broker・agent loopと子プロセス管理を持つ形。呼び出し元は起動結果を受け取って終了でき、実行担当のNOEが処理を続ける。「NOEがなくなる」は呼び出し元の終了を指し、agentを実行するNOEプロセスは残る。

単に端末から離れて実行を続ける用途なら、現状でもOS側で`noe run ...`をバックグラウンド実行する方法がある。NOE内から仕事を切り離して起動したい場合に、組み込みの独立実行を検討する。

採用する場合は、起動口をCLIにするかJS APIにするか、cwd・設定・認証の受渡し、呼び出し元と独立した寿命、ログ・最終結果・終了状態の保存、実行IDと停止方法を検討する。会話の永続化・中断後の再開・常駐daemon・再起動後の復旧まで必要かは別途判断し、独立実行と一括で必須にしない。会話中の`agent.start()`はAPI案であり、名称・戻り値とも未決定。

<a id="user-instructions"></a>

### <完了> ユーザー共通インストラクションの既定読込み — 2026-10-18

**現行実装：** 各agent instanceの最初のtask開始時に共通設定とworkspaceのMarkdownを一度読み、組込み指示と明示した`system`へ追記してinstance内で固定します。同じinstanceの次queue taskは再読込みせず、別のchild・agent instanceは自分の初回taskで新しく読みます。workspaceはNOE起動時の作業ディレクトリです。[使い方](instructions.md)・[実装と検証](instructions-implementation.md)を参照してください。

**利用者の希望：共通の設定ディレクトリとworkspaceの両方からMarkdownファイルを既定で読み込み、agentの不変インストラクションへ追加する。** ライブラリの利用案内などを置き、各JSで毎回`readFile`と`system`への設定を書かずに使えるようにする。共通側の基本パスは`~/.config/noe/instructions/`。会話中の`~/.congig/noe/instraction/`は、このパス表記へ補正して記録する。

**設定ディレクトリの解決は、既存のクレデンシャル探索と同じ挙動にする。** `XDG_CONFIG_HOME`が空でなければ`$XDG_CONFIG_HOME/noe/instructions/`、空または未設定なら`~/.config/noe/instructions/`を使う。`~/.config/noe`を固定パスとして扱わず、実装時は既存の設定ディレクトリ解決と整合させる。

**workspace側は`<workspace>/.noe/instructions/`を読む。合成は追記型で、共通側→workspace側の順に本文を連結する。** 各ディレクトリ内の`.md`をファイル名の昇順で読み込む。同名ファイルでも共通側を置換・除外せず、両方の本文を追加する。既存のクレデンシャルにはworkspace自動探索を追加せず、ここではインストラクションの読込みを対象とする。

**既定の`instructions/*.md`は自動で必ず読み込み、任意ファイルを明示的に追加する既存の`system`経路も併用できるようにする。** `system`自体は文字列なので、複数ファイルは呼出し元JSで`readFile`し、指定順に本文を結合して渡せる。ファイルパス配列を直接受け取る新APIは、このために必須とはしない。

追加する指示の結合順は、**共通instructions → workspace instructions → 明示した`system`**とする。明示した`system`によって自動読込み分を置換・省略しない。これは本文の追記順を定めるもので、前の本文を機械的に書き換える上書き処理ではない。

既存の外部インストラクションの合意に従い、run開始時の内容を固定し、そのrun中は再読込みしない。将来の実行コンテキスト圧縮でも本文を保持する。workspaceはNOE起動時の作業ディレクトリとし、祖先・Git rootは探索しない。組込みinstructionを先頭に保持し、その後に上記の追加指示を結合する。ディレクトリがなければ追加なしで進み、選択したファイル等を読めない場合はprovider呼出し前のtask失敗としてパス付きで報告する。

### <完了> 簡易的な実行コンテキスト管理

**現行実装 — 2026-10-18：** `contextPolicy`による要求直前の自動切り捨てと使用量／予算通知を実装。元のsystem・task・適用済みsteerと直近の完了blockを保護し、古いtool往復は分断せず削除する。同じkeepAlive instanceのqueueでは、trim後に残る会話・trim回数・直近のtrim情報を次taskへ引き継ぐ。保存・要約は必須にしない。総合回帰・実プロバイダの挙動確認は[実装記録](context-trimming-implementation.md)を参照。以下の「未実装」「未決定」は当時の検討履歴であり、現在の仕様は[hook・contextガイド](hooks.md)を正本とする。

**追加合意 — 2026-10-18：** 保存は任意で、必要な情報がなければ保存しない。保存専用turn・必須checkpoint・要約モデルを追加せず、使用量／予算と切り捨て方針を事前に通知する。切り捨て発動後は再推定した値と削減後の履歴を送る。window既知時は固定入力と応答予約を除いた残りの約90%を実行予算とし、未知かつ明示予算なしのNOE既定入力予算は1,000,000 tokensとする。これはモデルwindowの推測値ではない。Jevなど`noe:decision`の単発要求は切り捨て対象外。既存の入力全体の`contextBudget.inputTokens`と実行履歴だけの予算を区別する。[実装計画](context-trimming-plan.md)へsliceと検証方法を具体化した。以下の当初案・未決事項は、更新後の計画と区別する。

**質問：一回の長いタスクで膨らむ実行履歴を、費用と作業の継続性を保ちながら圧縮できるか。**

対象は一つの`run_agent()`内のモデル判断・tool実行の反復。人間との複数回の会話入力や、runをまたぐ会話保存は現段階で想定しない。現在のloopは最初の指示にassistant/toolメッセージを追加し、次の要求へ履歴全体を送るため、長いタスクで入力が増え続ける。

追加の検討事項として、**呼び出し時に、そのrunで不変として保持するインストラクションを指定できる形**を設ける。不変のインストラクションには、方針・制約に加え、そのタスクの指示文も含める。モデルへの入力を「不変のインストラクション（指示文を含む）＋実行コンテキスト」として管理し、実行履歴を切り詰めても元の指示を失わないようにする。

現在も`run_agent(config, instruction)`で初期の指示を渡せるが、ここでの不変性はコンテキスト管理上の区分を指す。systemメッセージだけを固定扱いにする意味ではなく、元のタスクを渡すuserメッセージも含む。呼び出し時の公開引数や既存optionとの対応は未決定。不変部分はrun中に書き換えず、実行中の追加指示は後述の仕組みで受け取り、有効な指示として保持する案と両立させる。

**外部ファイルの扱いについての合意**：ファイルから追加するインストラクションは、run開始時に読み込んだ内容を固定し、そのrunでは再読込みしない。`AGENTS.md`も追加元として利用でき、内容そのものを不変部分へ保持する。ファイルのパスだけを残して指示本文を実行履歴とともに切り捨てる方式にはしない。外部ファイルをJSで読み、`system`へ渡す既存の経路を実装済みとして扱う。**`AGENTS.md`等の自動探索は現段階では追加しない。** 専用のファイル指定APIや複数ファイルの合成ルールは、必要になった時の追加候補とする。

外部ファイルを`fs.readFile`で読み、その文字列を`run_agent({provider, model, effort, system: rules}, instruction)`へ渡せます。これは共通instructionと結合してsystemメッセージへ入り、起動時に受け取った各taskで固定されます。ファイル内容をJS側で読み込むため、その内容を実行中に自動再読込みすることはありません。将来のファイル指定APIでrun開始時に読む仕組みとは区別します。**今後圧縮を実装する際も、`system`に渡した外部ファイルの全文と元のタスク指示を必ず要求に残し、圧縮するのは実行コンテキストだけ**とする。圧縮機能自体は現在未実装。

大きなwindowは利用可能な容量であり、毎回使い切る目標ではない。例えば100万tokensのwindowでも、費用と品質に適した通常の入力予算を別に持ち、必要に応じて古い履歴を圧縮する案を検討する。

履歴増加だけでキャッシュが使えなくなるとは限らず、先頭の一致部分は再利用されうる。ただしキャッシュ読取りにも費用が発生するモデルがあり、履歴全体を繰り返し送るコストは増えうる。圧縮でprefixを変えれば、その変更箇所以降のキャッシュを再構築する可能性がある。同じ`session_id`でも内容の一致は別条件。[OpenRouter prompt caching](https://openrouter.ai/docs/guides/best-practices/prompt-caching)・[DeepSeek context caching](https://api-docs.deepseek.com/guides/kv_cache/)を参照（2026-10-16確認）。

利用者の追加意見を受け、初期案は**要約を生成せず、古い実行履歴を要求から外して直近を残す方式**とする。100万tokensの履歴でも1/10なら10万tokens残るため、まずその量の直近情報で作業を続けられるかを確認する。意味的な要約を必須にせず、切詰めと必要時のファイル参照で始める。具体化した既定値・予算計算・受入条件は後続の[承認済み実装計画](context-trimming-plan.md)を参照し、以下の数値や未決事項は当時の検討記録として読む。

```text
モデルへの入力：不変のインストラクション（指示文を含む）＋実行コンテキスト

要求の固定部分：共通instruction + 呼び出し時の不変のinstruction（方針・制約・タスク指示文）+ tool定義
実行コンテキスト：必要なら退避ファイルの参照 + 直近の判断・観察・tool実行と結果
任意のtmp退避：要求から外す履歴・大きなtool出力
```

- **不変のインストラクション**：呼び出し時に指定された指示文も含め、そのrunの間はそのまま要求に保持する。実行コンテキストの切詰め・要約によって削除・書換えしない。
- **実行コンテキスト**：実行中に増える判断・観察・tool実行と結果。古い完了済みの往復から要求対象を外す。保持するtool callと対応する結果はセットにし、reasoning等のprovider拡張も含めて保持する往復を途中で壊さない。
- **任意の退避**：再参照できるようにしたい履歴は、切り捨てる前にrun単位のtmpファイルへ書き出す。要求にはpathと対象区間などの短い参照情報だけを残す。必要になれば既存のrun_jsで必要な範囲を読み戻す。退避しなかった履歴は後から復元できない。

利用者の目安は、次の要求の入力見積りが設定した予算上限へ達する時に、実行コンテキストの可変部分を切詰め前の**1/5〜1/10（10〜20%）**まで減らすこと。直近側を残し、再び予算上限へ達するまでは追記だけにする。比率の対象から不変のinstruction等を除き、直近の完全なtool往復を残すための下限も考える。比率は最初の検証目安で、全タスクに十分な情報量とはまだ確認していない。

ここでの予算は1要求の入力token量の予算。不変部分と実行コンテキストの両方を入力量の見積りに含め、切詰めの対象は実行コンテキストだけにする。モデルのwindowには応答分と見積り誤差の余裕を別途確保する。JS側には現在もusageが返るため、直近のprompt token数と新規assistant/tool内容の増分を判断材料にできる。ただし文字数はtoken数そのものではなく、モデルwindow情報・次要求の見積り方法・単一の大きなtool結果の扱いは設計時に決める。

構造としては、モデル要求直前のhook（仮称`beforeModelRequest`）で「次の入力量を見積る→予算内なら続行→上限へ達するなら必要に応じてtmpへ退避し、古い履歴を外す→更新したコンテキストで要求」とする。切詰め自体にモデル要求を追加せず、loop側で実行できる。圧縮をモデルへtoolとして公開すると頻繁な呼出しが懸念されるため、初期案はloop内部のhookとし、利用者定義hookや専用toolの公開は別途検討する。

切詰め後もrunの`session_id`は維持し、親子それぞれが自分の履歴を管理する。tmpの保存先・寿命・後片付け、参照情報の形式は未決定。全文の退避は任意の作業データとして扱い、既定のメタデータtraceへ全文を追加する変更とは分ける。

比較案として、モデルが古い履歴を作業メモへ要約する方式と、OpenRouterの[context-compression plugin](https://openrouter.ai/docs/guides/features/message-transforms)がある。OpenRouterの仕組みはwindowに収まるまで中間メッセージを削除・短縮するもの。NOE側の入力予算と残す範囲を制御するhook方式を主案にし、単純な切詰めで以前の判断を忘れる・同じ作業を繰り返す等の具体的な問題が出た場合に、要約の価値を比較する。

採否は、同じ長いタスクについて、切詰めなし・切詰めのみ・tmp退避付き切詰めを比較して判断する。総費用（切詰め後のcache missや読み戻しを含む）、入力/キャッシュtoken数、所要時間、再読込み・作業の繰返し、最終成果・制約の維持を観測する。cache hit率や削減率だけを成功基準にしない。前後のtoken数・退避先・理由・回数はメタデータのtrace候補。入力予算の具体値・目標の選び方・直近履歴の残し方・見積りと発火の詳細・公開設定・既定値は未決定であり、実装は行っていない。

#### <完了> コンテキスト予算・使用量をagentへ通知する — 2026-10-18

**現行実装：** `contextBudget`、要求限定の標準通知、hookのcontext API、人間向けcontext行、JSONL/live/保存snapshotを実装した。実測・概算・未知値を区別し、通知を会話へ蓄積しない。[hookガイド](hooks.md)・[実装記録](execution-hooks-implementation.md)。続いて自動切り捨ても実装し、詳細は[切り捨ての実装記録](context-trimming-implementation.md)へ分離した。任意の履歴置換・意味的な要約は含まない。以下は実装前の検討履歴。

**検討事項：現在のコンテキスト予算と使用量をagent自身へ知らせ、残量を踏まえて作業を進められるようにする。** 長い自律実行で、agentが自分の履歴の増加を把握できるようにする。例えば、大きなtool結果をファイルへ保存して短い結果だけ返す、途中成果や継続に必要な情報を保存する、といった判断に使う。

通知する情報の候補は、モデルのcontext window、設定した1要求の入力token予算、直近の要求の実測入力token数、次の要求の入力token見積り、残りの入力予算・使用率。実測値と見積りは区別し、どの要求時点の値かを明示する。モデルのwindowとNOE側の入力予算は分け、応答用の余裕も考慮する。累積の利用token数や`maxTokens`の応答上限とは区別し、キャッシュされた入力もコンテキストの使用量へ含める。

モデルの次の判断へ動的な実行情報として渡す経路、`run_js`から取得する経路、観察イベントへの記録を候補とする。不変のインストラクションとは分けて扱い、親子それぞれのrunの値を通知する。予算や使用量を取得できない場合は未取得と分かる形にする。当初は通知の形式・頻度・公開APIを未決定としていた。通知のみを先行実装し、自動切詰めは含めない。

### <完了> 意思決定モデルの呼び出し

**現行実装：** `noe:decision`の`decide({provider,model,state,questions})`からOpenRouterのDecisions APIを利用できます。通常JS・agentの`run_js`に対応し、既存のGo側認証・SDK・Operationを共用します。Jev 1.14・Perplexity Decider V1.2・Clefの実接続を含め、実装・テスト・コードレビューを完了しました。[利用ガイド](decision.md)・[実装記録](decision-implementation.md)・[実接続記録](decision-openrouter-validation.md)。以下は実装前の検討履歴です。

**検討事項：Jevなどの意思決定モデルをNOEから呼び出せるようにする。参考実装は`projects/henji`の`henji decision`。**

2026-10-16、ローカルHenjiのREADME、設計文書、CLIと通信実装を確認した（HEAD `e5686d7`）。Henjiは通常生成とは別の専用Decisions APIを呼び、一つの判断対象と質問群を渡して、providerの応答JSON全体を返す。一つの対象について複数質問を一回の要求にまとめられ、判断結果に依存する次の処理や反復は呼び出し元が組み立てる。会話の継続やagent loopを伴う呼び出しではない。HenjiのOpenRouter用モデル例は`typesafe/jev-1.13`で、これは参照元の例でありNOEの対応モデルや最新モデルを確定するものではない。

NOEでの用途の例は、入力の分類、処理先・子agentの選択、成果物について条件を満たすかの判定。agentが必要に応じて既存の`run_js`内から意思決定用のJS APIを呼び、構造化した結果をその後の判断やJSの分岐に使う構成を検討する。モデルへ公開するtoolを`run_js`一つにする方針を保ち、通常のJSスクリプトからも同じAPIを利用できる形が候補。

認証・provider通信はGo側のラッパーに寄せ、JSへAPIキーを渡さない。OpenRouter用の既存の認証探索を利用できる構成を候補にし、通常のagent生成要求とDecisions要求の送信先・入出力を分けて扱う。既存`createAgent`のmodel名を変えるだけで対応できるとは扱わない。親runの取消し・終了時の要求回収は、既存のホスト側operation管理との接続を検討する。

HenjiではOpenRouterの質問は名前付きobject、OpenAIの質問は配列で、入力・送信形式にも差がある。NOEの公開引数をどこまで共通化するか、provider固有の質問・応答フィールドをどう保持するか、返却JSONをJSでどう扱うかを設計時に決める。専用モジュールにするか、API名・設定・モデル選択、画像入力の初期対応範囲、利用SDKは未決定。まず現在のOpenRouter構成を土台に検討し、実装時点の公式仕様とSDK対応を確認する。

参照元（NOEのリポジトリ直下からの相対パス）：

- `../henji/README.md`の「Decisions: text and image classification」
- `../henji/docs/design/decision-cli.md`：目的、入出力契約、認証、providerごとの形式
- `../henji/decision.go`：CLI、質問と入力の受渡し、認証との接続
- `../henji/internal/decision/decision.go`：質問の検証、専用要求、完全な応答JSONの保持、取消しと再試行
- `../henji/docs/examples/decision-openrouter-questions.json`：複数質問の例

2026-10-17、[意思決定APIの実装計画](decision-plan.md)に沿って実装・検証を完了。実接続は許可されたJev 1.14・Perplexity Decider V1.2・Clefの3モデルと親agent経路を確認し、`typesafe/jev-router`は利用者の追加指示で対象から外した。

#### <一部完了> 応用例：Jevによる起動設定プリセットの選択 — 2026-10-17

**新しい本体機能ではなく、既存の`decide`と`run_agent`・`spawn_agent`を組み合わせるJSの応用例。** よく使うモデル設定には一定のパターンがあるため、タスクに適したプリセットをJevに選ばせ、その設定で親agentを起動する。

```text
タスク＋プリセットごとの用途・得意分野・制約
  → decide（Jevのchoiceでプリセットを選択）
  → 返されたキーでJSの設定表を引く
  → 選んだprovider・model・effortで親agentを起動
```

プリセットは、例えばlight・standard・deep、またはコード向け・画像向けなど。実際のprovider・model・effortの対応表と各候補の説明は共通のJSに持たせる。`questions`の`criteria`に候補の説明を渡し、タスクの難易度・必要な能力・費用や速度の優先方針に応じて選択する。返却された`result.answers`内の`choice`で設定を取得し、起動APIへ渡す。モデルの差替えはプリセット側で行える。

同じ選択用JSを、親が子agentを起動するときにも利用できる。NOE本体に固定のルーティング方針を追加せず、候補と選択基準を呼出し元で組み立てる。`typesafe/jev-router`をテスト対象から外した背景も、この構成を通常のJevによる判断で作れると考えたため。2026-10-17、[モデルカタログ](model-catalog.md)と[起動前のモデル選択例](../examples/agent-route-model.js)を追加した。候補は内蔵JSON＋外部差分で管理し、`list_models`から取得する。カタログ→Jev→選択した親の起動→run_jsでのカタログ再参照をローカルfixtureで確認済み。サンプルのeffortは明示autoで、effortも含むプリセット選択とこの例の有料実接続は未実施。

### <未完了> agentのVision対応 — 2026-10-17

**検討事項：Vision対応モデルを使うagentへ画像を渡し、親agent自身が画像を見て判断できるようにする。**

現在の`run_agent`・`spawn_agent`はinstructionを文字列で受け取り、`run_js`の結果もテキストとしてモデルへ返す。Vision対応モデルを指定するだけでは画像入力にならず、base64を通常の文字列に含めても画像パートにはならない。現時点では、`run_js`から`noe:decision`の画像入り`state`を送信し、判断結果を親へ返す経路は利用できる。

候補は、起動時の入力と`run_js`の結果に、テキストと`image_url`等の画像パートを渡す経路を設けること。JSで画像ファイルを読み、base64のdata URLにして渡す形を出発点とする。Goの生成用provider層はメッセージをJSONで転送する土台があるため、公開API・agent loop・tool結果からモデルの画像入力へつなぐ契約を検討する。

入力形式、子agentへの画像受渡し、画像の履歴保持と将来のコンテキスト圧縮、JSONLへの記録範囲は実装時に決める。`noe:decision`の画像対応と通常agentの画像対応を区別し、実装後はVision対応の実モデルで確認する。現段階は検討メモのみ。

### <完了> モデルの推論エフォートの指定

**実装済み：agentからモデルを呼ぶ際に、推論エフォートを指定する。ルートではprovider・model・effortを明示指定し、`effort: "auto"`も選べる。**

タスクに応じて、深く考えさせる処理と短時間で済ませたい処理を使い分ける。親agentと子agentで異なる設定を選べる。応答に含まれるreasoning情報の保持とは別の設定として追加した。

設定口は`run_agent`・`spawn_agent`のconfigにある`effort`。指定値は`auto`・`none`・`minimal`・`low`・`medium`・`high`・`xhigh`・`max`。追加の合意により、ルートではprovider・model・effortの3項目を必須とし、`auto`は省略時の既定値ではなく明示的な選択肢にした。子で省略すると項目ごとに起動元runの設定を継承し、明示指定で上書きする。子の`auto`は継承を解除してprovider/modelの既定動作へ戻す。effortは各instanceのconfigで指定し、queue taskごとの上書きは行わない。既存の`maxTokens`や`maxSteps`とは目的を分けて扱う。

2026-10-16、OpenRouterの[公式仕様](https://openrouter.ai/docs/guides/best-practices/reasoning-tokens)と採用中の公式Go SDK v0.9.59を確認。`auto`はNOE側で`reasoning`を省略する指定として扱い、他の値は`reasoning.effort`へ渡す。推論の有効化を強制せず、既定の送信内容を維持する。model/providerごとの対応はAPIに従い、NOE側でモデル別の対応表や自動変更は追加しない。詳細は[APIリファレンス](api.md#自律agentopenrouter)と[実装記録](increment-13-implementation.md)を参照。

### <完了> agentの実行状況の観察

**質問：agentの様子を確認するには、ログをtailすればよいか。**

主用途は親agentが子の状況を読み、次の判断をすること。主案は共通の実行管理APIから状態とcursor以降のイベントを構造化して取得し、一度で戻る形。人間用CLIにも同じ情報の表示と追跡を用意する。ファイルの`tail -n`相当の読取りは簡易な比較案として残す。`tail -f`を待ち続けて親モデルに結果を返さない構成は、この用途に合わない。毎回ログ全体を渡さず、親のコンテキスト増大も抑える。

現在も`NOE_AGENT_TRACE=1`でメタデータをstderrへ出し、stdoutの回答と別々に保存できる。人間が端末から観察する場合は`tail -f`でよい。まず既存のファイルとCLIで観察できる内容を整え、専用UIや常駐サービスは必要性が出た時に検討する。

```sh
NOE_AGENT_TRACE=1 NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run try-agent.js "README.mdを読んで要点をまとめて" \
  >answer.log 2>agent-trace.log
# 実行中に別ターミナルで確認
tail -f agent-trace.log
```

現在のtraceはrun開始、model応答、JS結果、run完了/失敗/取消し、使用量等を含む。model要求開始・JS開始のイベントと絶対時刻はまだないため、応答を待っている段階や、長いJSを実行している段階は十分に識別できない。ログが増えないことだけでは停止・失敗と断定できない。

追加候補は、model要求開始と完了、JS開始と完了、将来のコンテキスト切詰めの開始と完了。時刻、run/親run ID、agent名、model、処理の対応を取るID、所要時間・使用量・終了結果を添え、どのagentがどの段階にいるかを追えるようにする。イベント名・項目は未決定。モデル本文・生成JS・tool出力まで確認したい場合の詳細ログは、現在のメタデータtraceとは別に検討する。

当初は、まず開始/完了・時刻・agent/runの識別ができる程度を想定した。親もこの状態を判断材料にし、必要なら既存のrun_jsで成果物等を読む。その後、以下の詳細イベントと外部JSによる表示の案を追加した。

機械処理ではstderrの通常診断とJSON Linesが混ざる点も考慮し、専用traceファイルを指定できる形を検討する。独立実行の案と組み合わせる場合は、起動結果として実行IDとログの場所を返し、最終的な終了状態・結果も確認できるようにする。将来のログ閲覧CLIはこの記録を読む窓口として追加できる。今回のメモでは実装・API・保存先は決めていない。

<a id="外部jsによるリアルタイム表示--2026-10-17"></a>

#### <完了> 外部JSによるリアルタイム表示 — 2026-10-17

当初の利用者のイメージは`noe run xxx.js | 観察.js`。NOE本体から出るイベントを外部の観察用JSが逐次読み、ターミナルで見やすく表示する案だった。その後、利用者は組立てをGoに任せることも許容し、人間向けはJSONL表示に拘らず可読性を重視する方針を選んだ。現在はGoの`noe observe`で表示し、親向けにはGoが状態を組み立てる`observe_agent`を利用できる。外部の観察用JSは同梱していない。

親agentも読むため、通常出力とイベントを全行JSONLに統一する保存モードを合意し、`--events`として実装した。`run`・`eval`・`repl`で共通の形式を使い、stdout・明示ファイル・自動命名ファイルへ出せる。[JSONL記録](events.md)は人間向けの表示とは独立して維持する。

初期案の`2>&1`による通常出力とtraceの混在は使わず、`--events=-`からJSONLを渡す。現在使える人間向けの入口：

```sh
./noe run --events=- xxx.js | ./noe observe
```

- 標準入力を行単位で逐次読み、`type`に従ってイベントを整形する。通常のJS出力も`stdout`・`stderr`イベントとして包まれるため、内容がJSONでもtraceと誤認せず表示できる。NOE自体の診断はJSONLとは別のstderrへ出る。
- 観察したい内容は、`run_js`の先頭の短い1行コメント、JS実行の開始・成功・失敗・所要時間、providerが返すthinking・thinking summary、agentの途中の発言と最終報告。これらのイベント取得は`--events`へ実装した。従来のメタデータtraceの内容は変えていない。
- 複数の親agentを`Promise.all`で並列実行した場合、イベントは到着順に混ざる。各表示行にagent名とrun IDを付け、複数行のthinking等もrunごとに区別して表示する。`runId`は`run()`ごとに異なり、子の`parentRunId`に親の`runId`が入る。子を起動しても親自身のIDは変わらない。
- 元のJSONLは保存やjqでの絞り込みにも使う。親agent向けの有限な差分取得と、人間向けに入力を待ち続けるリアルタイム表示は用途を分ける。

##### <完了> 親用・人間用の観察口の整理

追加LLMによる要約は使わず、Go側で状態と差分を組み立てる。親用は`noe:agent`の`observe_agent`から構造化データを受け取り、人間用は`noe observe`で可読性を重視したテキストとして表示する。現在の引数・戻り値・CLIは[観察の入口](observe.md)を参照。以下には設計時の戻り値イメージも残す。

| 観点 | 親agent用 | 人間用 |
| --- | --- | --- |
| 呼出し元 | 親のrun_jsや通常JSからobserve_agentを呼ぶ | run/eval/replの--observeで直接表示、またはnoe observeでログを読む |
| 入力 | ログの場所または管理API、対象run ID、前回のcursor | パイプのJSONL、またはログファイルの追跡出力 |
| 動作 | `observe_agent`で状態・差分・確定済みの結果を取得して戻る。必要なら`wait: true`で完了まで待つ | 新しいイベントを待ち、受信しながら逐次表示する |
| 出力 | 親モデルが判断に使う構造化した値 | agent名・短縮run ID・イベント種別を添えた読みやすい表示 |
| 主に見る情報 | 実行状態・段階、直近のrun_jsコメント・発言・実行結果、完了時の回答 | run_jsコメント、成功失敗と所要時間、発言・thinking・summary・最終回答、通常出力 |

**親用**は前回以降の差分と、対象runの状態をまとめて返す。戻り値のイメージは「対象run/親run ID、最後に観察した時刻・段階、現在または直近の処理、直近の発言、JSの成功失敗、完了結果、次のcursor」。元のイベント列やthinkingは必要な場合に取得できるようにし、毎回すべての断片を親モデルのコンテキストへ入れない。ログが増えないことだけで停止・失敗とは判定しない。

親用の戻り値のイメージ（項目名は仮）：

```json
{
  "runId": "child-run-id",
  "parentRunId": "parent-run-id",
  "status": "running",
  "phase": "run_js",
  "action": "README.mdを読む",
  "lastMessage": "構成を確認しています",
  "lastToolResult": {"success": true, "exitCode": 0},
  "cursor": {"executionId": "execution-id", "seq": 42}
}
```

この例の`lastToolResult`は完了済みの直近toolの結果で、現在実行中の`action`が成功したという意味ではない。初回以外の呼出しで状態を再構成するため、読取り位置だけでなく必要な再構成状態を保持・受渡しするか、管理APIの状態取得と組み合わせるかを実装時に決める。ファイル追記でexecutionIdが変わりseqが戻ることや、行の書込み途中を読んだ場合も扱う。

**人間用**は`noe run --events=- xxx.js | noe observe`や`noe observe --follow agent.jsonl`で使う。並列agentの断片を混ぜず、agent名・短縮run IDを付け、tool由来の出力には短縮tool IDも付ける。thinking・summaryは同じrun・要求・種類の断片を順に結合し、受信時に表示する。発言と最終回答は確定した本文を表示し、最終回答は見出し付きのブロックにする。元のJSONLは断片のまま残し、表示上の整形だけを行う。`message_delta`と確定した`agent.message`・`agent.final`を重ねて表示しない。

追加の利用者要望で、`noe run --observe xxx.js`のようにパイプなしでstdoutへ同じ整形表示を出す入口も追加した。`eval`・`repl`も共通で、`--events=path`または単独の`--events`を併用するとJSONLも記録する。`--observe`だけではファイルを作らず、`--events=-`は機械向けstdout JSONLとして維持する。

当初のJS案では`process.stdin.read()`がEOFまで全体を同期読取りする点が課題だった。現在の人間向けCLIはGoで標準入力やファイルを逐次読み、EOF前にも表示する。親用は既定では取得済みの情報を一度で返し、明示的に`wait: true`を選んだ場合だけ完了を待つ。

`run_agent()`を`Promise.all`で並行実行すると、各子の完了後にsnapshotを受け取れます。完了前に親モデルへ戻り、次の`run_js`から観察・queueする場合は`spawn_agent()`でIDを受け取ります。親run終了後も子を存続させる操作はありません。

SQLiteへのイベント保存も検索用の候補として挙がった。採用は未決定で、外部JSによる逐次表示とは併用可能。

### <完了> 実行中のagentへの追加指示

**質問：agentを実行したまま、外から追加指示を届ける経路を作れるか。**

実現可能。現在は`run()`開始時のinstructionだけを受け取り、実行中の追加指示APIはない。主用途は、親agentが子の状態を確認して、その子へ追加指示を送ること。検討案は、run単位のinboxへ親または人間から指示を入れ、対象の子のloopが次のモデル要求の直前に受け取る方式。追加指示がなければ通常どおり進み、入力・承認を待つ状態は作らない。

```text
親agentが子の状態・新しいイベントを取得
  → 判断して対象子のinboxへ追加指示
  → モデル要求直前のhookで取得
  → userメッセージとして履歴へ追加
  → 同じrunで次の判断へ反映
```

親と人間の窓口は共通の実行管理APIを主案とし、親はrun_js内のJSラッパー、人間はCLIから送信する。受け取った指示は対象runのinboxへ登録し、受領と実際の適用を区別する。内部のinboxをメモリにするか、ローカルファイル/directoryにも記録するかは未決定。ファイルを使う場合は書込み完了前に読まない受渡しを設計し、どの方式でも順序・指示ID・消費記録で重複適用を防ぐ。常駐サービスは前提にしない。

最初は処理の境界で反映する。進行中のモデル要求やJSを途中で書き換えず、その完了後の次の判断から適用する。即時の取消しやJSの強制停止は別の操作として検討する。最終回答の確定前にもinboxを確認し、完了との境界で受け付けた指示を黙って残さない。終了後に届いた指示の未適用表示や、新しいrunとして扱うかも設計時に決める。

system instructionは書き換えず、追加指示をuser入力として扱う。コンテキスト切詰めを行う場合、有効な追加指示が単なる古い履歴として脱落しないよう保持する。複数run・子agentへの宛先はrun IDで区別し、親が対象の子を指定できることを検討範囲へ含める。同じrunへの指示追加では`session_id`を維持し、既存の時間・回数上限を暗黙にリセットしない。

受領・適用・未適用を指示IDとrun IDでログへ記録し、tailで反映を確認できる形を検討する。指示本文はメタデータtraceへ自動追加せず、投入元の記録や詳細ログの扱いと合わせて決める。受け口・公開API・hookの順序・終了時の扱いは未決定であり、実装は行っていない。

#### <完了> 外部からの操作：steer・interrupt・queue — 2026-10-17

**この節は2026-10-17時点の実装前案を保存した履歴です。** 現行の`steer_agent`・`interrupt_agent`・人間用操作CLIの契約は[APIリファレンス](api.md#実行中taskへの指示と停止)と[実行中の操作計画](agent-control-plan.md)を参照してください。

人間から親agentへの操作と、親agentから子agentへの操作を、共通のGo側実行管理APIで受け付ける案。外部からの操作を次の3種類に整理する。いずれも未実装で、名前・引数などの公開APIは未確定。

| 操作 | 意味 | 反映するタイミング |
| --- | --- | --- |
| steer | 現在のタスクに追加・変更指示を送る | 次のモデル要求の直前に取り込み、同じrunの次の判断へ反映する |
| interrupt | 実行中のJSやモデル通信を止める | 受付後、取消し処理を開始する |
| queue | 次のタスクを予約する | 現在のrunが終わった後、新しいrunとして実行する |

人間が親のJS実行中にsteerする場合も、JSとは別にGo側で指示を受け付け、対象runのinboxへ保管できる。例えば別ターミナルのCLIから親のrun IDを指定して送る。JSやモデル通信が続いている間に受領できることと、モデルが指示を踏まえて判断するタイミングを分ける。steerでは実行中の処理をそのまま進め、完了後の次の判断時に適用する。終了を待たずに止める操作がinterruptになる。

inboxはsteerの指示を一時保管する場所で、次の仕事を予約するqueueとは区別する。queueは現在のrunへの履歴追加ではなく、別のrunとしてタスクを開始する案。親は既存のrun_jsからJSラッパーを呼び、人間はCLI等から同じ操作を使うため、モデルに渡すtoolはrun_js一つのままにできる。

### <完了> 自作REPLの必要性とqjsの再利用

**質問：qjsにREPLがあるなら、NOEが自作REPLを維持する必要はあるか。**

2026-10-16、固定依存go-quickjs v0.25.3の`docs/qjs.md`、`cmd/qjs/main.go`・`lineedit.go`・`prompt.go`とNOE側のSession/REPLを静的に確認した。go-quickjsにはGo製のqjsコマンドがあり、行編集、永続履歴、Tab補完、複数行、前回値`_`、top-level await、Ctrl+C中断を提供している。元のC製QuickJSにもqjsがあるが、今回の再利用候補はNOEと同じGo実装に同梱されたコマンドを主に考える。

汎用JavaScriptを対話的に試す目的はqjsと重なり、agentの自律実行や親による子の監督にREPLは必須ではない。NOE側で行編集・補完・await変換等を独自に拡張し続ける価値は、実際の利用頻度と保守量で見直す候補とする。

NOEのREPLを残す価値は、通常のrun/evalと同じstdlib・loader・`noe:agent`・認証・取消しを対話的に確認できる点。既存qjsをそのまま起動しても、NOE独自のnative moduleとoperation管理は入らない。また固定版qjsのREPLは`package main`内の非公開関数で、公開のREPL部品として直接importできる形ではない。再利用には上流の部品化、必要箇所の移植、またはNOE接続を持つ派生コマンド等の変更が必要で、qjsの存在だけを理由に置換可能とは判断しない。

追加の質問「qjs側のREPLからnoe:agentを呼ぶ構成にできるか」に対しては、実現可能。具体案は**qjs由来のREPLの入力・編集・表示を使い、評価先をNOEが構築したruntimeにする**こと。NOEが`noe:agent`のJSソースと`noe:agent/internal`のnative関数、認証・broker・子NOE実行設定を登録し、REPLから同じmoduleをimportできるようにする。Go側の認証を維持し、JSへAPIキーを渡す必要はない。

module登録だけでなく、入力ごとの評価context・agent Operationの開始/終了、PromiseのLoop追跡、Ctrl+C・REPL終了時の回収も接続する。現在のqjsは入力を自身でEvalContextするため、NOEのSession評価へ委譲する口、または評価前後のhookが必要。補完もNOEの現在contextとI/O回収に整合させる。エンジンが同じであることだけでは、この接続は自動で成立しない。

この再利用案ならNOEの`repl`コマンドを維持しつつ、入力UIにqjs由来の実装を利用できる。ただし現在の固定依存にそのまま使える公開APIはない。既存qjsの起動オプションだけでNOEのnative moduleを追加する構成ではない。

当初の選択肢は、(1) 現在のREPLを最小限で維持、(2) qjsの共通部分を再利用、(3) REPLを外して汎用の対話実行はqjsを使う、の三つ。その後の合意では、**固定版qjsの入力・行編集ソースをNOEへ取り込む**方針を選んだ。上流への公開パッケージ化の提案・採用は前提にしない。評価、await、履歴ファイル、補完候補の取得、agent operationと取消しは既存NOEの責務として残す。

実装・検証と更新時の扱いは[qjs REPL入力の統合記録](qjs-repl-integration.md)を参照。エンジンの依存更新だけでは取り込んだソースは更新されないため、上流の入力UI差分を見て必要な変更を移す。将来、実用的な公開REPL部品が上流に提供された場合は再評価できる。参照：[go-quickjsのqjsコマンド](https://pkg.go.dev/github.com/go-quickjs/go-quickjs/cmd/qjs)、固定版のローカルmodule source。

## v1.0.0時点の優先順案

当時は外部コマンド、ストリーミング、import map、HTTP URLからのimportを次の候補として検討していた。その後の標準ライブラリ導入により、外部コマンド・ストリーミング・暗号とハッシュは利用可能になった。import mapはローカルの完全一致名に対応済み（[計画](local-import-map-plan.md)・[検証記録](local-import-map-implementation.md)）。HTTP URLからのimportは未対応。以下は当時の優先順案であり、新しい実装順を確定するものではない。

| 順番 | テーマ | 目的 | 最初に検討する範囲 |
| --- | --- | --- | --- |
| 1 | <完了> 外部コマンド | 既存ツールとファイル・HTTP処理を組み合わせる | コマンドと引数、stdin、stdout・stderr、終了コード、実行中断 |
| 2 | <完了> ストリーミング | 大きなファイルやHTTP bodyを全量保持せず扱う | ファイルとHTTPクライアントの段階的な読取り・書込み |
| 3 | <一部完了> import map | ローカルモジュールに名前を付け、配置変更を吸収する | CLIで指定するimport mapとローカルファイルの解決 |
| 4 | <未完了> HTTP URLからのimport | URLで公開されたJSモジュールを読み込む | モジュール取得、URL基準の相対import、キャッシュ、取得失敗の扱い |

当時、次のインクリメントの第一候補は外部コマンドだった。現在は標準ライブラリ導入により、外部コマンドは`child_process`から利用できる。

当時はストリーミングをファイル・HTTPクライアントから始め、HTTPサーバーを別段階にする案だった。現在は標準ライブラリのWeb StreamsとFetchから利用できる。import mapとHTTP importは関連するが、別インクリメントとして仕様を確かめる案とする。

## 保留と製品方針

暗号・ハッシュは当時は要検討だった。現在はWeb Cryptoと`crypto`モジュールから利用できる。

TypeScript対応は引き続き後回しとする。npm・パッケージ管理とNode.js/Denoの完全互換は対象外とする。Web APIとNode形式APIは固定したgo-quickjs標準ライブラリの互換範囲に従う。

[開発記録へ戻る](README.md) · [現在のAPI](api.md) · [製品方針とMVP](mvp.md)

### <一部完了> OpenAI SDK・Anthropic系SDKの追加 — 2026-10-17

OpenRouter SDKに加え、今後OpenAI SDK、その先にAnthropic系の接続を追加する。モデルカタログのprovider・model・説明・能力はSDKから独立した共通データとし、Go側の通信adapterが各形式を扱う。現在の外部JSON上書きと`list_models`は共用する。2026-10-17、OpenAI SDKを組み込み、`openai-chatgpt`・`openai-api`のResponses、互換接続のChat Completions、外部JSのChatGPT認証ヘルパーを追加した。[利用ガイド](openai.md)・[実装記録](openai-implementation.md)。Anthropic系SDK・Messagesは未対応。

#### 利用者のモデル・接続先の選択方針 — 2026-10-17

普段はサブスクのOpenCode Goを優先し、OpenRouterは様々なモデルの試用と意思決定モデル3種類に使う。共通の利用順はDeepSeek V4.1 Flash、MiMo-V2.6-Flash、GPT 6 Luna、GLM 5.3。effortはタスクごとに変える。OpenAIではgpt-6.1-sol・gpt-6-lunaをResponsesで使い、ChatGPTサインインをAPIキーより優先する。OpenAI SDKは将来のローカルLLM接続にも使う。現在のカタログはprovider/model別の候補を返し、優先順位の自動適用やモデル横断の接続先選択は呼出し側のJSに任せる。MiMo・GLMの組込み候補追加と優先度項目は未実装。
