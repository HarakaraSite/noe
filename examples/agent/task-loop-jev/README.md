# Jevをゲートに使うタスクループ

[agentの実行例へ戻る](../README.md)

[workflow.js](workflow.js)は、レビュー担当agentの自然文をJevで評価し、修正か次工程への進行かをJavaScriptで分岐します。作業担当にはkeepAliveのagentを使い、`queue_agent`で渡したタスクを`wait_task({agentId, commandId})`で待ちます。ポーリングは行いません。

工程は、計画レビュー → Jev判定 → 指摘があれば計画修正・再レビュー → 実装 → テスト・コードレビュー → Jev判定 → 必要なら修正・再テストです。終了時には作業担当のagentを停止します。

agentは`openai-chatgpt / gpt-6.1-sol / high`、Jevは`openrouter / typesafe/jev-1.14`を指定しています。[ChatGPT認証](../../../docs/openai.md)と[OpenRouterの資格情報](../../../docs/providers.md)を用意してください。Jevの呼出しにはOpenRouterの利用料が発生します。

## 実行

`wait_task`に対応したNOEを使います。同梱の[計画](plan.md)と[実装](solution.js)には、小数やInfinityまで加算する誤りを意図的に入れてあります。[要件](requirements.txt)と[テスト](test.js)を使って、レビューで検出し修正する流れを試します。

リポジトリのルートで次を実行してください。サンプル一式を一時ディレクトリへコピーし、その中の計画・実装を修正します。`noe`はPATH上の配置済みバイナリを使います。

```sh
task_loop_dir=$(mktemp -d)
cp examples/agent/task-loop-jev/{workflow.js,requirements.txt,plan.md,solution.js,test.js} "$task_loop_dir/"
(
  cd "$task_loop_dir" &&
  noe run --observe --events workflow.js
)
```

OpenRouter keyを既定以外のファイルから読む場合は、起動前に`NOE_OPENROUTER_API_KEY_FILE`を設定します。モデルやeffortを変更する場合は`workflow.js`の`base`、Jevのprovider/modelを変更する場合は`review`内の`decide`呼出しと`report.configuration.gate`を編集してください。

結果は作業ディレクトリの`report.json`、工程ごとの記録は`steps.jsonl`、親・子の詳細イベントは`.noe/events/*.jsonl`へ保存します。初回の`baseline.test`失敗は意図したものです。最終的には5テストがすべて通り、`report.outcome`が`completed`になります。モデルの判断次第で修正回数は変わり、再レビュー回数の上限は設けていません。Ctrl+Cで中断できます。

2026-10-20に実プロバイダで確認：計画修正1回、コード修正0回、5テスト通過、`wait_task`待機2回、ポーリング0回。Jevは計画を`revise`、修正後の計画と実装を`proceed`と判定しました。
