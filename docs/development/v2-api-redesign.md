# v2：API再設計と移行契約

[改訂レビュー記録](v2-api-redesign-review.md)。計画段階で停止し、製品実装・動作検証は未実施。

2026-10-21。基点：`c1006f1c1604194aaa9ca61bdb07aed2666702b5`。利用者は提案を採用し、破壊的変更を許可した。状態：改訂・通常/批判的レビュー・指摘反映・限定再確認を完了。残る必須指摘なし。製品実装は未着手。

本書はAPIと機能変更の正本。[実装計画](v2-agent-runtime-implementation-plan.md)は所有/終了・スライス・検証、[責務台帳](v2-responsibility-boundaries.md)は所属と経路の正本とする。以前の「現行JS API維持」を前提にした計画とレビューは過去snapshotの記録であり、本改訂を承認済みと扱わない。

## 1. 採否と変更範囲

| 提案 | 決定と理由 |
| --- | --- |
| HostとJS binding | 採用。Host/ClientをGoの入口とし、bindingはHostを借用して自分のOperationだけを所有する |
| 実行設定/Loop | 採用。EnvironmentとProfileを唯一の設定元とし、個別APIのLoop/cwd/env/stdio重複を公開面から除く |
| I/Oの導入単位 | 採用。stdio/file/command/raw socket/signalを独立選択し、Process object導入を他APIの前提にしない |
| loader | 採用。root/子/Workerで同じfactory契約を使い、実行ごとにloader/cacheを作る |
| control | 採用。HostはGoの管理操作、NOEは受付HTTP/発見file/既定場所を持つ |
| docs | 採用。純粋な参照moduleとして常時導入。Docs boolと有効化別の案内を廃止 |
| provider/catalog/decision/image | 採用。agent管理なしで使えるmodel serviceをagentlib内に置く |
| auth save | 採用。model認証機構を再利用する独立helperとし、agent Host/Operationを要求しない |
| ログ観察 | 採用。live観察からfile targetを外し、入力dataの解析とfile読込み/待機を分ける |
| instruction | 採用。本文供給関数を初task時に呼び、directory規則はNOE adapterへ移す |
| file/commandの複数API | 共通backendを採用。Node形式とWeb Streams形式の用途差を維持する |
| 画像の入力/生成/保存 | model serviceはbytesを扱い、file helperを上位adapterへ分離。JSのmetadata-only方式は維持し、画像asset storeやbytesのJS往復は追加しない |
| hook client | 同じClient契約/実装へ統一。hookへ渡すapi.agentsはhook scope、通常importはtask scopeという既存の利用差を維持。ambientなhook自動判定やimport禁止は追加しない |
| task識別子/取消し | agentId/commandId/runIdの役割は維持。commandIdは既に受付時に確定するtask識別子であり、taskIdへの改名だけでは機構を減らせない。task取消しとinstance停止のAPIは分ける |
| 内部HTTP RPC | Go Clientを共通操作入口にすることに伴い、アプリ内task/toolは直接呼出しへ移す。内部broker/Connection/認証tokenを最終経路から除去する。外部人間向けHTTP controlとは別 |

同一repository/Go module内の分離。別repo/module化、engine/SDK更新、Workerからのagent、親の寿命を超えるagent、新たな会話永続化、一般的な権限framework、v2公開は含めない。API名の一括改名やNode.js全互換も目的にしない。

## 2. noestdlib：実行環境と資源を一箇所へ

以下は実装する公開面の形。命名/細部の型分割はスライス内で調整できるが、設定の意味・所有・到達先は変えない。

```go
type Environment struct {
    Cwd string
    Args []string
    Env map[string]string
    Stdin io.Reader
    Stdout, Stderr io.Writer
}
type Profile struct {
    Environment Environment
    Modules Modules
    NewLoader LoaderFactory
    Initialize RuntimeInitializer
}
func NewRuntime(ctx context.Context, profile Profile) (*Runtime, error)
func Execute(ctx context.Context, source Source, profile Profile) (Result, error)
// Runtime: Run(Source)、Cancel()、Close()、Bindings()。
func Attach(rt *quickjs.Runtime, loop *Loop, profile Profile) (*Bindings, error)
type RuntimeInitializer func(context.Context, *Bindings) (cleanup func(), err error)
```

Environment.Cwd/Env/Args/stdioを各APIが共用する。Env=nilは空map、stdio=nilは空入力/破棄。Cwd空はRootなしなら作成時のGo cwd、Rootありなら仮想rootの`/`を一度採用する。明示mutable値はコピーし、callback/client/stdioは借用する。os.Chdir/os.Setenvは使わない。

ModulesはProcessInfo、Stdio、FS、Commands、DNS、Fetch、Serve、WebSockets、RawSockets、Signals、Workers、OS情報をそれぞれ選べる設定とする。純粋な標準APIとnoe:docsは常時導入する。Web Streams等の純粋な前提APIをまとめて無効にするNoWebAPIsは最終公開面から外す。FSがNode形式とnoe:fs-handleを、Commandsがchild_processとnoe:processを選択する。独立した「native IO全部」のswitchは置かない。

個別APIへLoop/cwd/env/stdioを置かず、runtimeが所有する一つのLoopとEnvironmentを内部で接続する。Process.Exit/Unhandledを自由callbackで上書きする設定は廃止し、実行内exit/未捕捉例外をResultへ記録する。低水準Attachでもhostのos.Exitは呼ばない。callerはExitCode等の結果を読んでアプリの終了を決める。既存stdlib.Install/Coreの公開aliasは移行後に撤去し、互換設定を恒久保守しない。

NewRuntime/Run/VM操作/Closeは同じowner goroutine。Runは一つのentry実行、再実行にはfresh Runtimeを作る。Cancelは他goroutineから実行contextだけを止める。Executeは作成/Run/Closeの便利な入口。advanced Attachはcallerが一致するruntime/Loopと終了を管理し、別の設定型を持たせない。既存Random/OS情報/Worker上限などの独立した設定機能はProfile/Modulesへ移し、cwd/env/Loop統合を理由に失わない。

### binding viewと実行別initializer

managed Runtime.Bindings()とadvanced Attachの返り値は同じ借用viewを提供する。Bindingsは対象VM/Loop、実行context、固定したEnvironment、その実行に導入したGo FileSystem、Worker実行かの読み取り専用情報を持つ。FileSystem=nilはFSなしであり、Host.Runtimeから代用しない。FileSystemは共通backendのGo境界で、workerからcontext付きread/atomic write等を呼べる。JS値を渡さず、別Rootへ再解決せず、borrowerにはbackendのClose権を与えない。

agent.Attach、model/auth/logの各bindingは、このBindingsを明示的に受け取る。たとえばmodel.Bind(ctx, bindings, service)は単独model操作を、auth.Bind(ctx, bindings, options)は単独authを導入する。agent内ではscope付きmodel clientを同じbinding実装へ渡す。viewのVM/Loop操作はownerだけが行い、Go I/Oはviewの実行contextとcaller contextの両方へ従う。

Profile.Initializeはstdlib導入とBindings作成後、entry評価前にownerで一度呼ぶ。root/task/tool/Workerそれぞれで呼び、共有するのは関数であってJS値やbinding instanceではない。NOEのinitializerはBindingsのWorker情報で分岐し、root/task/toolには独立auth/logを明示導入し、Workerにはそれらを導入しない。agent/modelもWorkerには追加しない。外部hostは独自initializerまたはnilを選べる。agentexecがNOE既定authを補うことはない。

initializerはGo bindingの登録を行い、entryや利用者moduleを先に実行しない。runtimeの取消し後、entry評価/driveが戻ってからownerで返されたcleanupを実行し、その後Bindings/VMを閉じる。advanced AttachではBindings.Closeが自分のadapter/initializerを回収し、借用VM/Loopは閉じない。setup途中のerrorでcleanupも返された場合はそれを回収し、callback自身も未返却の部分資源を片付ける。cleanupはJS配送完了を待たず、共有service/元sinkを閉じない。

### file・cwd・command

RootなしではEnvironment.Cwdが実際のcwd。Rootありではscript-visible cwdを仮想root内として扱い、共通file backendが物理pathへ変換する。Rootの物理pathは作成時のGo cwd基準で固定する。process.cwd、Node FS、fs-handle、画像file helperは同じ仮想path契約を使う。ReadOnly/symlink/rename等の現行FS契約を共用し、native handleだけ別OS pathを開く経路を廃止する。

commandはEnvironment.Cwdから得た物理directoryで起動し、相対実行fileとPATH/PATHEXT/ComSpecも選んだEnvironmentに従う。絶対実行fileとPATHのentryはOSのpathである。FS.Rootはfile APIの範囲であり、許可したOSコマンド全体を閉じ込めるものではない。コマンドも使えるNOE profileの通常権限は維持する。

sync/async Node commandとnoe:processは共通processscopeを使用する。直接起動を外部hostの既定とし、POSIXでhost SIGKILL後の子孫回収が必要ならProcessSupervisorの実行fileを明示する。NOEは自分のhelperを指定する。library initによるprivate引数捕捉は廃止し、helper mainから明示dispatcherを呼ぶ。self-helperの依存initが再実行可能である条件と、別helperも選べることを説明する。WindowsはJob Objectを使う。

### loader

LoaderFactoryは実行contextとentryのorigin情報を受け取り、owner上で一つのloaderを作る。map解釈・ローカルfile読込みは付属のfactory helperで組み立てられ、外部hostは独自factoryを渡せる。root/子/Workerで共有するのはfactory設定であり、loader instance、module cache、JS値ではない。factoryは複数実行から呼ばれ得る。

entryはメモリ本文と仮想source IDで扱い、generated JS fileは作らない。eval/task/toolの仮想entryからの相対importはEnvironment.Cwdをbaseにする。通常file entryと読込済みmoduleからの相対importはreferrer基準のESM契約を維持し、全importをcwdへ寄せない。仮想IDの相対解決はloaderへ明示したbaseで行い、既存のprivate一時directoryを互換維持する分岐は残さない。stack trace/イベントは同じsource IDを使う。

hook登録ではspecifierと登録indexだけをHostで検証/固定する。Hostのcwd/import-mapでOS pathへ先に正規化しない。各taskのloader作成後にhookを解決/読込みし、canonical module ID＋登録indexでそのtaskの呼出しを同定する。同じmoduleの複数登録はindexで区別し、重複禁止を新設しない。メモリ/bare hookもfactoryで供給でき、module内の相対importはcanonical referrer基準となる。解決/読込みエラーは初model呼出し前のtask失敗とする。別queue taskではfactoryから再解決し、同じtaskではmodule/cache/呼出し同定を固定する。このエラー時期の変更は移行説明へ含める。

advanced Bindingsはentryを準備/実行する入口も提供し、その時点のSource/originでfactoryを作る。managed Runtime.Runも同じ内部準備を呼ぶ。直接engineを操作するcallerはこの準備後に行い、loader instanceを別runtimeへ渡さない。Workerは親profileのfactoryから自分のloaderを作り、stdinは空、agent moduleは導入しない。

## 3. noeagentlib：model service、Host、Client、binding

公開packageは`agent`に加えて`agent/model`・`agent/auth`・`agent/log`を責務別入口とする。同じagentlib内のpackageであり、別製品/versionを増やさない。

### model serviceと認証

model serviceはcatalog、Generate、Decide、GenerateImageを提供するSDK非依存のGo入口。既存SDK adapter、認証のread/refresh/lock/saveとusage情報を再利用する。serviceはagent Host、task、JS runtime、NOE controlへ依存しない。単独catalogにはモデル要求/credential本文読込みを要求しない。

Optionsは明示ConfigFile/ConfigDir/Env/Credentials/HTTP clientおよび既存custom providerを扱う。空ConfigFileは組込み定義のみ、Env=nilは空、ConfigDirからHOMEを探索しない。相対pathは明示したbase directoryで固定する。定義のfile→明示Env override→provider別CredentialFilesの順で選択し、選択providerだけを遅延読込みする。未使用placeholderだけでは失敗させず、OAuthは選択した同じfileを更新する。custom providerで未実装の操作はunsupportedとして返し、別providerへ切り替えない。現在のOpenRouter固有overrideはNOE adapterが明示Optionsへ変換する。

agent内ではClientのscope wrapperがprovider要求前の受付・取消し・group limit・usage・因果関係を処理して同じmodel serviceを呼ぶ。JS task/toolへ無制限のserviceを直接導入しない。GenerateImageのGo要求からOutputを外し、bytes/metadata/usageを返す。SessionIDはagent instanceで固定、run IDはtaskごと。standalone呼出しは明示sessionまたは要求単位の識別を使い、無関係な呼出しを同じsessionへまとめない。SDKのwire形式・cache headerの既存修正は維持する。

agent/authは明示した保存先へサインイン結果を検証/保存するhelper。JSのnoe:auth導入は独立して選び、Hostやagent Operationを作らず、runtime context/Loopで処理する。OAuth refreshと役割を区別する。model serviceやauth helperは借用HTTP clientをCloseしない。

### Go入口と所有

```go
type InstructionSource func(context.Context) (string, error)
type HostConfig struct {
    Models model.Service
    Runtime *stdlib.Profile
    Instructions InstructionSource
    Policy string
    Observe func(Event)
    Trace io.Writer
}
func NewHost(ctx context.Context, cfg HostConfig) (*Host, error)
func (h *Host) NewClient(ctx context.Context) (*Client, error)
func Attach(ctx context.Context, bindings *stdlib.Bindings, host *Host) (*Binding, error)
```

Hostはrootのinstance/会話/session/group/model接続とtask/tool実行を所有する。ConfigにはExecutable、Connection、ControlDir、Auth、InstructionDirs、Filesを置かない。Runtime nilならspawn/runを受付時に拒否し、NOE executableへ戻らない。Modelsは借用し、Host終了で共有service/HTTP clientをCloseしない。

Host.RuntimeのEnvironmentではStdin/Stdout/Stderrをnilに予約し、非nilならNewHostの設定errorとする。task/toolはtemplateをコピーして専用の空入力/captureを結び、元Profileは変更しない。initializerへ渡すBindingsもこの派生後のEnvironment/outputに接続する。汎用Runtimeのstdioは従来どおりcallerが指定でき、root runtimeとHostのtemplateは独立する。

Clientは一つのOperation/所有scopeを持つGo入口。Spawn/Run/Queue/Observe/WaitTask/ReadTaskResult/Steer/CancelTask/StopAgentとscope付きmodel操作を提供し、Closeで自分の要求と所有agentを取消し/joinする。Host.Closeは全Client/instanceを回収する。Go利用者は入口用のJS runtimeを作らずにこれらを呼べる。task/toolのJS runtimeは必要に応じてHostのExecutorが作る。

AttachはHostを借用して専用Client/Operationを作る。Binding.CloseはそのOperationとbinding workerを回収し、Host/rt/Loopを閉じない。root runtimeを閉じても、同じHostの別Clientは動作できる。呼出し元Client自体の終了では、既存契約どおりその所有agentを回収する。Hostより長く生きるagentや新しいdetached動作は追加しない。

root Go管理用のHost操作は一覧・steer・queue・cancel・stopを提供する。NOE control adapterがこれを使い、JSのscope付きClientへ全Host操作を公開しない。Go値をJSへそのまま露出せず、許されたmethodだけbindingする。

### 共通操作とJS

現行HTTP handlerから入力/操作/結果の処理を共通serviceへ抽出する。Go ClientとJS adapterが同じ受付を呼び、contextとscopeの同一性をGoで保持する。所有/委譲/自己許可拒否/group枠等の検査はadapterでなく共通受付に残す。内部Clientはscopeを自分で付け替えられない。task/toolの実行用Clientは対応するtask/tool scopeへ束縛し、新しいroot Hostを作らない。

内部HTTP server・Connection JSON・scope token/env渡しは撤去する。finish/final commitは対応taskのClientからだけ受け付け、Executorのcode=0だけで完了にしない。Go直呼出しへ移しても、現在のfinal確定・取消し・認証済みfinishに相当する所有検証と結果確定の規則を維持する。

JSはrun_agent/spawn_agent/queue_agent/observe_agent（liveのみ）/wait_task/read_task_result/steer_agent/list_modelsを維持する。run_agentはspawn＋instance終了待ち。interrupt_agentは`cancel_task({agentId, commandId})`と`stop_agent({agentId})`へ置換する。前者は未開始taskも取り消せ、実行中ならそのtaskだけを取消す。keepAlive/他queueは残す。後者はinstanceとqueue/所有子を停止する。受付ackと後始末完了の区別を維持し、待機で結果を確認できるようにする。

agentIdはinstance、commandIdは受付済みtask、runIdは実行開始後のrun。steerは既存runId指定を維持する。結果原文、includeAnswer:false、UTF-8範囲取得、反復wait、所有scope内での保持を維持する。単純な名前変更だけのtaskIdは追加しない。旧interrupt_agentやlogPath分岐の恒久aliasは置かない。

hookは同じClientのmethod集合を使い、api.agentsはhook scopeに束縛する。通常importからの操作はtask scopeとして維持する。hook終了で自分の要求を取消し、そこで起動した子の既存所有はrunへ残す。共有の可変「現在hook」や追加runtimeは使わない。

## 4. fileを使う便利機能の配置

### instruction

Instructions=nilは追加本文なし。instance初taskのhook/modelより前にsourceを一度呼び、事実→Policy→source本文→instance.systemで固定する。queueでは再呼出しせず、新しい子instanceで改めて呼ぶ。エラーはprovider未呼出しのtask失敗。sourceは複数instanceから並行に呼ばれ得てcontextへ協調する。任意callbackの強制停止は保証しない。

NOE profileがcommon/workspace dirsの`.md`・順序・symlink・欠けたdir/読込不能を実装し、場所は初使用時に固定する。NOE固有のautonomy/英語コメント/モデル選択方針はPolicyへ渡す。外部hostの空Policyへ補わない。NOE_CONTEXT_STATUSは履歴であり、送信ごとの動的値をinstructionに追加しない。

### ログ観察

agent/logはJSONL/event入力からrunの状態・差分・usage/contextを復元する。Goでは渡されたreader/bytes、JSの`noe:agent-log`では渡されたstring/Uint8Arrayを解析する。path・file open・poll・provider・Hostを要求しない。JS adapterの導入もHost生成なしで選べる。解析coreをCLI/SDKへ依存させない。

初版のJS入口は`analyze_log(input, {runId, cursor?, includeThinking?, includeOutput?})`とする。inputはその時点の完全な履歴snapshotであり、cursorは返すevent差分だけを絞る。状態は全履歴から計算し、末尾の未完了行は無視する。callerが差分だけをinputへ渡して状態を失うAPIにはしない。全fileをJSへコピーする費用は残るため、CLIはGoのreader解析を使い、大きいログをわざわざJSへ往復させない。

file読込み/追記待ちはnoestdlibのI/Oと呼出し側が行う。NOE observeのpath/--follow/--json/--waitを維持し、snapshot sizeの固定・完全行・poll/取消しをNOE adapterで扱う。保存ログに対するJSの旧wait optionは廃止。live agentはobserve_agent/wait_task、保存ログは新しい解析入口へ移行する。ログformat/cursorは既存のまま読めることを受入とし、破壊的API変更と保存dataの無条件破棄を混同しない。

### 画像

Goのmodel生成はbytesだけを扱う。JSのread_image(path)とgenerate_image({output,...})はfile helperとして残し、stdlibの共通file backendを借用してread/atomic saveを行う。Root/ReadOnly/cwdはその実行profileに従い、独立したagent.Filesは廃止する。画像format/metadata検証はagentlibのimage処理へ残す。

read_imageはactive run_jsのimageOwnerを先に検査し、実行file backendから読んだbytesを内部Go接続でそのtoolの画像候補として登録する。JSへbytesを返さず、rootの別cwdで再読込もしない。generate_imageはFSなし/ReadOnly等の設定不足を要求前に検出し、model serviceから得たbytesを同じbackendへatomic saveする。保存時のOS失敗は生成後も起こり得て、利用量とerrorを保持する。単独画像生成ではHostなしのmodel bindingを使え、agent内ではgroup/scope wrapperを通る。

file backendをworker処理から呼べるGoの内部境界で供給し、QuickJS値をowner外へ持ち出さない。借用元をCloseしない。新しいartifact ID/storeや画像bytesをモデル向けtextへ展開する方式は導入しない。

## 5. docs・イベント・NOE adapter

noe:docsは常時参照でき、全APIカタログであり導入済み機能一覧ではない。internal/apidocsの本文/metadata/検索/error案内を共通正本とする。CLI・JS・Worker・agent runtimeから同じ参照を使い、内容dataを持つだけでagent SDKのimportを要求しない。非導入APIのページ削除やprofile別の全文生成はしない。

agentlibが構造化eventの意味・因果関係・usageを出し、NOEがJSONLの出力先/端末表示を選ぶ。debug Traceの意味を失う統合はせず、内部RPC由来の診断だけ新しい操作診断へ置換する。原文/モデル向け結果・画像metadata・session/cache観測を維持する。schemaが変わる項目は版と対応を移行ガイドへ記す。

internal/noeprofileはNOEのdefaults・env filter・instruction source・model Options・helper選択とauth/log initializerを供給する。NOE controlは別のアプリadapterでHTTP/認証token/発見fileを所有し、HostのGo管理操作を呼ぶ。Hostへ既定directory resolverを逆注入しない。設定file/認証本文は必要時に読み、NOEを使わないlibrary入口でNOEの環境やHOMEを探索しない。

controlの自動activationは、NOEのObserve adapterが最初のagent受付event（agent.accepted）をアプリ所有controllerへ通知して開始する。callback内でHostを同期再入せず、controllerがHTTP開始/登録を行い、受付eventのcallbackを待たせない。単純run/evalやmodel/catalogだけではcontrolの場所を探索しない。成功後にcontrol readyを通知し、CLI discoveryが利用できる。初回登録errorはNOE実行の診断errorとしてapp contextを取消し、アプリの終了coordinatorがowned Host/controlをjoin/登録除去後に非ゼロ終了する。受付後の開始なので、従来の「最初の操作をreject」から「NOE実行の失敗」へ変わり、control ready前のtask開始を禁止する保証は置かない。この変更でlibraryの操作受付へNOE初期化callbackを戻す必要をなくす。activation中の終了と登録の競合はcontrollerがjoinして処理する。

## 6. 移行方針

| 現行/旧計画 | v2での置換 |
| --- | --- |
| agent.InstallがHostも所有 | agent.NewHost＋Go Client、JSは借用HostへのAttach |
| agent.ConfigのExecutable/ChildImportMap/Load | HostのRuntime profileと共通LoaderFactory |
| nested Core/Loop/stdio/IO一括設定 | Profile/Environment/Modules。Loopは一つ |
| agent.ConfigのInstructionDirs | InstructionSource。NOEがdirectory adapterを提供 |
| agent.ConfigのFiles | 廃止。ログはdata解析、画像file helperは共通stdlib FS |
| agent.ConfigのAuth/ControlDir | 独立auth binding、NOE control adapter |
| Docs bool | 廃止。noe:docs常時導入 |
| observe_agent({logPath,...}) | file取得＋noe:agent-log analyze_log |
| interrupt_agent({runId/agentId}) | cancel_task({agentId,commandId}) / stop_agent({agentId}) |
| 内部RPC/Connection/長HTTP待機 | 直接Clientとscope/context。長いmodel/tool待機の契約は維持 |

Go/JS API型、examples、内蔵docs本文/metadata/error、NOE生成task/tool source、外部consumerを同じ候補で更新する。JSの標準API全改名はしない。CLI observe/agents/steer/queue/interruptの表記は維持でき、interrupt --runはNOE adapterがrunIdからtaskを解決してcancel_taskを呼ぶ。内部API互換aliasは残さない。

この改訂に必要な実装・検証は実装計画の11スライスで行う。今回は計画・通常/批判的レビュー・反映後に停止し、製品コードや実providerは変更/実行しない。
