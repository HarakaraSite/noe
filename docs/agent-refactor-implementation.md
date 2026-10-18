# Agent内部の責務整理・実装記録

2026-10-18。[計画](agent-refactor-plan.md)に沿って実装する。既存の64KiB出力制限を含む未コミット作業を維持し、ユーザーの実行ログは対象外とする。

## S1: 基本オブジェクトの配置

- `agent.go`の28宣言を、`config.go`、`connection.go`、`host.go`、`operation.go`、`events.go`、`id.go`、`errors.go`へ移動した。
- 関数・型の本体、JSON、共有mutex、close処理とイベントの順序は変更しない。`agent.go`はpackage説明を担当する。
- `go test ./internal/agent ./internal/cli ./internal/runtime -count=1`: PASS。
- 独立レビュー: 必須findingなし。移動前後の全28宣言・コメント・importの集合を独立したtoken比較で照合し、本体・シンボル・処理順の一致を確認。

## S2: ライフサイクルとHTTP受付

- `lifecycle.go`と`control_interrupt.go`を廃止し、HTTP受付を`broker_lifecycle.go`・`broker_interrupt.go`・`broker_steer.go`へ配置した。
- 型と設定は`lifecycle_types.go`・`task_config.go`、instanceの状態/loopは`instance.go`、一つのtaskのプロセス起動は`task.go`、所有判定は`ownership.go`へ分けた。
- Hostのspawn・observe/wait・queue・steer・interruptは型付きの入力/結果を扱い、HTTPのRequest/ResponseWriterへ依存しない。`operationError`が元のstatus/code/messageを保持し、broker側で応答へ変換する。queue/steerの共通受付結果は`commandReceipt`。
- direct-callのinterrupt試験を型付き操作に合わせた。HTTP経路の既存試験は維持した。
- `go test ./internal/agent/... -count=1`: PASS。
- `go test ./internal/runtime -run 'TestQueue|TestPublicRunInterrupt|TestCommittedAnswer|TestInstanceInterrupt|TestControl|TestSteer|TestLifecycle' -count=1`: PASS。
- 実CLIのrunner/output/control/queue focused E2E: PASS。
- 独立レビュー: 必須findingなし。旧45宣言中38宣言がtoken一致。HTTPと型付き操作へ分離した6操作も入出力変換を除く本体、ロック・イベント・ACK・取消し順の維持を確認。共通結果型のrenameとエラー変換関数の移動は一回の限定再レビューで確認した。
- S3の並行移動中に一度`capturedOutput`未定義のcompileエラーが出た。S3完了後に上記試験を実行し、解消を確認した。実行時の不具合としては扱わない。

## S3: 子プロセス実行

- `internal/agent/runner`に明示Command入力によるOS実行・監督・Wait・出力収集を抽出。既存`processscope`を直接使い、親packageのadapterを削除した。
- 親`agent.Runner`は従来のbootstrap、source/import-map作成、Connection検証、環境変数除外を維持し、OS層へ絶対Executable/Cwd・argv・envを渡す。`agent.Result`は下位型のaliasで入口を維持する。
- 出力収集とその試験はpackage移動のみ。新しい下位入口のnil/明示envが親環境を暗黙継承しないことをPOSIX helperで確認した。
- 新runner単独unit、親agent suite、実NOEの大量出力・失敗・取消し・既存Runner E2E: PASS。
- 独立レビュー: 必須findingなし。cleanup順と親のtemp削除、ctx・exit・エラー結合、出力契約と依存方向を確認。任意指摘に従い、親PATHを一切参照しないという過大なコメントを修正し、絶対Executable/Cwdを呼出し元が渡す契約を明記した。

## S4: 総合確認

構成ガイドと開発記録からの導線を追加した。機能ごとの変更先、JS/Goの分担、Host.mu・request取消し・所有関係・受付イベント順の規則を記載した。

Linux amd64、Go 1.27.2で、最終のGo/JS sourceに対して以下を実行した。

| 確認 | 結果 |
| --- | --- |
| `CGO_ENABLED=0 go test ./... -count=1` | PASS |
| `CGO_ENABLED=0 go test -tags=e2e ./tests -count=1` | PASS。実CLI・Linux PTY・ローカルMCP SDK bundleを含む |
| `CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/... -count=1` | PASS |
| `go vet -tags=e2e ./...` | PASS |
| `git diff --check` | PASS |
| CGOなし、`-trimpath -ldflags='-s -w'`によるLinux/Darwin/Windows × amd64/arm64 build | 6targetすべてPASS |
| 計画・実装記録・構成ガイド・docs索引の相対リンク | 77参照すべて解決 |
| Linux配布設定buildのnative smoke | versionと6つの公開agent関数のlazy importを確認、exit 0・stderrなし |

E2Eは`NOE_MCP_SDK_BUNDLE=/tmp/noe-process-mcp.wzVT64/mcp-client.bundle.mjs`を使用した。instance/taskの起動・queue・steer・interrupt、会話継続とtrim、画像所有、hook、保存/可読観察、run_js出力制限・取消しなどの既存試験を維持して確認した。実プロバイダへの追加通信はしていない。

総合レビューは必須findingなし。公開JSとRPC/controlの入口、typed操作、task/run_jsから親Runner・下位runner・processscopeへの接続、結果/取消しの受渡し、下位の依存方向、ドキュメントの配置との一致を確認した。各sliceのレビューで出た任意の命名/コメント整理も反映済みで、未解消の必須指摘はない。

試験ログ・checks.json・6targetのbuild成果物は`/tmp/noe-agent-refactor-yoe5airq/validation/`。Linux版のnative smoke後、repository直下の`./noe`を今回のbuildへ更新した（49,180,832 bytes）。versionはv1.7.0のままで、新しいreleaseではない。Linux以外はcross-buildの確認で、native実行は未確認。

既存のJS/Go入口、RPC JSON/status/code/message、Host.muの共有状態境界を維持した。今回すべてのprivate RPCをservice化したわけではなく、対象は外からinstance/taskを操作するspawn・observe/wait・queue・steer・interrupt。モデル要求・履歴確定・画像・hookのhandlerは機能別の現行構成に残す。整理完了時点では未コミット。後続の利用者依頼でv1.7.1としてcommit・push・releaseする。公開の確認は[release checkpoints](release-checkpoints.md)へ記録する。
