# agentの観察：人間用CLIと親用API

実行イベントの保存形式は[JSONL](events.md)、人間向けの表示は読みやすいテキスト、親agent向けは構造化した状態・差分に分けます。組立てと表示はGoで行い、追加のLLMは呼びません。人間向けのMarkdown整形には、mdcli v0.0.5のソースから取り込んだ、Go標準ライブラリだけを使う整形処理を利用します。

## 人間向け：noe observe

`--observe`を付けると、パイプなしで実行中のイベントを人間向けに表示できます。

```sh
./noe run --observe try-agent.js '1から100までの合計を計算して'
./noe eval --observe 'console.log("hello")'
./noe repl --observe

# 画面はテキスト表示、ファイルには元のJSONLを保存
./noe run --observe --events=agent.jsonl try-agent.js '任意の指示'
# --events単独なら保存先をcwdに自動命名
./noe run --observe --events try-agent.js '任意の指示'
```

`--observe`だけではJSONLファイルを作りません。`--events=path`または`--events`を併用すると記録も残せます。stdoutには整形したイベントを出します。スクリプトと`run_js`のstdoutは人間向け表示から省き、JSの処理内容は先頭の1行コメントで確認します。NOE自体の実行診断はstderrへ出ます。REPLの入力画面はstdoutに保ちます。

JSONLを別の処理へ渡す場合は、従来どおり`--events=-`を使います。パイプ先の`noe observe`も同じ整形処理を利用します。

```sh
./noe run --events=- try-agent.js '任意の指示' | ./noe observe
```

`--observe`と`--events=-`は両方ともstdoutを使うため同時指定できません。表示とJSONL保存を両立する場合はファイルを指定します。agentを実行する例では、従来どおりOpenRouterの認証設定が必要です。保存済みログの観察自体はproviderへ要求しません。

保存済みのファイルを表示したり、追記を追ったりできます。

```sh
./noe observe agent.jsonl
./noe observe --follow agent.jsonl
tail -f agent.jsonl | ./noe observe

# 特定runだけ表示する。IDにはJSONLに記録された完全なrunIdを使う
./noe observe --run <runId> agent.jsonl
```

出力例：

```text
[parent-A/12345678] model: waiting for response
[parent-A/12345678/abcdef12] run_js started: Read README.md
[parent-A/12345678/abcdef12] run_js succeeded: 28ms
[parent-A/12345678] thinking: 次に結果をまとめます。
[parent-A/12345678] final answer
  READMEの要点は次の3つです。
  ...
[parent-A/12345678] completed
```

表示ラベルは英語を標準とし、モデルの本文は元の言語のまま表示します。`run_js`の先頭コメントも英語で書くようモデルへ指示します。名前と短縮run IDを付け、tool由来のイベントには短縮tool IDも付けます。thinking・summaryは受信時に同じrun・モデル要求・種類の断片をつなぎ、連続する間はラベルを最初の1回だけ表示します。改行・空行は保持し、続きの本文を2スペースで字下げします。別agent・別要求・別種類やtoolなどの表示が間に入ると、新しいラベルから表示します。完了まで溜めず、受信した本文を逐次表示します。途中の発言と最終回答は確定した本文を色なしのMarkdown整形で表示し、`message_delta`を重ねて表示しません。強調やリンクを読みやすいテキストにし、箇条書き・段落・表を整えます。コードブロックはハイライトせずインデント付きで表示します。stdoutの本文は表示せず、`run_js`の先頭コメントと成功・失敗を表示します。stderrは引き続き表示します。スクリプトが最終回答を`console.log`しても、人間向けには最終回答イベントだけを表示するため重複しません。

本文の幅は起動時の`COLUMNS`、出力先の端末幅、80桁の順で決めます。段落・箇条書きは単語の境界で折り返し、表は日本語の表示幅も数えてセルを折り返します。表が収まらない狭い幅では項目ごとの縦表示へ切り替えます。空白のない長い語やコードの行は幅を超える場合があります。CommonMark全体への準拠を目的としたレンダラーではなく、見出しの`#`やコードフェンスなど一部の記号は残ります。

整形するのは人間向けの途中の発言・最終回答だけです。thinking・stderr・JSコメントはそのまま表示します。stdoutは表示から省きますが、JSONLへの記録とモデルへ返すtool結果には残します。JSONLの本文、`noe observe --json`、`observe_agent`が返す本文は元のMarkdownを保持します。取り込んだファイル・ライセンス・差分は[出典](../internal/mdcli/README.md)を参照してください。

標準入力はEOFまで逐次読み、EOFで終了します。`--follow`はファイルへの追記を待ち、Ctrl+Cで終了します。追記途中の行は改行が届くまで保持します。通常表示はJSONLのままではなく、元のJSONLファイルを書き換えることもありません。

## 親向け：observe_agent

`noe:agent`から`observe_agent`をimportできます。現在は既存JSONLを読み取る入口で、`logPath`と`runId`を指定します。相対パスは実行時のcwd基準です。

```js
import { observe_agent } from "noe:agent";

const query = { logPath: "agent.jsonl", runId: "実際のrunId" };
const first = await observe_agent(query);
console.log(JSON.stringify(first));

// 状態は維持したまま、前回以降のイベントだけ受け取る
const next = await observe_agent({ ...query, cursor: first.cursor });

// 結果が必要になったら、完了まで待つこともできる
const settled = await observe_agent({ ...query, wait: true });
console.log(settled.finalText ?? settled.error ?? settled.status);
```

| 引数 | 意味 |
| --- | --- |
| `logPath` | 必須。既存のJSONLファイル |
| `runId` | 必須。観察するrunの完全なID |
| `cursor` | 前回返された`{executionId, seq}`。省略時は最初から |
| `wait` | 既定は`false`。`true`ならagentの完了・失敗・取消しまで待つ |
| `includeThinking` | 既定は`false`。差分へthinking・summaryを含める |
| `includeOutput` | 既定は`false`。差分へstdout/stderrを含め、直近toolの捕捉出力も返す |

戻り値には次の情報があります。

- `runId`・`parentRunId`・`name`・`model`
- `status`：`unknown`・`running`・`completed`・`failed`・`cancelled`
- `phase`：`unknown`・`starting`・`model`・`run_js`・`between_steps`・`settled`
- `action`：実行中または直近のJSコメント。`lastMessage`：直近の途中の発言
- `lastToolResult`：完了済みの直近toolのID・成功失敗・終了コード・理由・所要時間。実行中のtoolの結果ではない
- `finalText`：確定した最終回答。`error`：記録されているrunの失敗情報
- `usage`：そのrunでproviderから返った使用量の合計。`updatedAt`：最後に観察した関連イベントの時刻
- `events`：選択runの状態・発言・tool等の差分。空の場合は`[]`
- `cursor`：次回へ渡す実行IDとイベント番号

`events`はcursor以降に限定しますが、現在の状態や最終回答はファイル全体から再構成します。同じcursorを繰り返し読んでも共有の既読状態は進めません。rawの`message_delta`は返さず、thinking・通常出力も既定では含めないため、親へ細かい断片を毎回すべて渡す必要はありません。thinkingを要求した場合は、連続する同じrun・要求・種類の断片を結合します。

同じNOE内で親A・親B・子を動かしても、各runを分離して読みます。並列のrun_jsがある場合、一つのtoolが終わっても別のtoolが実行中なら`phase`は`run_js`のままです。toolが失敗しても、修復を続けているagent自体は`running`です。

cursorは取得対象の実行に属します。同じファイルへ別のNOE実行を追記してseqが戻っても混同しません。対象runと異なる実行のcursorはエラーになります。まだ対象runが記録されていない場合は`unknown`と空の差分を返し、返された初期cursorも後で再利用できます。

`wait: true`は100ms間隔でファイルを確認し、最終回答イベントだけでなくrunの終端まで待ちます。呼出し側の実行期限や取消しに従い、待機のGo処理は評価終了時に回収します。ログの更新が止まっていることだけでは終了・失敗と判定しません。非待機の読取りは開始時のファイルサイズまでを対象にするため、追記が続いても一度で戻ります。

シェルから構造化した状態だけ取得することもできます。

```sh
./noe observe --json --run <runId> agent.jsonl
./noe observe --json --run <runId> --wait agent.jsonl
```

`--json`はrun IDとファイルを指定する一回の状態取得です。`--wait`はこのモードで使い、`--follow`は人間向けの継続表示で使います。

`spawn_agent`と親による子の途中監督、steer・interrupt・queueはまだ検討段階です。今回の入口は既存ログの観察を可能にするもので、現在の`await child.run()`の完了待ちや生成JS終了時の子scope回収は変えていません。
