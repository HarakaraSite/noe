# NOE — No Opulence Engine

A tiny JavaScript/TypeScript runtime written in pure Go.

No bloat. Just scripts.

この文書は日本語版です。正本は[英語版README](README.md)です。
小さなスクリプトと自律agentのタスクを実行するGo製ランタイムです。JavaScriptで処理を書いたり、`run_agent()`へ仕事を頼んでファイル操作・JS実行・修正・回答まで任せたりできます。JavaScriptエンジンには[go-quickjs](https://github.com/go-quickjs/go-quickjs)を使います。

**現在の対応言語はJavaScriptです。TypeScriptは後から追加する予定です。** npm・パッケージ管理は対象外で、Node.jsやDenoの完全互換は目標にしていません。固定したgo-quickjs標準ライブラリのWeb APIとNode形式APIを有効にしていますが、各APIは一部実装です。

[APIリファレンス](docs/api.md) · [旧APIからの移行](docs/api.md#旧apiからの移行) · [REPLガイド](docs/repl.md) · [実行例](examples/README.md) · [開発記録](docs/README.md)

[v1.2.0リリースノート](docs/releases/v1.2.0.md) · [バイナリのダウンロード](https://forge.harakara.site/littleisland/noe/releases/tag/v1.2.0)

## ビルドと実行

Go 1.27.2以降が必要です。Cのツールチェーンは不要です。以下のコマンドはリポジトリのルートで実行します。

```sh
git clone https://forge.harakara.site/littleisland/noe
cd noe
CGO_ENABLED=0 go build -o noe ./cmd/noe
```

```sh
./noe --version
# noe v1.2.0

./noe run examples/hello.js
# Hello from NOE!

./noe eval 'console.log(1 + 1)'
# 2

./noe repl
```

## すぐに試す

標準入力のJSONを集計します。

```sh
printf '[100,200,300]\n' | ./noe run examples/summarize-stdin.js
# {"count":3,"total":600,"average":200}
```

ディレクトリ内のJSONファイルをまとめて集計します。

```sh
./noe run examples/collect-json.js examples/data/numbers
# ファイルごとの結果と、count: 5、total: 800、average: 160を表示
```

非同期の待機を使い、1秒間隔で3回表示します。

```sh
./noe run examples/sleep-demo.js
```

ファイル加工・レポート保存・HTTPダウンロードなどは[実行例の一覧](examples/README.md)を参照してください。

## OpenRouter agent

組み込みの`noe:agent`モジュールで、OpenRouterを使った自律agentを実行できます。agentは`run_js`でファイルの確認やJavaScript・コマンドの実行、失敗した処理の修正、子agentへの委譲を行います。モデルloopとprovider接続はNOEと同じOS権限で動きます。

```js
import { run_agent } from "noe:agent";

const snapshot = await run_agent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "summary",
  cacheKey: "readme-summary-v1",
}, "README.mdを読み、要点をSUMMARY.mdへ簡潔に保存してください。");
console.log(snapshot.lastResult?.answer);
```

公開関数は`run_agent`・`spawn_agent`・`observe_agent`・`queue_agent`・`steer_agent`・`interrupt_agent`です。`run_agent`はagent停止後にsnapshotを返し、taskの回答は`snapshot.lastResult.answer`から取得します。通常のtask失敗もsnapshotに記録されます。`spawn_agent`は起動と最初のtaskの受領後に`agentId`を返します。`observe_agent({agentId})`で現在の状態を取得できます。`{keepAlive:true}`で起動したagentには`queue_agent({agentId, instruction})`で別taskを渡せます。queueのtaskは毎回新しい会話履歴で実行します。

`steer_agent({runId, instruction})`は実行中taskへ指示を追加し、進行中のモデル要求やJavaScriptは止めず、次のモデル判断前に反映します。`interrupt_agent({runId})`はtaskとその子workだけを取消し、keepAlive instanceと他のqueueを残します。`interrupt_agent({agentId})`は実行中taskやqueueを含めinstance全体を停止します。いずれも受付後すぐ戻り、処理結果や回収完了は観察で確認します。待受agentは現在のNOE runが所有し、所有者の終了時に回収されます。

別のterminalからも稼働中のNOEを操作できます。

```sh
./noe agents
./noe steer <run-id> '結果を表形式にまとめてください'
./noe queue <agent-id> '続けてテストしてください'
./noe interrupt --run <run-id>
./noe interrupt --agent <agent-id>
```

`noe interrupt --run`はtaskだけを止め、`noe interrupt --agent`はinstance全体を止めます。詳しくは[APIリファレンス](docs/api.md)、[観察ガイド](docs/observe.md)、[操作例](examples/README.md#live-agent-control)を参照してください。

OpenRouter APIキーをテキストファイルへ保存し、そのパスをNOEへ指定します。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent-summary.js README.md
```

ファイルは最初のモデル要求時に読み込みます。環境変数が未設定なら、`$XDG_CONFIG_HOME/noe/credentials/openrouter-api-key`を使います。`XDG_CONFIG_HOME`も未設定の場合は`~/.config/noe/credentials/openrouter-api-key`です。moduleのimportや初期taskなしのidle起動では、キー読込み・provider通信は行いません。task付きの起動では、受領後に非同期でモデル要求を開始します。

ルートagentでは`provider`・`model`・`effort`を明示します。`effort`は`auto`・`none`・`minimal`・`low`・`medium`・`high`・`xhigh`・`max`を指定できます。子agentはprovider・model・effort・`maxTokens`を省略時に起動元runから継承します。他の設定は継承されません。`maxSteps`はtaskごとのモデル呼出し回数を制限します。`cacheKey`を省略するとtaskの`runId`をOpenRouterの`session_id`に使い、空文字なら`session_id`を省略します。task間で会話履歴は共有しません。

サンプルJSONを子agentが並行して集計する場合は、次の例を実行してください。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent-parallel.js
```

任意のタスクは[try-agent.js](try-agent.js)へ指示を引数で渡して実行できます。引数を省略すると、JSを使って1〜100の合計を計算します。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run try-agent.js "README.mdを読み、要点をSUMMARY.mdへ保存してください。"
```

モデルが生成する`run_js`では6つのagent関数が事前に束縛されています。importや再宣言は不要です。`run_js`ごとに新しいNOEプロセスが動くため、通常のJavaScript変数は呼出し間で保持されません。設定、操作、エラー、REPL、traceについては[実行例](examples/README.md)と[APIリファレンス](docs/api.md)を参照してください。

### agent実行の観察

人間向けには`--observe`で、パイプなしに読みやすく表示できます。thinkingの断片をつなぎ、agentとtoolを識別しながらJS実行結果や最終回答を表示します。途中の発言と最終回答は、表の整形を含む色なしのMarkdown表示になります。スクリプトとtoolのstdoutは表示から省き、`run_js`の先頭1行コメント・成功失敗とstderrを表示します。`--events=path`を併用すると元のJSONLも保存できます。

```sh
./noe run --observe try-agent.js '1から100までの合計を計算して'
./noe run --observe --events=agent.jsonl try-agent.js '任意の指示'
./noe observe --follow agent.jsonl
```

`observe_agent({agentId})`はlive snapshotをすぐ返します。保存済みJSONLには従来の`observe_agent({logPath, runId, cursor?, wait?, includeThinking?, includeOutput?})`でアクセスできます。観察自体はモデルを呼びません。両方の入口は[観察ガイド](docs/observe.md)を参照してください。

`run`・`eval`・`repl`共通の`--events`で、通常のJS出力と詳細イベントをJSONLへ記録できます。providerが返すthinking・summary、途中の発言、最終回答、`run_js`の先頭コメントと成功・失敗をrun ID付きで観察できます。`--events=-`はstdout、`--events=agent.jsonl`はファイルへ追記、`--events`単独は起動時のcwdに`noe-events-日時-ID.jsonl`を新規作成します。ファイル指定時は普段の画面表示を保ち、REPLのプロンプトや端末制御は記録しません。詳細は[観察ガイド](docs/events.md)を参照してください。

```sh
./noe run --events=- try-agent.js | jq --unbuffered -c .
./noe repl --events=agent.jsonl
```

`NOE_AGENT_TRACE=1`で実行メタデータをJSON Linesとして標準エラーへ出力します。最終回答とtraceは別ファイルへ保存できます。

```sh
NOE_AGENT_TRACE=1 NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent-summary.js README.md >answer.txt 2>agent-trace.log
```

`tail -f agent-trace.log`で実行状態を確認できます。traceにはrun ID・親run ID・agent名・model・状態・所要時間、返された場合はtoken使用量・cache量・costを記録します。標準エラーには通常の診断も入りうるため、全行がtraceとは限りません。生成JS本文、toolの標準出力・標準エラー、APIキーは記録しません。詳細は[APIリファレンス](docs/api.md)を参照してください。

## HTTPサーバー

小さなHTTPサーバーもJavaScriptで書けます。まず起動します。

```sh
./noe run examples/http-server.js 8080
```

別の端末から試します。

```sh
curl http://127.0.0.1:8080/health
curl -H 'Content-Type: application/json' \
  -d '{"message":"こんにちは"}' http://127.0.0.1:8080/echo
```

起動した端末のCtrl+Cで終了します。デモのポートは省略時8080、0なら空きポートで、起動時に実際のURLを表示します。

グローバルの`serve(options, handler)`はWebの`Request`を受け取り、`Response`を返すHTTPサーバーです。`new URL(request.url).pathname`で経路を判定し、JSON応答には`Response.json(value)`を使います。ハンドルにはURLと`close()`があり、詳しくは[APIリファレンス](docs/api.md#httpサーバー)を参照してください。

## CLI

| コマンド | 動作 |
| --- | --- |
| `noe run <file> [args...]` | ファイルを実行。スクリプト引数は`process.argv.slice(2)`で取得 |
| `noe eval <code>` | コードを実行。`process.argv.slice(2)`は空配列 |
| `noe repl` | 対話的にJavaScriptを実行 |
| `noe --help` | 使い方を表示。引数なしでも表示 |
| `noe --version` | バージョンを表示 |

`.js`と`.mjs`をESモジュールとして実行し、Promise・`async/await`・top-level awaitを使えます。ローカルimportはimport元のファイルが基準で、拡張子を明示します。ファイル操作と`eval`内の相対importはカレントディレクトリが基準です。

### 実行時間の上限

`run`・`eval`には任意の上限を指定できます。指定しなければ上限を設けません。

```sh
./noe run --timeout 5s examples/hello.js
./noe eval --timeout=500ms 'await new Promise(resolve => setTimeout(resolve, 60000))'
# 終了コード124、標準エラーへ時間切れの診断
```

`--timeout`はスクリプト名・コードの前に指定し、`500ms`・`5s`・`1m`などGoのduration形式の正の値を使います。空・不正・ゼロ・負・重複は使い方エラーです。`--`でオプションを終端できます。`process.argv`の先頭2項目は実行ファイル名とソース名で、その後にスクリプト引数が続きます。

エントリソースの読込み後、ランタイム初期化からJS評価・非同期I/O・sleep・サーバー待受・後続JSまでを計測します。`process.stdin.read()`などの同期ホストI/Oや、ローカルimportのOS読取りを強制停止する保証はありません。REPLはCtrl+Cで中断します。中断までの出力やファイルの変更は残ります。

## REPL

同じランタイムを使い続けるため、変数・関数は次の入力でも使えます。

```text
> let count = 1
> count += 1
2
> _ + 1
3
> await new Promise(resolve => setTimeout(resolve, 100))
> process.exit()
```

行編集、保存される履歴、Tab補完、複数行入力に対応します。`_`は最後に成功したundefined以外の結果です。通常の入力エラーの後も続行します。

空の入力でCtrl+D、または`process.exit()`で終了します。対応する端末でCtrl+Cを押すと入力や実行中の処理を中断し、通常は次のプロンプトへ戻ります。stdlibの通信・サーバー・Worker・メッセージポートなどが残った状態で中断やエラーが起きると、処理を停止してREPLセッションも終了します。タイマーだけなら取り消して続行できます。REPLで起動したサーバーは稼働中にプロンプトへ進まず、Ctrl+Cで停止します。

REPLの`await`は式、単一変数の初期化・代入に対応します。任意の複数文に含まれる`await`や静的importは提供せず、モジュールは`await import(...)`で読み込みます。操作と対応範囲は[REPLガイド](docs/repl.md)を参照してください。

## API

| 用途 | API |
| --- | --- |
| 自律タスク | `noe:agent`の`run_agent`・`spawn_agent`・`observe_agent`・`queue_agent` |
| 出力 | `console.log`・`console.warn`・`console.error` |
| 引数・環境・終了 | `process.argv`・`process.env`・`process.cwd()`・`process.exit()` |
| 標準入力 | `process.stdin.read()` |
| テキスト・バイナリ | `fs/promises`の`readFile`・`writeFile` |
| ディレクトリ・ファイル情報・移動 | `fs/promises`の`readdir`・`stat`・`lstat`・`mkdir`・`rename`・`rm` |
| HTTP | グローバルの`fetch()`・`serve()` |
| 待機 | `timers/promises`の`setTimeout` |

引数、戻り値、エラー、対応範囲は[APIリファレンス](docs/api.md)にまとめています。

## 組み込みモジュールと利用権限

`fs`・`process`・`os`・`path`・`child_process`・`dns`・`http`・`worker_threads`・`crypto`などのgo-quickjs標準ライブラリをimportできます。timer、Web Streams、WebSocket、Workersも利用できます。NOE独自の`noe:agent`はmain runtimeで使え、Workerからのimportは未対応です。JavaScriptのグローバル`noe`はありません。NodeやDenoの完全互換ではなく、npm・パッケージ導入・QuickJS本来の`std`モジュールも提供しません。bare importで解決できるのは組み込みモジュールだけです。ローカルのソースは相対`.js`・`.mjs` importを使います。

スクリプトには、実行中プロセスの環境変数とローカルファイルシステム全体、コマンド実行、DNS・ネットワーク通信、HTTP待受、WebSocket、Workersの権限を渡します。NOEはsandboxではなく、スクリプトは実行ユーザーのOS権限で動作します。

通常終了では、開始したI/O・タイマーと後続のJS処理を待ちます。サーバーが稼働中なら実行を継続します。`process.exit(code)`で実行を停止し、終了コードを指定できます。

## Fetch API

グローバルの`fetch()`はgo-quickjsのWeb APIで、`Request`・`Response`・`Headers`とストリーミングbodyを提供します。レスポンスbodyは一度だけ読み取れます。JSONを送る場合は、文字列化とContent-Type指定を明示します。

HTTP 4xx・5xxもレスポンスとして返し、通信失敗はPromiseのrejectになります。URLからのモジュールimportには対応しません。HTTPサーバーはグローバルの`serve()`から、WebSocketは標準ライブラリから利用できます。

[HTTP APIの詳細](docs/api.md#fetch) · [JSON APIへのリクエスト例](examples/README.md#json-apiへのリクエスト)

## エラーと検証

| run/evalの終了コード | 意味 |
| --- | --- |
| `0` | 成功 |
| `1` | 実行失敗・未処理のPromise rejection・SIGINTによる中断 |
| `2` | CLIの使い方の誤り |
| `124` | `--timeout`による時間切れ |

`process.exit(code)`で終了コードを指定できます。エンジンが提供するエラー位置とスタックを表示します。解決する処理が残っていない未完了のtop-level awaitも診断します。

REPLは回復可能な入力エラーの後も続行し、EOFで0を返します。入力読取り・エンジン内部の続行できない失敗や、対話端末でstdlibのI/Oが残った状態での入力エラー・中断は1、非端末や入力待ちでの外部SIGINTは130です。

```sh
go test ./...
go test -tags=e2e ./... -count=1
```

通常のE2EはCGOを無効にしてCLIをビルドし、別プロセスから実行します。一時ファイルとローカルHTTPサーバーを使い、LinuxではPTYによるREPLの操作も確認します。外部サービスには接続しません。実OpenRouter検証は別途`agent_live`タグと認証ファイルのパスを明示して実行します。[agent実装・検証記録](docs/increment-13-implementation.md)を参照してください。

設計、インクリメントごとの受入条件・検証記録、開発用コマンドは[開発記録](docs/README.md)を参照してください。

## ライセンス

NOEのライセンスは[MITライセンス](LICENSE)です。

依存する第三者のコード・データには、それぞれのライセンスが適用されます。
