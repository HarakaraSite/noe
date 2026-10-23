# AI向けコード実行契約の再設計・実装計画

更新日: 2026-10-23。
状態: 改訂案の通常レビュー・批判的レビューを反映した実装計画。製品実装・テストは未着手。

[MoonBit調査メモ](moonbit-ai-toolchain-notes.md)を起点とする採用範囲と実施順の正本。
調査メモの全候補を実装する計画ではない。今回の依頼は案と計画の更新までであり、
この文書の作成をもって製品実装、実プロバイダ要求、コミット、配置、公開へは進まない。

## 1. 目的と採用範囲

AIが覚える例外を減らし、APIの発見、コード生成、検査、実行、結果取得、失敗からの修正を
一貫した契約にする。既存APIとの互換性維持を必須条件とせず、
`CheckModuleSyntax`、run_js、docs、hook、履歴、出力制限も見直す。
既存の処理系へ説明だけを追加することや、選択肢を増やすこと自体を目的にしない。

| 対象 | 採用する方向 |
| --- | --- |
| コード | 通常のES moduleへ統一し、run_jsの暗黙importを廃止する。保存したJSとrun_jsで同じコードの書き方を使う |
| 検査・実行 | parse、依存解決、link、evaluateの規則と診断の取得経路を共有する。検査は到達段階と対象範囲を返す |
| データ結果 | データを返す標準の書き方を一つ決め、stdout、stderr、実行状態、診断から分離する |
| 診断 | エンジンが得た事実を構造化して取り出す。NOE分類、利用者例外のcode、実行段階を区別する |
| 自己記述 | 全カタログ、現在の実行環境での可用性、export、引数・戻り値、詳細説明の有無を区別して問い合わせられるようにする |
| hook | 新しい入力・結果契約へ統一する。実行した事実と、hookが加工してモデルへ渡した内容の因果関係を残す |
| 履歴・出力 | 内部の重複保持・転送を減らす。通常の抜粋、全文取得、失敗情報の保持を一緒に設計する |

データ結果の標準記法は、通常のESMを保つ`export default <値>`を第一候補とする。
例は`export default await operation()`。独自のトップレベルreturnや、
同じ目的の複数の返却関数を並べる前に、この方法で実用要件を満たせるかS1で確定する。
関数をexportしただけでNOEが自動実行する等の隠れた動作は導入しない。

今回の対象外は、意味を解釈したリポジトリ全体のsymbol探索・rename、言語の置き換え、
モデル内部のsampling制御。調査メモに残すが、今回の完了条件に含めない。

## 2. 責務と維持する実用契約

| 層 | 担当 |
| --- | --- |
| go-quickjs | parse・compile・link・evaluateの仕組み、位置・specifier等のエンジン事実、JS値の取得。NOE専用のdocsや履歴へ依存しない |
| stdlib / internal/jsexec | Profileとloaderに基づく検査・実行、VMとLoopの所有、取消し・cleanup、VM終了前の結果・診断のGoデータ化。NOE CLIやproviderを要求しない |
| agent library | run_jsのモデル向け契約、hook、会話履歴、結果の表示・取得、providerプロトコルへの変換。NOE固有の保存場所を固定しない |
| NOE | CLI、既定Profile・instructionの組立、保存先・設定・ログ表示のadapter。汎用実行機構を独自に実装し直さない |

- 各run_jsのfresh VM、owner goroutineでのJS操作、取消しとWorker・IOのcleanupを維持する。
  compile再利用のためだけに常駐VMへ変更しない。
- **同一実行内の不要な二重compileを避ける。** 独立した検査呼出しと実行呼出しは同じ規則を使うが、
  別VM間のcompile成果物再利用は必須にしない。再利用するなら所有・失効・source同一性と費用を示す。
- 非実行検査は「JS module本体を評価しない」ことと検査済み範囲を明示する。
  source読込みやcustom Go loaderまで副作用がないとは約束しない。
  dynamic importや実行時の値の正しさを、静的なlink成功で保証しない。
- 捕捉済み例外や、表示しただけの外部コマンド失敗から、run_js全体の失敗を推測しない。
  評価後の結果直列化失敗は、実行前の失敗と区別する。副作用済みの可能性を隠さず、暗黙に再実行しない。
- providerのnative items、opaque reasoning、tool call IDとの対応等、会話継続の必須情報を守る。
  内部表現は変更できるが、これらを単なる重複とみなして削除しない。
- instance内でqueueをまたぐsession/cache識別、安定したinstruction/tools、
  `NOE_CONTEXT_STATUS`を履歴として扱う方針を維持する。可変状態をinstructionへ混ぜない。
- 委譲権限・階層、子の寿命、画像の所有と配送を今回の改善理由だけで変更しない。
- engine変更はmodule cacheへ直接書き込まず、管理されたsourceで行う。
  開発時のlocal replaceと、外部consumerが取得できる候補の依存固定を区別する。
  upstreamへの書込み・公開はこの計画だけでは許可されたと扱わない。

## 3. S1で確定する契約

実装前に次の判断を同じ文書の設計節または相互リンクした契約文書へ記録する。
仮のフィールド名を先に全層へ配り、実装後に意味を合わせる進め方はしない。

| 判断対象 | 確定する内容 |
| --- | --- |
| 入力・origin | 保存JSとrun_jsのESM、明示import、virtual entryとfile entryの相対解決基準、hookが変更した実効sourceの同定 |
| データ結果 | 標準記法、未返却とnullの違い、JSON互換範囲、BigInt・循環・getter・toJSON等の扱い、直列化の取消しと失敗段階。画像bytesは混ぜない |
| 実行状態 | 正常・失敗・取消し・timeout・終了値とデータの関係、評価完了と結果取得完了の区別、実行済み範囲 |
| 検査 | 構文のみ/依存解決/linkのどこを提供するか、検査対象source・依存の同一性、未検査項目、loader error、検査の資源寿命 |
| 診断 | phase、source位置、specifier/name等の既知の事実、安定したNOE分類、利用者例外情報、不明値。候補とdocsへの案内は事実から生成し推測を断定しない |
| 自己記述 | catalogと現在の可用性、exportと説明項目の違い、署名・説明の正本、custom moduleの可用性が不明な場合。問い合わせのためにmoduleを実行しない |
| hookと履歴 | モデルの元呼出し、実効入力、実行結果、加工後結果の関係。providerが要求する履歴を維持したうえで、どの情報をどこへ保存・提示するか |
| 大きい出力 | data/stdout/stderr/diagnosticsの上限単位、抜粋形式、省略量、全文取得先・保持期間・終了後の挙動・保存失敗。agent libraryでNOE固有pathを固定しない |
| 画像と失敗情報 | 画像の別配送、hookの画像選択、診断が通常出力の省略へ埋もれない扱い、原結果と加工結果の参照 |
| 移行 | CheckModuleSyntaxの置換/責務変更、Workerのclassic/module判定、Go/JS API・型・docs・examples・fixtureの更新範囲、旧alias/説明の撤去 |

自己記述の情報は、runtime登録情報から確実に得られるものと、明示的に保守する署名・説明を区別する。
既存Markdownの見出し一覧を完全なexport一覧として流用しない。
普段必要な案内を小さく保ち、必要部分を問い合わせる形とする。

## 4. スライス計画

順序は **S1 → S2 → S3 → S4 → S5 → T（総合テスト）**。
S2以降は各スライスで **実装 → 対象テスト → コードレビュー → 指摘修正・対象再確認** を完了する。
S1は契約・比較基準を作成し、その整合性をレビューする。
対象・担当・変更範囲・実行コマンド・結果・finding採否を実装記録へ残す。
実装記録は実装着手時に作成し、この計画の完了状態と相互リンクする。

### S1: 契約と比較基準の確定

成果物:

- §3の契約と、engine→stdlib→agent→NOEのデータ経路・所有の設計。
- 破壊的変更一覧と移行対応。実装するengine sourceと、候補で固定する依存の取り扱い。
- 変更前revision・未コミット差分を特定した基準版、比較課題、成果の正解・完了条件、採取する指標。
  現在の作業ツリーにある他案件の変更と区別し、後から基準を取り違えない。

確認:

- 観測済みの暗黙import衝突、docsの「説明なし」と「APIなし」、module/exportの誤り、
  構造化データ、長い出力、hook加工、直列化失敗を、契約でどう扱うかを机上で通す。
- ESMのdefault export取得や非評価link等、設計判断を左右する不明点だけ、小さなlocal probeで確認する。
- 通常レビューは契約と実施順、批判的レビューは誤った保証、隠れた例外、層の逆依存、
  実行済み副作用と結果失敗の混同、比較指標の偏りを確認する。

終了条件: 標準的な使い方と失敗時の意味が説明でき、後段の実装判断を左右する未決事項がない。
効果比較の基準は変更開始前に保存する。コードの完成や速度改善はこの段階の合格条件ではない。

### S2: go-quickjsの検査・実行・診断の共通基盤

対象: engineのmodule parse/compile/link/evaluate経路と構造化診断・export取得。
現行`CheckModuleSyntax`を固定せず、S1の契約に従って見直す。

成果物:

- 同じ解決・link規則を使う検査と実行、検査済み範囲、元sourceに対応する診断。
- ESM結果を取得する仕組みとowner/VMの寿命契約。
- 通常の実行が検査用compileを重ねない経路。別呼出し間の再利用を必須にしない。

対象テスト: 構文失敗、依存解決失敗、存在しないexport、依存先の位置、循環依存、
top-level await、未捕捉例外。非実行検査でentry/依存moduleの本体が走らないこと、
取消しと解放、実行との解決規則の一致を確認する。実行時だけ分かるdynamic importは検査結果に正確に残す。

レビュー重点: 誤った検査保証、同一実行内の二重compile、JS値の所有、
engineへのNOE専用概念の混入。上流全体の再監査や仕様テストの複製は行わない。

終了条件: 必要な機構がengine単体で確認でき、NOE側から利用できるlocal候補と変更記録がある。
外部公開の有無と、再現可能な依存固定の残作業を明示する。

### S3: stdlibの実行結果・検査・診断API

対象: `internal/jsexec`、公開`stdlib`、loader/Profile、Workerの検査API利用箇所。

成果物:

- S2の機構を共通実行経路へ接続した検査・実行API。
- data、実行状態、診断を**VM終了前に切り離したGoデータ**として返す処理。
- 検査にも適用する実行環境・loader・originと、CLI表記に依存しない結果。
- 新契約に合わせたWorkerのclassic/module判定と、旧CheckModuleSyntax利用箇所の移行。

対象テスト: VM終了後の結果・診断利用、未返却/null/直列化失敗、source位置、
custom loader・異なるProfile、fresh state、top-level await、取消し、Worker cleanup、
借用資源の扱い。公開stdlibだけを使う外部Go consumerからも確認する。

レビュー重点: owner外のJS操作、VM所有値の漏出、終了待ちの退行、
stdlibへのagent/CLI/provider依存、検査と実行の規則のずれ。

終了条件: CLIやagent Hostなしで新契約が利用でき、既存の取消し・回収契約を満たす。

### S4: 新run_js・自己記述・agent経路の接続

対象: `internal/agentexec`、`internal/agent`のtool schema・task/hook/history・broker・provider変換、
`internal/apidocs`、公開agent、CLI docs、組込み案内、関連型・examples・利用ガイド。

成果物:

- 暗黙importを廃止し、S1の標準記法でdataを返すrun_js。
- 新しい診断と、catalog/可用性/export/署名/説明を区別した自己記述。
- **run_js → hook → 履歴 → provider要求まで新契約が通る最低限の完成経路。**
  validatorやprovider向け表現の更新をS5へ先送りしない。
- 生成コード・実効コード・実行結果・加工結果の対応、画像の別配送。
- 新しい書き方に揃えたinstruction・内蔵docs・型・examplesと移行説明。
  旧暗黙束縛や旧結果形式を推奨する説明を残さない。

対象テスト: 同じESMを保存JS/run_jsで実行、明示agent import、構造化結果のprovider到達、
beforeToolによるコード変更、afterToolによる結果変更、部分Profileとcustom module、
説明だけ欠けたAPI、未知module、画像、親子agent、queueでの会話継続。
Chat/Responsesと既存SDK adapterをlocal fixtureで通し、native items・reasoning・call ID対応も確認する。

レビュー重点: AI向け記法の例外、自己記述の誤情報、返却fieldの脱落、
加工による事実の取り違え、provider継続の退行、docsと実装の不一致。

終了条件: 新run_jsがモデルの次のステップまで利用できる。旧形式の内部adapterを仮置きする場合も
利用箇所と撤去先を記録し、S5終了時に残さない。

### S5: hook・履歴・出力の最適化と移行完了

対象: S4で接続済みの経路の重複保持・転送、出力capture、結果保存・取得、ログと表示。

成果物:

- 実行事実・モデル向け抜粋・保存用全文の所在と参照を明確にした保持方法。
- 大きいdata/stdout/stderrの上限と省略通知、必要時の全文取得、保持終了・取得失敗の明示。
- 診断・取消し・timeout・結果取得失敗を読み取れる表示。
- providerが必要とする履歴を維持した内部の重複削減。安定した送信内容とsession/cache識別。
- 旧結果形式、旧検査入口、不要adapter・alias・旧説明と、契約に意味のない重複テストの整理。

対象テスト: 上限境界と多byte文字、長いdataと大量stdoutの併存、失敗時の診断保持、
全文取得の正確さ・保持終了・保存失敗、hook加工前後の参照、並行toolと取消し、
queue継続・native履歴・画像の配送。関連する転送量・保持量を変更前と測定比較する。

レビュー重点: 必要情報を削って見かけの効率を上げていないか、無界の保持、
原結果と加工結果の混同、保存先のNOE固定、cacheを壊す可変instruction、旧経路の残存。

終了条件: 新契約の移行・最適化が完了し、総合テスト対象を固定できる。
軽微な表示変更まで個別の新agent・重複した試験群へ分割しない。

## 5. 最後の総合テスト T

S1〜S5の対象確認・コードレビューを終えた候補ソースを固定し、最後にまとめて行う。
修正が入れば影響範囲を再確認し、候補を更新する。過去版の合格を今回の結果へ転用しない。

| 検証 | 方法と確認内容 |
| --- | --- |
| 全体unit/E2E | `CGO_ENABLED=0 go test -tags=e2e ./... -count=1`。ローカルprovider fixtureでCLI・agent・stdlibを通す |
| race | `CGO_ENABLED=1 go test -race ./internal/... ./stdlib/... ./agent/... -count=1`。結果共有、hook、保存、取消し・終了の経路 |
| 長待機・取消し | `CGO_ENABLED=0 go test -tags=slow_integration ./agent -run '^TestGoClientLongModelAndToolWait$' -count=1` |
| 静的確認 | `CGO_ENABLED=0 go vet -tags=e2e ./...`、`git diff --check`、旧契約の残存と文書リンクの確認 |
| 6対象compile | linux/darwin/windows各amd64/arm64で`CGO_ENABLED=0 go list -export ./...`。native実行とは区別する |
| 外部consumer | `scripts/check-stdlib-consumer.py`のstdlib/agent両モード。local replaceだけでなく合成module artifactからbuild/runし、engine依存も再現可能に解決できること |
| 候補CLI・examples | 一時領域へbuildし、run/eval/Worker、内蔵docs、agent/tool、hook、画像、ログ表示・解析を確認。`examples/VALIDATION.md`の有効な全JS入口を新契約で実行し、認証等が必要なものは条件と未実施を明示 |
| 新契約の横断課題 | API発見→生成→検査→実行→構造化結果→次のmodel要求、誤生成→診断→修正、長い結果→抜粋→全文取得を、実際の入口を通して確認 |
| 成果と効率の比較 | S1で固定した課題を基準版と候補へ同条件で与え、下記の指標と成果を比較 |

engineの変更がある場合はS2で確定したengine側の対象テストも候補revisionで実行し、
NOEのテストだけでengineの検証済みとはしない。開発用replaceが必要な段階と、
候補を外部consumerから再現できる段階を区別し、後者を満たさなければ総合完了にはしない。
upstreamへの公開・リリースが必要なら、その外部操作は別途扱う。

### 成果と効率の比較方法

- 固定する課題は、観測済みのimport衝突、docs探索、import/export誤りの修正、
  データを使う複数ステップ作業、大きい出力の必要箇所取得を中心にする。
- **作業完遂と成果の正しさを先に判定する。** 成果物・返却データ・必要情報を独立に検査し、
  早期断念や情報欠落を効率改善として数えない。
- そのうえで修正往復、tool/model呼出し数、読込み・返却・転送量、時間、token使用量、
  親/子別のcached/inputと合計、子の起動理由・数・再利用を観測する。
- prompt cacheは初回を含む率と初回を除く率を分け、基準版/候補の課題・model・
  instruction・provider・反復条件の違いを記録する。固定の90%等を合格条件にしない。
- fixtureで確定できる正しさと、実modelでしか測れない生成・修正行動を区別する。
  実プロバイダ比較は費用発生を伴うため、実施時に有効な利用者の許可範囲で行う。
  未実施ならAIの迷いやcacheが改善したとは結論せず、その部分を未検証として残す。

## 6. レビュー指摘の反映と進行管理

改訂案は通常レビュー・批判的レビューとも詳細設計へ進める判断。
前回P2は、破壊的変更を選ぶ方針の明示、catalog/可用性/export/説明の分離、
engine事実とNOE分類・利用者例外の分離により方針上解消した。
これは実装の正しさが検証済みという意味ではない。

| 改訂案への指摘・補足 | この計画の対応 |
| --- | --- |
| P3: 二重compile禁止の単位 | §2で同一実行内に限定。別呼出し間の再利用は所有・費用に基づく設計判断とした |
| P3: provider継続に必要な履歴 | §2とS1の制約、S4とS5の対象試験へ明記 |
| P3: 効果比較に作業成果を含める | S1で正解・基準版を固定し、Tで完遂・正しさを先に判定 |
| 補足: VM所有値をagent層だけで扱わない | S3でVM終了前にGoデータへ切り離す |
| 補足: S4でhook/history/providerへの接続を完成 | S4の終了条件へ明記。S5は成立後の最適化 |

独立レビューは実装・契約の具体的な境界を確認するために使い、スライス数や役割名だけでagentを増やさない。
修正・再確認は元の担当を再利用する。初回30分以内、10分証拠更新なしなら暫定結果を受け取り、
再確認は変更箇所・既存findingを原則1回15分以内で確認する。
必要な通常/批判的視点は依頼に明記し、各回で両担当を無条件に新規起動しない。

defaultがfindingの採否、計画との対応、総合判定を行う。
目的達成を妨げる未解消のP2以上があれば、修正または利用者との方針整理を行ってから進む。
件数目標のテストや、旧仕様に合わせて新契約を弱める期待値は追加しない。
最終記録にはcandidate/engine revision、コマンド、合否、初回失敗と原因、再確認、未実施範囲、
成果・効率比較の条件と限界を残す。コミット・配置・リリースは別の明示依頼として扱う。
