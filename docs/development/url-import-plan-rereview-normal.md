# URL import計画：通常再レビュー

## 結論

**実装着手可。計画修正を必須とする重要指摘なし（RN指摘なし）。**

正本 `url-import-plan.md` と `url-import-slices.md` を全文確認し、`AGENTS.md` と下記の現行コードを照合した。利用者合意、追加API、既存互換、スライス依存順と成立条件の間に、実装を妨げる具体的な不整合は認めなかった。これは計画の承認であり、未実装機能の動作保証やコードレビュー完了を意味しない。

他レビュアーの結果は参照していない。製品コード・正本・他レビューの変更、追加委譲、外部接続、テスト実行はしていない。

## 重要指摘ID・根拠と影響

重要指摘ID：**なし**。以下は確認した成立根拠であり、新たな修正要求ではない。

- **公開Go APIと互換性（正本§6・§9、S2）**
  現行 `stdlib/profile.go` は `Profile / Origin / LoaderFactory` を `internal/jsexec` から公開し、`internal/jsexec/profile_options.go` の `Profile.NewLoader` は関数型である。別フィールド `Loader` にproviderを足す方式なら、この既存関数型と `FileLoader` の呼出し契約を維持できる。両方指定の設定エラー、標準providerのHTTP nil/non-nilとmetadataの対応、旧関数を推測せずunknownにする規則も具体化済み。既存 `FileLoader` は `Origin.FileSystem` のみで読み取るので、URL loader追加時にも選択FS・Fetchと独立させる方針は現行権限境界と一致する。S2の公開Execute・option copy・競合・FS/Fetch独立の確認が、この変更の利用経路に対応している。

- **解決とmodule同一性（正本§5、S1→S2）**
  現行 `internal/modulemap/snapshot.go / path.go / virtual.go` はローカルpath解決と拡張子検査を持ち、`loader.go` は解決IDごとにsourceを保持する。S1で純粋なURL解決を切り出し、S2でURL取得と最終ID対応を足す順序は妥当。ローカル拡張子制限とURL拡張子不問を分け、remote referrerの `/`・`//` 等をOS path処理へ渡さない条件も明示されている。固定依存go-quickjs v0.29.0の `internal/vm/module.go:loadDependency` はloader呼出し後に `r.modules[resolved]` を参照するため、複数の要求から同じ最終IDを返す設計は既存engine境界と整合する。ただし、redirectと循環を組み合わせた実証はS2のfixtureに残る。計画もそれを未検証の保証として扱っていない。

- **Worker入口と依存の分離（正本§6.4、S2→S3）**
  現行 `internal/jsexec/execution_loader.go:prepareWorker` は `Origin{Worker:true, Source:nil}` でloaderを作り、入口も `loadModule` で読む。したがってHTTPを汎用loaderへ足すだけでは入口まで有効になるが、計画はこの現経路を明示的に取り上げ、初回入口のpure resolve→local判定→readで拒否する。URL aliasも接続前に拒否し、同じWorkerの依存HTTPは許す条件であり、単なるresolved IDの事後検査やWorker全体のHTTP禁止を要求していない。`internal/stdlib/workerscript.go` のnode Worker固有のpath検査と、global Workerのlocal alias経路を統一しない点も互換維持に適切。S2で機構、S3で実際のWorker経路を確認する分担に欠落はない。

- **同期HTTPの取消し（正本§7・§8、S2→S3）**
  現行 `internal/jsexec/runtime.go:Runtime.Cancel` はcontextを直接取り消し、`profile.go:attach` はloaderへ渡す実行contextを作る。Worker側も `internal/stdlib/worker_runtime.go` で取消可能なloop contextを使う。これにGo HTTP要求を接続する案は、owner上の同期callbackをJS timerで中断しようとする設計ではない。ヘッダー待ちと本文待ち、contextとRuntime.CancelをS2で確認し、Host/task経路をS3へ進める条件は、実際の停止動作を評価できる。body close・借用client非変更・各redirectのAllow/CheckRedirectも同じ取得経路の契約として確認対象になっている。

- **Host能力通知と独立runtime（正本§6.3・§9、S3）**
  現行 `agent/host.go:NewHost` はProfileをsnapshotしてexecutorに保持し、`internal/agentexec/executor.go:Execute` は実行ごとに `stdlib.Execute` を呼ぶ。snapshotしたProfileからmetadataを判定してcore.Optionsへ渡す計画は、実際に適用する実行設定とfactsを同じ根拠にできる。`internal/agent/host.go:instructionFacts` は現在静的Factsを返すため、ここへ動的事実を接続する変更対象も明確。provider/loaderの状態をHost全体のsource cacheと混同せず、親子agent・別run_js・Workerで独立loaderを作る条件とfixture modelによる確認が置かれている。

- **スライスごとの完了と最終利用（S1→S2→S3→S4）**
  純粋resolver→取得/API→CLI/Worker/agent→利用文書/外部Go consumer/公開ESMの順に、各成果物の依存が解消される。static/re-export/dynamic、import map、text/plain、同一namespace、独立実行、取消し、入口拒否、能力factsがそれぞれ担当スライスの製品動作に結び付いている。公開ESM一件の実利用をS4へ置き、通常テストはローカルfixtureにする区別も適切。各sliceの対象テストと独立コードレビュー後、最終候補へ総合テスト・文書記録・コミットを集約する進め方に問題はない。

## 最小修正・残件

**再レビューに基づく必須修正はない。ユーザー判断待ちもない。**

実装時に残るのは計画済みの成立証拠であり、追加scopeではない。とくにS2のredirect/alias/循環のmodule同一性、同期HTTP中の取消し、S3のWorker入口非接続とHost能力通知、S4の公開consumerと実公開ESMの実行結果を、該当sliceのコードレビューで確認する。現時点で未実装であること自体を指摘や停止理由にはしない。

記録path：`docs/development/url-import-plan-rereview-normal.md`
