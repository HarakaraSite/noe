# NOE 次の機能候補

[ドキュメント一覧](README.md) · [開発ガイド](development/README.md) · [完了した機能](completed-features.md)

未着手・検討中の候補と、一部実装済みの機能の残件をまとめます。
`<一部完了>`は実装済み部分と残件がある状態、`<未完了>`は検討・未着手を表します。
未完了は実装の決定や優先順位を意味しません。現在の仕様は各利用ガイドを参照してください。

## 状況の要約

| 分類 | 件数 | 参照先 |
| --- | ---: | --- |
| [一部完了](#partially-completed) | 6 | 実装済み部分と残件を本文で区別 |
| [未完了](#not-completed) | 12 | 検討・未着手・保留中の候補 |
| [完了](completed-features.md#completed) | 23 | 別ファイルの実装・検証記録 |

第3階層の候補見出し単位で集計し、Jevの応用例を1件として含めます。
2026-10-24の整理では、完了項目の本文と旧REPLの判断を別ファイルへ移し、分類は維持しました。

<a id="completed"></a>

完了項目の詳細は[完了した機能と実装記録](completed-features.md#completed)、
棚卸しの経緯は同文書の[分類・照合の記録](completed-features.md#inventory)を参照してください。
実装前の案・旧APIは[検討履歴](archive/future-candidates-history.md)に残しています。

公開済み版は[v2.5.0](releases/v2.5.0.md)です。モデル向け出力予算はv2.4.0が既定3 KiB、v2.5.0が既定64 KiBです。v2.5.0のoutputBytesはstdout/stderrの捕捉にも連動し、最大256 KiB。最終返却JSONはmetadata・JSONエスケープも含めて予算内に収めます。[実行結果ガイド](execution-results.md)で版と契約を区別します。現行の責務・実行経路は[Agent内部の構成](development/agent-architecture.md)、過去の計画・検証は各実装記録を参照してください。

<a id="partially-completed"></a>

## 一部完了（実装済み範囲と残件）

### <一部完了> タスクループの完了待ちと観察 — 2026-10-20

利用者の当面の運用方針として、親NOEを`--events`付きで起動する。親・子・孫・同じinstanceのqueue taskは共通のJSONLへ記録し、必要なrunを`runId`で選んで読む。通常の状態確認には`observe_agent({agentId})`、thinking等の詳細確認には`observe_agent({logPath, runId, includeThinking: true, cursor?})`を併用する。単独`--events`での自動保存先は、起動時のcwdの`.noe/events/*.jsonl`として実装済み。保存は引き続き`--events`で選択し、常時保存へ変更する方針ではない。[イベント記録](events.md)・[観察API](observe.md)。

**完了待ち・保持結果の取得は完了。残る候補は、JSONL保存なしの実行途中の詳細ログ保持・live取得であり、当面保留する。**完了結果については2026-10-21の追加合意で、`wait_task`・live `observe_agent`の`includeAnswer:false`と、保持済み原文の範囲取得`read_task_result`を実装し、v1.16.0で公開した。原文の保持・寿命は維持し、追加モデルによる要約は行わない。`wait_task({agentId, commandId})`は実装済み。queueの受付・FIFO・人間向けCLIを維持し、特定タスクの保持済み結果を完了通知で待つ。初期タスクのIDも`spawn_agent`から返し、通常のJS・run_js・hookから利用できる。待機専用イベントは追加しない。[API](api.md)。

照合根拠：[`task_wait.go`](../internal/agent/task_wait.go)・[`task_wait_direct_test.go`](../internal/agent/task_wait_direct_test.go)・[`Go Host検証`](../agent/host_test.go)。自動保存先は[`internal/cli/events.go`](../internal/cli/events.go)。

<a id="一部完了-汎用の低レベルapi拡張と優先順位--2026-10-18"></a>

### <一部完了> 汎用の低レベルAPI拡張と優先順位 — 2026-10-18

目的は、MCP、LSP、常駐ワーカー、監視・対話スクリプトなどをJSで組み立てるための汎用機能です。NOEの拡張として検討し、stdlibの変更や上流採用を前提にしません。外部ESM・シェル・OSコマンド・ライブラリのNOE向け改修も選択肢です。既存APIとJS・CLIで足りる処理をGo必須とは扱いません。

| グループ | 現在の状態 | 残る検討 |
| --- | --- | --- |
| 1：プロセスと標準I/O | **完了**。`noe:process`の子プロセスハンドルとstdin/stdout/stderr、`noe:io`のホスト標準I/Oストリーム | [公開契約](process-io.md)・[実装記録](development/process-io-implementation.md)を参照 |
| 2：具体的な用途に合わせて追加 | **完了**。`noe:fs-handle`の位置指定I/O、`noe:sockets`のTCP・Unix接続/待受、`noe:signals`のOSシグナル購読 | [公開契約](low-level-io.md)・[実装と総合検証](development/low-level-group2-implementation.md)。stat/truncate/sync等の追加操作は後続候補 |
| 3：後続候補 | **未完了**。ファイル変更通知、端末操作 | ポーリングやCLIと比較。TTY判定・サイズ・raw mode・PTYは別々に評価 |

`execFile`は既に非同期、ファイルの逐次I/Oも既存stdlibで利用可能です。第1グループの追加価値は実行中の入出力・終了観察・停止をハンドルで扱えることです。Ctrl+Cの取消しは既にあり、JSへのOSシグナル配送は別候補です。元QuickJSのfd番号・`dup`・readiness callbackの再現は要件にしません。

照合根拠：[`internal/nativeio/host.go`](../internal/nativeio/host.go)で5 moduleを登録。位置指定I/Oは[`js/file.js`](../internal/nativeio/js/file.js)と[`file_test.go`](../internal/nativeio/file_test.go)、通信・シグナル・プロセスにも同packageの実装とテストがある。CLI内部の端末幅取得は、JS向け端末APIの実装とは数えない。

### <一部完了> MCPクライアントの利用整備 — 2026-10-18

**JSで外部MCPツールを呼ぶ経路は検証済みです。** 公式SDKのClientをESMへbundleし、`noe:process`を使う独自JS stdio transportを組み合わせた[スパイク](../scripts/spikes/process-io-mcp/README.md)があります。main・Worker・fixture agentの`run_js`で、初期化、tool一覧、並行呼出し、要求と応答の対応付け、大きなUTF-8応答、異常終了と子の回収を確認しました（[import mapの検証記録](development/local-import-map-implementation.md)）。

Node/npm/esbuildはbundleの事前作成に使い、実行時はNOEがJSを動かします。専用のGo製MCP APIは追加していません。2026-10-24に[Exa公式HTTPサーバーとローカルfixtureを使う通常利用例](../examples/mcp/README.md)を追加しました。SDKの事前bundleとimport mapでの配置、URLと任意keyファイルの指定を案内しています。製品としてのSDK自動配布・設定・接続先管理は未決定です。スパイクがあることと、製品として組込みMCPクライアントを提供していることは区別します。

照合根拠：[`noe-transport.js`](../scripts/spikes/process-io-mcp/noe-transport.js)と[`import_map_mcp_e2e_test.go`](../tests/import_map_mcp_e2e_test.go)。SDK bundleを事前作成し`NOE_MCP_SDK_BUNDLE`を指定するopt-inテストであり、未指定時はskipする。

### <一部完了> go-quickjs stdlibとNode.jsのAPI差異の説明整備 — 2026-10-17

現在の組込み[モデル向けfacts](../internal/agent/facts.md)は、NOEの実行環境と版が一致する内蔵docsの調べ方を説明します。Node-styleの全26 moduleの概要・export一覧と、優先APIの詳細を英文で収録しました。[参照方法](reference.md)と[実装・検証記録](development/builtin-docs-implementation.md)を参照してください。

現在の説明対象は、go-quickjs v0.27.0由来のNOE管理stdlibです。`execFile`のPromise、fsのWeb Streams、Node形式Workerのtype option、spawnSyncの起動失敗時statusなどの差異も該当topicに説明しました。全APIの個別詳細や全差異の列挙は完了条件にせず、実際の誤用や利用用途を基に説明を追加します。この「一部完了」は継続改善の分類であり、上記の既知差異に未説明の固定残件があるという意味ではありません。

照合根拠：[`stdlib-child_process.md`](../internal/apidocs/content/stdlib-child_process.md)・[`stdlib-fs.md`](../internal/apidocs/content/stdlib-fs.md)・[`stdlib-worker-threads.md`](../internal/apidocs/content/stdlib-worker-threads.md)と[`docs_test.go`](../internal/runtime/docs_test.go)。

### <一部完了> Web検索と検索APIの認証ラッパー — 2026-10-17

**ExaのJSライブラリと利用案内は実装済みです。** [examples/exa/](../examples/exa/README.md)の`exa.js`は`search(query, options)`を公開し、成功時は検索結果のJSONを返し、失敗時は`status`・`requestId`・`body`等を持つ`ExaSearchError`をthrowします。既定の`~/.config/noe/credentials/exa-api-key`または明示した`apiKeyFile`から、JS側でキーを読みます。このサンプルの既定パスは固定で、NOEホストのXDG設定解決と同じではありません。

既知URLは`run_js`の`fetch`で取得でき、agentは配置したライブラリやimport map・instructionsを通じてExaを利用できます。緊急に組込みWeb検索を作る必要はないという方針を維持します。

検索APIの認証をGoホストへ隠す組込みラッパーは未実装です。現在のExa JSサンプルと区別し、専用API・provider・配置方法の採用は必要性が出てから検討します。

照合根拠：[`examples/exa/exa.js`](../examples/exa/exa.js)の`search`・`searchError`・キー読込み。サンプルの既存検証は[`examples/VALIDATION.md`](../examples/VALIDATION.md)。

### <一部完了> 応用例：Jevによる起動設定プリセットの選択 — 2026-10-17

[agent-route-model.js](../examples/agent/agent-route-model.js)は実装済みです。`list_models`の候補と説明をJevに渡し、返却キーからprovider/modelを選んで親agentを起動します。専用ルーターモデルや本体の自動ルーティング機能は不要です。

ローカルfixtureで一連の経路を確認済みです。現在の例は`effort:"auto"`固定で、effortを含むプリセット選択は残る応用候補です。別の応用例として[Jevタスクループ](../examples/agent/task-loop-jev/README.md)もあり、判断結果とqueue・`wait_task`を組み合わせています。起動設定選択サンプル自体の有料実接続は未確認であり、`decide`そのものの実接続済み範囲とは区別します。[モデルカタログ](model-catalog.md)。

照合根拠：[`agent-route-model.js`](../examples/agent/agent-route-model.js)の`effort:"auto"`と[`catalog_test.go`](../internal/cli/catalog_test.go)のfixtureによる経路確認。

### <実装済み> 子の完了待ち中にsteerで親agentへ制御を戻す — 2026-10-25

<a id="未完了-子の完了待ち中にsteerで親agentへ制御を戻す--2026-10-24"></a>

v2.6.0で新しい`wait_tasks`を追加した。集合全体の完了または親owner runへの未適用steerで返り、子は継続する。既存`wait_task`の完了専用契約は維持する。戻ったtoolを終了して既存checkpoint/applyから親の次モデル判断へ進める。実executorで二子を未完了に保持し、trusted host／script-origin双方について親次要求への指示配送、その後の子の明示cancel／steerと結果再取得まで確認済み。Web UI拡張や任意JSの強制中断は含めない。

[実装・検証記録](development/steer-aware-wait-implementation.md)・[API](api.md#実行中taskへの指示と停止)。

#### 実装前の検討記録

利用者の要望：親agentが子の完了待ちに入っていても、人の追加指示を受けて待機を抜け、子の状態確認・追加指示・必要な停止などへ介入できるようにしたい。`examples/agent-server`のWeb UIはexampleとして作ったものであり、子を選んで停止するUI等の拡張は対応対象外。今回は要望と検討案の記録のみで、実装・API名・実装時期は未決定。

2026-10-25、NOEへ渡す[実装指示書](development/steer-aware-wait-implementation-task.md)を作成した。成功条件、維持契約、複数の子の待機解除、関連検証、自然な履歴trimの観測方法を記載した。指示書の保存時点では製品実装は未着手で、API名・返却形式は実装設計時に決める。

発端は`.noe/events/noe-events-20261024-224429-946e65d4bb5b6f10.jsonl`の確認。子が追加したテストには、観察callbackがイベント用mutexを保持して解除待ちする間に、解除する側も別イベントの同期発行で同じmutexを待つ循環待ちがあった。親は`run_js`内の`Promise.all([wait_task(...), wait_task(...)])`で子の完了を待ち、23:11:27に受け付けた「テストがデッドロックしている」というsteerも確認時点では未適用だった。テスト側の不備と、待機中の親へ制御を戻せない問題を分けて扱う。

現行の`wait_task`は子の完了または待機request/所有scopeの取消しを待つ。steerは次のモデル判断前に適用し、進行中の`run_js`を中断しないため、子の待機が戻るまで親モデルは対処を判断できない。根拠：[`task_wait.go`](../internal/agent/task_wait.go)、[`steer.go`](../internal/agent/steer.go)、[steerの現行契約](api.md#実行中taskへの指示と停止)。

検討案は、子の完了と親へのsteer受付を待ち合わせる専用の待機APIまたはtoolを設けること。追加指示が来たら子を動かしたまま親の待機だけを終了し、その理由をtool結果へ記録して、既存のsteer適用処理から親のモデル判断へ戻す。親がその後の観察・子への指示・取消し等を選ぶ。複数の子をまとめて待つ場合も、一部の待機だけが戻って残りの`Promise.all`に留まらない形にする。

既存の`wait_task`が返す完了結果と、追加指示による待機終了は区別する。待機解除を子の完了・失敗・取消しとして扱わず、親run/所有scopeの取消しによる子の一括回収も起こさない。任意の実行中JSの強制中断へ広げず、待機専用の経路で制御を返す方向を候補とする。通知と受付・適用イベントの順序、同時に子が完了した場合の結果保持は実装設計時に整理する。

<a id="not-completed"></a>

## 未完了（検討・保留）

<a id="microvm"></a>

### <未完了> 長時間agent作業用の隔離環境（Docker・microVM等）

大きく長い仕事は別の作業環境で、小さな修正は従来どおりNOE側で行う構想。成果はファイルをVCSのremoteへcommit/pushして届け、agentの最終応答の保存は必須にしない。送信先はホスト上のリポジトリ、Forgejo/GitHub等を想定する。

KVMやGoへのVMM直接組込みには限定しない。Dockerfileによる道具一式の共有、VS Codeからの途中参加、既存ツールを呼ぶ外部JSライブラリ（仮称dockerlib/vmlib）を検討する。採用方式・API・寿命・実装時期は未決定で、起動・接続・性能検証は未実施。

議論の経緯、候補比較と一次資料、libkrunの版の注意、vm201での読取り確認、未決定事項は[独立した検討記録](development/isolated-agent-workspaces-discussion.md)へ移した。今後の詳細は同文書へ追記する。

<a id="typescript"></a>

### <未完了> TypeScript実行とrun_tsの標準化 — 2026-10-21

2026-10-24の追加議論は[メリット・デメリットとexamples全51件の評価](development/typescript-discussion-20261024.md)へ保存しました。短い処理の型付け負担についての訂正、esbuildを基本候補とする理由、未決定事項を含みます。実装承認ではありません。

利用者との検討メモ。TS対応はv2.5またはv3.0の候補とし、具体的な版・実装時期・API契約は未確定。まずesbuild本家のGo APIを基本にし、実利用で不足する変換や不具合が出たら必要な差分を手直しする方針。今回の依頼は議論の記録であり、TS実行・型チェック・ツール名の変更は実装していない。

#### 実行と型チェックの分担

TSを実行するための変換と、型の正しさを検査する処理は分離する。NOE本体はTSをesbuildでJSへ変換し、既存のgo-quickjsで実行する構成を考える。esbuildは型チェックをしないため、型エラーがあっても変換・実行されうる。型アサーションは実行時の入力検証にはならない。

- 使い捨ての調査・短い処理は、`run_ts(code)`だけで変換・実行する。型注釈を毎回書く必要はない。
- 保存して繰り返し使う処理は、外部の`tsc --noEmit`やエディター・CIで検査し、`noe run app.ts`で同じファイルを実行する案。チェックしたファイルの本文を`run_ts`へ渡し直す必要はない。
- 型チェック用helperは外部JS・TSでもよく、NOE本体への`noe check`実装や`tsc`同梱をTS実行対応の前提にしない。helperはNOEの型定義・設定・import解決を実行環境と揃え、診断と終了コードを返す役割を持つ。

当初は`noe check app.ts`や実行前の毎回チェックも例に挙げたが、議論後半では外部helperでよいと整理した。毎回の型チェック、常駐チェッカー、チェック結果・変換結果のcacheは今回の必須要件にしない。必要なら後で選ぶ。NOEの公開APIの型宣言はv2.3.0で整備済みで、workspaceも正式宣言の参照へ移行した。[Zed環境の記録](completed-features.md#一部完了-zedでのnoeスクリプト開発環境の整備--2026-10-17)を参照。TS実行の追加は別候補とする。

参考となる分担は、Node.jsの型除去と外部`tsc`、およびDenoの`run`・`check`・`run --check`。Denoはチェッカーも同梱するが、NOEが同じ配布構成を採ると決めたものではない。[Node.jsのTS対応](https://nodejs.org/api/typescript.html)・[DenoのTS対応](https://docs.deno.com/runtime/fundamentals/typescript/)。

#### モデル向けの標準ツール

現在の`run_js`に代えて、TSも型注釈なしのJSも受け付ける`run_ts`を標準にする案。両方をモデルに公開すると従来の`run_js`が選ばれ続ける懸念があるため、モデルへ公開する入口を`run_ts`だけにし、旧`run_js`は古い呼出しを受ける互換入口として残す形を検討した。公開ツール・履歴・hook・イベントの移行契約は実装前に決める。

`run_ts`への標準切替まで含めるならv3.0の区切りにする案、v2.5でTSファイル実行や型定義を先行整備する案が出た。段階分けや版は未決定。TSを受け付けることと、実行前の型チェックを必須にすることは独立した判断とする。

#### 変換器の選択と交換しやすい境界

esbuildはGo APIの`Transform`でメモリ上のTSをJSへ変換でき、NOE実行時のNode.js/npm依存や変換用の外部プロセスを追加せず組み込める。速度は有力な選択理由だが、公式の大規模bundleベンチマークを短い`run_ts`の実測値とは扱わない。NOEでの変換追加時間・メモリ・バイナリサイズは未測定。[Transform API](https://esbuild.github.io/api/#transform)・[公式ベンチマーク](https://esbuild.github.io/faq/#benchmark-details)。

2026-10-21の確認時点で、esbuildの最新公開版はv0.28.2（2026-08-08）。作者は大きな新機能開発を休止している一方、保守・定期リリースと新しいJS/TS構文への追随を続ける方針を示している。現状のMCP SDKスパイクの固定版0.25.12を、将来の製品TS対応版として自動採用しない。[ロードマップ](https://esbuild.github.io/faq/#upcoming-roadmap)・[v0.28.2](https://github.com/evanw/esbuild/releases/tag/v0.28.2)。

別実装としてRust製のSWC・Oxc、Node.jsが使うSWCベースのAmaro、公式TypeScript 7系も調べた。Goへの直接組込みやすさからesbuildを基本とする。esbuildのフォーク自体は存在するが、今回確認した範囲では、TS対応を本家より積極的に保守する代替候補は見つからなかった。Netlify版は保守終了、yyx990803版は2021年の限定的変更、tooolbox版は古いGo Modules対応用。将来の不足に対して自分で小さな差分を持つ選択肢は残す。[SWC](https://swc.rs/)・[Oxc](https://oxc.rs/docs/guide/usage/transformer/typescript)・[Amaro](https://github.com/nodejs/amaro)・[Netlify版の保守終了案内](https://github.com/netlify/esbuild/blob/netlify/README.md)。

変換の入口はNOE側の一箇所へまとめ、入力をTS本文・ファイル名・必要な変換設定、出力をJS本文・ソースマップ・診断情報として考える。esbuild固有の型や設定を呼出し側へ広げず、import解決と実行はNOE側の責務にする。将来の公式Go APIや別実装を比較・交換しやすくするための小さな境界であり、大きなプラグイン機構を先に作る意図ではない。

#### TS 7系との互換性と公式Go API

TS 7.0は公式コンパイラのGo移植が中心で、版番号だけを理由にesbuildで変換できないわけではない。v0.28.2にはTS 7で禁止された曖昧な型キャスト構文への警告追加がある。ただし公式が構文エラーにする入力をesbuildが警告で受け付ける例でもあり、`tsc`との完全一致は保証しない。ファイル単位変換に合わせて検査側で`isolatedModules`を使うこと、`emitDecoratorMetadata`・通常の`.d.ts`生成はesbuildが提供しないこと、`tsconfig.json`の反映範囲が限定されることを考慮する。[TS 7.0](https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/)・[esbuildの制約](https://esbuild.github.io/content-types/#typescript-caveats)。

2026-10-20公開のTS 7.1 Betaには`typescript/sync`・`typescript/async`等のJS向けコンパイラクライアントAPIがある。これはesbuildのような直接組込み用Go APIとは区別する。公開Go APIの議論では、開発者Jake Baileyが2026-07-28に本家repositoryへの移行時にGo側を公開する意向を示し、翌日に「7.1より前かもしれない」と発言している。公開範囲・安定性・期日の確約ではなく、2026-10-21に確認した本家のparser・compiler・transpileは依然`tsc/internal/`配下で、外部から通常importできる公開Go APIは確認できなかった。「計画なし」とは扱わず、継続確認する。[7.1 Beta](https://devblogs.microsoft.com/typescript/announcing-typescript-7-1-beta/#typescript-7-1-api)・[公開Go APIの議論](https://github.com/microsoft/typescript-go/discussions/481)・[確認時の本家構成](https://github.com/microsoft/TypeScript/tree/main/tsc)。

#### go-quickjsとの接続と確認範囲

変換成功だけでNOE上の動作保証にはならない。対象engineの実装済み構文は活かし、必要な機能を変換する設定を決める。esbuildにはQuickJS専用targetがなく、`target`・`supported`は構文互換性の指定であり、不足するWeb/Node APIを自動追加しない。TS対応によりnpm探索やNode/Deno完全互換が付くわけではない。[targetの仕様](https://esbuild.github.io/api/#target)。

QuickJS-NGは最新のECMAScript仕様への追随を目指し、test262への適合状況を案内している。2026-10-21の検討時に参照したgo-quickjs v0.27.0は、class/private field・async/await・ES modules/top-level await・`using`、Intl、Temporal等の実装を記載し、decoratorを未実装としている。同版docsのtest262報告は、指定commitの対象99,979 variants中99,637成功・失敗0・342 skip。これは上流の対象範囲付き報告であり、今回NOEで試験を再実行した結果や、全Web/Node API互換の証明ではない。QuickJS-NGのIntl非対応方針と、go-quickjsのIntl実装も区別する。[QuickJS-NGの方針](https://quickjs-ng.github.io/quickjs/es_features/)・[go-quickjs v0.27.0の対応状況](https://github.com/go-quickjs/go-quickjs/blob/v0.27.0/docs/status.md)。

具体的な接続課題は、入口だけでなくimport先の`.ts`を変換するloader、ESM/import mapとmodule IDの整合、class field/decorator等の変換設定、および元のTS位置を返す診断。go-quickjs v0.27.0には`WithSourceMaps`があるため、変換結果のmapと組み合わせる案を考える。main・eval・Worker・agentの実行経路をどこまで同時に対応させるか、変換中の取消しや既存の資源所有との接続も実装範囲の決定時に整理する。[source mapの説明](https://github.com/go-quickjs/go-quickjs/blob/v0.27.0/docs/debugging.md#source-maps)。

2026-10-24の照合：[`go.mod`](../go.mod)の現在の固定版は正式go-quickjs v0.28.0です。上記のv0.27.0の機能・source map情報は検討当時の参照であり、現在のengineでのTS互換性確認ではありません。[`modulemap/snapshot.go`](../internal/modulemap/snapshot.go)は`.js`／`.mjs`のみを受け付け、[`modulemap/loader.go`](../internal/modulemap/loader.go)はローカルJS本文を読み込みます。[段階導入の提案](development/noe-improvement-proposals-20261024.md#3-typescript対応を段階的に検討する)も検討用であり、対応経路・診断・tool移行の範囲は着手前に決めます。今回の調査は文書・ソースの確認までであり、TS変換とNOE実行を接続した実測・互換性検証は未実施。既存のesbuild利用は[MCP SDK bundleの事前作成](../scripts/spikes/process-io-mcp/README.md)で、製品のTS実行対応ではない。

### <未完了> JSでのhook登録と、実行前評価・親agentへの問い合わせ — 2026-10-20

2026-10-25の追加議論は[hookの例外局所化・task通知とコンテキスト操作APIのメモ](development/hook-isolation-context-api-discussion-20261025.md)へ保存した。今回の中心は、検査に限定しない任意のhook処理について、例外をhookに閉じ込めてtaskへ通知する方向。同日の[実装計画](development/hook-failure-isolation-plan-20261025.md)と[スライス手順](development/hook-failure-isolation-slices-20261025.md)に沿って、登録済み4handlerの失敗局所化・通常モデル要求への通知・完了結果への保持を実装した。レビュー・検証状況は[実施記録](development/hook-failure-isolation-implementation-20261025.md)を参照。トリム外部化・コンテキスト操作APIは同実装の対象外で、引き続き検討のみ。承認機能の要望ではなく、以下の過去のcallback登録・実行前評価・親問い合わせの案とは区別する。

利用者の検討メモ。今は記録のみとし、実装・API名・優先順位は決定しない。ユーザーのJSで`hook("after_toolcall", handler)`のように処理を登録したり、`run_js`実行前にコードを評価したり、親agentが子の実行へ介入できる使い方を検討する。評価・承認を全実行に必須化する依頼ではない。

現在はconfigの`hooks: ["./hooks.js"]`とESM exportの`beforeTool`・`afterTool`等で登録する。`beforeTool`は実行予定のコードを受け取り、非同期の評価が終わるまで実行を待たせ、引数を置換できる。hook内から評価用agentを呼ぶ構成も可能で、評価用agentには`hooks: []`を指定して同じhookの継承を解除できる。登録済みhandlerの処理例外・返却値検証失敗はhookごとに局所化し、直前の有効値で後続hookと本体を継続する。`beforeTool`のthrowは対象toolの実行見送りにはならない。失敗は次の通常モデル要求へ通知し、最終応答後などの未送付分も`TaskOutcome.hookFailures`へ保持する。設定・module読込みの準備エラー、取消し・既存実行上限・Host等の継続不能は別扱い。[現行hook仕様](hooks.md)。

残る検討点は、同じJS内でのcallback登録と別runtimeへの受け渡し・外側の変数の扱い、task全体を失敗させずに今回のtool実行を見送って理由をモデルへ返す契約、子の実行前に親モデルへ判断を問い合わせて回答まで待つ仕組み。実行許可・引数変更・実行見送り・親への問い合わせをどう表現するか、待機中の取消しと親子の所有関係も含めて整理する。現状でも親が子へhookを指定・継承できるが、親モデル自身への同期的な問い合わせとは区別する。

照合根拠：[`js/hooks.js`](../internal/agent/js/hooks.js)・[`js/task.js`](../internal/agent/js/task.js)と[`hooks_phases_test.go`](../internal/runtime/hooks_phases_test.go)。既存のESM hookはあるが、新しいcallback登録・実行見送り・親モデル問い合わせの契約はない。

### <未完了> mdcli取り込みコードの整理（保留） — 2026-10-19

当初は未使用の色付け・syntax highlight関連の整理を候補にしました。その後の相談で、既にソースを取り込み済みで上流追随は必須でなく、将来`ANSI:true`で利用できる機能を残す利益もあるため、現状維持・整理保留としました。Markdown整形、日本語の表示幅、折返し・表表示も維持します。

`x/sys`とWebP用の`x/image`も維持する方針です。

照合根拠：[`markdown.go`](../internal/mdcli/markdown/markdown.go)は`ANSI`・`ColorTheme`とhighlightを保持。削除はしておらず、未完了は不具合ではなく保留方針を表す。

### <未完了> NOEへのMCPサーバー機能の組込み — 2026-10-17

**優先度は低い。** NOEで実行するJSがtoolを公開し、別アプリ・実行環境のagentから利用できるようにする候補です。同じVM内の別アプリでも別マシンでもよく、自作の集計・レポート、Exa検索ライブラリ、NOE agentへの仕事の委譲などを想定します。

NOE内の親子agentは既存の起動・観察・操作APIで連携でき、この用途にMCPサーバーは必須ではありません。外部へ公開したい具体的な処理が出てから必要性を再検討します。

サーバーSDKの採用、通信方式、起動・公開方法は未決定です。スパイクの`server.js`はクライアント検証用fixtureであり、製品のMCPサーバー機能ではありません。クライアントSDKの検証済み範囲を、そのままサーバーSDKの互換性確認とは扱いません。可能ならJSで実装します。

照合根拠：スパイクの[`server.js`](../scripts/spikes/process-io-mcp/server.js)と[`MCP E2E`](../tests/import_map_mcp_e2e_test.go)はクライアント検証用。製品サーバーの実装・SDK適合確認の根拠にはしない。

### <未完了> go-quickjsへの上流PR案：同期コマンドの失敗情報保持 — 2026-10-17

原型のgo-quickjs v0.27.0では、同期コマンド失敗時にstdout・stderrをErrorへ付けない経路がありました。上流へ改善を提案する候補です。NOE管理stdlibの修正は「NOE管理stdlibの同期コマンド失敗情報保持」の完了項目とし、上流PRとは分けます。

本リポジトリの作業として上流向けの修正・PR作成・提出は未実施で、このメモは提出許可ではありません。上流の最新実装・外部PRの有無は今回の照合対象外です。

### <未完了> Worker内からの`noe:agent`利用

Workerのloaderは`noe:agent`を受け付けません。通常JSからのagent起動は既に非同期で、並行起動や親子の委譲のためにWorker対応を足す必要はありません。必要ならWorkerからmainへメッセージを送り、mainでagentを呼ぶ構成を取れます。

重い計算・解析の途中から直接agentを呼ぶ用途で中継が実際の不便になった時に検討します。module登録だけでなく、Host/Client scope・認証への接続、Worker終了時の取消しと所有関係も決めます。`noe:process`・`noe:io`のWorker対応や、MCPクライアントのWorker検証とは別の残件です。

照合根拠：[`NOE Profile`](../internal/noeprofile/profile.go)はWorkerへagent bindingを導入せず、[`import_map_test.go`](../internal/runtime/import_map_test.go)もこの制約を検証する。

### <未完了> 呼び出し元の終了後も続く独立agent実行

`keepAlive:true`は同じNOE内の待受であり、呼出し元の終了後も続く独立実行ではありません。現在の子agentは親runに所有され、親終了・取消しで回収します。

独立したNOEプロセスが自分の認証・Host／Client・実行runtime・回収を持ち、起動元は終了できる構成を候補にします。内部HTTP brokerを復活させる前提ではありません。端末から離れて続けるだけなら、既存のOSのバックグラウンド実行・tmuxも利用できます。組込みAPIの必要性、ログ・結果・停止方法を検討し、会話の永続化・daemon・再起動後の復旧まで一括で必須にはしません。

照合根拠：[`Host.spawn`](../internal/agent/instance_operations.go)と[`Operation.Close`](../internal/agent/operation.go)の所有関係、[`agent/runtime_test.go`](../agent/runtime_test.go)の親終了時回収テスト。

### <未完了> Anthropic系SDK・Messages形式の追加 — 2026-10-17

OpenAI SDKは追加済みですが、Anthropic公式SDK・ネイティブMessages形式のadapterは未実装です。互換形式経由でAnthropic系モデルを利用することとは区別します。OpenCode Goの初期対応は利用者がChat Completions・Responsesに限定したため、Messagesはその未完了扱いにせず別候補にします。

追加時もprovider/modelとカタログは共用し、形式変換と認証はGoの通信adapterで扱う方針です。SDK選択と実装範囲は未決定です。

照合根拠：[`provider/config.go`](../internal/agent/provider/config.go)のSDK/API形式の受付はOpenRouter・OpenAIとChat Completions・Responses。Messages adapterはない。

### <未完了> HTTP URLからのimportとimport mapの拡張

ローカルの完全一致import mapは実装済みです。HTTP/HTTPS URLの直接import、永続ダウンロードcache、prefix mapping、scopes、npmのパッケージ探索は提供していません。必要性は個別に判断し、ローカルmapの完了条件へ追加しません。

ESMでもgo-quickjsでの動作保証にはならないため、自作JSと動作確認済みライブラリを再利用する方針です。対応済みの範囲は[import mapガイド](import-map.md)を参照してください。

照合根拠：[`modulemap/snapshot.go`](../internal/modulemap/snapshot.go)と[`snapshot_test.go`](../internal/modulemap/snapshot_test.go)。[`loader.go`](../internal/modulemap/loader.go)にはruntime内のローカルsource cacheはあるが、HTTP取得・永続cacheとは別。

### <未完了> 観察イベントのSQLite保存

JSONLを検索しやすくする外部保存先の候補です。採用は未決定です。現在のリアルタイム表示・親向け観察APIはSQLiteなしで利用できます。

照合根拠：現在の保存経路は[`internal/cli/events.go`](../internal/cli/events.go)のJSONL。組込みSQLite保存の実装・依存はない。

## モデル・接続先の選択方針

以下は利用者の運用方針であり、本体が自動適用する優先順位ではありません。普段はサブスクのOpenCode Goを優先し、OpenRouterは様々なモデルの試用と意思決定モデルに使います。よく使う順はDeepSeek V4.1 Flash、MiMo-V2.6-Flash、GPT 6 Luna、GLM 5.3で、effortは用途で変えます。OpenAIではSol・LunaのResponsesを使い、ChatGPTサインインをAPIキーより優先します。

カタログは組込みJSON＋外部JSON上書きと`list_models`を実装済みです。通常生成・意思決定モデルのwindow等も記録しており、実行時の予算計算に使います。画像生成モデルのwindowは未設定です。値の根拠・未知値・OpenCode GoへOpenRouter値を採用した経緯は[モデルカタログ](model-catalog.md)を参照してください。

MiMo・GLMの組込み候補、優先度項目、providerをまたいだ本体組込みの自動選択は未実装です。カタログは許可リストではなく、直接JSへprovider/modelを書いたり、外部JSONへ候補を追加したりできます。これらの選択方針を自動適用する本体機能を新しく決定したものではありません。

照合根拠：[`provider/catalog.go`](../internal/agent/provider/catalog.go)・[`defaults.json`](../internal/agent/provider/defaults.json)と[`catalog_test.go`](../internal/agent/provider/catalog_test.go)。列挙とメタデータ提供は実装済みですが、本体の自動ルーティングではありません。

## 維持する製品方針

外部コマンド、ファイル・HTTPのストリーミング、暗号・ハッシュは既にstdlibから利用できます。v1.0.0時点の未対応候補とは区別します。TypeScriptは引き続き未実装で、v2.5またはv3.0の候補として[検討メモ](#typescript)に整理しました。npm・パッケージ管理とNode.js/Denoの完全互換は対象外です。元QuickJSの`std`・`os`の提供を前提にしません。

[開発記録へ戻る](development/README.md) · [検討履歴](archive/future-candidates-history.md) · [現在のAPI](api.md)
