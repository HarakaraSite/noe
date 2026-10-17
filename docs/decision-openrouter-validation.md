# 意思決定APIのOpenRouter実接続確認

2026-10-17、Linux amd64 / Go 1.27.2 / OpenRouter Go SDK v0.9.59で確認。利用者の許可した3モデルを使用し、`typesafe/jev-router`は含めていません。[計画](decision-plan.md)・[実装記録](decision-implementation.md)・[機械可読結果](decision-openrouter-results.json)。

## 結果

3モデル×短文・構造化JSON・画像の9ケースと、親agentからの1ケースがすべて成功しました。再試行は不要でした。

| モデル | 短文 | 構造化JSON | 画像 | 今回返ったprovider |
| --- | --- | --- | --- | --- |
| `typesafe/jev-1.14` | 成功 / 588ms | 成功 / 408ms | 成功 / 417ms | TypeSafe |
| `perplexity/pplx-decider-v1.2` | 成功 / 650ms | 成功 / 588ms | 成功 / 666ms | Perplexity |
| `cloudflare/clef` | 成功 / 582ms | 成功 / 735ms | 成功 / 802ms | 短文・JSONはPrimeIntellect、画像はCloudflare |

時間はCLIプロセス起動から終了までで、APIだけのlatencyではありません。JevとPerplexityの応答modelにはそれぞれ`-20261014`・`-20261016`のrevisionが付きました。NOEは要求model・応答modelを分けて記録しています。

- 短文：checkoutで全顧客が支払不能という文章に、`noul`・`choice`・`score`の3質問を一要求で送信。
- JSON：`name`・`age`・`account.active`・`plan`を持つobjectに、active判定とplan分類の2質問を送信。
- 画像：320×320、1,125 bytesの赤色PNGをJSでbase64化してstateへ入れ、色分類と赤領域の有無を質問。

構造・質問キー・質問type・選択肢・返された数値の範囲を検証しました。`choice.probabilities`は任意項目として扱い、提供された場合だけ検証します。今回の全モデルは確率mapも返しました。確率の完全一致は要求していません。

判断内容も別に確認し、全モデルで`payments`、`pro`、`red`が選ばれました。支払問題・active account・赤領域のnoul値は高く、severityは最上位またはそれに近い値でした。これは小さな疎通用fixtureでの結果であり、分類品質の一般的な評価ではありません。Jevの小数桁が少ない確率とPerplexityの桁数の多い確率も、そのまま受け取れています。

## 親agent経路と観察

親は`deepseek/deepseek-v4.1-flash`・`effort:"auto"`で起動し、`run_js`内から`noe:decision`をimportしてJev 1.14を一度呼びました。次のモデル要求に判断JSONが渡り、親はPayments/Checkout担当と回答しました。親の処理は約3.36秒、Chat呼出し2回、`run_js`1回、Decisions要求1回でした。

実JSONLの`decision.started/completed`に親のrun/tool IDが付き、共通`model`はDeepSeek、`decisionModel`はJev、`responseModel`はJevのrevisionでした。意思決定完了直後までの実ログを`noe observe --json`で読み、親snapshotがDeepSeekのまま、`phase:"run_js"`・`status:"running"`であることを確認しました。意思決定イベントは差分にも含まれ、人間向け表示でも開始・完了を表示できました。

## 費用と保存物

| 項目 | 回数 | 報告されたusage.costの合計 |
| --- | --- | --- |
| Decisions | 10要求 | $0.000188626 |
| 親agentのChat | 2要求 | $0.001002924 |
| 合計 | 12要求 | $0.001191550 |

全要求が完了してusageを取得できました。値は実行時のAPI応答に基づき、将来の価格や同じ入力での費用を保証するものではありません。

各要求のJS、stdout/stderr、JSONL、全応答、summary、親の可読表示とsnapshotは次に保存しています。終了後の実行ホスト登録ファイルは残っていません。

```text
/home/agent/.cache/noe-qjs-repl/noe-decision-1bv69irw/live
```

## 自発的な利用の観察 — 2026-10-17

利用者の追加指示で、組込みの不変インストラクションに用途の指針を追記しました。分類・処理先や子agentの選択・成果物の条件判定で利用を検討し、特にJSの分岐や同じ基準による複数入力の評価に使えること、利用者がAPI名を指定しなくても自律的に選べることを伝えています。利用を一律には必須にせず、決定的な判定は通常のJSで行える旨も記載しました。

親はDeepSeek V4.1 Flash・effort autoで起動。課題文にはJev・`noe:decision`・APIの利用指示を含めず、6件の問い合わせをpayments/account/otherに分類する再利用可能なJSの作成・実行と、結果JSONの保存を依頼しました。

JSONLで次の挙動を確認しました。

- 入力ファイルを確認後、自ら`noe:decision`と`typesafe/jev-1.14`を選択。2件で応答形式を試しました。
- `decide`を呼ぶ`classify.js`を作成し、共通の分類基準で6件を評価。試行2要求と本番6要求はすべて成功しました。
- 保存結果は順にpayments/account/payments/account/other/accountで、各問い合わせの主な解決対象と整合しました。
- `run_js`内の`join`のimport漏れと、作業ディレクトリを誤認したimport先のエラーが各1回発生。いずれも自力で修正しました。
- 分類結果は保存できましたが、観察用に設定した`maxSteps:8`に達し、最終回答を生成する前にtaskはfailedとなりました。全体の完了成功としては扱いません。

約36.7秒、Chat 8要求・Decisions 8要求。報告費用はChat $0.008974904、Decisions $0.000217014、合計$0.009191918でした。課題・起動JS・生成JS・結果・JSONL・stdout/stderrは次に保存しています。

```text
/home/agent/.cache/noe-qjs-repl/noe-decision-discovery-4jzh3ma5
```

この1ケースではAPI名の明示なしでの選択を確認できました。再利用可能な意味分類JSを作る課題であり、他の課題でも適切に選べることや、追記前との差を示す比較実験ではありません。確認後の追加の実要求は送っていません。

## 再現コマンド

実行すると実プロバイダへの課金が発生します。通常のunit/E2Eではこのテストは選択されません。

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
NOE_DECISION_LIVE_DIR=/path/to/new-results-directory \
  go test -tags='e2e decision_live' ./tests \
  -run '^TestDecisionOpenRouterLive$' -count=1 -v
```

通常のAPI呼出しは認証の既定pathでも動きますが、実接続テストでは使用するキーファイルを明示します。実接続で通信中断・長文・巨大画像・refusalを網羅する試験は行わず、取消し・エラー経路・JSON保持はローカルfixtureで確認しています。
