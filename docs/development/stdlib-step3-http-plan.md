# NOE版stdlib：ステップ3・noe:httpの入口計画

2026-10-19。[段階案](../future-candidates.md)のステップ3を具体化した計画です。基点はv1.10.0公開確認後の`ec6391af316d2a7144964b01da4b848751b7ea14`です。その後の実装依頼に基づき、3スライスの実装・関連テスト・独立コードレビューと再レビュー・総合テスト・実プロバイダ基本フローまで完了しました。[実装・検証記録](stdlib-step3-http-implementation.md)と[構造化結果](stdlib-step3-http-results.json)を参照してください。以下は実装前にレビューした方針と受入条件です。

## 目的と到達点

`noe:http`を通常のHTTP送受信の推奨入口にし、既存の`fetch`・`serve`と同じ機能を名前付きimportから使えるようにします。英文の内蔵docsと実行例を用意し、main、Worker・孫Worker、agentの`run_js`まで届くことを確認します。

今回は公開入口の追加です。要求送信のstreamingはステップ4、要求受信のstreamingはステップ5、汎用登録境界の全面整理と外部Goライブラリ化はステップ6・7へ残します。HTTP処理を別実装に分岐させず、現在の通信・Web型・Streams・loop・WebSocket連携を共有します。

## 初版の公開契約

NOEの通常構成でのexportは、次の2つに固定します。

```js
import { fetch, serve } from "noe:http";
```

| 項目 | 方針 |
| --- | --- |
| `fetch` | 既存globalのfetch関数と同一の値。input/init、Response、例外・abort・本文の消費規則も同じ |
| `serve` | 既存globalおよび`node:http`/`http`のserve関数と同一の値。overload、server handle、終了規則も同じ |
| default export | 追加しない。NOE固有moduleとして名前付きimportを使う |
| Web型 | Headers、Request、Response、ReadableStream等は既存globalを使う。今回moduleへ再exportせず、constructorやbrandを作り直さない |
| WebSocket | 既存globalのWebSocket・upgradeWebSocketを組み合わせる。同じserve関数・同じServe設定を使い、upgrade tokenの経路を維持する。これらの再exportは今回含めない |
| 旧入口 | global fetch/serve、`node:http`・bare `http`のexport集合と同一性を維持する |
| module名 | `noe:http`のみ追加。bare `http`の意味を変更せず、新しいbare aliasは作らない |

`fetch === globalThis.fetch`、`serve === globalThis.serve`、`serve === (await import("node:http")).serve`を同一runtime内で保証します。関数を包むJS wrapper、二重のNetwork/Servers/Streams導入、新しいHTTP clientやloopは作りません。登録後に利用者がglobal変数を代入し直しても、module exportを追従させる機構は設けません。

この2関数の入口を先に確定し、後続のstreaming改善もこの共通実装上で行える状態にします。初版でHTTP関連の型・WebSocket全体までmoduleへ集めることは到達条件にしません。

## 実装場所と設定の扱い

`internal/stdlib/http.go`に内部登録helperを追加し、`stdlib.Install`が現在のNetwork・Servers・Sockets等を導入し終えた後に一度呼びます。helperは、そのInstallで有効にしたfetch/serveの既存JS値を`SetModuleValues("noe:http", ...)`へ渡します。登録失敗はInstallのerrorとして返します。

NOEの`internal/runtime`に専用wrapperや登録hookを置きません。現在のmainのInstallと、`worker.go`のchildConfig→Installから同じstdlib内の登録へ到達します。Workers.Installedはdocs/nativeioの現在の用途を維持します。`run_js`も変更後sourceからビルドしたNOEを起動する既存経路を使います。

NOEはFetchとServeの両方を有効にしていますが、stdlibを個別設定する既存テスト・hostの契約も維持します。

| Installの設定 | noe:http |
| --- | --- |
| Fetchあり・Serveあり | `fetch`・`serve`の2 export |
| Fetchあり・Serveなし | `fetch`のみ |
| Fetchなし・Serveあり | `serve`のみ |
| Fetchなし・Serveなし | moduleを登録しない |

無効な機能をundefinedのexportや別の通信関数で補いません。設定を判断の基準にし、hostが事前に置いた同名globalがあるだけでexportしません。Fetch.Allow/Client/MaxBodyBytes、Serve.Allow/Loop、NoWebAPIs等の現在の意味を変えず、新しい設定fieldや権限は追加しません。NoWebAPIsを含む既存の組合せのサポート範囲を今回広げません。

単体のGo関数`Network`・`Servers`を直接呼ぶ経路の契約は維持し、新moduleの登録はInstallの共通helperに集約します。部分導入のたびに登録済みmoduleを再定義する仕組みは作りません。これらのGo関数は現時点では外部公開ライブラリのAPIではなく、公開設計は後続です。

`internal/modulemap/snapshot.go`は既に`noe:`全体をimport mapのkeyとして予約しています。新名のために予約prefixやresolveの分岐を増やしません。native moduleがcustom loaderより先に解決されることを新入口で確認します。26個のNode形式moduleのbare名一覧は変更しません。

## 既存のHTTP契約と後続の境界

| 観点 | ステップ3で維持・説明する内容 |
| --- | --- |
| outbound | fetchの既定HTTP client timeoutは30秒、応答本文の既定上限は32 MiB。HTTP 4xx/5xxはResponseとして返り、ネットワーク・abort等の失敗はrejectする |
| 要求送信 | ReadableStreamを渡しても全量を収集してから送信する。ストリームを閉じる前に相手へ本文が届くとは説明しない |
| 要求受信 | serveは要求本文を読み込んでからhandlerを起動する。現在の32 MiB設定はLimitReaderで読む上限であり、超過を413で拒否する契約はない |
| 応答streaming | fetchのResponse.bodyを順次読め、serveからReadableStreamのResponseを送れる既存機能を維持する |
| serve既定値 | bindは127.0.0.1、portは0。read-header timeoutは30秒、idle timeoutは60秒。新しいJS timeout optionは追加しない |
| server handle | hostname・port・url・同期close()を維持。closeは受付を止め、処理中のHTTP要求を最大5秒待つ。handler自身からの同期closeは避け、応答消費後などに外から呼ぶ例にする |
| 失敗・終了 | handler失敗の500、options.onError、fetch abort、接続切断、closeの冪等性、timeout/SIGINT/Worker terminate/runtime Closeによる現在の回収経路を維持 |
| WebSocket | noe:httpのserveから既存upgradeWebSocketを使って通信できる。HTTP要求・応答型やupgrade識別を作り直さない |

この調査で見えた既存制約を入口追加へ紛れ込ませて変更しません。本文上限・送信/受信streaming・中断・背圧・全二重通信等の仕様は、ステップ4・5の計画で現行挙動との違いを明示します。新しいNode request/response互換API、TLS socket API、HTTP/2専用APIは今回の範囲に含めません。

## スライスと受入条件

### S3-A：stdlibに入口を追加し、契約を検証する

- 実装開始時のHEAD・Go環境・依存定義・root binaryのhashを記録し、現在sourceからビルドした一時CLIで旧global・26 module・docs IDの基準を取得する。既存のステップ2 inventory scriptを再利用できる。
- http.goの内部登録helperとInstallからの呼出しを追加する。fetch.go/serve.goの通信・JS本体・loop・終了処理は変更しない。
- `internal/stdlib/http_test.go`で、通常構成のexport集合と関数同一性、上の4設定、事前に同名globalがあっても無効機能を公開しないことを確認する。
- 新入口のfetch/serveでloopback JSON/バイナリ往復を実行し、Request/Response・Streamsが既存の型として扱われること、許可拒否が既存のAllowを通ることを確認する。新しい通信経路がないため、HTTP全caseを複製せず既存のFetch/Serve/Streams/WebSocketテストを併用する。
- modulemapのテストで新名のimport-map衝突拒否とnative優先解決を確認する。Node形式26名の予約・exportを維持する。

完了条件：stdlib.Installから登録され、NOEの通常構成では正確に2 exportが得られる。無効設定で新しい通信権限を与えず、旧入口と同じ関数・設定・loopへ到達する。既存のglobal集合・26 moduleのAPI inventoryは一致し、新moduleだけが追加される。

### S3-B：英文docs・推奨例・実行経路を揃える

- `internal/apidocs/content/noe-http.md`を追加し、`noe.go`にtopicを登録する。概要・export一覧・`fetch`と`serve`の2詳細を英文で書く。構文、返却値、例外、設定既定値、bufferingと応答streaming、WebSocket連携、closeと資源の寿命を自己完結する説明にする。
- `stdlib-globals.md`の既存fetch/serve詳細と`stdlib-http.md`に新しい推奨入口への導線を加え、従来のtopic/member・bare aliasは維持する。新topicと既存の共通契約を矛盾させず、後続のHTTP変更では両方を照合する。
- `content/noe.md`・`stdlib.md`、`docs/api.md`・`docs/reference.md`の必要な導線を更新する。stdlibのNode形式module数26は維持する。初期instructionsへHTTP API詳細を追加せず、既存のdocs探索導線を使う。
- `internal/stdlib/UPSTREAM.md`にNOE独自のmodule登録差分を英文で追記する。原型版・commit・checksum・license・ステップ1の適合差分は維持する。
- `examples/http/roundtrip.js`に、port 0のloopback serverへJSONをPOSTし、本文消費後にfinallyでcloseする有限の例を追加する。既存HTTP例4件も必要なfetch/serveをnoe:httpからimportする形にし、HTTP例READMEと一覧を揃える。exampleのcode/commentは英文、既存の日本語ガイドの言語は維持する。
- main・Node形式Worker・孫Worker・Web Workerで新入口のimport、同一性、実際のHTTP往復とdocs一致を確認する。既存のWorker初期化・禁止NOE module・所有関係は変更しない。
- CLI docsを無効なagent設定・認証なしで参照でき、移動した単体binaryとrun_jsでも同じ新topic本文へ到達することを確認する。fixture agentからdocs参照→新入口でのHTTP往復→最終回答を通す。

完了条件：通常のHTTP例が新入口で動き、APIの由来・既定値・寿命がバイナリと一致する。docsの差分は`noe:http` topicとfetch/serveの2 detail追加および関連本文の整合。既存ID/aliasは不変で、現行40 topics・98 detailsから41・100へ増える想定。件数だけでなくID集合と内容を検証する。

### S3-C：独立コードレビュー・総合テスト・引継ぎ

- 関連テストと例の確認後、実装に対する独立した通常コードレビューを行い、P2以上を解消する。その後、全体テスト・race・CLI/fixture agent E2E・vet、6対象のCLIビルドと変更したtest packageのcross-compileを行う。
- 実プロバイダの基本フローは1課題に限定する。既存で使用中のOpenCode Go / deepseek-v4.1-flashを使い、noe:httpで一時loopback server/clientを動かしてJSON往復・成果物保存・close・最終回答まで確認する。課題に新module名は指定するがdocs参照は指示せず、実際のtool codeと結果で使用を確認する。例外修復や並列agent等の広い実接続検証は追加しない。
- 実接続は一時workspace、最大6 provider要求・CLI上限115秒を基本にする。API key・認証headerを保存せず、prompt・tool・結果・所要時間を記録する。設定や認証が利用できない場合は未実行として報告し、fixture成功と混同しない。この計画作成では呼び出さない。
- `stdlib-step3-http-implementation.md`と構造化結果へ、対象source、API/doc差分、コマンド、review、skip、native実行とcross-compileの範囲を保存する。段階案はステップ3だけ完了へ更新する。

完了条件：入口追加を使うCLI・Worker・agentが動き、旧APIの互換性と終了・取消しを確認できる。ステップ4が要求送信のbufferingを置き換える前提・テストを引き継げる。

## 検証の対応付け

| 観点 | 主な確認先・追加するcase |
| --- | --- |
| stdlib登録・設定 | 新しいhttp_test.go。設定matrix、関数同一性、Allow拒否。既存TestFetchAllow/SeesRedirects・TestServeNeedsPermissionを維持 |
| HTTP・型・streams | runtimeの新入口の往復caseと既存TestStdlibHTTPAndWebSocket、stdlibのTestStreamingBodies・TestFetchStreamsTheResponse・TestServeStreamsTheAnswer |
| buffering維持 | 有限の要求streamを渡す既存caseを新入口でも使い、producer終了前の送信/handler起動を新機能として主張しない。ステップ4/5では専用の順序barrierを加える |
| abort・終了 | 新入口でのfetch abort、server.closeとruntime/Worker終了の代表case。既存stdlib lifetime、runtime root-error、CLI serve/execution_timeoutテストも実行 |
| Worker | 同じ内部登録へ到達することを、main/Node Worker/孫Worker/Web Workerそれぞれのimport・HTTP往復・docsで検証 |
| module解決 | modulemapでnative優先解決、noe:httpをimport-map keyへ指定したときの拒否、従来26名の維持 |
| docs | catalog・runtime・CLI・standalone binary・run_js。既存ID集合を保ち、新moduleのexport名を本文へ記載し、詳細2件へ到達できる |
| agent | 新sourceからビルドしたCLIでfixture agentの新入口使用とdocs返却、別に実providerの基本フロー1件 |
| 例 | 有限loopback例を実行。既存ダウンロード例はローカルfixture URLを使い、通常テストに外部HTTPを要求しない |

関連確認の基本コマンド：

```sh
CGO_ENABLED=0 go test ./internal/stdlib/... ./internal/runtime ./internal/modulemap ./internal/apidocs -count=1 -timeout=180s
CGO_ENABLED=0 go test -tags=e2e ./tests -run 'Test.*(HTTP|Serve|Stdlib|Docs|ImportMap)' -count=1 -timeout=5m
```

コードレビュー後の総合確認：

```sh
CGO_ENABLED=0 go test ./... -count=1 -timeout=180s
CGO_ENABLED=1 go test -race ./internal/... -count=1 -timeout=300s
CGO_ENABLED=0 go test -tags=e2e ./tests -count=1 -timeout=10m
CGO_ENABLED=0 go vet -tags=e2e ./...
```

Linux/Darwin/Windows × amd64/arm64でCGO無効のCLIを一時出力先へビルドし、stdlib/runtime/modulemap/apidocsの変更したtest packageを`go test -c`でpackage別に生成する。実行確認はLinux amd64、他対象はcross-compileと明記する。root配布binaryは上書きしない。既存のhelper/OS/opt-in skipは理由を記録し、今回の都合によるskipや期待値緩和はしない。準備済みMCP SDK bundleが利用可能なら通常E2Eへ渡し、新規取得は前提にしない。

## 範囲・停止条件・レビュー

今回作るのは計画とレビュー記録だけで、source・API docs・例・テストの実装は利用者の実装依頼後に行います。`_refs/`と利用者の`agent-server-build.jsonl`は参照・変更しません。依存・上流engineの更新、HTTP機能追加、package移動、外部向け公開path、commit・push・releaseは計画作成の範囲外です。

実装中に通信やcloseの変更が必要だと判明した場合は、入口追加に必要な差分か、後続のHTTP改善かを根拠に基づき切り分けます。互換性を変える修正や計画外の機能を混ぜず、判断が範囲を変える場合は利用者へ返します。局所的な登録方法・テスト構成はこの契約を満たす範囲で決めます。

独立した通常の計画レビューでは、2関数の公開契約、stdlib配置と設定matrix、main/Worker/agentへの到達、既存HTTPとWebSocketの共有、docsの完全性、後続との境界、テストとスライスの実現性を評価します。初回30分、新しい根拠・中間結論が10分ない場合は中断。再レビューは変更箇所と既存指摘の解消を15分以内で確認します。利用者の明示依頼に従い、P0/P1/P2の未解消指摘が0になるまで修正と再レビューを反復し、P3は任意改善として区別します。
