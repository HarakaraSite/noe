# NOE インクリメント8 HTTPサブセットの拡充

JSON APIの更新・削除をNOEから行えるよう、fetchにPUT・PATCH・DELETEを追加する。Go 1.27.2・既存go-quickjs・Go標準HTTPクライアントを維持し、新しい依存は追加しない。状態は実装・テスト・レビュー・総合E2E完了。

## APIの契約

| 項目 | 契約 |
| --- | --- |
| method | GET・POST・PUT・PATCH・DELETE。省略はGET。文字列を大文字へ正規化する既存動作を維持。HEAD・OPTIONS等はreject |
| body | POST・PUT・PATCH・DELETEで省略可能な文字列。省略・undefinedは空のリクエストbody。明示的な空文字も許可。GETでもundefinedは省略と同じで、それ以外のbodyは空文字も含めreject。null・数値・オブジェクト等はreject |
| headers | 既存の文字列値オブジェクト。Content-TypeやJSON化を暗黙に追加しない |
| response | status、ok、text()、json()を維持。4xx・5xxもレスポンスを返し、通信失敗はreject。204等の空レスポンスはtext()で空文字、json()は通常のJSON.parseエラーとしてreject |

URL・optionsの検証、未知オプションのreject、レスポンスbody一括取得と再読込み、JS所有goroutineとAsyncWorkの終了経路を変えない。リダイレクト・プロキシの動作は既存Goクライアントに従う。run/evalの--timeout、REPLのawaitと中断後の続行も維持する。

## スライスと受入条件

| スライス | 範囲 | 完了条件 |
| --- | --- | --- |
| 1 | メソッド・body検証とruntimeテスト | 新3メソッドの送信、大小文字、日本語・JSON・空・省略body、明示ヘッダーをローカルサーバーで観測。GET body・非文字列body・未対応メソッド等はリクエストを送る前にreject。既存GET/POST、4xx・5xx、204とtext/jsonを確認 |
| 2 | 実用例・README・実バイナリE2E | JSON更新・削除の例、PUT→PATCH→DELETEの実際の通信、REPLのawaitとエラー後続行、未awaitの完了待ち、--timeoutによる中断とserver側キャンセルを確認。既存機能を含む全E2Eを成功させる |

計画・各スライスを通常／批判的レビューする。入力の許可範囲と実際の送信、bodyの省略・空文字・型、204の扱い、既存レスポンス/終了/REPL契約との整合を独立確認する。初回15分、再確認は変更・finding解消に限って一回15分以内。親agentがfinding採否と最終検証を担当する。

## 実用例

`examples/http-json.js METHOD URL [JSON-body]`。JSON-bodyがある場合はJSON.parseしてから明示的にJSON.stringifyし、Content-Typeをapplication/jsonとして送る。ない場合はbodyを指定しない。レスポンスが成功なら`{status, ok, body}`をJSONでstdoutへ出し、空レスポンスはbodyをnullにする。HTTP失敗はこの例の方針として診断・コード1、引数不足は2。APIとしてのfetchは4xx・5xxをrejectしない。

READMEでは利用者が指定するJSON APIのURLで更新・削除する例を示す。実装・検証で外部サービスへ更新・削除リクエストを送らず、ローカルHTTPだけを使う。自動リトライや暗黙のJSON化は追加しない。

## 検証と対象外

対象Goテスト・実バイナリE2E・race・go vet・CGO無効ビルドを行い、全レビュー後に`go test -tags=e2e ./... -count=1 -v`を実行して直下のnoeを更新する。

HEAD・OPTIONS、リクエスト単位のtimeout、AbortController、レスポンスヘッダーAPI、バイナリ/ストリーム、URL import、HTTPサーバー、Web全面互換、npm・Node/Deno互換、TypeScript、名称変更は含めない。

## 実装・レビュー記録

| 段階 | 結果 |
| --- | --- |
| 計画 | 通常／批判的レビューで必須指摘なし。GETのbody:undefinedの扱いを明確にする文言提案を採用し、対応する送信テストも成功 |
| スライス1 | JSのメソッドallowlistとbody検証を拡張。新メソッド・大小文字・JSON/日本語/文字列/空/省略/undefined・明示ヘッダー・Content-Type自動設定なしをサーバーで確認。無効入力の未送信、204のtext/json、409レスポンス、再読込みと既存GET/POSTも成功。通常／批判的レビューで指摘なし |
| スライス2 | http-json.js・README・実バイナリE2Eを追加。PUT→PATCH→DELETEのサーバー状態、明示JSON変換/Content-Type、204/HTTPエラー/不正JSON/引数不足、REPL回復、未await PUT、PATCHのtimeoutとserver側キャンセルを確認。通常／批判的レビューで指摘なし |
| 総合E2E | 全レビュー後、既存CLI・API・REPL・端末操作を含む全E2Eが成功 |

最初のPATCHキャンセルE2Eは、テストサーバーがリクエストbodyを未読のまま待機していたため切断を観測できず失敗した。bodyを受信・照合してから待機するようテストを修正し、時間切れ・コード124・後続JS未実行・サーバー側contextのキャンセルを確認した。送信・キャンセルの実装は変更していない。

検証環境はGo 1.27.2、Linux amd64。以下の確認が成功した。

```sh
go test ./internal/runtime -run '^TestFetch' -count=1 -v
go test -tags=e2e ./tests -run '^TestHTTP(UpdateLifecycle|MethodsPiped|PatchTimeout|JSONExample)' -count=1 -v
CGO_ENABLED=1 go test -race ./internal/... -count=1
go test -tags=e2e ./... -count=1 -v
go vet -tags=e2e ./...
CGO_ENABLED=0 go build -o noe ./cmd/noe
git diff --check
```

一時Go overlayでTestMainのCLIビルドを実際のraceビルドへ切り替えて追加E2Eも成功し、race検出なし。通常の全E2Eと直下のnoeはCGO無効でビルドする。overlayとログはrepositoryへ追加しない。すべての通信検証はローカルサーバーで行い、外部サービスへの更新・削除はしていない。実装上の未解消指摘はない。

## 参照

- [製品方針・MVP](mvp.md)
- [インクリメント7](increment-7.md)
- [現在のHTTP API](../README.md#httpサブセット)
