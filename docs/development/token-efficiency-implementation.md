# トークン効率改善：実装・検証記録 — 2026-10-24

[計画とレビュー結果](token-efficiency-plan.md) · [測定結果](token-efficiency-results.json) · [開発ガイド](README.md)

ローカル実装・通常レビュー・批判的レビュー・検証を完了した。比較開始時のcommitは`46ae6f5`。公開バージョン、常用バイナリ、既存イベントログは変更していない。

## 実装

- docsのmember取得はtopic名・短いsummary・型宣言/Availabilityと選択したdetailだけを返す。topic概要とmember一覧は維持し、概要だけにあった利用条件を該当detailへ移した。
- run_jsのafterTool後のJSONに既定3 KiBの本文予算を適用する。予算内は形を維持し、超過時は不完全な`outputPreview`、省略量、元の実行状態を返す。診断内のコマンド出力と任意hook結果も同じ予算に入る。画像は既存の専用経路を使う。
- `// @noe: {"outputBytes": 12288}`を先頭の行コメント群に置くと、その呼出しだけ増量できる。1行目の操作説明を維持でき、不正指定は副作用の前にsetup診断になる。
- 元結果はrunner、afterTool、`run_js.finished`、`tool.finished.rawResult`に保持する。`tool.finished.result`と次要求の履歴には同じprojectionを使う。hookが変更しpreviewで失う値は`hookResult`へ保存する。ソースの再実行やartifact自動保存は行わない。
- 組込みopenai-chatgptのrun_js入力をResponsesのtext custom toolにした。内部は従来の`{code}`へ正規化し、原文は`rawArguments`に保持する。native itemとcall IDに応じてcustom/functionの結果を返す。他providerの既定はJSON。設定`runJSInput: "json"`で明示変更できる。

3 KiBはpreviewと診断summaryの本文予算であり、固定metadataやJSONエスケープを含む送信全体の上限ではない。今回の広い検索では、元JSON 66,133 bytesが本文3,072 bytes、metadataを含むモデル向けJSON 3,664 bytesになった。stdout/stderrの既存64 KiB捕捉上限は独立した契約で、元ログも捕捉上限より前の全文ではない。

## 測定と実サービス

同じglobalsの7詳細（ReadableStream、ReadableStream.getReader、Response、AbortController、AbortSignal、setTimeout、clearTimeout）を取得した。公開済みv2.3.0の実測38,368文字に対し、変更後は8,345文字。30,023文字、78.25%削減した。これは本文量の比較であり、token削減率ではない。

openai-chatgpt / gpt-6.1-sol / lowで、raw JS入力から最終回答までを確認した。

| 課題 | model要求 / tool呼出し | prompt / completion / cached tokens | 結果 |
| --- | --- | --- | --- |
| 広い検索・初回のAPI誤用から回復 | 4 / 3 | 12,583 / 221 / 6,400 | callback型execFileの誤用で失敗。長い例外出力も制限され、API参照後に絞込み検索で正答 |
| 正しいmoduleを指定した広い検索 | 3 / 2 | 8,271 / 121 / 1,664 | 全件検索をpreview化した後、モデルが自らTARGET行に絞って正答 |
| HTTP serveの完全なdetail取得 | 2 / 1 | 5,270 / 305 / 1,792 | 最初から24,000 bytesのpragmaを選び、5,922 bytesの資料全文と終了動作を取得 |

最初の課題の失敗を隠さず記録し、正しい呼出しを指定した課題は別ケースとして扱った。課題全体のtoken効率や往復数の改善を比較する対照実験ではない。今後の常用では不要な増量・再検索の頻度も見る。

probeの正本は`/var/tmp/noe-token-efficiency-gmmpzzx5/`。各ケースにsource、result、JSONLを保存した。実接続binaryは最終の観察用slice分離と一部docs条件補足より前のもの。影響する経路はローカルテストで確認し、同じ外部推論を繰り返していない。最終checkoutの一時binaryは同ディレクトリの`noe-after`。

## レビューと検証

計画と実装をそれぞれ通常・批判的レビューした。Blocker/P1なし。計画で挙がった操作説明とpragmaの両立、診断を含む共通予算、モデルへの事前案内を反映した。

実装レビューのP2は2件を採用・解消した。Observe callbackによるイベント加工がRPC返却値へ波及しないようバイト列を分離し、回帰テストで確認した。socket detailには必須の`network: "tcp" | "unix"`とoption形態を保持した。未解消のBlocker/P1/P2はない。

- `go test ./...`：通過。provider、UTF-8/境界/診断/hook/pragma、raw JSからhook・projection・次要求までのruntime統合も含む。
- `go vet -tags=e2e ./...`：通過。
- 対象E2E：出力超過、hook・元記録、取消し、docs、context観察は通過。context切捨てのrun/eval/queueも通過。
- `go test ./internal/apidocs -count=1 -v`：最終の条件補足と削減量を確認。
- `git diff --check`：通過。

既存のcontext切捨てfixtureは、縮小した結果では閾値へ到達しなくなったため、合成した大出力のcallに予算を明示した。切捨て本体の挙動や閾値を今回の変更に合わせて弱めていない。
