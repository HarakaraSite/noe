# Agent lifecycle/control：OpenRouter実接続確認

2026-10-17。利用者の明示許可を受け、commit `4f5d91bfe5c638c127b2e1a676e1863eb47d48ab`の変更を実プロバイダで確認した。**12ケースすべて成功。実装上の不具合は見つからず、製品コードの変更はない。**

[機械可読の結果](agent-control-openrouter-results.json) · [fixture・race等の検証記録](agent-control-implementation.md)

## 環境と確認方法

- Linux amd64、Go 1.27.2、CGOなし。OpenRouter公式Go SDK v0.9.59。
- モデルは利用者指定の`deepseek/deepseek-v4.1-flash`、effortは`auto`。
- 登録済みの`~/.config/noe/credentials/openrouter-api-key`をGo hostが参照した。キーのコピー・値の出力は行っていない。
- 既存の明示タグ付きlive testを実行し、追加の操作は実際の`./noe run/eval/repl`と別processのCLIから確認した。モデル応答のfixtureへの置換やHTTP proxyは使用していない。
- 操作中の待合せには、生成JSが作る準備完了ファイルと、観察snapshot・実際のSSE fragmentを使用した。モデルの所要時間を固定sleepで推測していない。
- 各ケースで作業directoryとcontrol登録先を分離。生成processのPID、JSONL、stdout、stderr、操作結果を保存した。正常終了と取消し後にcontrol登録ファイルが残らないことも確認した。

## 結果

| ケース | 実接続で確認した内容 | 結果 |
| --- | --- | --- |
| aggregate | run_jsで実ファイルを読み、count=4 / total=100 / average=25 | 成功 |
| repair | 故意のJS例外 → モデルによる修正 → 実ファイル集計 | 成功 |
| parallel | 親が2子agentへ並列委譲し、各子がJSで算出した30 / 70を親が100へ集計 | 成功 |
| lifecycle | idle開始、FIFOの2タスク、queue再送・内容競合、taskごとの別run ID、固定system、idle停止後の直近回答保持 | 成功 |
| js-steer | 生成JS実行中の追加指示、同じcommand IDの再送・内容競合拒否、一度だけの適用、変更後の最終回答 | 成功 |
| parent-supervision | 実際の親モデルがrun_jsからspawn/observe/steer/queue/interruptを使用し、待受の子を監督 | 成功 |
| human-controls | 別CLIからagents/steer/queue/run interrupt/agent interrupt。task取消し後のFIFO継続、busy停止と未実行予約取消し、人向け表示・ファイル観察 | 成功 |
| stream-interrupt | 実プロバイダのthinking fragmentを受信してから通信中のrunを取消し、cancelled結果へ収束 | 成功 |
| repl | 実タスク完了後にidle待機、外部停止後に回答とpromptが戻り、次の`2+2`を評価 | 成功 |
| nested-run-interrupt | 親runの取消しで子agent・親子run_js・shell・sleepを停止。生成NOEのreapも確認 | 成功 |
| nested-owner-sigint | rootへのSIGINTで上記の所有範囲全体を停止・回収。control登録も除去 | 成功 |
| step-limit-recovery | maxSteps=1でtool後の次モデル要求を拒否してfailed。keepAliveは残り、次タスクが正常完了 | 成功 |

`parent-supervision`では、監督用JSを親への指示に含め、親モデルが実際のrun_jsとして実行したことと、子の親run ID・steer適用・タスク結果・停止イベントを照合した。自由な監督方針の立案能力を評価する試験ではない。

保存した操作ケースのJSONLについて、seqの連続性、execution IDの一致、各runの終端イベントが一度だけであることも検証した。thinkingの受信、英語のrun_js先頭コメント、最終回答、成功・取消しの観察も確認した。

## リクエストとusage

- モデル要求35回、完了レスポンス34回、run_js開始19回。
- 完了レスポンスの`usage.cost`合計：`0.0107573472`。
- 完了レスポンスで観測したcached tokens合計：`37120`。

通信途中に取消した1要求には完了usageがないため、上記costは実請求総額を表さない。キャッシュの比較benchmarkや、session IDによるrouting効果の分離測定は行っていない。reasoningとsession IDの要求内容の完全な照合は既存fixtureテストで扱い、今回の実接続では複数モデル往復・thinking受信・実際のcached tokensを確認した。

## 検証スクリプトの修正

製品側の失敗と混同しないよう、最初の検証スクリプトで誤っていた期待値も記録する。

1. ファイル観察結果をlive snapshotと同じ`state`で読んでいたが、ファイル観察の公開フィールドは`status`。人間CLIの操作と生成processの回収は既に成功しており、保存ログを`status`で読み直して最終結果を確認した。
2. runへのSIGINTをexit 130と期待していたが、既存のrun/eval契約は`context canceled`を出してexit 1（`tests/execution_timeout_e2e_test.go`でも確認）。REPLとは異なる。既存契約に修正し、保存した終端イベント・診断・全PIDの回収を照合した。

どちらも再検証は保存済みログとprocess状態で行い、実プロバイダへの同じ要求は繰り返していない。初回の失敗記録もraw artifactsに残している。

## 証拠と対象範囲

raw artifacts：`/home/agent/.cache/noe-qjs-repl/noe-agent-live-4fulqulq`。

- `baseline-results.json`：既存live testの応答・trace・usage。
- `control-results.json` / `nested-results.json` / `limit-results.json`：追加ケースの結果。
- 各ケースの`main.js`・`events.jsonl`・`stdout.txt`・`stderr.jsonl`。人間用ケースには整形表示とfile観察結果も保存。
- `live_controls.py` / `nested_controls.py` / `limit_recovery.py`：実行・検証に使用したローカルharness。再利用時は準備完了ファイルを混ぜないよう、`NOE_AGENT_LIVE_WORK_DIR`に新しいdirectoryを指定する。
- 実行バイナリのSHA-256、各ケースの要求数・usage・イベント件数は機械可読結果へ記録。

確認対象はこのcommit・モデル・Linux環境。実装時のfixture/raceテストを補う実接続確認であり、他プロバイダ・他モデル・他OSや、競合の全順序を実サービス上で網羅したものではない。テスト後に追加したrepository内の変更は検証記録のみで、commit・pushは行っていない。
