# AI向け実行改善計画（範囲見直し版）

更新日: 2026-10-23。状態: 計画と[S1契約・詳細設計](ai-execution-contract-design.md)の通常・批判的レビュー完了。S1の指摘3件は修正・再レビュー済み。製品実装は未着手。

基点はv2.1.1への復旧コミット`5781755`。go-quickjsは正式な上流v0.28.0を使用する。
撤回・ロールバックした旧計画のengine改造と派生moduleの設計は引き継がない。
旧実装のテスト・レビュー結果を、この計画の実装済み・承認済みの証拠にはしない。

## 1. 利用者が指定した目的・範囲・権限

今回の目的は、AIが既存ツールで生成コードを検査し、NOEの標準APIを調べ、実行結果と失敗情報を使って修正できるようにすること。
採用範囲は次の3本に限定する。

1. **静的検査は既存ツール**：既存のBiome／tscを使う手順・設定例を文書化する。静的検査のための製品実装は伴わない。
2. **NOE標準APIの情報提供**：NOEが提供するAPIの名前・署名・制約を調べられるようにする。
3. **実行結果・実行時診断の改善**：データ・stdout・stderr・実行状態・診断を区別して返す。

利用者の明示的な指示を、設計判断より優先する。

- 「上流の改変を伴う修正はユーザーに明示的に許可を求める」。上流無改変を永久の禁止条件とする、という意味ではない。
- 改変を許可済みのstdlibは、**NOEが管理・提供するnoestdlib層**を指す。その許可をgo-quickjs本体や他の依存先へ広げない。
- 本計画にgo-quickjs本体の改変は含めない。必要だと判断した場合は、既存公開APIでの代替検証と制約を示し、対象・変更理由・影響を説明して変更前に明示的な許可を求める。
- 別moduleへの分岐、module path・上流Go型の変更、独自engine版の公開を実装上の裁量で追加しない。
- 計画見直し後、利用者からS1設計と通常・批判的レビューの指示を受けた。製品実装・コミット・配置・公開へ進む指示として扱わない。

## 2. 層と変更対象

noestdlibは層の名前であり、`internal/noestdlib`という1ディレクトリだけを意味しない。
[三者の責務](v2-responsibility-boundaries.md)を維持する。

| 層・対象 | この計画での役割 |
| --- | --- |
| 外部ツール：Biome／tsc | 構文・lint・型・import/exportの静的検査。project設定・型宣言に従う |
| noestdlib：`stdlib/`、`internal/jsexec`、`internal/stdlib`、`internal/noestdlib` | 公開実行API、VM/Loopの管理、NOE管理の標準API、データ・例外の切り離し |
| 共用API資料：`internal/apidocs` | docs、明示的な署名・制約、標準APIの案内。agentやSDKへ依存させない |
| noeagentlib：`agent/`、`internal/agent`、`internal/agentexec`等 | run_jsの入出力、hook、履歴、providerへの結果受渡し |
| noe：CLI・profile・利用ガイド | API情報の表示、既存ツールの利用案内、NOE既定の組立 |
| 上流go-quickjs | v0.28.0の公開APIを利用する。source・module cache・型・配布形態を変更しない |

NOE管理の`internal/stdlib`と、上流module内のstdlibは区別する。変更対象は前者。
外部ツールを利用するために、noestdlibへagent/CLI/Node.jsプロセスの常時依存を持ち込まない。

## 3. 3本の採用内容

### A. 静的検査は既存ツール

- この項目の成果物は利用案内・設定例。NOEへの組込み、自動実行、専用API・ラッパー・検査サービスは実装しない。
- Biomeは構文・lint、tscは型と宣言・module設定に基づく検査に使う。両者の役割と対象を明示する。
- 生成したJSをファイルへ保存して検査する手順、project設定・cwd・対象fileの渡し方、結果の読み方を用意する。
- agentは通常のコマンド実行で必要な検査を行い、結果を読んで修正する。未導入・未設定ならその事実を報告し、未検査を合格扱いにしない。これをNOEの追加機能として実装しない。
- 型宣言の整備はBのAPI情報提供に属する。`@types/node`全体をNOEの対応仕様として扱わない。
- Biome／tscの合格を、QuickJSでの実行成功、実際のProfileでのAPI提供、dynamic import、外部状態の正しさの保証にしない。
- 新しい`noe:runtime.check`や、非実行で依存解決・linkまで保証するNOE独自APIは作らない。
- 既存`CheckSyntax`／`CheckModuleSyntax`の内部利用は維持する。上流の公開関数自体を変更しない。

既存ツールの仕様は[Biomeの診断](https://biomejs.dev/reference/diagnostics/)・[tscのnoEmit](https://www.typescriptlang.org/tsconfig/noEmit.html)・[TypeScriptのmodule解決](https://www.typescriptlang.org/docs/handbook/modules/reference.html)を参照する。

### B. NOE標準APIの情報提供

- 対象はNOE固有API、NOE管理のNode互換module・別名、global API。任意の利用者moduleの解析へ広げない。
- run_jsで使う`noe:agent`のspawn_agent/wait_task等も対象に含む。「NOE標準API」をnoestdlibだけに限定しない。
- 既存の`noe:docs`とCLI docsを起点に、module名、公開名、明示的な引数・戻り値、主な制限、詳細docsへの案内を提供する。
- 「実装にある」「現在のProfile／実行contextで使える」「詳細説明がある」を区別する。説明がないことをAPI不存在と扱わない。
- export名はNOEが管理する登録・公開定義から得る。Markdownの見出しを実export一覧とみなさない。
- 署名・型宣言・docsは同じ公開契約に揃える。型の正しさや引数の意味をGoの反射から推測しない。
- noestdlibとnoeagentlibは自身のAPI説明・署名・型宣言を別管理する。NOEは既存のdocs索引でmodule名から対応する情報へ案内する。統合はこの索引・案内の範囲に限る。
- 型宣言は別ファイルのままtscから参照できるようにする。単一の型定義への変換、新しい共通検索・問い合わせ機構は完了条件にしない。
- agent APIの説明のためにnoestdlibからagentlibの実装をimportしない。既存の共用資料と、実装を伴う依存関係を区別する。
- API情報を読むために対象moduleを実行しない。任意moduleの非実行export列挙、汎用symbol解析、独自module resolverは対象外。

### C. 実行結果・実行時診断の改善

- run_jsの暗黙agent importによる名前衝突を解消し、通常のESMと明示importへ揃える。
- データ返却は`export default value`／`export default await operation()`を標準案とする。公開APIとnoestdlibの実行管理で実現する。
- データ、stdout、stderr、exitCode/reason、診断を別の項目にする。診断表示を実際のstderr出力へ混ぜない。
- 戻り値なしと明示nullを区別する。未awaitのPromise、BigInt、循環等のJSON化不能値の扱いを実装前に確定する。暗黙の関数呼出し・自動再実行は加えない。
- NOEが得た実行時例外、timeout、取消し、外部コマンドの失敗情報を構造化する。NOEの分類codeと利用者例外のcodeを区別する。
- message、既存のstack原文、取得できる構文位置、command例外のcode/signal/stdout/stderr、元のGo causeを保持する。位置・frame・内部phaseを取得できない場合は推測で補わない。
- 処理段階はNOEが観測した範囲で表す。engine内部のparse/link/evaluateを常に正確に分離する保証は置かない。
- Error formatterの常時差替えや、全例外の構造化frame取得を必須にしない。既存のError表示契約を維持する。
- JSON化失敗とコード実行前の失敗を区別し、既に副作用が起きた可能性を示す。catch済み例外や表示しただけの失敗から実行全体の失敗を推測しない。
- hookの元入力／実効入力、実行結果／加工後結果を区別し、履歴と次のprovider要求まで情報を渡す。
- fresh VM、owner goroutine、取消し・Worker/IO回収、画像の別配送、provider native履歴、queueのsession/cache継続を維持する。
- 出力の既存上限と省略を明示し、診断を出力省略で消さない。新しい全文artifact保存backend・範囲取得API、RPC最適化を、この3本の完了条件へ追加しない。

## 4. 公開APIだけで確認できた範囲と未確認事項

上流v0.28.0を使う小さなprobeで、wrapper moduleからnamespaceを受け渡す方法により、host待機後のdefaultデータ取得、namespaceの`then`誤実行回避、host待機後の例外保持を確認した。
`CheckModuleSyntax`は、存在しないmodule/exportを含むsourceにも合格し、link検査ではないことも確認した。
JSのCallSiteから位置を得ることは確認できたが、利用者独自formatterとの両立や既読stack等は未確認であり、採用済み機構として扱わない。

probeは実現可能性の限定確認であって、製品実装・受入試験の代わりにはしない。
S1で次の契約・接続方式を確定した。詳細と限定確認の範囲は[設計](ai-execution-contract-design.md)§3〜8を参照する。製品への接続・受入試験はS3/S4で行う。

- 現行Profile/loader/Loop、Worker、TLA取消しとの接続方法。wrapperで利用者の束縛・元source位置を変えないこと。
- 返却dataのJSON範囲・大きい値の扱い、noestdlibとrun_jsの返却field、失敗段階・省略の意味。
- API登録情報・明示署名・型宣言・docsの正本と、現在の利用条件を各層から供給する方法。
- 外部検査の対象file・設定・対応版。新しい実行サービスや独自解析器を作らずに既存のcommand実行・docsで足りる範囲。

不足を見つけたときは、上流改変が必要だと断定する前に公開APIでの代替と制約を整理する。
実現手段を未確認のまま必須要件へ格上げしない。

## 5. 実装スライスと終了条件

S1は設計とレビューまで進める指示を受けた。S2以降は後続指示に従う。製品コードを変えるスライスで実装・対象テスト・コードレビュー・必要な修正を行う。S2は文書整備と利用例の確認であり、製品実装のスライスとして扱わない。

| スライス | 成果物・確認内容 | 終了条件 |
| --- | --- | --- |
| S1 契約と公開API接続の確定 | 3本の契約、変更対象・許可境界、既存APIによる最小接続、未確認事項の切分け | engine改変・派生依存を前提にせず実装判断ができる。実装可否を左右する未決事項を解消するか、制約・別途相談事項として切り分けている |
| S2 既存静的ツールの利用案内 | Biome／tscの適用手順・設定例、agentが既存commandで検査する流れ | 文書の利用例で未検査と合格の違いが分かる。製品への検査機構の実装はない。型情報はS3の成果物を参照 |
| S3 標準API情報 | 両ライブラリが別管理する登録情報・署名・型宣言・docsと、既存docs索引の案内 | agent APIも対象に含める。未対応のNode APIを案内せず、説明不足とAPI不存在を混同しない。情報取得で対象moduleを実行しない |
| S4 結果・実行時診断の接続 | noestdlibの返却、run_js明示import、hook/履歴/provider接続、移行docs | データ・出力・状態・診断を区別できる。TLA、native Go cause、取消し・回収と会話継続が維持される |
| T ローカル総合検証 | 下記の利用経路と影響に応じた既存suite | 必要な重い検査を実装完了までに終え、結果をリリース準備へ引き継げる |

標準利用経路は「docs/型情報を調べる → ファイルへ生成 → 既存ツールで静的検査 → run_jsで実行 → データ/実行時診断を受け取る → 必要なら修正」とする。
通常レビューは契約・層・既存動作との整合、批判的レビューは権限境界、誤った保証、独自機構への不要な拡張を確認する。

レビューには本書§1の利用者の指示をそのまま渡す。依頼元が書いた「決定済み」「承認済み」を根拠に、上流改変や追加の公開権限を推定しない。
技術的に成立することと、許可範囲に収まることを別々に確認し、findingの採否は親が利用者の指示へ照合する。

## 6. 検証・リリースの分担

- 対象テストは利用契約を確認する。明示import、defaultなし/null/async、JSON化失敗後の副作用、構文位置、runtime stack/command詳細、hook加工、次model要求の情報保持を優先する。
- TでCLI・公開Go consumer・agent/provider fixtureを通す。静的検査合格でもruntimeで失敗する例を区別して確認する。型宣言だけで実行時能力を保証しない。
- VM駆動や取消し・共有状態を変更する場合は、関連する回収・並行処理・race等の重い既存検査を実装工程で行う。配布対象のcompile確認はportabilityへ影響する変更に応じて選ぶ。
- 外部consumerは正式な上流engineと既存のmodule pathで成立することを確認する。未公開依存やcheckout専用replaceで公開不能を隠さない。
- 実providerは必要な経路の確認に使う。今回の計画見直しでは呼び出さない。少数の実行だけでcache率や修正効率の一般的改善を主張しない。
- リリース検証は通常は軽くし、同一ソース・依存の有効な検証結果を再利用する。重い確認が本来必要ならリリース準備前に終える。
- CIは配布用ビルド・checksum作成・公開。ローカルで完了した検査を二重実行しない。リリース・配置・依存先への公開は別の明示指示に従う。

配布・検証方針は[AGENTS.md](../../AGENTS.md)と[release checkpoints](../releases/release-checkpoints.md)に従う。
旧計画の非実行link保証、任意module列挙、engine改造・派生配布、付随する最適化は、本計画の未達項目として復活させない。

## 7. 見直し後の計画レビュー

2026-10-23、会話履歴を継承しない独立した2担当が、通常レビューと批判的レビューを実施した。
各担当へ利用者の原文指示と本計画を渡し、各15分以内・read-only・再委譲なしで確認。
評価対象は、利用者指示との一致、許可範囲、責務・順序・受入条件、既存コードと公開APIでの成立性。

| レビュー | 結論 | 確認内容 |
| --- | --- | --- |
| 通常 | P1/P2/P3 findingなし。S1へ進める | 3本への限定、静的検査の文書限定、agent APIを含む情報提供、両ライブラリと共用資料の責務、run_js/結果/hook/履歴の対象根拠、検証とリリースの分担 |
| 批判的 | P1/P2/P3 findingなし。S1へ進める | 上流改変の権限拡大なし、新規統合機構・逆依存の強制なし、probeから保証への飛躍なし、「承認済み」という依頼元の記述から追加権限を推定しない境界 |

確認は計画・索引・必要な現行コード・正式上流の公開API・限定probeの記録に限る。製品の接続動作や受入試験は未実施。
計画レビュー時点で残した返却fieldと既存Go APIの互換性、JSON範囲、loader/Loop/Worker/TLA取消しとの接続、API情報の供給方式は、後続のS1設計で具体化した。[S1レビュー記録](ai-execution-contract-design.md#9-s1レビュー)を参照する。
このレビュー結論を、製品実装済みの証拠や上流改変・公開への追加許可として扱わない。
