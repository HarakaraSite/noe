# Agent起動と待受の実装・検証記録

[計画正本](agent-lifecycle-plan.md) · [後続の操作計画](agent-control-plan.md)

2026-10-17。ユーザーの依頼に基づき、起動と待受の計画の実装・検証・コードレビューを完了。steer・公開interrupt・人間用の操作CLIは別スコープとし、[後続計画の実装記録](agent-control-implementation.md)で扱う。commit・push・releaseは行っていない。

## スライスの状態

| Slice | 状態 | 確認 |
| --- | --- | --- |
| S1 内部loopとrunner分離 | 完了 | agent/runtime既存テスト、限定コードレビューで必須指摘なし |
| S2 Go manager | 完了 | 実NOE + fixtureによる所有・FIFO・回収・異常終了、race、レビュー指摘2件を修正・再確認 |
| S3 公開APIと最小観察 | 完了 | run/spawn/queue/live観察、native追跡、CLIと表示のテスト成功。限定レビューで必須指摘なし |
| S4 利用例と総合確認 | 完了 | 旧テスト・instructions・ガイドを移行。全体レビューの指摘を修正・再確認し、総合テスト・vet・race成功 |

## S1

- モデル/tool loopを非公開実装用の `executeTask` へ分離し、管理タスクの固定bootstrapから直接呼ぶ。
- `Runner.RunTask` は構造化configとinstructionをJSONとして渡し、run_js用preludeを前置しない。
- assistant message全体、reasoning拡張、tool結果順序、maxSteps、cache IDを維持する。finish RPCの失敗は握り潰さない。
- 実装担当の `go test ./internal/agent -count=1` と `go test ./internal/runtime -run '^TestAgent' -count=1` は抽出直後に成功。
- 独立レビューは限定Runnerテストと静的確認を実施。必須指摘なし。公開旧APIの暫定wrapperはS3で廃止済み。

## S2

- Go managerがinstanceと各タスクのfresh NOE processを所有し、結果を認証付き報告とprocess回収の両方で判定する。
- 通常JSでは評価Operation、親run_js経由では起動元の親runへinstanceを所属させる。短命な起動元の終了と、所有親の終了を区別する。
- 実NOE + fixtureで、idle中のモデル未呼出し、FIFO、通常失敗後の継続、新履歴・独立cache、spawnの即時受領、別scopeの拒否、親終了時の回収を確認。
- 完了報告なしのexit 0を失敗とし、認証付き回答の後にprocessが非zero終了した場合も、回答を保持した実行失敗へ収束させる。

### レビュー指摘と修正

- **採用 / P2：fatal終了中のqueue受領。** 最終queue掃出し後もstateがrunningのままでは新規予約を受領して取り残す。queue切離しと同じlock内でstoppingへ遷移させる。
- **採用 / P2：取消し通知が受領通知を追い越す。** queue登録と受領イベントの間に所有者取消しが入ると、cancelledがacceptedより先に記録される。実行・取消し双方で、lock外から受領通知の公開完了を待つ。
- 上記2件は制御gateを使うテストで確認し、レビュアーが修正とテストの通過を確認。新たな必須指摘なし。

## S3・S4

- 起動APIにwaitを追加せず、runはinstanceの終端・回収まで待ち、spawnは登録後にagent IDを返す。
- spawn直後のqueueを可能にするため、登録済みstartingでも受領することを計画へ明記した。
- 単発実行の成功・失敗はsnapshotで返し、入力・受付不備はrejectする。
- rootのspawnはnative仕事として追跡し、remote run_jsは開始要求だけを追跡する。
- 終端結果は所有評価の間に観察でき、評価終了時に管理表から解放する。
- 人間向け表示はinstanceの寿命とタスクのrunを区別し、ファイル観察のsnapshotにもagent IDを付ける。

runtimeのagentテストとruntime全体、agent関連E2Eはテスト担当の実行で成功。旧generated tool終了時の子回収テストを、新契約の「tool終了後もobserve・queueでき、親run終了で回収」へ更新した。rootのspawn後に本文末尾へ到達してもtask完了まで生存し、idleのkeepAliveがモデルを呼ばずtimeoutで回収されるE2Eも追加した。

### 全体レビューの採否

- **採用 / 文書契約：認証のlazy初期化と失敗の返却。** import・初期taskなしのidle開始はproviderへ接続しない。task付きrun/spawnは受領後にモデル要求が進む。REPLのprovider失敗は入力例外ではなくfailed snapshotとなる説明へ修正。
- **採用 / 文書契約：rootの待受寿命。** root本文が終わってもkeepAliveは継続し、REPLも次promptへ進まない。現在の終了方法はCtrl+C・timeout等。remote run_jsの短命な本文とは区別して説明。
- **採用 / P2：root spawn E2Eの完了確認。** CLIのexit 0とstdoutだけでなく、対象agent IDのJSONLで `agent.completed` が一度、failed/cancelledがゼロであることを確認する。
- **採用 / P2：補完テストの旧実行順。** managed taskはQuickJSのPromise継続と独立して動くため、補完の取消し前に最初のprovider要求が到着する可能性がある。runtime/PTYとも要求数ゼロを条件にせず、到着した要求の取消し・回収を確認する。
- 独立レビュアーは修正後の文書と上記テスト2件を再確認し、未解消の必須指摘なし。総合テストの重複実行は依頼せず、rootが統合検証を担当。

CLI単体テストは接続付きのchild再実行だけをTestMainからCLIへdispatchする。runtimeテストは実NOEを一度ビルドしてtask実行へ渡し、従来のstdlib command helperはTestMainで先に処理してtest suiteの再実行や不要なビルドを避ける。

## 総合検証

2026-10-17、rootが最終ソースに対して実行。ローカルfixtureのみで、実プロバイダへ接続していない。

- `go test -tags=e2e ./... -count=1 -timeout=180s`：成功。
- `go vet -tags=e2e ./...`：成功。
- `go test -race ./internal/... -count=1 -timeout=180s`：成功。
- `go test -race -tags=e2e ./tests -run '^TestAgent' -count=1 -timeout=180s`：成功。
- Linux / Darwin / Windows各amd64・arm64、計6targetの `CGO_ENABLED=0 go build`：成功。成果物は `/home/agent/.cache/noe-qjs-repl/noe-lifecycle-builds-31j1jH`。Darwin/Windowsのnative実行は未確認。
- `CGO_ENABLED=0 go build -o noe ./cmd/noe`：成功。作業ディレクトリのバイナリを更新済み。
- 更新バイナリのmodule import smoke：公開関数が `observe_agent, queue_agent, run_agent, spawn_agent` であることを確認。

総合テストの初回で、観察E2Eに旧「タスク失敗でCLIもexit 1」の期待が残っていた。新契約どおり、JSがfailed snapshotを処理して正常終了し、同じrun IDのログから失敗情報を取得できることを確認する形へ修正した。修正後の全テストが成功。

全テストは `TMPDIR` と `GOTMPDIR` に `/home/agent/.cache/noe-qjs-repl` を指定した。新規sourceの空白、`git diff --check`、更新文書12件のローカルリンクとコードフェンスも確認済み。各slice・全体reviewの未解消必須指摘はない。Goコード・テスト・バイナリの実行確認はLinux amd64で行った。
