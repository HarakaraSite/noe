# バイナリに内蔵されたAPI説明を調べる

`noe docs`は、そのバイナリに組み込んだNOEとNOE管理のstdlibに対応する英文API説明を返します。インターネット接続や認証、リポジトリの配置は不要です。v1.8.0以降で利用できます。

```sh
noe docs
noe docs stdlib
noe docs noe:fs
noe docs noe:process execFile
noe docs noe:sockets connect
```

引数なしで全canonical topic IDとkind・短い概要・CLI/JSの参照方法を載せた一覧、topicを指定すると概要と詳細memberの一覧、memberも指定するとtopic名・短い概要・該当する型宣言/availabilityの条件と指定detailを表示します。member取得では全overviewや他memberの説明を繰り返しません。返却objectの操作も、表示されたIDで参照できます。

```sh
noe docs noe:fs open
noe docs noe:fs FileHandle.read
noe docs noe:sockets Connection.close
```

通常のJSやWorker、agentの`run_js`からも同じ本文を取得できます。

```js
import { docs } from "noe:docs";
console.log(docs());
console.log(docs("noe:fs", "FileHandle.read"));
```

`docs()`は同期でMarkdown文字列を返します。全文を一度に読む必要はありません。module概要で用途と制約を確認し、必要なmemberを選んでください。For remaining Node-style modules, documented bare aliases return the same reference. Filesystem access uses only `noe:fs`.

stdlibは全対応moduleの概要とexport一覧を収録し、よく使うAPIから個別詳細を整備しています。個別詳細が未収録でも、そのAPIが未対応とは限りません。未登録のtopic/memberでは、CLIはstderrに説明を出して終了コード2、JSは`code: "ERR_DOC_NOT_FOUND"`のErrorをthrowします。JSで引数型が違う場合はTypeErrorです。

生成したJSを実行前に確認する場合は、[既存Biome／tscの利用手順](javascript-checks.md)を参照してください。ツールがない環境でもNOEは利用でき、実施できなかった検査は未検査として区別します。

## API情報と型宣言の管理

module概要にはcanonical名と別名、公開export、情報の所有者、型宣言のpath、Profileやbindingの必要条件を表示します。型宣言は[noestdlib](../stdlib/types/noestdlib.d.ts)と[noeagentlib](../agent/types/noeagentlib.d.ts)が別々に管理します。`noe:agent`・model・image・decision・auth・logはagentlib側です。対応するsource版の2ファイルを、必要に応じて既存tscから参照します。取得・設定例は[JS検査ガイド](javascript-checks.md#noe-apiの宣言ファイル)を参照してください。

docsの索引は両方を案内します。情報取得で対象moduleを実行したりproviderへ接続したりせず、型の統合や新しい検索APIも行いません。公開moduleのAPI情報が載っていても、現在のProfileにそのmoduleがある保証にはなりません。例えば`noe:http`はFetch/Serveの選択に応じてexport自体が変わり、型宣言は全対応範囲を記述しています。詳細member一覧は説明のある項目であり、公開exportの全一覧とは別です。

`noe:run_js`はagent toolを説明するtopicです。同名のmoduleをimportすることはできません。NOEの起動方法や設定は[agent起動ガイド](agents.md)・[APIとCLIのガイド](api.md)、APIの使い分けは[プロセスとストリーム](process-io.md)・[ファイル・ソケット・シグナル](low-level-io.md)を参照してください。

## run_jsの実用例を調べる

`noe docs noe:run_js`で、入力・結果の契約と作業別の例を選べます。`files`はファイル読書きと一覧取得、`commands`はコマンド実行と検索、`imports`はローカルJSの読込み、`http`はHTTP取得、`agents`は子agentの実行・queue・完了待ち、`failures`は失敗時の確認手順です。

```sh
noe docs noe:run_js files
noe docs noe:run_js agents
noe docs noe:fs readdirSync
```

The agent instructions include common operations and API differences. `noe:fs.readdirSync`/`readdir` return names by default; `{withFileTypes:true}` returns Dirent objects with type predicates.Use global `process` or `import {process} from "noe:process"` for `process.cwd()`. `execFile`は既にPromiseを返すので、`promisify`やcallbackは不要です。

## HTTPの契約を調べる

Use `noe docs globals fetch` for standard Fetch, request/response streams, abort, and response-body overflow errors. Use `noe docs noe:http serve`, `noe docs noe:http upgradeWebSocket`, and `noe docs noe:http createWebSocket` for host listeners and WebSocket helpers. These helpers are explicit imports rather than globals; the old HTTP modules and `noe:http.fetch` export are removed. See the [HTTP migration](development/stdlib-noe-http.md).

`noe docs noe:http HTTPServer.close` and `noe docs noe:http "HTTPServer[Symbol.asyncDispose]"` describe asynchronous server cleanup. Request-limit overflow and migration from synchronous close are recorded in the [Serve lifecycle contract](development/stdlib-serve-lifecycle.md).
