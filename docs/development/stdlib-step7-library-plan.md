# NOE版stdlib：ステップ7・外部Goライブラリ提供計画

実装完了：利用者の追加依頼により7.1の実装・テスト・コードレビュー・総合テストを完了。[実装記録](stdlib-step7-library-implementation.md)を現在の状態の正本とし、以下の計画時点の記述は履歴として残す。commit/push/releaseは今回の対象外。

2026-10-19。[段階案](../future-candidates.md)のステップ7。基点はステップ6のcommit `741a1e1e4302ffdc709d506af01335d955ea096c`です。今回は計画と独立した通常レビューだけを行います。実装・テスト実行・実プロバイダ呼出し・commit・push・releaseは行いません。[レビュー記録](stdlib-step7-library-review.md)へ結果を保存します。

利用者の追加方針（2026-10-19）：先にv1.13.0を常用し、小さなバグを潰して問題なさそうと判断できた時点でv2.0.0にする。今回のv1.13.0リリースでは本計画を実装せず、既存moduleと内部package構成を維持する。以下は公開Goライブラリ化へ進む際の技術計画であり、計画レビュー完了だけでv2公開や常用上の安定性確認が完了したとは扱わない。

## 到達点と段階の分割

repository外の小さなGoアプリが、公開packageだけをimportし、go-quickjs runtimeへNOE管理の汎用APIを選択して導入できるようにします。NOE自身も同じ公開入口を使います。JSのAPI名・動作、ステップ6の権限・設定複製・出力とWorkerの所有関係を保ちます。

合意済みの「汎用stdlibから始め、agent拡張を段階的に追加」を具体化し、ステップ7を次に分けます。

| 単位 | 目標と完了判定 |
| --- | --- |
| **7.1：汎用stdlib公開** | この文書でS7-A〜Eの実装可能な計画を定める。外部moduleから公開APIを構築・実行でき、NOE本体も利用し、取得用module artifactと公開手順が整う |
| **7.2：agent拡張の外部接続** | 7.1成立後、別の実装計画と独立レビューを作る。Goホストの実行方法・認証・broker・run_js/子NOE・観察・取消し/終了を接続し、外部アプリで動かす。今回のS7-A〜Eには含めない |

7.1だけの実装でagentも外部利用可能になった、段階7全体が完了した、v2.0.0を公開したとは扱いません。段階案では「7.1完了・7.2未着手」のように区別します。初めからagentの内部型を丸ごと公開せず、汎用stdlibを使える状態を先に作る方針です。

別repository・別Go moduleへの分離、engine/HTTPの再実装、Node互換性の拡大、JS API改名・fs統合、module loader/import mapの公開、汎用Go Eval/Execute runner、新しいsandbox/権限モデルは今回の対象外です。

## 調査した前提

- `go.mod`は現在`module noe`。外部取得の正規名へ変更する必要があります。root module内のGo import、テスト、現行build設定にもこのprefixがあります。
- `.forgejo/release-profile.yml`・生成workflow・現行release手順の`-X noe/internal/cli.Version=...`も変更対象です。linkerの対象間違いをbuild成功だけで見逃さず、生成CLIのversionを実行確認します。
- `internal/noestdlib.Config.Core`は`internal/stdlib.Config`、Bindingsは`*internal/hostio.SharedWriter`を参照します。このまま組立関数だけを公開しても、利用者が設定型を名前で構築しづらく、内部出力型を公開契約へ固定してしまいます。
- stdlib Configの型にはLoop・FS・Process・OSInfo・Fetch・DNS/Resolver・Run・Serve・WebSockets・Workersがあります。Worker callbackはengine Runtimeを受け取り、Loopの公開methodも外部ホストの実行/通知に必要です。
- 汎用層と現在のGo例のproduction依存closureにはCLI・runtime・agent・認証・provider SDKがありません。ただしroot go.modのrequireにはagent SDKが残ります。packageのbuild依存とmodule graph上のrequireを混同せず、今回moduleを細分化しません。
- rootの`go test ./...`はnested moduleを含みません。`spikes/agent-loop`は独立module、`spikes/repl-lineedit`は`require noe`/local replaceとinternal/runtime importを持つ過去のREPL調査です。歴史的スパイクを復活させるためにREPLを戻すことはしません。
- 2026-10-19のread-only HTTP確認では、[repository](https://forge.harakara.site/littleisland/noe?go-get=1)と[/v2入口](https://forge.harakara.site/littleisland/noe/v2?go-get=1)はいずれもHTTP 200で、`go-import`はrepository prefix `forge.harakara.site/littleisland/noe`、VCS `git`、clone URL `https://forge.harakara.site/littleisland/noe.git`を返しました。現時点でv2 module/tagの取得成功を検証したものではありません。

## moduleと公開package

合意したv2の節目に合わせ、同じrepositoryのroot moduleを次へ変更します。

```text
module: forge.harakara.site/littleisland/noe/v2
public package: forge.harakara.site/littleisland/noe/v2/stdlib
package name: stdlib
```

root directoryを維持し、別の物理`v2/`directoryは作りません。Goのv2以降のmodule pathとtagの対応は[公式module reference](https://go.dev/ref/mod#major-version-suffixes)に従います。公開Go moduleの初版はv2系列を対象とします。今回または実装依頼だけでtag・release・サーバー設定変更は行わず、公開時期と具体的versionは利用者のrelease依頼で確定します。既存v1タグ・バイナリは書き換えません。

module移行後、旧`noe`と新pathを二重にrequire/replaceして同じ実装の型を二組作る互換層は設けません。NOEのGo APIはこれまでinternalで、外部向け旧import pathの互換提供はありません。CLIの名前・flags・JS import名は従来どおりです。default CLI versionはrelease作業まで変更しませんが、公開取得用tagは/v2と整合する必要があります。

### module prefixを変更する範囲

- root go.modのmodule行と、root moduleに属するproduction/test/例/現役spikeのGo import。依存version・engine・SDK更新は伴わせない。
- root moduleを参照する現行build/test helper・go listの期待値・version linker flag。release profileと生成workflowを同時に更新し、生成物の整合を既存テストで確認する。
- 現行の開発/公開手順と新しいGo利用例。過去の実装/検証記録・公開済みrelease記録・旧command例は当時の事実として残し、全Markdown/JSONの文字列置換はしない。release-checkpointsの現在の手順だけを更新し、過去の表は維持する。
- 独立した`spikes/agent-loop`はrootをimportしないためそのmodule名/versionを変えない。廃止済みREPL spikeも履歴として残し、root module移行後の現役利用例/検証対象として案内しない。必要ならそのREADMEへ歴史的環境の説明を足し、旧go.mod/依存やREPL実装を復旧しない。

移行後のroot配下の現役Go source/現行build commandから旧prefix参照がなく、除外したnested module・履歴だけに残ることを分類して確認します。古い開発結果のpackage名を一律書き換えて過去の証拠を変えません。

## 公開APIの方針

新しいrootの`stdlib/`は薄い公開窓口です。実装は`internal/noestdlib`・`internal/stdlib`・`internal/nativeio`等を保持します。forkした全実装や個別installerをまとめてpublicへ移動しません。

| 公開要素 | 初版の形と意味 |
| --- | --- |
| `Config` | 公開struct。`Core CoreConfig`、`IO *IOConfig`、`Docs bool`。既定でOS権限を増やさない |
| `CoreConfig` | coreのConfigのtype alias。公開名で設定literalを作り、既存の全設定を保持 |
| `Loop` | core Loopのalias。`NewLoop(rt)`で作成。既存method setも公開契約に含む。zero Loopは使わずconstructorを案内 |
| `FS`, `Process`, `OSInfo`, `Fetch`, `DNS`, `Resolver`, `Run`, `Serve`, `WebSockets`, `Workers` | core設定型/Resolverの公開alias。callbackや標準Go/engine型は同一性を保つ。個別のFiles/Network/Servers等は初版では公開しない |
| `IOConfig` | 公開の設定struct。`Stdin io.Reader`、`Cwd string`。内部設定へ変換し、mutable pointerをpublicから直接露出する必要はない |
| `ContextReader`, `ContextWriter` | hostioと同じmethod setの公開interface。借用IOで文脈取消しへ協調する契約を英文説明する |
| `Output` | `io.Writer`と`ContextWriter`を満たす公開interface。Closeを要求しない。利用者はruntimeに所有される出力を借用する |
| `Bindings` | 公開struct。`Stdout, Stderr Output`。内部SharedWriterの具体型をpublic signatureへ出さず、内部Bindingsから変換する |
| `Install(rt, cfg) (Bindings, error)` | 内部の組立Installを一度呼ぶ。登録・検査・複製・Worker wrapper・回収を複製実装しない |
| `ExitCode(rt) (int, bool)` | process.exitCodeの既存値を読むcore helperへの薄い窓口。Goホストの終了判断に利用できる |

これは実装前の初版APIリストです。新しい機能を追加しない範囲の命名・ファイル分割は実装時に判断できますが、公開要素を増やす・権限や所有を変える場合は理由を記録し、範囲拡大なら利用者へ戻します。

### aliasと型の閉じ方

aliasesの参照先は内部実装でも、利用者が設定を構築しfield/callback/return値を使う際に必要な型は、上表・Go標準package・公開go-quickjs packageから名前で参照できるようにします。内部packageを利用者にimportさせません。特にCoreConfigの各pointer型、Workers.New/Installedのengine Runtime、Loop、DNS.Resolverを外部fixtureで構築してcompileします。

公開aliasは内部実装の変更を無条件に隠すものではありません。exported fields/methods、型の同一性、callbackの契約を互換性対象として扱います。内部を動かした結果、公開型の同一性やmethod setが変わる変更は互換変更と決めつけません。公開symbol/field/methodの一覧と英文Go docsをレビューし、利用者の実コードをcompileする契約試験を維持します。全GoDoc出力の丸ごとgolden化やinstallerの単純転記テストは作りません。

Bindingsのinterfaceが隠すのは内部出力の具体型です。Closeやhostio固有errorのidentity・型assertionを公開契約に含めません。callback・IO・Resolver・client等は借用のままで、alias導入を理由にdeep-copyや強制Closeを追加しません。

### 公開する実行・所有の契約

- callerがruntimeとLoopを作り、同じowner goroutineでJS評価・Loop進行・JS値の操作・runtime.Closeを行う。Post/Begin/Done/Close等の既存Go側の並行操作は、その既存契約を英文で明記する。Go callbackがすべてparentのJS ownerで動くとは説明しない。Worker callbacksは子owner、HTTP等のIO側callbackは対応するGo側の実行場所を扱う。
- Loop.Runはkeepalive資源があれば継続する。PromiseのAwaitとLoop進行は同じ役割ではない。例では有限処理の資源を閉じ、bounded contextでLoopを進め、その結果を確認する。CPU評価にも同じcontextを渡す。無期限serverを作ってからLoop.Runが自然終了すると説明しない。
- runtimeとLoopはcaller所有。runtime.Closeを省略しない。Loop.Closeだけでnativeio資源まで回収する新契約は作らない。root所有出力のLIFO順と子の借用、callerの元sinkが開いたままであることを維持する。
- IOは五moduleの明示opt-inで、ProcessとWeb APIsが必要。Core.FS/Fetch制限は別途有効化したIOを制限しない。stdinは子へ継承しない。Docsは全NOE catalogであり、有効化済みcapabilityの検査ではない。
- 設定はcaller側を変更せず再利用できるが、借用resource/callbackの並行変更安全性を付け加えない。導入はtransactionではなく、途中失敗したruntimeはCloseして捨てる。
- engineは引き続きgo-quickjs v0.27.0で確認し、engine Runtimeを公開signatureで受ける。GoのMVSで利用側が高いengine versionを選ぶ場合まで互換性を主張しない。consumerの実際の選択versionを記録し、強制replaceを利用者へ要求しない。

## 外部consumerによる受入

repository内部のexampleが動くだけでは完了としません。`example.com/noe-stdlib-consumer`などNOE prefix外のmoduleを、checkout外の一時directoryで作ります。GOWORK=offと明示的な一時go.modを使い、利用者のworkspace設定に依存させません。

### 1. local replaceで開発中sourceを利用

consumerのrequireは正規の/v2 module、replace先だけをcheckoutの絶対pathにします。consumer Go sourceは公開stdlibとgo-quickjs・Go標準packageだけをimportし、`noe`旧module・internal importを使いません。検査するのはconsumer自身のimportであり、公開packageが正当に参照する内部実装まで禁止しません。

- Docs-only/NoWebAPIsを含む部分構成で、未導入moduleがimportできず、stdin readやkeepaliveを増やさない。
- 公開設定型を名前で構築し、Loop・全capability設定・Resolver・Worker callbacksの型を渡せること。root/子/孫・元Installed一回・子stdinなし・子終了後の出力継続を公開入口で確認する。
- FSの短いファイル操作、native IO、fixture子processのEnv/Cwd、noe:httpのloopback往復を少数の実用フローへまとめる。必要なcapabilityを明示して、既存内側単体テストを外へ全部複製しない。
- 有限処理・例外・timeout/context取消しとruntime Close、pending IO/子の終了、callerのsinkが閉じないことを確認する。出力のalias/ContextWriter・動的比較不能writerの重点回帰は既存S6試験を保持し、公開BindingsのWrite/WriteContextも確認する。
- engineをcaller側で作り、CLI binary・provider認証・agent機能なしで全フローが成立する。未導入のagent moduleをimportできると案内しない。

fixtureはroot配下のtestdataにsource/templateを保存し、試験時に外部directoryへcopyします。nested go.modをrootの./...が自動で検証すると仮定せず、rootの統合test/helperからconsumerのGo commandを明示実行します。後述のCIにはlocal consumerのportableな部分を組み込みます。

### 2. replaceなしのmodule artifactを利用

local replaceだけではmodule path/major suffixと配布sourceの欠落を確認できないため、同じsourceからlocal file GOPROXY用の一時module zip・.mod・.infoを作り、syntheticなv2 pre-releaseとして取得します。[公式proxy protocol](https://go.dev/ref/mod#module-proxy)に従い、version・zip prefix・go.modの/v2を整合させます。

consumerはreplaceを持たず、隔離GOMODCACHEへlocal proxyから取得したartifactでbuild/runします。proxy/キャッシュ/checksum用envは子commandだけに設定し、go env -wや利用者の設定を書き換えません。synthetic moduleだけをGONOSUMDB対象にし、一般の依存のchecksum検証を一律無効化しません。依存取得には既存download cache由来のfile proxyや通常のproxyを利用できます。実サーバーにfake tagを作りません。

zipにはroot moduleの配布sourceを入れます。tracked fileと今回導入する既知の新規sourceをmanifestで列挙し、nested module、_refs、root binary、利用者JSONL・認証・一時成果物を含めません。Goのzip読込みで受理されることを確認し、archive生成のために新しいGo依存を加えません。checkoutの絶対pathやreplaceがartifact内のconsumerへ必要な状態なら失敗です。

この試験はlocal artifactの利用確認であり、公開tag・実サーバー・module proxyでの取得成功とは区別します。releaseが別途許可された時点で、正規v2 tagを使うclean consumerのgo get/build/runとversion確認を公開後チェックへ追加します。公開tagがない今、既存v1 tagを新module pathで取得できるとは説明しません。

## 実装スライス（7.1）

### S7-A：module identityと配布設定

- canonical /v2へroot module/importを移し、依存versionを維持。現役source/commandとnested module・履歴を切り分けて旧prefix残存を確認する。
- profile・生成workflow・現行release手順のlinker pathを同期し、通常CLI buildと`-X .../internal/cli.Version=stdlib-smoke`のnative実行で表示値を確認する。default versionや既存tagは変えない。
- root module全体がbuild/testできる中間状態を作る。このスライスでは公開APIを使えるとまだ案内しない。

完了条件：新しいmodule identityで既存機能をbuildでき、release生成設定とversion注入が正しい。

### S7-B：公開stdlib facade

- 上表の公開型/関数と英文package docsを追加。内部組立へ委譲し、Bindingsを公開interfaceへ変換する。
- aliasの型closureを点検し、設定・所有・partial opt-inを維持。SDK/CLI/runtimeをproduction importしない。
- 内部S6単体/重点raceと新facadeの外部fixture compileを行い、公開契約に必要な型をinternal importなしで扱えることを確認する。

完了条件：公開入口の型と振る舞いが閉じ、利用側が内部型を直接importする必要がない。

### S7-C：外部consumerとartifact確認

- local replace版とreplaceなしlocal proxy版を実装し、前節の実用フロー・終了・配布内容を確認する。
- 少数のconsumerをrootのtest/helperから明示起動し、少なくともpure/部分構成のportable smokeを通常CIの既存unit gateへ接続。Go child buildの待機に十分な期限を設け、同じroot全体テストをconsumerから再帰実行しない。
- 公開packageのproduction依存closureとconsumer自身のimport、実選択engine versionを記録する。module graphにSDK requireがあることとpackageへの混入を分ける。

完了条件：checkout外の別moduleが、replaceの有無それぞれで正規公開pathを利用し、CLIなしで実行・回収できる。

### S7-D：NOE利用先と英文ガイド

- runtimeの組立Install・設定型・Loop constructorを公開stdlibへ揃える。内部実装の個別テストまでpublic facadeへ強制移動しない。
- `examples/embedding`を公開importだけのGo例へ変更し、外部consumer用のgo.mod templateと実行手順を付ける。英文のGo embedding guide/GoDocにpartial config、runtime/Loop/出力/子の所有、callback実行場所、導入失敗、engine依存を説明する。
- README/開発indexへ公開Go利用の入口を追加。公開前のlocal手順と公開後tag用commandを区別し、未公開versionへの取得成功を記載しない。
- JS API本文/IDsは原則変更しない。Go公開入口・module pathの出自説明が必要な箇所だけ英文で更新し、catalog 41 topic/100 detail・stdlib 26 moduleと全JS exportを保つ。

完了条件：NOE自身もpublic入口を使い、利用者が英文ガイドと公開型だけで小さなGoホストを作れる。

### S7-E：独立コードレビューと総合確認

- 関連テスト後に独立した通常コードレビュー、P2以上を修正して限定再レビュー。その後、全体unit/race/E2E/vet、6対象CLI build/影響test compile、外部consumerのnative実行と6対象compileを行う。
- main/子/孫/Web WorkerのJS inventoryを同じ採取器で比較し、119/119/119/129 globals・26 module・41 topic/100 detailをbaselineと照合。NOEのagent/run_js、auth、HTTP/Streams、Worker、MCP・取消し/終了は既存fixtureとE2Eを利用する。
- root prefixとlinker path変更がagent/run_jsにも届くため、実装/総合テストが許可された後に実プロバイダの短いファイル集計→保存→英文最終回答を1課題確認する。docs参照は指示せず観察結果を記録。最大6モデル要求・115秒で区切り、製品の回帰とモデルのAPI誤用を区別し、失敗を成功に上書きしない。
- 初回失敗/再試/skip/未確認を記録し、7.1の到達と7.2未着手を段階案へ反映する。公開Go互換性をレビュー済みの契約として記録し、release時の実取得検証手順を整える。commit/push/releaseは別依頼。

完了条件：未解消P0/P1/P2なし、外部利用・NOE自身の利用・既存JS契約を確認でき、7.1を公開可能な実装として示せる。外部release済みとは区別する。

## 予定する検証

今回は以下を実行しません。実装時に新しいbaseline/manifestを取り、関連→コードレビュー→総合の順に進めます。Go versionは現在の1.27.2を維持します。

```sh
CGO_ENABLED=0 go test ./stdlib ./internal/noestdlib ./internal/stdlib/... ./internal/nativeio ./internal/hostio ./internal/runtime ./internal/apidocs -count=1 -timeout=180s
CGO_ENABLED=0 go test ./... -count=1 -timeout=300s
CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... ./stdlib/... -count=1 -timeout=300s
CGO_ENABLED=0 go test -tags=e2e ./tests -count=1 -timeout=10m
CGO_ENABLED=0 go vet -tags=e2e ./...
```

通常CIでは既存unit/vet gateとrelease profile/workflowの生成整合を維持し、そのunitに外部consumerのportable試験を含めます。reference Linuxではnative IO/Worker/cancellationとmodule artifactまで確認し、6対象はlinux/darwin/windows × amd64/arm64のcompileで実機実行と区別します。外部consumerのGo commandにもGOOS/GOARCH/CGO_ENABLEDを明示し、親testの環境任せで対象を取り違えません。

大きなbuild/cacheはdisk、GOTMPDIR/TMPDIRは短い/var/tmp pathを使います。既存MCP bundleを指定し、環境由来のskipを増やしません。root配布binary、利用者のagent-server-build.jsonlと_refs/は読取り・変更・archiveの対象外です。実取得serverのread-only metadata確認以外に、今回provider/公開APIへの外部書込みは行いません。

## 7.2へ進む際の計画入力

7.1が成立した後、agent拡張について次を確認し、別のbounded計画/レビューを作ります。以下は将来の設計入力であり、今回は公開型や実装方式を確定しません。

1. 外部ホストがGo設定で明示提供するprovider/認証/観察と、NOE CLIの既定値解決を分離する。generic stdlibへのSDK依存を増やさない。
2. 現行agent.OptionsのProvider/Decision/Image/ProviderOptions/ImportMap/Connection/Observe等が参照する内部型の公開closureを整理し、全部alias公開する案と小さい接続interfaceを比較する。
3. run_jsと子agentを誰がどのNOE executableで起動するかを決める。外部Goアプリ自身がNOE CLIであるとは仮定せず、独立したNOE executableやhost runnerの明示接続を検討する。
4. host/runtime/operation/broker/子processの所有、取消し・終了・観察callback、既存CLIとの共通経路を定義し、外部consumerとfixture providerで確認する。認証値をAPIや記録へ不要に公開しない。

agentまで含む外部利用とv2の完成範囲は、この計画入力を基に利用者と具体化します。7.1をagentの一括公開やTUI/会話復元等の追加へ拡大しません。

## 計画レビューと停止条件

新しいreviewerへ会話履歴を渡さず、限定contextと本計画・段階案・必要なsourceをread-onlyで渡します。評価基準は7.1/7.2の区切り、正規module pathとv2/配布、公開型の構築可能性と互換性、内部との依存方向、NOE自身の共通利用、外部consumer/CI/公開前後の証拠の区別、既存の所有/JS契約の維持です。

初回30分、10分進展なしなら確認済み範囲と暫定findingを返します。re-reviewは変更部と既存findingに限定して各15分。利用者の指定に従い、P2以上がなくなるまで修正と再レビューを反復します。採否・計画修正・最終確認は親が担当します。

新しい公開runner/agent設計、別module化、engine変更、広い権限/transportの変更が必要なら、局所修正として進めず根拠・代案を利用者へ戻します。repositoryやサーバーへの公開/設定変更を、計画/実装の検証に紛れ込ませません。
