# 責務境界と共有状態の整理・実装記録

[承認済み計画](responsibility-boundaries-refactor-plan.md)。計画は通常レビュー・批判的レビューともP2以上0件を確認し、`7f25a49` として実装前にコミットした。

## 状態

| slice | 内容 | 状態 |
| --- | --- | --- |
| S1 | command/task状態遷移 | 実装中 |
| S2 | Profile設定・loader・実行寿命 | 関連試験・独立レビュー完了、P2以上0 |
| S3 | Worker設定継承・VM起動停止 | 未着手（S2後） |
| S4 | messaging codec/broadcast/Worker通知境界 | 未着手（S3後） |
| S5 | JS run_js adapter・tool pipeline | 関連試験・独立レビュー完了、P2以上0 |

検証ログの親ディレクトリ：`/var/tmp/noe-boundaries-KVDans`。各担当の詳細ログは該当sliceへ追記する。実行ログ・既存の未追跡ファイル・配布済みバイナリは変更/コミットしない。

## 中断からの回復

ユーザーが応答のないテストを停止した後、残留したgo/testプロセスがないことと、未コミット差分が保持されていることを確認した。以前の子agentは所有scopeの終了で回収済みで再利用できなかったため、担当を再割当てした。計画コミットは維持し、実装をやり直さず既存の差分と保存ログから継続した。以後のテストにはGoの `-timeout` と外側の `timeout --kill-after` を付け、出力を実行中からログへ保存する。

## S2: Profile設定・ローダー・実行寿命

- 設定型・snapshot/normalizeを `profile_options.go`、下位Config変換を `profile_config.go` に分離した。
- loader factory、defined modules、prepared/load状態と解決処理を `executionLoader` が所有し、`Bindings` は委譲する。`bindingsState` は既存のmutex/closedの唯一の寿命gateで、loaderとBindingsが共有する。二重のclosed状態を作らない。
- `profile.go` はattach、Worker installerとの接続、借用資源を含む実行寿命を統括する。公開 `stdlib/profile.go` のaliasとsignatureは変更しない。
- 環境設定・下位変換・Close/失敗順序・エラー文言の同一性を旧sourceと比較。証拠：`/var/tmp/noe-s2-profile-refactor/contract-checks.log`。
- `go test ./internal/jsexec ./stdlib -count=1`：成功。保存済みログ `/var/tmp/noe-s2-profile-refactor/related-tests.log` を回復後に確認した。ソースが変わっていないため同じ全関連suiteを形式的に再実行しない。
- 追加した `TestBindingsLoaderSharesCloseState` は、抽出したloaderがBindingsのCloseを共有し、Prepare後のmodule追加、二度目のPrepare拒否、Close後の登録/解決を既存契約どおり扱うことを確認する。
- 回復後に独立code reviewerが追跡差分・新規ファイル・呼出し元・成功ログを照合し、P2以上0件。必須修正なし。

## S5: task loopとtool処理の境界

- `js/run_js.js` がモデル向けrun_js schema、引数/実行結果/diagnostics/画像metadataの検証とnative実行を担当する。
- `js/tool_pipeline.js` がbefore/after hook、raw/effective・実行事実/提示結果、画像、tool_result報告を担当する。
- `js/task_protocol.js` はtaskとtoolに共通のJSON request境界とrecord判定だけを持つ。task→pipeline→adapter/protocolの一方向依存とし、toolからtask loopへ依存しない。
- `js/task.js` は設定・履歴/context・steer・モデル反復・最終確定の調停に残す。既存のprivate `executeToolWithHooks` exportは、import済みbindingのlocal exportで同じ関数identityを維持する。
- Go embedとagentbridgeの登録を同時に更新する。新しい公開agent APIは増やさない。
- 実executorにprivate moduleのimport、compatibility export、invalid JSONが実行前のsetup診断になることを確認するケースを追加。これは分割後のmodule接続とadapterへの委譲が実際に解決されるかを確認するためで、既存のモデル反復・hook/画像/順序試験は再利用する。
- 変更JSのBiome lint：成功。
- 初回実executorで、固定go-quickjs v0.28.0のnamed re-exportがcolon付きmodule specifierを誤分解し `cannot resolve "noe"` となることを検出した。上流の `indirectSource` は隠しbinding名を最初のcolonで分割する。依存やengineは変更せず、既存import bindingをlocal exportする等価な形へ修正した。
- 修正後 `CGO_ENABLED=0 go test ./internal/agentbridge ./internal/agentexec -count=1 -timeout=60s`：成功。compatibility exportのidentityと実task→tool→次モデル要求のdata/diagnosticsも確認。
- 修正後 `CGO_ENABLED=0 go test ./internal/runtime -run 'Test(Hook|ToolOutput|Image|ContextTrimming|Queue|AgentRootRequires|Decision)' -count=1 -timeout=120s`：成功。hooksのraw/effective/失敗と並列順、画像、context/trim、queue継続とdecisionを現行fixtureで確認。
- ログ：`/var/tmp/noe-boundaries-KVDans/s5-executor-local-export.log`、`s5-runtime-local-export.log`。初回失敗ログも同ディレクトリに保持し、成功扱いに混ぜない。
- 独立code reviewはP2以上0件。上記export修正後、同じreviewerが一行と登録接続、成功ログを再確認し、P2以上0・必須修正なしを確認した。

## 総合検証

未実施。全sliceのレビュー修正が落ち着いた候補に対して計画の総合メニューを実行する。
