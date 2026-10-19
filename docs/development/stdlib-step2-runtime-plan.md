# NOE版stdlib：ステップ2・runtime切替計画

2026-10-19。[段階案](../future-candidates.md)のステップ2を具体化した計画です。ステップ1は`abddc24448feb054b7f0b9b6bafaadc1862a4851`でcommit済みです。その後の実装依頼に基づき、4スライスの実装・関連テスト・独立コードレビュー・総合テストを完了しました。[実装・検証記録](stdlib-step2-runtime-implementation.md)と[構造化結果](stdlib-step2-runtime-results.json)を参照してください。以下は実装前にレビューした方針と受入条件です。

## 目的と到達点

NOEの通常実行・Worker・agentのtaskと`run_js`が、取り込み済みの`noe/internal/stdlib`を使う構成へ切り替わることを目指します。JavaScriptエンジンは引き続き`github.com/go-quickjs/go-quickjs v0.27.0`を利用します。

公開API名、exports、既定値、実行・終了・取消しの契約を維持します。機能追加、`noe:http`、要求streaming、`noe:fs`への改名、agentやnativeioのpackage移動、外部向けライブラリ化は後続です。

到達点は「NOE本体と統合テストがNOE版stdlibを使い、現行のスクリプト・CLI・Worker・agentが従来どおり動き、内蔵docsも実装の由来を正しく説明する」です。

## 調査で確認した切替箇所

| 対象 | 現在の接続 | 今回の扱い |
| --- | --- | --- |
| `internal/runtime/stdlib.go` | 上流`stdlib.Install`・`Config`、Worker作成/読込み/Installed hook | importをローカルstdlibへ変更。Configの値とhook内容を維持 |
| `internal/runtime/eventloop.go` | 上流`stdlib.Loop`・`NewLoop`、所有goroutineでのRun、取消しとOnClose | 同じローカルstdlibのLoopへ変更。駆動・終了の手順を維持 |
| `internal/runtime/runtime.go` | 上流`stdlib.ExitCode`、main runtimeの初期化順序 | importを変更。既存の初期化順序・exit code取得を維持 |
| `internal/modulemap/quickjs_test.go` | 上流stdlibを直接導入しmodule予約一覧を検証 | ローカルstdlibを対象とする。予約名・aliasの集合自体は変更しない |
| `internal/nativeio/streams_test.go` | 上流stdlibでnative I/O用runtimeを生成 | ローカルstdlibで実際の組合せを検証 |
| `internal/nativeio/net_test.go` | 上流stdlibでsocket用runtimeを生成 | 同上 |
| `internal/nativeio/signals_process_test.go` | 上流stdlibとWorkerを使う専用子process | 同上。Worker hookと専用子processの起動手順を維持 |
| `internal/stdlib/worker.go` | 同packageの`Install`・`newLoop`を使い、Installed hookを呼ぶ | 実装変更せず、mainの切替から子・孫WorkerへローカルConfig/Loopが渡ることを確認 |
| `internal/agent/runner.go` | 指定されたNOE executableでtask・`run_js`を新しいprocessとして実行 | 実装変更せず、変更後のsourceからビルドしたbinaryによるfixture E2Eで確認 |

productionの直接importは3ファイル、統合テストの直接importは4ファイルです。`spikes/low-level-group2/`の独立した過去の実験には上流importが残りますが、製品の切替対象には含めません。

`internal/runtime/agent_test_helpers_test.go`と`tests/main_test.go`は現在のsourceから一時出力先へCLIをビルドします。これを利用し、rootに置かれた既存配布binaryで切替後のagent/`run_js`を検証したことにしません。

## 実装方針と維持する契約

### 一つのruntimeでは一つのstdlibを利用する

production 3ファイルと統合テストhelper 4ファイルの参照先を、一つの切替スライスで揃えます。Go package名は引き続き`stdlib`のため、基本的な変更はimport pathと実装由来のコメントです。

mainのConfig、Loop、`Install`、`ExitCode`をすべて`noe/internal/stdlib`へ揃えます。mainの初期化は現在どおり、共有writer生成 → `quickjs.New()` → loop作成 → stdlib → docs → nativeio → agent → authです。Workerはローカルstdlibが作るloopとConfigを使い、Installed hookでdocsとnativeioを追加します。上流版を追加導入するfallbackや、constructorを作り直す二重インストールは設けません。

JS値の操作とPromiseの解決を所有goroutineへ戻す経路、`context.AfterFunc`によるloop停止、`rt.OnClose`、agent Operationとnative resourceの所有関係は変更しません。Workerのctx終了・terminate・子Workerの回収も現在の手順を維持します。

### 設定・名前・挙動を維持する

- 26個のNode形式module、bare名/`node:`名、現在のglobalとNOE moduleを維持する。`noe:http`・`noe:fs`は登録しない。
- `Workers.Max: -1`、現在のWorker loaderと`.js`/`.mjs`の扱い、Workerで許可/拒否するNOE moduleを維持する。
- processのargv/env/cwd/stdin/stdout/stderr、`process.exit()`・`process.exitCode`、共有writerの関係を維持する。`process.version`等の値を新しい名称へ変更しない。
- Configの権限、WindowsPathsなど現在の明示値・zero値を維持する。特にOSに合わせたpath挙動の変更などを切替へ混ぜない。
- HTTPの既定timeout・本文上限・要求buffering、WebSocket upgrade、Web Streamsの契約を変えない。
- rootエラー・未処理rejection・timeout・SIGINTでの終了と、診断をruntime Close後も保持する現在の挙動を維持する。

切替に起因する不一致は、変更前sourceでの結果と比較して原因を確認します。既存テストの期待値やskipを変更して吸収する方針にはしません。上流由来の別の不具合が見つかった場合は移行差分と区別し、今回の目的に必要な適合かをdefaultが判断します。一般的なstdlib修正は今回へ広げません。

### 評価helperとエンジン境界

ステップ1の公開`Runtime.EvalFile`による適合をそのまま使用します。main・Workerともdebuggerを有効にせず、エンジン・非公開API・clone brand・stdlibのJS本体を変更しません。debugger有効hostで内部scriptが表示・計装され得る差分は引継ぎ事項として維持します。

`go.mod`・`go.sum`の変更、新しい依存や別のengine/stdlib versionの導入は不要です。上流stdlibはGo moduleの一部なので、エンジンへの依存を削除するのではなく、CLIのpackage依存グラフから上流stdlibが外れたことを確認します。

## スライス

### S2-A：変更前の基準と確認範囲を固定する

- 実装開始時のHEAD・Go環境・step 1のsource/検証記録を識別する。root binary・go.mod/go.sumのhashと現在のCLI依存一覧を記録する。
- 変更前のsourceから一時CLIをビルドし、mainとWorkerの公開global名、26 moduleのexport名、bare/`node:`対応をJSONで取得する。同じ最小scriptを切替後にも使えるようにする。
- 名前の比較は各modeの変更前後で行う。mainとWorkerのAPI集合が同一であることや、別runtime間のconstructor同一性は要求しない。aliasはnamespace object自体の同一性ではなく、公開export名と共有されるexportの値・参照の関係で確認する。
- 現行テストを次節の受入観点に対応付ける。すでにステップ1で成功した全テストを、基準取得だけのために再度すべて実行する必要はない。

完了条件：上流stdlibを使う現行状態と、切替後に維持するAPI/挙動が区別でき、実装前のbinaryを比較用に確保できる。基準の取得は実装時の作業であり、この計画で切替後の成功を主張しない。

### S2-B：productionと統合helperをまとめて切り替える

- 上記3つのruntime sourceと4つの統合テストhelperをローカルstdlibへ切り替える。
- 予約一覧の内容は維持し、`internal/modulemap/builtins.go`等の由来コメントを実態へ合わせる。
- main・Workerの初期化順序、Config、loop、終了処理、nativeio/agentとの接続を確認する。stdlibやagentのpackage移動は行わない。
- 関連packageの単体テストを実行する。APIのmode別inventoryを取得して変更前と比較する。

完了条件：CLIのproduction依存に`noe/internal/stdlib`が入り、`github.com/go-quickjs/go-quickjs/stdlib`がなくなる。`internal`と`cmd`のGo source/testに上流stdlibの直接importが残らない。既存のruntime/nativeio/modulemap/stdlibテストが新しい組合せで成功し、API inventoryが一致する。

この切替は一つの実装単位とし、mainだけ・loopだけ・Workerだけを別々に利用先変更するスライスにはしません。利用者向けの完了やreleaseは、続くdocs整合と総合検証まで含めて判断します。

### S2-C：実装由来の説明を揃える

- 内蔵docsを、NOE管理の`internal/stdlib`とその原型go-quickjs v0.27.0を区別する英文へ更新する。対応module/export/member、構文・既定値の説明は維持する。
- `internal/stdlib/UPSTREAM.md`の「productionは上流版を使う」という現在状態の説明を更新する。固定版・commit・checksum・取り込み範囲・最小差分・licenseは維持する。
- 現行README・API/参照/開発ガイド、必要な短いagent初期説明を揃える。過去の計画・検証結果・release/archiveを現在状態へ書き換えない。
- CLI/main/Worker/`run_js`のdocsが同じ内蔵本文へ届き、providerや外部サイトなしで参照できることを確認する。

完了条件：実行バイナリの説明が実装と一致し、API名やdocs topic/member/aliasの変更を伴わない。docsの変更を除けば変更前のJSON inventoryと同じ公開APIを持つ。

### S2-D：コードレビュー・総合検証・引継ぎ

- 実装と関連テスト・docsの確認後、独立した通常コードレビューを行い、P2以上の指摘を解消する。
- その後、全体テスト・race・CLI E2E・配布targetのビルドとcross-compileを実施する。
- `docs/development/stdlib-step2-runtime-implementation.md`と必要な構造化結果へ、対象source・コマンド・結果・skip・native実行とcross-compileの違いを記録する。
- 候補メモではステップ2のみ完了へ更新し、`noe:http`整備以降は未着手のままにする。

完了条件：後述の受入観点を満たし、NOEが取り込み版stdlibを通常利用していることを依存グラフと実行の両方で確認できる。ステップ3はこの状態を基準に開始できる。

## docsの変更対象

| 対象 | 変更する説明 |
| --- | --- |
| `internal/apidocs/content/stdlib*.md` 28件 | index・globals・26 moduleの原型と現在の実装場所。固定版はoriginで、実装はNOE管理のsnapshotであることを英文で明示 |
| `internal/apidocs/content/noe.md` | engineとNOE管理の標準ライブラリの関係 |
| `internal/apidocs/stdlib.go` | 出自コメントとglobalsの概要など、上流を直接利用するという説明 |
| `internal/stdlib/UPSTREAM.md` | productionへの採用状態。source/テストの取り込み由来とdebugger差分は維持 |
| `README.md`・`README.ja.md` | 現行stdlibの提供元に関する短い説明 |
| `docs/api.md`・`docs/reference.md`・`docs/development/README.md` | 現在の実装・docs・開発上の参照先。`docs/api.md`の古いserve版表記もローカル実装を確認して整合させる |
| `internal/agent/instructions.md` | 必要な出自説明のみ。API詳細を増やさず、内蔵docsへの導線を維持 |

英文説明の例：`NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib.`

engineの依存版と、stdlibの原型版と、NOEの変更を含むバイナリの版を区別します。別管理のstdlib version定数や架空のrelease番号は追加しません。既存のNOE版・内蔵docs・UPSTREAMの由来で説明します。publicなAPIをGoの内部pathで選択する操作は利用者へ要求しません。

## 検証と受入観点

関連テストの最初の確認は次を基本とします。

```sh
CGO_ENABLED=0 go test ./internal/runtime ./internal/nativeio ./internal/modulemap ./internal/stdlib/... -count=1 -timeout=180s
```

既存テストを優先し、次の受入観点で不足するcaseだけを追加します。Go importの文字列をなぞるだけの新規unit testや、切替確認のための公開JS markerは作りません。

| 観点 | 既存の確認先・受入条件 |
| --- | --- |
| 実際の利用先 | `go list -deps ./cmd/noe`にローカルstdlibが存在し上流stdlibが存在しない。統合テストhelperもローカル版を直接importする。Go moduleとしてのgo-quickjs依存は残る |
| 公開APIとalias | mode別の変更前後inventoryが一致する。`TestPinnedStdlibModuleReservationListMatchesRegisteredNames`、`TestDocsRuntimeContractAndCatalogExports`をローカル版で維持する |
| 型とインストール | 同一runtime内でBuffer、MessageChannel、BroadcastChannel、Streams等の共有export/constructorの関係が維持される。Workerの検証はそのWorker内で行い、別runtime間で参照の一致を要求しない |
| 通常のJS実行 | `internal/runtime/stdlib_test.go`、`tests/stdlib_e2e_test.go`等でrun/eval、fs、timer、子process、HTTP/WebSocket、Unicode/binary入出力が従来どおり動く |
| Workerと孫Worker | Node形式/global Worker、ローカル読込みとeval、messaging/structured clone、nativeio/docs、`Workers.Max:-1`と禁止NOE module、terminate/owner終了を確認。既存のstdlib・docs・low_level/process_ioテストを活用する |
| agentと`run_js` | 変更後のsourceから作られたCLIを使うfixture provider経由でtask、tool実行、子agent、docs参照、結果・エラー・終了コードの返却が維持される。既存のruntime agent/diagnosticテストとCLI E2Eを活用する |
| HTTP・WebSocket・Streams | 既存のfetch/serve/upgradeWebSocket、応答streamingと背圧、abort/切断・closeのテストが切替後も成功する。要求streamingの追加は行わない |
| 終了・取消し・診断 | `process.exit()`/`process.exitCode`、rootエラー・未処理rejection、timeout・SIGINT・Worker終了時の資源回収とClose後の例外情報を維持する。`stdlib_test.go`・`diagnostic_test.go`・serve/process_io/low_level/execution_timeoutの既存テストを使う |
| docs | catalog/CLI/runtime/Worker/孫Workerと`TestDocsStandaloneBinaryAndRunJS`が成功し、NOE管理snapshotという出自が英文本文に反映される。実装由来以外のAPI詳細やtopic/member集合は変えない |

総合確認は次を基本とし、最初の関連確認後は変更・失敗・未解決事項に応じて必要なものだけを繰り返します。

```sh
CGO_ENABLED=0 go test ./... -count=1 -timeout=180s
CGO_ENABLED=1 go test -race ./internal/... -count=1 -timeout=300s
CGO_ENABLED=0 go test -tags=e2e ./tests -count=1 -timeout=10m
CGO_ENABLED=0 go vet -tags=e2e ./...
```

Linux/Darwin/Windows × amd64/arm64の6対象では、CGO無効でCLIを一時出力先へビルドし、変更した統合テストを含むruntime/nativeio/modulemapのtest binaryを各package別に`go test -c`でcross-compileします。実行確認はLinux amd64で行い、他targetはcross-compileと明記します。rootの既存`noe`バイナリは上書きしません。

通常E2Eはローカルfixtureを使います。実provider呼出しやMCP SDK bundleの新規取得を完了条件にしません。既存のopt-in/helper/OS条件によるskipはtest名と理由を記録し、移行のためにskipを追加しません。`go.mod`・`go.sum`、上流engine、stdlib本体とwsprotoのsourceが不要に変わっていないことも確認します。

## 範囲と次のステップ

今回はproductionの接続、対応する統合テスト、実装由来の説明、検証記録を対象にします。stdlib本体は`UPSTREAM.md`の採用状態以外を変更する想定ではありません。必要なmigration適合が見つかった場合は、根拠と差分をdefaultが評価し、最小限に限定します。

`_refs/`と利用者の`agent-server-build.jsonl`は参照・変更しません。外部向け公開pathの決定、API追加・改名、agent/nativeioの移動、モデル要求やretryの変更、依存更新、実provider検証、commit・push・release、上流への提出は今回の計画作成に含みません。

本計画のレビュー後も、利用者から実装依頼があるまでは切替を行いません。実装完了後は、ローカルstdlibを利用するCLIの検証結果、API inventory比較、module/engineとsnapshot由来、debugger差分をステップ3へ渡します。

## 計画の独立レビュー

通常レビューの評価基準は、切替箇所の完全性、main/Worker/agentの接続と所有関係、既存契約の維持、テストの実際の利用先、docsの出自、スライスと後続範囲、受入条件の実現性です。

初回30分以内、新しい根拠・中間結論が10分ない場合は中断して確認済み範囲を返します。再レビューは変更箇所と既存指摘の解消を15分以内で確認します。利用者の明示依頼に従い、P2以上の指摘がなくなるまで修正と再レビューを反復します。P3は任意改善として区別します。[レビュー記録](stdlib-step2-runtime-review.md)へ根拠と結果を残します。
