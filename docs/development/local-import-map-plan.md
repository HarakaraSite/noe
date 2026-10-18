# ローカルESMのパス解決とimport mapの計画

状態：2026-10-18、計画レビュー・全スライスの実装/テスト/コードレビュー・総合検証・全体レビュー完了。[実装・検証記録](local-import-map-implementation.md)に進捗を記録する。利用者の「自作JSと動作確認済みライブラリを名前で使うことをまず目指す」という合意に基づく。

## 目的と到達点

自作JS、動作確認済みの外部ESM・bundle、NOE向けアダプターをローカルに置き、通常JS・REPL・Worker・agent生成JSから同じ名前でimportできるようにする。agentの一時ソースの場所に合わせて相対パスを計算する必要を減らす。

最初の応用例は、検証済みMCP SDKを再利用用ESMへbundleし、NOE向けtransportを別moduleとして公開すること。

```json
{
  "imports": {
    "mcp/client": "./vendor/mcp-client.mjs",
    "mcp/transport": "./lib/noe-mcp-transport.js",
    "app/helpers": "./lib/helpers.js"
  }
}
```

```js
import { Client } from "mcp/client";
import { NoeTransport } from "mcp/transport";
```

これは名前解決の改善であり、任意のnpm packageの互換性を保証する機能ではない。SDKごとのホストAPI適合は引き続き必要で、今回のSDKは既存の[実接続スパイク](../../scripts/spikes/process-io-mcp/README.md)を土台にする。

## 範囲

今回実装するもの：

- 共通のローカルmodule resolver。相対・絶対ファイルパスとローカル`file:` URLを扱う。
- JSONの`imports`による、完全一致のbare specifierからローカルESMファイルへの対応付け。
- 明示的なCLI指定、run/eval/repl・Workerへの接続、agent task/run_jsへの対応表の継承。
- 再利用用MCP SDK bundleの準備手順と、独立したJS transport・利用例。
- 各スライスの実装・テスト・コードレビュー、最後の総合検証。

HTTP/HTTPS import、永続ダウンロードキャッシュ、URL取得、npm/package.json/node_modulesの解決、CommonJS loader、TypeScript変換、Node互換の拡大、stdlib/engineの変更は含めない。bundle作成には既存の外部ツールを使える。ライブラリやアダプターのNOE向け修正を禁止しない。

初回は`imports`の完全一致だけを提供する。末尾`/`のprefix mapping、`scopes`、`integrity`、nullによるblock、複数mapのmerge、パスやURL自体のremapは後続とし、入力にあれば未対応と診断する。今回必要な`mcp/client`等の具体名は完全一致で記述できる。HTML Standardのimport map全体との互換を標榜しない。

標準との関係：JSONの`imports`と完全一致による対応付けを参考にする。標準にはprefix・scope等もあるため、NOEの対応部分を資料で明示する。[HTML Standard: import maps](https://html.spec.whatwg.org/multipage/webappapis.html#import-maps)

## 利用方法と設定の寿命

```sh
./noe run --import-map ./import-map.json task.js
./noe eval --import-map ./import-map.json 'const h = await import("app/helpers"); console.log(h)'
./noe repl --import-map ./import-map.json
```

- `--import-map path`と`--import-map=path`をrun/eval/replのoptionとして追加する。指定は最初のoperandより前。`--`以後やscript名以後の引数を取り上げない。
- mapファイルへの相対パスはCLI起動時のcwd基準。map内の相対targetは**mapファイルが置かれたディレクトリ**基準である。
- 初回は明示指定のみとし、自動探索・暗黙のユーザー共通map・環境変数による追加設定は導入しない。ユーザー共通のmapも`--import-map ~/.config/noe/import-map.json`で明示できる。
- 指定なしでは現行の組込み・相対importを維持する。mapは一つだけ、重複option・値なしはCLI usage error（2）。ファイル読込み・JSON/契約の検証失敗は実行開始前のエラー（1）にする。
- mapはrootのExecute/NewSession作成時に一度読み、検証・targetの絶対化を終えた変更不能のsnapshotにする。REPL入力ごとの再読込みや実行中の再設定は行わない。別のCLI起動で変更を反映する。
- 同じsnapshotをWorker・agent task・そのrun_js・子agentへ継承する。元mapファイルの変更・削除、子の一時ソース位置によってbindingが変化しない。
- **継承するsnapshotは名前とtargetの対応表だけ**。ライブラリの内容・依存ファイル一式を起動時にsnapshot化しない。各runtimeでは最初に読めたソースをそのruntimeの寿命だけ保持し、ESMのmodule registryと併せて再利用する。新しいruntime/プロセスではローカルソースをその時点で読む。
- 任意の`noe:process.spawn()`で起動した外部プログラムへCLI設定を自動注入しない。対象はNOE管理のWorker・agent実行経路であり、任意の子NOEへは必要なoptionをJS側から明示できる。

Go内部では、CLIが読んだsnapshotを`runtime.Options`へ渡し、直接Execute/NewSessionを使う埋込み側にも同じ設定を渡す入口を用意する。元mapのパスだけをruntime/agentへ渡して後から再読込みする実装にはしない。

## JSONと解決規則

| 項目 | 初回の契約 |
| --- | --- |
| 文書 | JSON object。`imports`は省略可能（空map）。存在時はobject。未対応のtop-level fieldは診断する |
| key | 空でない完全一致のbare specifier。`mcp/client`・`@example/helper`等。相対/絶対path・URLのremap、末尾`/`のprefixは初回非対応 |
| target | 空でないstring。`./`・`../`付きpath、OSの絶対path、ローカルfile URL。裸の名前へのalias連鎖はしない |
| 入力の不備 | 不正JSON、重複key、型不一致、未対応field/形式を起動前に診断。全体ではなく位置・keyと理由を示す |
| targetの存在 | 起動時は形式と絶対化を確認する。ファイルの存在・読取り・構文のエラーは実際のimport時に報告し、未使用targetを先に評価しない |
| 拡張子 | `.js`・`.mjs`を明示。省略補完、ディレクトリのindex探索はしない |
| 組込み | `noe:*`・`node:*`・stdlibのbare module名は既存の意味を維持し、衝突するmap keyを起動時エラーにする。外部fileを組込み名でshadowしない |
| 未登録のbare名 | 解決エラー。暗黙のpackage探索やネットワーク取得を始めない |
| ファイル内の相対import | 対象moduleのファイル位置を基準に解決する。mapの位置や実行cwdを使わない |
| static/dynamic | `import`・`export ... from`・`import()`で同じ解決規則を使う。`import.meta`は既存engineの契約を維持し、解決hookへの影響も確認する |

パスはOSの絶対pathへ正規化し、同じ表記へ到達するalias・相対path・絶対path・file URLに同じmodule IDを返す。`./a/../lib.js`等の字句上の差も吸収し、同一runtimeで重複評価しない。symlinkのrealpath統合、filesystemの大小文字同一視、hardlinkのinode統合までは保証しない。

固定版engineは未見の`(referrer, specifier)`に対してloaderを呼んでから、resolved IDのmodule registryを見る。このためIDの統一だけでは、読込み済みファイルの削除後に別alias・別referrerからimportすると読取りエラーになる。共通loaderはcanonical IDごとに最初に成功した読取りのsourceを保持し、以降はディスクを再読取りせず同じsourceとIDを返す。Execute入口のin-memory sourceも同じ扱いとする。保持先はroot・Workerそれぞれのruntime内で独立し、runtimeとともに解放する。読取り失敗は保持せず、作成後の再試行を妨げない。構文・評価失敗の扱いはengineに従い、NOE独自の再コンパイル・hot reloadは導入しない。永続cacheやruntime間のソース共有は行わない。

S2の実装レビューで確認した境界：Workerの入口はstdlibの専用`Load(specifier)`→直接評価であり、共通loaderを通らない。固定版の公開hookは入口のsourceを対象runtimeへ渡す情報を持たないため、Worker入口そのものはこのsource保持保証の対象外とする。入口削除後に入口を指す新しいaliasからimportすると再読取りで失敗しうる。Workerが共通loaderからimportするライブラリには保持保証を適用する。ライブラリ再利用という目的を優先し、共有sourcecache、起動順に依存する対応付け、Worker sourceの書換え、stdlib変更は追加しない。既存Workerの入口・classic/module・診断の挙動を維持する。

file URLはpercent-encodingを復号し、ローカルの空authority/localhostを扱う。query・fragment・非ローカルauthorityを初回のlocal file指定としては受け付けない。通常pathの空白・日本語・`#`・`%`はOSのファイル名として扱い、URLと混同しない。Windowsはdrive絶対pathとfile URLのdrive変換を揃え、drive相対形式は明示的に診断する。既存WorkerのOS絶対path（UNCを含む）の扱いを不用意に狭めない。Windows file URLのUNC authorityは初回の共通化対象から分け、native UNC pathによる指定と区別して資料へ記録する。

入口ソースは今と同様に、CLI/Executeが渡した内容を使う。絶対化したIDが入口自身ならディスクから読み直さず、bootstrap・alias経由で入口の二重評価を起こさない。REPLの仮想referrerはcwdを含む既存source名を使う。

## 配置と統合

配置案は`internal/modulemap/`等の独立packageで、JSONの検証、snapshot、ローカルpath解決を持つ。runtimeとagentの両方から使える依存方向にし、runtime↔agentの循環依存を作らない。公開JSへ新しい管理APIを追加することは今回の成立条件にしない。

### main・REPL・Worker

- `runtime.installLoader`と`loadLocalModule`の重複を共通resolverへ寄せる。main専用のnoe:agent/decision/auth/task sourceと、native登録済みnoe:process/io・stdlibの区別は維持する。
- Worker runtimeにも同じ変更不能snapshotを捕捉するloaderを登録する。Worker内でのimport名に適用し、Worker constructorのentry specifierをbare aliasで起動する追加機能は初回に含めない。既存のWorker entryのcwd・path規則を保つ。
- Workerのnoe:agent/decision/auth未対応を、map経由で対応済みに見せない。親のQuickJS ValueをWorkerへ共有しない。
- builtin名衝突の検証は、固定版の公開hook/登録済みmoduleを使って、対象ファイルを実行せずに成立させる。S1でloaderのinstall順を含めて確認する。組込み名一覧の維持が必要なら一か所に集約し、固定版stdlib更新時の確認点として記録する。
- 現在のstdlibは登録済みnative moduleをloaderより優先する。NOE source moduleの予約名も含め、mapが無効なまま黙って受理される状態を避ける。

### agent task・run_js・子agent

`runtime.installAgent`からsnapshotをagent.Optionsへ接続する。以下の両Runner生成点へ明示的に渡す。

- `internal/agent/lifecycle.go`のRunTask：agent task bootstrap用NOE。
- `internal/agent/broker_tools.go`のRun：run_js用NOE。

Runnerは自身のprivate一時ディレクトリへ**targetが絶対化済みのsnapshot JSON**を書き、子CLIへ`run --import-map <snapshot-file> <source-file>`として渡す。子は元のmapではなくこのsnapshotだけを読む。一時ファイルは既存sourceと同じ所有者・寿命で回収し、孫のために親の削除予定ファイルへの参照を残さない。各Runnerは受け取ったsnapshotから自分の子用ファイルを作る。

map設定はagentのmodel/provider/effort/configや認証Connectionへ混在させず、ホストのmodule設定として扱う。provider要求・会話履歴・イベントへmap全体やsnapshot本文を自動挿入しない。モデル向けinstructionsには、設定済みaliasを使えることとローカルライブラリの使い方を追記する。利用例は必要なaliasとAPIをタスクに示して検証し、モデルの自発的なSDK選択を成功条件にしない。

既存Runnerの取消し、process group/Job、出力収集、例外保持、private sourceの削除を維持する。snapshotのロード失敗も既存の子実行エラーとして返し、元mapへのfallbackで隠さない。テスト用の再実行バイナリ/CLI dispatchも、追加optionを持つ実起動経路で確認する。

## MCP SDKを再利用可能にする

現在の`client.js`はSDK・transport・実行処理を一つのbundleへ入れる検証entryであり、そのまま`Client`をexportするライブラリではない。次を分ける。

1. bundle用entryで`Client`等の利用例に必要なsymbolだけをexportする。既存のSDK 1.32.1・esbuild 0.25.12を開始点とし、依存はbundleへ含める。`noe:*`・必要な`node:*`は外部参照のままとする。
2. `NoeTransport`を独立したJS moduleとしてexportする。サーバーentry pathとNOE実行ファイルを明示できるようにし、呼出し元の`process.argv[2]`やentryの置き場所に依存させない。start/send/close・例外・子終了をSDK transport契約と照合する。
3. consumerはSDKとtransportをbare aliasからimportする。SDK本体へのpatchが必要になれば適合箇所・理由を記録する。既存スパイクで不要だったpatchを前提条件にはしない。
4. 生成物は利用者の任意のローカル配置へ準備する。Node/npm/esbuildは準備時だけで、NOE実行時や通常Goテストの必須依存へ加えない。新しい製品MCP moduleやGo専用MCP APIは作らない。

元のスパイクで検証したinitialize、tool一覧、3件の並行呼出し、270000 byteのUTF-8入力、終了・回収を新しい分割構成でも確認する。consumerをmainと別ディレクトリに置き、Workerでも同じaliasを使う。agent経路ではfixture providerがaliasをimportするrun_jsを返し、同じローカルMCP serverと通信する。実providerや費用の発生は不要である。

## 既存機能への影響

| 機能 | 改善点 | 維持・確認する契約 |
| --- | --- | --- |
| run/eval/repl | 名前付きのローカルESM・file URL・絶対pathを共通利用 | 引数境界、entryのin-memory source、REPLの状態/取消し/評価待機 |
| Worker | mainと同じ対応表で依存をimport | entry解決規則、runtime独立、未対応のNOE module、terminate時の回収 |
| run_js・子agent | 一時sourceの位置に依存しないaliasとsnapshot継承 | 設定・認証・履歴・実行寿命の分離、出力・例外・取消し |
| noe:process/io | JS transportをmoduleとして再利用 | 公開API・process ownership・Streamsと標準I/Oの挙動 |
| 組込みstdlib | 固定版の機能をそのまま使う | export/prototype/依存版を変更せず、組込み名の既存解決を維持 |

## 実装スライス

各スライスで実装・目的に対応するテスト・コードレビューを行い、必須指摘を解消してから次へ進む。

| Slice | 実装内容 | 受入条件 |
| --- | --- | --- |
| S1 共通resolverとJSON snapshot | 独立package、path/file URL正規化、完全一致map、診断、runtime内source保持、module IDの実証 | Go単体でbase差・入力不備を確認。固定版engine上でalias/relative/absolute/file URLから同じnamespace・副作用1回。初回読取り後のファイル変更/削除を挟んでも別alias/別referrerから同じnamespaceへ到達する。循環import、in-memory entry、組込み優先・衝突診断が成立する |
| S2 CLI・main・REPL・Worker | `--import-map`、runtime.Options、共通loader、Worker snapshot | run/eval/replとWorkerから同じaliasを使える。REPLの次の入力でも既存namespaceを維持する。cwd/map/consumerの配置を変えても基準が正しい。既存相対/組込みimport・CLI script args・Workerの利用範囲を維持する |
| S3 agent継承 | agent.Options・両Runner・private snapshot、子への再配布 | fixture provider→task bootstrap→run_js→子agentでaliasが一致。root作成後の元map変更/削除に影響されない。二つのrootのmapが混線しない。正常・失敗・timeout/interrupt時に一時ファイルとプロセスを回収する |
| S4 MCP SDK分割・応用例 | SDK export entry、JS transport、consumer・map・準備手順 | main/Worker/fixture-agentの各経路でlocal MCPを初期化・tool呼出し。並行応答ID、大きい入力、closeと子回収を確認。準備済みbundle実行時にNodeを起動しない |
| S5 文書・総合検証 | README両言語、API/REPLガイド、instructions、examples、検討メモ、実装記録 | 公開部分・未対応部分・準備方法が整合。通常Go tests/E2E/vet、変更packageと関連E2Eのrace、配布6targetビルド、全体レビューを完了する |

## 検証の方針と着手時の確認

通常検証はGo fixtureとローカルJSで行う。MCP SDKの準備・適合検証は通常Goテストから分離するが、S4の完了には分割構成でのローカル接続検証を必須とする。版・コマンド・結果を`docs/development/local-import-map-implementation.md`へ記録する。SDK取得のためのネット接続と、NOE実行時のmodule取得を混同しない。credential・実provider・有料APIは不要。

- 固定版engineの`SetModuleLoader`が返すresolved名でmoduleを共有することをS1で実証する。同期loaderを使い続け、ファイル読込みだけのために新しい非同期配送機構を増やさない。
- 評価済みmoduleはruntimeごとのregistryで共有し、loaderの読取り済みsource保持も同じruntime内に限定する。永続cache・別runtime間のsource/JS Value共有を追加しない。
- 起動時のJSON snapshot生成と、import時のソース読取りを分け、mapの未使用targetが先に実行されないことを確認する。
- Workerとroot loaderのinstall順、builtin衝突判定の副作用、入口IDの重複をS1/S2で確認する。公開hookで成立しない場合は実装前に計画を更新し、勝手に上流修正へ広げない。
- Windows drive/URL/UNCはクロスビルドとpath table testで確認し、native未実行を明記する。Darwin/Windows実動とLinux実動を混同しない。
- 異常終了テストはready通知・marker・gateで対象の開始を確認してから止める。固定sleepだけを成功の根拠にしない。

最後に`go test ./...`、`go test -tags=e2e ./...`、`go vet -tags=e2e ./...`を行う。並行処理を変更したpackageと関連E2Eはrace検査する。再実行processを多用するraceテストでは、必要に応じて`GORACE=atexit_sleep_ms=0`と`-p 1`を使い、race検出を保って終了待機・CPU競合の影響を減らす。CGOなしのLinux/Darwin/Windows各amd64/arm64ビルドを確認する。リリースはこの計画に含めない。

## 計画レビュー

2026-10-18、通常レビューと批判的レビューを別reviewerが実施。対象は初回案（SHA256 `6970a0d36011dde76ee96271616b8021c14ada6e5fcc3b1c05d2b4c594013b0a`）。計画と固定版sourceの照合を行った。

- 通常レビュー：必須指摘なし。任意指摘としてSDK検証の「任意」とS4受入条件の関係が曖昧だったため、通常Goテスト外だがS4完了には必須と修正した。
- 批判的レビュー：P2を1件採用。未見のimport要求ではregistry参照前にloaderが読取りを行うため、ファイル削除後の別specifierでENOENTになることを既存binaryの一時fixtureで再現した。runtime内の初回source保持、変更/削除後の別alias・別referrer、REPL次入力の受入条件を追加した。
- module ID、builtinの優先順位・衝突判定の実現方法、両Runnerを通る子agentへのsnapshot継承、Workerの制約、スライス順に追加の必須指摘はなかった。

通常・批判的reviewerの両者が修正版（SHA256 `f052327ebbf256680ea969791d7936f9f716fc3a368b8198c3df14ab15db02f2`）の変更箇所を再レビューし、指摘の計画上の解消と追加の必須指摘なしを確認した。実装着手可能と評価している。実装着手後、S2レビューに基づきWorker入口のsource保持保証の境界を修正し、通常reviewerの再確認を受けた。判断根拠・進捗は[実装記録](local-import-map-implementation.md)にまとめる。

実装段階ではengine上の実証と、分割SDKのmain/Worker/fixture-agent接続をLinuxで完了した。通常suite・E2E・vet・race・配布6targetビルド・全体レビューも完了。Windows/Darwinのnative実動は未実施。詳細は[実装記録](local-import-map-implementation.md)を参照。

[開発記録](../README.md) · [検討メモ](../future-candidates.md) · [プロセス/I/Oの実装記録](process-io-implementation.md)
