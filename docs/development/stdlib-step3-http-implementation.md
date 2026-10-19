# NOE版stdlib：ステップ3・noe:httpの実装と検証記録

2026-10-19。[計画](stdlib-step3-http-plan.md)に沿って実装・関連テスト・独立コードレビューと再レビューを行い、その後の総合テスト・実プロバイダの基本フローまで完了しました。[構造化結果](stdlib-step3-http-results.json)にコマンド・結果・hash・skip・実接続の課題とtool codeを保存しています。

## 結果と変更範囲

```js
import { fetch, serve } from "noe:http";
```

NOEの通常構成で、既存と同一のfetch・serveを名前付きexportから利用できます。default exportはなく、Request/Response/Headers/Streams・WebSocketは既存globalを共有します。global fetch/serveと`node:http`・bare `http`の入口も維持しています。

基点は`ec6391af316d2a7144964b01da4b848751b7ea14`です。S3-A（登録と契約）、S3-B（英文docs・例・各実行経路）、S3-C（レビュー・総合検証・実接続）は完了しました。要求の送信・受信は引き続きbufferingします。

| 対象 | 変更 |
| --- | --- |
| `internal/stdlib/http.go`・`stdlib.go` | Installの終端から内部helperを1回呼び、有効なFetch/Serveの既存関数値を登録 |
| `internal/stdlib/http_test.go` | 設定4通り・同一性・事前globalから無効機能を公開しないこと・Allow拒否を確認 |
| `internal/runtime/http_test.go` | 4モードのHTTP/型/Streams/WebSocket/docs、fetch bodyのabort、Worker終了時のlistener回収を確認 |
| `internal/runtime/http_agent_test.go` | fixture agentでdocs参照→新入口のJSON往復→最終回答を確認 |
| modulemapの既存test | 新名の予約衝突とnative優先を確認。resolver・予約一覧の製品コードは維持 |
| apidocs・現行ガイド・UPSTREAM | 英文のnoe:http topicとfetch/serveの詳細、既存docsからの導線、NOE独自差分の由来を追加 |
| HTTP例とE2E | 既存4例にimportを追加。有限loopback roundtrip例とローカルfixture検証、移動したbinary/新topicのrun_js一致を追加 |

Fetch/Serveの両方が有効なら2 export、片方ならその関数だけ、両方nilならmoduleを登録しません。設定を判断の基準にするため、hostが事前に同名globalを置いても無効機能をexportしません。単体のGo関数Network/Serversの契約、main/Workerの導入hook、通信・loop・終了処理は変更していません。

## API比較と関連テスト

変更前sourceと変更後sourceをそれぞれ一時CLIへビルドし、同じinventory scriptをmain・Node形式Worker・孫Worker・Web Workerで実行しました。4モードすべてで既存global名、26 Node形式moduleのexport/alias関係、constructorの関係、process.versionが完全一致しています。新入口自体のexport集合と同一性は各modeの実行テストで確認しました。

| mode | global名 | Node形式module | docs topic | 詳細member |
| --- | --- | --- | --- | --- |
| main | 119 | 26 | 41 | 100 |
| Node形式Worker | 119 | 26 | 41 | 100 |
| 孫Worker | 119 | 26 | 41 | 100 |
| Web Worker | 129 | 26 | 41 | 100 |

旧docs topic/member IDとaliasは維持し、追加はnoe:http topicとfetch/serveの2 detailです。本文は現行の契約に合わせ、要求buffering・応答streaming・timeout・本文の切詰め・同期close・WebSocketの寿命を説明しています。初期instructionsへHTTP詳細を追加していません。

最初の関連確認はstdlib/wsproto/modulemap/apidocsで成功し、runtimeでは新規abort testだけが失敗しました。新testで応答本文の取消しにも元のsignal.reasonが返ると誤って期待したためです。現行APIは本文readを`TypeError: terminated`でrejectするので、rejectとserver側の取消し観測を確認する条件へ修正しました。製品のHTTP挙動や既存testの期待値は変更していません。

修正後のHTTP登録/全新runtime case、fixture結果の厳密なJSON検証とcatalog、新入口を含むHTTP/Serve/Stdlib/Docs/ImportMapの重点E2Eは成功しました。新規abort testはさらにchunkを蓄積してから照合する形にし、最終の関連確認でも成功しています。初回の失敗ログも構造化結果に残し、解消後の成功と区別しています。

## 独立コードレビューと再レビュー

`/root/stdlib_step3_code_review`が会話履歴を継承せずread-onlyで24ファイルと証跡を確認しました。初回はP0:0、P1:0、P2:1、P3:0です。fetchの32 MiB上限を「超過するとreadが失敗する」と書いたdocsの誤りを指摘され、実装どおり「prefixを返し正常終了、超過自体ではrejectしない」へ修正しました。

docsとabort testの2ファイルに限定した再レビューはP0〜P3すべて0件。P2以上の未解消指摘は0件で、総合検証へ進める判定でした。[レビュー記録](stdlib-step3-http-review.md)に根拠・範囲・修正を保存しています。

最終manifestのSHA256は`43db74b32d733b7b93d46d6126f5812f8c197baed9be551c8f2c602e2c8dc561`です。総合検証後も対象24ファイルが一致しています。ファイル別hashはJSON記録に含めています。

## レビュー後の総合テスト

環境はGo 1.27.2、Linux amd64です。

| 確認 | コマンド・結果 |
| --- | --- |
| 全体の単体テスト | `CGO_ENABLED=0 go test -json ./... -count=1 -timeout=180s`成功。23package成功、2packageはテストなし。wall time 40.703秒 |
| internal全体のrace | `CGO_ENABLED=1 go test -race -json ./internal/... -count=1 -timeout=300s`成功。20package成功、1packageはテストなし。wall time 98.564秒 |
| full CLI・fixture agent E2E | `CGO_ENABLED=0 go test -json -tags=e2e ./tests -count=1 -timeout=10m`成功。wall time 29.551秒、test skipなし |
| E2Eを含むvet | `CGO_ENABLED=0 go vet -tags=e2e ./...`成功 |
| 6対象CLIビルド | Linux/Darwin/Windows × amd64/arm64、CGO無効の一時出力先で6件成功 |
| 6対象test compile | 同じ6対象でstdlib/runtime/modulemap/apidocsをpackage別に`go test -c`。24件成功 |
| production依存 | ローカルstdlibがCLI依存にあり、上流stdlibはない。go-quickjsエンジンへの依存は維持 |
| 変更境界 | 既存46ファイルのhash比較で、変更はstdlib.goとUPSTREAM.mdだけ。go.mod/go.sum、既存配布binary、通信・loop・Worker・Streams・WebSocket本体とlicense/fixtureは不変 |

E2Eには既存のprepared MCP SDK 1.32.1 bundleを渡し、MCP統合も実行しました。通常suiteでの既存skipはstdlibのTestHelperHeartbeat（子process helper）、実provider用spikeのTestImageProviderSpike・TestImageWireLiveDiagnostic・TestOpenAIProviderLive・TestLive（opencode-go）です。新しいskipはありません。

native実行はLinux amd64です。他の5対象はコンパイル確認で、実機実行ではありません。cross-compileは`CGO_ENABLED=0 GOOS=<os> GOARCH=<arch>`でCLIを`go build -p=2 -o <temporary-output> ./cmd/noe`、testを`go test -p=2 -c -o <temporary-output> ./internal/<package>`で生成しました。

## 実プロバイダの基本フロー

OpenCode Go / `deepseek-v4.1-flash`、`effort: auto`で1課題を初回試行で完了しました。課題は、noe:httpでport 0のloopback server/clientを作り、JSON `{value:21}`を送って`{value:42}`を受け取り、HTTP status・値・実portをreport.jsonへ保存し、listenerを解放して英文で結果を返すものです。課題文に新module名は指定しましたが、docs参照は指示していません。

| 観点 | 結果 |
| --- | --- |
| モデルとの往復 | provider要求4回、tool 3回、すべてexit code 0 |
| 自発的なdocs参照 | noe:http概要とserve詳細の2 toolで確認 |
| 実際の新入口使用 | 3番目のtoolがnoe:httpのserve/fetchをimportし、JSON往復・本文消費・finallyのclose・ファイル保存と再読取りを実行 |
| 成果物 | `{status:200,value:42,port:37069}`で期待値と一致 |
| 終了 | listenerへの接続が失敗し解放を確認。lastResult.stateはcompleted、CLI exit code 0、最終回答あり |
| 所要時間 | 9.895秒。上限は6 provider要求・CLI 115秒 |

実接続は一時workspaceと既存認証を使いました。API keyや認証headerは保存していません。tool code・結果・課題・最終回答・binary hashを構造化結果へ、イベント・stdout/stderr・成果物をartifact内のlive-smoke/へ保存しています。

## 引継ぎ

ステップ4では共通fetch実装の要求送信bufferingを見直します。現在は要求streamを閉じる前の送信を保証せず、serveも本文を読んでからhandlerを起動します。応答側は既存のstreamingを共有し、本文上限到達は送受信とも現在のprefix切詰めという契約を維持しました。後続ではこれらとabort・背圧・終了の違いを明示して設計します。

生ログ・CLI・inventoryとscript・manifest・build出力・実接続の証跡は`/tmp/noe-stdlib-step3-20261019-84fpyjw_/`です。commit・push・releaseと既存配布binaryの更新は行っていません。
