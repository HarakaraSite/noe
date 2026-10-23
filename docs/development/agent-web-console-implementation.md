# 常駐agentのローカルWebコンソール：実装・検証記録

[開発記録](README.md) · [設計・完成条件](agent-web-console-plan.md) · [利用方法](../../examples/agent-console/README.md)

## 実装

2026-10-23、計画の初版スコープに沿って `examples/agent-console/` を追加した。NOE本体の公開APIや既存の `examples/agent-server/` は変更していない。

| ファイル | 責務 |
| --- | --- |
| main.js | provider・model・effort、port、保存先の起動設定。実際のローカルURLを表示 |
| server.js | noe:httpのserve、静的ファイル、状態・履歴・操作API、固定したHost・Originの検証 |
| controller.js | idleのkeepAlive agent、約1秒のobserve、snapshot重複除去、対象固定の操作、各commandの結果全文回収 |
| store.js | セッションのJSONL追記、固定長byte offset索引による範囲取得、過去セッション初回閲覧時の索引再構築 |
| public/ | agent中心の履歴、最新ヘッダー、操作欄、過去セッション閲覧、表示範囲の入れ替え |
| tests/backend.js | 保存・範囲取得・再起動・snapshot・回答回収の局所テスト |
| tests/ui-helpers.mjs・ui-browser.mjs・ui-cli.mjs | 画面の純粋関数、実ブラウザの表示・スクロール、実NOE経由のブラウザ操作 |
| ../../tests/agent_console_e2e_test.go | 実CLIとローカルprovider fixtureによる統合テスト |

### 状態・結果・保存

- spawn時は初期指示なし・keepAliveでidle。ブラウザを監視主体にせず、サーバーがagentと監視処理を所有する。
- observeとwaitはincludeAnswer:false。各commandを個別にwaitし、read_task_resultのnextOffsetをeofまでたどり、回答を原文のまま結果レコードに保存する。lastResultの上書きで回収先を決めない。
- 操作受付と適用・完了を区別する。SteerとInterruptはクリック時に取得したrun/commandを送り、切替競合でも新しい対象へ変更しない。
- 履歴はsession IDと単調増加seqで識別。JSONLは自動削除・本文切り詰めをしない。保存ファイルを範囲取得し、画面の上限と保存量を分ける。
- 過去sessionのJSONLは書き換えず、完全な行だけで索引を再構築する。結果のない受付は「セッション終了時点で結果未記録」と表示する。旧agent・会話・Queueは復元しない。
- NOE自身によるタスク結果のホスト生存中保持は別であり、プロセス全体のメモリを一定に保つ保証ではない。

### HTTPの境界

待受は127.0.0.1。許可するauthority・Originはserver.urlから固定し、要求から導出しない。NOEのserveではnet/httpのHostは通常のヘッダーとして配信されずRequest.urlに投影されるため、そのauthorityを固定したURLと照合する。全HTTP要求のauthority、および操作POSTのOriginを検証する。Originの異値・null・欠落を拒否し、CORSのみの防御にしない。表示には実行可能なHTMLではなくテキストを使う。

## 検証

Linux amd64 / Go 1.27.2で確認。画面は既存のPlaywrightとChromium 151を利用し、サンプルへnpm依存やブラウザ実行環境を追加していない。モデル応答はローカルSSE provider fixtureを使用した。

| 確認 | 結果・根拠 |
| --- | --- |
| JSバックエンドの局所確認 | tests/backend.jsが成功。1,108レコードの範囲取得・保存非削除、破損した再構築可能な索引と不完全末尾行からの閲覧、過去の未回収結果、snapshot重複除去、nextOffset/eofによる回答全文回収 |
| 実CLIの統合確認 | TestAgentConsoleの対象テストが成功。idle時にproviderを呼ばない、会話継続と高速な後続結果の対応付け、Steer、Interruptで後続Queueを維持、全停止、古い対象への操作競合、切断中の記録、再起動後の閲覧・非再実行、Host/Origin拒否 |
| UIの純粋関数 | ui-helpers.mjsが成功。最大1,000レコード、重複除去、anchor保持、操作可否、対象IDの固定 |
| 実ブラウザの画面確認 | ui-browser.mjsが成功。snapshot全文と原文の安全表示、長い回答、操作と入力保持、過去sessionの閲覧専用・未回収表示、2,200件超の前後pagingとDOM上限、スクロール位置保持、新着件数と末尾追従、再接続・再表示、狭い画面、CSP |
| ブラウザ→実NOE→ローカルprovider | ui-cli.mjsが成功。実main.jsの起動・静的ESM/CSS配信、実操作POSTのOrigin検証、12,014-byteのUnicode回答全文、指示・回答のHTML非実行、ブラウザ再表示時のsession/履歴保持、全停止後のstopped表示・操作無効化 |

### 実行コマンド

~~~sh
./noe run examples/agent-console/tests/backend.js
go test -tags=e2e ./tests -run '^TestAgentConsole' -count=1
node examples/agent-console/tests/ui-helpers.mjs
PLAYWRIGHT_MODULE=/path/to/installed/playwright/index.mjs node examples/agent-console/tests/ui-browser.mjs
PLAYWRIGHT_MODULE=/path/to/installed/playwright/index.mjs node examples/agent-console/tests/ui-cli.mjs
biome lint examples/agent-console
~~~

ui-cli.mjsはNOE_BINで検査するバイナリを変更できる。テスト用のkey・runtime・作業ディレクトリ・保存先は一時領域を使う。実providerの課金を伴うサンプル実行、長期連続稼働、OSサービス化、外部公開は検証対象外である。

## 独立コードレビューと全体検査

独立レビューでfailed instanceの操作ボタンが有効になるLow/P3を1件確認。元の画面実装担当へ修正を返し、terminal failedでの操作無効化と局所テストを追加した。同じreviewerによる限定再レビューで修正を確認し、未解決の指摘はない。追加した実NOE経由のブラウザsmoke testもコードレビューした。

レビュー修正が落ち着いた候補に対して、親が次の全体検査を1回実行した。すべて成功した。

| コマンド・確認 | 結果 |
| --- | --- |
| go test -tags=e2e ./... -count=1 | 全package成功。全体62.29秒、tests package55.607秒。局所JSテストと実CLI統合も含む |
| go vet -tags=e2e ./... | 成功 |
| biome lint examples/agent-console | 12ファイルを検査、成功 |
| バックエンドと局所JSテストのtsc --noEmit | 成功。workspaceの.zed/jsconfig.jsonをextendsする一時設定を使用。既存ローカル型定義にないnode:cryptoと新しいagent操作APIの宣言を、現行の内蔵docsに合わせて一時補完した |
| 差分・空白確認 | tracked差分と追加17ファイルに空白エラーなし |

全体検査のログは実施環境の /tmp/noe-console-final-3037468492/ に保存した。検証のためにNOE本体のコード・公開APIや依存関係を変更していない。

## 完了と残る利用条件

### 追加レビューの修正（2026-10-23）

- P2: 起動時のディレクトリ作成とmetadata保存の間で終了すると、不完全な1セッションが過去セッション一覧全体を失敗させた。初期化は `.initializing-*` 内で行い、完了後にrenameで公開する。既存のmetadata欠落・不正は項目別の「読み込み不可」として報告し、正常な履歴の選択・読み込みを維持する。backend.jsで欠落・不正metadataと未公開の初期化ディレクトリを含む回帰確認を追加した。
- P3: 表示上限1,000件の反対側の端を読んでいるとき、手動ページ取得の全件がanchor優先で破棄された。手動操作は取得方向を優先し、元のanchorが残れば位置を維持し、外れた場合だけ取得ページへ移動する。自動スクロールのanchor保持は従来どおり。ui-browser.mjsで先頭からの「新しい履歴」と末尾からの「古い履歴」の両方を上限到達状態で確認した。

修正後、`go test -tags=e2e ./tests -run '^TestAgentConsole' -count=1`（13.819秒）、UI純粋関数、Chromium 151のui-browser.mjs、`biome lint examples/agent-console`、差分の空白確認が成功した。実プロバイダへの要求は行っていない。

計画の完成条件に沿うサンプル実装・局所検査・実CLI統合・実ブラウザ・独立コードレビューを完了した。実providerでのモデル回答の質・認証状態や長期連続稼働は、この結果から保証しない。常駐は起動したターミナルのNOEプロセスが動いている間に限る。終了時の結果未記録、保存量のディスク依存、起動ユーザーのOS権限で実行される点はREADMEに記載したとおり。
