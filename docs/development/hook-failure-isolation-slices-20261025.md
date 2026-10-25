# hook例外局所化 — スライス実装・レビュー手順（2026-10-25）

[機能計画](hook-failure-isolation-plan-20261025.md) · [議論メモ](hook-isolation-context-api-discussion-20261025.md) · [実施記録](hook-failure-isolation-implementation-20261025.md)

## 依頼・作業順

利用者から、計画のスライス化 → 独立レビューと批判的レビュー → P2以上の指摘がなくなるまで計画修正・再レビュー → スライスごとの実装・テスト・コードレビュー → 最終総合テスト → commit、の実施指示を受けた。
本書は `74e7056` を基準とする実行用の分割。機能契約は元の計画を正本とする。push・リリース・依存更新は行わない。

開始時からある議論メモ・計画・参照リンクの変更は今回のdocsとして保持する。未追跡の実行JSONLとPythonキャッシュは変更・追加コミットしない。

## 製品として満たす動作

- hookは検査器や承認機構ではなく、任意の付随処理。登録済み4handlerの例外・reject・返却値検証失敗を各hookに閉じ込める。
- 直前の有効値で後続hookと本体を継続。先行の置換・副作用・実行結果・画像を保持し、toolやhookを自動再実行しない。
- hook.failedを残し、次の通常モデル要求に事実通知を同送。最終応答後は追加モデル要求を強制せず、TaskOutcome.hookFailuresに失敗を保持。
- 取消し・既存実行上限・Host/scopeの継続不能を警告へ変えない。
- 登録/読込みの準備失敗、トリム外部化、承認、実行見送り、汎用公開通知APIは対象外。

## 計画レビューの進め方

1. 同じ正本を読む独立した2名へ、通常レビューと批判的レビューを並行で依頼する。両名は実装を行わない。
2. 各指摘はP0〜P3、根拠箇所、実利用での失敗経路、必要な修正を示す。P2以上を未解決のまま実装へ進めない。
3. 親が計画を修正し、指摘した元のreviewerへ再レビューを戻す。両者の最終見解がP2以上なしになるまで反復する。
4. 単なるテスト不在・未観測variant・別案の好みを指摘の根拠にしない。通知だけの追加モデル呼出しをしない等の採用済み契約は、その契約に具体的な欠落がある場合に指摘する。
5. 指摘・対応・判定は実施記録に残す。追加の再委譲は行わない。

## 分割と依存関係

`S1 → S2 → S3 → 最終総合テスト → commit`。
全スライスが一つの機能候補を作る。途中状態のリリースはしない。モデル通知未接続のS1を完成品として扱わない。

### S1：hook単位の例外境界・結果保持

**目的:** ファイルI/O等でhandlerが失敗しても、後続hookとモデル/tool本体が進み、失敗をoutcomeから取得できる。

担当範囲：
- JS共通hook runnerでhandlerと返却値検証だけを回復する。withHookAPI全体の例外を一括で握り潰さない。
- Hostのscope開始/終了・継続不能とhandler失敗を型/状態で区別する。callbackとscope終了の両方が失敗しても内部障害を隠さない。
- HookFailureとrun所有の記録を作り、同じinvocationの重複登録を防ぐ。
- TaskOutcome.hookFailures、Go公開型alias、JS型、投影・保持結果へ接続する。コピーを変えて保持元を書き換えない。
- tool pipelineまで同時に接続し、回復済みafterTool失敗でHookError/modelResultAvailable:falseを立てない。元結果・先行の結果加工/画像選択・並列toolを失わない。
- 旧afterTool失敗fixtureを変更し、全4handlerの続行、後続hook、置換値、結果/画像、実行回数と取消しの関連テストを追加・更新する。
- S2で使うpending識別子と送付状態の内部表現を用意する。ここではモデル通知の同送までは完了条件にしない。

主な変更先：
internal/agent/js/hooks.js、tool_pipeline.js、broker_hooks.go、必要なhook-scoped bridge、run状態、instance.go、task_lifecycle.go、agent/host.go、agent/types/noeagentlib.d.ts、関連テスト。

完了条件：
- 関連fixtureでtaskはcompleted、後続hookと並列toolは完了、対象toolの作用は一回。
- 元結果/画像/成立済み変更を保持し、hookFailuresとhook.failedのID・発生元が一致。
- 取消し・上限・scope/Host継続不能の終了経路は維持。正常hook、開始時設定/読込みエラーの現行契約も保持。
- 対象Go/JS/宣言の必要な検査が通り、独立コードレビューでP2以上がない。

### S2：通常モデル要求への通知と配送状態

**依存:** S1の失敗記録・pending状態・結果保持。

**目的:** taskが次の通常判断でhook失敗を知れ、送付されない最終失敗も保持結果から確認できる。

担当範囲：
- NOE_CONTEXT_STATUS内に失敗の事実を同送する。人間のsteer・保護指示・新しい履歴kindは作らない。
- 通知本文を最終準備と概算に含め、実際に送るID batchだけをprovider admissionで送付済みにする。
- context_get/観察/準備では消費せず、steer-only戻りでも失わない。古いbatchの準備や再読取りで別の失敗を誤って消費しない。
- source requestIdと通知先requestIdを区別し、通常の会話blockへ保存する。pendingはtrim対象履歴だけに置かない。
- 最終応答後はcommit_finalをhook失敗だけで拒否せず、追加要求/追加上限枠なしで完了する。
- keepAliveの次taskにpendingを自動移送しない。既存会話prefixと旧outcomeの不変性は保持する。
- 4phaseの配送、最終失敗、steer再準備、概算/trim、結果投影・queueをローカルfixtureで確認する。

主な変更先：
internal/agent/context.go、broker_context.go、broker_model.go、js/task.js、必要な内部型・結果投影、関連テスト。

完了条件：
- beforeModelRequest失敗はその要求、応答/tool後の失敗は次の通常要求へ載る。
- 同じinvocationの新規通知は二重挿入されず、steer-only戻りで未送付分を失わない。
- 毎回afterModelResponseが失敗しても、最終応答後の追加要求がなくtaskが終了する。
- includeAnswer:falseでも失敗を取得でき、read_task_resultの原文に警告を混ぜない。
- 関連検査と独立コードレビューでP2以上がない。

### S3：公開利用経路・説明の完成

**依存:** S1/S2の製品動作。

**目的:** Go/JS利用者が変更後の例外契約と通知の受け取り方を理解・利用でき、旧終了挙動と混同しない。

担当範囲：
- hooks/API/events/observeと内蔵agent docs、必要なfacts/policy、公開宣言の整合。
- throwでtaskを止める旧挙動が変わり、beforeTool例外後も直前の引数で実行する点を明示する。
- hookFailures、未送付/送付済み、最終応答後にモデルが必ず読む保証はないことを説明する。
- JSONL保存なしのJS実利用、Go公開Clientの保持結果、wait_task/wait_tasks/observeの必要な経路を確認する。既存fixtureを再利用し、同じ動作の重複テストを増やさない。
- 必要な全機能結合テストを完成させ、実施記録を更新する。製品動作の具体的な不足が見つかれば元のスライスへ修正を戻す。

完了条件：
- 現行利用資料に「handler例外はtask失敗」という旧契約が残らない。過去の検証記録は当時の記録として保持。
- 公開型/例と実装が一致し、任意フィールドは失敗がない時省略される。
- 影響する公開利用経路の関連テスト、JS lint、宣言型確認、差分検査が通る。
- 独立コードレビューでP2以上がなく、最終批判的レビューでも未解決のP2以上がない。

## スライスごとの実装・レビュー

- 実装担当は同じ担当に保持し、各スライス完了時に変更箇所・判断・関連テスト・残件を返す。
- reviewerは実装担当とは独立。元の通常reviewerをコードレビューに使い、修正後の再確認も同じreviewerへ戻す。
- コードレビューのP2以上は修正→関連テスト→再レビュー。次スライスへ未解決のP2以上を持ち越さない。
- S3候補では通常レビューに加え、元の批判的reviewerへ横断レビューを戻す。既知の採用契約を勝手に変えない。
- 一つの関連変更だけを理由に全体検査を繰り返さず、各sliceは影響箇所のテストで検証する。

## 最終総合テストとcommit

実装・レビュー修正が落ち着いた候補へ、親がまとめて実行する。

- 全体unit：`go test ./...`
- ローカルfixtureの全体E2E：`go test -tags=e2e ./tests -count=1`。e2e build tagの対象はtests配下で、tests/README.mdの正規コマンドに従う。unit全体をもう一度実行するための`./...`は使わない。
- 変更したHost/scope/bridge/runtimeと公開agent利用の同期に対するrace検査。対象packageは実差分で確定する。
- Go整形・必要なvet、変更した保存JSのBiome lint、公開宣言/利用例の型確認、`git diff --check`。
- 実際のJS/Go利用fixtureで得られた失敗通知と正常完了の証拠を実施記録へ残す。
- 失敗があれば変更処理からの到達経路を調べ、今回の退行を修正する。既存別件は証拠と影響を分ける。再検証は修正の影響範囲で決める。
- 必要な根拠が揃ったら対象ファイルを明示してcommit。生成ログ/キャッシュは含めず、pushやreleaseはしない。
