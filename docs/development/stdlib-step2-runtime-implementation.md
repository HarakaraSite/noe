# NOE版stdlib：ステップ2・runtime切替と検証記録

2026-10-19。[計画](stdlib-step2-runtime-plan.md)に沿って実装・関連テスト・独立した通常コードレビューを行い、その後の総合テストまで完了しました。[構造化結果](stdlib-step2-runtime-results.json)も参照してください。

本記録と結果JSONは実装検証時点の記録です。v1.10.0へのversion更新後の再検証と配布物の確認は[リリース記録](../releases/release-checkpoints.md)へ保存します。

## 結果と変更範囲

NOEのmain runtime、Node形式Worker・孫Worker、Web Worker、agentのtaskと`run_js`が、取り込み済みの`noe/internal/stdlib`を利用する構成になりました。S2-A（変更前の基準）、S2-B（接続の切替）、S2-C（実装由来の説明）、S2-D（レビュー・総合検証・引継ぎ）は完了です。

基点はステップ1のcommit `abddc24448feb054b7f0b9b6bafaadc1862a4851`。JavaScriptエンジンは引き続き`github.com/go-quickjs/go-quickjs v0.27.0`です。stdlibの原型commit・checksum・取り込み差分とlicenseは[UPSTREAM.md](../../internal/stdlib/UPSTREAM.md)に維持しています。

| 対象 | 変更 |
| --- | --- |
| `internal/runtime/stdlib.go`・`eventloop.go`・`runtime.go` | Install/Config、Loop、ExitCodeのimportをローカルstdlibへ揃えた |
| `internal/modulemap/quickjs_test.go` | ローカルstdlibの登録moduleと予約一覧を比較する構成へ変更 |
| `internal/nativeio/streams_test.go`・`net_test.go`・`signals_process_test.go` | productionと同じローカルstdlibで統合テストを実行する構成へ変更 |
| modulemapのコメント・内蔵docs・instructions・現行ガイド・UPSTREAM | エンジン、原型v0.27.0、NOEが保守するstdlibを区別する説明へ更新 |

Goの動作変更はproduction 3ファイルと統合test 4ファイルのimport切替です。Config、初期化順序、Worker loader/Installed hook、所有goroutine、取消し・終了、agent/nativeioの登録は変更していません。`Workers.Max: -1`、process値、HTTPの既定値・buffering、API名・aliasも維持しています。stdlib本体・wsproto・テスト・fixture・license、`go.mod`・`go.sum`、rootの既存配布binaryは変更していません。

内蔵API説明とagentの初期instructionsは英文です。26 moduleの実装由来をNOE管理のsnapshotとして説明し、index・globals・NOE概要も更新しました。API詳細のtopic/member集合は維持しています。`docs/api.md`の古いserve版表記は、現在の実装を確認して更新しました。

## 変更前後のAPI比較

rootの既存binaryを使わず、変更前のsourceから一時CLIをビルドしました。mainからNode形式Worker、その子から孫Worker、別にWeb Workerを起動し、各runtime内で同じinventory関数を実行しました。切替後のsourceからビルドしたCLIでも同じscriptを実行し、4モードのJSON全体が完全一致しました。

| mode | global名 | module | docs topic | 詳細member |
| --- | --- | --- | --- | --- |
| main | 119 | 26 | 40 | 98 |
| Node形式Worker | 119 | 26 | 40 | 98 |
| 孫Worker | 119 | 26 | 40 | 98 |
| Web Worker | 129 | 26 | 40 | 98 |

比較対象は、global名の集合、26 moduleのbare名/`node:`名それぞれのexport名・型・exportごとの値/参照の対応、docs topic/member ID、`process.version`です。Buffer、ReadableStream/WritableStream/TransformStream、MessageChannel/BroadcastChannelのmodule exportとglobalの同一性、およびRequest/Responseのbodyが同じReadableStreamに属することも各runtime内で確認しました。異なるruntime間のconstructor参照の同一性は要求していません。

docs本文の出自説明は意図した変更のため、本文文字列の一致は要求していません。新しいCLIの`noe docs stdlib`でNOE管理の実装と原型版の説明が返ることも確認しました。

## 関連テストと独立コードレビュー

最初に`CGO_ENABLED=0 go test -json ./internal/runtime ./internal/nativeio ./internal/modulemap ./internal/stdlib/... -count=1 -timeout=180s`を実行し、5packageすべて成功しました。runtimeは38.171秒、nativeioは0.577秒、modulemapは0.027秒、stdlibは8.661秒、wsprotoは0.003秒です。

その後、独立レビュアー`/root/stdlib_step2_code_review`が会話履歴を継承せずread-onlyで通常レビューを行いました。P0・P1・P2・P3はすべて0件。切替の完全性、main/WorkerのConfig・Loop・終了処理、agentへ渡すCLI、API比較、docsと変更境界を確認し、総合テストへ進める判定でした。具体的な不足caseがなく、既存テストと変更前後の比較で今回の切替を確認できるため、新しいテストは追加していません。

コード修正要求がなく、再レビューは行っていません。[レビュー記録](stdlib-step2-runtime-review.md)に評価範囲と限界を保存しています。レビュー対象40ファイルのmanifest SHA256は`d6f7f55c316682702017261bb9d391011d0a9435911f095801a91c3a0bcf58c4`。総合テスト後にも同じhashを確認しました。ファイルごとのhashは構造化結果に保存しています。

## レビュー後の総合テスト

Go 1.27.2、Linux amd64で実行しました。

| 確認 | コマンドと結果 |
| --- | --- |
| 全体の単体テスト | `CGO_ENABLED=0 go test -json ./... -count=1 -timeout=180s`成功。テストのある23packageが成功、2packageはテストなし |
| internal全体のrace | `CGO_ENABLED=1 go test -race -json ./internal/... -count=1 -timeout=300s`成功。20packageが成功、1packageはテストなし |
| CLI・fixture agent E2E | `CGO_ENABLED=0 go test -json -tags=e2e ./tests -count=1 -timeout=10m`成功、25.757秒。変更後sourceから作ったCLIでrun/eval、Worker、HTTP/WebSocket、I/O、終了・取消し、agent task・`run_js`・子agent・docs等を確認 |
| E2Eを含むvet | `CGO_ENABLED=0 go vet -tags=e2e ./...`成功 |
| 6対象ビルド | Linux/Darwin/Windows × amd64/arm64でCGO無効のCLIを一時出力先へビルド。6件成功 |
| 6対象の統合test compile | 同じ6対象でruntime/nativeio/modulemapをpackage別に`go test -c`。18件成功 |
| production依存 | `go list -deps ./cmd/noe`に`noe/internal/stdlib`があり、上流stdlibがない。go-quickjsエンジンへの依存は維持 |
| 直接import | `internal`・`cmd`のGo source/testに上流stdlibの直接importがない |
| 変更境界 | 作業前hashと比較した46ファイルのうち変更は`internal/stdlib/UPSTREAM.md`の採用状態だけ。依存定義・既存配布binary・stdlib本体等は一致 |

native実行はLinux amd64です。他の5対象はコンパイル確認で、実機実行ではありません。cross-compileでは`CGO_ENABLED=0 GOOS=<os> GOARCH=<arch>`、CLIは`go build -p=2 -o <temporary-output> ./cmd/noe`、testは`go test -p=2 -c -o <temporary-output> ./internal/<package>`を用いました。

既存のskipは次のとおりで、新しいskipや期待値変更はありません。

- stdlibの`TestHelperHeartbeat`は終了処理テストから子processとして起動するhelperのため通常列挙ではskip。
- `TestOpenAIProviderLive`、`TestImageProviderSpike`、`TestImageWireLiveDiagnostic`、`TestLive`（opencode-go）は実provider用opt-in条件がなくskip。
- E2Eの`TestImportMapMCPProcessIO`は事前作成済み`NOE_MCP_SDK_BUNDLE`が未指定のためskip。bundleの新規取得は行っていません。通常suiteでskipした実provider用の広い検証とは別に、利用者の追加依頼で次の基本フローを実接続で確認しました。

生ログ、変更前後のCLI・inventoryとscript、依存一覧、hash manifest、ビルド出力は`/tmp/noe-stdlib-step2-20261019-vl4czy1h/`に保存しました。主要なコマンド・結果・skip・対象・hashは[JSON記録](stdlib-step2-runtime-results.json)へ保存しています。

## 追加の実プロバイダ基本フロー

利用者の「実プロバイダで基本的な流れを確認する」という追加依頼に基づき、OpenCode Go / `deepseek-v4.1-flash`、`effort: auto`で1課題を実行しました。前述の切替後sourceからビルドした同じ一時CLIを使用し、コード・初期instructionsは変更していません。

一時workspaceの`numbers.json`（`[3,7,10,20]`）をJavaScriptで読み、件数・合計・平均を計算して`report.json`へ保存し、再読み込みで確認して最終回答する課題です。CLIの実行上限115秒、provider要求上限6回とし、追加のworkspace/common instructionsを混ぜず、既存の認証ファイルを通じて接続しました。

| 観点 | 結果 |
| --- | --- |
| agentの起動・実モデルとの往復 | 成功。provider要求3回 |
| `run_js` | 2回ともexit code 0。`node:fs`・`node:path`を使い、読み込み・集計・保存・再読み込みを実行 |
| 成果物 | `{count:4,sum:40,mean:10}`でテスト側の期待値と一致。入力ファイルも維持 |
| 最終回答と終了 | `lastResult.state: completed`、結果を含む英文回答、CLI exit code 0 |
| 所要時間 | 10.368秒、初回試行で成功 |

実行script・イベント・stdout/stderr・成果物・集計は`/tmp/noe-stdlib-step2-20261019-vl4czy1h/live-smoke/`、課題文・実行したtool code・結果・CLI hashは構造化結果の`live_provider_smoke`へ保存しました。API keyや認証headerは記録していません。確認範囲は基本的な正常系です。任意のMCP SDK bundle検証は引き続き未実行です。

## 次の段階への引継ぎ

NOEが自分で保守するstdlibで従来のスクリプト・Worker・agentを実行する状態まで確認できました。後続のステップ3では、この状態を基準に`noe:http`の入口を計画できます。今回HTTP機能追加、API改名、外部向けlibrary化、agent/nativeioの移動は行っていません。

ステップ1の公開`Runtime.EvalFile`適合は維持しています。NOE mainとWorkerはdebugger無効ですが、将来debugger有効hostへ公開する場合は内部scriptの表示・計装が上流と異なる点を引き継ぎます。

commit・push・releaseは実施していません。
