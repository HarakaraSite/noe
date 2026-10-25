# JSONLによる実行状況の観察

agentイベントには、CLI全体の`executionId`に加え、所有ルートの`groupId`、
仕事の`activityId`、原因の`causedBy`と`origin`を付ける。
独立したrootは別group、子孫・同instanceのqueueは同groupになる。
`execution`はgroupの累計で、個々の要求のcontext量とは区別する。
`interrupt.accepted`のactivityとoriginは停止を要求した操作を示し、
`runId` / `agentId`は停止対象を示す。再送で新しい受付イベントは作らない。

同じinstanceのqueue taskにはそれぞれ新しい`runId`が付きますが、会話は前taskの最終回答を含む
確定済みのtrim後snapshotから続きます。queue指示は受付時点で現在の会話へ加わらず、
task開始時にuser messageとして追加されます。`context.trimmed`は実際にtrimを行ったrunへ記録し、
trim回数は同instanceの後続queue taskにも引き継ぎます。新しいinstanceは別の会話です。

| イベント | 内容 |
| --- | --- |
| `model.step.started` | 論理判断の開始。steer再準備中は同じ`modelStepId` |
| `hook.started` / `hook.finished` / `hook.failed` / `hook.cancelled` | module・phase・invocationの寿命 |
| `tool.finished` | 元引数・実行引数、元結果・モデル向け結果を区別したtool結果 |
| `context.updated` | 実際のモデル要求の終了時に1回通知する`context` snapshot。`status`は`completed`・`failed`・`cancelled` |
| `context.trimmed` | 実際に適用した切り捨ての前後概算、削除block数、理由、予算source、原因情報と更新snapshot |

`context`は明示入力予算、catalogのwindow、直前要求の実測、次要求の概算を持ち、
`executionContext`に実行履歴の有効予算・source・使用率・切り捨て回数を持つ。
`contextBudget.inputTokens`は固定入力を含む総入力目安のまま。未知値は`null`。
catalog window不明かつ明示予算なしの場合に使う1,000,000-token fallbackは総入力目安であり、モデルcapacityを表さない。
cached入力は実測promptの一部で、group累積tokenや応答上限とは別。
概算方式・snapshotの型・hook APIは[hookガイド](hooks.md)を参照。
標準通知は観察無効時もモデルに届き、JSONL記録自体は従来どおり`--events`で選ぶ。
回復したhandler失敗は同じ`hook.failed`として残り、その後に`agent.completed` / `command.completed`が続くことがあります。`hook.failed`だけでtask失敗や並列tool取消しとは判定しないでください。取消し・上限・Host継続不能は別です。`hook.failed`は発生診断であり、モデルへの通知配送の完了イベントではありません。

失敗の`invocationId`・`hookIndex`・`module`・`phase`・`modelStepId`と、該当時の発生元`requestId` / `toolCallId` / `toolName`で処理を対応付けます。エラー詳細は`error`に残ります。配送状態はtaskの保持結果`hookFailures[].notifiedRequestId`で確認します（未設定は未送付）。JSONLを保存しなくても記録・通常要求への通知・保持結果取得は成立します。[通知の時点と結果型](hooks.md#モデルへの同送と保持結果)を参照してください。

再計算・切り捨て・通知の発火は独立しています。準備時と送信時にも内部状態とlive snapshotを
更新しますが、`context.updated`は発火しません。応答または失敗・取消しの結果が出た時点で、
その要求の概算と取得できた実測値を含むsnapshotを通知します。providerへ進まないsteer配送や
受付前の拒否では発火しません。モデルへ渡す標準通知は、引き続き要求ごとの最新情報1つです。

`context.trimmed`はJS履歴への削除適用を確認した後に一度だけ出し、削除したtool本文やreasoning本文は含めない。
steer-onlyの再準備中は切り捨て通知とcauseを次の実provider要求まで保持する。
`observe_agent`はこのeventと同じ汎用`context` snapshotを返す。過去のJSONLも未変更で、
そのevent以後に削除された会話本文を再構成するものではない。

`run`・`eval`の`--events`オプションで、通常出力とagentの詳細イベントをJSON Linesとして記録できます。省略時の出力は変わりません。人間と親agentが同じイベントを読み、runや種類で絞り込むための形式です。

## 出力先

人間向けに直接表示する場合は`--observe`を使います。`./noe run --observe examples/agent/try-agent.js`でパイプなしに整形表示でき、`--events=agent.jsonl`を併用すれば元のJSONLも保存できます。詳細は[観察の入口](observe.md)を参照してください。以下はJSONLを出すオプションです。

```sh
# stdoutをJSONLにしてパイプで読む
./noe run --events=- examples/agent/try-agent.js | jq --unbuffered -c .
./noe eval --events=- 'console.log("hello")' | jq --unbuffered -c .

# 通常の画面表示を保ち、ファイルにも記録する
./noe run --events=agent.jsonl examples/agent/try-agent.js

# .noe/events/へ名前を自動生成して保存する
./noe run --events examples/agent/try-agent.js
```

agentを実行する例では、従来どおり`NOE_OPENROUTER_API_KEY_FILE`等による認証設定が必要です。`--events`自体はproviderを呼びません。オプションはスクリプト名またはevalコードより前へ置きます。`--events`単独は次の引数を消費しません。ファイル名を指定するときは`--events=path`の形を使います。

自動生成先は、NOE起動時のcwdにある`.noe/events/noe-events-20261017-153000-<ランダムID>.jsonl`です。`.noe/events/`がなければ自動作成します。1回のNOE起動につき1ファイルを作り、その絶対パスをstderrへ表示します。同じNOE内で複数の親agentや子agentを動かしても、保存先は共通です。`run_agent`・`spawn_agent`に子ごとの保存先を指定する設定はなく、親NOEの`--events`指定で子孫のイベントも同じファイルへ記録します。

相対パスと自動生成先は、NOE起動時のcwd基準です。スクリプトの所在や実行途中のcwd変更には影響されません。絶対パスも指定できます。明示したファイルは追記で開き、自動生成ファイルは新規作成します。明示した保存先の親ディレクトリは事前に用意してください。ファイルへの書込み・終了時のcloseに失敗した場合はstderrへ診断し、NOEは失敗終了します。

`--events=-`では、スクリプトのstdoutとstderrもイベントに包み、stdoutへ生のテキストを混ぜません。NOE自体の診断はstderrへ出します。ファイル指定時は、`--observe`がなければスクリプトの通常出力をそのまま表示し、同じ内容をファイルにも記録します。`--observe`併用時は画面を整形表示にします。ファイルは利用可能なイベントから逐次書き込み、完了までまとめて保持しません。

## イベント形式

すべての行に以下の共通項目があります。

| 項目 | 意味 |
| --- | --- |
| `version` | 現在は`1` |
| `executionId` | NOE起動ごとの識別子 |
| `seq` | その起動内で1から増えるイベント番号 |
| `time` | UTCのRFC 3339形式の時刻（小数秒付き） |
| `type` | イベントの種類 |

明示ファイルへの追記では、起動ごとに`executionId`が変わり、`seq`は1に戻ります。差分取得では実行IDと番号を組にするか、ファイルの読取り位置を保持します。

`agentId`は起動から停止まで同じinstanceを示し、`runId`はtaskごとに発行されます。taskとinstanceのイベントには`parentRunId`・`name`・`model`も付きます。`parentRunId`は起動元taskのrun IDで、親のないinstanceでは空文字です。raw JSONLでは、instance状態の`instance.*`、task状態の`agent.*`、queueまたはsteer操作の`command.*`を区別します。`command.*`は`operation`で`queue`と`steer`を識別します。instanceとtaskのイベントには`agentId`が付き、taskのイベントにはそのtaskの`runId`も付きます。スクリプト全体の通常出力には、特定agentのIDを割り当てません。

| `type` | 主な追加項目・タイミング |
| --- | --- |
| `execution.started` / `execution.finished` | CLI開始・終了。`command`または`exitCode` |
| `execution.error` | 実行エラー・タイムアウトの`text` |
| `stdout` / `stderr` | JSが出力した文字列断片の`text`。改行を含め元の文字列を保持 |
| Legacy `repl.result` / `repl.error` | 旧バージョンで保存されたREPL評価結果・エラーの`text`。現行CLIは生成しません |
| `instance.started` / `instance.idle` / `instance.running` / `instance.stopping` / `instance.stopped` / `instance.failed` | AgentInstance状態の変化。`agentId`・状態・停止理由など。taskの成功・失敗イベントとは別に記録 |
| `command.accepted` / `command.applied` / `command.completed` / `command.failed` / `command.cancelled` | queueまたはsteer commandの受領・適用・結果。`operation`は`queue`または`steer`、`commandId`とstatusも付く。queueの結果にはtaskの`runId`も付く。`command.accepted`には指示本文の`instruction`も付く（後続状態には繰り返さない） |
| `interrupt.accepted` | taskまたはinstanceへの停止要求の受付。`target`は`run`または`agent`、`status`は`accepted`。run対象には`runId`、agent対象には`agentId`が付き、taskが実行中なら`runId`も付く。いずれも`parentRunId`・`name`・`model`を含む |
| `agent.started` / `agent.completed` / `agent.failed` / `agent.cancelled` | TaskRunの状態・終了時の`duration`。instanceが管理するrunにはinstance側の`agentId`も付く。失敗時は`error`、明示的なVision refusalがあるrunでは`errorDetails`も付く |
| `model.started` / `model.finished` / `model.failed` | モデル要求の`requestId`。完了時は`finishReason`・`usage`・`duration`、失敗時は`reason`等。明示的なVision refusalを含む`model.finished`には`errorDetails`が付く |
| `thinking` / `thinking_summary` | providerから返った推論本文・要約の断片`text`と`requestId` |
| `message_delta` | モデルの発言本文の断片`text`と`requestId` |
| `agent.message` | toolを呼ぶ応答に含まれる途中の発言`text`と`requestId` |
| `agent.final` | loopが正常な最終回答として受け入れた`text` |
| `run_js.started` | `toolCallId`・モデルが渡したJS全文の`code`と、codeの先頭行が`//`コメントならその行の`text` |
| `run_js.finished` | `toolCallId`・`success`・`exitCode`・`reason`・`duration`、返却がある場合の`data`、失敗情報の`diagnostics` |
| `execution.diagnostic` | agent task自体のVM診断。`source: "agent_task"`・`agentId`・`commandId`・判明時のみ`runId`・`diagnostics` |
| `image.read` | `path`・`modelStepId`・`toolCallId`と、`image`内のimage ID・MIME・byte数・寸法・detail |
| `image.started` / `image.completed` / `image.failed` / `image.cancelled` | `provider`・`imageModel`・`path`。完了時は画像metadata・取得できる`responseModel`・`providerRequestId`・`usage`・`duration`、失敗・取消し時は`error`と`duration` |
| `tool.finished` | `run_js`では`rawImages`（hook前の読取り候補）と`images`（採用後）を`rawResult`・`result`と分けて記録。画像項目はmetadataのみ |
| `decision.started` | `requestId`・`provider`・要求モデルの`decisionModel` |
| `decision.completed` / `decision.failed` / `decision.cancelled` | 同じ要求IDと`duration`。取得できる`statusCode`・`responseModel`・`usage`、失敗時の`reason`・`error` |

hookのエラー説明にもpath・出力・利用者の値などが含まれ得ます。`hook.failed.error`だけでなく、モデルへ同送する事実通知と`TaskOutcome.hookFailures`にも内容が含まれます。自動redactionは行わないため、hookで扱う秘密値、保存先のアクセス権、外部送信・共有範囲を確認してください。

`command.accepted.instruction`は受け付けた指示の元の文字列です。CLIからの操作だけでなく、初回タスク・JS/Go API由来のqueueやsteerも記録対象です。指示本文には機密情報が含まれうるため、画面共有やJSONLの保存・共有時は内容とアクセス権を確認してください。古いログにはこの項目がない場合があり、その本文は復元しません。

`duration`はnanosecondsです。並列実行のイベントは到着順に並びますが、同じNOE内の書込みを直列化するため、JSON同士が文字単位で混ざりません。

Vision応答でproviderが明示的にrefusalを返した場合、`model.finished`および失敗した`agent.failed`の`errorDetails`に`code: "refusal"`と取得できる`providerMessage`・`requestId`を残します。モデルのrefusal本文を検出した場合の情報で、通常の通信・入力エラーをrefusalと推定しません。画像の生成と読取りの使い方は[画像ガイド](agent-images.md)を参照してください。

意思決定要求は[noe:decision](decision.md)を使います。`run_js`内からの要求には所有runのmetadataと`toolCallId`を付け、共通の`model`は親agentの設定モデルを維持します。意思決定のモデル名は`decisionModel`・`responseModel`へ分けます。通常JSからの直接要求にはagent/run IDや共通の`model`を割り当てません。人間向けは要求の開始・終了を短く表示し、親のファイル観察は差分イベントへ含めます。意思決定イベントで親runのphaseや終了状態を変更しません。応答本文はJSへ返し、JSONLのdecisionイベントはmetadataを記録します。

thinking・summary・messageの断片はストリーム受信時に出ます。providerが返さない推論本文・要約は生成しません。暗号化されたreasoningや署名は表示用イベントへ出さず、次のモデル要求に必要な元のreasoning情報は従来どおり履歴へ保持します。`message_delta`の時点では最終回答か未確定なので、確定した回答が必要なら`agent.final`を読みます。本文の断片と確定イベントは重複表示せず、用途に応じて選びます。

通常のJS出力は改行なしでも書込みごとに通知します。UTF-8文字の途中で分割された書込みは、文字が揃ってから通知します。`child_process`が返す捕捉出力は、JSが表示した時点で通常出力になります。`run_js`内のstdout/stderrは現在もrunnerが捕捉し、tool完了時に`source: "run_js"`とrun/tool ID付きで記録します。tool内部の出力の逐次中継は行っていません。

`run_js.started.code`は実行開始時に記録するため、失敗・取消しの場合も呼出し方を確認できます。元sourceをそのまま記録します。agent helperは利用者が明示importし、内部wrapperや接続情報をcodeへ合成しません。人間向けの`--observe`と`noe observe`は従来どおり先頭コメントを表示し、JS全文は表示しません。コードの記録は`--events`に含まれ、従来のメタデータtraceには含まれません。

`run_js`のstdout・stderrは各streamについてoutputBytes分（既定64 KiB、最大256 KiB）の捕捉出力と省略マーカーを記録します。超過時は先頭・末尾を保持します（既定では32 KiBずつ）。省略があるstreamの出力イベントには`omittedBytes`、`run_js.finished`には`stdoutOmittedBytes`・`stderrOmittedBytes`を該当streamについて追加します。`tool.finished.rawResult`にも同じ省略情報が入り、`result`はhook変換後にモデル向けの同じoutputBytes予算（既定64 KiB）を適用した値です。metadataとJSONエスケープを含む返却JSON全体を数えます。超過時は`outputPreview`・`outputLimit`・元の実行状態を返します。加工値が元結果と異なりpreview化で失われる場合だけ、`hookResult`へ制限前の加工値を保存します。[実行結果](execution-results.md)を参照してください。JSONLは省略された出力の完全ログではありません。通常のトップレベルJS出力の記録は変更しません。

v2.3.0以降では、未処理例外を`diagnostics`のcode/stage/messageと、取得できたstack/position/exceptionとして返します。診断をtoolのstderrやJSONLの`stderr`イベントへ合成しません。commandのstdout/stderrは例外詳細内で区別し、byte出力はencoding付きで保存します。`run_js.finished.exitCode`はNOEの実行終了コードであり、`diagnostics[].exception.code`は内部コマンドや利用者例外の別の値です。agent task自体の診断は`execution.diagnostic`へ残します。詳細は[実行結果と診断](execution-results.md)を参照してください。

詳細保持は固定した項目の文字列・有限の数値・booleanを対象とし、読取りに失敗する項目やオブジェクトは省略します。任意のプロパティ、循環参照、独自の`toJSON`、バイナリ出力等を完全に保存する仕組みではありません。JS内で捕捉して処理した例外は自動記録しません。NOE管理の`child_process`では、非同期の`execFile`・`exec`に加え、同期の`execFileSync`・`execSync`も失敗時のErrorにコマンドの`code`・`stdout`・`stderr`を持つため、未処理ならこの診断で取得できます。同期版の出力は常に文字列です。これはNOE側の変更であり、原型のgo-quickjs v0.27.0の同期版にはありません。

## 過去のREPLイベントログ

旧バージョンの保存済みJSONLには、REPL評価結果の`repl.result`とエラーの`repl.error`が含まれることがあります。`noe observe`はこれらの既存ログも読み取り、人間向けに表示します。現行CLIはこの2種類のイベントを生成しません。プロンプト・補完・カーソル制御は当時もイベントへ入りませんでした。

```sh
tail -f agent.jsonl | jq --unbuffered -c .

# 名前で絞り込む
tail -f agent.jsonl | jq --unbuffered -c 'select(.name == "parent-A")'

# 特定runと直接の子を読む（孫以降は別途辿る）
tail -f agent.jsonl | jq --unbuffered -c \
  'select(.runId == "aaa" or .parentRunId == "aaa")'
```

人間向けの整形表示はGoの`noe observe`、live状態は`noe:agent`の`observe_agent`、保存ログの状態・差分・結果解析は`noe:agent-log`の`analyze_log`を利用できます。`--observe`と`noe observe`は1行目に`[name agent=<agentId8>]`や`[name run=<runId8> tool=<toolId8>]`、2行目に`agent idle`や`run_js started: 内容`を表示し、イベント間に空行を入れます。task開始時にはagent IDとrun IDの両方を示します。steerの受付・適用・取消しとinterruptの受付も人間向け表示に出します。`noe queue`・`noe steer`から送った指示（`origin.kind: "human"`）は受付時に本文を1回表示し、Markdownとして整形せず、改行も保持します。人間由来のqueueの完了・失敗・取消しと、steerの後続状態はcommand IDだけを表示します。初回タスクや内部queueは人間向け表示では引き続き省略します。`./noe run --events=- examples/agent/try-agent.js | ./noe observe`で逐次表示できます。使い方は[観察の入口](observe.md)を参照してください。steer・interrupt・人間用control CLIの受付と確認方法は[APIリファレンス](api.md#実行中taskへの指示と停止)にまとめています。外部JSによる表示とSQLite保存は、当初の[検討メモ](completed-features.md#外部jsによるリアルタイム表示--2026-10-17)に残しています。設計と対象範囲は[control計画](development/agent-control-plan.md)を参照してください。

従来の`NOE_AGENT_TRACE=1`はメタデータのみをstderrへ出す機能として維持しています。詳細本文やtool出力を含む`--events`とは独立しており、従来のtraceの内容は変わりません。
