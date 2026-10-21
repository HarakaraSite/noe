# HTTP agent サーバー

[examples 一覧へ](../README.md)

NOE 上で動くローカル専用の小さな UI / JSON API です。一体の keepAlive agent に依頼を順番に渡し、結果を依頼 ID 別にメモリへ保持します。ブラウザでは指示の送信、一覧・詳細・進捗・回答の確認、追加指示、停止ができます。外部の UI ライブラリや CDN は使いません。

## 起動

リポジトリのルートでビルド済みの `./noe` を使います。既定の provider の認証は先に [ChatGPT サインイン](../../docs/openai.md)で設定してください。

```sh
./noe run examples/agent-server/server.js

# すべて明示する場合（以下が既定値）
./noe run examples/agent-server/server.js \
  --port 8080 --provider openai-chatgpt --model gpt-6.1-sol --effort high

# 空きポートを OS に選ばせる
./noe run examples/agent-server/server.js --port 0
```

待受先は **127.0.0.1 のみ**です。起動時に `{"url":"http://127.0.0.1:...","agentId":"...",...}` の JSON が出ます。その `url` をブラウザで開いてください。以下の curl の `BASE` も実際の URL に置き換えます。ブラウザは GET を約 1.5 秒おきに直列実行し、更新中も入力欄を保持します。追加指示の下書きは依頼ごとに保持します（ページを閉じると消えます）。

### 詳細な進捗を表示する場合

```sh
# この起動専用の新しいログを使う（相対パスの基準は作業ディレクトリ）
EVENTS_DIR=$(mktemp -d)
EVENTS="$EVENTS_DIR/agent-server.jsonl"
./noe run --events="$EVENTS" examples/agent-server/server.js \
  --events-path "$EVENTS" --port 8080
```

`--events-path PATH` は、この **同じ CLI プロセス**が `--events=PATH` に書く既存の JSONL イベントログを読みます。二つのオプションは同じファイルを指定してください。別プロセスのログや過去の実行のログを流用しないでください。サーバー自身が別の agent 実行やログ書込み用プロセスを起動する設定ではありません。

ログがあれば、詳細な進捗と `command.applied` / `command.cancelled` の正確な記録を反映できます。ログなしでも live snapshot の状態・コンテキスト情報を表示し、実行中は同一 commandId の冪等な受付照会で steer の状況を追跡します。ただし、受付や配送は適用の証拠ではありません。イベントや観察間隔によっては中間状態が見えないこともあります。

## API と状態

| メソッド・パス | 内容 |
| --- | --- |
| `GET /` | 日本語の操作画面 |
| `GET /tasks` | `{tasks:[...], agent:{agentId,state}, supervisorError?}` |
| `POST /tasks` | JSON `{instruction:"..."}` を予約し、202 で受付 ID を返す |
| `GET /tasks/:id` | 依頼の全情報（下記） |
| `POST /tasks/:id/steer` | JSON `{instruction:"..."}` を実行中の依頼へ追加し、202 で受付を返す |
| `POST /tasks/:id/interrupt` | body なし。依頼単位の停止 / 未開始予約の取消しを202で受け付ける |
| `POST /shutdown` | body なし。202 の応答後、サーバーと所有 agent を正常停止する |

依頼詳細は `{id,instruction,state,runId,answer,error,result,steers:[{commandId,instruction,status}],progress?:{phase,action,lastMessage,...}}` です。開始前の runId や未取得の結果などは未設定 / null の場合があります。一覧だけではなく詳細 API で結果を取得してください。progress は利用可能な情報のみを含みます。UI はその内容をテキスト表示し、モデルが返した HTML は実行しません。

依頼の状態は `queued`（予約）、`starting`（開始中）、`running`（実行中）、`stopping`（停止処理中）、`completed`（正常完了）、`failed`（失敗）、`cancelled`（取消し）です。停止の受付から完了までは時間がかかることがあります。

追加指示の状態は次のように区別します。

- `accepted` / `delivered`: 受付 / 配送。**まだ適用済みとは限りません**。実行中のモデル要求や tool を中断せず、次のモデル判断の前に適用されます。
- `applied`: 適用済み。イベントがなくても、依頼の正常完了（completed）は受理済み steer の適用を意味します。
- `cancelled`: 取消しが確認されたもの。
- `unknown`: 適用を確認できないもの。failed / cancelled で終了し、未適用だった指示の適用・取消しを観測できなかった場合などです。**適用済みとは表示しません**。

不明な ID は404、不正 JSON・空の指示は400、操作できない状態は409、未対応メソッドは405、Content-Type 不備は415、停止中・利用不能なサーバーへの依頼は503を返します。starting は NOE へ引き渡した直後で runId 未取得の場合があり、その間の操作は409になります。`/tasks` と steer の POST は `Content-Type: application/json` が必須です。ブラウザは HTTP / 通信エラーと supervisorError を表示します。logError があれば、詳細な進捗や追加指示の適用記録が不完全な可能性を警告します。通信が切れてもサーバー側で受付済みの場合があるため、同じ依頼を再送する前に一覧を確認してください。

## curl で操作

次の例は別の端末で実行します。Python 3 は返された ID の抽出だけに使っています。

```sh
BASE=http://127.0.0.1:8080
curl -sS "$BASE/tasks"

TASK_ID=$(curl -fsS -H 'Content-Type: application/json' \
  -d '{"instruction":"作業を段階的に進めてください。まず run_js で30秒待機し、その後 README.md を読んで日本語で要約してください。"}' \
  "$BASE/tasks" | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')
curl -sS "$BASE/tasks/$TASK_ID"

# 待機中にもう一件予約して、その未開始予約だけを取り消す
QUEUED_ID=$(curl -fsS -H 'Content-Type: application/json' \
  -d '{"instruction":"前の回答を一文にしてください。"}' "$BASE/tasks" \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')
curl -i -X POST "$BASE/tasks/$QUEUED_ID/interrupt"
curl -sS "$BASE/tasks/$QUEUED_ID"
```

モデルへの指示だけで実行時間を保証することはできません。上の取消し前に GET で queued を確認してください。すでに開始していれば、その依頼の実行停止として扱われます。

最初の依頼が running になり、runId が取得できたら追加指示を送れます。以下は **受付**の確認であり、適用の確認ではありません。

```sh
curl -i -H 'Content-Type: application/json' \
  -d '{"instruction":"最後に「追加指示を確認しました」と添えてください。"}' \
  "$BASE/tasks/$TASK_ID/steer"

# 同じ依頼の steers[].commandId / status を繰り返し確認する
curl -fsS "$BASE/tasks/$TASK_ID" | python3 -c \
  'import sys,json; t=json.load(sys.stdin); print(t["state"]); print(json.dumps(t["steers"],ensure_ascii=False,indent=2))'
```

`status: applied` が適用確認です。accepted / delivered の間は待ってから同じ GET を繰り返してください。詳細ログを使わない場合は中間の適用確認を得られず、正常完了時に applied へ更新される場合もあります。失敗・停止後の unknown を applied と読み替えないでください。

実行中の依頼を停止する場合は次のとおりです。

```sh
# task ID はこの例の依頼、agentId + commandId は NOE の取消し対象
AGENT_ID=$(curl -fsS "$BASE/tasks" | python3 -c \
  'import sys,json; print(json.load(sys.stdin)["agent"]["agentId"])')
COMMAND_ID=$(curl -fsS "$BASE/tasks/$TASK_ID" | python3 -c \
  'import sys,json; print(json.load(sys.stdin).get("commandId") or "")')
printf 'agentId=%s commandId=%s\n' "$AGENT_ID" "$COMMAND_ID"
curl -i -X POST "$BASE/tasks/$TASK_ID/interrupt"
curl -sS "$BASE/tasks/$TASK_ID"

# task 停止後にも別の依頼を送れる（同じ agent / 会話を継続）
curl -i -H 'Content-Type: application/json' \
  -d '{"instruction":"ここまでの会話を一文でまとめてください。"}' "$BASE/tasks"

# 終了（テストや curl 用）。所有 agent の停止も行う
curl -i -X POST "$BASE/shutdown"
```

サーバーは実行中の依頼を `cancel_task({agentId, commandId})` で取り消します。agent instance と他の予約は残ります。キュー待ちの依頼はまだNOEへ渡していないため、サーバー内で取り消します。CLI の対応操作は `./noe interrupt --run <run-id>` ですが、通常はこの例では実行せず HTTP を使ってください。**管理対象 agent を外部の CLI から queue / interrupt して同時操作しないでください**。サーバーの順序制御・結果保存と競合します。

起動した端末の Ctrl+C（SIGINT）でも NOE の通常の所有リソース回収で終了します。`/shutdown` は HTTP ハンドラ内で終了を待たず、応答後に別途停止処理をスケジュールする経路です。

## 動作確認

```sh
python3 examples/agent-server/test_server.py -v

# 別のビルド済みバイナリを検証する場合
NOE_BIN=/path/to/noe python3 examples/agent-server/test_server.py -v

# 任意: Firefox を使った実ブラウザ検証（Linux / POSIX、Firefox が必要）
python3 examples/agent-server/test_browser.py
```

テストは既定でリポジトリの `./noe` を使い、Python 3 標準ライブラリのローカル SSE fixture でモデル応答を再現します。継承した `NOE_*` 接続設定を除去し、設定・鍵・runtime registry・ログ・作業ディレクトリを一時領域に隔離します。外部 provider の認証・モデル品質は検証しません。

HTTP統合テスト9件で、同時受付した12依頼すべての結果保存・会話継続、HTTP400失敗後の継続、steer の受付／実行中の適用／取消し、予約取消し、実行中停止後の後続処理、正常終了・SIGINT・起動失敗時の回収を確認します。停止テストは子agentと実OS子プロセスを起動し、PID消滅・空のregistryを確認します。`/shutdown` の正常終了コードは0、SIGINTはNOEの中断終了規約に従います。

任意のブラウザテストはFirefoxのMarionetteを直接使い、UIからの送信・一覧・回答、HTMLの非実行、追加指示の accepted → applied、停止、ポーリング中の入力保持を確認します。WebDriver等の追加Pythonパッケージは不要です。`FIREFOX_BIN` でFirefox実行ファイルを変更できます。

実装時には新規ビルドしたNOEで統合テスト9件、Firefox headlessテスト、既存の `go test ./...` の成功を確認しました。

## 安全性・制約

- **認証も利用者ごとの分離もありません。全クライアントが同じ一体の agent と同じ会話を共有**します。他の利用者の指示・回答が後続の依頼に影響します。trusted なローカル環境専用です。
- agent / tool は NOE と同じ OS 権限を持ち、任意のファイル操作・コマンド実行が可能です。サンドボックスではありません。ネットワークへ公開、ポート転送、外部公開用リバースプロキシの背後への配置はしないでください。
- CORS は提供せず、cross-origin リクエストは拒否します。これは認証の代替ではありません。同じホスト上の別プロセスを防御する仕組みではありません。UI は起動時 URL の同じ origin から利用してください。
- 予約・依頼・結果・追加指示はメモリ内だけです。永続化、再起動後の復元、ログからの依頼リロードはありません。ブラウザの再読込みは生きているサーバーから再取得するだけです。キューや結果保存の件数上限・保持期間・利用者別の予算制限もありません。長時間の無制限運用向けではありません。
- サーバー側で一件ずつ queue し、結果を保存してから次へ進むため、snapshot の lastResult が最新一件でも過去の結果を依頼 ID 別に保持できます。失敗・task 停止後も後続の依頼を処理します。
- 同じ会話を継続しますが、コンテキストの自動 trim により古い履歴が削除されます。既定の保持比率は 0.1。trim された履歴は次の依頼で復元されません。モデルのコンテキスト・出力上限もあり、会話の完全保持は保証しません。[hook / コンテキストガイド](../../docs/hooks.md)も参照してください。
- 詳細ログの読取りはこの小規模な例ではファイル全体を走査します。ログのローテーション・削除・他プロセスとの共有はせず、長時間運用ではログの読取り方式やメモリ保持上限を別途設計してください。
- task失敗では待受を維持しますが、instance自体の致命的停止時には自動再生成しません。保存済み結果は取得でき、新規依頼は503になります。サーバーを再起動するとメモリの依頼・会話は失われます。
- UI はスナップショットのポーリングであり、回答トークンのストリーミングではありません。詳細な進捗や正確な command 適用 / 取消し記録は、上記の同一プロセスのオプションログが必要です。停止要求は完了通知ではなく、回収中は stopping のままになることがあります。
- 指示・回答・tool 出力・進捗・JSONL ログには機密情報が含まれえます。ログを共有・公開しないでください。events ログは API のメモリ内データとは別にファイルとして残るため、適切な権限で管理し、不要になったら削除してください。
