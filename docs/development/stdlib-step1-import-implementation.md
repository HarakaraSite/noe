# NOE版stdlib：ステップ1・取り込みと検証記録

2026-10-19。[計画](stdlib-step1-import-plan.md)のステップ1を実装し、単独テスト、独立した通常コードレビュー、その後の総合テストまで完了しました。[構造化結果](stdlib-step1-import-results.json)も参照してください。

## 結果と範囲

`internal/stdlib`へgo-quickjs v0.27.0のstdlibを取り込み、Go packageとして単独でビルド・テストできるようにしました。S1-A（由来と範囲）、S1-B（移植と最小適合）、S1-C（検証と記録）は完了です。

NOE本体の利用先は従来の上流stdlibのままです。`noe:http`追加、runtime・Workerの切替、API名変更、既存`nativeio`・agentの移動、外部向けGoライブラリ化は行っていません。これらは後続のステップで扱います。

## 取り込みと変更

| 項目 | 結果 |
| --- | --- |
| 取り込み元 | `github.com/go-quickjs/go-quickjs v0.27.0` |
| commit | `bef3107cc7eeb5e392cdd9a3768f64fcbf0e609c` |
| checksum | `h1:TVemuGmYmaR+4CCNIVIcHk3dhrqYRq+PGljCkT1EqAE=` |
| stdlib | production Go 26件、Go test 9件、testdata 3件をすべて取り込み |
| WebSocket内部処理 | `internal/wsproto`のsourceとtest各1件を取り込み |
| LICENSE | 上流rootのMIT本文・著作権表示を変更せず保存 |
| 原本比較 | 41ファイル中30件がbyte単位で一致、11件が計画内の適合変更 |
| ローカル追加 | 英文の`UPSTREAM.md`と`eval_internal_test.go` |

11件の変更は、外部packageテスト8件の自己import、`serve.go`・`websocket.go`のwsproto import、`console.go`の評価helperとコメントに限定しました。HTTPの要求buffering、timeout、本文上限、公開module/export、WebSocket protocolは変更していません。

評価helperはエンジンの公開`Runtime.EvalFile`を使用します。既存のhost closure・source名・clone brandを維持し、追加テストで戻り値、初期化から戻る前のPromise job処理、例外のsource名、終了済みruntimeの`ErrClosed`を確認しました。

由来、取り込み元と配置先の一覧、ローカル差分、debuggerに関する違いは、英文の[UPSTREAM.md](../../internal/stdlib/UPSTREAM.md)に記録しています。

## 実装後の単独検証

環境はGo 1.27.2、Linux amd64です。

| 確認 | コマンド・結果 |
| --- | --- |
| 単独テスト | `CGO_ENABLED=0 go test ./internal/stdlib/... -count=1 -timeout=180s -json`。stdlib 3.743秒、wsproto 0.003秒で成功 |
| vet | `go vet ./internal/stdlib/...`成功 |
| race | `CGO_ENABLED=1 go test -race ./internal/stdlib/... -count=1 -timeout=300s -json`。stdlib 13.146秒、wsproto 1.012秒で成功 |

既存の`TestHelperHeartbeat`は子processとして起動するためのhelperで、通常のテスト列挙では上流と同じ理由でskipします。終了処理テストからの子process起動は実行されています。移植のための新しいskipや期待値変更はありません。

## 独立した通常コードレビュー

単独テスト・vet・raceの後、`/root/stdlib_step1_code_review`へ独立した通常レビューを依頼しました。会話履歴を継承せず、承認済み計画、原本比較、43ファイルのhash manifest、変更したsourceとテストをread-onlyで確認しています。

結果はP0・P1・P2・P3の指摘がすべて0件でした。原本一致、ライセンス、自己importと非公開依存、評価と所有の意味、debugger差分、production未切替を確認し、総合テストへ進める判定でした。コード修正要求がなく、再レビューは行っていません。詳細は[レビュー記録](stdlib-step1-import-review.md)にあります。

レビュー対象manifestのSHA256は`e9a0c8545c66e02fe6877f9410683ff67d97618fdb36220d95b77258577f8326`です。総合テスト後にも対象43ファイルの一致を確認しました。

## レビュー後の総合テスト

| 確認 | 結果 |
| --- | --- |
| 全体の単体テスト | `CGO_ENABLED=0 go test ./... -count=1 -timeout=180s -json`成功。テストのある23packageが成功、2packageはテストなし |
| CLI・fixture agent E2E | `CGO_ENABLED=0 go test -tags=e2e ./tests -count=1 -timeout=10m -json`成功、26.534秒。通常のCLI・Worker・HTTP・WebSocket・I/O・agent・`run_js`等の既存E2Eを実行 |
| E2Eを含むvet | `CGO_ENABLED=0 go vet -tags=e2e ./...`成功 |
| 6対象cross-compile | Linux/Darwin/Windows × amd64/arm64で、stdlibとwsprotoのテストbinaryをそれぞれ`CGO_ENABLED=0 go test -c`で生成。12件すべて成功 |
| CLIの通常依存 | `go list -deps ./cmd/noe`で上流stdlibが存在し、`noe/internal/stdlib`とその配下がproduction依存にないことを確認 |
| 取り込み先の直接依存 | `go list -json ./internal/stdlib`で上流stdlibとエンジン非公開packageを直接importしないことを確認。公開エンジンAPIを通じたエンジン内部への間接依存は通常どおり存在 |
| CLIビルド | `CGO_ENABLED=0 go build -o <temporary-output> ./cmd/noe`成功 |
| 変更境界 | `go.mod`・`go.sum`・既存runtime source/test・rootの`noe`バイナリ計41ファイルが作業前のhashと一致 |

外部サービスを使う実provider spike 4件は従来のopt-in条件がないためskipしました。E2Eの`TestImportMapMCPProcessIO`は事前作成済みの`NOE_MCP_SDK_BUNDLE`が未指定のためskipです。これらを成功扱いにはせず、構造化結果にtest名を記録しています。新しい依存、Node.js、実provider呼出しは追加していません。

cross-compileは非Linux環境での実行確認ではありません。また、CLIの総合テストは従来の上流stdlibで動くNOEの確認であり、取り込み版をCLIへ統合した確認ではありません。取り込み版自体のHTTP・WebSocket・Worker・Streams・終了処理は、移植した単独テストとraceで確認しています。

生のテストログ、原本との差分、hash manifest、cross-compile binary、CLIビルドと依存一覧は`/tmp/noe-stdlib-step1-20261019-h7gkjmzy/`に保存しました。主要な結果・対象・hash・skipと制限は、この文書と[JSON記録](stdlib-step1-import-results.json)に保存しています。

## ステップ2への引継ぎ

- productionのGo import・Config・loop・Workerを切り替える作業は未着手です。内蔵docsの実装由来も、現在の上流版の説明を維持しています。
- 通常のNOEとWorkerはdebugger無効のため公開評価経路で互換性を確認できました。`WithDebugger`を使うhostでは、stdlib内部scriptの表示・計装を上流のように隠しません。将来の公開ライブラリ設計時にもこの差分を扱います。
- 新旧stdlibを同じruntimeへ二重導入せず、次のステップでloopとWorkerも含めて切り替えます。その際は取り込み版を使うCLI・Worker・agent・`run_js`のE2Eを実施します。
- commit・push・releaseは実施していません。
