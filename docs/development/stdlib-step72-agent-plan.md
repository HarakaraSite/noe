# NOE版stdlib：ステップ7.2・agent外部接続計画

2026-10-19。[段階案](../future-candidates.md)と[7.1計画の7.2入力](stdlib-step7-library-plan.md#72へ進む際の計画入力)に基づく。今回は計画の作成・独立した通常レビューと必要な修正だけを行う。製品source変更、test/build実行、実プロバイダ呼出し、commit/push/releaseは行わない。[レビュー記録](stdlib-step72-agent-review.md)に対象版と結果を残す。

## 現在地と完了条件

基点HEADは `15e34b8ab060f73b580b3d8114a30b6879ff24de`。その上の7.1実装は未コミットで、[実装・検証記録](stdlib-step7-library-implementation.md)・[結果](stdlib-step7-library-results.json)にある公開 `stdlib`、正規 `/v2` module、本体切替、外部consumerと検証が成立している。今回この差分を保持し、7.1をやり直したりrelease済みと扱ったりしない。

7.2の到達点は、別Go moduleのアプリがcaller所有のgo-quickjs runtimeと公開stdlibへagent拡張を選択して導入し、独立したNOE executableを使ってagent/task/run_js/子agentを実行できること。provider/認証、module読込み、hook、イベント、取消し・終了が同じ内部実装へ接続され、NOE本体もその入口を使う。汎用stdlibだけの利用へSDK/agent依存を増やさない。

7.2が実装・検証まで終われば、当初の7段階の実装範囲は完了とする。常用での不具合修正、利用者による安定性判断、v2.0.0の公開と実tag取得検証はその後の作業。今回の計画や後続の実装だけで公開を実行しない。

対象外は別repository/module、engine/HTTP再実装、任意のhost runner interfaceやGo Eval/Execute runner、Goからの全broker RPC公開、外部Workerへのagent自動継承、会話復元/TUI/新しいretry、権限モデルの変更、JS改名・fs統合。これらをv2の追加必須条件にしない。

## source調査から分かった接続点

- `internal/runtime/agent.go`はprivate JS bridge、評価ごとのOperation、Promise返却とLoop.Post、hook scopeとログ観察を持つ。`runtime.go`のmodule loaderがagent/api/hooks/task/history・decision/image/authのESM sourceを供給する。`auth.go`も現在のOperationのcontextを使う。Go Hostだけの公開ではJSのimportや取消しが成立しない。
- `internal/agent.Options`にはprovider/decision/image interfacesのほか、provider.Options、内部modulemap.Snapshot、Connection、events.Eventがある。Options全体をalias公開すると内部の接続capabilityや設定型まで公開契約になる。
- providerのRequest/Response/StreamEventとdecision/image provider境界は既にSDK型を含まない。provider.Optionsの参照先はHTTPClient、Definition、ImageGenerationDefinitionと標準Go型で閉じる。認証saveのOptionsもHTTPClientとDiscoveryURLだけである。
- `Host.NewHost`はGo stateだけを作る。provider registry/config/key読み込み、broker listenとcontrol登録は後から発生する。registryはcustom Provider併用時にもcatalog/model上限を供給する。custom Providerの`HasProvider(string) (bool,error)`も既存の名前検査で使われ、未実装時の従来fallbackはopenrouterである。
- `Runner`はtaskとrun_jsを別NOE processで動かし、scope付きConnection、import-map snapshot、cwdを渡す。CLIだけが`os.Executable()`と`NOE_AGENT_CONNECTION`を解決する。外部アプリを同じ方法で再起動してはいけない。
- `run_agent`はmanaged instanceのtask完了を待つAPIであり、instance自体の終了ではない。OwnedLifetimeがkeepaliveを持つので、有限タスクで`Loop.Run`が自然終了すると説明してはいけない。
- Workerは現在agent/decision/image/authをimportできない。これは7.2でも維持する。観察callbackはprovider/broker等のGo goroutineで同期・直列実行され、JS ownerとは限らない。

## 採用する構成

公開packageを同じmodule内の `forge.harakara.site/littleisland/noe/v2/agent` とする。公開`stdlib`とは別にimportし、callerがstdlibを導入してからagentを追加する。公開stdlib側にagent設定を足さない。

新しい非公開 `internal/agentbridge` にruntimeのagent/auth JS接続を抽出する。Host/Operation/broker/SDK/runnerそのものは既存のinternal packageに維持し、bridgeは登録・module source選択・非同期返却・hook/operation lifetimeを一か所で所有する。public agentは設定変換と小さいBinding wrapperとし、JS sourceやbroker logicを複製しない。

```text
external Go host -> public stdlib -> existing generic composer
                -> public agent  -> internal/agentbridge
                                      -> internal/agent/provider/credentials/observation
                                      -> existing broker + runner -> explicit NOE process
NOE runtime     -> public agent (root)
                -> same internal/agentbridge (private connected-child adapter)
```

CLIのscope付き子processは内部Connectionを消費するため、private adapterで同じbridgeへ接続する。これをpublic Configへtoken付きConnectionを足す理由にしない。通常のNOE rootはpublic Installを使い、private adapterでも登録・hook・回収を再実装しない。bridge/public agentのproduction依存にinternal/runtime・CLIを入れず、循環依存を作らない。

### 比較した案

| 案 | 判断 |
| --- | --- |
| internal/agent.Options・Host・Operation・Runnerを全alias公開 | 型closureと認証/Connection/内部RPCまで契約が広がるため不採用 |
| public Executeが現在のinternal/runtimeを丸ごと呼ぶ | caller所有runtimeへ追加できず、CLIの全OS機能・loader方針と結び付くため今回は不採用 |
| 汎用RunnerやTransport interfaceを新設 | 既存NOE process/brokerが実際の実行先なので初版には不要。まず明示Executableを使う |
| SDK非依存DTO/interfaceの限定alias＋独自Config/Binding＋共通bridge | **採用**。既存provider注入と型identityを保ち、公開面を必要な接続に限る |

## 公開APIの初版

以下を実装入力とする。登録や処理の実質を変えないファイル分割は実装時に判断できる。公開symbol増加・所有/権限変更は理由を記録し、計画外のscope拡大は利用者へ戻す。

| 要素 | 形と契約 |
| --- | --- |
| `Install(ctx context.Context, rt *quickjs.Runtime, cfg Config) (*Binding, error)` | 同じowner上で導入。一つのbindingが一つの実行lifetimeを持つ。ctxは必須で、nilはerror。runtime・Loopを内部で作らない |
| `Config` | 下記の公開struct。internal.Optionsのaliasにはしない |
| `Binding` | 内部bridgeへの非公開参照だけを持つopaque struct。zero value利用・複製・内部型assertionを契約にしない |
| `(*Binding).Close()` | Go側のagent資源を取消し・joinする。冪等、Go側からの並行呼出し可。runtime/Loop・借用sinkをCloseしない |
| `(*Binding).Context() context.Context` | このbindingのOperation context。独自のhost仕事やauthへ同じ寿命を渡すために使い、Close/ctx取消し/runtime終了でcancelされる |
| `Provider`, `Request`, `Response`, `StreamEvent` | internal providerのSDK非依存型のalias。Generateとstream callbackの型identityを保持 |
| `DecisionProvider`, `DecisionRequest`, `DecisionResponse` | decision境界だけのalias。parser/helper群は公開しない |
| `ImageProvider`, `ImageRequest`, `GeneratedImage`, `ImageError` | image provider境界と構造化errorのalias。codec/保存関数やinternal image stateは公開しない |
| `ProviderOptions` | 独自struct。registryで有効なConfigFile/UserAgent/KeyFile/BaseURL/HTTPClient/DecisionServerURLだけを公開し、内部provider.Optionsへ変換する |
| `HTTPClient` | provider.HTTPClientのalias。標準http.Request/Responseだけを使うDo interface |
| `Event` | events.Eventのalias。既存の構造化イベントをObserveで受ける。Writer・control capability・observation内部型を一括公開しない |
| `AuthOptions` | 独自struct `{HTTPClient HTTPClient; DiscoveryURL string}`。credentials.Optionsへ変換する |

Configとregistry設定のfieldsを次で閉じる。

```go
type ProviderOptions struct {
    ConfigFile        string
    UserAgent         string
    KeyFile           string
    BaseURL           string
    HTTPClient        HTTPClient
    DecisionServerURL string
}

type Config struct {
    Loop             *stdlib.Loop
    Cwd              string
    Executable       string
    Load             quickjs.ModuleLoader
    ChildImportMap   json.RawMessage
    Providers        ProviderOptions
    Provider         Provider
    DecisionProvider DecisionProvider
    ImageProvider    ImageProvider
    Auth             *AuthOptions
    Trace            io.Writer
    Observe          func(Event)
    ControlDir       string
}
```

ProviderOptionsはadapter単体向けのProfileを受け取らない。provider/modelごとのAPI・認証・headers・画像設定は現在のJSON schemaを持つConfigFileで指定する。Registryが参照しないOptions.Profileは公開せず、それをGoで構築するためのDefinition/ImageGenerationDefinitionもpublic symbolに含めない。custom Provider自身が持つ設定はそのProviderの責務とし、CLIの既存registry経路の動作を変えない。

すべてのfield/callback/returnの型をpublic agent/stdlib・Go標準・公開go-quickjsから参照できるよう、外部fixtureで構築する。aliasesのexported fields/methods・型identityは互換性対象。SDK更新は今回含めず、engineは7.1と同じv0.27.0を確認対象にする。利用側MVSが選ぶengine版を記録する。

### 設定と導入の契約

- 必須は生きたctx/runtime、そのruntime用にNewLoopしたLoop、明示Executable。Loop nil・Executable空・不正なChildImportMapは登録前にerror。Loopのzero valueや別runtimeのLoopはcallerの契約違反で、engine内部へ依存する検査APIを追加しない。
- ExecutableはNOE binaryのpathでありPATH検索や`os.Executable()`fallbackをしない。相対ExecutableはInstall時のGo process cwdから一度絶対化し、子の作業directoryであるCwdとは分ける。Cwdは指定値またはos.Getwdを一度正規化する。単なるcatalog利用のために起動可能性を事前実行で検査せず、実行失敗は既存runnerのerrorとして返す。
- Configのmutable bytes/maps/slices/設定structはsnapshot化しcaller側を変更しない。Provider/HTTPClient/Load/Observe/Traceなどresourceとcallbackは借用。ConfigFileやcredential/instructions fileの内容は既存どおり使用時の読み取りであり、Install時snapshotとは説明しない。
- Provider nilなら既存registryを使い、custom Providerがあればモデル要求に使う。Providersはどちらの場合もcatalog/model上限の設定になる。customのHasProviderとcatalogの関係を英文説明し、alias公開を理由にprovider名・モデルを制限/追加しない。
- Providers.ConfigFile/KeyFile等の空値は現在のNOE registry/credential既定値を保ち、最初に必要となった際にNOE_CONFIG_FILE/XDG等を参照し得ることを明記する。外部アプリは明示path・HTTPClient・Providerを渡せる。CLIだけがversion入りUser-Agent、CLI専用環境解決、connected capability、既定control directoryを供給する。外部Config.ControlDir空ならcontrol discoveryを自動登録しない。
- Auth nilは`noe:auth`の保存helperを導入しない。CLIは既存Auth optionsがnilの場合も既定の非nil AuthOptionsを渡して従来のmoduleを維持する。非nilで既存save機能を追加し、そのHTTP client/discoveryを明示接続する。providerによる既存credential読込み・OAuth refreshはAuth helperの有無と独立。新しいsign-in方式や認証保存formatを設計しない。
- stdlibは別途callerが選ぶ。agent InstallがFS/Fetch/native IO/Processを追加付与しない。root側image/pathやauth helper等が必要とするpure/Web/Process設定は例と必要条件に明記する。stdlibの制限は別processのtask/run_jsを制限せず、NOEの通常OS権限で実行することを説明する。
- 導入はtransactionではない。失敗時は作成済みGo資源を閉じ、runtimeはcallerがCloseして捨てる。同runtimeへの重複Install・失敗後の再導入・閉じたbindingの再開は非対応。別のfresh runtimeは同じ設定を再利用できる。

## module、hook、子NOEの接続

`Load`はcallerの同期module loaderを明示的に受け取る。Installはagentの組込みESM sourceを優先するloaderを設定し、その他をLoadへ渡す。engineには既存loaderを取得する公開APIがないため、暗黙に既存loaderを探してwrapすると説明しない。nil fallbackは他のsource importを拒否する。非同期loader/import map全体の新しい公開APIは設けない。

agent/api/hooks/task/history、decision/imageの現行sourceとprivate bridge moduleを同じ実装から供給する。Authが有効な場合はauthも扱う。task/history/hooksや`*/internal`を新しいpublic JS APIとして広告しない。CLIの予約名・module identity、import-mapのbuiltin衝突拒否を維持する。外部Goホストの独自Load・root custom module・相対referrer/cache identityを壊さないことを試験する。

ChildImportMapは**子NOE向け**のimport-map JSONであり、Cwdをbaseに既存modulemapでvalidate/absolute snapshot化する。rootの任意Loadを子processへserializeできるとは約束しない。callerは必要なmoduleを子にも読み込ませるmap/fileを提供する。CLIは既存SnapshotをJSON化して同じ内容を渡す。外部の例ではroot Loadの選択と子mapを区別する。

Runner・RunTask・run_jsは現行のprivate source生成、scope capability、cwd、filtered process env、出力上限/omitted bytes、process group/Job Object回収を使う。新しいhost runner interfaceは作らない。config/key/SDKとprovider callbackはroot Go processに置き、子は既存のNOE_AGENT_CONNECTIONを使う。アプリ自身のenvを秘密情報のないものに変える追加契約はせず、既存filterを維持する。raw connection/tokenをpublic ConfigやObserve/Traceへ追加しない。

NOE executableは同じsource/tagからbuildしたものをreferenceとする。公開Go moduleの版とCLI --version文字列の一致だけで互換を判定しない。新しいhandshakeや旧版一律拒否は導入しない。任意の古い/新しいNOE binaryとの互換を保証せず、実際に試した版を結果へ記録する。公開前はcheckoutの一時CLI、公開後は同じv2 tagのbinaryを例で指定する。

## lifetime、owner、終了順

1. callerがctx・runtime・Loop・stdlibを作り、同じowner上でagent Install、その後JS評価/Loop進行を行う。public bindingは一つのOperationとHostを持ち、同じlifetime中のJS呼出しを担当する。別評価用のBegin/End APIは公開しない。
2. CLIはExecuteのctxとentry Sourceをhost構築へ渡し、一評価一runtimeの既存範囲で同じlifetimeを作る。entryの名前/codeを持つroot用fallback loaderを先に組み立ててpublic InstallへLoadとして渡し、runで再度SetModuleLoaderしてagent wrapperを上書きしない。現在のagentOperation/hookScopesと認証context取得をbridgeへ集約し、authだけが古いOperationを参照する状態を残さない。private test helperの変更はこの接続に限定する。
3. private hook scopeは評価Operationの子として現在のbegin/request/observe/endと期限を保つ。ctx取消し・binding終了でhook/観察待ち/要求・owned agent/task/run_jsを取り消す。外部Workerには導入しない。
4. Go側はctxに従う既存provider/HTTP/runnerを使う。Promise生成・JS値操作・settlementはownerだけで行い、Go goroutineは文字列/Go stateとLoop.Postだけを扱う。Close後のposted replyはJSを触らず破棄する。
5. bridgeはasync開始とCloseをgateし、Go側仕事をjoinする。CloseはLoopでJS callbackが実行されるのを待たずに終わる設計にする。ctx終了にはGo-onlyのCloseを接続し、rt.OnCloseも同じ冪等Closeを呼ぶ。Host/Operationのrequest gate・待機・process reapを再実装しない。
6. stdlib導入後にagentのOnCloseを登録し、LIFOでagent/子processを回収してからstdlibの出力境界を閉じる。binding Closeだけではruntime/Loop/native IO/元sinkを閉じない。callerは必ずruntimeとLoopも終了時に閉じる。
7. Observeは既存どおりGo側で直列化し、Host.mu内で呼ばない。callbackは短くし、同じbindingのClose/RPCへ同期的に再入しない。JSを触る通知はcallerがownerへ渡す。event mapを後で使う場合はcopyする。custom Provider/HTTPClient等がctxを無視して永久にblockする場合の強制終了は約束しない。

有限処理の英文例は**root Promiseのfulfilled/rejectedを両方捕捉→done通知→Loop.RunUntil(ctx, done)→rootの結果/error確認→binding.Close→runtime/Loop.Close**とする。run_agentのtask完了後にidle instanceが残ることを説明し、Loop.Runの自然終了を成功条件にしない。deadline時はctx取消しとGo-only cleanupを完了させる。CPU評価にも同じctxを渡し、LoopのdeadlineだけでCPUを停止できるとは書かない。常駐hostは同じbinding lifetimeを明示的に長く保てる。

## 実装スライス

### S72-A：内部bridgeとlifetimeを共通化

- runtime/agent.go・auth.goの接続をinternal/agentbridgeへ抽出。登録、source、hooks、観察、Go-only回収とctx取得をまとめる。
- Executeのctxをhost構築へ渡し、root/connectedの既存動作を保つ。公開APIはまだ使えると案内しない。
- agent/auth/hook/context/runner/cancel関連の既存試験を実行。JS inventoryとWorker拒否を比較し、Host/Loop/出力の終了順を重点確認。

完了条件：同じ内部bridgeで従来のCLIとconnected task/run_jsが動き、private fieldや二重登録への依存が残らない。

### S72-B：公開agent packageとNOE root切替

- 上記Config/Binding/限定alias/Installと英文GoDocを実装。public→bridgeのみの依存にし、汎用stdlibのclosureを保つ。
- NOE rootをpublic Installへ切替。connected childだけはprivate adapterで同じbridgeを使い、CLIの環境/認証/control既定値はCLIに残す。
- 設定snapshot・外部型構築、lazy config/credentials/listener、nil/失敗/重複導入、borrowed資源とctx/Closeの重点試験。

完了条件：外部の型はpublic/標準/engineで閉じ、NOEの通常rootが公開入口を使い、内部tokenやSDK具体型を公開しない。

### S72-C：独立NOE process・loader・provider接続

- 明示Executable、root LoadとChildImportMap、cwd/env、scope/子agent、hook/import、provider/decision/image/Authの注入を接続する。
- 既存runner/認証fixtureでtimeout/cancel/エラー・出力切詰め・子process回収を確認。全SDK試験を外部へ複製しない。
- 有限タスク完了後のidle instance、期限切れ、Closeの並行呼出し、他binding/元sinkの継続利用を確認。

完了条件：external app自身を起動せず別NOEでtask/run_js/子agentが動き、rootのproviderと設定を正しく使って回収できる。

### S72-D：外部consumerとmodule artifact

- `agent/testdata/consumer`へpublic importだけのsourceを置き、root test/helperがcheckout外moduleへcopyして明示実行する。GOWORK=off・GOOS/GOARCH/CGOを設定し、Go child buildには十分なtimeoutを与える。
- 通常unitには、全公開設定/interface型構築・部分stdlib＋agent導入/終了・lazy side effectを確認するportable consumerを入れる。portable部分は実NOE/credential/実providerを要求しない。Executableは起動しないfixture pathで構築できる。
- reference consumerには別NOEを明示注入し、root Go側のfixture Providerからtool call→run_jsで集計/保存→最終回答、nested agent、queue/observe/interrupt、hook・独自module/子mapの少数のflowを用意する。provider/認証はlocal HTTP fixtureとfake fileで確認し、decision/image/Authの型だけでなく実接続も最小ケースで確認する。
- consumer自身を誤ってNOEとして再起動した場合にmarker付きで失敗させる。root Provider callbackが使われたことと、子がroot brokerへ戻ったことを観測する。ctx取消し/JS error/Closeで待機するagent・要求・子process・listenerを回収し、元sinkは開いたままを確認する。
- 7.1のhelperへ後方互換のagent選択を追加する等により、local replaceとreplaceなしlocal GOPROXY artifactを同じsourceで検証する。manifestはtracked fileに加え、既知の新規 `agent/**`・`internal/agentbridge/**` のGo source/testdataと移動/新設したgo:embed対象資産、英文guide・Go例を明示列挙して含める。未コミットの内部bridgeや埋込みJSがzipから抜けないことを配布moduleのbuild/runで確認し、nested module・利用者JSONL・_refs・root binary・credential/成果物を除外する。隔離GOMODCACHE、synthetic rootだけのGONOSUMDB、依存checksum維持、go env -w禁止を継続する。
- public stdlibのSDK非依存とpublic agentにCLI/internal runtimeが混入しないこと、consumer direct importsと実選択engineを検査する。6対象consumer compileを行い、実行したLinux amd64と分けて記録する。

完了条件：外部Goアプリがinternal importなしで実際のagent機能を組込み、local/配布artifactの双方からbuild/run・終了できる。

### S72-E：英文ガイド・例・公開契約

- 英文agent embedding guideとGo例を追加し、7.1 guideからリンク。provider/credential既定値と明示設定、SDKの依存範囲、別NOE指定、root/子loader、Auth opt-in、callback/owner、idle instance/RunUntil、Close順を説明する。
- local replaceの公開前手順と実v2 tag/binary公開後の手順を区別する。7.1の汎用例はagent importを増やさず保持する。
- JS APIと41topic/100detail・26stdlib module・Worker拒否を維持。Go追加を理由にJS IDs/exportやinstructionsへ不要な変更を加えない。

完了条件：英文ガイド・公開型だけで有限のagent hostを作れ、library実装・local artifact・実tag公開の区別が明瞭。

### S72-F：独立コードレビューと総合テスト

- 関連試験後に新しいreviewerで独立した通常コードレビュー。P2以上を修正して変更部/既存finding限定の再レビューを反復し、その後総合確認。
- 全体unit/race/E2E/vet、CLI/影響testと外部consumerの6対象build/compile、main/子/孫/Web子inventory、既存agent/auth/hooks/decision/image/HTTP/MCP/制御/取消しfixtureを使う。MCPは既存準備bundleを明示指定する。
- 実装・総合テストが別途許可された後、external Go consumerから既存registryと明示NOE binaryで実providerの短いファイル集計→保存→英文回答を1課題。docs参照を指示せず観測し、最大6モデル要求・115秒。model misuse/外部障害と製品回帰を区別し、初回失敗・再試・skipを隠さない。
- source manifest、実際のengine/executable版、commands・raw結果・未確認事項を保存。完了時は「7.1/7.2実装検証完了・v2未公開」と段階案を更新する。commit/push/releaseは別依頼。

完了条件：未解消P0/P1/P2なし、外部利用とNOEの既存機能・所有/JS契約を確認でき、常用へ進める実装を提示できる。

## 実装時の検証入口（今回は未実行）

```sh
CGO_ENABLED=0 go test ./agent ./internal/agentbridge ./internal/agent/... ./internal/runtime ./internal/credentials ./stdlib -count=1 -timeout=300s
CGO_ENABLED=0 go test ./... -count=1 -timeout=300s
CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... ./stdlib/... ./agent/... -count=1 -timeout=300s
CGO_ENABLED=0 go test -tags=e2e ./tests -count=1 -timeout=10m
CGO_ENABLED=0 go vet -tags=e2e ./...
```

6対象はlinux/darwin/windows × amd64/arm64。外部consumer/例のGo commandもroot側から明示起動し、nested moduleが./...で自動検証されると仮定しない。portable gateはprovider料金/実credential/実NOEを要さず、reference native/実providerとは分ける。大きなbuild/cacheはdisk、TMPDIR/GOTMPDIRは短い/var/tmpへ置く。

## レビューと停止条件

独立した通常の計画reviewerへ、会話履歴なしの限定context、本文・段階案・7.1契約と必要sourceだけを渡す。評価基準は外部Goからの型/実行可能性、7.1との依存方向、root/connected/loader/認証/hookを含む共通経路、別NOEの明示、実際のOperation/keepalive/回収契約、有限例と外部consumer/CI/artifact、公開前後・常用/v2の区別である。

初回30分を上限とし、10分新証拠・tool結果・中間結論なしなら確認済み範囲と暫定findingを返す。再レビューは変更部と既存findingだけを各15分以内。利用者の指定どおりP2以上がなくなるまで修正と再レビューを反復し、採否と最終確認は親が行う。

任意runner/transport、別module、engineやbroker protocolの再設計、公開Go操作API一式が必要になった場合は局所実装に紛れ込ませず、不足・影響・代案を示して利用者へ戻す。公開tag/リリース時期・常用の安定性判断は利用者が決める。既存7.1差分、利用者JSONL・_refs・root binaryは今回の計画変更対象外。
