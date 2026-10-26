# URL import：独立コードレビュー

## S1：URL resolverと既存ローカル境界（通常レビュー）

### 結論

**承認。重要指摘なし（S1-R指摘なし）。S1の範囲で必須修正・未解決事項はない。**

正本plan/slicesのS1と、`git diff -- internal/modulemap`、未追跡の `url.go / url_test.go` を確認した。HTTP取得・redirect・HTTP有効providerはS2の対象であり、この承認には含めない。他レビュアー結果は参照していない。製品・正本は編集していない。

### コード根拠と対応する利用動作

- `snapshot.go:Snapshot.Resolve` は完全一致mapの後、local/virtual path分岐の前に `resolveHTTPReference` を呼ぶ。remoteの `./ ../ / // ? #` がOS path処理へ入らず、明示file URL・local map targetは従来のローカル経路を維持する。`url_test.go:TestResolveRemoteReferences / TestRemoteReferrerExplicitFileURLIsLocal` がそれぞれを確認する。
- `url.go:CanonicalHTTPURL` はHTTP/HTTPSの階層URLとhostを検証し、scheme/host小文字化・既定port除去・空path・literal dot segmentを正規化する。`net/url` のescaped pathを使う構成で、URLの拡張子を制限せず、query順・escape・非空fragmentを保持する。`TestCanonicalHTTPURL` はこの規則と再正規化の同一性、`TestHTTPURLValidation` は不正URLの直接指定・map指定を確認する。
- `snapshot.go:resolveTarget / virtual.go:virtualTarget` はHTTP URLだけをlocal拡張子検査から分離する。local targetの基準directory、key制約・builtin予約・重複JSON検査、snapshotのcopy/transportは維持される。`TestURLMapTargetsStayRemoteAndRelativeTargetsStayLocal` がremote/localの分離とsnapshot不変性を確認する。旧拒否テストのHTTPをFTPへ置き換えた変更は、今回の許可範囲に一致しており、ローカル契約を弱めていない。
- `loader.go:NewReaderLoader / Loader.Load` はHTTP入口名および解決済みHTTP IDを、filesystem callbackや拡張子検査に渡す前に明示的に拒否する。`TestLocalLoaderRejectsHTTPWithoutFilesystemRead` は直接URL・alias・remote相対参照でread callback未呼出しと、明示file URL/local aliasの読取り・source共有を確認する。HTTP非取得はS1の意図した境界である。

### 検証・最小修正・残件

実装担当の `/var/tmp/noe-url-import-s1-tests.log` を確認した。`go test -v ./internal/modulemap` と最終候補の同package testは成功し、既存QuickJSのalias/namespace・循環・in-memory入口・builtin優先のテストも成功している。gofmtと差分の空白検査の成功記録もある。コード確認で追加実行が必要になる具体的な疑義はなく、レビュー側ではテストを重複実行していない。全体test・外部接続・追加委譲は行っていない。

**最小修正：不要。** URL取得と新providerの権限境界、redirect/module同一性、取消しは計画どおりS2で実装・検証するもので、S1の未解決不具合には数えない。

## S2：URL loader・HTTP・公開Go provider（通常レビュー）

### 結論

**承認。重要指摘なし（S2-R指摘なし）。S2の必須修正・未解決事項はない。**

正本planの§5〜§9とslicesのS2を基準に、tracked差分と未追跡Go filesを確認した。対象はmodulemapのremote reader/source保持、stdlibのprovider・HTTP reader・公開alias/helper、jsexecのProfile競合/選択/metadataおよび関連tests。S1の変更との接続も確認した。S3のCLI/Host factsとS4の利用文書・公開consumerは承認範囲外であり、未実装をS2の不足とは扱わない。

### コード根拠・実利用経路と検証

- **module ID / namespace / redirect / 循環**
  `internal/modulemap/loader.go:Loader.load` は要求ID→最終IDのaliasとcanonical IDのsourceをruntime-localに保持する。既知IDは読取り前に返し、未知redirectから保持済み最終IDへ到達した場合も古いsourceを優先する。`stdlib/loader_http.go:httpModuleReader` は最終要求URLを返し、Locationの非指定fragment継承と明示fragment（空を含む）の置換をredirect hookで行う。`TestModuleLoaderURLGraphRedirectIdentityAndCycle` は公開Execute経由でstatic/re-export/dynamic、alias/直接URL/最終URLのnamespace一致、副作用一回、最終URL基準の相対依存、redirectを含む循環と未知alias後のsource非置換を確認する。`TestModuleLoaderFragmentsAndQueriesAreModuleIDs` はquery/fragment別評価と空fragment統合、request targetへfragmentを送らない動作を確認している。

- **取消し・HTTP本文の契約**
  `httpModuleReader` はfactoryの実行contextでGETし、同期読取り中も同じHTTP要求contextを維持する。bodyは取得後直ちにdefer Closeされ、status/MIME/読取り/limit/UTF-8エラーおよび保持済みsourceへの合流でもcloseされる。上限は実際に読む本文へ適用し、0=32MiB、負数=無制限。MIME集合、UTF-8/BOMの処理も正本どおり。`TestModuleHTTPCancelHeaderAndBodyWaits` はヘッダー待ちと本文待ちをそれぞれcontext/Runtime.Cancelで止め、取消しが期限切れに化けずserver要求も終了することを確認する。`TestModuleHTTPValidatesResponsesAndClosesOwnedBodies / AcceptsJavaScriptMIMEContract / DefaultAndDecompressedBodyLimit / ReadFailuresAreNotRetained` が失敗区別、close、圧縮展開後上限、失敗後の再取得に対応する。

- **borrowed client / Allow / CheckRedirect**
  clientは値copyし、元のTimeout/Transport/Jar/CheckRedirectを変更・closeしない。既存CheckRedirectの拒否とErrUseLastResponseを尊重し、独自hook設置時も標準のredirect回数制限を維持する。実際に追跡する各要求へscheme検証とAllowを適用する。`TestModuleHTTPRedirectFragmentsAndBorrowedClient / PermissionAndRedirectRejection` は初回・redirectの許否、既存hook、非HTTP遷移、fragmentとclient再利用を確認する。未知aliasへの応答本文を採用しない場合にもresponse bodyを閉じる構造である。

- **option snapshot / runtime独立 / FileLoader互換**
  `stdlib/loader.go:ModuleLoader` はImportsをencodeして固定し、HTTP option値をcopy、client/callbackだけを借用する。`newModuleLoader` は毎回新しいmodulemap loaderを作り、FileLoaderまたはHTTP nilではURL targetを設定段階で拒否し、FSはOrigin.FileSystem以外から取得しない。`TestModuleLoaderOptionsSnapshotAndFreshRuntimeSource` はoption変更からの隔離、HTTPS fixture、未使用mapへの非接続、同一runtimeのnamespace保持と別runtimeの再取得を確認。`TestModuleLoaderFSAndFetchStayIndependent` はHTTP/Fetch/FSの独立、旧FileLoaderのURL拒否とremoteから選択FSへの明示file URLを確認している。既存FileLoaderのrooted FS/Worker/cache testsの成功記録もある。

- **Worker入口と依存 / Profile選択 / metadata**
  `newModuleLoader` の入口pending分岐は現行prepareWorkerが最初に渡すOrigin.Entryに対してだけ `LoadLocal` を使い、pure resolve後・HTTP取得前にURL/URL aliasを拒否する。その後の依存は通常LoadでありWorker全体をHTTP禁止にしない。`TestModuleLoaderWorkerEntryPureResolutionAndDependencyHTTP` はprovider入口で非接続拒否と、実Workerのlocal alias入口→URL依存成功を確認する。constructorからのURL拒否統合はS3の対象。
  `internal/jsexec/profile_options.go:normalize` はLoader/NewLoader競合をfactory呼出し前に拒否し、`profile.go:attach` はprovider.NewLoaderを選択。`loader_provider.go:URLImportCapability` は旧関数を推測せずunknown、loaderなしdisabled、未知metadataをunknownにする。`stdlib/profile.go` の公開alias/定数/helperも一致する。`loader_provider_test.go` と公開capability testがsnapshotの借用・factory選択・競合・通知値を確認している。

### 検証・最小修正・残件

実装担当の `/var/tmp/noe-url-import-s2-tests.log` を確認。初期のmime.ParseMediaType戻り値数のbuild失敗は現行ソースで修正され、後続の対象testsと最終 `go test -v ./internal/modulemap ./internal/jsexec ./stdlib` はすべて成功している。gofmt/差分空白検査の成功記録もある。コードとtestsを照合して追加実行で解決すべき具体的な疑義はなく、レビュー側では重複テストをしていない。全体test・外部接続・追加委譲は行っていない。

**最小修正：不要。** S3のCLI既定・実Worker入口拒否・Host facts/親子agent接続とS4の利用証拠は、予定された後続作業として残る。

## S3：CLI・Worker・agentの実行経路（通常レビュー）

### 結論

**承認。重要指摘なし（S3-R指摘なし）。S3の必須修正・未解決事項はない。**

slicesのS3と正本のCLI/Host/Worker契約に照らし、指定されたtracked差分・新Go files・instruction prefix更新testsを確認した。S1/S2は承認済みの前提とし、実行Profileとfacts、Worker、取消しへつながる影響経路のみ再参照した。他レビュアー結果は参照せず、製品・正本は編集していない。

### コード根拠と利用動作

- **CLI既定・map継承**：`internal/noeprofile/profile.go:Profile` はImportsを引き継いだHTTP非nilの標準providerを選択する。`internal/runtime/managed.go:newManagedExecution` は同じProfileをCLI入口runtimeとHostのtask templateへ渡す構成を維持する。`internal/cli/url_import_test.go` はrun/evalのURL・alias・re-export/相対依存graph、namespace一致、別実行の再取得、未使用targetの非接続、およびCLI URL入口の非接続拒否を確認している。`internal/runtime/runtime.go` の変更はImportMapコメントの実態への整合のみ。

- **能力facts・秘密非通知**：`agent/host.go:NewHost` はsnapshot済みexecutor.Profileに対して公開capability helperを呼び、実際に使用するloaderと同じ設定から `core.Options.RuntimeFacts` を作る。Runtime未設定はdisabled。`internal/agent/host.go:instructionFacts` は汎用Factsの直後にこの事実を付加し、既存のpolicy/instruction/explicit systemの合成順を維持する。`url_import_facts.go` は能力値だけを定型文へ変換し、map/URL/client/header等を渡す経路はない。`agent/url_import_test.go:TestGoHostURLImportFactsMatchSnapshottedProfile` がenabled・Fetch有効でもdisabled・legacy unknown・no-loader disabled、作成後のcaller Profile変更からの隔離、秘密URL非通知を確認する。通知の追加はProfileを変更する操作ではなく、ユーザー指示・spawn設定による権限昇格経路も追加されていない。

- **親子agent・runtime独立**：`tests/url_import_agent_e2e_test.go` はfixture modelから親のrun_jsを二回、子のrun_jsを一回実行し、全要求のenabled通知、URL map継承、相対URL依存の利用、同一runtimeのmodule同一性と評価一回、配信更新後の別run_js再取得と子runtime独立を確認する。source/dependency要求回数も実際の取得回数に対応している。

- **Worker入口と依存・task取消し**：`internal/runtime/url_import_test.go` はCLI標準Profileでglobal Workerのlocal alias入口→URL依存graph成功と、直接URL/URL alias入口のエラー・HTTP未接続を同じ実経路で確認する。`agent/url_import_test.go:TestGoHostTaskCancellationStopsURLBodyRead` はHost配下のtaskがHTTP本文待ちに入ってからCancelTaskを呼び、cancelled outcomeとserver側の切断を確認している。これによりS2のcontext接続がtask管理経路からも働く根拠がある。

- **既存互換・文書**：既存instruction testsの変更は期待prefixへruntime factsを追加するもので、policy/ユーザー指示/explicit systemの順序・一回性やinstance snapshotの確認を弱めていない。`facts.md` とrun_js catalogは、全API一覧と実能力、CLI既定有効とGo明示選択、Fetch独立、同期取得・runtime間cacheなしを矛盾なく説明している。S4の利用guide/consumerをここへ追加要求しない。

### 検証・最小修正・残件

`/var/tmp/noe-url-import-s3-unit-tests.log` の `go test ./agent ./internal/agent ./internal/runtime ./internal/cli ./internal/noeprofile` 全成功と、`/var/tmp/noe-url-import-s3-e2e-recheck.log` の新URL E2E成功を確認した。新E2Eのfixtureは `NOE_AGENT_TRACE=""` を明示し、外部環境のtrace出力にstderr期待が左右されない構成になっている。追加確認が必要となる具体的な疑義はなく、レビュー側では重複テストをしていない。全体test・外部接続・追加委譲は行っていない。

**最小修正：不要。** 残るのは計画S4の利用文書・外部Go consumer・公開ESM実利用と、各slice完了後の総合検証であり、S3の未解決不具合はない。

## S4：利用文書・Go consumer・公開ESMの実利用（通常レビュー）

### 結論

**承認。重要指摘なし（S4-R指摘なし）。今回差分の必須修正・未解決事項はない。**

README日英、API/import-map/Go embedding/Go agent embedding/future-candidatesと各索引の差分、新規URL guide・examples一式、外部consumerへの追加、計画の現在状態と実装記録を確認した。S1〜S3は承認済みとして、文書・例に対応する公開APIと外部consumer実行経路だけを参照した。他レビュアー結果を追加で参照せず、製品・正本・利用文書は編集していない。

### 根拠と実利用

- **外部Go consumer**：`stdlib/testdata/consumer/main.go` は新規 `urlImports(ctx)` を実行する。`url_import.go` はpublic stdlibだけを使い、新provider/options/capability helper/定数を外部packageから利用する。alias/staticと直接dynamicのnamespace一致、独立Executeでの配信更新、Fetch非付与も実行している。既存 `TestExternalConsumerPortable` はfixtureの全Go filesをcheckout外の独立moduleへcopyし、GOWORK=off・CGO_ENABLED=0でcompile/runするため、新ファイルも検証対象になる。旧FileLoader consumerの経路は削除されていない。`/var/tmp/noe-url-import-s4-consumer.log` で同test成功を確認した。

- **公開ESMの例**：`main.js` は拡張子なしの実公開URLからchunkをstatic importし、`mapped.js` はmap aliasのstatic importと同じURLへのdynamic importのnamespace同一性を確認する。mapと実行コマンド、READMEの期待出力がコードと一致している。`s4-js-lint.log` は両JSのlint成功。`s4-build.log` と `s4-public-esm.log` および実装記録には新binaryで両例を実行した証拠があり、ともに成功・stdout `[[1,2],[3,4],[5]]\n`・stderrなし。これは `https://esm.sh/lodash-es@4.17.21` 一件の利用成立であって、任意npm互換・将来のCDN可用性・本文固定の保証ではない、という説明も明確である。

- **API・能力・制約の説明**：新guideとembedding文書はProfile.Loaderと旧NewLoaderの排他、HTTP nil/non-nil、借用client/callback/provider、option/map copy、FS/Fetch独立、旧FileLoader維持、unknown metadataの理由を現APIと一致して説明する。CLI既定有効とGo明示選択、Host snapshot由来の通知・秘密非通知も一致する。URL入口非対応、Worker入口のみの制限、TS/JSX変換・npm探索なし、JS MIMEとtext/plain、UTF-8/BOM、同期中のJS非進行・ホスト側取消し、runtime内保持とruntime間cache/lockなしの区別がある。Go断片は呼出し元で用意する変数/importを示し、Host例はNewHost前のinitializer置換と旧フィールドを残さない注意がある。

- **現在状態と履歴**：READMEとURL guideは「現在のソースからbuild」の機能であり、既存配布版へ遡って適用されないと説明する。future-candidatesは実装済みURL importと対象外の後続拡張を分ける。開発計画の状態欄と実装記録はS1〜S3承認・S4レビュー中・総合テスト/commit未実施を区別し、初回レビューは当時の記録として維持する構成。公開利用成立をplanの例示だけで代用せず、別途コマンド・結果・一時logへ記録している。新guide・examples・実装記録への索引リンクにも不整合は認めない。

### 最小修正・残件

**最小修正：不要。** 最終総合テストとcommitは親がこのレビュー後に実施する計画済み作業であり、S4の不足・不具合とは扱わない。レビュー側で全体test・外部接続再試行・追加委譲は行っていない。

## 最終検証修正：run_js imports文書assertの追従（通常再確認）

**対応済み。残る具体的問題・必須修正なし。**

`internal/apidocs/catalog_test.go:TestMemberDetailsKeepRunJSExecutionAndImportContext` のtest-only差分と、承認済み `content/noe-run-js.md` のimports節だけを照合した。旧literal `HTTP imports` の要求を、現文書のHTTP/HTTPS ESM URL、CLI既定有効、embedded optionalとFetch独立、JavaScript MIME＋text/plain、TS変換/npm探索/runtime間download cacheなしのassertへ置き換えている。既存のalias・aliasなしローカル読込み・run_jsごとのfresh stateのassertは維持され、削除・弱化によって失敗を隠す変更ではない。製品文書・APIの再変更も不要。

`/var/tmp/noe-url-import-final-apidocs-recheck.log` で当該testとapidocs packageの成功を確認した。全体検査・外部接続・追加委譲は実施せず、再確認の範囲を拡大していない。
