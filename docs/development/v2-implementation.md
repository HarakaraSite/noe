# v2実装記録

2026-10-21。利用者の「計画に従って進めましょう」により、計画段階の停止を解除し、[承認済み11スライス](v2-agent-runtime-implementation-plan.md)の実装・対象試験・独立コードレビュー・指摘修正、最後の総合検証へ進む。基点は `89c7069`。release/pushは今回の範囲に含めない。

API契約は[API再設計](v2-api-redesign.md)、責務と経路は[境界台帳](v2-responsibility-boundaries.md)を正本とする。旧経路は移行比較のため内部に一時保持し、S11までに撤去する。未追跡の利用者ファイルは変更しない。

| Slice | 状態 | 担当・変更・検証・レビュー |
| --- | --- | --- |
| S1 M1/M2 | 完了 | model/providerはimplementer、auth/bridgeは親。対象試験・追加race・別module consumer合格。v2_runtime_reviewのR1を修正・限定再確認済み |
| S2 | 完了 | Profile/Environment/Bindings、実行FS viewと独立auth binding。対象package/consumer/race合格、R1/R2修正・限定再確認済み |
| S3 | 完了 | FS/I/O共通基盤。対象unit/race/FS結合合格。Windows R1/R2修正・同reviewer限定再確認済み（Windowsはcompileのみ） |
| S4 | 完了 | 直接/明示helper、Node/native共通監督、dispatcherとNOE組立。対象unit/race/consumer合格、R1修正・限定再レビュー済み |
| S5 | 完了 | managed実行とscope所有。R1/R2/R3修正、unit/race/consumer合格、限定再レビュー残件は親が回帰試験で確認 |
| S6 | 完了 | Go Host/Clientと共通受付。R1〜R4修正、unit/race/consumerと限定再レビュー済み |
| S7 | 完了 | standalone/scoped model bindingと実FS画像、task/tool/子/queue。R1/R2修正、unit/race/SDK wireと限定再レビュー済み |
| S8 | 完了 | instruction facts/source/NOE dirs と純粋log解析/binding。R1修正、unit/race/consumerと限定再レビュー済み |
| S9 | 完了 | NOE組立/control。R1/R2修正、対象unit/raceと限定再レビュー済み |
| S10 | 完了 | API移行/consumer。R1文書修正・限定再確認済み |
| S11 | 完了 | 旧経路撤去・独立review/限定再確認、最終総合gate合格 |

対象試験には短い `/var/tmp` 下のTMPDIR/GOTMPDIRを使い、外部NOE_AGENT_CONNECTIONを除く。実providerはfixtureで解消できない具体的な疑問が残った場合だけ使用し、その問いと範囲を記録する。

## S1の経過

親担当：`agent/auth` のGo helper、`internal/authbridge` の独立した非同期binding、旧agentbridgeからそのbindingへの委譲。既存のcredentials検証/lock/atomic保存を再利用する。公開 `auth.Bind` はS2の共通Bindingsへ接続し、raw runtime/Loopを別の恒久公開入口にはしない。

`CGO_ENABLED=0 go test ./internal/authbridge ./internal/credentials ./internal/agentbridge ./agent/auth -count=1` は合格（`/var/tmp/noe-v2-s1-auth-test.log`）。authbridge/credentials、および旧agentbridgeのrace検査も合格。最初のbridge compileは同時実装中providerの未定義methodで失敗し、providerの保存完了後に再実行して合格した。製品上の失敗としては扱わない。

実装担当：`agent/model`、providerの明示registry経路とfixture、`internal/image`の生成専用検証。旧registryはNOE既定経路の比較用として維持する。OpenRouterの旧KeyFile overrideが選択済みCredentialsを再上書きする経路を止め、相対path/優先順位のfixtureを追加した。

`go test ./internal/agent/provider ./internal/image ./agent/model -count=1` と別module model consumerが合格（`/var/tmp/noe-s1-logs/target-tests.log`、`external-consumer.log`）。`CGO_ENABLED=1 go test -race ./agent/model ./internal/agent/provider ./internal/authbridge ./internal/agentbridge -count=1` も合格（`/var/tmp/noe-v2-s1-race.log`）。4組込みadapterの既存local HTTP fixtureを再利用し、追加試験は明示設定・未使用認証先・custom unsupported・画像のOutput不要を確認する。OAuth同fileのrefresh/lock/saveは既存credentials fixtureで確認した。

公開model/authのproduction import closureにcore agent、agentbridge、NOE runtime/CLIがないことを確認（`/var/tmp/noe-v2-s1-deps.txt`）。独立レビューではこのS1差分を固定し、S2以降や旧内部経路の撤去を対象に含めない。

通常コードレビューはS1-R1/P2を1件報告：旧NOE_CONFIG_FILEで明示したfile不存在と空の設定fileを無視して組込み定義へ戻る退行。採用し、設定選択・read未実施・read成功を区別して修正する。その他の必須指摘なし。修正後に該当fixtureと限定再レビューを行う。

R1修正後、provider全体とmodel/auth/旧bridgeの再確認が合格（`/var/tmp/noe-s1-logs/r1-provider-tests.log`、`/var/tmp/noe-v2-s1-r1-recheck.log`）。同reviewerが変更とR1だけを限定再確認し、解消・追加必須指摘なし・S2へ進行可と判定した。実provider/CLI総合はこのsliceでは実施せず、後続の総合検証へ残す。

## S2の経過

同implementerをProfile/Environment/Bindingsの型とadvanced Attachの実装に再利用する。親は実際のFSを表す共通backendと独立auth/model bindingの接続を担当する。S5でmanaged実行を完成する前に、S2ではcaller所有のVM/LoopへAttachしてinitializer/loader/viewを検証する。

親の`internal/filesystem`とNode FS接続では、cwd固定・Root/ReadOnly・非存在tailの親symlink検査・atomic保存・caller/実行ctxのFS viewを共用する。既存FS対象試験、新backend試験とraceは合格（`/var/tmp/noe-v2-s2-filesystem.log`、`/var/tmp/noe-v2-s2-fs-race.log`）。独立`auth.Bind`を共通viewへ接続し、利用者loader＋initializerの公開package試験も合格（`/var/tmp/noe-v2-s2-auth.log`）。ProcessInfoを選ばない場合のnode:pathにもEnvironmentのcwd/envを渡す。

後続sliceで完成する接続：S3のnative fs-handleへの同Backend配線・commandsの同Env/PATH、S5のmanaged評価/context stdio、advanced Attachで導入するlower coreのnetwork/Worker等の取消しとjoin、Workerのfresh loader/entry準備・ProcessInfo非選択時のargv/env override。S2の型/owner初期化導入だけで、これらの受入を完了扱いにしない。model binding本体はscoped model client/画像と共用するS7で導入する。

担当側のS2 package試験（internal/stdlib、noestdlib、nativeio、jsexec、公開stdlib）と別module stdlib consumerが合格（`/var/tmp/noe-s2-profile-logs/packages.log`、`public-consumer.log`）。Profile fixtureと実Worker fixtureのraceも合格（同directoryの`race.log`、`worker-focused.log`、`worker-race.log`）。親のauth接続/旧agent consumer/旧bridgeと新FSのunit/raceも合格（`/var/tmp/noe-v2-s2-parent-integration.log`、`noe-v2-s2-parent-race.log`）。

Worker metadataはhelperのcopy内容だけをなぞる試験を使わず、実WorkerのisMainThread/threadId/parentPort、initializer Worker判定、共有Max:1での再Worker拒否、argv/env、process.exit(7)を確認する。ProcessInfo非選択時のworker env/argv供給もS2で接続済み。S5ではentry/loader/lifetimeを統合して確認する。stdlib production import closureにagent/SDK/CLIへの逆依存なし（`/var/tmp/noe-v2-s2-deps.txt`）。

通常レビューはS2-R1/P2（legacy Workerのoptions.env省略時に親Process.Envが失われる）を報告。採用して元担当へ限定修正を戻した。legacy argv[0]の退行は親の初期推測をreviewerが否定したためfindingに含めない。ProfileのProcessInfo非選択時も明示Environment.Args[0]を使う点は同じ共通環境契約に揃え、qjsという試験期待を仕様根拠として固定しない。

親発見・reviewer確認のS2-R2/P2（auth.Bindへ零値Bindingsを渡すとnil contextでpanic）は、初期化済みview検査に修正。公開APIの該当試験とauth対象全体は合格（`/var/tmp/noe-v2-s2-auth-r1.log`）。その他の必須指摘/任意提案なし。R1修正・対象試験後、両指摘と変更だけを限定再確認する。

R1修正後、stdlib全体・jsexec・対象raceが合格（`/var/tmp/noe-s2-profile-logs/worker-env-package.log`、`worker-env-jsexec.log`、`worker-env-race.log`）。reviewerがR1/R2と対応変更だけを限定再確認し、両件解消・追加必須指摘なしと判定。S2を完了しS3へ進む。

## S3の経過

同implementerがNode/native commandの共通cwd/env/PATH/shell resolverを担当する。親はnative fs-handleへ同じfilesystem Backendを渡し、Root/ReadOnly/symlinkがNode FS/Go viewと一致するよう接続する。Profileのmodule独立選択はS2で導入済み、S3では各backendへの設定到達を検証する。process監督方式の全面変更はS4。

親のnative FS接続は、Node FSと同じbackendでvirtual pathを解決し、r+はopen前にReadOnlyを拒否する（native error code `ERR_READ_ONLY`、型/docs一覧はS10で整合）。Profileのnative IO cwdをphysicalへ揃え、command cwd overrideにはbackendのdirectory resolverを渡す。Node/native/Go view間の実際の読み書き一致と、Root外symlink/ReadOnly拒否の結合試験・raceが合格（`/var/tmp/noe-v2-s3-filesystem.log`、`noe-v2-s3-filesystem-race.log`）。

command側は`internal/commandenv`へ共通resolverを導入。Profileは明示Environmentだけをdefaultにし、nil env overrideは継承・{}は空。legacy Installだけprocess.env overlay/null-deleteを一時adapterで維持し、S11撤去対象とする。shellでもAllowは従来どおり起動するshell名＋argsを検査する。

commandenv/stdlib/nativeio/noestdlib/jsexec全unit、commandenv/nativeio対象raceが合格（`/var/tmp/noe-s3-command-logs/target-packages.log`、`race-target.log`）。Windows向け5packageのtest binary compileが合格（同directoryの`*-windows-build.log`）、Windows実行は未確認。レビュー候補のGo sourceをtree `b34e1f150c5a4d962a542ecce7a3ebe5f12959cb`へ保存（`/var/tmp/noe-v2-s3-review.tree`）。private indexを使用し、実index/HEADと利用者untrackedは変更していない。S3の固定差分を独立レビュー中。

S3レビューはWindowsのR1/P2（selected PATHEXT失敗後のhost fallback）とR2/P2（volumeなしrooted pathをcwd配下へ誤変換）を報告。両件を採用し、同implementerがcommandenvとRootなしFS resolverを修正。Linux6package合格、Windows2package compile合格（`/var/tmp/noe-s3-command-logs/s3-r1r2-target.log`）。修正tree `afb8ef8d868abbd9cc183543c10c757128d040e1`を同reviewerが指摘限定で再確認し、両件解消・追加Blocker/P1なし。Windows実機は未実施。S3を完了しS4へ進む。

## S4の経過

同implementerはNode sync/async commandのowned process backend移行とnativeへのSupervisor設定到達を担当し、親は直接/明示helper起動、library init撤去とmain dispatcher、NOE組立を担当する。generic直接起動はアプリinitを再実行せず、POSIXのowner SIGKILL回収は明示helperで提供する。

S4のgeneric直接起動はunreaped leaderをgroup回収まで保持し、signalとreapをgateで直列化する。POSIX helperは明示したpathだけを使い、library initを撤去してmain dispatcherへ移した。NOEが自己helperを指定し、Node/native両方へ到達させる。Nodeはowned childと並行pipe pumpをjoinしてから結果確定し、出力上限のdiscard/drain仕様を維持する。

対象5package unitとstdlib/nativeio race合格（`/var/tmp/noe-s4-command-logs/target-packages.log`、`race-target.log`）。親のprocess単体・race、正常exit時の孫/pipe保持回収、外部module consumerのinit回数（直接1/self2/別helper1）、別helper/NOE helperのowner SIGKILL回収、private argvをlibraryが捕捉しないこと、NOE runtime/CLI結合が合格（`/var/tmp/noe-v2-s4-logs/`）。Windowsはprocessと担当3packageのcompileのみ、実機未実施。S4 sourceを固定して独立レビューへ提出する。

S4レビューはR1/P2（誤helper/初期化待ちでdispatcher未到達のままstatus readが取消し不能）を報告。採用し、targetを起動させるConfig送信前にready通知を要求するhandshakeを追加。ready前はhelper group停止とstatus解除、ready後は既存のcontrol EOFによるtarget回収を維持し、startup取消しcallbackをjoinしてからreapする。dispatcher前で停止するfixtureと、既存startup/owner死亡/normalexit/外部consumerの対象試験が合格（`/var/tmp/noe-v2-s4-logs/r1-target.log`）。限定raceと再レビューで確認する。

S4-R1の対象raceも合格（`/var/tmp/noe-v2-s4-logs/r1-race.log`）。同reviewerが修正tree `cdab1c45c807555483f074068c713d8ec520aaff`を指摘限定で再確認し、解消・追加Blocker/P1なし。S4完了、S5へ進む。

## S5の経過

親はmanaged Runtime/stdio/loaderと公開入口を担当し、同implementerはlower stdlibの実行scopeとowned producer回収を担当する。borrowed VM/Loopはadvanced Attachの終了で閉じず、実行scopeとmanaged ownerの役割を分ける。

S5では`stdlib.NewRuntime/Execute`とowner `Run/Close`、他goroutineからの`Cancel`、GoだけのResult/診断を追加。root Promiseの拒否/未捕捉はidle intervalに待たされず止まり、process.exitはhostのos.Exitを呼ばない。stdioは導入前にcontext-bound facadeへ揃え、consoleが無視したIO errorも記録してdriveを止める。元streamは閉じない。Workerはinstallerからowner cleanupとentry preparerを受け、initializer→fresh factory→entry評価、終了はinitializer→scope→adapter→Loop/VMの順に回収する。

lower ExecutionScopeはone Loopに一度だけ接続し、cancel→owned cleanup→producer joinを実行する。public Loop.Postはcallerの仕事を保ち、stdlib内部だけscope-aware deliveryを使う。advanced AttachのCloseはVM/Loopを閉じない。native spawnもJS配送前にraw childをGo側で所有し、配送されずにbindingが閉じてもchildとtimeout producerをjoinする。

lower全unit/race合格（`/var/tmp/noe-s5-model-target.log`、`noe-s5-model-race.log`）、public Post契約の限定修正後も全unit/race合格（`/var/tmp/noe-s5-post-target.log`、`noe-s5-post-race.log`）。親側はjsexec/公開stdlib/auth/noestdlib/nativeioのunit/外部consumerが合格（`/var/tmp/noe-v2-s5-logs/parent-integration.log`）、jsexec/nativeio/authのrace合格（`parent-race.log`）。CPU/同期stdio/native stdio取消し、元sink継続、満杯pipe、IO error、Promise/exit/fresh module/Worker loader/empty stdin/initializer cleanup、advanced fetch終了とcaller Post継続、未配送native child回収を確認。旧NOE runtime/CLIも合格（`noe-legacy-integration.log`）。Windows jsexecはcompileのみで実機未実施。stdlib import closureにagent/SDK/CLIなし（`stdlib-deps.txt`）。

S5候補を固定して独立レビューへ渡す。付属file loaderの公開便利入口と文書例の最終整合はS10のconsumer/API移行で仕上げる。Profileの明示NewLoaderと実行別factory経路自体はこのsliceで検証済み。


S5レビューのR1/P2（ordinary MessagePort配送とLoop保持がscope終了後も残る）、R2/P2（wrapper経由JS errorがVM終了後まで残る）、R3/P1（診断のJS conversion/getterで取消し不能）を採用し修正。MessagePortの実transfer＋未drive配送で回収を確認し、Worker parentPortはshutdown(keep)まで保持してexit通知を維持する。診断は実行context内でGo値へ切り離し、detail getterを呼ばない。Cancel/期限とpartial setupの回帰試験を追加した。

修正後の6package結合とjsexec/nativeio/auth raceが合格（`/var/tmp/noe-v2-s5-logs/fixed-integration.log`、`fixed-race.log`）。lower全unit/raceも合格（`/var/tmp/noe-s5-message-full.log`、`message-race.log`）。一回の限定再レビューでR1/R3とlive JS残留の解消を確認。R2のnative sibling causeがwrapper＋Join内で落ちる残件を指摘したため、単一Unwrapも再帰して保持するよう修正し、実VM終了後のerrors.Is/As/診断の試験を含むjsexec全体が合格（`r2-sibling.log`）。親がこの限定変更と結果を確認しS5完了。Windowsはcompileのみ。

## S6の経過

同implementerがinternal/agentの業務処理をHTTPから共通Go serviceへ分離し、親は公開Host/Clientとin-process実行adapterを担当する。S6のGo入口の受入を実際のtaskで検証するため、S7予定のexecutor/JS接続の最小部分を先に組み込む。S7ではhooks/images/scoped model等の残りを完成する。旧HTTP/runnerは既定切替後にS11で撤去する比較経路としてのみ残す。

S6で公開Host/NewClientとscope付き操作を追加し、Hostを借用するAttach、task/tool用in-process executorを接続。専用bounded captureはcontext Writerを実装し、stdio templateを変更しない。start/model/context/model-step/final/finish/hooks/run_js/decisionとtool記録の業務処理をHost serviceへ抽出し、旧HTTP adapterとDirect dispatcherが同じ受付を呼ぶ。Go入口ではbroker/Connection/NOE executableを使わない。画像file helperのDirect接続はS7。

親の公開Go fixtureでspawn/queue/wait/retained UTF-8 result/answerBytes、session固定とrun更新、pending/running取消し＋他queue維持、wait取消しが観測だけであること、binding終了後の他Client、借用model serviceの取消しとjoinが合格。独自bare/memory hook＋相対importのcanonical同定、queue taskごとのfresh factory、別module GoHost consumerも合格（`/var/tmp/noe-v2-s6-logs/parent-integration.log`、`parent-result.log`）。親側agent/bridge/jsexec全race合格（`parent-race.log`）、core全unit/raceとDirect tool scope fixtureも合格（`model-unit.log`、`model-race.log`）。pending取消しのpublicationもinstance ownerがjoinし、別taskのfinal commitを理由に誤拒否しない回帰を確認。S6候補を固定して独立レビューへ渡す。


追加で公開Hostのtrusted管理から一覧/queue/stop、別Clientからのobserve/stop拒否、片方のClient終了後に他Client所有instanceが継続することをraceで確認（`parent-admin-race.log`）。S6レビュー対象sourceは固定し、この追加試験は親の追加証拠として扱う。

S6独立レビューは必須4件を報告し全件採用：R1/P2はDirect catalogがHost contextだけで待つためrequest/Client取消しで止まらない、R2/P2はcancel flagの未同期読取とfinish後run除去によるfinal確定済みtaskへの取消し受付、R3/P2はClient.Closeが先にoperation登録を除去してHost.Closeのjoin対象から落ちる、R4/P1は公開Go SpawnのTaskConfig.MarshalJSONがExecutionLimitsを落とし指定上限を失う。

R4はTaskConfigの登録入力encodingをConfigと共通化して全fieldを保持し、instanceのtask実行用configJSONを作る箇所だけでgroup limitsを除くよう修正。公開GoでProviderCalls=0のmodel未呼出し、AgentStarts=0の受付拒否が合格、agent/core全unitも合格（`/var/tmp/noe-v2-s6-logs/r4-packages.log`）。R1の公開Spawn request取消しfixtureも合格（`r1-public-request.log`）。R1/R2/R3の残りは元implementerが限定修正・回帰確認中。

R1はprovider確認とcontext model limitsのcatalogへrequest/op合成contextを伝播、取消し時は受付を中止。R2はterminal outcomeをHost.mu内で確定し、final gateをcommandにも保持してfinish後のcleanup中も取消拒否。R3はclosing operationを別join集合で保持し、Host.Closeがrequest/instance/既closing Close終了をjoinする。逆順（Host.Close先行）も公開Clientとroot bindingだけCloseClientでHost joinを待ち、task/tool内部Closeは循環待ちを避ける入口を維持する。

4件の修正後、公開Go/bridge/executor/core/jsexecの結合と公開Go/bridgeのraceが合格（`/var/tmp/noe-v2-s6-logs/fixed-integration.log`、`fixed-parent-race.log`）。coreのfinding回帰全体もunit/race合格（`s6-findings-unit.log`、`s6-findings-full-race.log`）。固定修正treeを同reviewerへ指摘と対応差分に限定して再確認する。


同reviewerが修正tree `b9fa468e0c4af893fced14807bd3deee6ad9bb30`を一回の限定再確認し、R1〜R4全解消、追加Blocker/P1なし、S7進行可と判定した。S6完了。

## S7の経過

親はHostなしのmodel bindingと同じbindingをscope serviceで使うagent bridge接続、実行FS画像helperを担当。同implementerはcoreの画像owner/data接続、JS取消し/停止のAPI分離、実際の親tool→子→queue等を担当する。S6固定差分の確認完了後に開始。NOEの既定実行切替はS9。


S7のstandalone model bindingは`model.Bind(ctx, bindings, service)`を追加し、`noe:model`のgenerate/list_modelsと、既存noe:decision/noe:imageの便利入口を提供する。同じ内部bindingへagentはop.Models()とWithRequest/ReadImage/ObserveImageSaveを渡す。file保存・observerまでrequest gateを保持し、画像bytesはJSへ返さない。生成後保存失敗のImageErrorはprovider request IDとusageを保持する。生成完了とfile保存をimage.saved/image.save_failedで分け、group usageは再加算しない（S8 parser/S10移行文書で整合）。

親側のHostなしmodel/decision/catalog/画像保存、FSなし/ReadOnlyのprovider前拒否、保存失敗時のusage/宛先維持、agent Attach先とHost templateで異なるRootを使うread/writeとモデル画像入力が合格。公開agent/model/bridge全体とraceも合格（`/var/tmp/noe-v2-s7-logs/parent-integration.log`、`parent-race.log`）。画像のgroup providerCalls=3/imageCalls=1、既知usage合算=10を確認し、二重加算を検出する。根拠fixtureがusageを返さない最初のmodel応答を誤って加算した試験期待だけを修正した。

担当側は実GoHostの親→parallel tool→子→queue/cancel/stop、再委譲許可の境界、fresh VM/loader、initializer/capture/空入力、stdout/stderrのUTF-8 head/tailを確認。対象fixture反復とrace、core全raceが合格（`agent-runtime-repeat.log`、`public-runtime-race-final.log`、`core-race-final.log`）。auth/log固有initializerはlog導入後S8で確認する。旧NOE runtimeの既定経路はS9まで維持するため、新JS破壊的APIと旧RPCの移行途中比較はこの時点の完了条件に含めない。

S7独立レビューはR1/P2（binding context取消し後でもserviceがnil errorを返す／配送待ち中に取消すと成功になる）とR2/P2（不正な入力画像を生成応答用codecでresponse分類してしまう）を報告、両件採用。R1は配送時のbinding context errorをrejectへ反映し、serviceが取消し後に正常response,nilを返す実fixtureへ強化。R2は共通codecに入力bytes検証を追加して旧ReadFileとinput分類を共用し、実FSの壊れた画像をrun_jsで読む回帰を追加した。対象unit/race合格（`r1-model-cancel.log`、`r2-image-input.log`）。

単独model.GenerateではツールなしのResponses要求が実際に到達可能になるため、旧agent loopでは不要としたtools:nullを空配列へ修正。共有Responses builderの1箇所だけを変更し、実OpenRouter SDK経由のlocal HTTP fixtureでwireのtools:[]と正常応答を確認した（`standalone-no-tools.log`）。SDK/engine版は変更しない。S7全対象を修正後raceで確認し、同reviewerへ指摘＋この追加変更だけを限定再確認する。


同reviewerが修正tree `c21884d57cffc22fd341fba3453908259cef20a0`のR1/R2とtools空配列変更を限定確認し、全解消・追加Blocker/P1なし・S8進行可と判定。修正後のagent/model/bridge/core/provider/image全対象race合格（`fixed-race.log`）。S7完了。

## S8の経過

同implementerはinstanceごとのinstruction snapshot/cacheとfacts/Policy分離、親はNOE directory sourceとdata入力log parser/Go/JS bindingを担当する。旧NOE file observe adapterは新parserへ向け、path/follow/waitをlibrary解析入口へ持ち込まない。

## S8の経過

Directのinstructionsを汎用Facts→注入Policy→instance単位のSource→instance.Systemへ分離した。Sourceは最初のtaskで一度だけ評価し、本文・エラーともqueue間で固定する。NOE固有のinstruction directory解決とPolicyは`internal/noeprofile`へ置き、directoryの場所を初回固定、本文は新instanceごとに再読込する。旧NOE組立への切替とlegacy helper撤去はS9/S11。

保存ログの再構築を`internal/loganalysis`へ移動し、公開`agent/log.Analyze`（Reader）と`AnalyzeBytes`、独立`noe:agent-log.analyze_log` bindingを追加した。file open/固定size snapshot/poll/waitはNOE側`internal/observation`へ残した。cursorはEventsだけを絞り、状態は全入力から再構築する。未完の末尾行を無視し、既存saved log形式を維持する。image.saved/save_failedも投影する。

旧testのcontext.Err呼出回数への依存を除き、実Readerが読込中にcancelする契約試験へ置き換えた。純粋stateの試験はparser側へ移動し、保存file/pollの試験はNOE adapterへ残した。root/task hook/toolで同じinitializerが独立auth/log bindingを供給し、各runtimeの終了でcleanupされる結合試験が合格した。Sourceのqueue再利用・合成順・失敗cache・並行とcancel joinもfixtureで合格。対象ログは`/var/tmp/noe-v2-s8-logs/`。

S8独立レビューはR1/P2を報告：非nilでもmessageが空のSource errorがtask.jsのtruthiness判定をすり抜ける。失敗フラグを本文から分離し、既存error fixtureを空/非空の両caseへ拡張して、初回/queue失敗・Source一度・hook/provider未呼出しを確認した。限定unit/race合格（empty-source-unit.log、empty-source-race.log）。S8全対象unit/raceとWindows public log compile、純粋parser/publiclogの依存境界確認も合格。修正後の限定再レビューへ進む。

S8-R1を同reviewerが限定再確認し、解消・追加Blocker/P1なし・次へ進行可と判定した。S8を完了しS9へ進む。

## S9の経過

NOEの既定Executeを、公開stdlibのmanaged実行とGo Hostの組立へ切り替えた。`internal/noeprofile`がCLI権限、同一環境snapshot・4キー変数と旧Connectionの除外、明示self helper、実FSを読むfresh loader、auth/log initializer、遅延model設定選択を供給する。task/tool templateのstdioは空にし、executor所有captureへ渡す。旧private runtime/Options変換はS11撤去対象。

`internal/app/control`へ人間用HTTP・token・discovery・CLI clientを移し、公開Host管理APIだけを呼ぶ。agent.acceptedを受けて非同期activationし、readyを通知する。Accept直後にCloseされても初回activation errorを保持し、NOE全体の取消し・診断へつなぐ。単純計算とcatalogではcontrolの場所を探索しない。旧Connection環境の解析はNOE既定から外した。

既存runtime/CLI検証で旧JS interrupt/saved-log APIの期待を移行した。失敗したSourceをqueueで再試行する旧期待は、失敗cacheを確認してから新instanceを起動するfixtureへ改めた。context予算fixtureは汎用Facts＋NOE Policyの実際の長さから算定する。任意のtest reader/writerにはcancellable I/O契約を実装し、同じfixtureのエラー/borrowed性を保つ。process helperは新しいenv置換契約に従い、必要な環境を明示する。

新CancelTaskでは要求元の因果情報が受付eventから欠けていたため、interrupt.acceptedのtarget:task/commandId/origin/causedByを保持し、terminal publicationを受付eventの後にするgateとjoinを追加した。hook eventへmanaged commandIdも供給する。root/task/toolの実行とHTTP controlのcancel/stop、自己cancel/stop、hook取消しと子回収の対象fixtureは合格。

最初の広い検証は旧RPC/旧JS前提により停止したため、自分のtest processだけSIGQUITでtraceを採取して終了した（assembly-full.log）。旧RPC fixtureはCLI既定切替により比較実行不能となりS11撤去対象。runtimeの新経路全体検証は、画像/decision metadata・task error詳細の退行も発見し、実装担当へ修正を戻した。private raw startを使うgeneric hook fixtureはS11で共通service側へ移行/整理する。これらを未確認のまま総合合格とはしない。

実行失敗のauthenticated report Errorを非zero executor summaryより優先し、reportなしの完了認証errorは維持した。decision.completedへstatusCode/responseModelとusage反映後のexecution情報、image.completedへ生成使用量反映後のexecutionを追加した。custom model serviceへprovider判定を委ね、custom image/decisionがchat catalogへ不必要に依存しないようにした。対象unit/raceが合格した。

CLI/control/noeprofile/publicagentの全unit/race、core Direct/cancel/close対象raceが合格（assembly-cli-unit.log、assembly-race.log、core-direct-race.log）。runtimeではcustom providerが任意IDを扱う契約へ合わせ、validationの不正providerを空文字へ修正した。旧private generic hook fixture1件を除く101件のruntime raceも合格（runtime-selected-race.log）。除外fixtureと旧RPC testはS11で整理し、最後の全体gateに除外を残さない。S9固定差分を独立レビュー中。

S9独立レビューはR1/P2（受付publication前にagent.cancelledが出得る）とR2/P2（fileなしのmemory entryをalias/相対名で再importするとFSを要求）を報告した。R1はagent terminal直前にも確定済みcommandの受付gateを待つ。run除去後の取消しfixtureで両terminalとの順序を確認し、gateを外したoverlayによるnegative controlでは実際の逆転を再現した（r1-negative-control.log、期待する失敗）。R2はOriginへ供給済みentry Sourceのsnapshotを渡し、NOE loaderのcanonical source cacheをseedする。Worker初回source読込時はSource=nilで区別する。既存memory entry試験からfile作成を除き、aliasと相対selfimportを併せて確認し合格。対象unit/race（review-fixes-unit.log、r1-order-race.log、r2-loader-race.log）後、同reviewerの限定再確認へ進む。

S9-R1/R2を同reviewerが限定再確認し、両件解消・追加Blocker/P1なしと判定した。S9を完了しS10へ進む。

## S10の経過

公開`stdlib.FileLoader(imports)`を追加し、NOEのfactoryも同じhelperへ統一した。mapをsnapshotし、各executionのcwd/実FSだけでlocal moduleを読み、Source snapshotでmemory entryをcacheする。Rootを使うWindowsではvolumeを持たないvirtual namespaceを処理するresolverを下位へ用意した（pure fixtureとcompile確認、Windows実機は未確認）。Root/ReadOnly上のread・entry alias/相対selfimport・Worker・次executionのfile更新・未選択FS・外向きsymlinkを実fixtureで確認した。

外部module consumerは旧Install/Configと別NOE processのreference/live経路を、新しいmanaged/advanced stdlibとmodel/auth/log独立binding、Go Hostの実際の親tool→子→queueへ置き換えた。未使用の型aliasを列挙するcompile-only検査と、旧backend前提の手動reference/live fixtureは撤去し、borrowed stdio・initializer cleanup・Worker/FS/fetch・caller Loop継続とsession/run所有を実際の外部呼出しで確認する。commands/helperの外部consumerは既存の専用fixtureを保つ。provider wire/auth/imageの既存package試験も保つ。

embedded referenceへnoe:model/noe:agent-logを追加し、Go埋込みガイド・examples・旧JS cancel/saved-logの説明を最終APIへ更新した。repoにはTS宣言fileがなく、Go型と組込referenceを正本として整合した。agent-serverのstatusはuser履歴、task/tool runtimeは同processであるため到達markerを使い、実OS subprocessのreap検査を残した。両Go例とagent-server統合9件、syntax/local-link確認が合格した。

stdlib/agent全package、apidocs/modulemap/noeprofileのunit/raceと外部consumer全modeが合格（/var/tmp/noe-v2-s10-logs/{integration,race,agent-consumer}.log）。docs/example担当のログも同directoryに保存した。任意browser確認とprovider依存task-loop実行は未実施。旧公開Config/Install/aliasとprivate backendはS11で除く。S10を固定し独立レビューへ進む。

S10初回snapshotの対象pathにexamplesが抜けていたことをreviewerが検出した。親のprivate-index scriptへ追加し、検証済み8tracked example変更を含む候補へ差し替えた。作業tree/HEAD/実indexは変更せず、旧artifact上のexamples不整合を入力漏れとして修正した。

S10独立レビューのR1/P2（NOE固有の先頭英語コメント方針を共通API必須条件と記載）を修正。apidocs unit合格後、同reviewerが3688170の変更のみ限定再確認し解消・S11進行可と判定した。S10完了。

## S11の経過

実装担当はcore/bridgeの旧HTTP・Runner・Connection撤去と契約fixture移行、親は公開旧入口・NOE組立Options・provider ambient探索・runtime/e2e整理を担当する。ローカルGo共通受付、app control HTTP、provider HTTP、process helper IPCは維持する。

S11で公開agent.Install/Config/旧provider aliasとstdlib.Install/Config/grouped IO/Docs切替を撤去。NOEのmodel/control/helper設定をapp所有のnoeprofile.AgentOptionsへ移し、runtimeはstdlib.NewRuntime/Run/Closeと公開Go Hostだけを組み立てる。旧private VM/Loop/Worker/診断実装とimage provider変換adapterを除去した。

旧noestdlibのgrouped Install/Config/extrasも撤去し、明示Profileの内部正規化入口だけを残す。clone/output identity/borrowed writer/docs検証は現入口へ移行。旧Worker孫へのdocs/空stdin/initializer cleanup、およびWorker初期化失敗後の親console/native出力の生存は公開managed fixtureへ移行した（stdlib/worker_lifecycle_test.go、worker-migration.log PASS）。コマンドenv/cwdは新しい置換環境契約のcommand profile/consumer検証へ対応する。

private generic tool fixtureは公開taskでは提供しないraw start+root hook resolutionを前提としていたため撤去した。製品で使う同じexecuteToolWithHooks関数の前後phase/引数と結果の置換/raw情報保持/並行順序/失敗回収はruntime/hooks_phases_test.go等の実task/run_js検証で保持する。旧root/private VMにGo関数を登録していた3fixtureは実NOE managed組立へ移し、例外の元Go causeを失う退行を発見した。root Promise拒否時にownerがdriveから戻ってからengineのAwaitで例外を取り出し、detached診断へ渡すよう修正。runtime全通常試験PASSを確認した。

人間用controlのprivate registration/discovery/prefix曖昧性/redirect/caller cancel/default directory優先の試験を旧coreからapp/controlへ移した。S10で更新した埋込docsに加え、現行のapi/observe/hooks公開ガイドにも残る旧JS停止・保存ログ・task processの説明を更新した。履歴であるrelease/archive/development旧記録は書き換えない。

provider内部はNewExplicitRegistryだけにし、ambient NOE_CONFIG_FILE/XDG/HOME/credential env探索を除去。明示Envとcredential path/優先順、SDK/wire/sessionは維持。NOEが既定場所と環境snapshotを選択する。provider/model unit・race PASS（provider-unit.log/provider-race.log）。tracked spikeの3callerも明示optsへ移行し、実providerを呼ばずcompile確認した（spikes-compile.log）。

E2Eはtests専任実装担当が移行中。旧standalone Runnerだけを検査していたagent_runner_e2e/agent_runner_linux_e2eは撤去し、実OS PID helperは専用fileへ残す。実task/toolのfresh VM/loader/stdio/cancel/事前取消し・env filterはpublic stdlib/agent、NOE profile、実CLI fixtureへ対応する。JS VMのprocess.pidを別processの回収証拠にせず、実OS commandのreapとtask/outcome/owned resourceの終了を検査する。

S11親/Provider範囲の独立reviewはR1/P2（旧Windows TEMP/cwd書込制約と別process説明の残存）1件を採用・修正。公開API/NOE組立/provider/診断/試験保全に追加必須問題なし。consumer検証scriptの旧reference/--noe依存とartifactへ新internal sourceが入らない問題も修正した。6c6ac544を同reviewerが限定再確認しR1解消・追加必須なしと判定。local replaceとreplaceなし隔離module artifactでstdlib、model/auth/log、Go Host/Client consumerがPASS、stdlib consumerは6対象cross compileもPASS（stdlib-artifact.log、agent-artifact.log）。外部公開は行わない。

E2E初回は旧fixture前提3件と実装退行2件で失敗（e2e-full.log）。実装退行はtoolの非zero process.exitをreason=completedにしていたことと、exitによるview context取消しがbridgeの自動Close経由でOperation/caller contextまで取消されexitCodeを上書きする競合。前者は非zero code→failed、後者はmanaged AttachOperationをowner cleanupだけで閉じ、借用側AttachClientの自動context cleanupは維持して修正した。単発E2Eの成功だけを解消根拠にせず、2つの既存出力/子所有fixtureを修正後-count=30で反復しPASS（e2e-race-30.log）。意味のあるphase/queue/所有期待を変更して回避していない。

旧long-RPC fixtureはagent/slow_integration_test.goのGo Client直呼出しへ移し、model/toolそれぞれ30秒の待機と取消しを確認する。CLI/内部HTTP/fetch timeout前提は持たない。初回4case並行PASS、tool取消しの開始点を実際のtimer登録後へ明確化し、最終gateで再確認する。

新coreのwait/result fixtureは観察context取消しの非波及、複数/繰返しwait、答え省略、UTF-8 byte範囲と不正境界、原文保持を検証。実Go Hostの既存queue fixtureへcommandId再送/別instruction拒否/完了後再送と明示cache key継続を追記した（queue-idempotency.log race PASS）。default SessionID/cache identity・FIFOとfailure recoveryは実runtime queue continuation fixtureで維持する。

S11 core/bridge/tests範囲は46391037の独立reviewで必須findingなし。所有/close join、queue admission/結果保持、cancel受付順、committed final、実OS process回収の新経路への対応を確認し、総合gateへ進行可と判定した。最後の走査で見つけた参照ゼロの旧decisionProvider helper/Host.Defaultsを撤去し、root defaultsのassertは現在のOperation.Defaultsへ移した。対象unit合格後、同reviewerへその3file差分のみ一回の限定再確認を依頼した。最終コード候補はdc32c1bf44cf7148dca3a0a7701e2cb03bf7339a（私有indexのtree、実index/HEADは変更しない）。


## 最終総合検証

S11の独立reviewとcleanup差分の限定再確認は必須findingなし。除外なし全体gateを実施した。通常のcode候補dc32c1bfから、最終vetのcancel所有明確化とlive fixtureのローカルhash helper復元を追加し、その影響範囲を再検証した。HEAD/実indexは変更していない。

| Gate | 結果と根拠 |
| --- | --- |
| 全unit | `CGO_ENABLED=0 go test ./... -count=1` PASS、30.27秒、unit-results.json/unit.log |
| 全race | `CGO_ENABLED=1 go test -race ./internal/... ./stdlib/... ./agent/... -count=1` PASS、91.07秒、race-results.json/race.log |
| MCP込み全E2E | `CGO_ENABLED=0 go test -tags=e2e ./... -count=1` PASS、55.251秒、e2e-results.json/e2e.log。NOE_MCP_SDK_BUNDLEへSDK 1.32.1/esbuild 0.25.12の準備済bundleを明示し、MCP fixtureを実行。entryはrepoとcmp一致。実provider呼出しなし |
| 長待機 | `go test -tags=slow_integration ./agent -run '^TestGoClientLongModelAndToolWait$' -count=1` PASS、33.111秒。Go Client直結のmodel/tool各30秒＋実開始後の取消し4case |
| vet | CGO-free `go vet -tags=e2e ./...` PASS。最初はnative commandの条件付きtimeout cancel所有の指摘。setup失敗時はdeferでcancel、成功時は子終了waiterへ所有を渡すと明記し解消。新たなtimeout/goroutine/権限制限は追加していない |
| 全tag compile | e2e/agent_live/decision_live/hooks_live/context_live/docs_live/agent_efficiency_live/slow_integrationをまとめて`go test -run '^$'` PASS。初回は削除した旧efficiency fixtureが提供していたhash helperだけが残存probeから未定義となり、同probe内へ移して解消。実課金fixtureは実行していない |
| 6対象compile | Linux/macOS/Windows × amd64/arm64で`CGO_ENABLED=0 go list -export -test ./...`全PASS。cancel所有の局所修正後も再確認。Windows/mac/arm64のnative実行結果ではない |
| 最終局所修正の回帰 | `go test -race ./internal/nativeio ./stdlib ./internal/runtime -count=1` PASS。native timeoutの子寿命と回収・stdio/command・実NOE runtime/consumerを再確認 |
| 独立consumer | stdlibとagent/model/auth/log/Go Hostのlocal replace、およびreplaceなしの隔離module artifact双方PASS。stdlib consumerの6対象buildもPASS。ログはS11のstdlib-artifact.log/agent-artifact.log |
| 依存境界 | stdlib、agent/model、agent/auth、agent/log、agentのproduction import closureを検査し、禁止したCLI/noeprofile/app/core Host/SDKの逆依存ゼロ。dependency-boundaries.json |
| CLI/文書/撤去 | E2Eでarithmetic/command failure/親子/queue/session/結果範囲/hook/control/observe follow/json/wait/eventsを確認。旧Connection/RPC header/Runner/NewRegistry/public Installのproduction参照なし。現行変更guide/example 10fileの相対リンク欠落ゼロ、diff check合格 |

最終gateの実行command・環境・時間・exit codeと詳細ログは `/var/tmp/noe-v2-final-logs/`。S11までの対象検証・初回失敗・修正再確認は `/var/tmp/noe-v2-s11-logs/` と各sliceの記録を参照。最終gateでは外側の旧NOE_AGENT_CONNECTIONを除き、短いTMPDIR/GOTMPDIRを明示した。

未来候補一覧のアプリ内runtime項目は完了へ移し、旧実行pathへのリンクを現行source/fixtureへ更新した。過去release/archive/developmentの設計時記録は履歴として保持する。全11sliceの実装・対象試験・reviewと最後の総合検証を完了した。実providerを使わなければ解消しない問いは残らず、実接続/実課金は行っていない。実装・検証完了時点では公開、release/tag/push、実装commitは行っていない。続く利用者の「コミットして」により、検証済みの全実装差分とこの記録をコミット対象とする。利用者の未追跡fileは含めない。
