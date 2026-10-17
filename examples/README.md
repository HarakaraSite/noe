# NOE 実行例

[READMEへ戻る](../README.md) · [APIリファレンス](../docs/api.md) · [REPLガイド](../docs/repl.md)

以下のコマンドは、ビルド済みの`./noe`を使い、リポジトリのルートで実行します。ファイル操作の相対パスは`process.cwd()`が基準です。スクリプト引数は`process.argv.slice(2)`で取得します。

通常の実行例はgo-quickjs標準ライブラリを使います。agent例は組み込みの`noe:agent` moduleからOpenRouterへ要求します。旧`noe.*` APIは廃止済みで、置換先は[移行表](../docs/api.md#旧apiからの移行)にまとめています。

## 一覧

| スクリプト | 内容 |
| --- | --- |
| [hello.js](hello.js) | 最小の出力 |
| [transform-json.js](transform-json.js) | ローカルモジュールを使ってJSONを加工・保存 |
| [sales-report.js](sales-report.js) | 複数ファイルを読み、JSONとMarkdownの売上レポートを保存 |
| [summarize-stdin.js](summarize-stdin.js) | 標準入力の数値配列を集計 |
| [collect-json.js](collect-json.js) | ディレクトリ内のJSON数値配列を集計 |
| [process-json.js](process-json.js) | 集計レポートを保存し、処理済み入力を移動 |
| [fetch-json.js](fetch-json.js) | HTTPでJSONを取得・保存 |
| [download-file.js](download-file.js) | HTTPでバイナリを取得・保存 |
| [http-json.js](http-json.js) | JSON APIへのリクエスト |
| [http-server.js](http-server.js) | healthとJSON echoのHTTPサーバー |
| [sleep-demo.js](sleep-demo.js) | 非同期の待機を使った定期表示 |
| [repl-demo.js](repl-demo.js) | REPLへ1行ずつ貼り付けて試す計算例 |
| [agent-route-model.js](agent-route-model.js) | カタログの候補からJevが選び、そのモデルで親agentを起動 |
| [agent-openai.js](agent-openai.js) | ChatGPTサインインでOpenAI Sol/LunaのResponses agentを実行 |
| [agent-opencode-go.js](agent-opencode-go.js) | OpenCode GoのChat / Responsesモデルを選んでagentを実行 |
| [agent-summary.js](agent-summary.js) | `run_agent`で指定ファイルを読み、snapshotから要約を表示 |
| [agent-parallel.js](agent-parallel.js) | `spawn_agent`で二つの子agentを起動・観察し、親agentが結果をまとめる |
| [agent-control.js](agent-control.js) | keepAliveのroot agentを別terminalからsteer・queue・interrupt |
| [try-agent.js](../try-agent.js) | 引数のタスクをagentへ依頼。引数なしならJSで1〜100の合計を計算 |

`lib/`は例で使うモジュール、`data/`はサンプル入力です。agentの並行例は[左のJSON](data/agent/left.json)と[右のJSON](data/agent/right.json)を使います。

## OpenRouter agent

OpenAIは`./noe run --observe examples/agent-openai.js gpt-6-luna`、または`gpt-6.1-sol`で実行します。先に[ChatGPT認証ヘルパー](../docs/openai.md)で登録するか、既存のcredential recordを用意します。

`./noe run --observe examples/agent-route-model.js openrouter 'README.mdを3点に要約して'`は、カタログ取得・Jevによるモデル選択・親agentの起動を組み合わせた例です。候補の説明は[外部JSONで上書き](../docs/model-catalog.md)できます。

OpenCode Goは`NOE_OPENCODE_GO_API_KEY_FILE=/path/to/key ./noe run examples/agent-opencode-go.js`で実行できます。第二のモデル例は`examples/agent-opencode-go.js gpt-6-luna`です。[組込みデフォルトと外部上書き](../docs/providers.md)も参照してください。

agent例はOpenRouterへモデル要求を送ります。APIキーをテキストファイルに保存して、ファイルのpathを設定します。key valueをscriptやshell commandへ直接記述しません。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent-summary.js README.md
```

`agent-summary.js`は既定で`README.md`を読み、簡潔な日本語の要約を標準出力へ表示します。別のファイルは引数で指定できます。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent-parallel.js
```

並行例の既定入力は`examples/data/agent/left.json`と`examples/data/agent/right.json`です。必要なら二つのpathを引数で置き換えられます。モデル出力の文章や形式は変わる場合があります。`run_agent()`はinstance snapshotを返し、例は`snapshot.lastResult.answer`を表示します。並行例は`spawn_agent()`で子IDを受け取り、`observe_agent({agentId})`で終了までの状態を確認します。

ルートagentには`provider`・`model`・`effort`を明示指定します。子agentはprovider・model・effort・`maxTokens`を省略時に起動元runから継承します。他のconfig項目は継承しません。`NOE_OPENROUTER_API_KEY_FILE`を省略した場合の既定path、新しい関数APIの設定・戻り値・エラー、keepAliveとqueue、REPL、任意の実行timeout、traceは[APIリファレンス](../docs/api.md)を参照してください。agentと生成JavaScriptはNOEと同じOS権限で動きます。

任意のタスクを試す場合は、ルートの`try-agent.js`へ指示を渡します。次の例はREADMEを読み、要約ファイルを作らせます。

```sh
NOE_AGENT_TRACE=1 NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run try-agent.js "README.mdを読み、要点をSUMMARY.mdへ保存してください。" \
  >answer.txt 2>agent-trace.log
```

`tail -f agent-trace.log`で実行状態を確認できます。traceは状態・使用量などのメタデータで、生成JS本文やtoolの出力を含みません。標準エラーには通常の診断も入りえます。

## 意思決定モデル

`decision-classify.js`は一つの文章をチーム別に分類し、providerの応答JSON全体を表示します。第一引数が判断対象、第二引数がモデルで、モデル省略時は`typesafe/jev-1.14`を使います。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/decision-classify.js 'Checkout failed.' typesafe/jev-1.14
```

モデルへ渡すtoolを増やさず、agentの`run_js`からも`await import("noe:decision")`で利用できます。並列化、画像part、REPL、失敗・取消し、観察は[意思決定APIガイド](../docs/decision.md)を参照してください。

## Live agent control

この例は最初のtaskを45秒間実行し、その後も`keepAlive`でidleになります。OpenRouter keyを設定して一つ目のterminalで起動します。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run --observe examples/agent-control.js
```

二つ目のterminalからactive instanceのIDを調べます。

```sh
./noe agents
```

最初の45秒以内に`RUN ID`を指定してsteerすると、実行中のtimerは続き、指示はその後のモデル判断で適用されます。taskがidleになった後はsteerを受け付けないため、別taskは`queue`で追加します。

```sh
./noe steer <run-id> '回答にこの文を含めて: control message received'
./noe queue <agent-id> 'README.mdの目的を一文で説明してください'
```

`./noe interrupt --run <run-id>`はそのtaskだけを止め、instanceをidleにします。実行を完全に終了するには`agentId`を指定します。

```sh
./noe interrupt --agent <agent-id>
```

操作CLIは受付結果を返し、task完了や回収を待ちません。`--observe`のinstance/task表示やJSONLから実際の進行を確認できます。別の利用例・API引数は[APIリファレンス](../docs/api.md#実行中taskへの指示と停止)と[観察ガイド](../docs/observe.md)を参照してください。

## JSONの加工と保存

```sh
DEMO_DIR=$(mktemp -d)
printf '{"value":21}\n' > "$DEMO_DIR/input.json"
./noe run examples/transform-json.js "$DEMO_DIR/input.json" "$DEMO_DIR/output.json"
cat "$DEMO_DIR/output.json"
```

入力のプロパティを保持し、`processed: true`を追加します。保存先の親ディレクトリは事前に用意します。

## 売上レポート

```sh
DEMO_DIR=$(mktemp -d)
REPORT_TITLE='今日の売上' ./noe run examples/sales-report.js "$DEMO_DIR/sales-report"
cat "$DEMO_DIR/sales-report.md"
```

既定のサンプル2ファイルを並行して読み、キャンセル済み注文を除いて集計します。JSONとMarkdownを保存し、集計結果も表示します。`REPORT_TITLE`はレポートのタイトルです。

引数なしなら保存先の接頭辞は`/tmp/noe-sales-report`です。入力を指定する場合は、接頭辞の後にファイルを並べます。

```sh
./noe run examples/sales-report.js "$DEMO_DIR/custom-report" examples/data/sales-day-1.json
```

## 標準入力とディレクトリの集計

```sh
printf '[100,200,300]\n' | ./noe run examples/summarize-stdin.js
# {"count":3,"total":600,"average":200}

./noe run examples/collect-json.js examples/data/numbers
# ファイルごとの結果と、count: 5、total: 800、average: 160を表示
```

入力は有限の数値だけを含むJSON配列です。空配列のaverageはnullです。ディレクトリ集計は直下のファイル名が`.json`で終わるものを名前順に調べます。`readdir()`は名前だけを返し、`lstat()`で通常ファイルを確認します。シンボリックリンクは対象にしません。再帰しません。

出力を保存し、次のスクリプトの標準入力として渡すこともできます。

```sh
DEMO_DIR=$(mktemp -d)
./noe run examples/collect-json.js examples/data/numbers > "$DEMO_DIR/summary.json"
./noe eval 'const report = JSON.parse(process.stdin.read()); console.log(report.total)' < "$DEMO_DIR/summary.json"
# 800
```

## 処理済みファイルの移動

この例は入力ファイルを移動します。サンプルを作業ディレクトリへコピーして試します。

```sh
DEMO_DIR=$(mktemp -d)
mkdir "$DEMO_DIR/input"
cp examples/data/numbers/*.json "$DEMO_DIR/input/"
./noe run examples/process-json.js "$DEMO_DIR/input" "$DEMO_DIR/output"
cat "$DEMO_DIR/output/report.json"
ls "$DEMO_DIR/output/processed"
```

入力と出力は別のディレクトリを指定します。全入力の解析後、出力ディレクトリを作り、一時レポートを`report.json`へ名前変更し、入力を`processed/`へ移動します。成功すると作業マーカーを削除します。

`mkdir(path, {recursive: true})`で出力先を作ります。既存の`report.json.tmp`・`.processing`は上書きします。名前変更の置換可否はOSに従い、Linuxでは同名の通常ファイルを置換します。ファイルシステムをまたぐ移動はコピーで補いません。処理はトランザクションではなく、失敗・中断までの変更や作業ファイルは残ります。

## HTTPダウンロード

URLは利用するAPI・ファイルに置き換えてください。保存先の親ディレクトリは事前に用意します。

```sh
DEMO_DIR=$(mktemp -d)
./noe run --timeout 5s examples/fetch-json.js https://example.com/api/data.json "$DEMO_DIR/data.json"
./noe run --timeout 5s examples/download-file.js https://example.com/image.png "$DEMO_DIR/image.png"
```

JSONは解析して整形保存し、バイナリは文字列に変換せず保存します。グローバル`fetch()`のレスポンスbodyは一度だけ読み取ります。どちらの例もHTTPエラーを実行失敗にします。

## JSON APIへのリクエスト

URLとJSONの項目は利用するAPIに合わせて置き換えてください。

```sh
ITEM_URL='https://example.com/api/items/1'
./noe run --timeout 5s examples/http-json.js PUT "$ITEM_URL" '{"name":"日本語","count":1}'
./noe run --timeout 5s examples/http-json.js PATCH "$ITEM_URL" '{"count":2}'
./noe run --timeout 5s examples/http-json.js DELETE "$ITEM_URL"
```

JSON-bodyがある場合は解析して検証し、明示的に文字列化してapplication/jsonで送ります。成功時は`{status, ok, body}`のJSONを表示し、空レスポンスのbodyはnullです。この例はHTTP失敗を診断してコード1で終了します。`fetch`自体は4xx・5xxもレスポンスとして返します。レスポンスbodyは`text()`で一度だけ読み取ります。

## HTTPサーバー

一つ目の端末で起動します。

```sh
./noe run examples/http-server.js 8080
```

別の端末から試します。

```sh
curl http://127.0.0.1:8080/health
curl -H 'Content-Type: application/json' \
  -d '{"message":"こんにちは"}' http://127.0.0.1:8080/echo
```

healthは`ok`、echoは送ったJSONを`Response.json()`で返します。経路は`new URL(request.url).pathname`で取得します。不正JSONは400、その他のパスは404です。ポート省略時は8080、0ならOSが空きポートを選び、起動時に実際のURLを表示します。起動した端末のCtrl+Cで終了します。

## 待機とREPL

```sh
./noe run examples/sleep-demo.js
```

`timers/promises`の`setTimeout()`を使い、`実行 1`から`実行 3`まで1秒間隔で表示します。待機中もHTTPやファイル処理を進められます。

[repl-demo.js](repl-demo.js)を`./noe repl`へ1行ずつ貼り付けると、変数・関数の保持を試せます。↑で入力を呼び出し、割引率を編集して再実行できます。詳細は[REPLガイド](../docs/repl.md)を参照してください。
