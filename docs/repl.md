# NOE REPLガイド

[READMEへ戻る](../README.md) · [APIリファレンス](api.md) · [実行例](../examples/README.md)

`./noe repl`で起動し、1行ずつJSを試せます。同じランタイムを使い続けるため、変数・関数・オブジェクトは次の入力でも使えます。

`./noe repl --events=agent.jsonl`で、対話画面を保ったままJS出力・評価結果・agentイベントをJSONLへ記録できます。`--events`単独なら起動時のcwdにファイル名を自動生成します。別ターミナルで`tail -f agent.jsonl`から読めます。プロンプトやカーソル制御は記録しません。`--events=-`でstdoutへJSONLを流す場合は、対話入力の表示先がstderrになります。詳細は[観察ガイド](events.md)を参照してください。

別ターミナルで`./noe observe --follow agent.jsonl`を使うと、JSONLをGoで整形して観察できます。REPLからも`const am = await import("noe:agent")`、`await am.observe_agent({logPath:"agent.jsonl", runId:"実際のrunId"})`で状態と結果を読めます。[観察の入口](observe.md)に使い方をまとめています。

`./noe repl --observe`なら、入力画面を保ちながら同じターミナルへ整形表示できます。`--events=agent.jsonl`も付けるとJSONLを記録します。`--observe`単独ではファイルを作りません。

```text
> let count = 1
> count += 1
2
> const fs = await import("fs/promises")
> const text = await fs.readFile("examples/data/sales-day-1.json", "utf8")
> const orders = JSON.parse(text)
> orders.length
4
> const sales = await import("./examples/lib/sales.js")
> sales.summarizeSales(orders).totalRevenue
3100
> process.exit()
```

式の結果を表示し、`undefined`は省略します。文字列の結果には引用符が付きます。通常のJSエラーや未処理のPromise rejectionは標準エラーに表示して続行し、実行済みの変更を巻き戻しません。

## OpenRouter agent

起動時にAPIキーのファイルパスを設定します。APIキーそのものをコマンドへ書かず、ファイルへ保存してください。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key ./noe repl
```

REPLでは静的importの代わりにdynamic importを使います。moduleをimportしたりagentを作ったりするだけでは、キー読込みやprovider通信は始まりません。

```text
> const agentModule = await import("noe:agent")
> const summaryAgent = agentModule.createAgent({provider:"openrouter",model:"deepseek/deepseek-v4.1-flash",effort:"auto",name:"summary"})
> const summary = await summaryAgent.run("README.mdを3点に要約してください。")
> console.log(summary)
```

`run()`は最終回答の文字列を返します。同じ`summaryAgent`を別の入力でも使えますが、各呼出しの会話履歴は独立し、前回の会話を保存・再開しません。`createAgent`と`run`のoption、子agentへの設定継承、認証pathとtraceは[APIリファレンス](api.md)を参照してください。

完了したprovider errorはその入力の失敗として表示され、次の入力へ進めます。model通信中にCtrl+Cを押すと要求を取り消し、進行中のI/Oを残さないようREPL sessionを終了します。agent通信を開始するProxy trap等をTab補完が評価した場合も、残った内部要求は取消し対象となりsessionが終了することがあります。通常のagent処理は入力中に人間の追加指示を待ちません。

非同期入力は`await expression`、`const name = await expression`・`let`・`var`の単一変数初期化、`name = await expression`に対応します。`const`は再代入できません。通常のPromise値を暗黙に結果値へ変換しないため、解決結果を使う場合は明示的に`await`してください。

非同期の初期化は右辺を待ってから変数を宣言します。同名の`const`・`let`が既にあっても、右辺のI/Oを実行した後に重複宣言のエラーになります。非同期の分割代入・複数変数宣言・任意の複数文の変換、静的`import`・`export`は提供しません。モジュールには例のように動的`import`を使います。対応するawait式・単一変数の初期化や代入は、改行を含んでも使えます。

JavaScriptの`noe`グローバルはありません。旧スクリプトの置換先は[移行表](api.md#旧apiからの移行)を参照してください。REPLの標準入力はソースに予約され、`process.stdin.read()`は`null`を返します。

各入力で開始したファイル・HTTP処理・タイマーと後続のJS処理を待ってから、次の入力へ進みます。パスは起動時のカレントディレクトリが基準です。REPLの`process.argv.slice(2)`は空配列です。

グローバルの`serve()`で起動したサーバーは稼働中に次のプロンプトへ進みません。応答が完了した後の別の処理から`server.close()`を呼ぶと停止します。同期的な停止と完了待ちについては[HTTPサーバーの説明](api.md#httpサーバー)を参照してください。稼働中にCtrl+Cで中断するとランタイムが閉じ、REPLセッションも終了します。

未完の関数・オブジェクト・括弧などを入力すると、`... `で続きを待ち、確定した入力をまとめて評価します。確定した構文エラーは診断して次の入力へ進みます。

```text
> function twice(n) {
...   return n * 2;
... }
> twice(21)
42
> _ + 1
43
```

`_`は最後に成功したundefined以外の結果です。最初はundefinedで、エラー・中断・undefinedの結果では更新しません。オブジェクトは参照を保持します。

対応する端末では、入力の途中を編集したり、前の入力を履歴から呼び出したりできます。履歴は次の起動でも使え、複数行の入力も1件として保存します。複数行の履歴を呼び出すと、改行やタブは`\n`・`\t`として表示しますが、評価には元のソースを渡します。既定の保存先はユーザー設定ディレクトリの`noe/repl_history.jsonl`です。Linuxでは`$XDG_CONFIG_HOME`、未設定なら`~/.config`の下になります。メモリ内の履歴は直近1000件で、保存ファイルへは追記します。

`NOE_REPL_HISTORY`で保存先を変更できます。空文字を指定すると、保存・読込みを無効にし、そのセッション内の履歴だけを使います。読込み・保存に失敗すると標準エラーへ通知し、REPLを続けます。

```sh
NOE_REPL_HISTORY='' ./noe repl
NOE_REPL_HISTORY=/tmp/my-noe-history.jsonl ./noe repl
```

| キー | 操作 |
| --- | --- |
| ←・→、Ctrl+B・F | 文字ごとに移動 |
| Home・End、Ctrl+A・E | 行の先頭・末尾へ移動 |
| Backspace・Delete | 文字を削除 |
| Alt+B・F、Ctrl+←・→ | 単語ごとに移動 |
| Ctrl+W、Ctrl+U・K | 前の単語、行の先頭まで、行末までを削除 |
| ↑・↓、Ctrl+P・N | 履歴を呼び出す。修正してEnterで再実行できる |
| Ctrl+R・S | 履歴を後方・前方へ検索。Enterで実行、Ctrl+Gで検索前の入力へ戻る |
| Tab | キーワード・グローバル名・成功した入力の宣言名・オブジェクトのプロパティを共通部分まで補完。進展しない場合は続けてTabを押すと候補一覧を表示 |
| Ctrl+L | 画面を消去 |

`process.ar`や`console.lo`を入力してTabを押すと、候補を補完できます。成功した入力のトップレベルの`let`・`const`宣言は、配列・オブジェクトの分割代入も含めて候補へ追加します。関数や`var`はグローバル名から取得します。失敗した入力内の部分宣言や、識別子のエスケープ表記の収集には対応しません。識別子とドットの列を対象とし、関数呼出しや添字を含む式は対象外です。プロパティのgetterは呼びません。Proxyのtrapが候補取得を妨げる場合は、その補完を中断して通常入力へ戻ります。

空の入力でCtrl+D（EOF）、または`process.exit()`で終了します。未完の入力を保持したままEOFになると、診断を表示して終了します。対応する端末でのCtrl+Cは、編集中・履歴検索中・複数行入力中の入力を破棄し、実行中の入力を中断します。タイマーだけが残る場合は`Interrupted`を表示して次のプロンプトへ戻ります。Fetchなどの標準ライブラリ処理が残る場合の動作は次の段落を参照してください。中断前の変更は残り、ファイルの変更は巻き戻しません。

go-quickjsのstdlibも利用できます。グローバルの`fetch`、`serve`、Workers、メッセージポートなどの処理が残った状態で中断やエラーが起きた場合は、ランタイムを閉じてREPLセッションも終了します。stdlibの処理はランタイム全体に結び付いており、入力単位の停止後に既存の関数やimportを安全に再利用できないためです。`setTimeout`・`setInterval`だけなら取り消してセッションを続行します。

パイプやリダイレクトした入出力、端末制御に対応しない環境では、通常の行読み取りを使い、履歴ファイルを扱いません。パイプ入力でも`> `や継続時の`... `を表示し、改行のない最終入力も評価します。非端末のSIGINTと入力待ちでの外部SIGINTはコード130で終了します。対話端末で評価中のSIGINTは入力中断として扱います。引数なしの`./noe`はhelpを表示します。
