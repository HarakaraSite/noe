# 常駐agentのローカルWebコンソール

[実行例一覧](../README.md) · [設計と完成条件](../../docs/development/agent-web-console-plan.md) · [agentの観察](../../docs/observe.md)

1プロセスに1つのkeepAlive agentを所有し、ブラウザからQueue・Steer・Interrupt・全停止を送るサンプルです。agent単位の同じタイムラインに、定期observeのsnapshot、操作受付、タスク結果を表示します。タスク一覧やJSONの要約・展開切り替えはありません。

## 起動

リポジトリのルートから、ビルド済みのNOEを使います。npm・フレームワーク・ビルド工程は不要です。

~~~sh
# 利用するproviderの既存NOE認証設定を用意する
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent-console/main.js
# NOE Agent Console: http://127.0.0.1:8787
# History: /path/to/noe/examples/agent-console/data/sessions
~~~

表示されたURLをブラウザで開いてください。起動時はidleで、Queueを送るまでモデルを呼びません。ターミナルのCtrl+Cでサーバーと所有agentを終了します。ブラウザを閉じただけでは、実行・観察・保存は終了しません。

~~~sh
# provider・model・effortを変更。port 0なら空いているポートを自動選択
./noe run examples/agent-console/main.js \
  --provider openai-chatgpt --model gpt-6.1-sol --effort high \
  --port 0 --data-dir /path/to/private/console-sessions
~~~

| オプション | 既定値・意味 |
| --- | --- |
| `--provider` | `openrouter` |
| `--model` | `deepseek/deepseek-v4.1-flash` |
| `--effort` | `auto` |
| `--port` | `8787`。0はOSが選ぶポート |
| `--data-dir` | このサンプル配下の `data/sessions`。指定した相対パスは起動時cwdが基準 |
| `--help` | 起動オプションを表示 |

認証情報は[NOEの既存provider設定](../../docs/providers.md)で管理します。ChatGPTの場合は[認証ガイド](../../docs/openai.md)を参照してください。ブラウザへ資格情報は配信しません。選択モデルの推論には利用料・利用枠が適用されます。

## 操作と履歴の読み方

- **Queue**：次の依頼です。同じagentが順番に実行し、前の依頼の確定済み・trim後の会話を引き継ぎます。独立したagentを作る操作ではありません。
- **Steer**：画面が示したrunへの追加指示です。現在の処理を中断せず、モデルの判断境界で適用されます。「受付」は適用済みを意味しません。
- **Interrupt**：画面が示したcommandのタスクだけを中断します。agentと後続のQueueは維持します。一時停止・再開やOS操作の巻き戻しではありません。
- **全停止**：agent・実行中タスク・残りのQueueを停止します。停止完了は最新の状態・結果で確認します。HTTPサーバーと履歴閲覧は残ります。新しいagentはサンプルを再起動して作ります。

Steer・Interruptの対象IDはクリック時に固定し、タスクの切替と競合しても後続の依頼へ自動変更しません。実行対象がないときは利用できません。受付やエラーにも元の指示・対象を表示します。

ヘッダーは最新のagent状態です。末尾では自動追従し、過去へスクロールすると追従を止め、新着件数と「末尾へ」を表示します。保持量は最大1,000レコードで、前後の履歴を読み込む際に表示範囲を入れ替えます。画面から外れた記録も保存ファイルには残ります。レコード数はブラウザの厳密なメモリ上限ではありません。

「古い履歴」「新しい履歴」の手動操作では取得したページを保持します。それによって読んでいたレコードが表示範囲から外れる場合は、取得したページへ表示位置を移します。自動読み込みでは読んでいる位置を保持します。

セッション選択で以前の履歴を読み取り専用で閲覧できます。以前のsnapshotの `running` は観測時点の値で、現在稼働中という意味ではありません。以前のagent・会話・未処理Queueは復元しません。受付は記録されていても結果が残っていない依頼は「セッション終了時点で結果未記録」とし、成功・失敗・キャンセルを推測しません。

## 保存と制約

- observeと画面の更新は約1秒間隔です。同じsnapshotは重複保存しません。snapshotは差分や独自要約に置き換えず、そのまま整形JSONで表示します。
- `observe_agent` と `wait_task` は `includeAnswer: false` で取得します。各commandの結果は個別に待ち、`read_task_result` を `eof: true` まで読み、回答全文を別の結果レコードへ保存します。
- 定期observeはサンプリングです。全ツール呼出し、生成中の全文、thinkingを網羅する実行イベントビューアではありません。
- セッションごとの `metadata.json` と `history.jsonl`、および再構築可能な `offsets.idx`・`results/` を保存します。固定長のbyte offset索引で必要な範囲だけを読み、サンプル側で全履歴をメモリへ保持しません。過去セッションの初回閲覧時はJSONLを逐次走査して索引を再構築します。
- 新しいセッションは初期化完了後に一覧へ公開します。初期化中に終了した場合の `.initializing-*` は保存先に残ります。以前の不完全なセッションなどでmetadataを読めない場合も、正常なセッションは閲覧できます。一覧の該当項目は「読み込み不可」となり、エラーはその項目のツールチップと `/api/sessions` の `error` に残します。元データは削除・書き換えません。
- JSONLは自動削除・ローテーション・本文切り詰めを行いません。ディスク空き容量を利用者側で管理してください。NOE自体はタスク結果をホストの生存中保持するため、このサンプルの範囲取得はプロセス全体のメモリ使用量を一定にする保証ではありません。
- Ctrl+Cなどによるサーバー終了時、NOEによるagentの回収とJSによる結果の取得・保存は別です。すべての最終結果が保存される保証はありません。過去履歴の不完全な末尾行は索引から除外し、元のJSONLは書き換えません。

## セキュリティとデータの扱い

**NOEはsandboxではありません。agentと生成JavaScriptは起動ユーザーと同じOS権限で動きます。** 指示内容・モデル出力・変更されたファイルを確認できる環境で使用してください。

- 待受は `127.0.0.1` のみです。外部公開、リバースプロキシ経由、リモート接続、複数ユーザー認証は対象外です。
- 許可するHost・Originは実際の待受URLから固定します。NOEの `serve` が `Request.url` に投影する要求のauthorityを固定値と照合し、静的ファイル・履歴も含めて不一致を拒否します。要求自身から許可値を作りません。
- 操作はJSONのPOSTのみです。異なるOrigin・`null`・Origin欠落を拒否します。CORS設定やloopbackへのbindだけを防御にしません。URLは表示された `http://127.0.0.1:PORT` のまま使用してください。
- 指示・回答・エラーは実行可能なHTMLとして挿入せず、テキストで表示します。
- 履歴には機密情報が含まれ得ます。保存先を適切に管理してください。既定の `data/` はこのフォルダの `.gitignore` で除外します。独自の保存先は利用者側でバージョン管理や共有から除外してください。ログの暗号化・認証付き配信は行いません。

## 構成とHTTP API

`main.js` は起動設定、`server.js` はHTTP、`controller.js` はagent操作と結果回収、`store.js` は保存と範囲取得、`public/` は画面です。

| API | 内容 |
| --- | --- |
| GET /api/state | 最新snapshot、現在のsession ID、モデル、更新間隔・画面保持量、サーバー側エラー |
| GET /api/sessions | 現在・過去セッションのメタデータ |
| GET /api/history?session=ID&limit=N | 最新のN件。Nは1〜1,000、既定100 |
| GET /api/history?session=ID&before=S&limit=N | Sより前のN件 |
| GET /api/history?session=ID&after=S&limit=N | Sより後のN件 |
| POST /api/queue | `{sessionId, instruction, commandId?}` |
| POST /api/steer | `{sessionId, instruction, runId, commandId?}` |
| POST /api/interrupt | `{sessionId, commandId}` |
| POST /api/stop | `{sessionId}` |

`before` と `after` は同時指定しません。履歴は `{sessionId, records, firstSeq, lastSeq, total}` を返し、recordsは昇順です。`total` はセッションの最終レコード番号です。再接続はsession IDとレコード番号で差分取得できます。command IDは冪等な再送にも使います。過去session IDでの操作は拒否します。これはサンプル内のAPIであり、NOE本体の公開APIを追加するものではありません。

## テスト

~~~sh
# ディスク範囲取得・再起動時の索引再構築・回答全文回収・snapshot重複除去
./noe run examples/agent-console/tests/backend.js

# 実CLI＋ローカルprovider fixture。外部サービスへ接続しない
# 上記JSテストもこのE2Eから実行する
go test -tags=e2e ./tests -run '^TestAgentConsole' -count=1

biome lint examples/agent-console
~~~

開発用の画面検査はNode.jsと既存のPlaywright・Chromiumがある環境で任意に実行できます。これらはコンソールの起動・利用には不要で、このサンプルに依存パッケージを同梱しません。

~~~sh
node examples/agent-console/tests/ui-helpers.mjs
# インストール済みPlaywrightのESM入口を指定。通常のimport解決が可能なら環境変数は省略可
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
  node examples/agent-console/tests/ui-browser.mjs
# ブラウザから実NOEサンプルを経由し、ローカルproviderへQueueするsmoke test
PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs \
  node examples/agent-console/tests/ui-cli.mjs
~~~

画面の確認方法と実行結果は[実装・検証記録](../../docs/development/agent-web-console-implementation.md)を参照してください。
