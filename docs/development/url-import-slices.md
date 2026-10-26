# URL import：実装スライスと再レビュー対象

状態：計画再レビュー、4スライスの実装/対象テスト/独立コードレビュー、最後の総合検証まで完了。以下は実施した手順と終了条件であり、結果は[実装記録](url-import-implementation.md)に集約する。

正本：[設計案](url-import-plan.md)。利用者の追加指示により、計画の再レビュー→各スライスの実装・対象テスト・コードレビュー→最後の総合テスト→コミットの順で進める。[実装記録](url-import-implementation.md)に結果を集約する。

## 変更しない利用方針

公開ESMも対象、HTTP/HTTPS両方、static/re-export/dynamic/import map URL target、URLの実行入口は初回対象外。JavaScript MIMEとtext/plainを許し、URL拡張子は不問。ダウンロードキャッシュ・lockfile・TS変換・npm探索なし。通常のruntime-local source/module保持は維持。CLI既定有効、Go組込みは明示的provider選択。親agentが指示文やspawn引数でホスト権限を昇格する機能は追加しない。

## 計画再レビュー

通常レビュアーと批判的レビュアーが正本と本書を独立に確認する。重点は各スライスの利用動作、依存順、API互換、標準providerの能力metadata、Worker入口と依存の分離、同期HTTPの取消し、URL/redirectのmodule同一性。未実装で実行証拠がないこと自体を計画不備とはせず、必要な証拠を担当スライスへ対応付ける。

再レビュー記録は url-import-plan-rereview-normal.md / url-import-plan-rereview-critical.md。具体的な指摘を正本へ反映し、必要なら元担当が該当指摘だけ再確認する。

## S1：URL resolverと既存ローカル境界

目的：HTTP/HTTPS URL、URL referrer、URL targetを副作用なしに解決できる。

担当範囲：
- internal/modulemap のsnapshot/path/virtualと新URL helper、対応するtests。
- Loaderの現行ローカル経路はURLを取得せず、明示的な未対応エラーにする。HTTP取得はS2まで導入しない。
- host/scheme小文字化、既定port/空path/literal dot segmentの正規化、query/fragment保持。
- remote referrerの./、../、/、//、?、#参照をURL解決。明示file URLはreferrerに依存せず選択FSへのローカルIDへ解決。
- import mapの完全一致bare targetに絶対HTTP/HTTPSを許可。virtual/rooted FSでもURLをOS path化しない。
- ローカル.js/.mjs、file URL制限、builtin予約、未登録bareエラー、map snapshotの寿命を維持。

必要な確認：
- resolver/map/virtualの対象unit、固定engineを使う既存modulemap tests。
- query順やpercent-escapeの意味を変えず、未知scheme/不正URLを診断。
- ローカル既存テストを変更の都合で削除・無効化しない。

完了：go test ./internal/modulemap 成功、gofmtと差分確認、独立コードレビューの具体的指摘を解消。HTTP取得は未実装と記録する。

## S2：URL loader・HTTP・公開Go provider

依存：S1完了。

目的：Goホストが明示的にURL importを許し、実行contextで取消せる。

担当範囲：
- modulemap.Loaderにremote readerと最終ID/元URL対応、runtime-local source保持を追加。既存NewLoader/NewReaderLoader契約は維持する。
- stdlib.ModuleLoader(LoaderOptions)とModuleHTTPOptionsを追加。HTTP nilはdisabled、非nilはenabled。各runtimeで独立loaderを生成。
- jsexec.Profile.LoaderとModuleLoaderProvider/URLImportSupportを追加。Profile.NewLoaderは旧関数型のまま維持、同時指定は設定エラー。
- provider.NewLoader(context, Origin)とURLImports()でfactoryとmetadataを一緒に渡す。URLImportCapability(Profile)が判定を一元化。旧NewLoaderはidentity推測せずunknown、両方nilはdisabled。公開stdlibへ必要型・定数・helperをexport。
- FileLoaderは従来どおりFSだけ。URL target設定もローカル専用として拒否し、HTTP Client/Fetch等を暗黙利用しない。
- HTTP GET/2xx、MIME、UTF-8/BOM、body上限、response body close、borrowed client/CheckRedirect/Allowの整合、context取消し。
- 最終URLをIDにし、fragmentをLocationに従って継承/置換。保持済み最終IDのsourceを後続取得で置換しない。
- Worker入口はOrigin（Workerかつ初回Source nil）の入口読取りだけをpure resolve→local判定→readに限定する。URL aliasも取得前に拒否。同じWorkerの依存HTTPを禁止しない。custom providerはホスト責任。

必要な確認：
- stdlibの公開Executeから直接URL/static/dynamic/re-export、相対graph、URL alias、最終URL基準、namespace一回/循環が成立。
- text/plainとJS MIME、パラメータ、不正MIME/UTF-8/HTTP失敗、上限とbody closeの具体的経路。
- Go提供clientを変更/closeせずAllowを各redirectに適用、既存CheckRedirectを尊重。
- ヘッダー待ち・本文待ちをcontext/Runtime.Cancelから取消し、同じruntime JS timerに頼らず実行が戻る。
- Profile.Loader/NewLoader競合、標準provider metadata、旧関数unknown、option copy、FS/Fetch独立。
- 同一runtimeは保持、独立runtimeは再取得。未知redirect URLを知るための接続とダウンロードキャッシュを混同しない。

完了：go test ./internal/modulemap ./internal/jsexec ./stdlib の変更対象と関連契約が成功、独立コードレビュー済み。成立しないengine条件があればscopeを広げず根拠を返す。

## S3：CLI・Worker・agentの実行経路

依存：S2完了。

目的：CLIとNOE管理の各実行で同じURL import能力を利用し、agentへ正確に通知する。

担当範囲：
- internal/noeprofile.Profileの既定loaderをHTTP有効providerへ変更。
- CLI run/evalでmapのURL targetを継承。CLI run入口はローカルのまま。
- ローカル入口module WorkerからURL依存を使う。global Workerのlocal alias入口は維持。直接URL/URL target alias入口は取得前に拒否。
- agent.NewHostでsnapshot済みProfileから能力factsを生成し、core.Options経由で親子agentのモデル要求へ伝える。
- enabled/disabled/unknownを区別、URL/client/headers/map全文は通知しない。fetchの有効化と独立と説明。
- 一律の「HTTP imports unavailable」という現行docs/factsを更新。全APIカタログを実設定の代わりにしない。

必要な確認：
- CLI実経路でURL/aliasのgraphを利用。
- Workerの入口URLとURL aliasに接続が発生しない。同じWorkerの依存は接続して成功。
- ローカルfixture modelが親子agentのrun_jsを実行し、URL import結果と能力factsの整合を確認。実providerは不要。
- 別run_jsでfixture sourceを更新すると新しい本文を取得する。状態・sourceをHost全体で共有しない。
- enabled/disabled/unknownのfacts、policyやユーザー指示から権限昇格しないことを構成で維持。

完了：変更対象のCLI/Profile/agent/Worker統合テスト、独立コードレビュー済み。外部サービスを通常テストの必須依存にしない。

## S4：利用文書・Go consumer・公開ESMの実利用

依存：S3完了。

目的：利用者が実際に新APIとCLIを使える説明・例を用意し、公開ESMのexportを利用する。

担当範囲：
- docs/url-import.md（またはimport-map guideの適切な節）、API/README（日英）/Go embedding/run_js catalog/将来候補/開発索引の整合。
- public Go consumerのURL provider設定が外部moduleからcompile/runできることをfixtureで確認。
- examples/url-importに最小の公開ESM利用例。static/dynamic/mapの説明、信頼とOS権限、同期取得中JS timer非進行、cacheなし、MIME、npm/TS非互換を明記。
- 選定公開ESM（例：esm.sh/lodash-es@4.17.21）を実際に取得してchunk等のexportを使う。これは一件の成立証拠であり任意npm互換の保証ではない。

必要な確認：
- Go consumerと例の対象テスト。保存したJSはbiome lint。
- 新文書のリンク/API例の整合、外部ESMの実行結果。
- CDNが外因で利用不能の場合、原因と到達点を記録し、別の同等公開ESMで目的を満たせるか具体的に判断する。fixture成功だけで公開利用成立を偽らない。

完了：利用ガイド・consumer・公開利用の証拠が揃い、独立コードレビュー済み。

## 最後の総合テストとコミット（親が集約）

各スライスの修正とレビューが落ち着いた候補に対して、原則一回まとめて実行する。

- go test -tags=e2e ./...（unitと通常E2Eを一度に含める）。
- go vet -tags=e2e ./...、gofmt/JS lint、git diff --check。
- 共有provider/HTTP clientとWorkerの並行利用に対応するaffected packagesのrace確認を必要範囲で行う。配布全OSのビルドや実provider検証はこの変更の成立条件へ自動追加しない。
- 失敗は今回の変更から到達する経路を確認し、対象を絞って退行か既存別件かを分ける。関連修正のみ必要範囲を再確認し、全メニューの機械的再実行はしない。
- 実装記録、レビュー採否、実際の確認結果、未解決事項を更新。
- 既存の.noe/eventsや__pycache__等の無関係な未追跡ファイル、生成バイナリを含めず、今回の変更を明示的にstageしてコミット。
- コミットhash、必要な動作が成立した根拠、利用判断に影響する残件を報告して終了。

機能実装や設計再レビュー前にコミットを先行させない。sliceごとのcommitは必須ではなく、最終検証後にまとまった機能commitを作る。
