# NOE版stdlib：ステップ6・実装と検証記録

2026-10-19。[計画](stdlib-step6-composition-plan.md)のS6-A〜Dを実装し、関連テスト、独立した通常コードレビューと限定再レビュー、その後の総合確認を完了しました。基点はv1.12.0公開確認後の`4ee824d20b78279979176410ebfe79bcc2480277`です。[レビュー記録](stdlib-step6-composition-review.md)と[構造化結果](stdlib-step6-composition-results.json)も参照してください。commit・push・releaseは行っていません。

## 実装した境界

`internal/noestdlib`が、NOE管理のstdlib・nativeio・docsと共有出力を組み立てます。runtimeは同じInstallを使い、CLI固有の権限設定、loader/import map、cwd・環境、終了とagent/auth等を接続します。JSのAPI名・export・設定・HTTP実装・engineと外部依存は変更していません。

| スライス | 実装と確認 |
| --- | --- |
| S6-A | Core/IO/Docsの選択導入、設定複製、SharedWriterの正規化、JS docs adapterを実装。IOにはProcessとWeb APIs、WorkersにはLoopとWeb APIsが必要なことを登録前に検査。DocsだけのNoWebAPIs構成、非busy・読取りなし、二runtime再利用、writer aliasと借用を確認 |
| S6-B | root/子/孫の共通追加API、元Installed callback、子stdinなし、共有出力を接続。子登録失敗後のroot出力継続を確認。DNSを子Loopへ複製・再bindし、fake Resolverによる子だけのloop進行で解決、親設定とServers不変を確認。nativeio.InstallはHost作成後の全error returnをdeferで回収 |
| S6-C | runtimeの直接stdlib/docs/nativeio登録を置換。Bindingsをagent/authへ渡す。CLI/agentをimportしない[英文Go例](../../examples/embedding/README.md)を追加。API inventory、CLI出力、Go例、fixture agent/run_js、HTTPと終了の関連E2Eを確認 |
| S6-D | 独立コードレビューのP2を修正・再レビューし、全体unit/race/E2E/vet、6対象build/compile、実CLI、実プロバイダを確認 |

Coreのpointer設定と変更されるslice/mapをコピーし、Serve/Socketsのaliasを維持します。writerの比較は型だけでなく両値の動的な比較可能性を検査します。比較不能writerの同一性を推測する保証はありません。nativeioはcoreのsyncWriter再wrap前のBindingsを受け取り、ContextWriterを保持します。

rootは作成した出力facadeを所有し、callerのsinkや既存SharedWriterは借用します。子終了でroot出力をCloseしません。rootの出力hookはcore登録前に置き、LIFOの終了順で子資源の後に解放します。callerはruntimeとLoopを所有し、Loop.Closeだけではnativeio全体を閉じません。登録途中の失敗ではruntimeをCloseして捨て、同じ部分構成のruntimeへ再Installしません。

汎用packageとGo例のproduction依存closureは257 packageで、CLI・runtime・agent・認証・provider SDKを含みません。外部向け公開Go packageはまだ提供せず、ステップ7へ残します。IO五moduleは明示opt-inの既存OS権限を持ち、CoreのFS/Fetch制限とは独立です。spawnの既定環境・cwdはProcess由来で、実行するfixture子に既定値と明示optionsが反映されることを確認しました。

## 関連確認とコードレビュー

追加後のfocused unitはnoestdlib・stdlib・nativeioで成功し、新規組立/DNSの重点raceも成功しました。runtime/apidocsの関連138 caseと重点E2E90 caseが成功しています。

独立レビュアー`/root/stdlib_step6_code_review`は会話履歴を継承せず、限定contextとsource・既存ログをread-onlyで確認しました。初回P2は、interface内にslice/mapを持つstruct writerを型のComparableだけで比較するとpanicする問題です。親が両値のreflect.Value.Comparableへ修正し、slice/map・入れ子・片側だけ比較可能の5 caseを追加しました。旧sourceでpanicを再現し、修正後のnoestdlib全体unit/raceが成功しました。変更2ファイルの限定再レビューで未解消P0/P1/P2は0件です。

初回13ファイル・最終14ファイルのmanifestは各レビュー開始/終了時に一致しました。総合確認後も全14ファイルが一致し、その後は開発索引の完了リンク・記録・段階案だけを更新しています。production/test/例のsourceはレビュー後に変更していません。

nativeioの後段module登録失敗だけを人工的に発生させるtest seamは追加していません。deferの全error returnへの適用をsource reviewで確認し、子Installedのsentinel失敗と各終了経路は実行して確認しています。

## 総合確認

Linux amd64・Go 1.27.2・固定go-quickjs v0.27.0。大きな成果物はdisk側、GOTMPDIR/TMPDIRは短い`/var/tmp/n6-mwtprqle`を使用しました。

| 確認 | 結果 |
| --- | --- |
| 全体unit `CGO_ENABLED=0 go test ./... -count=1 -timeout=180s` | 成功、1,147 case pass、42.830秒 |
| 全体race `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1 -timeout=300s` | 成功、1,146 case pass、62.274秒。data race・初回失敗・再試なし |
| 全E2E `CGO_ENABLED=0 go test -tags=e2e ./tests -count=1 -timeout=10m` | 成功、203 case pass・skipなし、36.096秒 |
| vet `CGO_ENABLED=0 go vet -tags=e2e ./...` | 成功、0.510秒 |
| 6対象cross確認 | linux/darwin/windows × amd64/arm64のCLI 6 buildと、noestdlib/stdlib/nativeio/runtime/testsの30 test compileが成功。Linux amd64以外の実機実行ではない |
| API inventory | main/Node Worker/孫/Web Workerの119/119/119/129 globals、各26 module、41 topic/100 detailが変更前と完全一致。export型・alias/constructor同一性も比較 |
| 実CLI/Go例 | 主要CLI 9 caseの出力・終了コードがbaseline一致。Go embedding、HTTP roundtrip/upload-stream/receive-stream例が成功 |
| HTTP受信edge | GET/HEADのwire本文非公開、既知空POSTはnull・未知長空POSTはstream、32 MiB prefixで残り1 byteのEOFを待たず200応答し元Content-Lengthを維持。SIGINTでcontext canceled・終了コード1、listener port再bind成功 |

unitのskip 5件は既存のlive spike 4件とfixture専用TestHelperHeartbeat、raceのskip 1件は同fixtureです。実プロバイダは次の独立した基本フローで確認しました。MCPの既存bundleをNOE_MCP_SDK_BUNDLEへ指定し、fixtureをskipしていません。

API inventory採取の初回だけはWorkerの相対pathをrepository cwdで解決して失敗しました。採取script自身のdirectoryで実行し直し、製品変更なしでbaselineを取得しています。全体unit/race/E2E/vet/crossは最初の実行で成功しました。意図的な旧writer regressionの失敗と、以下の実モデル初回失敗は区別して記録しています。

## 実プロバイダの基本フロー

OpenCode Go / `deepseek-v4.1-flash` / effort `auto`。実行バイナリのcatalogに選択modelがあることを確認しました。同じ基本課題を初回と限定再試の2回だけ実行し、各回の上限は6モデル要求・115秒です。provider/model比較はしていません。合計10モデル要求でした。

課題はinput.jsonの3行から合計を計算し、loopback HTTP serverへfetchして検算、report.jsonへ保存、serverを閉じて英文で完了報告するものです。docsを使う指示は初回・再試とも含めませんでした。

- 初回：16.237秒、6モデル要求・6 tool（exit 0/1/0/1/0/0）、未完了。モデルがNodeのcreateServerを仮定し、未提供APIで失敗。自発的にdocsへ移り、未収録のnode:http/serve detailでのERR_DOC_NOT_FOUNDを経てnoe:httpを発見しましたが、最終回答前にmaxStepsへ到達しました。report未作成で、成功扱いにはしていません。
- 限定再試：課題のHTTP入口をnoe:httpと明示しただけで、実装例やdocs誘導は加えませんでした。11.976秒、4モデル要求・3 toolがすべてexit 0、task completed。モデルは自発的にdocsを2 tool・3 lookup参照しました。
- 生成されたJSを監査し、inputを実読取り、serveのResponseをfetchして検証し、`{"rowCount":3,"total":31}`を保存、finallyでserver.closeしたことを確認しました。英文の最終回答があり、独立したTCP接続確認とbindでlistener終了・port再利用も確認しました。

この結果はNOEへの組込み経路が実プロバイダから使える証拠です。初回にはAPI選択の誤りがあり、入口を明示しない任意課題の成功を保証するものではありません。今回、API docsやモデル向けinstructionsの変更で結果を合わせてはいません。

## 成果物と残る範囲

rawログ・採取script・binary・cross成果物・初回と再試のevents/tool codeは`/home/agent/.cache/noe-stdlib-step6-p38edv26/`へ保存しました。永続的な要約とmanifestは[構造化結果](stdlib-step6-composition-results.json)に収録しています。認証値は記録に含めません。利用者のagent-server-build.jsonl・root配布binary・_refs/は変更していません。

ステップ6は完了です。次のステップ7では公開package/import path・安定した設定・外部Goアプリの利用例を決めます。agent拡張の外部接続、API改名/統合、engine/HTTPの追加機能は今回の完了条件へ含めていません。
