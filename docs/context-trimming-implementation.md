# 実行コンテキスト管理の実装・検証

2026-10-18。[承認済み計画](context-trimming-plan.md)に沿って実装・総合確認を実施。commit・release・既存`./noe`置換は行っていない。

## スライスの進行

| Slice | 実装・確認 | review |
| --- | --- | --- |
| S1 policy・予算 | Go/JS検証、null/省略/継承、window・出力予約・100万fallbackを実装。focused test成功 | 必須・任意指摘なし |
| S2 履歴 | 保護entry・完了block・provider object保持、prefix削除と最新block保護。focused test成功 | 必須指摘なし。拒否テストの自己catchを修正 |
| S3 要求前接続 | hook後の最終準備、digest/revision検証、削減記録、steer-only通知保持を実装。focused test成功 | 取消しと適用記録の競合を候補commit方式で修正。再レビューで解消確認済み |
| S4 通知・観察 | 英語context/trim行、JSONL/live/saved snapshot・モデル案内・資料を更新。CLI/observation test成功 | tool履歴の保護範囲の説明を修正。予算式も追記 |
| S5 総合回帰 | unit/E2E/vet/race・6target build成功。親の削減後に子・孫を起動する実process検証も成功 | 不足した子・孫の検証を補完し、再レビューで解消確認済み |
| S6 実モデル | 最終版で制御3条件と自然taskを実施。型契約を明記した20%保持の再確認も成功 | harness・結果と限界を確認。初回失敗も保存 |

## 実装上の補足

- 切り捨ては要求用履歴のcommitとして扱う。JSで候補を作っても元履歴は変更せず、ホストが検証・確定・記録した後にJSが置換する。確認前のinterruptで未記録の削除を起こさない。確定後は次のモデル要求がなくても記録は残る。
- ホストのplanはID・revision・digest・集計のみを保持し、削除済み本文を裏で蓄積しない。
- OpenRouterのDeepSeek V4.1 Flashだけ、[公式モデルページ](https://openrouter.ai/deepseek/deepseek-v4.1-flash)で確認したwindow 1,048,576を組込みに追加。[OpenCode Go資料](https://opencode.ai/v2/docs/console/go/)にはwindowの記載がなく、別providerの値は転用していない。確認できない値を埋めない。

## 現時点の検証

focused testsで、繰り返し削減後のtask完了、並列tool順序、reasoning/署名/native field保持、steer保護、hook一回、policy無効、receipt再送拒否、steer-onlyでの通知・cause保持、decision入力の非加工を確認。

最初の実モデル試行は`opencode-go` / `deepseek-v4.1-flash`、effort auto、retainRatio 0.1。制御hookでtool結果へ24,000文字を追加し、4入力を逐次加工。18秒・7要求・5削減で合計70のreport.jsonを生成・検証した。候補commitの順序修正前の予備結果であり、最終版の合格結果と区別する。
予備artifact：`/tmp/noe-context-live-20261018-x6qngL/controlled_retain_01`。


## 総合確認

- `go test ./...`、`go test -tags=e2e ./...`、`go vet -tags=e2e ./...`：成功。
- `go test -race ./internal/agent ./internal/runtime -run 'TestContext|TestResolveExecution|TestHooksContext|TestHooksLogical|TestHookWork|TestDecisionAgent|TestPrivateTask' -count=1`：成功。
- `go test -race -tags=e2e ./tests -run '^TestContextTrimming' -count=1`：成功。
- Linux/macOS/Windowsのamd64・arm64、CGOなし6targetのbuild：成功。Linux amd64は生成バイナリで実行確認し、他5targetはcross-build確認。
- run/eval/repl、次queueの履歴・trim回数リセット、human/live/savedの一致を実CLIで確認。
- 20個の完了blockを用いるローカル検証ではretainRatio 0.1と0.2が異なる直近suffixを保持することを確認。
- 通知に記載した総入力概算が、実際のprovider境界へ渡すmessages/toolsの同じ概算式と一致することを確認。

build出力：`/tmp/noe-context-final-20261018-ZGkIrT/builds/`。試す場合は同directoryの`noe-linux-amd64`を利用できる。既存repository直下の`./noe`はv1.5.0のまま。

## 実モデルの挙動

対象は`opencode-go` / `deepseek-v4.1-flash`、effort autoのみ。各taskの明示入力予算11,000、maxTokens 8,192、providerCalls/maxSteps 12、CLI timeout 175秒・harness timeout 180秒。
制御ケースではafterTool hookでtool結果へ24,000文字を付加し、同じ4入力の逐次加工と独立した検証を行った。通常suiteでは実接続せず、`e2e context_live`と明示opt-inの両方で有効化する。

| 試行 | 時間 | provider要求 / tool実行 | 削減 | 入力実測 / cached | 結果 |
| --- | ---: | ---: | ---: | ---: | --- |
| 制御・10%保持 | 17秒 | 7 / 6 | 5回 | 45,600 / 19,200 | 数値型を含め正しいreport、合計70 |
| 制御・20%保持・初回 | 20秒 | 7 / 6 | 5回 | 46,025 / 18,304 | 合計70で完走したがID 3/4が文字列、成果物検証は失敗 |
| 制御・無効 | 35秒 | 7 / 6 | 0回 | 116,392 / 89,856 | 正しいreport、合計70 |
| 自然task・paddingなし | 16秒 | 8 / 7 | 0回 | 37,065 / 29,824 | 正しいreport、合計70 |
| 制御・20%保持・型を明記して再確認 | 28秒 | 7 / 6 | 5回 | 48,118 / 23,808 | 数値型を含め正しいreport、合計70 |

詳細値・削減前後・成功/失敗の区別は[結果JSON](context-trimming-live-results.json)。初回20%の誤りは製品コードの修正で隠さず、元の結果を残した。初回taskでは`id`の型を明示していなかったため、再確認では入力元の数値型を保持する契約を明記した。harnessは再試行時に既存case directoryを拒否し、前回reportを引き継がない。

観察したこと：

- 削減後も元taskと直近tool結果を使い、必要時に成果物のreport.jsonを読んで継続・検証できた。tool call/resultの欠落はproxyで検出されず、User-Agentと同runのsession IDも維持した。
- 任意の記憶用ファイルはどの試行でも作らなかった。report.jsonは課題の成果物であり、コンテキスト退避を義務付けたものではない。保存専用のモデルturnは追加していない。
- 自然taskでは最初の入力に対するtool実行が2回あり、制御taskより1回多かった。taskは成功したが、モデルの作業順序や再読込みが一定になるとは主張しない。
- 制御ケースは巨大な最新blockを保護する例外に当たり、約12,600～14,600から約6,200～7,800tokensへ削減した。`targetUnreachable`を表示し、10%/20%まで減らすためにtool結果を分断していない。この条件では両ratioが最新1blockを残すので、比率の品質差は評価できない。
- 入力・cachedはprovider実測で、cachedを入力から引かない。少数の試行・異なるthinking長から一般的な性能改善率や最適ratioは推定しない。金額はprovider未報告のため算出していない。

最終版artifact：`/tmp/noe-context-final-20261018-ZGkIrT/`。
20%再確認：`/tmp/noe-context-retry-20261018-hRjLJ3/`。
JSONL、実行JS、hook、入力・成果物、人間向け観察、認証headerを含まないwire metadataを保持した。


## 全体レビューの結論

独立reviewerがスライスごとのコード・テスト・資料と、最後に実CLI／実provider harness・検証記録の整合を確認した。必須指摘は、S3の取消し時の記録競合、S4のtool履歴の保護範囲の説明、全体確認時の実際の子・孫process経由の検証不足。すべて修正し、各変更を対象とした再レビューで解消を確認した。残る必須指摘はない。

追加した親子・孫fixtureでは、親が削減してから子を起動し、子の削減後に孫を起動する。保持比率0.2の継承、null継承と明示override、モデルごとのwindow・応答予約・実効予算、初回trimCountゼロと独立履歴を確認した。focused test・race・3回の反復は成功。既存の共有execution counterをrun固有のcontext counterと混同しない。

実provider 5試行のcompact結果も、reviewerがJSONL・wire metadata・report.jsonと照合した。モデル成果物の型誤りと再試行を含め、失敗を合格へ読み替えていない。保存の有無や目標削減率の達成自体はtask合否にしていない。
