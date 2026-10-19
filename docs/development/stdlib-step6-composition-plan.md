# NOE版stdlib：ステップ6・API導入と所有関係の整理計画

2026-10-19。[段階案](../future-candidates.md)のステップ6。基点はv1.12.0公開確認後の`4ee824d20b78279979176410ebfe79bcc2480277`です。計画と独立した通常レビュー後の実装依頼に基づき、S6-A〜Dの実装・関連テスト・コードレビューと限定再レビュー・総合確認・実プロバイダの基本フローまで完了しました。[実装・検証記録](stdlib-step6-composition-implementation.md)・[構造化結果](stdlib-step6-composition-results.json)・[レビュー記録](stdlib-step6-composition-review.md)を参照してください。以下は実装前の計画と受入条件です。commit・releaseは行っていません。

## 目的と範囲

CLI・agentに依存せず、ホストがgo-quickjs runtimeへNOEの汎用APIを選択して登録できる内部の組立境界を作ります。NOE自身も同じ境界を使い、mainとWorkerの登録内容、共有出力、各runtimeの資源所有を一か所で組み立てます。JSから見た既存API・権限・終了動作を保ちます。

今回の到達点はrepository内の小さなGoホストでの利用です。外部向けGo package・import path・安定した設定API・別module化はステップ7です。agent/auth/decision/imageの移動、loaderの一般化、API改名、`noe:fs-handle`統合、nativeioの細かな権限モデル、engine更新、新しいHTTP機能は含めません。

## 確認した出発点

- `internal/runtime/runtime.go`がSharedWriterを作り、stdlib、docs、nativeio、agent、authを順番に登録しています。`runtime/stdlib.go`のWorkers.Installedがdocs/nativeioの子側登録を別途持ちます。
- `internal/stdlib`はCLI・agentに依存しません。`nativeio`もstdlibをproduction importせず、公開engine APIで非同期処理します。ただしnativeioの同packageテストはstdlibをimportするため、stdlib→nativeioという依存追加はtest import cycleになります。
- `runtime/docs.go`はengine上の引数検査・例外変換と、純粋な`apidocs.Lookup`のadapterです。apidocsへengine依存を加える必要はありません。
- stdlib.Installは設定ポインタのLoop・Process出力・WebSockets.Serve等を補完します。Workerは設定を複製して専用Loopへbindしますが、現在の`Config.bindLoop`はDNSを複製・再bindしていません。DNSが親Loopへ通知し得る経路を局所修正します。
- stdlibのsyncWriterはContextWriterを公開しません。nativeioへはcoreによる再wrap前のSharedWriterを渡す必要があります。現行runtimeの共有境界をなくして各moduleにwriterを独立作成すると、出力の並行安全性と取消しが崩れます。
- 固定engine v0.27.0のRuntime.CloseはVM停止・host job停止→runtime context取消し→OnClose逆順実行です。nativeioはこのcontext/hook、stdlib資源は主にLoop.Contextを使います。両者の実行機構の統合は不要です。

根拠は上記ファイル、`stdlib/worker.go`・`dns.go`・`websocket.go`、`nativeio/host.go`、`hostio/hostio.go`、固定engineのClose/AsyncWorkです。外部最新仕様や上流変更を前提にしません。

## 内部構成と導入契約

新しい`internal/noestdlib`を組立packageとし、依存方向を次に揃えます。

```text
internal/runtime → internal/noestdlib → internal/stdlib
                                    → internal/nativeio → internal/hostio
                                    → internal/hostio
                                    → internal/apidocs
```

noestdlibとそのproduction依存closureからCLI・runtime・agent・認証・provider SDKを参照しません。stdlib/nativeioは既存実装を保持し、相互依存を増やしません。apidocsは純粋なcatalogのまま、JS adapterだけをruntimeからnoestdlibへ移します。

概念上の設定は`Core stdlib.Config`、`IO *IOConfig{Stdin io.Reader, Cwd string}`、`Docs bool`です。Installはcoreを一度登録し、選択された追加APIを登録して、正規化済みStdout/StderrのBindingsを返します。型名・返却形は内部の実装詳細で、公開Go契約として固定しません。

| 選択 | 契約 |
| --- | --- |
| Core | 既存stdlib.Configを保持。FS/Fetch/DNS/Run/Serve/Sockets/Process/Workers等のnilは従来どおり未導入。pureなWeb型やnode moduleの既存既定値も維持 |
| IO=nil | nativeioの五moduleを追加しない |
| IOあり | `noe:io`・`noe:process`・`noe:fs-handle`・`noe:sockets`・`noe:signals`を一組で追加。個別権限化は今回しない |
| IOあり＋Core.Process=nil | spawnがglobal process.env/platform/cwdを参照するため、登録開始前にerror。Processや実ホストの環境変数を暗黙導入しない。IOなしならProcess=nilを維持できる |
| Docs=false/true | `noe:docs`を未導入/導入。IOとは独立。引数・戻り値・ERR_DOC_NOT_FOUNDを維持 |
| IOあり＋NoWebAPIs | Streamsが必要なため登録開始前にerror。Web APIを暗黙に有効化しない。DocsだけならNoWebAPIsと併用可能 |
| Workersあり | 従来どおりLoop必須・Web APIs必須。New/Load/Max/Installedを保持。Workersなしで追加APIを使うためにLoopを新たに必須にはしない |

Core.FS等の制限は、別途明示的に有効にしたIOへは波及しません。IOは現在のNOE相当のOS権限を持つグループです。IOグループはCore.ProcessとWeb APIsを前提とし、spawnの既定Env/Cwdは現在どおりJSのprocessから取得します。IO.CwdとCore.Process.Cwdを組立層が自動で一致させる契約にはしません。DocsのcatalogはNOE全体の参照情報であり、部分導入したホストの利用可能API一覧ではありません。これらを英文の組込み例の説明へ明記します。Process非依存のspawnへ作り替えることは今回の対象外です。

### 設定の複製と再利用

組立Installがcallerの設定を変更しないよう、stdlibへ渡す前に変更されるpointer設定を複製します。FS・Process・OS・Fetch・DNS・Run・Serve・WebSockets・Workersを対象とし、Process.Args/Env・Run.Env・DNS.Serversのslice/mapも複製します。callbacks、reader/writer、Resolver、HTTP client等の借用資源をdeep-copyしたり、callerの並行変更を安全にしたりする契約ではありません。

WebSockets.ServeがCore.Serveと同じなら複製後も同じ複製先を参照し、異なる明示設定ならその別設定も複製します。Socketsが設定するupgrade状態をcallerのServeへ書き戻しません。明示的なroot capability Loopは既存の意味を保ち、nilだけをCore.Loopで補完します。子側では各capabilityを子Loopへbindし、DNSも他の非同期capabilityと同じ所有者へ通知させます。

同じ未変更設定から二つの新規runtimeへInstallしても、Loop・設定の既定値補完・Workers.Installed wrapperが混ざらないことを確認します。stdlib.Install単体の既存利用方法は保持し、この新しい複製契約は組立Installに適用します。

### mainとWorkerの登録

1. 全体設定の前提を検査し、設定を複製して出力を正規化する。所有する出力wrapperの終了hookをcore登録より先に置く。
2. Workers設定があればそのコピーのInstalledを包む。追加APIの共通関数を呼んだ後、元のInstalledを一度呼ぶ。New/Load/Maxを置き換えない。
3. rootへstdlib.Installを一度呼び、同じ共通関数でrootのdocs/nativeioを追加する。rootでは元のWorkers.Installedを呼ばない。
4. 子は既存worker.goによる専用runtime/Loop・stdlib.Installの後、Installedから追加APIを導入する。孫も同じ設定を継承し、module二重登録やwrapperの再重複を起こさない。Installedからstdlib.Installや組立Installを再帰呼出ししない。
5. 子のnativeio stdinは従来どおりnil。rootのIO.Stdinを継承しない。出力はrootの正規化済みBindingsを借用し、IO.Cwdは明示設定を継承する。

CLIのcwd正規化、環境取得、Process.Exit/Unhandled、module loader/import map、Workerのソース読込みとagent系module拒否、timeout・SIGINT・root errorはruntimeに残します。組立層は任意ファイルのloaderやagent機能を暗黙追加しません。

## 出力と資源の所有関係

| 対象 | 所有者と終了 |
| --- | --- |
| callerのruntime・Loop | caller所有。Installは借用し、成功/失敗で勝手にCloseしない。callerがowner上で実行・loop進行・終了する |
| callerのstdin/stdout/stderr・設定内のclient等 | 借用。元のfile/sink等を組立層からCloseしない |
| 正規化した共有writer | root Installが所有するfacade。root終了でClose。子は借用し、子終了で閉じない |
| nativeio Hostと作成したfile/socket/process/signal等 | 各runtimeごとに所有。runtime終了でGo側取消し・回収。JS Promise通知の実行を待たない |
| coreのHTTP/WebSocket/Worker等 | 従来どおり対応Loop/runtimeの寿命に従う。登録整理だけを理由に別のtransport/終了機構へ移さない |

Stdout/Stderrと明示的なProcess.Stdout/Stderrを同じ正規化処理へ通します。nil Process出力は対応するcore出力へ既定化し、別途指定した出力先は保持します。比較可能な同一sinkは一つの共有wrapper/lockを参照します。比較不能なio.Writerをmap keyやinterface比較へ使ってpanicさせません。比較不能な値の同一性を推測して統合する保証はせず、共有が必要ならcallerが同じpointer sinkを渡せます。

既存のSharedWriterをcallerが渡した場合も、それを借用するroot所有のfacadeを作り、元のSharedWriterをCloseしません。nativeio、agent/auth、console/processと子runtimeはこのrootの共有境界を通ります。coreが付けるsyncWriterをnativeioへ渡さず、元のContextWriter機能を保ちます。子のendpointが持つlocal adapterをCloseしてもrootの出力は引き続き使えます。

所有出力のhookはcore登録より前とし、LIFOのOnCloseでWorker終了hookより後に出力を閉じます。nativeioのhookはさらに後から登録され、先に各Hostを止めます。runtime既存のLoop hookの位置が変わっても、runtime context取消し、子の回収、出力閉鎖が待ち合わないことを確認します。callerがLoop.Closeだけを行った場合にruntimeやnativeioまでCloseする新契約は作りません。終了時はruntime.Closeも必要です。

従来の任意io.Writerの同期Writeが永久にblockする場合まで、新たな強制取消しを保証しません。隠れたgoroutineで逃がしません。nativeioで既に扱えるContextWriter/OS pipe等について、取消しと共有出力の継続利用を維持します。

### 導入失敗

不正な組合せはmodule登録・資源作成より前に拒否します。それ以降の失敗では組立層が作ったwriter facade等を解放し、nativeio.Installは作成済みHostをすべてのerror returnでCloseする局所的なunwindへ揃えます。caller所有のruntime/Loop/元sinkは閉じません。

module登録のtransactionやrollbackは提供しません。途中失敗したruntimeは再利用せず、callerがCloseして捨てます。登録済みGo-only hookは冪等なCloseで扱います。子の追加登録/元Installed callbackが失敗したときは既存Workerのdefer終了経路で子を回収し、root出力を閉じません。rootの後続agent/auth登録失敗はruntimeの既存Close経路で全体を回収します。

## 実装スライスと受入条件

### S6-A：組立packageと設定・出力境界

- `internal/noestdlib`へConfig/Install/Bindings、設定複製、出力正規化、docs adapterを実装する。nativeio実装は元のpackageを使う。
- 部分導入表の各組合せ、引数例外、同じ設定の二runtime再利用、明示設定/aliasの保持、caller設定の不変を確認する。IO＋Process=nil/NoWebAPIsはmoduleが未登録のまま事前拒否し、IOなし・Process=nilでDocsを使えることも確認する。
- 有効なProcessを明示したIO構成ではfixture子processを実spawnし、Process.Env/Cwdの既定値とoptions.env/cwdの明示指定が従来どおり反映されることを確認する。登録/importの成功だけでspawn利用可能と判定しない。
- 同一/異なる/nil/比較不能writer、明示Process出力、ContextWriterを確認。元sinkは閉じず、所有facadeだけが閉じることを確認する。
- IOなしの登録/importだけでread・process起動・listener・signal購読・keepaliveが発生しない。IOありでも登録だけで実IOを始めない。
- production依存closureをgo listで検査し、CLI/agent/provider依存なしを確認する。

完了条件：runtime/CLIをimportしない内部の小さなGoホストから選択したAPIを導入でき、設定・出力の契約が単体確認できる。

### S6-B：Worker・失敗・終了の所有関係

- 共通追加APIをWorkers.Installedへ接続し、元callback・New/Load・Maxの意味を保つ。main/子/孫それぞれ登録一回、元callbackは各子一回を確認する。
- DNSをbindLoopで複製し子Loopへ結び直す。fake Resolverとowner上の通知で親Loopへの依存なし・親設定不変を確認する。実DNS接続は使わない。
- nativeio.Installのerror unwindを揃える。子callbackのsentinel error等、既存engineで再現できる失敗経路を使う。必要な場合もpackage-privateな試験seamに限り、公開fault injectionやengine改造を加えない。
- 子stdinなし、子終了後のroot/兄弟stdout継続、同じsinkへのcore/nativeio/Worker並行出力、pending read/write/socket/process/signalのruntime終了回収を確認する。
- root Close・Worker terminate・loop終了後の明示runtime Close・導入失敗をbarrier/done/port再bind・子process終了等で観測する。総goroutine数や固定sleepだけで資源回収を判定しない。
- S4/S5のHTTP終了テストを回帰実行し、Worker内の送受信stream停止、graceful→force、Expect、hijackの所有を維持する。HTTP実装の再設計はしない。

完了条件：子の失敗/終了がroot資源を閉じず、親終了で子資源と待機を回収できる。DNSを含む通知は各ownerに戻る。

### S6-C：NOE切替と内部ホスト例・英文説明

- runtime.newHostの共有出力/stdlib/docs/nativeio登録を組立Installへ置換する。返却Bindingsをagent/auth登録にも渡す。root/Workerでの直接追加登録を撤去する。
- runtime.stdlibConfigはCLI固有の権限・callbacks・loader設定を作る責務を保持する。必要な既存内部テスト/helperも同じ境界へ揃える。
- `examples/embedding/`にrepository内で動く小さなGo例と英文READMEを追加する。agent/provider認証不要で純粋なdocs参照とnative IOの小さな処理を行い、明示的なProcess設定・Loop進行・runtime.Close・借用出力を示す。internal importのため外部アプリからはまだ利用できないこと、部分導入/IO権限とProcess・Web APIsの前提/Docs catalogの意味を明記する。
- 現行ガイドの構成説明とpackage commentsを英文のAPI説明に整合させる。JS API/docs本文やcatalog IDは変更しない。
- 同じ採取器で変更前後のmain/Worker/fixture agent/run_jsのAPI inventoryを比較する。Step5時点の119/119/119/129 entries、stdlib 26 module、docs 41 topic/100 memberを参照値にし、各export値・alias同一性も維持する。実測baselineが異なる場合は理由を確認し、数字だけを合わせない。
- `noe run`/eval、import map、Worker、agent/hooks/auth、MCP既存fixture、終了/例外経路を関連E2Eで確認する。CLIのflags・version・出力を変えない。

完了条件：NOEが同じ汎用組立境界を利用し、4modeのJS公開契約とCLI/agent固有動作を保つ。内部例だけでCLI非依存を示せる。

### S6-D：コードレビューと総合確認

- 独立した通常コードレビューへ差分・要件・試験結果を渡し、P2以上を修正して限定再レビューする。JS owner、設定/出力/子資源所有、依存方向、公開契約の維持を中心にする。
- その後、全体unit・race・E2E・vet、6配布対象のCLI buildと影響test package cross-compile、Goホスト例の実行、実CLIの送受信streaming/終了を確認する。
- 実プロバイダは基本フロー1課題だけ。直近と同じOpenCode Go/deepseek-v4.1-flashのautoを候補に、実行時の利用可否を確認する。最大6モデル要求・115秒、docsを使う指示なしで、ローカルの短い処理→結果保存→最終回答を確認し、docsの自発参照は観察結果として記録する。複雑な総合課題や反復的なモデル比較はしない。費用を伴う実行は実装/総合テストの許可後に行う。
- 失敗・再試・未確認を含む実装記録/構造化結果を保存。段階案は6完了・7未着手へ更新する。commit/releaseは別の明示依頼で行う。

完了条件：未解消P0/P1/P2なし、選択導入・NOE利用・終了回収が実環境で成功し、外部Goライブラリ化へ進める内部境界がある。

## 検証手順と環境

以下は実装時の予定で、今回は実行しません。各スライスの関連テスト後、コードレビュー修正を取り込んで総合確認します。新しい変更/失敗がなければ通過済みの確認を無目的に繰り返しません。

```sh
CGO_ENABLED=0 go test ./internal/noestdlib ./internal/stdlib/... ./internal/nativeio ./internal/hostio ./internal/runtime ./internal/apidocs -count=1 -timeout=180s
CGO_ENABLED=0 go test -tags=e2e ./tests -run 'Test.*(HTTP|Stdlib|Docs|Worker|Process|Signal|Socket|Timeout|Interrupt)' -count=1 -timeout=5m
CGO_ENABLED=0 go test ./... -count=1 -timeout=180s
CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1 -timeout=300s
CGO_ENABLED=0 go test -tags=e2e ./tests -count=1 -timeout=10m
CGO_ENABLED=0 go vet -tags=e2e ./...
```

6対象はlinux/darwin/windows × amd64/arm64。CGO不要のCLIと変更したtest packageをcross-compileし、実機確認と区別して保存します。Go例もbuild/runし、go listのproduction依存closureを検査します。

大きいbuild成果物/cacheはdisk側を使い、GOTMPDIRは`/var/tmp`直下の短いpathにします（Go 1.27のtesting.TempDirとUnix socket path長の制約）。MCP fixtureは既存bundleをNOE_MCP_SDK_BUNDLEで指定できます。rootの配布binaryと利用者の`agent-server-build.jsonl`は変更・読取り対象にしません。

v1.12 releaseのrace初回にはnativeioの即時Busy確認が一度失敗し、同じsourceの重点20回/全体再試は成功しています。今回の所有関係変更に関連するか失敗ログとdoneの順序で判断し、成功するまで再実行しただけで済ませず、即時Busy検査を無条件に緩めません。engine AsyncWorkのPost/Done順序変更は対象外です。

## 計画レビューと判断境界

初回は新しいreviewerへ会話全体を渡さず、本計画・段階案・必要なsourceを渡して独立した通常レビューを依頼します。30分上限、10分進展がなければ確認済み範囲と暫定findingを返します。観点は段階6への適合、実現可能性、partial configとWorkerの互換性、借用/所有と失敗回収、十分な受入観測です。re-reviewは変更箇所と既存findingに限定して各15分。利用者の指定に従い、P2以上がなくなるまで修正と再レビューを反復します。

新しい公開API・大きなloop/transport書換え・agent実行方式変更が必要になった場合は、局所実装に紛れ込ませず、根拠・影響・代案を整理して利用者へ戻します。内部の型名やファイル分割など可逆的な選択は、上記の契約を保って実装時に判断できます。
