# URL import計画・批判的再レビュー

## 結論

**計画を承認。実装開始を妨げる具体的な重要指摘なし（RC番号の採番なし）。**

`url-import-plan.md`（全320行）、`url-import-slices.md`（全114行）、ルートの`AGENTS.md`を全文確認した。以下は現行コードと固定依存の読取りによる独立判断であり、未実装機能の動作保証ではない。他レビュアーの結果は読んでいない。製品コード・正本は変更せず、外部接続・追加委譲・テスト実行も行っていない。

## 重点経路の検証と根拠

### 同期HTTP中の取消し

現行のmanaged runtimeでは、`Runtime.Cancel()`がGoのcontextを直接取消し、`NewRuntime`→`attach(..., ctx)`→`executionLoader.ctx`→factoryへ伝わる（`internal/jsexec/runtime.go:46–111`、`internal/jsexec/profile.go:93–107`、`internal/jsexec/execution_loader.go:122–137`）。取消しの伝達にruntime owner上のJS jobやtimerを必要としない。Workerも`terminate`でGo contextを取消し、そのcontextからloopとbindingsが作られる（`internal/stdlib/worker.go:316–332, 364–368`、`worker_runtime.go:94–110`）。

したがって、factoryが受けたcontextをHTTP要求・本文読取りへ接続するS2の設計には、現行の所有権モデル上の成立経路がある。JS timerで同期HTTPを止められるという前提にもなっていない。計画済みのヘッダー待ち・本文待ち、およびcontext/Runtime.Cancelのfixtureは、この接続を実証する妥当な完了条件である。

### redirect先のmodule identityと循環

固定版`go-quickjs v0.29.0/internal/vm/module.go:349–402`では、loader呼出しの**後**に返却された`resolved`でmodule registryを照合し、未登録の場合だけcompileする。`requested`はreferrer/specifierごとの既知requestを保持する。グラフ走査もmodule単位のseenを持つ（同:328–345）。NOEの既存`internal/modulemap/quickjs_test.go`にも、alias間のnamespace共有と循環の確認がある。

この順序なら、標準loaderが要求URL→最終IDを保持し、最終IDのsourceを再利用することで、別alias・最終URL・循環から同じmoduleへ合流できる。registryの照合が通信の前に必ず済む、という誤った前提は計画にない。未知redirect URLの通信は残る一方、保持済みsourceは置換しないという仕様も整合する。redirect/fragment込みの成立証拠は、計画どおりS2のfixtureで残す必要があるが、未実装の証拠がないこと自体を計画不備とはしない。

### Worker入口のaliasと依存HTTPの分離

現行`prepareWorker`は`Origin{Worker:true, Source:nil}`でfactoryを作り、入口を同じloaderへ渡す（`internal/jsexec/execution_loader.go:180–197`）。ここへHTTP読取りだけを追加するとURL入口が開く、という計画の認識は正しい。

global Workerはbare文字列をそのまま渡すためローカルtarget aliasの既存経路があり、node Workerの文字列には別のpath検査がある（`internal/stdlib/workerscript.go:54–79, 141–155`）。plan §6.4 / S2は、入口だけをpure resolve→ローカル判定→readとし、URL aliasも取得前に拒否し、後続の依存HTTPは許すと明記している。この境界は現行経路に対応しており、constructorの入力形式を広げる必要もない。

### metadataと実際の能力

現行のHostはRuntime ProfileをsnapshotしてExecutorへ渡し、各実行がそこから新しいruntimeを生成する（`agent/host.go:45–65`、`internal/agentexec/executor.go:50–95`）。既存のfactsは静的だが（`internal/agent/host.go:92–94`）、S3はそのままURL対応を断言するのでなく、snapshot済みProfileからHost固有のfactsへ接続する変更になっている。

新しい構造化providerにfactoryとmetadataをまとめ、旧関数はidentity推測せずunknown、両loaderなしはdisabled、標準providerはHTTP nil/non-nilで判定する契約は、実設定と通知の二重管理を避けられる。FileLoaderの実能力がdisabledでも旧関数metadataはunknownとなる点も明示済みで、誤通知ではない。独自providerの申告、借用client/callback、全URLへの許可やライブラリ互換性を保証しない境界も正本に明記されている。

## 依存順・互換契約

S1の副作用なしresolver、S2のHTTP/provider・入口制限、S3のCLI/Worker/Host通知、S4の公開consumerと実利用という順序は実装可能。S2でengineのidentityと取消しを確認してからS3へ進むため、CLI既定有効化を未確認のengine条件の上へ先行させる構成でもない。

既存の`FileLoader`は選択FSのみを読む（`stdlib/loader.go:34–47`）。計画はこれを別providerへ置き換えず残し、FetchとHTTP importを独立にする。旧`Profile.NewLoader`/`LoaderFactory`保持、競合設定のerror、runtime-local source保持も、確認した現行経路と衝突しない。

## 残件・最小修正

正本への修正要求なし。ユーザー判断待ちなし。

実装後の取消し・redirect namespace/循環・Worker入口の非接続・Host factsの整合は、すでにS2/S3へ割り当てられた確認であり、本レビューの未解決指摘ではない。それらが実装中に成立しない具体的な証拠が得られた場合だけ、該当設計を修正して必要範囲を再確認する。

成果物：`docs/development/url-import-plan-rereview-critical.md`
