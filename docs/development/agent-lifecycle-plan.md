# Agent起動と待受の実装計画

状態：実装・検証完了。2026-10-17の会話と設計レビューに基づく。[実装・検証記録](agent-lifecycle-implementation.md)にテスト結果とレビュー指摘の採否を記録した。

対象コードの基準は `4176a82`。本書は、起動・待受・queue・最小観察の計画正本とする。[実行中の操作の計画](agent-control-plan.md)は本計画の完了後に実装する。既存の[検討メモ](../future-candidates.md)より具体的な契約を両計画に定める。互換性維持は要求しない。ユーザーの実装・テスト・コードレビューの依頼に基づき本計画を実施し、commit・push・公開は行わない。

## 目的と対象範囲

通常の `noe run`・`eval`・`repl` 内から、自律agentを起動する。1タスクの回答を待つ使い方、親が子を起動して途中を監督する使い方、タスク終了後も次の仕事を待つ使い方を提供する。

本計画だけで、親agentが子を起動し、状態・結果を観察して次のタスクをqueueできるようにする。モデルに公開するtoolは引き続き `run_js` 一つとし、そのJSからagent APIを呼ぶ。人間は既存のJSONL・`noe observe`で様子を確認できる。通常の実行で人間の入力・承認待ちは設けない。

実行中のsteer、agent/run IDを指定する明示的なinterrupt、別ターミナルからの操作CLIは[後続計画](agent-control-plan.md)へ分離する。Ctrl+C・タイムアウト・親run終了による回収は本計画に含める。

以下は対象外とする。

- `steer_agent`・`interrupt_agent`、最終回答とsteerの競合処理、人間用のhost発見・操作CLI。
- 親NOE終了後も生存する独立プロセス、daemon、永続queue、クラッシュ後の復旧。
- 複数タスク間の会話履歴の自動引継ぎ、コンテキスト圧縮、AGENTS.mdの自動探索。
- 新しいプロバイダ、Web検索API、Worker対応、TUI、ネットワーク越しの管理。
- live観察とファイル観察のcursor統合、詳細イベントの再生機構。
- 互換alias、旧APIの廃止予告期間、リリース作業。

## 公開API

`noe:agent` の入口を関数形式へ統一する。

```js
import {
  run_agent, spawn_agent, observe_agent, queue_agent,
} from "noe:agent";

const config = {
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
};

// 1タスクを実行し、終了結果まで待つ。
const result = await run_agent(config, "READMEを要約して");

// 起動要求の受領後に戻り、後から観察する。
const { agentId } = await spawn_agent(config, "調査して");
const snapshot = await observe_agent({ agentId });

// 最初のタスクの後も次の仕事を待つ。
const worker = await spawn_agent(config, "最初の調査", {
  keepAlive: true,
});
await queue_agent({ agentId: worker.agentId, instruction: "続けてテストして" });

// タスク未指定ならidleから開始し、所有者の終了・取消しまで待つ。
await run_agent(config, undefined, { keepAlive: true });
```

| API | 戻る時点 | 戻り値 |
| --- | --- | --- |
| `run_agent(config, instruction?, options?)` | agent全体の終了と後始末の完了後 | 終端snapshot |
| `spawn_agent(config, instruction?, options?)` | agentの登録と初期タスクの受領後 | `{ agentId }` |
| `observe_agent({ agentId })` | 現在の状態を取得したとき | live snapshot |
| `queue_agent({ agentId, instruction, commandId? })` | 次のタスクを受領したとき | commandの受領結果 |

`run_agent` と `spawn_agent` は同じ起動処理・引数検証を共有し、戻る時点だけが異なる。起動APIに `wait` オプションは設けない。いずれもPromiseを返し、待機中もGo側は通信・観察・所有者取消しを処理できる。

### タスク終了後の寿命

`options.keepAlive` は既定で `false`。

| 指定 | タスク終了後 | instruction省略 | queue |
| --- | --- | --- | --- |
| `false` | 1タスクで終了 | 入力エラー | 受領しない |
| `true` | 次のタスクを待つ | idleから開始 | 登録後のstarting / running / idleで受領 |

`keepAlive:true` の `run_agent` は、最初のタスクが完了・失敗しても戻らない。待受全体の停止まで待つ。待機中はGo側の通知を待ち、sleepや自発的なモデル呼出しは行わない。

本計画では、待受を終了させる経路はCtrl+C・タイムアウト・親run終了・所有者取消し・致命的失敗となる。agent IDを指定して正常に停止させる入口は後続計画で追加する。JSでqueue先のIDが必要な場合は `spawn_agent` を使う。

### 設定と破壊的変更

- 公開 `createAgent()` と `agent.run()` を廃止し、互換aliasは作らない。
- `provider`・`model`・`effort`・`name`・`system`・`maxTokens` を `config` にまとめる。旧run optionsの `maxSteps`・`cacheKey` も `config` へ移す。
- rootは `provider`・`model`・`effort` を必須にする。子は起動元の `provider`・`model`・`effort`・`maxTokens` を省略時に継承し、明示指定で上書きする。
- `effort:"auto"` は有効。推論設定を送らず、プロバイダの既定動作に任せる。
- `name`・`system`・履歴・`maxSteps`・`cacheKey` は子へ自動継承しない。
- `maxSteps` はタスクごとのモデル呼出し回数上限。既存の無制限指定を維持し、各queueタスクでカウントを初期化する。
- `cacheKey` 未指定なら各runに自動付与し、そのrun内で固定する。明示文字列は各タスクで使用する。空文字列は `session_id` を送らない指定として引き継ぐ。
- `system` は呼出し元がファイルを読んだ文字列も受け取れる。instanceが受領した文字列を各runへコピーし、そのrun中は固定する。ファイルの再読込みは行わない。

### 結果とエラー

`spawn_agent` は `runId` の発行や最初のモデル応答を待たない。登録されたinstanceの `agentId` を返し、現在の `runId` は観察から取得する。

`run_agent` の終端snapshotは、少なくとも `{ agentId, state, reason, lastResult }` を持つ。`lastResult` は最後に実行したタスクのrun ID・成否・回答・エラーを持ち、未実行なら `null`。単発agentでは唯一のタスク結果となる。全タスクの回答配列は返さない。

instanceの停止理由とタスクの成否を分ける。例えば、最後のタスクが成功した後にidle中のagentを停止しても、そのタスクを失敗へ書き換えない。

- 受付前の入力不備・登録失敗はPromiseをrejectする。
- 受付後のタスク失敗は状態・結果として記録する。単発の `run_agent` はその終端snapshotを返す。
- 待受agentの通常タスク失敗は、その結果を記録して次の予約またはidleへ進む。
- 実行基盤の致命的失敗はinstanceを終了させ、理由を記録する。
- Ctrl+C・所有者取消しではGo側の回収を優先する。終了中のJSへ戻り値を届けることを回収完了の条件にしない。

## 管理単位と所有関係

| 単位 | ID | 寿命と用途 |
| --- | --- | --- |
| AgentInstance | `agentId` | 起動から停止まで固定。設定・queue・所有者を持つ |
| TaskRun | `runId` | 1タスクごとに新規。履歴・モデル呼出し・結果の単位 |
| 操作要求 | `commandId` | 受領・適用・取消し・未実行を追跡する単位 |

`name` は表示名。`parentRunId` は実際の起動元runを表し、agent IDや前のタスクのrun IDでは代用しない。

- 通常JSから起動したinstanceは、その評価Operationが所有する。native仕事として追跡し、起動Promiseが戻った後も実行中・待受中の仕事をevent loopが把握する。
- モデルの短命な `run_js` から起動したinstanceは、起動元の親runが所有する。生成JSが終了しても子instanceを保持する。
- 親runの完了・取消しは、所有する子instanceとそのタスクを取消し・回収する。
- 子instanceを短命な `run_js` のscopeへ残さない。起動要求を処理するscopeと、instanceを所有するscopeを分ける。
- 正常停止時はJS ownerへPromiseのsettleを投稿する。所有者取消しやLoop終了時はGo側のcleanupとjoinで完結させる。

instance状態は `starting -> idle <-> running -> stopping -> stopped` を基本とし、致命的な監督失敗は `failed` とする。単発instanceは1タスクの回収後に終了する。

## 実行構成

Go managerがinstance・queue・context・task runnerを管理する。モデルとの対話ループは既存JSを再利用し、Goへ移植しない。

```text
公開 run_agent / spawn_agent
  -> Go managerへ登録
  -> 各タスク用のfresh NOEプロセスを起動
  -> 非公開 executeTask でモデル / run_js loopを実行
  -> 認証付きの完了報告
  -> プロセスと子scopeを回収
  -> タスク終端を通知し、次の予約またはidleへ移る
```

`executeTask` は内部名の案。固定bootstrapはこの内部loopへ直接入り、公開 `run_agent` を再呼出ししない。公開APIへ戻すと、起動処理が再帰する。

生成する `run_js` のpreludeと、管理タスク用の固定bootstrapを分ける。現状の `createAgent` の無条件importを更新し、preludeとbootstrapで同じ名前を重複宣言しない。configと指示は構造化データで渡し、shell文字列を組み立てない。

task専用scope/contextをcontrollerから分離する。scopeにinstanceと予約タスクの対応を登録し、そのscopeの最初の `start` だけを対象runとして紐付ける。タスク内で生成した別runを誤って紐付けない。

QuickJSの値はowner上で操作する。Go goroutineから直接触らず、既存の `Loop.Begin/Done/Post` に接続する。未解決Promiseだけを残す待受は採用しない。

## queueと所有者取消し

- `keepAlive:true` の登録後のstarting / running / idleで受領し、FIFOで1件ずつ実行する。spawnの応答直後にqueueしても最初のprocess開始待ちは要求しない。
- stopping / 終端では新規受領しない。
- 各タスクで新しいrun・messages・既定cache ID・回数カウントを作る。前タスクの会話を自動追加しない。
- 必要な引継ぎはタスク本文・ファイル・観察した結果を使う。
- `commandId` 未指定時は発行する。同じIDの再送は同一instance・同一プロセス内で重複実行しない。異なる内容でのID再利用はエラーにする。
- 受領済みの予約は、実行・取消し・未実行の結果を追跡する。所有者取消しで黙って捨てない。再起動を越える配送保証は設けない。
- 所有者取消しでは新規受領を止め、実行中task・未実行予約・子scopeを取消して回収する。取消しをidle待受へ変換しない。
- 終了判定はGo側で一度だけ確定し、受付停止と取消しを順序付ける。lock保持中や自分自身のrequest handler内でjoinしない。

本計画の完了報告は正常終了・失敗・所有者取消しを扱う。steer用inboxや、保留steerをJSへ返す最終回答の確定判定は後続計画で追加する。

## 完了報告とプロセス回収

認証付きの完了報告はモデル結果の確定、Runner終了は実行プロセスの回収として区別する。stdoutを回答やタスク成功の判定に使わない。

- final確定時は `agent.final` を一度通知する。
- taskの `agent.completed` 等の終端通知、次taskの開始、停止待ちの解決はRunner・子scopeの回収後に行う。
- 正常な完了報告と正常なRunner終了の両方でタスク成功とする。
- 完了報告なしのprocess終了はprotocol failureへ収束させ、報告待ちで残さない。
- 非zero終了・監督失敗ではtask失敗を記録する。確定済み回答があれば診断と共に保持し、モデル回答と実行全体の成否を混同しない。
- 通常終了・失敗・取消しで既存のprocess group / Job Objectによる回収を利用する。

## 最小観察と後続計画との境界

live snapshotには少なくともinstance状態・現在のrun ID・queue件数・直近task結果を含める。起動直後はまだrun IDがない状態を許す。最初のタスク完了を待受instance全体の完了として表示しない。

既存JSONLイベントへ `agentId` を追加し、instanceのstart / idle / stopping / stoppedと、タスクの開始・終了を区別する。queueにはcommand IDと受領・実行・取消し等の結果を対応付ける。

`observe_agent({ logPath, runId, ... })` のファイル観察も提供する。認証やモデル接続なしで既存ログを読める用途を保ち、live用agent ID指定とファイル指定は混在させない。詳細な結果・イベントはJSONLへ残し、live snapshotに全履歴を累積しない。人間向けの既存 `--observe`・`noe observe` も利用できる。

本計画のlive観察とqueueは、実行中JSまたは親run_jsから既存broker経由で呼ぶ。scopeと親子の所有関係で宛先を検証する。別ターミナルからのhost発見やcontrol認証は後続計画に含める。

後続計画が利用する前提は以下に限る。

- `agentId`・`runId`・所有関係・状態をGo managerで解決できる。
- taskとinstanceのcontextが分離され、取消しと回収を内部処理として呼べる。
- queue受領、終了確定、取消しに明確な順序と受付停止の境界がある。
- 完了報告は結果確定とRunner回収を区別し、終端通知を一度だけ行う。
- 現在runと直近結果を観察できる。

steer用inbox、公開interrupt、human control endpoint等の先行実装は本計画の完了条件にしない。

## 実装スライス

各スライスは関連する実装・テスト・文書を更新し、次のスライスの前提となる契約を確認する。完了状態と検証結果は実装時に記録する。

| Slice | 作業 | 主な受入条件 |
| --- | --- | --- |
| S1 内部loopとrunnerの分離 | 非公開task実行入口、管理用bootstrap、prelude分離、構造化入力・完了報告 | 既存のモデル/tool/思考履歴の往復を維持。bootstrapが再帰せず、importも重複しない |
| S2 Go manager | instance/task/command ID、FIFO、所有context、状態遷移、task回収 | fresh taskと構造化結果を紐付け、回収後に次taskへ進む。親取消しで子を回収 |
| S3 公開APIと最小観察 | run/spawn、keepAlive、queue、agent IDによるsnapshot、旧公開API廃止 | 単発と待受、rootと親run_js両方の寿命が成立。受付後すぐ観察可能 |
| S4 利用例と総合確認 | instructions・README両言語・APIガイド・examples・REPL例の統一、総合テスト | 親JSによる起動→観察→queueと、root待受→所有者取消しが成立 |

依存順はS1→S2→S3→S4。instructionsと例は機能導入に合わせて更新し、S4で全体の整合性を確認する。ここまでで本計画を単独で完了でき、[実行中の操作](agent-control-plan.md)は別の実装単位とする。

変更対象の中心は `internal/agent/`、`internal/runtime/agent.go` とevent loop接続、関連tests。既存表示のイベント対応が必要な範囲で `internal/cli/` も更新する。生成JS・補完・REPL例・`examples/`・README・`docs/api.md`・`docs/observe.md`・`docs/events.md` に残る旧契約も確認する。過去インクリメント・リリース記録の旧API表記は当時の履歴として扱う。

## 検証と完了条件

fixture provider・ローカルbroker・実NOEの子プロセスを使う。競合の確認には制御可能なgateを使い、固定sleepに依存しない。

- 単発runの結果、spawnからの観察、独立した親の並行実行。
- 初期タスク完了後もrootのJSを維持し、idle中のモデル呼出しがなく、queueで起床する。
- FIFO、新run/履歴/既定cache ID、タスクごとのmaxSteps、通常失敗後の継続。
- 短命run_jsの終了後も子が存続し、所有親runの終了・取消しで回収される。
- agent IDから現在のrun ID・直近結果を取得できる。未開始・idle・終端も観察できる。
- モデル/tool往復で完全なassistant messageとreasoningを保持する。
- 完了報告なしの終了、報告後の異常終了、出力失敗、Ctrl+Cのmodel / run_js / idle各経路。
- run/eval/replでの動作、REPL中断後の再評価、既存のファイル観察と人間向け表示。
- 親JSの観察とqueueが所有範囲に従い、別の親のagentと混同しないこと。

スライスごとの変更経路を確認後、総合的に `go test -tags=e2e ./...` と `go vet -tags=e2e ./...` を実行する。並行処理を含む変更packageと関連E2Eはrace detectorで確認する。OS別のprocess管理に変更した場合は配布対象のクロスビルドも確認し、native実行と区別して記録する。

実プロバイダを使う有料の確認はfixture検証から分ける。従来許可された接続先・モデルを使う場合も、対象時点の認証・モデル指定と実行範囲を確認して記録する。この計画だけを根拠に接続や公開は行わない。

分割前の設計レビューではAPIの重複、寿命・所有関係、最終steerと終了の競合、回収順を確認済み。本計画では起動・待受・queue・観察・所有者取消しまでを実装する。最終steer等の指摘は後続計画へ移した。実装後は変更コードと検証結果に基づくレビューを行い、設計レビューで代用しない。

[開発記録へ戻る](../README.md)
