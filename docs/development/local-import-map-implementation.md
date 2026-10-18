# ローカルESMとimport mapの実装・検証記録

2026-10-18。[計画](local-import-map-plan.md)に従い、全スライスの実装・テスト・コードレビューと総合検証・全体レビューを完了した。実provider・credentialは使わない。

| Slice | 状態 | 検証・レビュー |
| --- | --- | --- |
| S1 resolver・snapshot | 完了 | `go test ./internal/modulemap`、reviewerによる`-count=1`再実行成功。Windows amd64テストcross-build成功、native未実施。必須review指摘なし |
| S2 CLI・main・REPL・Worker | 完了 | runtime/CLI既存suite、追加`TestImportMap`群成功。Worker入口の境界を明記し、reviewerが再確認。依存library削除後のWorker内別aliasimportも実行成功 |
| S3 agent継承 | 完了 | Runner・runtime focused tests、2rootsと子agentのfixture E2E成功。private snapshot正常/失敗/取消し時の回収を確認。reviewer独立実行成功、必須指摘なし |
| S4 MCP SDK分割・応用例 | 完了 | SDK 1.32.1/esbuild 0.25.12。main/Worker/fixture-agent、EOF・不正JSON・起動失敗・起動途中close・保留write中close成功。reviewer再実行成功、必須指摘なし |
| S5 文書・総合検証 | 完了 | README両言語、API/REPL・import mapガイド、基本例、instructions、検討メモを更新。通常suite/E2E/vet/race/6builds成功 |

## 総合検証

S4実装後に通常suite・E2E・vetを実行し、すべて成功。全体レビューでも追加の必須指摘はなく、引き渡し可能と評価された。

| 確認 | 結果 |
| --- | --- |
| `go test ./...` | 全package成功 |
| `NOE_MCP_SDK_BUNDLE=<prepared-bundle> go test -tags=e2e ./...` | 全package・E2E成功。任意SDKチェックも有効化 |
| `go vet -tags=e2e ./...` | 成功 |
| `GORACE=atexit_sleep_ms=0 go test -race -p 1 ./internal/modulemap ./internal/runtime ./internal/cli ./internal/agent` | 関連4packageの全suite成功 |
| `NOE_MCP_SDK_BUNDLE=<prepared-bundle> GORACE=atexit_sleep_ms=0 go test -race -tags=e2e ./tests -run TestImportMap -count=1` | 新しいagent/MCP E2E成功 |
| `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build -o <temporary-output> ./cmd/noe` | Linux/Darwin/Windows各amd64/arm64の6target成功。native実行はLinux amd64のみ |
| `go build -o noe ./cmd/noe`、基本例のrun/eval実行 | 成功、どちらも`6`を出力。helpのoption表示も確認 |

## 実装時の判断

S2レビューでWorker入口がsource保持の対象から漏れる点を確認した。固定版stdlibの`Workers.Load(specifier)`は対象runtimeとclassic/module optionを渡さない。起動順依存の対応付けやsource書換えを追加すると既存の並行起動・directive・hashbang・診断に影響するため、[計画](local-import-map-plan.md)の保証を明確化した。保持対象はExecute入口と共通loaderが読む依存module。Worker入口そのものは既存専用hookで読み、削除後の新aliasによる自己importは失敗しうる。ライブラリ再利用の目的は維持し、通常reviewerが再確認した。

S4のtransportでは、起動途中のcloseが後から起動したchildを残す経路と、`writer.close()`待機でgrace期限に到達できない経路を修正した。closeは起動完了を待って回収し、期限はstdin closeからprocess終了までを覆う。fatal cleanupでもpending writeのabort完了を待つ前に`child.close()`を開始する。ready通知後にstdinを読まないserverへ合計8 MiBを書き込み、未完了writeが残っている状態でcloseとwriteの終結・子回収を検証した。

## MCP SDKの準備と接続検証

準備手順は[スパイクREADME](../../scripts/spikes/process-io-mcp/README.md)を参照。SDK/esbuildの固定版を既存の一時node_modulesで確認し、`mcp-client-entry.js`をESM bundleへ変換した。生成bundleは約594 KBで、repository・配布バイナリには含めない。SDK本体へのpatchはない。

```sh
NOE_MCP_SDK_BUNDLE=/tmp/noe-process-mcp.wzVT64/mcp-client.bundle.mjs \
  go test -tags=e2e ./tests -run '^TestImportMapMCPProcessIO$' -count=2 -v
```

成功。`Client` export、独立transport、共通exerciseをaliasで再利用し、main・Worker・fixture-agentの3経路でinitialize/initialized、tools/list、逆順応答の3件並行call、270000 byte UTF-8を確認した。異常系も上記のとおり確認。Node/npm/esbuildは準備だけで、E2EとNOE実行中はNodeを起動しない。未準備環境の通常Goテストではこのチェックをskipし、今回のS4完了確認と総合テストは明示的に準備済みbundleを指定する。実provider・認証・課金は使わない。

## 全体レビューと残る境界

各スライスをreviewerが確認し、最後に未追跡ファイルを含む全実装・テスト・公開文書を横断したレビューを実施した。名前解決、source保持、Worker/agent snapshot継承、SDK分割、利用例と検証結果は整合し、未解消の必須指摘はない。

実行確認はLinux amd64。Darwin/Windowsはクロスビルド確認であり、native実行は未実施。実providerは今回の受入に不要なため使用していない。HTTP import・npm探索・完全なHTML import mapは範囲外で、Worker入口自身のsource保持保証については上記の境界が残る。
