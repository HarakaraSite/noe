# AI向け実行契約の実装記録

正本: [計画](ai-execution-contract-plan.md)・[契約](ai-execution-contract.md)。
利用者は計画に沿った実装・各スライスのテスト/コードレビュー・最後の総合テスト、実provider利用を許可。
コミット・配置・公開は対象外。既存の未追跡実行ログ4件には触れない。

| 工程 | 状態 | 根拠 |
| --- | --- | --- |
| 最終計画再レビュー | 完了 | f5aa0dbを通常/批判的に確認、追加P1/P2/P3なし |
| S1 契約・基準 | 完了 | baseline revision/source archive/binaryを/var/tmp/noe-ai-execution-20261023へ保存 |
| S2 engine | 完了 | 新API・全engine suite・外部consumer合格。既存3findingを独立再確認し解消 |
| S3 stdlib | 完了 | 対象suite合格。独立review P2と親review Worker scopeのP2を修正・再確認 |
| S4 run_js/docs/agent接続 | 完了 | 対象unit/4wire/E2E合格、P3署名修正と限定再review完了 |
| S5 最適化・移行 | 完了 | 対象unit/E2E/raceと独立review P2/P3修正・再確認完了 |
| T 総合テスト | 完了 | 更新候補で全体unit/E2E・engine・race・vet・長待機・6対象compile・両consumer合格。examples/実modelの範囲と限界は下記 |

各工程の対象試験・review・findingと採否は以下へ追記する。

## S1のレビューと修正

- 通常/批判的レビュー共通P2: 通常Initializeを検査VMへ再利用すると、agent bindingのcleanupで親OperationをCloseする。
- 採用: Checkは通常Initializeを再実行せず、明示InspectInitializeと検査専用Operationを使う。親scopeは借用して閉じない。
- engine candidateを合成proxyへ含める方針とS2→S3→S4の順序は成立。
- local probeでdynamic import namespaceからdefault exportを取得しJSON化できることを確認（answer:42）。
- 批判的P2: default PromiseはJSON.stringifyで{}となりawait漏れが成功に見える。採用: Promise defaultをJS_RESULT/serializeで拒否して明示awaitを案内。
- 批判的P3: 全文rangeのUTF-8境界。採用: offset境界を検査、末尾は文字境界へ短縮し実byte数からnextOffsetを返す。limit最小4で進捗を保証。
- S1の限定再確認: 通常/批判的とも既存P2/P3解消、追加findingなし。S2着手可。

## 変更前の実provider基準

- 保存先: `/var/tmp/noe-ai-execution-20261023/baseline-live`。既存NOE keyを明示しoptional instructionを分離。
- OpenCode Go/deepseek-v4.1-flash、固定の計算・report.json保存・読戻し課題。
- completed、count=200/sum=2686700/lastSquare=40000を独立に確認。
- model要求2/tool1/agent起動1、input=4112/cached=1664/output=389、約5.2秒。
- 1課題の基準であり、cacheの一般的改善や回帰率を推定する試行ではない。

## S2対象確認とコードレビュー

- root担当consumer/helperの通常コードレビュー: findingなし。
- stdlib/agent両consumerのlocal+合成artifact build/run成功。3つのGo consumer fixture対象テストも成功。
- critical P1採用: EvalModuleContextがTLA後のDrainJobsまでcontextを保持せずdeadline無効。全処理でcontextを保持し回帰test。
- critical P2採用: namespaceをPromise fulfill値にするとnamed export thenを自動実行。completion PromiseとNamespace取得を分離し回帰test。
- engine全suite初回はnested tool moduleのgo.mod欠落でdependency検査2件失敗。module cache配布物の欠落と切り分け、正規sourceからの補完を確認中。

## S2再起動後の確認

- 前agentは再起動で失われた。既存sourceを確認し、修正を作り直さずテストと限定reviewを再開。
- critical追加P2採用: nested link errorのSourceをentryと誤報しない。失敗moduleを保持するModuleLinkErrorへ修正。nested依存先を確認する回帰testを追加。
- engineのgo test ./... -count=1が全合格。追加の既存finding対象testも合格。ログ: /var/tmp/noe-ai-execution-20261023/engine-s2-recovery-tests.log、engine-s2-fixed-targets.log。
- 欠けたnested go.modの2件は公式v0.28.0 raw sourceとbyte一致を確認しUPSTREAMへchecksum記録。test skipや期待値緩和なし。
- S2限定再確認は3件とも解消、追加findingなし。S3移行可。artifact helper最終版もstdlib consumerのlocal/artifact双方成功。

## S3 実装・テスト・コードレビュー

- managed moduleのprepare→completion Promise→Loop→Namespace→default JSONを接続。Go Result/DiagnosticInfo/StackFrame/CheckResultを公開aliasへ反映。
- CheckはInspectInitializeと独立VMを使用。JS checkの通常parse/link失敗はok:false。
- go test ./internal/jsexec ./stdlib -count=1合格。
- 独立review P2: rejected default Promiseが通常unhandledで先に失敗。default Promiseだけobserverを付けJS_RESULT/serializeへ分類。pending/TLA後/無関係unhandledを回帰確認し、同担当が変更箇所を再review、解消。
- 親review P2: Worker内のcheckがparent modeを使うとscope外APIを誤って検査可能にする。内部checkWithModeへBindings.workerを伝え、InspectInitializeとOrigin.Workerを一致させた。実Workerでscope別登録とscope外失敗を確認。
- 親の変更箇所レビュー: Go Checkはparent mode、JS checkは現在worker mode、attachとloader originへの伝搬を確認。対象testとjsexec suite合格。S4移行可。

## S4 対象確認

- 構造化結果をhook/履歴/次model要求へ届ける公開Go Hostの結合testが合格。before/afterTool、元source/実効source、raw/加工resultの分離、check後の親agent API、Promise診断、then exportの非実行を確認。
- OpenRouter/OpenAI両SDK × Chat/Responsesの4経路で実CLI assembly+local HTTP fixtureを通し、dataと元source lineの診断がwireへ届きnative reasoningを保持することを確認。
- describeのnative/source非評価、実exportと説明/署名の区別を対象testで確認。
- 旧fixtureの暗黙呼出しとstderr診断期待がE2E初回で失敗。明示importと構造化diagnosticsへ契約移行中。並行・回収等の目的/期待値は維持。
- S4独立review P3: run/spawnのdefault configとsteerのcommandId省略を修正。実装と署名を照合し、同担当が解消確認。追加findingなし。

## S5で確認した候補依存の修正

- 親review: v0.28.0-noe.1は同patchのstable v0.28.0より低く、既存consumerがstableを直接要求するとMVSが新APIのないstableを選び得る。
- 未公開engine候補をv0.28.1-noe.1へ変更し、consumerの直接要求も現行stable v0.28.0に強めて検証する。開発用replaceでこの問題を隠さず、最終artifact検証で選択versionを確認する。
- 上流に未採用のengine APIを含む候補であり、外部公開時の正式依存は別途決める。公開済みengineとして扱わない。

## S5 実装と対象確認

- >64KiBのstdout/stderrはring previewとtemporary spoolへ分け、VM/producer終了後にread ownershipをtask storeへ移譲。保存先はNOE固有pathへ依存しない。
- full JSONをstream保存し、data/巨大診断のpreviewとopaque resultRefを返す。read_tool_resultは同active taskだけ認可し、task operationのrequest/child join後にstore.Close。
- 既定pageを24KiBに変更。64KiB textをそのままdefault exportするとescaping/metadataでdata枠を超えて再artifact化する具体的経路を避ける。
- argumentsUnchanged/resultUnchangedにより通常のsource/resultをJS→Goへ1回だけ送り、Go側で元/effectiveを復元。provider native履歴は変更しない。
- Backend target/race、capture transfer/cleanup、公開Hostでfull data/stream middle/Unicode/診断復元と次queue ref失効が合格。
- 対象unit、Agent/Docs/Hooks E2Eも合格（24.306s）。
- 親review: NOE標準auth/logとroot agent登録もinspectionに供給する必要あり。各viewで独立bindingを作る既存factoryをInspectInitializeへ明示設定。CLI root/managed toolの検査後provider実行を4wireを含む対象testで確認。

- S5独立review P2: 巨大sourceURL/stack文字列のpreview上限抜けを修正し、最終サイズと省略byte数を確定。全文復元/target/race合格。
- S5独立review P3: pass-through hookのJSON copyでidentityを失い重複転送。iterative JSON値比較で同一内容/キー順変更も再利用し、実変更は区別。production hook→RPC frameの対象test合格。
- 同担当が既存2findingの修正とNOE標準InspectInitializeの独立client所有を限定再確認。追加findingなし、T移行可。
- 最終候補のsource manifestは `/var/tmp/noe-ai-execution-20261023/candidate-source-manifest.json`。

## T 初回総合検証で見つかった残存箇所

- engine全suiteは合格。NOE全体unit/E2Eはinternal/runtimeとimport-map E2Eの旧契約fixtureで失敗。暗黙agent APIを明示importへ、public export一覧へread_tool_resultを追加し、command例外はdiagnostics[].exceptionで確認するよう移行した。継承、並列回収、自己cancel/stopの目的は維持。
- 初回raceはfixture編集中の一時的なGo構文エラーを拾った。これは候補の合格扱いにせず、変更終了後の固定候補で全体unit/E2E・raceを再実行する。
- 親reviewで、unhandled Promise/timerの旧文字列診断には位置があるのに構造化framesが空の経路を確認。位置の期待値をoptionalにする案は採用せず、Value.ErrorFramesでengine内部のフレームをrender前にdetachするよう修正。同期throw・Promise・timerのsource/line/column必須検証を復活させた。engine APIはgetter非実行・close後の空結果も確認。対象suite合格後、独立した限定reviewで追加findingなし。
- 更新文書のリンク検査で既存development索引の削除済instructions.md参照を発見し、現行facts/policyへの参照へ修正。

## 実provider比較の条件と観測

保存先は `/var/tmp/noe-ai-execution-20261023/real-provider-comparison.json` と各case directory。
基準はf5aa0db、候補は今回の実装。OpenCode Go / deepseek-v4.1-flash / effort auto / maxTokens 4096、同じJS入口と課題、別の空workspace・optional instructionなしで実行。認証は既存NOE keyを明示。違いは組込み契約と生成結果であり、モデル応答は固定していない。

| 課題 | 版 | 完遂・成果検査 | model/tool/agent | input/cached/output | cache率（初回を含む / 除く） |
| --- | --- | --- | --- | --- | --- |
| 計算→保存→queueで独立検算 | 基準 | 合格 | 4 / 2 / 1 | 10836 / 5632 / 1450 | 51.97% / 43.59% |
| 同上 | 候補 | 合格 | 4 / 2 / 1 | 11065 / 7936 / 787 | 71.72% / 87.73% |
| named import修復・API維持・実行検証 | 基準 | 合格 | 4 / 3 / 1 | 10415 / 6656 / 945 | 63.91% / 77.01% |
| 同上 | 候補 | 合格 | 4 / 3 / 1 | 12224 / 7936 / 1349 | 64.92% / 78.08% |

計算結果はcount=200/sum=2686700/lastSquare=40000、verification.jsonはvalid:true。修復はbuildReport([3,5,8])={count:3,total:16}を確認し、math.mjsの不変も別途確認した。queueは同じagentが継続し追加起動なし。修復課題も追加agentなし。

候補は両課題でdefault exportのdataを自然に使用した。一方、修復課題では未対応のfile URL queryを生成して1 tool失敗し、構造化診断からplain importへ修正した。初回にファイル書換え済みという副作用を維持して検証を続けている。基準のtool失敗は0。この試行では修正往復やtoken総量の一貫した改善は示していない。

source生成byte/モデル向けtool結果JSON byte（wire総量ではない）は、計算queueで2666/794→1698/576、修復で1617/851→2368/1346。RPCの重複省略は別の実フレーム検査で確認し、これらのevent値をネットワーク転送総量と混同しない。

各条件1回で、cache状態・生成長・実行時間にはばらつきがある。計算queueの約50.8秒→8.6秒も性能改善の推定には使わない。cache・AIの迷い・一般的な効率改善は未確定。モデルの実API発見行動の反復評価や統計的比較は今回行わない。

## 新契約の実model横断確認

- 保存先: `/var/tmp/noe-ai-execution-20261023/candidate-contract-live`。async診断修正後の候補CLI、OpenCode Go / deepseek-v4.1-flash / auto、agent 1体、provider上限12。
- 実modelがdescribeでcheck/read_tool_resultを調べ、存在しないnamed exportをcheckでlink不合格と確認し、正しいimportをlink合格と確認した。check対象のmodule本体を実行する必要はない。
- 50,000文字のprefix/suffixで挟んだmarkerを含む100,031文字のdataはinlineから省略され、resultRefを取得。次のrun_jsで原結果JSON 100,119 bytesを2ページで復元し、中央markerを照合した。
- contract-verification.jsonのinvalidImportRejected / validImportAccepted / markerRecoveredがtrue、長さとmarkerが入力ファイルと一致することを親が別途確認。provider 7回、tool 6回、tool失敗0、追加agentなし。
- このprobeはAPI接続と成果の検証であり、基準との効率比較には加算しない。

## T 最終結果

最終候補はf5aa0db上の未コミット差分。Go 1.27.2 / Linux amd64で実行し、
`GOMAXPROCS=4`、Go build/test並列package数を2に抑えて実施した。

- 固定manifest: `/var/tmp/noe-ai-execution-20261023/candidate-source-manifest-T2.json`（1275 files、SHA-256 `c799bc374e7c267148c7827b9032ff79bbae09bdb95b0caa842a185cd25087cf`）。cmd/agent/stdlib/internal/engine/testsとconsumer helper/templateを含む。報告文書は対象外。検証後に全項目の不変を確認。
- managed engine: v0.28.0由来、候補依存名 `v0.28.1-noe.1`。上記manifest内engine 739 filesのcanonical JSON SHA-256は `a6ccf6b1b8b47cc36248c88896659a8d055edb917c8b0b03786455b7886ebff5`。
- 候補binary: `/var/tmp/noe-ai-execution-20261023/noe-candidate`、SHA-256 `dc756e351a3ff723071eb37354a04beff65110d4b96f2f1f6e4c3dfc2dceaa35`。
- 全実行履歴: 同directoryの `final-results.json`。初回失敗を残し、更新後の実行は `T2-` prefixで区別。runnerはexit 0、`ALL CORE VALIDATIONS PASSED`。

| 最終検証 | 結果 / ログ |
| --- | --- |
| `CGO_ENABLED=0 go test -p=2 -tags=e2e ./... -count=1` | 合格、T2-unit-e2e.log |
| engine moduleで `CGO_ENABLED=0 go test -p=2 ./... -count=1` | 合格、T2-engine.log |
| `CGO_ENABLED=1 go test -race -p=2 ./internal/... ./stdlib/... ./agent/... -count=1` | 合格、T2-race.log |
| `CGO_ENABLED=0 go vet -p=2 -tags=e2e ./...` | 合格、T2-vet.log |
| `CGO_ENABLED=0 go test -tags=slow_integration ./agent -run '^TestGoClientLongModelAndToolWait$' -count=1` | 合格、T2-long-wait.log。30秒model/tool待機と各取消し |
| `CGO_ENABLED=0 go build -o <候補binary> ./cmd/noe` | 合格、T2-build.log |
| linux/darwin/windows × amd64/arm64、`CGO_ENABLED=0 go list -export ./...` | 6対象合格、T2-compile-*.log。native実行の検証ではない |
| `python3 scripts/check-stdlib-consumer.py --output <一時directory>` | local/artifact両build/run合格、T2-stdlib-consumer.log |
| 同上 `--agent` | portable/gohost両run合格、T2-agent-consumer.log |
| 差分の空白・Go整形・更新Markdownの相対file link | 合格。削除済instructions参照1件を現行facts/policyへ修正 |

両artifact consumerは空の専用GOMODCACHEと合成proxyからengineを解決し、
consumer自身のreplaceなしで `v0.28.1-noe.1` が選択されることを機械検証した。
root checkoutのreplaceだけで成立させた検証ではない。

examplesは初回に[今回の検証記録](../../examples/VALIDATION.md)の範囲で49/51 filesを実行し、コミット後の追加実行で全51 filesの確認を完了。
ログ表示は候補の `observe` と `observe --json --run <runId>` で実際の修復課題ログを読み、
構造化診断が人間向け表示へ出ることとrun別JSON取得を確認した。
新契約の横断課題は上記実model probeと公開Host/CLI 4wireの結合testで確認した。

初回未実施だったOpenAI OAuth専用exampleとtask-loop全workflowは、下記追加検証で完了。残る未実施は実vision/画像生成/Exaの品質、
macOS/Windows/ARM実機での実行、長期常駐・統計的なcache/効率比較。
未解消のP2以上のfindingはない。独立reviewの各指摘とTで親が発見した位置欠落は修正済み。
managed engineは未公開候補であり、公開用の正式なengine依存・upstream公開は今回行っていない。
コミット・配置・リリースも今回の対象外で、既存未追跡実行ログ4件を維持している。

## コミット

実装・検証完了後の利用者指示により、今回の変更をコミットする。既存未追跡実行ログ4件は含めず、配置・公開は行わない。

## コミット後のOAuth例・全workflow追加実行

利用者の追加実行指示を受け、コミット`af8c2a6e0ba2767c91a1d8734fdd018ce7cd22ff`から
CGO無効でCLIをビルドし、既存NOE認証を使って実providerへ接続した。
保存先は `/var/tmp/noe-oauth-workflow-20261023-165717`。
`manifest.json`、`execution-results.json`、`verification.json`と各events/stdout/stderrを保存。
設定は空の専用configと明示credential pathで、URLのfixture置換はない。credential内容は記録しない。

- `agent-openai.js`: 元sourceの一時コピー、既定の`openai-chatgpt / gpt-6-luna / auto`。
  OAuth自動refreshが成功して有効期限が更新され、11.827秒・exit 0、回答は`437`。
  model 2回、run_js 1回、agent 1体。model/tool失敗0。
- `task-loop-jev/workflow.js`: サンプル一式の一時コピーをcwdにして元sourceを実行。
  `openai-chatgpt / gpt-6.1-sol / high`と`openrouter / typesafe/jev-1.14`。
  意図されたbaseline失敗を確認後、計画reviewが`revise`、修正後reviewとコードreviewが`proceed`。
  66.555秒・exit 0、report.outcome=completed、planRevisions=1、codeRevisions=0、waits=2、polls=0。
- model 7回、Jev 3回、run_js 2回、agent instance 4体（worker 1体、計画review 2体、コードreview 1体）。
  workerは同じinstanceで計画修正・実装の2 taskを処理。model/tool失敗0。
- workflow.js・requirements.txt・test.jsが初期hashとrepository原本に一致することを確認し、
  親が同じtest.jsをCLIで再実行して5項目すべて合格を独立確認した。
  修正されたsolution.jsはNumber.isInteger(value)かつvalue>0を加算する。
- 集計usageはOAuth例input/cached/output=3651/1536/39、workflow=18922/5248/1507（model分のみ、Jev除外）。
  いずれも単発の動作確認でcache性能比較ではない。

これにより有効なJS/MJS全51 filesが利用側を含む実行経路で確認済み。
元サンプル・製品sourceに変更はなく、追加したのは検証記録のみ。追加記録はv2.2.0の配布準備へ含めた。

## v2.2.0正式配布への調整

利用者の2.2.0リリース指示に従い、未公開github候補を正式な依存として残さず、管理engineを
`forge.harakara.site/littleisland/noe/third_party/go-quickjs` v0.28.1-noe.1へ移した。
engineとNOE双方のmodule/import pathを整合させ、独自Go bindingの型移行をリリースノートへ明記。
engine Goの変更はprefixのみで、333 filesの差分を機械確認。engine全suite・対象suite・外部consumerと
独立packaging reviewが合格。公開用のroot replace除去・実取得と最終release gatesは
[release checkpoints](../releases/release-checkpoints.md)へ記録する。
