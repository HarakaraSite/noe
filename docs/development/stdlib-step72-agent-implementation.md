# NOE版stdlib：ステップ7.2・実装と検証

2026-10-19。[承認済み計画](stdlib-step72-agent-plan.md)（SHA256 `a481fe341b0b874f649745edf02d3e25a1e4e27a016f4fbec07935fff0174bcd`）に基づきS72-A〜Fの実装・検証を完了した。基点は7.1のcommit `5d00ed9cb194febc8d523980558f0e56b75883ac`。今回は実装・関連試験・独立コードレビュー・総合テストまでで、commit/push/releaseは行わない。

## 構成と所有

公開 `agent` facadeはcaller所有runtime/Loopへ追加し、ctx・明示NOE executable・provider/認証/loaderを共通 `internal/agentbridge` へ接続する。Host/Operation/broker/runner/SDKの実装は既存internalに保持。NOE rootは公開入口、connected childは同じbridgeのprivate adapterを使う。汎用stdlibだけの利用へagent/SDK依存を増やさない。

有限hostはroot Promiseの成功/失敗を捕捉しLoop.RunUntilで結果を受け、bindingをCloseする。task完了後もidle instanceが残るためLoop.Runの自然終了を成功条件にしない。runtime/Loop/元sinkはcaller所有。ctx/CloseからGo仕事と子processを回収し、JSownerによるreply駆動を待たない。

## 状態

関連試験→独立コードレビューと限定再レビュー（P2以上解消）→総合確認を完了した。raw artifactsは `/home/agent/.cache/noe-stdlib-step72/`、TMPDIR/GOTMPDIRは `/var/tmp/n72`。7.1の実装・既存JS契約と依存versionを維持し、利用者の `.zed/`・`agent-server-build.jsonl`・`_refs/`・root配布binaryは読取り/変更/配布対象外。

API docs/GoDoc・Go利用ガイド・例は英文。計画と開発記録は日本語。実装とsynthetic artifact成功をv2公開や常用の安定性確認と混同しない。

## 実装上の判断

- 公開ProviderOptionsはregistryで有効な6fieldに限定。詳細provider/model設定は既存JSON schemaを使い、KeyFile/BaseURL/DecisionServerURLの既存OpenRouter向けoverrideもそのまま維持する。
- CLIは既定のAuth moduleを維持するため非nilの零値設定を渡す。外部GoではAuth=nilをopt-outとして扱う。
- CLIのEnableControlは既定directoryの読取り解決からControlDirへ変換する。解決不能時はprivate adapterを使い、agentを起動しないscriptまで構築時に失敗させない従来のlazy動作を保つ。
- legacyな内部pure host・接続済みchild・内部Profile注入はprivate adapter、明示Executableを持つ通常rootはpublic Installを使う。登録・hook・終了処理は同じbridgeに集約する。
- artifact helperは新しいbridge/公開package/例を明示的に収録し、削除済みtracked sourceは除外する。利用者ファイルやnested moduleを配布対象へ加えない。

## 関連確認と初回レビュー

bridge/runtimeの関連unit、bridgeのrace、root agent/観察/認証/run_jsの限定raceは成功。新しいGo例から今回のNOE binaryへ接続し、run_jsのstdoutをproviderで検証して最終回答まで成功した。例をcheckout外へコピーした別Go moduleでも同じ結果を確認した。

独立した通常コードレビューの初回はP0:0/P1:0/P2:1/P3:0。S72-C01として、live consumerがrun_agent snapshotを捨て、report作成後にtaskが失敗しても成功扱いし得る点を採用した。stopped/completedと非空answerを検証し、英文最終回答をtaskで指定する修正を行った。限定再レビューはP0〜P3すべて0件。製品facade/bridge/runtimeは初回レビュー後に変更していない。


## 総合確認

[構造化結果](stdlib-step72-agent-results.json)と[レビュー記録](stdlib-step72-agent-review.md)に保存。全体のraw logは上記cache directoryにあり、総合確認に失敗はなかった。

| 確認 | 結果 |
| --- | --- |
| 全体unit `go test ./... -count=1 -timeout=300s` | 成功、49.955秒 |
| E2E `go test -tags=e2e ./tests -count=1 -timeout=10m` | 成功、42.349秒 |
| 通常/e2eの `go vet` | 両方成功 |
| race `go test -race ./internal/... ./stdlib/... ./agent/... -count=1 -timeout=300s` | 成功、62.592秒 |
| Linux/macOS/Windows × amd64/arm64 | CLI build 6件＋8packageのtest compile 48件、すべて成功 |
| 外部agent consumer | local replace・no-replaceのlocal GOPROXY artifact・独立module cache・6対象compile、19確認成功 |
| 既存stdlib consumer | local replace・同artifact・独立module cache、12確認成功。stdlibのproduction build closureは257package、agent/SDK依存0件 |
| CLI/機能smoke | 29件、fixture model要求2回・hook6回も成功 |
| 4モードのJS inventory | baselineと完全一致。globals 119/119/119/129、各26module・41topic・100detail |
| HTTP要求送受信streaming | EOF前prefix、CLI/JS docs一致、有限example・listener再bind成功 |
| Go例 | current NOEへrun_js・最終回答成功。checkout外の別moduleでも成功 |

外部consumerのreferenceは実際の別NOEを起動し、root Load/child import-map snapshot、CSV集計・保存・再読込み、nested agent、decision/image注入、queue/observe/interruptとqueueの解消、実行中providerのClose/join、JS Promise拒否、HTTP registry注入、fixture OIDC/JWKSによるAuth保存を確認した。borrowed sinkを閉じず再利用でき、consumer自身をNOEとして再実行していないことも確認した。

実providerは外部consumerから **OpenCode Go / deepseek-v4.1-flash / effort auto** で1回実行し成功、16.962秒。モデル要求3回・run_js 2回でinput 3件を合計31としてreportへ保存し、再読込み・completed snapshot・非空最終回答まで確認。taskにはdocs利用の指示を入れていない。上限は115秒・provider要求6回。credentialは明示pathで渡し、その内容は記録・表示しない。docsを実際に呼び出したかの測定は今回の基本フローには含めない。

## 途中で修正した試験・補助script

bridge testのerrors import漏れ、consumerのEvalContext戻り値とunused import、URL形式のchild-map key、runtimeによるimage Outputの絶対化、checksumが不正なPNG fixture、会話履歴全体からtaskを誤分類して待機が終わらなかったprovider fixtureを修正した。PNGはGo標準image/pngで生成し、provider分岐は最後のuser messageで行う。artifact helperは削除済みtracked fileをreadしないよう修正。これらで製品契約を緩めていない。

関連unit/raceの初回tool出力と一部fixture失敗はファイル保存していないため、実行commandと担当agentの報告をcore-related.json/consumer-related.jsonに区別して残した。最終focused成功logと全総合確認のraw logは保存した。レビュー対象24項目のmanifest SHA256は `694958ef9efaa1b04f10cff67256beb8594914bd984d5a62244ff9c7f03ef3aa`、テスト後も全件一致。

## 完了範囲

S72-A〜Fは完了。段階7は7.1の汎用Go提供と7.2のagent接続まで実装・検証済み。go.mod/go.sum、engine v0.27.0、CLI既定version v1.13.0、既存JS APIを維持した。API docs・GoDoc・新しいGo利用ガイドと例は英文。非Linux対象はcompileのみで実行試験ではない。

v2 tag公開、常用での安定性判断は未実施。今回commit/push/releaseはしていない。次は利用者の方針どおり常用して小さな不具合を修正し、問題なさそうと判断してからv2.0.0を公開する。


本記録と結果JSONは実装検証時点の状態。後続の明示依頼により7.2をコミットし、CLIをv1.14.0として公開した。版更新・配布物の確認は[リリース記録](../releases/release-checkpoints.md)を参照。v2タグは未公開。
