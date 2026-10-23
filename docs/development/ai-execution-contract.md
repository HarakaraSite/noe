# AI向け実行契約

状態: S1設計。実装の正本は本契約、工程は[計画](ai-execution-contract-plan.md)。

## 入力と結果

run_jsは通常のES moduleであり、暗黙import・コードの先頭への注入を行わない。
agent操作も`import {spawn_agent} from "noe:agent"`と明記する。
データは`export default <JSON互換値>`で返す。非同期処理は`export default await operation()`。
関数やPromiseのexportをNOEが自動で呼んだりawaitしたりしない。
defaultがPromiseの場合はJS_RESULT/serializeで失敗させ、`export default await ...`へ案内する。
JSON.stringifyがPromiseを空objectへ変換しても、正常なdata:{}として返さない。
保存したmoduleでも同じ記法を使い、利用側がdefault exportを結果として読む。
virtual entryの相対importはProfileのcwd、file entryではimport元の場所を基準とする。

stdlibのResultは既存Codeに加えData（json.RawMessage）、Diagnostics、Phaseを持つ。
Dataがnilならdefault exportなし。明示nullはJSON `null`として区別する。
データはJSON.stringifyの標準規則で直列化する。undefined/function/symbolをdefaultにした場合と、
BigInt・循環等で直列化できない場合は結果取得失敗。object内のundefined等は通常のJSON規則に従う。
getter/toJSONは実行され得るので実行contextの取消し対象とする。Promiseをそのままexportしても暗黙awaitしない。
JSON関数は利用者による上書き前に捕捉する。結果はVM終了前にGoへ切り離す。

agentの結果はstdout、stderr、exitCode、reasonを維持し、data、diagnostics、phaseを追加する。
reasonはcompleted/failed/timeout/cancelled。phaseはprepare/parse/compile/resolve/link/evaluate/serialize/cleanupのうち
到達した段階で、成功はcomplete。エンジンが区別できない段階を推測して細分化しない。
実行後のserialize失敗は副作用を巻き戻さず、failed/serializeとして返す。自動再実行しない。
非ゼロprocess.exitCodeは実行失敗、catch済み例外や単に表示した失敗は実行成功。
評価を完了していない場合はdefault exportを結果として返さない。
取消し・timeout・cleanup errorは既存の優先順位と回収を維持する。

## 検査とエンジン

engineはmoduleのcompile/登録、link、評価を分離できる入口を持ち、通常実行と共有する。
CheckModuleSyntaxは構文のみの互換入口として残せるが、NOEが提供する検査は成功段階を明示する。
stdlib.Check(ctx, Source, Profile)はfresh runtimeでentry/静的依存をcompile/linkして閉じ、
GoデータだけのCheckResult（ok、stage、entry、diagnostics）を返す。JS本体は評価しない。
Goでは検査不合格をresultとerrorで返し、JSのcheckは通常の検査不合格をthrowせずok:falseと診断で返す。
実行時のdynamic import、JS値、外部状態の正しさは保証しない。
custom loader/検査initializerのGo処理には副作用があり得る。利用者の非評価検査がJS module本体を動かすことはない。
検査はProfile.Initializeを再実行せず、明示的なProfile.InspectInitializeだけを使用する。
検査用callbackはmodule登録・metadata供給と検査専用資源を担当し、通常実行のOperationやcleanupを共有しない。
agentは検査VM専用の独立Operationを作成して同じmodule sourceを登録し、cleanupもそのOperationだけを閉じる。
独自Initializeだけで導入したmoduleは検査へ自動継承しない。必要なら所有規則を満たす登録callbackを両方へ明示設定する。
検査時に未登録のcustom moduleはunknown/解決失敗として扱い、実行時の不存在とは断定しない。
検査と実行の間のfile変更を保証しない。同一実行内はcompile済みmoduleを再利用し、
別呼出し・別VM間の成果物cacheは追加しない。

エンジンsourceはv0.28.0を管理された`third_party/go-quickjs`の独立moduleへ取り込み、
変更を記録する。module cacheは変更しない。開発時はroot moduleのrelative replaceを使う。
外部consumerの合成artifactではengineも固定候補versionのartifactとしてlocal proxyへ提供し、
consumer自身のreplaceなしで取得する。未公開候補であり、公開済み依存として説明しない。
upstreamへのpush/公開は別作業。既存engineのLICENSE/AGENTSと由来を保存する。

## 診断

Diagnosticはcode、phase、messageと、取得できるsource/line/column/stack、exception情報を持つ。
codeはNOE分類（JS_SYNTAX、JS_MODULE、JS_EXCEPTION、JS_RESULT、JS_CANCELLED、JS_TIMEOUT、JS_HOST）。
利用者例外codeはexception.codeへ分け、NOE分類を上書きしない。
位置はengineから得た情報だけを使い、不明位置は省略する。エラーテキストの正規表現解析に依存しない。
command例外のcode/signal/stdout/stderrは従来どおり取得可能なdata propertyを保存し、
利用者getterを診断収集だけのために呼ばない。診断は成功したdataと混同しない。

## 自己記述

`noe:docs`の既存docs(topic, member)は人間向け詳細説明として維持する。
`describe(topic?, member?)`を構造化したAPI発見の入口とする。
全catalogと、そのruntimeでのavailable/unavailable/unknown、実際のexport名、
署名・説明の有無を別fieldで返す。unknown custom moduleを不存在と断定しない。
実際のexport/登録情報はengineのmodule metadataから取り、module本体を評価しない。
引数・戻り値の署名は明示metadataとして保守し、反射で意味を推測しない。
説明のないexportでも一覧に表示する。検索候補は同じcatalog/exportsから作る。
署名や説明の不足は不足として表示し、未記述のAPIを利用不能にはしない。

実行前検査は`noe:runtime`の`check(code)`からも使えるようにする。
現在のProfileのcapability/loader factoryと、InspectInitializeを用いた別VMで検査し、CheckResultを返す。
現在実行中のVMを汚さず、再帰的なmodule評価もしない。

## hook・履歴・出力

beforeToolはモデルの元codeと実効codeを区別する。afterToolは実行した結果の抜粋と参照を受け、
モデル向け結果を加工できる。元のprovider native assistant itemsを加工後codeで書き換えない。
元呼出し、実効引数、実行事実、加工結果はtool eventの因果関係で識別する。
hookの加工結果がモデルに渡ることと、実行の成否は別の事実として記録する。

通常のtool結果では各stdout/stderrを先頭末尾の計64KiB、dataを64KiBまでインラインで提示する。
dataが大きい場合は壊れたJSON断片をdataに置かず、dataを省略しサイズと全文参照を返す。
各streamの抜粋はUTF-8を壊さず、省略量を付ける。diagnosticsはstream省略で消さない。
診断自体が大きい場合も全体64KiBを目安にmessage/exception textとstackの中間を省略し、分類・phase・位置を残す。
`diagnosticsOmittedBytes`で元の診断JSONと抜粋のbyte差を示し、元の全診断を同じartifactへ保存する。
大きい元結果はtask所有のartifactへ保存し、モデルへresultRefを返す。
`noe:agent`の`read_tool_result(resultRef, {offset?, limit?})`でUTF-8 byte範囲を取得する。
limitは4〜65536 bytes、既定24KiB。既定ページはJSON escapingとmetadataを含めても通常の64KiB data枠に収まり、取得ページ自体の再省略を避ける。offsetは0以上のUTF-8文字境界とし、文字途中や末尾超過はrange error。
範囲末尾が文字途中なら直前境界まで短縮し、nextOffsetは実際に返したbyte数を加算する。
応答はtext、offset、nextOffset、totalBytes、truncated。textのUTF-8 bytesは原結果の当該範囲と一致する。
同じactive taskだけが取得可能で、task終了時にファイルと参照を回収する。
queueの次taskからは取得不可。利用者が成果を残す場合はtask内で通常のFSへ保存する。
保存場所はagentの一時artifactとしてhostが所有し、NOEのworkspaceや固定pathへ依存しない。
保存失敗は情報を失った成功として隠さず、diagnosticと省略情報へ示す。
stdout/stderrの全文はcapture時に一時fileへstreamし、大量出力を無制限のmemoryへ蓄積しない。
全文保存のIOも取消し・終了回収に従う。hookがモデル向け内容を変更した場合も元artifactを失わない。

provider継続に必須のnative items/reasoning/call IDは保持する。
全文を履歴・hook RPC・eventへ繰り返しコピーせず、通常は同じ抜粋と参照を使う。
診断やstate通知を可変system instructionへ混ぜない。instance session/cache識別はqueueでも維持する。

## 移行と受入

新しいfieldはGo/JS型、tool validator、providerへ渡すJSON、イベント、docsと揃える。
Workerのclassic/module判定はsyntax-onlyの判断を保ち、link失敗をclassicへのfallbackにしない。
公開stdlib/agent consumerはNOEのbinaryや設定pathを必要としない。
examplesとfixtureの旧暗黙agent呼出しは明示importへ移す。旧instructionやaliasを残さない。

変更前基準はcommit f5aa0db、source archiveとbinaryを`/var/tmp/noe-ai-execution-20261023`へ保存。
比較課題は(1)明示agent importと局所値の同名再宣言、(2)説明のない既存API発見、
(3)missing exportの検査と修正、(4)返却objectを使う次のmodel要求、(5)長い出力の中間部分取得。
成果の正解（実行完了、期待値、全文の対象byte一致、module本体の非評価）を先に確認し、
その後でmodel/tool往復・転送・usage/cacheと時間を比較する。
実providerはNOEの既存keyを使用し、少数の固定課題で比較する。
エラーを直すための動作以外にcache率を上げる目的の反復はしない。
