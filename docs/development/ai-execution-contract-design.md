# AI向け実行改善：S1 契約・詳細設計

更新日: 2026-10-23。状態: S1完了。通常・批判的レビューの指摘を修正し、再レビューで解消確認済み。S1終了時点では製品実装は未着手。以後の進捗は[実施記録](ai-execution-contract-implementation.md)を参照。
正本の範囲は[見直し計画](ai-execution-contract-plan.md)§1。基点はv2.1.1復旧後の`5781755`。
本書はS2〜S4で実装する契約を定める。上流変更、製品実装、コミット、公開、配置の許可を追加するものではない。

## 1. 固定する範囲と責務

- 静的検査は既存Biome／tscの利用案内・設定例のみ。組込み、専用API、自動実行、ラッパーを作らない。
- noestdlibとnoeagentlibが各自のAPI資料・型を管理し、既存docs索引から案内する。新しい統合検索・問い合わせ基盤は作らない。
- noestdlibは実行値・例外をVM終了前にGoデータへ切り離す。agentlibはrun_js、hook、履歴、providerへの配送を担当する。
- go-quickjs v0.28.0の公開APIを利用する。NOE管理stdlibへの変更許可を、上流本体・module path・派生版公開の許可へ広げない。上流改変が必要なら変更前に利用者へ明示的に許可を求める。

## 2. S2：既存静的ツールの利用手順

成果物は`docs/javascript-checks.md`と同ページ内のJS用設定例。既存のagent/JS利用ガイドから案内する。
利用者のprojectにあるBiome／TypeScriptの版と設定を使い、確認に使った版・cwd・対象ファイル・commandを例に併記する。未導入なら導入・実行していない事実を示す。NOEが導入や検査を自動で行う機構は追加しない。

順序は、JSをファイルへ保存し、projectのBiomeで構文・lintを確認し、必要に応じて`allowJs`・`checkJs`・`noEmit`を設定したtscへ渡す。
tscの例はS3の2つの宣言ファイルを`files`または参照指示で読み込み、`types: []`で意図しない`@types/node`の混入を避ける。
`lib`は実際のNOE APIとの対応を説明して選ぶ。`DOM`の一括利用をNOEが全Web APIを提供する証拠にしない。
設定例のcommandをS2で確認し、S3の型を含む最終例はS3で確認する。外部ツールを製品のGo依存や実行時必須依存へ追加しない。

Biome／tscの合格は静的検査結果だけを意味する。QuickJSの動作、Profileの権限、dynamic import、外部状態は実行結果で判断する。
既存`CheckSyntax`／`CheckModuleSyntax`のWorker判定等は維持し、link検査や新しい事前検査へ拡張しない。

## 3. S3：API情報の正本と案内

### 管理単位と配布

| 所有者 | 管理する情報 | 正本・配置先 |
| --- | --- | --- |
| noestdlib | NOE管理の標準module、Node互換の実対応部分と別名、global、`noe:docs` | 既存の登録実装、`internal/apidocs/content`の該当説明、`stdlib/types/noestdlib.d.ts` |
| noeagentlib | `noe:agent`、`noe:model`、`noe:image`、`noe:decision`、`noe:auth`、`noe:agent-log`等の公開JS API | 各bridge/moduleの公開定義、同じcontent内の担当説明、`agent/types/noeagentlib.d.ts` |
| NOE組立 | 各bindingを有効にする既定の組立とCLI利用条件 | profile・CLIの利用ガイド。ライブラリAPIの型を別所有にしない |
| 共用資料 | 既存topic/alias索引と説明表示 | `internal/apidocs`。実装・SDKへのimportを持たない |

型はライブラリ別の2ファイルとし、必要な利用例で両方を参照する。auth/logは公開`agent/auth.Bind`・`agent/log.Bind`で独立提供できるAPIなのでagentlibの型へ含める。
単一ファイルへの統合、型を配るための新しいGo moduleやnpm packageは作らない。ファイルは同じ版のsource配布から取得する。パスと取得手順をdocsへ記載する。
初回は宣言ファイルへの案内だけで足りる。埋込み用の複製や型の統合配布機構は作らない。

### 既存docsの表示契約

`noe docs [topic [member]]`と`docs(topic?, member?) -> string`はMarkdown返却のまま維持する。`Topic.Members`も「詳しい説明がある項目」の意味を変えない。
各topicのoverviewに次を載せる。

1. canonical module名、実際に登録する別名、公開export名またはglobal名。
2. 対応する宣言ファイルと、引数・戻り値・失敗・主な制約の明示的な説明。
3. Profileの必要条件、main/Workerの条件、bindingの追加条件。これは利用条件の説明であり、現在のVMの能力照会ではない。
4. 詳細説明がある項目の案内。説明未整備と未対応APIを区別する。

export名の根拠は`SetModule`／`SetModuleValues`の登録map、所有するJS moduleのexport定義、globalの登録箇所。Markdown見出しや任意moduleの実行から推測しない。
情報取得時は既存の埋込み資料だけを読む。外部loader、provider、対象moduleの評価を起こさない。
S3で登録箇所と資料の対応表を確認し、所有する標準APIの既存fixtureで名前・alias・宣言の一致を検証する。資料取得のための本番時のVM生成・module評価や、汎用export解析器は作らない。

署名の正本は所有者の明示的な`.d.ts`、制約・使用例の正本は担当Markdown。登録実装との差分は同じ変更で更新する。
overload、同期/非同期、byte/string、失敗時のrejectか戻り値かを実装に揃える。未確認の部分を`any`で覆って「型対応完了」にしない。engineの標準ECMAScript組込みはTypeScriptのES libへ任せ、NOEの上乗せ・差分を記述する。
共用索引がagentの説明を含んでも、noestdlibからagent実装へ依存しない。`noe:agent/internal`等の内部通信moduleを公開APIとして型保証しない。

## 4. S4：返却値の契約

### JavaScript

run_jsは通常のES module。agent操作も`import {spawn_agent} from "noe:agent"`等を明示する。source先頭への暗黙importを廃止し、元の束縛・行番号を保つ。
返却は`export default value`。非同期処理は`export default await operation()`を推奨する。

| 値 | 結果 |
| --- | --- |
| default exportなし | 成功時も`data`を含めない |
| `null` | `data: null` |
| JSON化可能な値 | JSONデータを`data`へ返す |
| defaultがPromise／thenable | ECMAScriptのPromise解決規則で一度待ち、その結果をJSON化。既存Loopと取消しの対象 |
| defaultが関数 | 呼び出さない。JSON表現がなければserialization失敗 |
| `undefined`／SymbolなどrootのJSON表現なし | defaultなしと区別し、serialization失敗 |
| BigInt／循環、`toJSON`・getterの例外 | serialization失敗。JSON化のための独自文字列変換や自動再実行はしない |

JSON化は初期化前に捕捉した`JSON.stringify`をVM owner上のcontext内で一度行う。標準のJSON規則を使い、再帰的な独自型検査器は作らない。
そのためnestedなundefined/function/Symbolの省略や配列内null化、非有限数のnull化、Dateの`toJSON`等は通常のJSONと同じ。nested Promiseは深くawaitせず、通常のJSON表現になる。複数の非同期値は利用者が`Promise.all`等で確定させる。
getter、`toJSON`、thenableの`then`は利用者コードとして実行され得る。default関数をmainとして呼ぶ機構はない。
defaultの束縛はentry評価の完了時に一度読み、その値を待つ。snapshotはentry/default待機と既存Loop上の仕事が正常終了した後に取る。
JSON化のgetter/`toJSON`が追加したmicrotask・timer・host仕事も、同じcontext/Loopで処理してから成功とdataを確定する。snapshotは再生成しないため、JSON化後に値が変わってもdataはJSON化時点の値である。
その後続仕事が失敗したらstage=executeとしてdataを破棄し、取消し・期限は通常の規則で分類する。JSON化自体が失敗した場合はstage=serializeとしてdataを返さず、追加仕事は既存の失敗時Closeで取消し・回収する。
途中で`process.exit`した場合は既存の終了codeを使い、取得に到達しなかったdataは含めない。

### Goとrun_js

`stdlib.Result`は既存`Code int`を維持し、`Data json.RawMessage`（`json:"data,omitempty"`）と`Diagnostics []ExecutionDiagnostic`（`json:"diagnostics,omitempty"`）を追加する。
nil Dataは返却なし、`json.RawMessage("null")`は明示null。VM値やnamespaceを外へ返さない。`Execute(ctx, Source, Profile) (Result, error)`と`Diagnostic(err) string`は維持する。追加fieldにより、外部の位置指定struct literalは修正が必要になり得る。
公開`stdlib.Diagnostic`関数と型名は衝突させず、診断要素の公開型名は`ExecutionDiagnostic`とする。
共有する診断・位置・例外詳細は純粋なGoデータとして`internal/executionresult`へ置き、public stdlibからaliasする。agent coreはこのデータだけへ依存し、engine/stdlibをimportしない。

agentのResultとrun_jsのJSONは既存の`stdout`、`stderr`、`exitCode`、`reason`、省略byte数に`data?`、`diagnostics?`を加える。
成功例は`{"stdout":"","stderr":"","exitCode":0,"reason":"completed","data":{"answer":42}}`。dataなしならfield自体を省く。reasonは既存のcompleted/failed/timeout/cancelledを維持する。
Go errorがあれば成功扱いにしない。取消し／期限の既存優先順位を守る。非zeroの明示exitはfailedとするが、例外の発生を捏造しない。
実行済みdataがあり、その後のCloseだけが失敗した場合はdataを保持し、reasonは失敗、diagnosticsにcleanup失敗を付ける。

stdout/stderrは実際に書かれた内容だけ。現行の各64 KiB（先頭/末尾各32 KiB）と省略byte数を維持する。診断文をstderrへ追記しない。
dataは有効なJSON全体を返し、途中の切断をJSON成功として返さない。新しいdata上限やartifact保存機構は追加しない。大きな成果物は既存fsでファイルへ書き、小さな要約・pathを返す利用例を用意する。
診断は出力とは別配送し、stdout/stderrの省略によって失わない。画像も既存の専用経路を維持しdataへ埋め込まない。

## 5. 公開APIだけによる実行管理

変更は`internal/jsexec`とNOE管理loader内に置く。上流v0.28.0の型・source・配布形態を変えない。

1. VM生成直後、利用者Initializeの前にPromise.resolve/then・Reflect.apply・JSON.stringify・own-property確認のintrinsicを捕捉する。
2. 元Sourceをそのままprepareし、Origin.Source/Entry、virtual/fileの解決基準、Profile.NewLoaderの呼出しを維持する。
3. NOE内部のwrapper source、捕捉済みhelperを公開する内部module、値を受け取るnative callbackを同じVMへ登録する。内部specifierはentryや既存登録と衝突しない名前を割り当て、公開APIにはしない。
4. wrapperは元entryを`import * as ns`し、捕捉済みのprivate helperへnsを渡してその完了をawaitする。wrapper自身はexportを持たない。dynamic importはwrapperだけへ向ける。元namespaceをPromise.resolveやPromiseのresolve値へ渡さないため、元moduleのnamed `then`を誤実行しない。
5. helperはdefaultの存在を確認し、存在するときだけ捕捉済みPromise操作でdefaultを待つ。その完了時にnative callbackへ値を渡し、wrapperへはundefinedを返す。結果はowner内のValueとして保持し、Loopは現行どおりwrapperの完了と既存の仕事を駆動する。defaultとして明示的に返されたthenableのthen実行は§4の契約であり、namespaceの誤実行と区別する。
6. defaultの待機は元entryを再評価しない。entry/defaultからwrapperへ伝わった拒否は既存の観測方式で記録し、Loopの駆動を止め、job runnerを離れてから元Promiseの`Await(ctx)`でGo causeを回収する。拒否を処理する観測callbackの戻り値に置き換えない。
7. Loopが正常に空になりdefault待機も完了してからJSON snapshotを一度取る。未settleで仕事がなければ既存ErrNeverSettles、取消しは既存context優先。serialization開始後も取消しを有効にする。
8. JSON化が成功したら、同じLoop/contextを再駆動し、JSON化が追加した仕事と未処理拒否、およびBindingsに記録された失敗を確認する。§4の規則でdataの確定/破棄とstageを決める。JSON化を再実行しない。
9. 例外・dataを切り離した後、既存Closeでscope、Worker、I/O、Loop、VMを回収する。cleanup中にJSの再実行をしない。Go causeはerrors.Is/Asで辿れる形で残す。

native callbackはnamespaceをVM内で受け渡すだけで、exportされたdefault関数を呼ばない。元entryのsource/nameをwrapperへ置き換えず、利用者の相対importと元の位置を維持する。
通常script（Source.Module=false）は既存のcompletion/exit動作を維持し、新しいdefault dataは作らない。Worker自身のentry実行方式・default返却方式も変更せず、親の実行管理・回収との接続だけ確認する。
direct Attachの借用所有関係、Runtimeのsingle-use、Cancelの呼出し元goroutineとVM ownerの区別も維持する。

## 6. 診断の契約

各要素は`{code, stage, message, stack?, position?, exception?}`。`code`はNOE分類、`exception.code`は利用者例外のcodeで別物。

| field | 内容 |
| --- | --- |
| code | `execution_error`、`serialization_error`、`cancelled`、`timeout`、`cleanup_error`。setup失敗もexecution_errorとstageで区別 |
| stage | `setup`（Profile/prepare）、`execute`（module評価・default待機・Loop）、`serialize`、`cleanup`。engine内部phaseは名乗らない |
| message / stack | messageと取得できた既存stack原文。stackを解析して構造化frameを捏造しない |
| position | 上流SyntaxError.Position等から実際に取得したsource、line、column。1始まり、未取得要素は省略 |
| exception | 自前のdata propertyとして得たcode/signalのscalar、stdout/stderrの文字列またはbyte出力。getterを呼んで詳細を埋めない。外部command出力とtool自身のstderrを区別 |

Error.prepareStackTraceは常時差し替えない。元のstackが得られなければ省略する。取消し後にJSへ入り直して不足情報を集めず、既に取得した情報とGoの取消し原因を使う。
commandの`encoding: "buffer"`によるstdout/stderrは、上流公開`Value.Bytes()`でVM終了前にコピーし、`{"encoding":"base64","data":"..."}`として保持する。文字列出力はそのまま文字列。一般の例外objectを無制限に再帰展開する機構へは広げない。現行scalar snapshotではbyte出力を取りこぼすため、この既知の制約も診断整備時に解消する。
serialization時の例外はstage=serialize。前段のコードが動かなかったとは言わず、「評価後の結果変換に失敗。副作用は再実行されない」と案内する。
複数の実原因はdiagnostics配列で保持する。Go errorのJoin/Unwrapは維持し、JS値を抱えたquickjs.ErrorをVM終了後へ逃さない。
`try/catch`してログへ表示した外部command失敗を、NOEが文面から実行失敗へ格上げしない。通常の非zero exit・caught例外・未処理例外を区別する。

## 7. agentlib・履歴・表示への配送

対象は`internal/agentexec/executor.go`、`internal/agent/{executor,broker_tools,broker_tool_result}.go`、`internal/agent/js/{task,history}.js`と結果を表示するCLI/log解析。
新fieldはRPCだけで止めず、runner検証→rawResult→afterTool→実効result→provider native tool結果へ運ぶ。元引数/実効引数、raw/加工後の区別は既存を維持する。
runner境界ではdataがあればJSON値、diagnosticsがあれば既定field型を確認する。afterToolは既存の任意result加工契約を維持し、結果を変えた事実と元結果を観察可能にする。hookの加工結果へrunner用schemaを再強制しない。
hook自体の失敗は実行結果とは別に扱い、既存のmodelResultAvailable=falseとhookErrorを維持する。
不正tool引数、scope初期化、executorのGo error、取消し等、通常stdlib結果を経由しない経路も診断へ揃え、エラー文字列をstderrへ足す経路を残さない。

`run_js.finished`の既存のexitCode/reason/successは維持し、診断・dataも記録できるようにする。`tool_result`のrawResult/resultは元/実効値の正本を維持する。CLIは診断を別項目として表示し、解析APIも新fieldを落とさない。
provider adapterは既存のtool結果JSONを通す。reasoning item、tool-call id、画像別配送、会話継続・session/cache keyを変更しない。固定のtool説明へ返却fieldと明示importの例を追記し、送信ごとの可変情報をsystem instructionへ追加しない。
S4で同梱examples、初期案内、run_js docs、hook例を新契約へ更新する。旧暗黙importへの互換preludeは残さない。公開Go呼出しのerror/exitcode契約は維持し、dataが必要な呼出しだけ追加fieldを使う。

## 8. 検証根拠・後続の受入条件

S1では製品を変更せず、正式上流のAPIと現行ソースを確認した。
`/var/tmp/noe-upstream-api-xvgqsp54`の単体probeに加え、`/var/tmp/noe-s1-loop-probe`で現行public stdlib.Attach/Bindings.Prepare/DefineModule/Loopへwrapperを接続した。
後者はroot checkoutを参照する一時consumer（NOEだけlocal replace、engineは正式v0.28.0）で、`GOPROXY=off GOWORK=off CGO_ENABLED=0 go run -mod=mod .`により以下を確認した。

- timerを跨ぐTLA後のdefault値取得と、named thenの非呼出し。
- default自身のPromiseとthenableをwrapper内で待ち、timer後の値を受け取ること。
- timer後のPromise拒否に含まれるnative Go causeをerrors.Isで保持。
- 待機中のcontext期限による停止。
- Workerの値取得と、Closeから戻る時点でinitializer cleanupが完了（1/1）。

これは公開API接続の限定probe。最初の試行ではprobe側で拒否callbackの戻りPromiseを待って原因を失ったため、既存Runtimeと同じく元Promiseを返す観測へ直して再確認した。製品不具合や製品受入試験の結果とは扱わない。
全Profile/loaderの組合せ、構文位置、hook/provider配送は製品実装で確認する項目として残る。probeは型別JSON化・変換失敗の受入試験を代替しない。

| 段階 | 必要な確認 |
| --- | --- |
| S2 | 記載command/設定の成立、cwdと対象の一致、未検査と合格の区別。型を使う完成例はS3 |
| S3 | 両libの公開名・alias・署名（auth/logを含む）、型利用例のtsc確認、存在するが説明が薄いAPI、main/Worker/Profile条件、docs取得の無評価性 |
| S4 | defaultなし/null/undefined、Promise/thenable、関数非呼出し、nested値のJSON規則、循環/BigInt/変換例外と副作用非再実行、JSON化が追加した仕事の完了/失敗、source/name/相対import、TLA後のGo cause、取消し、Worker/I/O回収、Close失敗とdata保持 |
| S4配送 | stdout/stderrの既存上限、診断の独立配送、commandの文字列/byte詳細、raw/実効hook、hook失敗、画像、次のprovider要求までのdata/診断保持、log/CLI表示 |
| T | CLI→docs/型→既存command検査→run_js→修正の流れ、公開Go consumer、必要な既存suite。VM/回収変更の関連race等は実装工程で終える |

S3/S4は実装・対象テスト・コードレビュー・必要な修正を行う。Tの証拠は同一source/依存のリリース準備へ再利用し、CIで同じテストを重ねない。
上流改変や独自resolver、構造化frameの全件取得、artifact基盤等を、実装中の便利さから必須条件へ追加しない。

## 9. S1レビュー

2026-10-23、利用者の原文条件、変更権限、製品未実装の状態を2担当へ渡し、通常・批判的レビューを実施した。各15分以内、read-only、再委譲なし。初回のP2は重複を除いて次の3件で、すべて設計へ反映した。

| 指摘 | 修正 |
| --- | --- |
| auth/logの型所有者が公開bindingと不一致 | agentlibの宣言・説明へ含め、型を2ファイルへ整理 |
| JSON化が追加した仕事・未処理拒否を確認せず成功になる | JSON化は一度だけ行い、その後も同じLoop/contextを駆動し、成功とdataの確定前に失敗を確認 |
| commandのbyte出力がscalar限定の診断で欠落する | 公開Value.Bytesでコピーし、encodingを明記したbase64表現で保持 |

両担当が変更箇所と既存指摘に限った再レビューを一回行い、指摘の解消・追加findingなしを確認した。残るP1/P2/P3 findingなし。設計としてS2以降へ進める状態と判断する。
確認は設計と関連source・公開API・限定probeの範囲。製品実装と受入試験の完了を意味せず、§8のS3/S4/Tの確認は後続作業に残る。
