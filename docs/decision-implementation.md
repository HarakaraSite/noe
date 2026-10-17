# 意思決定APIの実装・検証記録

2026-10-17。[計画](decision-plan.md)に沿った実装・テスト・コードレビュー・実接続確認を完了。既存の作業中変更を保持し、commit・push・releaseは行っていません。[利用ガイド](decision.md)・[実接続記録](decision-openrouter-validation.md)。

## スライスの進捗

| Slice | 状態 | 検証・レビュー |
| --- | --- | --- |
| S1 provider | 完了 | `go test ./internal/decision ./internal/agent/provider`成功。独立レビューでP1/P2なし、レビュアー実行のraceも成功 |
| S2 JS入口 | 完了 | rootと親broker経由、JSON検証、並列、REPL再利用、未await要求の追跡・取消しを確認。独立レビューでP1/P2なし、関連race成功 |
| S3 agent内利用と観察 | 完了 | 親model保持・scope/tool対応・差分取得・可読表示を確認。レビューP2を修正し、対象を絞った再レビューで解消確認 |
| S4 文書・総合確認 | 完了 | 通常全体unit、全体E2E、vet、変更packageのrace成功。全体レビューと指摘箇所の再レビュー完了 |
| S5 実接続 | 完了 | 指定3モデル各3ケースと親agent経路、計10ケース成功。Decisions 10要求＋Chat 2要求、報告費用$0.001191550 |

## 設計・レビューの記録

- 計画レビューP2：意思決定modelが親snapshotのModelを上書きする経路。`decisionModel`・`responseModel`を使い、親modelと`run_js`段階を維持する契約を計画へ反映済み。
- S1：固定SDKの`Alpha.Decisions.Create`を利用。要求ごとのHTTP境界でraw JSONを保持し、ChatのSSEとは別の状態で処理する。`DecisionServerURL`はGo側のfixture/gateway設定で、公開JS optionではない。
- S1テスト調整：取消し時のbody回収は、サーバーのheader送信だけでなくクライアントのbody読取り開始をgateにして検証する。header段階の取消しではbodyを受け取らない場合があり、close数の前提が異なる。
- S2テスト調整：REPLは既存の対応構文に合わせて`const dm = await import(...)`、`await decide(...).then(...)`を使用する。REPLの構文対応範囲の拡張は行わない。
- S3レビューP2：親終了時にrun mapが削除された後でmetadataを引くと、要求イベントの親run/toolが欠落する。子scope登録時のimmutableな`decisionOwner`を使う形へ修正し、親run削除後にも帰属を保つ回帰テストと再レビューで解消確認。
- S4：CLIの`NOE_OPENROUTER_BASE_URL`指定時もDecisionsをfixture/gatewayへ向けられるよう、専用Go設定がなければChat用URL末尾の`/api/v1`を除いてoperation serverとする。実CLI E2Eで要求pathと認証を確認。
- S4の既存E2E調整：前段で追加済みの実行JS全文JSONL記録に合わせ、旧「生成sourceを含まない」assertをsource存在確認へ更新。認証値が出ない確認は維持。
- 全体レビューP2：実接続ハーネスが任意の`choice.probabilities`を必須にしていた。提供時だけキーと値の範囲を検証し、欠落はNotesへ残す形に修正。対象を絞った再レビューで解消を確認し、その後に実接続を開始。production実装への追加P1/P2はなし。

## 総合検証

| コマンド・確認 | 結果 |
| --- | --- |
| `go test ./...` | 成功 |
| `go test -tags=e2e ./...` | 成功。CLI・PTYを含む既存E2Eと新規意思決定E2E |
| `go vet -tags=e2e ./...` | 成功 |
| `CGO_ENABLED=1 go test -race ./internal/agent ./internal/agent/provider ./internal/decision ./internal/runtime ./internal/cli ./internal/observation` | 成功 |
| `go test -tags='e2e decision_live' ./tests -run '^$'` と同tagのvet | 成功。実要求を送らずlive harnessを確認 |
| `go test -tags='e2e decision_live' ./tests -run '^TestDecisionOpenRouterLive$' -count=1 -v` | 最後に実行し10ケース成功。結果は実接続記録とJSONへ保存 |
| `CGO_ENABLED=0 go build -o noe ./cmd/noe` | 成功。ローカル`./noe`へ反映済み |
| `git diff --check` | 成功 |

Linux amd64 / Go 1.27.2で実行。他OSでの実行確認やrelease作業は今回行っていません。

ローカル作業のbaselineと成果物ディレクトリ：`/home/agent/.cache/noe-qjs-repl/noe-decision-1bv69irw`。
