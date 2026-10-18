# NOE インクリメント10 共通イベントループと簡易HTTPサーバー

> この文書は実装当時の計画・検証記録です。現在の仕様は[APIリファレンス](api.md)と[stdlib移行記録](increment-12.md)を参照してください。旧`noe.*` API、独自I/O実装、当時の対象外・中断動作は現行仕様ではありません。

JavaScriptで小さなHTTPサーバーを書けるよう、NOEの非同期処理を共通イベントループへ整理し、`noe.serve()`を追加する。txiki.jsのように、ランタイム側が公開APIとホスト機能を持つ構成を採用する。Go 1.27.2、go-quickjs v0.25.3、CGO無効ビルドを維持し、依存は追加しない。状態は全スライスの実装・テスト・通常／批判的レビュー・総合E2E完了。

## 方針と実装の分担

ファイル・通信はGoの標準ライブラリを使い、公開API、実行の継続、キャンセル、リソースの終了はNOEが管理する。go-quickjsはJS実行、値変換、Promiseとホスト処理の接続に使う。`stdlib.Install()`・`stdlib.Files()`・`stdlib.Network()`・`stdlib.Servers()`・`stdlib.Loop`は導入せず、既存のconsoleでのstdlib利用は継続する。ファイル操作とfetchの公開仕様は変更しない。

既存の`startAsyncWork`、`drain`、`drainCancelled`と、run/REPLのawait待機を共通の管理へ整理する。go-quickjsの`AsyncWork`・`Busy`・`Wake`・`RunJobsContext`を土台にし、エンジンが持つキューを利用する。未完了のtop-level await中もNOEの未処理rejection・ホストエラーを監視する。将来の別エンジン対応を目的とする抽象層や、汎用のI/Oポーラーは追加しない。

```text
Go側のファイル・HTTP処理
    ↓ 完了またはリクエストをPost
NOE共通イベントループ
    ↓ 所有goroutineでJS呼出し・値変換・Promise/jobs処理
JSの続行、またはGo側へHTTP応答を返す
```

HTTPサーバーはGoの`net/http`で実装し、共通ループへ繰り返しイベントを届ける。JS実行は単一の所有goroutineに集める。接続やI/Oは並行し、asyncハンドラーが待機している間は別のリクエストを処理できる。CPUを使う同期JSは他のJS実行を待たせる。

## 共通イベントループの契約

ランタイムごとに一つの管理を持ち、一回で完了する処理と継続的なイベント源を扱う。

- ファイル・fetch・stdinは一回の処理として登録し、完了通知とPromiseの解決まで実行を継続する。既存の未await処理の完了待ちを保つ。
- HTTPサーバーは継続的なイベント源として登録する。待受中と、受付済みのリクエストの処理・応答・接続の終了が残る間は実行を継続する。
- タスクやイベント源がなく、キュー上のJS処理もなくなれば通常終了する。解決する処理のない未完了のtop-level awaitは、既存どおり失敗にする。
- 継続イベント源が稼働中でも、jobs処理後に未処理rejectionとホスト側の実行エラーを確認する。通常終了まで診断を先送りせず、その実行を失敗させてリソースを停止する。ハンドラーのrejectは後述の500応答経路で捕捉する。
- I/OのgoroutineはGoのデータを扱う。JS値の生成・参照、ハンドラー呼出し、Promiseの解決、診断の出力は所有goroutineで行う。
- `AsyncWork.Post`は複数回の配送に使い、`Complete`は一回の処理に使う。Done後・runtime終了後の配送ではcallbackが投稿側で呼ばれ得るため、成功した配送と生存確認を通った場合だけJSへ触れる。
- 正常完了、受付停止、入力中断、実行期限、明示終了を区別する。リソース解放は複数回呼ばれても成立し、終了後の遅延通知でJSを続行しない。

Go側のリソース停止をJS所有goroutineの進行に依存させない。特に終了hookや`noe.exit()`から、JSハンドラーの完了を同期的に待たない。共通管理のファイル分割は`internal/runtime/eventloop.go`を中心にし、HTTP固有の接続管理はサーバー側へ置く。

## 公開APIの計画

```js
const server = noe.serve({ hostname: "127.0.0.1", port: 8080 }, async request => {
  if (request.method === "POST" && request.path === "/echo") {
    const value = await request.json();
    return {
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(value),
    };
  }
  return { status: 200, body: "Hello from NOE\n" };
});
console.log(server.url);
```

### 起動とサーバーハンドル

`noe.serve(options, handler)`は同期的に待受を開始してサーバーハンドルを返す。Promiseではない。optionsは`hostname`・`port`を持つオブジェクトとし、hostnameの既定値は`127.0.0.1`、portの既定値は`0`。port 0はOSが空きポートを選ぶ。hostnameは空でない文字列、portは0〜65535の整数、handlerは関数とする。未対応のoptions、型違い、待受の失敗はthrowし、起動に失敗したlistenerを残さない。

| ハンドルのAPI | 契約 |
| --- | --- |
| `server.hostname` | 実際にbindしたIPアドレス |
| `server.port` | 実際に待受するポート番号 |
| `server.url` | bindしたアドレスから作るHTTP URL。IPv6はブラケットを付ける。ワイルドカードbindでは接続に使うホストを利用者が選ぶ |
| `server.close()` | 新規受付を停止し、listenerを解放してundefinedを返す。受付済みの処理と応答の完了は共通ループが待つ。繰り返し呼べる |

`close()`は全ハンドラーの完了を待つPromiseを返さない。ハンドラー内で`server.close()`を呼んでから応答を返せる。複数のサーバーを起動でき、一つを閉じても他のサーバーの受付は継続する。

### リクエスト

リクエストbodyをGo側で全体取得してから、独自のJSオブジェクトをハンドラーへ渡す。`Request`・`Headers`クラスは追加しない。

| リクエストのAPI | 契約 |
| --- | --- |
| `request.method` | GoのHTTPサーバーが受け取ったメソッド文字列。fetch側の対応メソッド範囲とは独立 |
| `request.url` | HTTPスキーム、Host、リクエストURIから作る絶対URL文字列。パスとqueryのエスケープ表記を保持 |
| `request.path` | GoのURL.Pathによるデコード済みのパス文字列。queryを含まない。URLクラスを追加せず、単純なパス分岐に使える |
| `request.headers` | 小文字の名前をキーとする文字列配列のオブジェクト。同名ヘッダーの値を保持し、Hostも含める |
| `request.text()` | 全bodyをPromise<string>で返す。既存fetchと同じ文字列化を使い、反復して呼べる |
| `request.json()` | 全bodyをJSON解析し、Promiseで返す。解析失敗はreject。反復して呼べる |
| `request.bytes()` | 全bodyのPromise<Uint8Array>。毎回独立したコピーを返す |

空bodyはtextで空文字、bytesで長さ0、jsonでは解析失敗となる。返した配列を変更・detachしても、後のbytes/text/jsonに影響しない。読み取りに失敗したリクエストはハンドラーへ渡さず、接続が生きていれば400を返す。

### 応答とエラー

ハンドラーは応答オブジェクト、またはそれを解決するPromiseを返す。`Response`クラスは追加しない。

| 応答のフィールド | 契約 |
| --- | --- |
| `status` | 省略時200。200〜599の整数 |
| `headers` | 省略時空。名前をキー、文字列または文字列配列を値とするオブジェクト。HTTPヘッダーとして不正な名前・値はエラー |
| `body` | 省略・undefinedなら空。文字列またはUint8Array。Uint8Arrayの型判定とビュー範囲は既存writeFileに合わせ、Goへ渡す前にコピーする |

JSONはスクリプトが明示的に文字列化し、Content-Typeを指定する。HEAD、204、304ではGoのHTTP応答規則に従いbodyを送信しない。

ハンドラーのthrow、Promiseのreject、不正な応答は、そのリクエストを500・`Internal Server Error`で応答し、元のエラーをstderrへ診断する。未処理rejectionとしてアプリ全体を失敗させず、次のリクエストを処理する。JSエンジンの致命的エラー、実行contextの中断、`noe.exit()`は通常のハンドラーエラーと区別する。待受が予期せず失敗した場合は共通ループへ実行エラーを届け、run/evalはコード1、REPLは当該入力の診断とサーバー停止を行う。

クライアント切断時はGo側のbody読取り・応答待ちを解放し、その接続への遅延応答を破棄する。他のリクエストと共有するJS実行全体は割り込まない。すでに開始したJSやファイル・fetch処理は実行contextの範囲で続行でき、切断による巻戻しはしない。

## 停止とREPLの動作

通常の`server.close()`は受付停止と処理中の応答を分ける。GoのShutdown完了も管理し、JSハンドラーが結果を返しただけでruntimeを閉じて、Go側の応答flushを途切れさせない。受付済みのbody読取り、配送済み・配送待ちのハンドラー、未完了のPromise、Goへの応答引渡し、接続終了のどの段階でも、計数と終了通知を一度だけ処理する。ハンドラーからcloseしても自分自身の終了待ちにならないことをスパイクで確認する。

`--timeout`はサーバーの待受を含む実行全体に適用し、期限満了は既存の診断・コード124。SIGINTによるrun/eval終了は既存のコード1、`noe.exit(code)`は指定コードを維持する。中断と明示終了ではサーバーと接続を閉じ、応答完了を待たない。mainの評価エラーや未処理rejectionで実行が失敗する場合もサーバーを解放する。既存の同期OS syscall強制停止の制限は引き継ぐ。

REPLは入力で開始した処理を待つ方式を維持する。サーバーを作った入力は待受中に次のプロンプトへ進まない。別の端末からHTTPリクエストを送り、Ctrl+Cでその入力のサーバーとI/Oを中断して、宣言・変数を保持したままプロンプトへ戻る。close済みのハンドルに再度closeでき、同じポートで再起動できる。通常のファイル・fetchのawait、エラー回復、補完、EOF、`noe.exit()`の動作も維持する。

REPLの通常評価・結果表示エラーでも、入力のサーバーを先に停止してからdrainする。単発I/Oの既存の完了待ちは維持し、通常エラー後の診断・再評価・再bindを確認する。未処理rejectionやホストの実行エラーは即座に返し、当該入力contextを取消し、queued jobsと遅延completionを片付ける。停止中の単発I/Oが診断と回復を妨げないようにする。

## スライスと受入条件

前段のスパイクで、現依存の複数回Post、最後のイベント源終了時のWake、キャンセル後の配送、ハンドラー内closeとHTTP応答flushを実行確認する。作業用コードは一時ファイルへ置き、結果と確定した実装方針をこの文書へ記録する。契約を満たせない場合は実装前に計画を修正する。

| スライス | 範囲 | 完了条件 |
| --- | --- | --- |
| 1 | 共通ループ、単発処理と継続イベント源、実行・入力contextと終了管理 | 同一イベント源から繰り返し配送でき、最後の源を閉じた後にqueued jobsを処理して終了する。複数源、キャンセル後の遅延配送、未await処理、未完了await診断を確認。既存ファイル・fetch・stdin・REPL中断・timeoutを維持 |
| 2 | net/http接続、noe.serve、リクエスト・応答の変換、ハンドラーエラー | 同期・asyncハンドラー、query・ヘッダー・JSON・空body・全バイト・部分ビューを実HTTPで確認。反復読取りとコピーの独立性、不正起動・ポート競合・不正応答、500の後の正常応答を確認。待機中のasyncハンドラーに別のリクエストが応答できる |
| 3 | close、稼働継続、複数サーバー、実行終了とREPL | エントリ評価後も待受を継続し、ハンドラー内closeの応答が完全に届いてコード0で終わる。一部の源の停止、受付済み処理、切断、mainエラー、timeout、SIGINT、noe.exit、終了後の再bind、遅延通知を確認。REPLのCtrl+C後に評価・再起動できる |
| 4 | 実用例、README、実バイナリE2E | 小さなJSサーバーを別プロセスから利用できる。各スライスのレビュー後、サーバーと既存CLI/API/REPL/端末操作を含む総合E2Eを完了する |

スライス1〜3の主な対象は`internal/runtime`と対象テスト。HTTPの実装は`serve.go`、変換処理は必要に応じて`serve_request.go`・`serve_response.go`へ分け、既存binary・async_resultの境界を再利用する。スライス4は`examples/http-server.js`、README、`tests/serve_e2e_test.go`を中心とする。CLI/REPL側は既存の中断経路への接続に必要な変更に限る。

## レビューと検証

計画と各スライスは通常／批判的レビューを行う。通常レビューはAPIと既存契約の整合、実現可能性、実用例を確認する。批判的レビューはJS所有goroutine、イベント源の生存期間、受付停止と配送の競合、応答flush前の終了、closeの循環待ち、終了後のJSアクセス、キャンセル後のREPL回復を確認する。実装レビューには確認したコードとテスト結果を渡す。初回各15分、再レビューは変更箇所と既存findingに限って一回15分以内。親agentが採否と最終検証を担当する。

テストはローカルのloopbackと一時ディレクトリを使う。通常はport 0で取得した実ポートを使い、競合テストだけlistenerを保持して衝突を作る。起動完了は出力したURLなどの通知で確認し、固定sleepに依存しない。並行処理と停止はチャネルや実リクエストで順序を作り、結果・body全体・終了コード・再bindで利用者の動作を確認する。有限の外側期限と必ず実行するcleanupを設ける。外部サービスへ接続しない。

サーバーハンドラーから既存のファイルAPIやfetchをawaitする経路、待受中とハンドラー内JS無限ループのtimeout、明示終了、クライアント切断、REPLの実Ctrl+Cも実バイナリで確認する。既存のstdin借用Readerの非所有、FIFO中断、遅延completion抑止、_・履歴・補完・端末設定復元を回帰検証する。

各スライスで対象テストとレビューを完了し、最後に次を行う。並行処理を追加するため、通常のruntime/CLI raceに加え、raceビルドした実CLIでサーバーの並行処理と終了のE2Eを確認する。通常のE2Eは既存どおりCGO無効ビルドを使い、race用の一時overlayやログは追跡しない。

```sh
go test ./internal/... -count=1
CGO_ENABLED=0 go build ./...
CGO_ENABLED=1 go test -race ./internal/... -count=1
go test -tags=e2e ./... -count=1 -v
go vet -tags=e2e ./...
CGO_ENABLED=0 go build -o noe ./cmd/noe
git diff --check
```

## 実用例と対象外

`examples/http-server.js [port]`を用意する。省略時8080、引数があれば0〜65535の整数として検証する。GET /healthはテキスト、POST /echoはJSONを解析してJSONで返す。不正JSONは400、その他のパスは404。起動時に実際のURLを出力し、Ctrl+Cで終了する。READMEには二つの端末からの起動・curl・終了を載せ、REPLでは待受中にプロンプトへ戻らないことも説明する。

今回はWeb全面互換、Request/Response/Headersクラス、TLSの公開設定、WebSocket、直接のTCP/UDP、ストリーミング、ルーティングライブラリ、タイマーAPI、リクエスト単位の実行期限・JS強制中断、bodyサイズ上限、プロンプト待ち中のREPLバックグラウンド実行を追加しない。HTTP bodyは全体をメモリへ保持する方式とする。これらは本インクリメントの対象外で、必要になった機能を後で選ぶ。npm・Node/Deno互換は引き続き製品方針として実装しない。TypeScriptと名称変更も保留する。

## 実装とレビュー記録

| 段階 | 結果 |
| --- | --- |
| 計画 | 通常レビューPASS。批判的レビューでpending awaitのエラー監視とREPL評価エラー時の停止順を補足し、スライス1で確認 |
| スパイク | 複数Post、Done後のqueued callback、最後のWake、Done後の投稿拒否を実行確認。idempotent listenerの同期closeと別goroutineのShutdownで、ハンドラー内close・応答全体のflush・再bindが成立 |
| スライス1 | 共通ループ、NOE管理のawait、単発処理と継続源、context取消しを実装。通常レビューPASS。批判的レビューの、致命的エラー後の単発I/O待機と結果表示エラー時の源停止を修正し、対象再レビューPASS。runtime全体とEventLoop raceが成功 |
| スライス2 | net/httpとnoe.serve、独自のリクエスト・応答変換を実装。async並行処理、bodyの反復コピー、型検証、ヘッダー、500後の続行が成功。通常レビューでabsolute-formのURL組立てを修正し、実TCPの回帰テストと再レビューPASS。批判的レビューPASS。Serve raceが成功 |
| スライス3 | 別ハンドラーのfetch待機中のclose、512KiBの応答全体、通常終了、複数サーバー、即時close/rebind、切断後のJS続行、mainエラー・rejection・stdin待機・JS無限ループ・timeout・明示終了を実CLIで確認。PTYでCtrl+C、変数保持、再起動、通常エラー回復も成功。通常／批判的レビューPASS。raceビルドした実CLIの同じサーバーE2Eも成功 |
| スライス4 | http-server.js、README/MVP、実バイナリE2Eを追加。デモのhealth・JSON echo・400・404・usage・SIGINT終了を確認。通常／批判的レビューPASS |
| 総合検証 | 全レビュー後に既存CLI/API/REPL/端末操作を含む全E2Eを実行し成功。全internalのrace、CGO無効ビルド、go vetも成功。直下のnoeを更新 |

スパイクは`/tmp/noe-increment10-spike.go`で行い、追跡しない。スライス1の回帰確認には、複数源からの配送、pending await中のホスト失敗・rejection、REPLエラー・中断後の宣言保持、結果表示getterのエラー、停止中の単発I/Oの取消しと遅延通知抑止を含む。

検証環境はGo 1.27.2、Linux amd64。対象runtime/CLIテスト、既存REPL・timeoutのE2E、サーバーE2E、EventLoop/Serveのraceに加え、上記の総合検証コマンドが成功した。race専用の一時overlayでE2EのTestMainを変更し、`go build -race`・CGO有効で実CLIを作ることも確認した。次の実CLI検証でもrace検出はなかった。

```sh
CGO_ENABLED=1 go test -race -overlay=/tmp/noe-increment10-race-overlay.json \
  -tags=e2e ./tests -run '^TestServe' -count=1 -v
```

overlayと総合E2Eログは作業用一時ファイルに置き、repositoryへ追加しない。実装上の未解消必須指摘はない。

## 参照

- [製品方針とMVP](mvp.md)
- [インクリメント9](increment-9.md)
- [既存API](api.md)
- [既存の実行・ジョブ待機](../internal/runtime/runtime.go)
- 当時の非同期処理とキャンセル: `internal/runtime/asyncwork.go`（stdlib移行で削除。現行構成は[移行記録](increment-12.md#実装の変更)を参照）
- txiki.js参照コミット: `b82d829a4a5febcdba834bcf4d38fb9d89461eee`。ローカルの`_refs/txikijs/source/src/js/core/index.js`・`fs.js`・`httpserver.js`、`src/mod_fs.c`・`httpserver.c`・`vm.c`を構成の参考にする。
- go-quickjs参照コミット: `1bfb2432eed66203bcabc709a820aa8d787b2fd1`。継続配送は使用中のv0.25.3の`asyncwork.go`の契約を使い、`stdlib/serve.go`・`loop.go`は設計の参考にする。

`_refs`は引き続き未追跡とし、参照実装の組込みやコピーは行わない。
