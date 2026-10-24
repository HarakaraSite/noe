# インストラクション自動読込みの実装・検証記録

2026-10-18。要件は[検討メモ](../completed-features.md#user-instructions)、公開仕様は[ガイド](../instructions.md)。
状態：実装・テスト・独立コードレビューを完了。

## 実装

- Go hostでrun開始時に共通設定とworkspaceのMarkdownを読み、既存のstart応答へ本文のsnapshotを追加する。
- JS task loopは組込み指示→snapshot→明示した`system`を一つのsystemメッセージへ結合する。
- ファイルI/OをHostのlock外で行い、hook・モデル要求より前にsnapshotを固定する。
- 読込み失敗でも認証済みrun IDを確定し、既存のfinish経路でtask失敗を報告する。初期taskのないidle instanceでは読み込まない。
- 子や次のqueue taskはそれぞれ新しいsnapshotを取り、親の本文や過去taskの本文を重複させない。

workspaceはNOEの起動時の作業ディレクトリを採用した。Git root・祖先・`AGENTS.md`の自動探索は追加しない。
共通側はクレデンシャルと同じXDG/HOME規則で、明示したXDG側にdirectoryがなくてもHOME側へ重ねてfallbackしない。
直下の小文字`.md`だけを名前順で読む。同名を置換せず、空ファイル・directory・特殊ファイルは追加本文なし。
通常ファイルへのsymlinkは読めるが、選択した`.md`の壊れたリンクや読取りエラーはパス付きの失敗になる。
独自上限・無効化option・新しい公開APIは追加しない。

## 検証

`internal/agent/instructions_test.go`は順序・同名・XDG優先・HOME fallback・不存在・非再帰・symlinkとパス診断を確認する。
`internal/runtime/agent_instructions_test.go`は、実際のtask/run_js/子processとfixture providerで次を照合する。

- 共通→workspace→複数ファイル由来の明示`system`、組込み本文の保持。
- toolでファイルを変更しても親の後続要求は同一、新しい子は変更後の内容を一度だけ取得。
- 不正directoryでもimport/idle spawnは可能、taskではproviderを呼ばず失敗する。
- 同じkeepAlive instanceを修復し、次taskで再読込み、その次taskも更新を取得。

`tests/agent_instructions_e2e_test.go`は実CLIのrun/eval/replを起動し、送信されたsystem本文、
同名ファイルの追記、scriptディレクトリではなくCLIのcwdを使うことを確認する。
新しいruntime/CLIテストとagent/runtimeの関連hook回帰はrace付きでも成功した。
初回の結合fixtureではExecuteのfs相対パスとREPLの複数await入力を調整し、変更後に全新規ケースを再実行して成功した。

検証ログ：`/tmp/noe-instructions-validation`。既存の`./noe`は置換しない。
新しい課金API要求や実資格情報の読込みは行わず、providerはローカルfixtureを使う。

最終確認：

```sh
go test ./...
NOE_MCP_SDK_BUNDLE=/tmp/noe-process-mcp.wzVT64/mcp-client.bundle.mjs go test -p 1 -tags=e2e ./...
go vet -tags=e2e ./...
GORACE=atexit_sleep_ms=0 go test -race ./internal/agent ./internal/runtime -run 'TestLoadUserInstructions|TestAgentInstructions|TestAgentMinimalRun|TestHookModule|TestHooks' -count=1 -timeout=120s
GORACE=atexit_sleep_ms=0 go test -race -tags=e2e ./tests -run '^TestAgentInstructionFilesRunEvalAndREPL$' -count=1 -timeout=90s
```

結果は`unit.log`、`e2e.log`、`vet.log`、`race.log`、`e2e-race.log`に保存。

## レビュー

実装担当と別のreviewerが、探索・結合・runの固定・子/queue・失敗の終了経路を確認し、必須修正指摘はなかった。
親の途中確認では、選択済み`.md`のstat失敗を黙って省略していた処理を修正した。
壊れたsymlinkの回帰を追加し、通常ファイルへのsymlinkと不存在directoryの扱いを維持した。
