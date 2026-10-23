# トークン効率改善：実装計画 — 2026-10-24

状態：実装・通常レビュー・批判的レビュー・検証を完了。[実装・検証記録](token-efficiency-implementation.md)と[測定結果](token-efficiency-results.json)を参照。

## 目的と合意範囲

対象はdocsの重複解消、run_jsのモデル向け出力制限、openai-chatgptの生JS入力。
同じ仕事を完了するためにモデルへ追加する本文と生成時のJSONエスケープを減らす。
キャッシュ済み履歴を途中で書き換えて縮める方式にはしない。

- モデル向け本文予算の既定値は3 KiB（3,072 UTF-8 bytes）。stdout・stderr・dataだけでなく、診断本文とhookの任意JSONも同じpoolに含める。短い実行状態・省略通知は別の固定metadataとする。
- 超過時は表示量・省略量・不完全であることを明示し、検索条件や対象、返却形式の見直しを案内する。
- 実行成否と出力超過を区別する。ソースの自動再実行や全文artifactの自動保存は追加しない。
- 生JSはまずopenai-chatgptで採用する。他providerは現行JSON入力を維持する。
- 同一応答で複数run_jsを生成することは採用条件にしない。JS内のPromise.allは維持する。
- effortとrole設計、compaction、ファイル監視、独立agent実行は今回の対象外。
- リリース、配置、コミット、プッシュは今回の実装依頼に含めない。

比較開始時のソースは`46ae6f5`。既存の未追跡イベントログは変更しない。

## 根拠

直近の課題ではdocs取得を含む4回のstdoutが計68,414文字。
うちglobalsの7詳細を取得した出力38,375文字には約4,897文字の共通概要が7回含まれた。
共通概要の重複6回だけで約29,382文字を削減できる。文字数であり、トークン削減率ではない。

NOE v2.3.0とCodex 0.164.0の実サービスprobeを実施済み。
gpt-6.1-sol/low/ChatGPT認証では直接呼出しの複数生成は再現せず、Codex Code Modeの生JS内Promise.allによる並行処理は成立した。
probe正本は`/var/tmp/noe-parallel-runjs-f890hunl/parallel-investigation.json`、
`/var/tmp/noe-codex-live-probe-vk5hyxkd/comparison.json`。
これらは今回変更後の検証結果として流用しない。

## A. docsの重複解消

- `docs(topic?, member?)`とCLI引数は維持する。
- member取得時は全文overviewを付けず、topic ID・短い概要・必要な利用条件と指定detailを返す。
- `docs(topic)`はoverviewとmember一覧を維持し、長い全グローバル一覧などの参照情報も失わない。
- 重要なmoduleの公開名、Profileによる有効化、Nodeとの差異など、detailの正しい利用に必要な条件を省かない。
- 既存資料のoverviewにしかない利用条件を確認し、共通の短い説明へ残すかdetailへ移す。
- `noe:docs`の返却説明と関連テストを更新する。新しい汎用検索・batch APIは必須にしない。

主な対象：`internal/apidocs/catalog.go`、関連contentとテスト。

## B. モデル向け出力制限

### 適用位置と保持

- runner自体のResultや公開Go実行APIには3 KiB制限を加えない。stdout/stderrの既存64 KiB捕捉上限は別契約として維持する。
- `afterTool`に渡す実行結果は制限前のままにし、その加工後にモデル向けprojectionを作る。
- Hostの`tool_result`でprojectionを生成し、呼出し元へ返す。JS task loopはその値を履歴へ追加する。
- `run_js.finished`と`tool.finished.rawResult`は実行事実を維持する。`tool.finished.result`は実際にモデルへ返した値に一致させる。
- hookで加工された制限前の値は、元実行結果と異なりprojectionで失われる場合だけ、追加フィールドに保持する。
- 画像の専用配送、終了状態、hook失敗時のmodelResultAvailable、call IDと所有関係を維持する。

### 返却形式の案

- afterTool加工値のJSON表現が予算内なら既存resultの形を変えない。
- 超過時は全体JSONの先頭・末尾を残す`outputPreview`と、`outputLimit`、`execution`からなる専用envelopeへ変換する。`data`として不完全な値を返さない。
- UTF-8境界を守る。previewは不完全なJSONテキストであり、完全なJSONとしてparseしないと説明する。
- `execution`のexitCode/reasonと短いdiagnosticSummaryはrawResult由来とし、hookが作った任意JSONのfieldとは区別する。
- diagnosticSummaryには本文予算の最大1/4（既定768 bytes）を予約し、残りをpreviewへ使う。診断がなければ全体をpreviewに使う。
- diagnosticsのexception.stdout/stderr、長いmessage/stack、hookの独自JSONも同じ全体JSON previewに入るため、長文が別fieldから上限を迂回しない。
- `outputLimit`でlimitBytes、sourceBytes、shownBytes、omittedBytes、returnedBytesと`complete:false`を示す。shown/omittedは元JSONの残したbytesと省略bytes、returnedはpreviewと診断summaryの本文bytes。固定metadataは含めない。captureによる省略は元結果の別契約として説明する。
- 案内は「検索なら条件や対象を絞る、該当ファイル名や集計値だけ返す。必要な本文は部分取得する」。検索結果だと内容から断定しない。

### 必要な大出力の取得

既定値は小さく保ち、呼出しごとの明示指定として先頭の連続する行コメント内の
`// @noe: {"outputBytes": 12288}`を採用する。操作を説明する英文コメントを1行目、pragmaを2行目に置けるようにし、観察表示の操作説明を保つ。
JSON入力・生JSの両方で同じソースコメントを使い、通常のJSとしても有効にする。
beforeToolでコードを変更した場合は実効コードの指定を使う。
指定は正のsafe integer、既知のoutputBytesのみとする。不正指定は実行前のsetup診断として返し、副作用後に指定ミスで結果全体を失わせない。
既定を超えた検索を機械的に大きな予算で再実行する指示は加えない。
runJSTool.description、facts、noe:run_js参照、execution-resultsガイドへ、既定値・preview・pragma・完全なdocs detail取得の方法を事前提示する。

主な対象：`internal/agent/broker_tool_result.go`、`broker_tools.go`、toolCallState、`js/task.js`、新規projection helperとテスト。

## C. openai-chatgptの生JS入力

- provider Definitionへrun_js入力形式のデータ設定を追加する案。既定JSON、openai-chatgptはJavaScript。
- Responses adapterでrun_jsのfunction定義をcustom toolへ変換する。通常のJS module原文を受け取り、コードの加工・再実行はしない。
- custom_tool_callは内部の既存function形式・`{code}`へ正規化し、runner・beforeToolの実効argumentsを維持する。
- 原文のJSはnormalized tool_callの内部carrier `_noe_raw_input` とnative output itemで保持し、JS wrapperでrawArgumentsへ渡す。JSON形式providerでのrawArgumentsの意味は変えない。
- 結果返却はcall IDに基づきcustom_tool_call_outputとfunction_call_outputを選び、mixed historyでも誤らないようにする。
- native reasoning、未知フィールド、assistant phase等は既存のraw item保持を維持する。
- provider設定による明示的な形式選択にし、エラー後の自動fallback・ソース再実行は追加しない。
- API入力形式はprovider境界で選ぶ。JS task、agentの公開config、Go embeddingへ特定providerの分岐を広げない。

主な対象：`internal/agent/provider/config.go`、`defaults.json`、`responses.go`とテスト、hookへの原文受渡し部分。

## 検証と完了条件

1. docs：概要の参照性と個別detailの利用条件を確認。同じ7詳細の出力量を測り、重複削減を示す。
2. projection：境界bytes、日本語、stdout/stderr/data混在、超過JSON、診断内の大きなコマンド出力、hook加工、画像、並行callごとの予算を確認。
3. 履歴：モデルに返したprojectionと記録・次要求が一致し、旧履歴を書き換えず、元の実行成否と出力を追えることを確認。
4. provider：custom定義、生JS原文、ID対応、native reasoning保持、次ターンのcustom結果、JSON providerの維持をローカルfixtureで確認。
5. 実サービス：変更後バイナリでopenai-chatgptの生JS実行から最終回答まで、検索出力超過から条件を絞り直して完了する流れを少数の限定課題で確認。リトライ回数の多さを目標にしない。
6. 変更箇所のunit/E2Eを実装中に行い、全体unitは変更が落ち着いた段階で一度まとめる。失敗や追加変更がなければ同じ検査を繰り返さない。

トークン削減率や課題成功率の改善は実測前に保証しない。
実機結果・残る制約・元出力とモデル向け出力の関係を実装記録へ残す。

## レビュー依頼

- 通常レビュー：要件との一致、既存API・hook・provider・履歴の整合、実装可能性と検証の妥当性。
- 批判的レビュー：3 KiBで往復が増える経路、上限の抜け道、意味を失うprojection、不要な設定・複雑化、効果を過大評価している箇所を疑う。
- 判断基準は合意した利用目的と現在の実行経路。仮想的な将来用途や網羅性だけで追加範囲を要求しない。

## 計画レビュー結果

通常・批判的レビューを別々のread-only reviewerが実施。双方ともBlocker/P1なし。
必要なP2指摘は、操作説明とpragmaの両立、診断を含む共通予算の明確化、tool descriptionと内蔵docsへの事前提示。
defaultはすべて採用し、上記へ反映した。通常reviewerは全体JSON preview方式が既存hook/履歴境界と整合すると確認した。
critical提案に従い、任意JSONの再帰的配分・構造保持ではなく、少数の予約枠と専用preview envelopeへ単純化した。
実モデル課題では往復数と取得できたusageも記録し、文字数削減だけを成功判定にしない。
