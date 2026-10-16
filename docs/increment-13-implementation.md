# インクリメント13 実装・検証記録

[計画正本](increment-13.md) · [計画レビュー](increment-13-review.md)

ユーザーの指示に従い、各スライスで実装・テスト・コードレビューを完了して次へ進み、最後に総合テスト・全体レビューを行う。実APIは許可済みOpenRouterキーと`deepseek/deepseek-v4.1-flash`で確認する。

状態：2026-10-16、S1〜S7と最後の総合テスト・全体レビューが完了。以下に指摘の採否、修正後の確認、実接続と検証の証拠を記録する。

| スライス | 状態 | 検証・レビュー |
| --- | --- | --- |
| S1 SDK・アダプター・認証 | 完了 | fixture・race・vet・修正再レビュー合格 |
| S2 組込みmodule・内部RPC | 完了 | テスト・race・vet・修正再レビュー合格 |
| S3 子NOE実行基盤 | 完了 | runner・E2E・race・vet・6target・修正再レビュー合格 |
| S4 単体loop・自律修正 | 完了 | CLI縦テスト・race・vet・修正再レビュー合格 |
| S5 親子agent・並行処理 | 完了 | テスト・race・vet・独立レビュー合格（指摘なし） |
| S6 REPL・補完 | 完了 | runtime・PTY・回帰・race・vet・独立レビュー合格 |
| S7 実接続・ガイド・全体確認 | 完了 | 実OpenRouter3ケース・sliceレビュー・総合チェック・全体レビュー合格 |

製品実装後の成功を、以前のスパイクの結果で代用しない。Darwin/Windowsのnative動作は、実行できない場合クロスビルドと区別して記録する。

## 開始時の確認

- `go test ./internal/runtime ./internal/cli -count=1`：成功。既存動作の基準として確認。
- 各sliceの独立レビューは要件整合と具体的な失敗・取消し・回収経路を対象とし、初回15分以内、変更確認は一回15分以内で行う。

## S1：SDK・アダプター・認証

- 変更：`internal/agent/provider/{provider,openrouter,sse,errors}.go`とfixtureテスト、`go.mod/go.sum`。OpenRouter公式Go SDK `v0.9.59`を固定。
- 境界：`Provider.Generate(ctx, Request) -> Response`。履歴・tool・応答・usageはJSONで保持し、SDK型を公開しない。遅延ファイル認証、session_id、retry無効化、全要求の独自timeoutなし、取消しと分類したエラーを実装。
- SDK制約への対処：生成型で未知の履歴フィールドが落ちるためHTTP要求境界でRawMessageを送る。SDKのSSE iteratorを利用しつつ、HTTP bodyでSSEイベント・正常終端・読取りエラーを独立観測する。
- 検証：`go test ./internal/agent/provider -count=1`は親でも成功。implementerの`go test -race ./internal/agent/provider -count=1`、`go vet ./internal/agent/provider`、`git diff --check`も成功。ローカルfixtureのみ。
- 独立レビュー：critical reviewerへ依頼中。実キー・実APIはまだ使用していない。

### S1レビューの採否と修正

- **S1-C1 / P1 / 採用**：DONE受信後のHTTP EOF待ちで接続保持時に終了しない。EOF待ちを削除し、正常SSE終端・既観測read error・contextで判定してdeferでcloseする。DONE後の接続保持fixtureを追加して成功。
- **S1-C2 / P2 / 採用**：初期`content:null`で後続テキストが失われる。テキスト受信の有無を区別し、後続nullでも蓄積本文を保持。上記fixtureで最終本文を確認。
- C2に対応する異常切断fixtureは、正常finish後・DONE前のread errorへ修正。正常終端後のEOFを新たな成功条件にしない。
- 修正後providerテスト・vet成功。reviewerへ2件と変更箇所だけの再確認を依頼。

S1の修正後raceも成功。critical reviewerが2件の解消と残存必須指摘なしを確認し、S1完了。SDK v0.9.59の採用を確定する（実接続はS7で確認）。

## S2：組込みmodule・内部RPC

- 変更：`internal/agent/`のlazy broker・Host/Operation・埋込み`js/agent.js`、`internal/runtime/agent.go`とloader・Execute/Eval/Complete接続、CLI設定接続・テスト。
- 内部RPCは専用Go HTTP clientからPromiseへ戻し、`Loop.Begin/Post/Done`に参加。QuickJS操作はownerに限定。設定はruntimeへ明示供給し、moduleのキャッシュに評価contextを保持しない。
- CLI設定：認証パスに加え`NOE_OPENROUTER_BASE_URL`で接続先上書き、`NOE_AGENT_TRACE=1`でstderrへメタデータを出す。子用の一時接続情報は`NOE_AGENT_CONNECTION`としてCLIからruntime設定へ渡す。
- 親の検証：最小run、同じagentの再利用・並行runの履歴分離、設定エラー時の未通信、import/createのみの未認証動作、provider取消し、CLI run/eval/自動出力なし/timeout124が成功。変更箇所raceと既存Session/Completion/Worker/CLI関連テストも成功。
- `go test ./internal/runtime -run TestAgentInternalRPCLongerThanFetchTimeout -count=1 -v`：**30.27秒で正常完了**。公開Fetchの30秒制限を内部RPCへ持ち込まないことを確認。
- broker側のテストと独立コードレビューは進行中。tool実行とREPLの連続利用全体は後続sliceで確認する。

S2 brokerテスト・race・vetも成功。不完全/非loopback接続を診断し、親brokerへ接続するfixtureも成功。normal reviewerへS2全変更責務の独立レビューを依頼した。

### S2レビューの採否と修正

- **S2-N1 / P2 / 採用**：Host.CloseがServe終了しか待たず、取消し後handlerが戻る前にCLI診断やcallerのTrace読取りとraceになる。handler入場gateとWaitGroupを設け、新規入場停止・context取消し・接続停止後に進行中handler終了を待つ。並行Closeも最初のClose完了を待つ。
- 取消し後のproviderをreleaseまで止めるfixtureでCloseの待機と最終traceを確認。trace有効CLI timeoutを含む変更責務race、agent通常テスト、vet成功。normal reviewerへ変更確認を依頼。

S2-N1をnormal reviewerが再確認し解消・合格。reviewer実行の変更箇所raceも成功。S2完了。

## S3：子NOE実行基盤

- 変更：`internal/agent/runner.go`とbuild-tag別`process_posix.go/process_windows.go`、focused runnerテスト、`tests/agent_runner*_e2e_test.go`。`Runner{Executable,Cwd,Connection}.Run(ctx,code)`が新しいNOEを`run <temp source>`で起動する。
- 専用temp source、事前束縛createAgent、cwd・source-relative import、stdout/stderr/exit/reason、明示pipe収集、providerキー値env除外を接続。POSIX group、Windowsはsuspended開始→Job割当→resume。
- `go test -tags=e2e ./tests -run TestAgentRunner -count=1 -v`：7テストすべて成功。`go test -race -tags=e2e ./tests -run TestAgentRunner -count=1`も成功。fresh globals、cwd/import、例外/exit7、env、timeout/pre-cancel、root/sh/sleep取消し、通常終了後の背景子停止、子が保持するpipeの終了を確認。
- Linux PID検証は、直接所有するNOEのWaitとPID消失、および通常の子孫の停止を確認する。再parent化された子孫のゾンビはOS reaper管理として実行中と区別する。
- implementerのagent通常/race/vetも成功。critical reviewerへS3独立レビューを依頼。Darwin/Windowsはnative未確認。

S3配布ビルド：Linux/Darwin/Windows各amd64/arm64の6targetすべてCGO_ENABLED=0成功（artifact `/tmp/noe-s3-builds-20n1q1e2`）。native実行確認とは区別する。

### S3レビューの採否と修正

- **S3-C1 / P2 / 採用**：Windows TEMPとcwdが別volumeだと、生成ソースからcwd moduleへの相対importが成立しない。異volume時だけcwd内に専用一時領域を作るよう修正する。loaderの既存契約は維持。
- この配置時はcwdへの一時書込みが必要。書込みできない場合は利用者が既存TEMP設定を同volumeの書込み可能な場所へ指定する。正本へ条件を追記。Darwin/Windows native未確認は維持する。

S3-C1修正後：agent通常テスト/vet、Windows amd64/arm64 test binaryのクロスコンパイル成功。Windows volume判定fixtureは追加したがnative実行は未確認。critical reviewerへ変更確認を依頼。

critical reviewerがS3-C1の解消と残存必須指摘なしを確認、S3完了。

## S4：単体loop・run_js・自律修正

- 変更：埋込みJSの反復loop・run_js schema、brokerのrun_js route/child scope、js_resultとcache/cost metadata、実CLI fixture `tests/agent*_e2e_test.go`、最小例 `examples/agent-summary.js`。
- JSがassistant応答・reasoning/tool IDを履歴へ保持し、複数tool結果を対応付ける。不正引数とJS失敗はResultとして返して自律修正、最終回答は文字列で返す。maxStepsはmodel call数、maxTokensは要求ごとの出力上限。
- 親の実CLI fixture：ファイルcount4/total100/average25、2 tool callとprocess分離、reasoning拡張の次要求への保持、不正引数→JS例外→修正100、maxSteps・length・401・DONE欠落・空応答の失敗、cache/costのtrace、privateデータ未記録が成功。
- `go test -race -tags=e2e ./tests -run TestAgentCLI -count=1 -v`：成功。LinuxでCLI timeout124、生成NOEのWait/PID消失、sh/sleepの停止も確認。既存S2該当runtime/CLIテストとvetも成功。
- agent package側のテストと独立コードレビューを続ける。親子並行処理の受入はS5で行う。

S4 agent通常/race/vetも成功。実装を固定しnormal reviewerへ履歴/tool往復・上限・scope回収を対象に依頼。

### S4レビューの採否と修正

- **S4-N1 / P2 / 採用**：tool call IDのrun全体一意性を要求し、別turnの同IDによる修正を止める。重複判定を同一assistant応答内へ限定。
- reviewerが実CLI/local SSEで再現（2回目でcode1）を確認。親は既存自律修正E2Eを同じID `work`の失敗→修正版へ変更し、4model callsで100まで完了することを確認。該当E2E race成功。normal reviewerへ変更確認を依頼。

normal reviewerがS4-N1の解消を確認し、対象E2E raceも成功。S4完了。

## S5：親子agent・並行処理

- start RPCでresolved provider/model/name/maxTokensをrunごとに保持。生成JSへ渡すdefaultsは起動元runのprovider/model/maxTokensだけで、name/system/history/cacheKey/maxStepsを継承しない。
- child Operationを親runへ登録し、finish・異常終了・operation closeでcancel/再帰close/handler barrier後に回収。traceはname/model/parentRunIdを追加。
- 実CLI fixture4件成功：2子のfirst model要求を両方待つgateで並行再入を確認、各子JSの30/70を親が100へ統合、設定継承/明示override/system分離/親子traceを確認。並行した異設定の2親でdefaults分離と明示maxTokens0を確認。bad.runのfinish後markerまで待つgood.runが完了。generated JS process.exit(9)でfinallyを飛ばした待機childのcancel・trace回収も成功。
- Linux nested timeoutは親側/子側2つのNOE process group、sh/sleep停止と直接所有NOEのWait/PID消失を確認、CLI124を維持。
- `go test -race -tags=e2e ./tests -run 'TestAgent(ParentTimeout|CLI|Children|ChildDefaults|FailedRun|GeneratedExit)' -count=1 -timeout=45s`でS5とS4関連ケース成功。agent通常/race/vet、runtime/CLI該当回帰、e2eを含むvetも成功。critical reviewerへscope再入/終了待ちと独立性のレビューを依頼。

S5 critical reviewerはscopeの受付停止→Waitと、共通lockを解放してchild Closeする待機順を確認。必須/任意指摘なしで合格、S5完了。

## S6：REPL・補完・評価context

- S2で先行接続したcurrent-operation hookを、`internal/runtime/agent_session_test.go`と`tests/agent_repl_terminal_e2e_test.go`で検証。追加のproduction変更を必要とする失敗は見つかっていない。
- runtime：cached moduleと同じagentを別Evalで利用、前Eval context取消し後も成功。provider失敗はrecoverableで、次runは新履歴。import/createでは通信せず、通信中の中断はprovider取消しとsession close。
- Complete：Proxy trapがagentを開始し、native RPCのstarted traceを同期probeで待つ。現在inputCtxで開始できたこと、Pending判定後のcancelled trace、session終了、後続Eval不可を確認（古いcontextなら開始せずtimeoutする）。
- 実Linux PTY：import/createから次promptへ戻る、同agent再利用と401後の復帰、Ctrl+Cでモデル通信取消し・session終了、Tab trapによるRPCの回収とsession終了、termios復元を確認。
- `go test ./internal/runtime -run 'TestAgent(Session|Completion)' -count=1 -v -timeout=30s`、`go test -race ./internal/runtime -run 'TestAgent(Session|Completion)|TestSessionComplete' -count=1 -timeout=30s`、`go test -race -tags=e2e ./tests -run TestAgentREPL -count=1 -v -timeout=45s`、対応vet：成功。normal reviewerへ要件/現在context/終了契約の確認を依頼。

normal reviewerがS6要件/current context/終了契約を確認し、対象runtime/既存Complete/PTYのraceも成功。必須/任意指摘なしでS6完了。

## S7：実接続・利用ガイド

- README日英、`docs/api.md`・`docs/repl.md`・`examples/README.md`を更新。認証path、root model、run/child設定、任意上限、履歴分離、trace、生成JSとWindows temp条件を記載。summary/parallel例とサンプルJSONを追加。
- モデル向け共通instructionにcreateAgent/run/Promise.allの使い方と継承・返却値を明記し、固定stdlibのcommand API（async spawnなし）も案内。機能/APIを増やす変更ではない。
- 実接続は通常テストから除いた`e2e && agent_live`タグの`tests/agent_openrouter_live_test.go`で明示実行。Go hostだけが許可済みファイルを読み、ファイル移動・複製・キー値出力なし。モデルは指定の`deepseek/deepseek-v4.1-flash`、SDK v0.9.59、Linux amd64 Go1.27.2。
- [実OpenRouter観測記録](increment-13-openrouter-results.json)：aggregate/repair/parallelの3ケース全成功、全CLI code0。count4/total100/average25、JS失敗→model判断→修正版成功、2子JS30/70→親100を確認。
- 実traceのparallelでは、子right started（index2）・left started（index3）が、最初のchild完了（index7）より先に記録され、runの重なりも観測。local gated fixtureでmodel要求の並行性を別途実証済み。
- この試行でmodel call **14**、JS **9**、cached tokens **5,632**、API返却cost合計 **0.003895594**。成功・cache hit・文章の一致を今後の試行へ保証しない。最終回答は実際にprose/fenced JSONを含み、そのまま記録。検証だけが中の数値を照合し、runtimeの返却文字列は加工していない。

実行コマンド（明示タグはOpenRouterへ実接続する）：

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/allowed/openrouter-api-key \
NOE_AGENT_LIVE_RESULTS=/absolute/path/to/noe/docs/increment-13-openrouter-results.json \
go test -tags=e2e,agent_live ./tests -run '^TestAgentOpenRouterLive$' -count=1 -v -timeout=10m
```

各fixtureは検証用に`maxTokens:4096`・`maxSteps:8`とCLI120秒を明示している。製品の未指定時上限とは別。認証の内容はdriverでは読まず、CLIのGo hostが読む。

S7 sliceレビュー後、最後の通常/E2E/race/vet・6target buildと全体レビューを行う。

S7 normal reviewerが文書・例・共通prompt・live testの明示分離・実観測記録の整合を確認し、必須/任意指摘なしでsliceレビュー合格。最後の総合チェックへ進む。

## 最終総合検証

[総合検証記録](increment-13-validation.json)に実行環境・正確なコマンド・終了コード・出力を保存。Linux amd64、Go1.27.2で以下はすべて成功。

- `CGO_ENABLED=0 go build -o noe ./cmd/noe`
- `go test ./... -count=1`
- `go test -tags=e2e ./... -count=1`（実APIなし、既存機能も含む）
- `CGO_ENABLED=1 go test -race ./internal/... -count=1`
- `go vet -tags=e2e ./...`
- `CGO_ENABLED=0`でLinux/Darwin/Windows各amd64/arm64、計6targetのビルド。

Darwin/Windowsのnative実行は未確認。クロスビルド成功と区別する。sliceレビュー後の全体reviewは、通常観点（公開API・仕様・利用手順・回帰・証拠）と批判的観点（SSE・Promise・scope・並行処理・取消し・プロセス回収）を独立して確認する。

### 受入条件への証拠の対応

| 条件 | 実装後の確認結果・主な証拠 |
| --- | --- |
| A1 | runtime/CLIの最小run・evalとLinux PTYのimport/create/reuse。S2・S6の記録および通常/E2E成功。 |
| A2 | `TestAgentCLIReadsFilesAndPreservesToolHistory`と実接続aggregateで4/100/25。 |
| A3 | `TestAgentCLIFixesFailedJavaScriptAndMalformedArguments`と実接続repairで失敗→修正→成功。fresh processはrunner E2Eでも確認。 |
| A4 | `TestAgentChildrenRunConcurrentlyAndInheritSettings`のgated model要求と実接続parallelで2子のJS、30+70=100。 |
| A5 | runtimeの独立履歴、runnerのfresh globals/cwd/import、`TestAgentChildDefaultsBelongToLaunchingRun`の設定継承・分離。 |
| A6 | provider fixtureのfinish/read error/DONE/取消し・close・認証分類とCLIの上限/不完全応答、tool/reasoning往復。S1・S4に修正レビューを含めて記録。 |
| A7 | 30.27秒の内部RPC成功。runner/CLIのroot・nested timeout、正常/異常終了、独立run維持、生成JSのfinallyを飛ばした子回収。S2〜S5に記録。 |
| A8 | cached module再利用・エラー回復と現在Complete context、Pending・取消し・session終了、Linux PTYのCtrl+C/Tabと端末復元。S6に記録。 |
| A9 | provider fixtureの遅延認証・パス優先順位、runnerのsecret env除外、CLI traceのprivate内容非出力。実接続でも許可済みキーを移動・複製せず使用。 |
| A10 | 最終の通常/E2E/race/vet、CGO0通常ビルドと6targetビルドすべて成功。Linux native、Darwin/Windows native未確認。 |

## 最後の全体レビューと完了判断

各sliceのレビューとは別に、実装全体を通常・批判的のread-only reviewerへ依頼。各15分以内、認証読取り・実API・変更なしで実施した。未追跡の新規ソースも確認対象に含め、スパイクの過去結果を本体の証拠に代用していない。

| 観点 | 確認範囲と結果 |
| --- | --- |
| 通常 | 公開API・A1〜A10、agent/provider/broker/runner、runtime/CLI・REPL、設定/履歴/認証/上限、テスト・ガイド・例・観測記録を照合。新しい必須・任意指摘なし、合格。 |
| 批判的 | SSE終端/read error/contextから履歴/tool往復、native Promiseの所有goroutine、Execute/Eval/Completeのoperation寿命、受付gate/再帰Close、親子回収と独立runを横断。自己待機・親子待機循環・前評価context再利用の具体的経路は認めず、新しい必須・任意指摘なし、合格。 |

両reviewerはsourceと保存済み証拠を確認し、親が実施済みの総合テストや実APIを重複実行していない。defaultも証拠の対応、文書リンク、Go書式、`git diff --check`を確認。開発記録の旧「NOE固有はCLI/REPLのみ」という説明と通常E2E/明示liveの説明を更新し、通常reviewerも整合を確認した。最後の変更は文書のみ。

defaultは、各sliceの必須指摘が修正再レビューで解消され、全体の新規指摘がなく、受入条件の証拠と総合チェックが揃ったため、今回の実装を完了と判断する。Darwin/Windows nativeは未確認。モデルの将来の応答一致・cache hit・厳密JSONを保証しない。コミット・release・公開は行っていない。

## v1.2.0公開後の追加：session_idの自動付与 — 2026-10-16

ユーザーの追加要件により、`cacheKey`未指定時はrunごとに既存brokerが生成するランダムな`runId`を利用し、OpenRouterの`session_id`へ送るよう変更。JS loopの開始時に一度解決し、そのrun内の全モデル要求で固定する。子agent、同じagent objectの再利用、並行runも個別のIDを持ち、traceの`runId`と照合できる。

明示した`cacheKey`は従来どおり使用し、空文字は`session_id`省略を維持する。公開APIやSDK依存の追加はない。モデル向けの子agent利用説明、APIガイドとREADME日英も更新した。未指定時に毎回新しいIDになるため、runをまたいで同じrouting用IDを使う場合は明示指定する。キャッシュヒットや会話の永続化を保証する変更ではない。

実装後の確認はすべて成功（Linux amd64、Go1.27.2）：

- `go test ./... -count=1`
- `CGO_ENABLED=1 go test -race ./internal/runtime -run '^TestAgent(MinimalRunHasIndependentHistories|LazyImportAndConfigurationErrors)$' -count=1`
- `CGO_ENABLED=1 go test -race -tags=e2e ./tests -run '^TestAgent' -count=1 -timeout=90s`
- `go vet -tags=e2e ./...`
- `go build -o noe ./cmd/noe`

既存fixtureを拡張し、未指定runのID生成・再利用/並行run間の分離、明示値と空文字の維持、修正loop内の4要求でのID固定、親/子の実HTTP要求での個別IDとtrace対応を確認した。追加の実API呼出しは行っていない。defaultが変更箇所と証拠を確認し、別agentへのレビュー委譲は行っていない。リリース済みv1.2.0の検証記録とは別の、公開後のローカル変更である。
