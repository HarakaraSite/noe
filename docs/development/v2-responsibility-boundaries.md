# v2：責務・設定・所有・経路台帳（API再設計版）

[改訂レビュー記録](v2-api-redesign-review.md)。計画段階で停止し、製品実装・動作検証は未実施。

2026-10-21。基点：`c1006f1c1604194aaa9ca61bdb07aed2666702b5`。状態：改訂・通常/批判的レビュー・指摘反映・限定再確認を完了。残る必須指摘なし。

[API再設計](v2-api-redesign.md)は機能/移行契約、[実装計画](v2-agent-runtime-implementation-plan.md)は終了順・11スライス・検証の正本。本書は機能の所属と現行入口から移行先までを対応付ける。旧版のFiles/Docsフラグ/内部RPC/InstructionDirsを維持する設計は置き換える。

## 1. 三者と共用部品

| 層 | 実装と方針 | 依存と所有 |
| --- | --- | --- |
| noe | CLI、既定場所/環境の解決、NOE方針、instruction directory source、control HTTP/発見file、event保存/表示、ライブラリ組立 | stdlib/agentと各serviceを利用。アプリのHost/runtime/control/fileを所有 |
| noestdlib | Profile/Environment、JS実行/Loop、loader、FS/command/network/Worker/stdio/回収、docs導入 | agent/SDK/CLIへ依存しない。runtimeのowned資源だけを閉じ、借用元を閉じない |
| noeagentlib | model/認証機構、Host/Client、会話/session/task/queue/group/委譲、hook、run_js、結果、画像処理、event解釈 | 汎用実行とI/Oはstdlibを利用。NOE profile/control/保存先は知らない |
| 共用下位部品 | apidocsのdata/検索、eventsの型とJSONL writer、hostio/processscope等 | 上位の方針を決めず、agent/CLIの実行をimportしない |

noestdlibをagentlibを含む総称には使わない。公開packageはstdlib、agent、agent/model・auth・log。後者はagentlib内の責務別入口で、別製品や別versionではない。同じGo module内で分離し、別moduleのconsumerを受入に使う。

agent/model・auth・logはagent Host/Clientを作らず単独利用できる。agent coreはJS値を保持しない。内部agentexec/bridgeがstdlib実行とscope付きClientを接続する。NOEは共通実装を呼び、libraryにNOE既定値の別実装や「private経路だけのfallback」を残さない。

## 2. 機能・設定元・所有

| 機能 | 実体を持つ層 | 設定/方針の選択元 | 所有と終了 | 経路/slice |
| --- | --- | --- | --- | --- |
| JS実行/例外/Loop | noestdlib | hostのProfile | managed Runtimeが一つのLoop/JS状態を所有。advanced Attachはcaller所有 | E1/E2・S2/S5 |
| cwd/env/stdio/loader | noestdlib | 一つのEnvironmentとLoaderFactory | 値snapshot、loader/cache実行別、Bindingsで実行FSを渡し、initializer/元sink/factoryは借用 | E1/E2・S2/S5 |
| Node FS/fs-handle/画像file I/O | noestdlibの共通FS backend | Profile.Modules.FSのRoot/ReadOnlyと実行cwd | file/handle/IOは実行scopeが回収、画像adapterは借用 | F1/I1/I2・S3/S7 |
| Node/native command | noestdlib | 共通Commands/Environment/helper選択 | processscopeが子孫/pipe/Waitを所有 | P1/P2・S3/S4 |
| socket/signal/stdio/Worker等 | noestdlib | Modulesの独立選択 | runtime contextに従いstop/join。Worker stdinは空 | N1・S3/S5 |
| provider/catalog/decision/image生成 | agent/model | hostの明示Options/custom provider | 要求context、共有HTTP clientは借用 | M1・S1/S6/S7 |
| credential/OAuth/auth save | agent/model・auth | path選択はhost/NOE | 処理がfile/lockを所有。更新先固定、auth bindingはruntime context | M1/M2・S1 |
| Host/Client/会話/queue/session/結果 | agent coreと公開agent | Go callerまたはscope付きJS Client | Hostが全体、Clientが自分のOperation/所有agentを回収 | A1・S6/S7 |
| 再委譲/group/finish/final | agent coreの共通受付 | 既存の明示権限/上限 | sourceがGo/JSでも同じscope/所有検査。JSへadminを渡さない | A1/A2・S6/S7 |
| hookとmodel/tool操作 | agent core/bridge | handler登録、hook用Client | hook要求はhandler終了で取消し、子の所有はrun | A2・S7 |
| instruction | 合成/固定はagent core、directory規則はNOE | hostのsource/Policy、instance.system | 初taskでsource実行/固定、queue継続、新子で再実行 | A3・S8 |
| ログの意味の復元 | agent/log | callerが渡すdata/readerとquery | 入力の所有はcaller、file/Host/pollを作らない | L1・S8 |
| ログfile取得/追記待ち | NOEまたはJS呼出し側、I/O機構はstdlib | callerのpath/待機選択 | caller/runtimeがfileと待機を回収 | L2・S8/S9 |
| 画像添付/生成結果 | image処理とscope受付はagentlib | scoped model Client、file helperは実行FS | bytesはGo保持、添付候補は現行run_js owner | I1/I2・S7 |
| control管理操作 | agent HostのGo管理入口 | Go caller/NOE control | root admin操作、JS Clientとは別 | C1・S6/S9 |
| control HTTP/登録file/defaults | noe | CLI/NOE profile | NOE adapterがserver/認証token/登録fileを所有 | C1/C2・S9 |
| event/Trace/保存/表示 | 意味はagentlib、writerは下位、保存/表示はnoe | hostのsinkとCLI option | producer終了後にCLIがflush/close。元writerは借用 | V1・S7/S9 |
| docs | 下位の共通data/検索、stdlib導入、CLI表示 | 参照は常時可、本文は説明する層が保守 | embed済みdata、導入済みAPI一覧とは別 | D1・S5/S10 |

## 3. 現行入口と移行経路

source名は現行の同定用。新file名を固定するものではない。各sliceは台帳ID・新しい到達先・旧経路の撤去・受入を実装記録へ対応付ける。

| ID | 現行入口/結合 | v2の到達先/変更 |
| --- | --- | --- |
| E1 | stdlib.Install/Config、CLI internal/runtime | Profile→managed NewRuntime/Executeまたはadvanced Attach→同じBindings view。実行FS/VM/Loopを明示し、Initializeが各実行に独立bindingを導入/回収 |
| E2 | task.go/broker_tools.go→Runner→別NOE、別Load/ChildImportMap | core→内部Executor→jsexec→実行別loader/stdlib→root scopeに束縛したClient/bridge。root Host/registryを再生成せず、予約stdioをコピーに束縛。NOEのinitializerでauth/logを導入 |
| F1 | internal/stdlib/fs.goとnativeio/file.goの別path/Root | 同じFS backendへNode/path解決/positional handle/画像file helperを接続。Root/ReadOnlyが入口で分かれない |
| P1 | stdlib execFile/exec/spawnSyncのsync/async | 共通Environment/resolver→processscope。Node形式の結果/error契約は維持 |
| P2 | nativeio/noe:process、JSがprocess.env/cwdへ依存 | P1と同じ設定/監督。Process objectを要求せず、host Environmentをbindingが渡す。Web Streamsの結果契約は維持 |
| N1 | IO一括導入がFS/socket/signal/processを全導入 | 独立Modulesへ分解。Web純粋APIは常時、必要な実装だけ導入。Workerにも同じprofile規則 |
| M1 | Host.NewHost内registry/decider/imager、list_models/decide/image RPC | agent/modelへ機構を移し、HostなしGo/JS利用を提供。agent内はscope wrapper→同じservice。credential/SDK処理を二重化しない |
| M2 | agentbridgeのAuth helperがHost/Operationに依存 | agent/authの単独binding→runtime context/Loop→既存検証/保存。modelのOAuth更新と保存先規則は再利用 |
| A1 | Go InstallとJS API→内部HTTP handler | 公開Host/NewClient/JS Attach→同じcore受付。spawn/run/queue/wait/read/steer/cancel/stopを直接呼ぶ。HTTP broker/Connection/tokenを撤去 |
| A2 | hook専用request、model/tool/finish/imageOwnerのRPC | scope付きClientで共通受付。hook/task/tool/adminの役割を保持。hookは登録specifier/indexをtask loaderで解決しcanonical IDで同定。finish/画像/再委譲の検査をadapterへ分散しない |
| A3 | InstructionDirsとagent内HOME/XDG/MD読込 | InstructionSource→初task合成/固定。NOEがdirectory source/Policyを供給。Hostにpath規則を残さない |
| I1 | read_image→root RPC→root cwdからOS ReadFile | 実行FS backendで読む→検証→scope付きGo接続で画像候補を登録。JSにbytesを返さず、rootで再openしない |
| I2 | GenerateImage要求のoutput→model→root OS SaveFile | model serviceはbytes生成のみ。JS file helperが実行FSで事前設定検査/atomic save。agent内はgroup wrapperを通す |
| L1 | observe_agent(logPath)→Operation.ObserveLog、hookからobservation直呼出し | agent/logのdata解析。通常/hookともfile opener/専用ログOperationが不要。JSの旧file target/waitを廃止 |
| L2 | CLI observe structuredとfollow表示のfile処理 | NOEがOS file/size snapshot/poll/待機を所有し、agent/logの意味解析か共通event rendererへ渡す。CLI表記は維持 |
| C1 | broker control受付、DefaultControlDir、runtime/CLIの登録 | NOE側の初agent受付event→遅延controller→server/発見file→Host管理操作。失敗はNOE実行errorとして取消し/join。初回操作rejectからの変更とready前の非同期性を明記。内部RPC/admin tokenをJSへ渡さない |
| C2 | agentOptions/runtimeのdefaults、NOE path/env探索 | internal/noeprofileへ一元化。instruction source/model Options/helper/env filterをNOEが選択。ControlDirをHostへ逆注入しない |
| V1 | Observe/Trace→CLI events/render | event因果関係/結果/usageを維持。内部RPC診断は操作診断へ更新し、保存pathと表示はCLIだけ |
| D1 | Docs boolとroot/子/Workerの案内分岐 | noe:docsを常時導入。共通本文/metadata/error検索、agent/SDKをstdlibへimportしない |

### Go/JS/NOE管理が共通受付へ届く条件

Go Host/ClientとJS Clientは同じdomain型/受付を使用する。現行handlerだけにあるgroup枠、所有、画像・finish検査、operation cancel、error分類を取り残さない。root管理操作が全体を操作できることと、JSの所有scopeを区別する。Clientは自分のscopeを指定し直せない。

旧内部HTTP transportを削除しても長いprovider/tool待機へfetch既定timeoutを持ち込まない。Operation gate/ctx/joinとfinal commitの試験を直接Clientへ移す。HTTP固有の認証文字列を検査するだけの旧試験は撤去理由を記録し、対応する所有検証は残す。

### file便利機能と認証の違い

画像file helperは実行FS、ログfile取得は呼出し側へ移るため、以前のagent.Filesを廃止する。credential/OAuthは明示した保存先を使うmodel/authの機構であり、実行FSのRootへ混ぜない。module loaderもhostの明示factoryから供給する。OSアクセスを名前だけで一律禁止せず、どの設定/入口で選ばれたかを確認する。

## 4. 経路別受入

| 確認 | 成功条件 | slice |
| --- | --- | --- |
| stdlib単体 | 外部module、NOE binary/configなし、SDK/agent/CLIへimportしない。独立Modulesとcancel/回収 | S2〜S5/S10 |
| model/auth/log単独 | agent Host/instance/内部brokerなしでcatalog/decision/画像生成/認証保存/ログ解析。独自credential path、借用HTTP、unsupported分類 | S1/S8/S10 |
| Go agent入口 | 入口JS runtimeなしでSpawn→Queue→Wait/Read→Cancel/Stop。Host共有の別Clientが生存し、所有scope終了では自分のagentを回収 | S6/S10 |
| JS agent実行 | 親run_js/子/queue/hook、直接Client、再委譲/group、session/cache、finish/結果原文/画像owner/取消し | S7/S10 |
| file/command設定到達 | Node/native/imageのFS一致、Root/ReadOnly/symlink/Windows、commandのEnvironment/名前解決/直接-helper/子孫回収 | S3/S4/S7 |
| source/loader | 初task/new childでsource、queue固定、factory/cache/initializerは実行別、仮想entryの相対importはcwd・file moduleはreferrer、memory/bare hookの同定、source IDはイベント/stack一致 | S2/S5/S8 |
| ログ分離 | 通常/hookからdataだけ解析、全履歴状態とcursor差分、不完全末尾/既存JSONL、CLI follow/json/wait、file pollingの取消し | S8/S9 |
| NOE control/profile | CLI run/eval/controlとdefault場所・event保存・env filter、controlの遅延activation/ready・app失敗/終了joinを確認。libraryに登録やNOE探索なし | S9 |
| docs/移行/撤去 | 常時docs、本文/metadata/error/型/例一致。旧Files/Docs/InstructionDirs/Connection/RPC/agent process backend/aliasの未到達 | S10/S11 |

sliceごとの対象試験と独立reviewを終えた候補で、最後に総合テストを行う。全API組合せの件数や検索語の禁止を目標にしない。共有service・scope・設定元が実在する全対象入口へ届くかで判断する。

## 5. 現在の状態

この改訂は計画段階であり、製品の分離完了や動作検証成功を意味しない。通常reviewと批判的reviewで改訂差分を確認し、採否/修正/必要な限定再確認を記録して停止する。旧レビューの判定はこのAPI改訂へ流用しない。
