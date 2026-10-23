# AI向け実行改善：実施・検証記録

正本は[見直し計画](ai-execution-contract-plan.md)と[S1設計](ai-execution-contract-design.md)。製品の基点はv2.1.1復旧後の`5781755`。

## S1：設計・レビュー

2026-10-23完了。計画・設計・索引を`e5bbe1e`へコミットした。通常・批判的レビュー、指摘3件の修正・再確認は[S1の記録](ai-execution-contract-design.md#9-s1レビュー)を参照。

## S2：既存静的ツールの利用案内

2026-10-23完了。利用者の追加条件「既存静的ツールは環境にないケースもある」を反映した。

成果物は[JS検査ガイド](../javascript-checks.md)。docs一覧・agent起動ガイド・API説明の入口から案内する。
両ツールあり、片方だけ、両方なし、起動・設定・型情報の問題を区別し、未検査を合格へ変換しない。未導入だけで元の依頼を一律中断せず、必要な確認を進める。
新しいインストール処理、検査API、自動実行、ラッパー、製品依存、CI検査は追加していない。製品コード・設定も変更していない。

### 利用例の実行確認

文書のJS・JSON code blockを読み取り、一時projectへそのまま配置して確認した。repoの`biome.json`を変更したり、一時projectへ継承させたりしていない。

- 環境: Linux amd64、Node v24.21.0、PATH上のBiome 2.5.15・TypeScript 5.9.3。
- cwd: `/var/tmp/noe-s2-checks-q3arremn`。
- 設定: 文書掲載の`biome.json`、`tsconfig.noe-checks.json`。
- 対象: `generated/task.js`。正常例と、構文不正・型不一致・実行時失敗へ変更した各内容。
- command: `biome lint --config-path ./biome.json ./generated/task.js`、`tsc --project ./tsconfig.noe-checks.json --pretty false`。
- 個別出力・結果: 同cwdの`results.json`と各caseの`.txt`。

| 内容 | Biome終了code | tsc終了code | 確認した意味 |
| --- | --- | --- | --- |
| 文書の正常例 | 0（1ファイル処理） | 0 | この設定による構文・lint・JS型検査が成立 |
| `export const = 1;` | 1 | 2 | 構文不正を検出 |
| `sum(["30"])` | 0 | 2 | このlint規則では合格でも、JSDocに対する型不一致は検出 |
| `export default JSON.parse("{");` | 0 | 0 | 静的検査だけでは実行時成功を保証しない |

最後の例は既存のv2.1.1バイナリで`noe run generated/task.js`も実行し、SyntaxError・終了code 1を確認した。外部サービスや実providerは利用していない。
一時PATHへ実行ファイルへのsymlinkを置く方法で「両方あり・Biomeのみ・tscのみ・両方なし」の`command -v`結果も確認した。取得・インストールはしていない。

### 文書レビューと残る範囲

親が文書と利用例をレビューし、任意ツールを必須依存へ変えていないこと、検査範囲と実行結果を混同しないこと、projectの既存設定を上書きしない案内、各guideからのリンクを確認した。
TypeScript公式資料のJS検査・noEmit・types・libと、Biome公式CLI・設定仕様も参照した。今回の文書変更のためにGo全体suiteや既存の重い検査を繰り返していない。

S2終了時点では`stdlib/types/noestdlib.d.ts`・`agent/types/noeagentlib.d.ts`は未提供だった。NOE固有APIを使う型検査と2つの宣言ファイルを参照する完成例は、下記のS3で実装・確認した。
S4の実行結果・診断変更、Tの総合検証も未着手。S2の確認結果を、それらの検証済みの証拠にはしない。

## S3：標準API情報・型宣言

2026-10-23完了。利用者の「s3」を受け、実装・対象検証・コードレビュー・指摘修正を行った。S2の未コミット変更を保持したまま進め、コミット・配置・公開は行っていない。

### 実装

- [noestdlib.d.ts](../../stdlib/types/noestdlib.d.ts): NOE管理の26 Node-style moduleとbare alias、7つのnoe:* module、NOE管理globalの型。engineの標準ECMAScript型はTypeScriptのlibを使う。
- [noeagentlib.d.ts](../../agent/types/noeagentlib.d.ts): agent/model/image/decision/auth/logの6つの公開module。内部moduleを公開型へ含めない。
- 両ライブラリの型は別ファイルのまま。情報の所有者も分け、共用の既存`internal/apidocs/content`から宣言path、公開名・alias、利用条件、公開定義のsourceを案内する。
- [JS検査ガイド](../javascript-checks.md)へ両宣言を使うNOE APIのJS例・tsconfigを追加。未導入や型不足の扱いはS2どおり。新しい共通検索・問い合わせ基盤やcheckerは追加しない。
- 各overviewで、型・docsの存在と現在のProfile/bindingの能力を区別。`noe:http`のFetch/Serve別export、Worker作成権限、auth/logの独立Bind等を明示した。
- 既存docsの`node:dns`をPromise方式とする誤記をcallback方式へ訂正した。Promise方式は`node:dns/promises`。実行APIの動作は変更していない。

noestdlib型とagentlib型は担当を分け、親がdocs・登録exportとの照合・統合検証を担当した。型宣言を成立させるための上流改変、依存追加、module path変更、NOE実行処理の変更はない。型宣言に`any`による穴埋めはない。

### 検証

環境はS2と同じTypeScript 5.9.3。ES2022、`types: []`、strict、noEmitで、DOM型や`@types/node`なしに確認した。恒久fixtureは各ライブラリの`types/testdata`へ配置した。

| 対象 | 実行・結果 |
| --- | --- |
| noestdlibの正負例・alias | `tsc --project stdlib/types/testdata/tsconfig.json --pretty false`: exit 0 |
| agentlibの正負例 | `tsc --project agent/types/testdata/tsconfig.json --pretty false`: exit 0 |
| guide掲載のJSと2宣言 | 掲載code blockを一時projectへ配置し、`tsc --project ./tsconfig.noe-api-checks.json --pretty false`: exit 0。既存NOEでの実行もexit 0 |
| 公開名・alias | 既存v2.1.1 runtimeの65 moduleの実exportと、既存TypeScript compiler APIで取得した宣言のvalue exportが完全一致。宣言した52 global名もruntimeに存在 |
| catalog・alias索引・元APIの無評価性 | `internal/apidocs`、`internal/modulemap`の対象テストと`stdlib.TestDocsReferenceDoesNotLoadOptionalAPIs`が合格。agent/FSなしのProfileでもdocsを取得でき、対象loader呼出しは0 |
| runtime経由のdocs | 既存`TestDocsRuntimeContractAndCatalogExports`を強化し、moduleの実exportとdocsの明示一覧を双方向で照合。Node以外のnoe:*とbare aliasも含めて合格 |
| Worker・agent・CLI経由のdocs | `TestDocsInWorkersAndNestedWorkers`、`TestAgentLooksUpDocsThenUsesAPI`、CLIの`TestDocsBypassesAgentConfiguration`・`TestDocsUsageAndErrors`が合格 |

結果・一時probeは`/var/tmp/noe-s3-validation`へ保存した。`final-tsc-results.json`、`types-inventory.json`、`agent-regressions.json`、`stdlib-regressions.json`が各検証の記録。runtimeのexport列挙は検証fixtureであり、製品のdocs取得へ評価処理を追加したものではない。

TypeScript compiler APIを使う一時検証で、誤った旧宣言をメモリ上で復元すると正負fixtureが失敗することも確認した。対象はprovider省略、contextのnull、commandの広いencoding、DNS module/Resolverの混同、resolve4の返却shape、空hostnameのnull。負例の期待エラーを外した際に未定義名エラーがないことも確認し、import不足による偶然の合格を解消した。

`runtime-shapes.json`では、Resolver instanceのlookup不在とserver操作、空hostnameのaddress:null、MessagePortのinstanceof、Worker.terminateの初回number・終了済みundefinedを実動作で確認した。このprobeも実providerやDNS問い合わせを行わない。
Go全体suite・race・Tの総合検証は今回の資料・型変更だけを理由に繰り返していない。CIも変更していない。

### コードレビュー・修正

通常担当はagentの6moduleと主要Go結果型、批判的担当はfs/command/DNS・native I/O・主要Streams/HTTP型と関連資料を確認した。各初回15分以内、read-only、再委譲なし。両担当に原文要件・許可範囲を渡し、変更箇所の再レビューを一回行った。

| 指摘・確認事項 | 対応 |
| --- | --- |
| generateの必須providerがoptional、contextの実際のnullを除外 | 実装の必須/nullableへ修正。undefinedだけ除外した誤用とprovider省略をfixtureで検出 |
| commandの既定出力・buffer指定・変数で広がるencodingの誤推論 | 確定bufferはUint8Array、確定text/未指定はstring、不確定encodingはunionへ修正。最後の変数経路は親が変更箇所と正負fixture・旧宣言への復元で解消を確認 |
| resolve4/6の返却をobject配列とし、DNS moduleとResolver instanceを混同 | string配列へ訂正し、module/instanceを分離。server操作、存在しないlookup/cancel等を正負例で確認 |
| 負例が旧optional型でも失敗し、別の負例はimport不足を隠していた | 元の誤りへ戻した場合に期待エラーが未使用となる例へ修正。親が復元検証で確認 |
| 親の追加確認: 空hostnameのnull、Worker終了値、MessagePortのinstanceof | 実sourceとruntime probeに合わせて型・例・Worker説明を訂正 |

実装担当の自己レビューではBufferの対応encoding/default、MessagePortの直接new不可、Node WorkerとWeb Workerのoption差、SHARE_ENV、URLPatternのmutable fieldも実装に合わせた。
親が最終変更と対象検証を確認し、残る確定P1/P2/P3なしと判断した。独立レビューは優先対象に限り、crypto・VM・全Web APIの各memberを網羅確認したものではない。型検査はruntime成功や現在のProfileでの利用可能性を保証しない。

S3終了時点の次工程はS4だった。下記でその結果を記録する。Tの総合検証はその後に行う。

## S4：実行結果・診断・配送

2026-10-23完了。利用者の「s4を」に従いlocal実装・対象検証・通常/批判的コードレビューと指摘修正を行った。S2/S3の変更も保持し、コミット・配置・公開はしていない。

### 実装と移行

- `internal/jsexec`は元のSource/loader originを維持した無export wrapperを実行し、defaultだけを待機する。元namespaceのnamed thenをPromiseへ渡さず、関数の自動呼出しもしない。内部module登録はsource prepare後に行い、entry/定義済みmoduleとの衝突を避ける。
- 初期化前にPromise/JSON操作を捕捉。VM ownerで一度だけJSON化し、変換が追加した仕事も同じLoop/contextで処理する。未処理例外/取消しではdataを破棄し、最終exitCodeも再取得する。
- `stdlib.Result`にData/Diagnostics、公開のExecutionDiagnostic等のaliasを追加。`Diagnostic(err)`とerrors.Is/AsのGo cause追跡は維持する。script/Worker entry方式は維持。
- `internal/executionresult`はengine/providerをimportしない共有Goデータ。診断はcode/stage/messageと、得られたstack/position/own-data例外詳細。command bytesはbase64にし、code/signalはscalarに限定する。
- agentexec/broker/runnerはdataとdiagnosticsを転送し、診断のstderr追記と暗黙agent importを廃止。既存TaskHistoryが結果全体を保持する経路を使い、raw/effective hook結果・任意afterTool加工・provider native履歴・画像の別配送・session/cacheは維持する。
- task自体の診断は、report前後に関係なくexecution.diagnostic eventへ保存する。agentId/commandIdと、判明している時だけrunIdを付ける。TaskOutcomeの公開shapeは増やさない。
- CLI観察とログ解析も接続。diagnosticsは出力選択と独立に保持し、dataはログ解析の既存includeOutputへ従う。通常のstdout/stderr捕捉上限は変更しない。
- 初期Policy、内蔵run_js docs、[移行ガイド](../execution-results.md)、観察/events/API/Goガイドとexamplesの案内を更新。既存生成JS fixtureは必要なimportをsourceに明示し、互換preludeを隠してテストを通す方法は使っていない。

動的importで拒否された構文エラーは、公開APIから構造化位置を取得できない場合にpositionを省く。取得できたmessage/stack原文は保持する。位置補完のための再parse/source保管案は採用せず削除した。直接script評価で得られるSyntaxError.Positionは保持する。上流・module cache・go.mod/go.sum・CIは未変更。

### 対象検証

各コマンドの有効な結果を再利用し、失敗したfixtureや変更が影響した範囲だけを再確認した。実providerは呼んでいない。

| 確認範囲 | 結果 |
| --- | --- |
| runtime/公開stdlibのunit・race | 担当が`go test ./internal/jsexec ./stdlib -count=1`と同race版を実施し合格。defaultなし/null/Promise/thenable、JSON規則・失敗・後続仕事、非再実行、Go cause、command bytes、取消し、helper衝突、公開aliasを確認 |
| agent/agentexec | 担当のunit合格。明示import、raw/effective結果、setup/取消し/Go error診断、task report前後の診断を確認 |
| 外側runtimeとpublic agent/CLI等 | `CGO_ENABLED=0 go test ./internal/runtime ./agent/... ./internal/cli ./internal/loganalysis ./internal/noeprofile ./internal/apidocs ./internal/modulemap ./internal/observation -count=1`を実行。初回のpublic agent/CLI 4件は旧暗黙binding fixtureへの依存で失敗し、sourceを明示importへ移行後、失敗した4件を再確認して合格。他の確認済み結果は再利用 |
| 結果の配送 | 新規TestRunJSResultsReachHooksEventsAndNativeProviderHistoryでdata/null/返却なし・実stdout・例外診断・afterTool加工・rawイベント・次要求のnative reasoning/vendor項目を確認。既存command failure testも診断fieldを読む契約へ移行し合格 |
| 関連race | `CGO_ENABLED=1 go test -race ./internal/agent ./internal/agentexec ./internal/runtime ./internal/loganalysis -count=1`が合格 |
| affected E2E | JS修正ループ/不正引数、履歴、委譲権限、task waitの選択E2Eを実施。委譲権限の関数配列fixtureにも旧binding依存が残り修正後に当該case合格。別途、移行した5ファイルの生成JS fixture 10件も合格 |
| 型・docs/観察 | stdlibとagent両types/testdataのtsc合格。最終内蔵docs、CLI/ログ解析のdata/nullと診断分離テスト、リンク・差分空白も確認 |

親の結果は`/var/tmp/noe-s4-tests`へ保存した。`target-packages.log`は初回結果、`fixture-migration-recheck.log`はpublic agent/CLIの再確認、`related-race.log`、`affected-e2e.log`と`delegation-import-recheck.log`、`migrated-generated-js-e2e.log`とselection JSON、`final-docs-observation.log`が該当する。初回失敗ログを合格ログへ上書きしていない。

### コードレビュー

実装をruntimeとagent配送へ分け、親がCLI/log・Policy・移行・外側fixtureを担当。独立した通常担当はagent配送、批判的担当はruntimeを、各初回15分以内・read-only・再委譲なしで確認した。

| 指摘 | 修正・確認 |
| --- | --- |
| P2: task VM診断がstderr分離後に配送されない | execution.diagnosticを追加。report前後・stderr非混入のテストと通常担当の一回再レビューで解消 |
| P2: JSON後のexitCode更新を取りこぼす | 最終Loop後に再取得。同期toJSON/timer更新のfixtureと批判的担当の一回再レビューで解消 |
| P2: JSON化が追加した未処理rejectionのGo causeが消える | 捕捉済みthrow helper→公開CallContext/Unwrap→detach。errors.Is/Asとdata破棄のfixture、同じ再レビューで解消 |
| 親確認: 内部helperとentry/定義済みmoduleの衝突 | 登録時期と候補選択を変更。3つの衝突fixture、批判的担当の再レビューで解消 |

両担当とも変更箇所と既存finding解消に限る一回の再レビューを行い、残るP1/P2/P3なし。親が最終接続と対象検証を確認した。レビューの静的確認と実行結果は区別し、未確認範囲を網羅検証済みとは扱わない。

S4として完了。この時点で未実施だったTは、これらの結果を引き継いで下記で完了した。S2〜S4は`4389385`にコミット済み。リリース準備時に同じ重い検査を繰り返さず、CIにも同じ検査を追加しない。

## T：総合検証

2026-10-24完了。S4後の確認で、`examples/agent-server/test_server.py`の所有リソース検証用JSに`run_agent`の明示import漏れを発見した。現行ソースから作った一時バイナリでは`child.started`待ちが失敗し、importだけ補った場合は成功した。fixtureへimportを追加し、同じコードを共有するinterrupt・graceful shutdown・SIGINTの3テストを確認して合格した。

S3/S4で合格した対象unit・型fixture・raceを引き継ぎ、未確認のpackageとCLI E2E、外部consumer、標準利用経路を追加確認した。成果物は`/var/tmp/noe-t-validation`へ保存した。

| 確認範囲 | 結果・証拠 |
| --- | --- |
| agent-serverの修正 | `NOE_BIN=/var/tmp/noe-s4-audit-bin python3 examples/agent-server/test_server.py`の該当3テストが合格。`server-cleanup.log` |
| 残りのunit | 既に確認済みの15 packageを除く30 packageを選択し、`CGO_ENABLED=0 go test -json ... -count=1 -timeout=5m`で確認。testのある22 package、top-level 389 testが合格。選択は`unit-selection.json`、結果は`remaining-unit.jsonl` |
| CLI E2E | 通常E2E 99件のうち、S4で合格を記録済みの11件を再利用し、残る88件を実行。87件合格、任意MCP SDK bundleが未指定の1件はskip。`e2e-selection.json`・`remaining-e2e.jsonl`。実provider用build tagは付けていない |
| 外部Go consumer | 既存`check-stdlib-consumer.py`をstdlib/agentそれぞれに実行。localと、ローカルfile proxyへ作ったmodule zipを別modcacheへ取得する経路で合格。後者のconsumerにはreplaceがなく、正式`go-quickjs v0.28.0`でstdlib、agent portable/gohostを実行。`stdlib-consumer/`・`agent-consumer/`の`results.json`と`artifact-versions.log` |
| 公開Result契約 | zipから取得したstdlibの外部consumerで、defaultなし/null/Promise、VM終了後の構造化診断・native Go cause、JSON化失敗時の副作用一回を確認。`external-result.log`・`stdlib-consumer/artifact-consumer/resultprobe.go` |
| 標準利用経路 | CLI docsからnode:path/noe:agentを参照→生成JSを保存→Biome/tsc→fixture providerのrun_js→実行時診断→修正→data/stdoutを次要求とJSONLで受領。型不一致はtscで検出し、静的検査を通る実行時失敗も区別。両ライブラリの型宣言を同時参照。`contract-flow.py`・`contract-flow/results.json`・`contract-flow/requests.json`・`contract-flow/events.json` |

標準利用経路の一時検証では、空stderrがJSONL eventで省略される既存仕様を誤って必須fieldとしたため、最初のassertionだけ失敗した。runner結果は空文字、eventは空field省略を許す条件へ直し、同じ経路を再確認して合格。初回ログは`contract-flow-attempt1.log`、最終結果は`contract-flow.log`に分けた。製品のイベント仕様は変えていない。

unitのskip 4件は実provider用3件と専用processから起動されるheartbeat helper 1件。通常E2Eの任意MCP確認は、事前に用意した`@modelcontextprotocol/sdk` bundleがないため未実施とした。静的ツールが両方/片方/どちらもない場合の扱いと両宣言の型fixtureはS2/S3の結果を再利用した。実provider・長時間待機の任意suite・他OS向けcross buildは追加実行していない。今回の失敗で必要になった再確認を除き、既に有効なrace等を繰り返していない。

Tの確認で追加の製品不具合は検出していない。修正はテストfixtureの明示import 1行で、上流・依存・CI・配置済みバイナリは変更していない。この修正は本記録と同じコミットに含める。リリース・配置・公開は行っていない。

## 実装完了時の引き継ぎ（公開前、2026-10-24）

- 作業場所は`/home/agent/projects/noe`、branchは`main`。復旧基点`5781755`→S1設計`e5bbe1e`→S2〜S4実装`4389385`→本記録とagent-serverの修正、という状態。計画のS1〜S4・Tは完了し、実装・検証の残作業はない。次の作業は利用者の指示を受けて決める。
- 範囲は「既存静的ツールの利用案内」「noestdlib/noeagentlibがそれぞれ供給するAPI説明・型宣言」「実行結果・実行時診断」の3本。[計画](ai-execution-contract-plan.md)と[S1設計](ai-execution-contract-design.md)が正本。利用契約は[実行結果・移行](../execution-results.md)と[JS検査](../javascript-checks.md)を参照する。
- 上流への変更は事前に利用者へ明示して許可を求める。NOE側のnoestdlibを変更する許可と、go-quickjs等の上流変更の許可を混同しない。今回は正式`github.com/go-quickjs/go-quickjs v0.28.0`を使い、派生module・engine改造・独自の静的checkerは追加していない。撤回済みの旧設計を復活させない。
- Tの結果と任意試験の未実施範囲は上記のとおり。集約結果は`/var/tmp/noe-t-validation/summary.json`、S3/S4の詳細は`/var/tmp/noe-s3-validation`・`/var/tmp/noe-s4-tests`。一時成果物を正本にせず、本記録を完了状態として使う。同じ製品ソース・依存なら有効な検証結果を再利用し、セッション変更だけを理由にunit/E2E/raceをやり直さない。
- 配置は更新していない。引き継ぎ時にrepository直下の`./noe version`と`/home/agent/.local/bin/noe version`はどちらも`noe v2.1.1`と確認した。今回の検証用バイナリは`/var/tmp/noe-s4-audit-bin`。公開・push・tag・Release作成・配置はこのコミット依頼に含まれず、実施していない。リリース指示後も[AGENTS.md](../../AGENTS.md)の「ローカル修正→テスト→通常軽いリリース検証→軽いCI」に従う。
- 利用者の未追跡イベントログ4件（20261021の203157/215119、20261023の110042/112051）は変更・削除・コミットしない。

## 公開後の引き継ぎ（2026-10-24）

- 利用者がv2.2.0を欠番としてv2.3.0へ進むことを了承し、「では進めて」と指示したため、リリースノート・バージョン・利用ガイドを更新し、軽いリリース検証、コミット、main/tagのpush、CI公開、公開後検証まで完了した。[リリース記録](../releases/release-checkpoints.md#v230-release-validation--2026-10-24)が公開状態の正本。
- `v2.3.0`は`8e7c82ad980d2bf51a11b1615a28e01cc625ca4f`を指す。CI run 215（表示番号34）は成功。Release ID 147は公開済みで、6配布物のchecksumが一致し、公開Linuxバイナリのdata/診断配送と公開Go moduleのstdlib/agent利用・型宣言収録も確認済み。正式go-quickjs v0.28.0を維持し、実providerは呼んでいない。
- S2/S3/S4/Tの有効なソース検証を再利用した。追加検証はバージョンの対象subtest、native release build・docs・依存/リンク確認と、公開配布物・実moduleの成立確認。全体unit/E2E/race等を繰り返さず、CIもビルド・checksum・公開だけ。証拠は`/var/tmp/noe-v2.3.0-release-u241ahed`。
- 作業場所は同じrepositoryの`main`。この公開後記録はタグ後の文書コミットとし、mainへpushする。リリースタグは移動しない。未追跡イベントログ4件は保持する。
- 配置は未更新で、repository直下の`./noe`と`/home/agent/.local/bin/noe`はv2.1.1のまま。公開は完了し、未完了の実装・検証・公開作業はない。次の作業は利用者の指示で決める。上流変更の事前許可など、これまでの権限境界は維持する。

## 配置後の引き継ぎ（2026-10-24）

利用者の「配置して 上記の指示もファイルにして」に従い、公開済みv2.3.0のLinux amd64バイナリをrepository直下の`./noe`と`/home/agent/.local/bin/noe`へ配置した。両方のversionと公開checksumの一致を確認し、PATH上の`noe`もv2.3.0。旧版は`/var/tmp/noe-v2.3.0-release-u241ahed/installation-dip_3ak0`へ保存し、配置記録は同release evidenceの`installation.json`へ保存した。

相談したrun_jsのHTTP stream・並列収集課題は、repository直下の[run-js-stream-challenge.txt](../../run-js-stream-challenge.txt)へ原文のまま保存した。配置作業では課題自体は実行していない。この配置記録と課題ファイルは、その後の調査メモ・Zed利用ガイドとともに利用者の追加指示でコミットする。`.zed/`のlocal設定は既定どおりGit管理外とし、設定内容と再利用手順を[Zedガイド](../zed.md)へ残す。利用者のイベントログは変更しない。
