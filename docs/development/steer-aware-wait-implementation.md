# Steer-aware grouped task wait・実装記録

## 正本と開始状態

2026-10-25。[作業指示](steer-aware-wait-implementation-task.md)とAGENTS.mdに従う。利用者指定の末尾 .m は存在せず、同名 .md を正本として確認した。開始HEADは `525a96a`（v2.5.1公開後）。開始時の既存差分：docs/development/README.md、docs/future-candidates.md、docs/model-catalog.md、未追跡の作業指示と既存events/cache。これらを上書き・巻戻しせず、関連箇所を編集する場合も既存内容を維持する。push/release/常用binaryの置換は行わない。

ログ：`/var/tmp/noe-steer-aware-wait-Ekbh7s`。以下は採用契約と短い実装計画であり、独立reviewで必要な修正があれば理由とともに記録する。

## 採用契約

- 新しい公開JS入口は `wait_tasks({tasks: [{agentId, commandId}, ...], includeAnswer?})`。少なくとも一つの一意なtask参照を受ける。既存 `wait_task` の完了専用契約は変えない。
- 結果は `{reason: "completed" | "steer", runId?: string, completed: [{agentId, outcome: TaskOutcome}], pending: [{agentId, commandId}]}`。各配列は入力順、outcomeは従来同様includeAnswer:falseでanswerを省略しanswerBytesを返す。結果を消費せず、後からwait_task/read_task_resultで取得できる。pendingを終端状態に偽装しない。
- 全対象が完了すればcompleted。呼出しscopeの現在のowner runへ未適用steerがあればsteerで集合全体を返す。両者が同時に観測できる場合はreason:steerを優先し、完了結果も含める。トップレベルのGo/JS Clientにowner runがない場合は全完了だけを待つ。任意runIdの指定は受けず、既存owner/親scopeから導く。
- 全IDの所有/存在確認を待機開始前に行う。request/Operation/親run取消しは拒否・errorとして区別し、steer理由にしない。待機を返すためにOperation・run・子をcancelしない。
- steerは通知だけで消費・delivered/appliedへ変更しない。command.acceptedの観察公開を終えてからwaitを起こす。Host.mu内で未適用statusと通知channelを確認し、登録直前の受付もscanで捕捉する。applied通知を再待機で使い回さない。origin.kindに制限しない。
- per-runの小さな通知channelと既存command.doneを待ち合わせ、定期polling・モデル呼出し・新eventbusを導入しない。動的な複数channelはreflect.Selectで待ち、Goの65,536-case上限を超える集合のみrequest-local completion通知へ集約する。上限による新規入力制限や追加goroutineは設けない。JSのPromise.raceで未解決RPCを残さない。observer/待機はHost.muの外。
- run_jsの既存scope終了・request join・子のrun所有は維持する。tool結果→完全block/checkpoint→既存begin_model_step/apply_steers→次要求という経路を使う。任意JSや他の無期限toolを強制中断する機能ではない。
- Go入口は `Client.WaitTasks(ctx, WaitTasksRequest) (WaitTasksResult, error)`。公開aliasとしてTaskReference/WaitTasksRequest/CompletedTask/WaitTasksResultを追加し、JSONとtypedのsnapshot/結果複製規則に合わせる。hook-scoped JS APIにも同じ入口を接続する。

## 分担・検証計画

1. Core/API担当：Host待機・steer公開通知、JSON/typed/公開Go/JS/hook接続と直接関連unit試験。対象internal/agent、internal/agentbridgeの必要箇所、agent/host.goとGo試験のみ。既存待機互換、事前steer、競合、複数steer/再待機、owner終了、権限、保持結果を決定的なchannelで確認。
2. Integration担当：新規internal/runtime/steer_aware_wait_test.goに実executorと制御可能モデルfixtureを実装。二つの子を未完了のまま保持し、script-originも含む親steerでwait toolが返り、親の次要求に指示とwait結果が入り、子が継続していること、親が明示的に子へ対処を選べることを確認。coreと共有fileを編集しない。
3. 親：公開型・内蔵docs/facts/policy・利用ガイドと例を更新し、モデルがgrouped waitを選んでreason:steerならtoolを終了する経路を説明する。旧wait_taskも保持。registry/inventory試験を更新する。
4. 関連unit/fixture・変更JS lint/gofmt/diff checkを実施後、共有状態・取消し・履歴を独立reviewし、具体的経路を伴うP2以上を解消する。修正は元担当へ戻す。
5. 安定候補に関係するraceと一回の全体unit/E2E統合確認。明示timeoutと実行中ログ保存を使う。非関連のcross-platform全種compile、実provider、長時間待機、負荷反復は追加しない。既存に由来する失敗は限定比較で分ける。

## 進捗

- 完了：作業指示、既存待機/steer/run_js scope/公開API接続を確認。上記の契約と分担を決定。
- 現在：実装・独立コードレビュー・最終検証が完了。親の型・facts/policy・内蔵docs（run_jsの委譲recipeを含む）・利用ガイド・並列例の更新と例のlintは完了。public export inventory assertionも更新し、型consumerの静的確認を実施した。
- core担当：agentId a6e370f4c89ccf59bf35f8012a61676ca00027f2b97336fa、task 01a32626eb629bb6d2f178ce876af82c472d27a2b4ee8b0a。
- integration担当：agentId 16dfc8adae255b7df998f2bdc07f7c9ee817201b494b4d8a、task 39755ccd2d696fdaa8c7258aebcc199bc090c400e403e2e9。
- 完了した関連検証：coreのinternal/agent・公開agent・agentbridgeが成功。実executorのtrusted_host/script_client両方で、2子pendingのまま親次要求へsteerが入り、次toolから左cancel/右steer・右の結果取得が成功。型consumerのtscと例のBiome lintも成功。最初の型検査は古い案内にある.zed/noe.d.tsが実在せず失敗したが、現行のstdlib/types・agent/typesだけを指定して修正済み。
- 証拠：core/affected-packages-final.log、integration/evidence.json、type-check.log（すべてログ親ディレクトリ配下）。
- review担当：agentId 44c1b4caf9357068297cc38c6dd352148cb0149b92418d99、task 6de6cf0d8d66a3b5f5aa01b7a5d85ba4885ba7d77080bfd4。
- review完了：P2以上0件。任意P3の型付きrecipeはcommandId確認と明示pair化で改善。completion通知の二経路は整合しており必須修正なしとの判断で維持する。
- 次：完了。追加承認によるコミット・公開・常用binary置換も完了（末尾参照）。
- 公開・コミット・binary置換：実装依頼完了時点では未実施。その後の追加承認によりv2.6.0公開・常用binary更新済み（末尾参照）。

## コンテキスト管理の観測

開始時点ですでに長い会話を引き継いでいる。NOE_CONTEXT_STATUSは履歴のbyte推定が約81〜87%である旨を通知しているが、これはcontext.trimmed eventやtrimCount増加の証拠ではない。contextWindow/入力予算/retainRatio/cache設定は変更せず、観測のための不要な読込みはしない。新機能の開発が終わった時点までに自然なtrimを観測できた場合だけseq/runId/usageと進捗維持を記録し、未観測ならその事実を記録する。

## 最終検証・レビュー結果

いずれも成功。テスト出力は開始時から保存し、外側のtimeoutも付けた。検証後の変更はdocsの型付きrecipe・末尾空行・この記録のみで、成功済みの全体試験は繰り返していない。

| 対象 | 実行したコマンド・結果 | ログ（ログ親ディレクトリ配下） |
| --- | --- | --- |
| core/APIの関連unit | `timeout 150s go test -timeout 120s ./internal/agent ./agent ./internal/agentbridge -count=1`：PASS | `core/affected-packages-final.log` |
| 実executor受入 | `timeout --signal=TERM --kill-after=10s 150s go test ./internal/runtime -run '^TestSteerAwareWaitRealExecutor$' -count=1 -timeout=90s -v`：trusted_host／script_clientともPASS | `integration/executor-targeted-1.log`、`integration/evidence.json` |
| 全体unit/E2E | 外側300s、`CGO_ENABLED=0 go test -tags=e2e ./... -count=1 -timeout=3m`：PASS、一回実施 | `final-unit-e2e.log` |
| 関係packageのrace | 外側300s、`CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race ./internal/agent ./agent ./internal/agentbridge ./internal/agentexec ./internal/runtime -count=1 -timeout=3m`：PASS、一回実施 | `final-race.log` |
| 変更JS lint | `biome lint internal/agent/js/api.js internal/agent/js/agent.js examples/agent/agent-parallel.js`：PASS | `final-static.log` |
| 新公開型のconsumer | `tsc --noEmit --project /var/tmp/noe-steer-aware-wait-Ekbh7s/jsconfig.json`：PASS | `type-check.log`、`type-consumer.js` |
| 整形・差分 | 変更・新規Goの`gofmt -l`出力なし、`git diff --check`：PASS。facts.md末尾の余分な空行を一度検出し修正 | `final-static.log` |

独立reviewは共有状態、accepted公開、取消しとjoin、結果保持、所有scope、tool結果→checkpoint→apply→次要求、JS/Go/typed/hook/型/docsの接続を確認し、**P2以上0件**。任意P3だったdocs recipeのoptional commandIdは確認後の明示pair化で改善した。completion通知の二経路は不具合なし・必須修正でないとの判断で維持した。既知の関連未解決事項はない。配布先全種compileや実providerの長時間試験は本機能の完了条件でなく、追加していない。

### 実利用経路の成立

trusted Hostと通常のGo Client（control=false、origin=script）の両方で、二子をchannelで未完了に保った状態から親へのsteerにより待機RPCとrun_jsが終了した。親の次の実モデル要求にはsteer理由・二つのpending ID・自動導出runIdと追加指示が一回だけ含まれ、子のcontextは取り消されなかった。その後、親の次toolから両子をobserveし、左を明示cancel、右を明示steerして指示の適用と結果再取得まで成功した。受付→run_js終了→tool終了→既存checkpoint/apply→次モデル要求の経路を確認した。旧wait_taskの完了専用・保持結果・繰返し取得の契約も維持している。

## 自然な履歴trimの観測結果

- 既存events：`.noe/events/noe-events-20261024-224429-946e65d4bb5b6f10.jsonl`
- `context.trimmed`：**seq=28205**、runId=`e29f249f17eec6bab8a471feb553be744c60009f7f7d0039`、2026-10-25 01:04:16 JST、reason=`input_budget`。
- `trimCount: 0 → 1`、108 blocksを除去。履歴のbyte推定は **667626 → 61952**、当該budget=663867、totalInputBudget=670236、modelWindow=872000。これはJSON UTF-8 bytes/4の推定であり、providerの実token数とは区別する。
- trim直前のprovider usageはpromptTokens=409152、cachedTokens=408320。trim後の最初のモデル要求はpromptTokens=29390、cachedTokens=0。その後の観測ではpromptTokens=35805、cachedTokens=29952。詳細はログ親ディレクトリの`context-observation.json`。
- trim後、AGENTS・作業指示・この実装記録を再読し、目的、採用契約、担当、成功済み試験、残作業（静的差分チェックの末尾空行修正と最終記録）を復元した。成功済みの全体試験を再実行せず完了できた。
- **履歴trimを観測したもので、モデルによる要約は観測していない。** contextWindow・入力予算・retainRatio・cache設定の変更、観測待ちや履歴を膨らませるための読込みは行っていない。

実装依頼の完了時点ではローカル未コミット差分として保持し、push・release・常用binary置換は行っていなかった。開始時の他の文書差分・events/cacheは保持した。

## 追加承認：コミット・公開・常用binary更新

2026-10-25、実装完了後に利用者がコミット・push・release・常用binary置換を明示承認した。上記「未実施／対象外」は実装依頼の完了時点の記録である。追加APIのminor releaseとしてv2.6.0を準備し、既成功の全体unit/E2E・関係race・独立reviewを再利用する。公開証拠は[release checkpoints](../releases/release-checkpoints.md)へ記録する。公開作業ログは`/var/tmp/noe-v2.6.0-release-T7lygk`。既存model-catalog差分、events、cacheは今回のcommit対象から除外する。

## v2.6.0公開・常用binary更新の完了

追加承認を実施した。実装commitは`f01351e`、release準備は`c2d6121`、tag `v2.6.0`は`9ae801f145b232e718b1fff74e3bcff9bfb1dfd2`。mainとtagをpushし、[CI run 38](https://forge.harakara.site/littleisland/noe/actions/runs/38)が成功、[Release](https://forge.harakara.site/littleisland/noe/releases/tag/v2.6.0)を公開した。

全6配布binaryのchecksum、Linux amd64のversion/build metadata・新API/内蔵docs・local fixture、実際の公開moduleからのstdlib/agent consumerと新Go WaitTasks・保持結果の取得が成功した。公開済みassetを`./noe`と`~/.local/bin/noe`へ原子的に配置し、両方のversion/hashとPATH解決を確認した。旧v2.5.1のbackupは`/var/tmp/noe-v2.6.0-release-T7lygk/installation-tvx98f1a`。

全体unit/E2E・raceは既成功結果を再利用し、再実行していない。既存model-catalog差分・events/cacheをcommit対象から除外し保持した。公開・導入の詳細は[release checkpoints](../releases/release-checkpoints.md#v260-release-validation-and-local-installation--2026-10-25)と`/var/tmp/noe-v2.6.0-release-T7lygk`に保存した。実行中processの再起動やtagの付け替えは行っていない。
