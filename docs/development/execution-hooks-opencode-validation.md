# 実行group・hook・contextのOpenCode Go実接続確認

状態：2026-10-18、利用者のkey更新後に全6ケースの実接続受入を完了。
成功済みケースは繰り返さず、未達ケースのみ再試行したため、成功の証拠は複数の実行ログに分かれる。
通常・批判的reviewでも要求・usage・session・取消し終端・PID消滅を確認し、必須追加指摘なし。

対象は許可済みの`opencode-go` / `deepseek-v4.1-flash`、`effort:"auto"`。
資格情報は既存の`~/.config/noe/credentials/opencode-go-api-key`をGo hostから読む。
他providerの実接続は行わない。API key、認証header、control capabilityは成果物へ保存しない。

基準commitは`6bd0093527242c894f4b9ecaada2c5d924a86969`と今回の未コミット差分。
一時ビルドを使い、既存`./noe`は置換しない。
全体の検証記録は[実装記録](execution-hooks-implementation.md)、受入条件は[計画](execution-hooks-plan.md)。

| ケース | 実施内容 | 結果 |
| --- | --- | --- |
| L1 | context通知、実tool往復、usage/reasoning replay、4phase | 成功。4要求・3tool往復、計算結果4件/合計100/平均25、4/4 model hookと3/3 tool hook |
| L2 | 親・子・孫、hook継承、通常import、keepAlive queue、session | 成功。3instance・4run、同group・別session、queue再送は1件、終端active数0 |
| L3 steer | 準備中の追加指示 | 成功。before hookは1回、最初の実要求に追加指示、回答確定 |
| L3 hook取消し | stalled hookと所有子instanceのinterrupt | 成功。hook.cancelled、子owner_cancelled、親task_interrupted、終端active数0 |
| L3 process取消し | 実run_jsとshell・孫processのinterrupt | 成功。実tool開始後のcancel終端と3PID消滅を確認 |
| L4 | Aの共有上限と予約回収、独立root Bの完了 | 成功。A/B各1実要求、Aはlimit1/used1で停止・queue取消し、Bはその後正常完了 |

成果物の親directory：`/tmp/noe-execution-hooks-validation`。
case成果物は`live/`、成功caseの集計は`accepted-cases.json`。
各directoryに実行JS/hook、結果、JSONL、人間向け出力、許可項目だけのwire receipt、summaryを残した。
`summary.exitCode:0`だけで判定せず、Goのcase成功とイベント・実結果を照合した。

| 成功case directory（`live/`以下） | 実要求数 | prompt / completion / cached tokens |
| --- | ---: | --- |
| `l1-notice-tool-1679603462` | 4 | 16011 / 1673 / 10496 |
| `l2-descendants-queue-1109249868` | 4 | 12735 / 787 / 11264 |
| `l3-steer-in-hook-459985156` | 1 | 3233 / 136 / 0 |
| `l3-cancel-stalled-hook-2584091174` | 1 | 3182 / 166 / 3072 |
| `l3-cancel-run-js-tree-3216055641` | 1 | 3236 / 373 / 3072 |
| `l4-shared-limit-isolation-1166572800` | 2 | 6364 / 255 / 6016 |

数値は成功caseの要求別`model.finished.usage`を合算したもの。cachedはpromptの内数であり、
差し引かない。失敗試行のusage未取得分を補完したり、費用合計として扱ったりしない。
成功caseの13要求はすべてHTTP 200で、`User-Agent: noe/v1.4.0`、run IDに対応するsession、
要求末尾のcontext通知1件を確認した。L1は同sessionを固定し、後続要求にreasoningと
対応するtool call/resultを再送した。L2の親・子・孫・次queue taskは別sessionになった。
contextの直前request ID・prompt/cached実測も実要求のusageと一致した。

ログ`live-retry.log`は先頭4case成功、その後のtool準備未到達によって全体FAIL。
`live-retry-remaining2.log`は残り2caseだけを実行してPASS。製品コードの追加修正は不要だった。

## 保存した失敗試行と限界

最初の`live-test.log`はHTTP 401 / `api_key_expired`。当時のNOE/hjh keyは同一で、
直接接続でも同じ応答だった。診断は`auth-status.json`、初回caseは
`l1-notice-tool-1657587125`と`l2-descendants-queue-255706166`。
中断時に所有NOE/testのPID消滅は確認したが、初回L2の最終snapshot照合は未完了。
key更新後の成功caseとは区別して残す。

tool取消しの最初の2試行（`l3-cancel-run-js-tree-2535302360`と`4268067785`）は
HTTP 200でもtool開始前に`invalid or incomplete response stream`となり、task失敗で回収された。
後者のログは`live-retry-remaining.log`。長いthinkingは保存されたが、wire receiptに
finish reasonは残していないため、失敗の直接原因は確定していない。
task文の「必要なbackground process tree」を調べようとしていたため、最終試行では
呼び出す短いJSと、beforeToolが実process treeへ置換することを明記した。
hook本体・実process・interrupt・回収判定は変更せず、実tool開始後の停止を確認した。
これは任意の曖昧な課題でモデルが必ずtoolを選ぶことの保証ではない。

decision共有計数・未知usage・並列受付競合・その他provider/adapterはfixtureで確認した。
今回の実接続対象は許可されたprovider/modelのChat Completions経路に限る。

## 再実行

harnessは追加tagとopt-inを両方必要とし、通常suiteでは接続しない。
tag付きのopt-in無効実行とvetは成功した。実行コマンド：

```sh
NOE_AGENT_HOOKS_LIVE=1 \
NOE_AGENT_HOOKS_LIVE_DIR=/tmp/noe-execution-hooks-validation/live \
NOE_OPENCODE_GO_API_KEY_FILE=/home/agent/.config/noe/credentials/opencode-go-api-key \
go test -v -failfast -tags='e2e hooks_live' ./tests -run '^TestHooksOpenCodeLive$' -count=1 -timeout=30m
```

`-failfast`で失敗後の後続case実行を止める。初回の失敗成果物は残す。
最終reviewではharnessのtool replay・instance終端・追加queueの照合条件を修正した。
製品側のunit/E2E/race/vetと6対象ビルドの記録は実装記録へ保存した。
