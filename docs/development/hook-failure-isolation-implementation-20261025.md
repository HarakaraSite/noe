# hook例外局所化 — 実装・検証記録（2026-10-25）

[機能計画](hook-failure-isolation-plan-20261025.md) · [スライスと手順](hook-failure-isolation-slices-20261025.md) · [議論メモ](hook-isolation-context-api-discussion-20261025.md)

## 状況

- 基準：`74e7056`。
- 利用者指示：計画の通常/批判的独立レビューをP2以上なしまで反復後、スライスごとに実装・テスト・コードレビュー、最後に総合テストしてcommit。pushなし。
- 現在：S1〜S3、最終の通常/批判的レビュー、親の総合検査が完了。未解決P2以上なし。以下の実装・docsをcommit対象とする（push/releaseなし）。
- 実行ログとPythonキャッシュは対象外。既存の議論メモ・計画・入口リンクは保持。

## 計画レビュー

通常の独立レビューr1と批判的独立レビューr1を並行で行い、両者ともP0〜P3の修正要求なし、未解決P2以上なし。計画修正による追加ラウンドは不要と判定した。

確認対象は4handlerの境界、scope/Host継続不能、画像・並列tool、通知batch/admission、最終応答後の終了性、trim/queue寿命、公開Go投影の不変性。詳細は `/var/tmp/noe-hook-isolation-review-plan-normal-r1.md` と `/var/tmp/noe-hook-isolation-review-plan-critical-r1.md`。

レビュー時にHEADが `e0c01fa` へ進んでいることを確認。`74e7056..e0c01fa` は README.md、README.ja.md、docs/go-embedding.md、docs/go-agent-embedding.md の4文書のみで、確認対象の製品コードは同一。これらの既存コミットを保持し、巻き戻さない。

## スライス実施

| slice | 実装 | 関連検証 | 独立コードレビュー |
| --- | --- | --- | --- |
| S1：例外境界・結果保持 | 完了 | 関連Host/bridge/runtime/Go公開Client・型/lint/整形/差分：成功 | 通常/批判的r1：P2以上なし |
| S2：通常モデル要求への通知 | 完了 | 関連Host/runtime/Go公開Client・型/lint/整形/差分：成功 | 通常/批判的r1：P2以上なし |
| S3：公開利用経路・説明 | 完了 | 限定CLI E2E・型/lint/整形/差分/リンク：成功 | 両r2：文書P2解消、P2以上なし |

### S1の実装・検証

handler本体と返却値検証の局所化、Go側のscope/RPC継続不能の記録と終了時確認、run所有HookFailureとoutcomeへのdeep clone、Go alias/JS宣言、toolパイプラインの正常結果確定を実装した。モデルへの同送はS2で接続する。

関連検査はHostのhook/model-step/投影、bridgeの実JS境界fixture、runtimeの4phase・正常順序・置換・画像・並列・取消し/上限・module寿命、Go公開Clientの保持結果/queue、Biome lint、公開宣言tsc、Go整形、差分確認を実施して最終成功。全体unit/E2E/raceは未実施。

詳細とコマンド・ログ：`/var/tmp/noe-hook-isolation-s1-implementation.md`。主要ログは同prefixの `-unit.log`、`-bridge.log`、`-runtime.log`、`-public-final.log`、`-cancel-r2.log`、`-lint.log`、`-types.log`。初回のfixture記述・loader/FS設定・既存typed投影の誤った期待は修正済みで、最終ログと区別する。

S1コードレビューは通常・批判的ともr1でP2以上なし、P3修正要求もなし。通常reviewerは限定fixtureも独立実行して成功。詳細：`/var/tmp/noe-hook-isolation-review-s1-normal-r1.md`、`/var/tmp/noe-hook-isolation-review-s1-critical-r1.md`。受入時の製品ソースsnapshot：`/var/tmp/noe-hook-isolation-s1-snapshot/`。

### S2の実装・検証

run所有の失敗をNOE_CONTEXT_STATUSへ同送し、Hostが最終準備本文とinvocation ID batchを保持する。実送付本文を照合してprovider admissionでそのbatchのみ送付済みにし、準備/読取り/steer-onlyでは消費しない。通知込み概算と既存block保存・trim、最終応答後の追加要求なし、旧task pendingの非移送を接続した。

関連Host通知/context/model-step、runtimeの4phase・steer再準備/概算/trim/上限/取消し、Go公開Clientの配送状態/queue、保存JS lint、公開宣言型検査、Go整形、差分確認が成功。詳細・正確な実行コマンドは `/var/tmp/noe-hook-isolation-s2-implementation.md`、主要ログは同prefixの `-unit.log`、`-runtime.log`、`-public-initial.log`、`-public.log`。公開Goの一部selectorは省略名を完全一致にしていたため、そのログの配送fixture未選択を明記し、実際の配送fixture成功は直前の `-public-initial.log` を根拠とする。

S2コードレビューは通常・批判的ともr1でP2以上なし、P3修正要求もなし。通常reviewerは限定fixtureを独立実行して成功。詳細：`/var/tmp/noe-hook-isolation-review-s2-normal-r1.md`、`/var/tmp/noe-hook-isolation-review-s2-critical-r1.md`。受入時のソースsnapshot：`/var/tmp/noe-hook-isolation-s2-snapshot/`。

### S3の実装・検証

hooks/API/events/observe・実行結果・画像・Go組込みガイド、内蔵agent docs、factsと公開型コメント/positive例を現行契約へ整合した。過去の計画/検証記録は書き換えていない。JSONLなしのCLI結果取得とobserve後の正常完了、配布用にコピーした単独バイナリ内の新docsを限定E2Eで確認した。S1/S2の実行ロジック修正は不要だった。

限定E2Eは `go test -tags=e2e ./tests -run '^(TestContextObservationRunAndEval|TestHookFailuresCLIWithoutEventFile|TestDocsStandaloneBinaryAndManagedEval)$' -count=1 -v` で成功。保存mjsのBiome lint、公開宣言とCLI fixtureのtsc、Go整形、差分、利用資料のローカルリンク57件も成功。Go公開Client等の受入済みS1/S2証拠は再利用。詳細：`/var/tmp/noe-hook-isolation-s3-implementation.md`、ログは同prefixの `-cli-e2e.log`、`-lint.log`、`-types.log`、`-fixture-types.log`、`-doc-check.log`。

### 最終レビューr1と修正

通常S3レビューと横断批判的レビューの双方が、同一のP2（N-S3-01：docs/future-candidates.mdの「現在」の説明にhandler例外がtask全体を失敗させる旧契約が残る）を指摘した。beforeToolのthrowを停止手段と誤認し得る実利用影響があるため修正対象とした。

親が所有する候補一覧の現行説明を、handler例外の局所化、直前の有効値による続行、beforeToolはskipにならないこと、モデル/保持結果への通知、開始時準備・取消し/上限等との区別へ更新。製品コードは未変更なので文書の再照合・差分検査で確認し、同じ両reviewerへr2を戻す。その他のP2以上/P3修正要求はなかった。

詳細：`/var/tmp/noe-hook-isolation-review-s3-normal-r1.md`、`/var/tmp/noe-hook-isolation-review-final-critical-r1.md`。

最終レビューr2は、元の通常/批判的reviewerの双方がN-S3-01（同一P2）の解消を確認し、新たなP2以上なし・P3修正要求なしと判定した。対象文書のみの修正なので、製品テストの途中再実行は行わず、次の最終総合検査へ進む。詳細：`/var/tmp/noe-hook-isolation-review-s3-normal-r2.md`、`/var/tmp/noe-hook-isolation-review-final-critical-r2.md`。

## 最終確認

最終レビュー修正後の同一候補に対し、親が以下を一回まとめて実行し、すべて成功した。途中のスライス検査に全体unit/E2E/raceは使っていない。最終検査の失敗・製品コードの追加修正・全体再実行はなかった。

環境：Linux amd64、Go 1.27.2、gcc 14.2.0、Biome 2.5.15、TypeScript 5.9.3。Go一時領域は既存設定の `/var/tmp/noe-gotmp`。

| 検査 | 実行内容 | 結果 |
| --- | --- | --- |
| 全体unit | `go test ./... -count=1` | 成功 |
| 全体ローカルE2E | `go test -tags=e2e ./tests -count=1` | 成功 |
| 影響packageのrace | `CGO_ENABLED=1 go test -race ./internal/agent ./internal/agentbridge ./internal/runtime ./agent -count=1` | 4package成功 |
| vet | `go vet -tags=e2e ./...` | 成功 |
| Go整形 | 変更・追加した27 Goファイルへの `gofmt -l` | 出力なし |
| 保存JS | 変更・追加したhooks/task/tool_pipelineとCLI fixture 2件へのBiome lint | 成功 |
| 公開宣言 | `tsc --noEmit --project agent/types/testdata/tsconfig.json` | 成功 |
| CLI JS fixture型 | `tsc --noEmit --project /var/tmp/noe-hook-isolation-s3-jsconfig.json` | 成功 |
| 差分 | `git diff --check` | 成功 |

総合ログ・機械可読集計：`/var/tmp/noe-hook-isolation-final-1792941850015/report.json`。同directoryの `unit.log`、`e2e.log`、`race.log`、`vet.log`、`format.log`、`lint.log`、`types.log`、`fixtureTypes.log`、`diff.log` に全出力を保持した。

### 成立した利用経路

- 実executorの4phase fixtureは、2回の通常モデル要求でtaskを正常完了し、9件のhook失敗のうち8件を対応する要求へ通知、最終応答後の1件は未送付として保持する。hook失敗だけの追加モデル要求はない。
- ファイル/コマンド処理失敗、不正な返却値でも後続hook・元tool・並列toolが継続し、tool作用は一回。元結果・先行置換・画像選択を保持する。
- 通知込みの概算、steer-only再準備、trim後の会話保存が成立し、pendingの誤消費やhookの再実行はない。
- 公開Go Clientのwait/observe/回答省略投影でも失敗記録を取得でき、利用者による返却slice変更は保持元へ漏れない。原文範囲取得は回答だけを返し、queue後も旧結果は不変。
- JSONLなしのCLI run/eval--observeでも1回のモデル要求で完了し、通知済みbefore失敗と未送付after失敗を保持する。hook failed表示と正常完了が両立する。
- 取消し・既存上限・scope/Host障害は通常のhook失敗へ変換せず、既存の終了理由と所有物回収を維持する。

### 完了範囲と残件

今回の未解決不具合・P2以上のレビュー指摘はない。登録/読込みの準備エラーは現行維持。トリム外部化、承認/skip、汎用公開通知API、リリース/配布確認は実装範囲外。
有料実provider接続・性能負荷試験・6対象配布ビルドは今回行っていない。通常のローカルfixtureと実runtime/CLI/Go Clientで必要な動作を確認した。
生成JSONL・Pythonキャッシュはcommitに含めず、起動時からのものを保持する。
