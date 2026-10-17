# JSONLによる実行状況の観察

`run`・`eval`・`repl`は共通の`--events`オプションで、通常出力とagentの詳細イベントをJSON Linesとして記録できます。省略時の出力は変わりません。人間と親agentが同じイベントを読み、runや種類で絞り込むための形式です。

## 出力先

人間向けに直接表示する場合は`--observe`を使います。`./noe run --observe try-agent.js`でパイプなしに整形表示でき、`--events=agent.jsonl`を併用すれば元のJSONLも保存できます。詳細は[観察の入口](observe.md)を参照してください。以下はJSONLを出すオプションです。

```sh
# stdoutをJSONLにしてパイプで読む
./noe run --events=- try-agent.js | jq --unbuffered -c .
./noe eval --events=- 'console.log("hello")' | jq --unbuffered -c .

# 通常の画面表示を保ち、ファイルにも記録する
./noe run --events=agent.jsonl try-agent.js
./noe repl --events=agent.jsonl

# 名前を自動生成する
./noe repl --events
```

agentを実行する例では、従来どおり`NOE_OPENROUTER_API_KEY_FILE`等による認証設定が必要です。`--events`自体はproviderを呼びません。オプションはスクリプト名またはevalコードより前へ置きます。`--events`単独は次の引数を消費しません。ファイル名を指定するときは`--events=path`の形を使います。

自動生成名は`noe-events-20261017-153000-<ランダムID>.jsonl`です。1回のNOE起動につき1ファイルを作り、その絶対パスをstderrへ表示します。同じNOE内で複数の親agentや子agentを動かしても、保存先は共通です。

相対パスと自動生成先は、NOE起動時のcwd基準です。スクリプトの所在や実行途中のcwd変更には影響されません。絶対パスも指定できます。明示したファイルは追記で開き、自動生成ファイルは新規作成します。親ディレクトリは事前に用意してください。ファイルへの書込み・終了時のcloseに失敗した場合はstderrへ診断し、NOEは失敗終了します。

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

agent由来のイベントには`runId`・`parentRunId`・`name`・`model`が付きます。`runId`は`agent.run()`ごとに異なり、親のないrunの`parentRunId`は空文字です。子の`parentRunId`には親の`runId`が入り、親自身のIDは変わりません。スクリプト全体の通常出力には、特定agentのrun IDを割り当てません。

| `type` | 主な追加項目・タイミング |
| --- | --- |
| `execution.started` / `execution.finished` | CLI開始・終了。`command`または`exitCode` |
| `execution.error` | 実行エラー・タイムアウトの`text` |
| `stdout` / `stderr` | JSが出力した文字列断片の`text`。改行を含め元の文字列を保持 |
| `repl.result` / `repl.error` | REPL評価結果・エラーの`text` |
| `agent.started` / `agent.completed` / `agent.failed` / `agent.cancelled` | runの状態・終了時の`duration` |
| `model.started` / `model.finished` / `model.failed` | モデル要求の`requestId`。完了時は`finishReason`・`usage`・`duration`、失敗時は`reason`等 |
| `thinking` / `thinking_summary` | providerから返った推論本文・要約の断片`text`と`requestId` |
| `message_delta` | モデルの発言本文の断片`text`と`requestId` |
| `agent.message` | toolを呼ぶ応答に含まれる途中の発言`text`と`requestId` |
| `agent.final` | loopが正常な最終回答として受け入れた`text` |
| `run_js.started` | `toolCallId`と、codeの先頭行が`//`コメントならその行の`text` |
| `run_js.finished` | `toolCallId`・`success`・`exitCode`・`reason`・`duration` |

`duration`はnanosecondsです。並列実行のイベントは到着順に並びますが、同じNOE内の書込みを直列化するため、JSON同士が文字単位で混ざりません。

thinking・summary・messageの断片はストリーム受信時に出ます。providerが返さない推論本文・要約は生成しません。暗号化されたreasoningや署名は表示用イベントへ出さず、次のモデル要求に必要な元のreasoning情報は従来どおり履歴へ保持します。`message_delta`の時点では最終回答か未確定なので、確定した回答が必要なら`agent.final`を読みます。本文の断片と確定イベントは重複表示せず、用途に応じて選びます。

通常のJS出力は改行なしでも書込みごとに通知します。UTF-8文字の途中で分割された書込みは、文字が揃ってから通知します。`child_process`が返す捕捉出力は、JSが表示した時点で通常出力になります。`run_js`内のstdout/stderrは現在もrunnerが捕捉し、tool完了時に`source: "run_js"`とrun/tool ID付きで記録します。tool内部の出力の逐次中継は行っていません。

## REPLと閲覧

REPLのプロンプト・補完・カーソル制御はイベントへ入れません。ファイル指定なら対話画面を保ち、評価結果・JS出力・agentイベントを記録します。`repl --events=-`ではJSONL専用のstdoutを確保するため、入力プロンプトと端末制御をstderr側へ出します。

```sh
tail -f agent.jsonl | jq --unbuffered -c .

# 名前で絞り込む
tail -f agent.jsonl | jq --unbuffered -c 'select(.name == "parent-A")'

# 特定runと直接の子を読む（孫以降は別途辿る）
tail -f agent.jsonl | jq --unbuffered -c \
  'select(.runId == "aaa" or .parentRunId == "aaa")'
```

人間向けの整形表示はGoの`noe observe`、親向けの状態・差分・結果取得は`noe:agent`の`observe_agent`を利用できます。`./noe run --events=- try-agent.js | ./noe observe`で逐次表示できます。使い方は[観察の入口](observe.md)を参照してください。外部JSによる表示も当初の[検討メモ](future-candidates.md#外部jsによるリアルタイム表示--2026-10-17)に残しています。SQLite保存、spawnによる子の途中監督・追加指示は別の検討項目です。

従来の`NOE_AGENT_TRACE=1`はメタデータのみをstderrへ出す機能として維持しています。詳細本文やtool出力を含む`--events`とは独立しており、従来のtraceの内容は変わりません。
