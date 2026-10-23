# ZedでNOE用JavaScriptを編集・実行する

[ドキュメント一覧](README.md) · [JS検査](javascript-checks.md) · [agent起動](agents.md)

v2.3.0以降の[stdlib宣言](../stdlib/types/noestdlib.d.ts)と
[agent宣言](../agent/types/noeagentlib.d.ts)を、ZedのJavaScript projectから直接参照します。
このrepositoryの`.zed/`はGit管理外です。以下はローカル設定の作成・更新手順で、
既存のタスクやproject設定がある場合は必要な部分を取り込みます。

## 型宣言を設定する

repositoryルートをworkspaceとして開き、`.zed/jsconfig.json`へ次を設定します。
対象は`.zed/`内のJSです。別のディレクトリも編集する場合は`include`へ追加してください。

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "Bundler",
    "allowSyntheticDefaultImports": false,
    "lib": ["ES2022"],
    "allowJs": true,
    "checkJs": true,
    "noEmit": true,
    "strictNullChecks": true,
    "types": [],
    "skipLibCheck": false
  },
  "typeAcquisition": { "enable": false },
  "files": [
    "../stdlib/types/noestdlib.d.ts",
    "../agent/types/noeagentlib.d.ts"
  ],
  "include": ["./**/*.js"]
}
```

NOEのglobalとmoduleは宣言ファイルから得ます。`DOM`や`@types/node`全体で代用せず、
以前の`.zed/noe.d.ts`はこの設定から外して削除します。宣言を複製・再定義する互換層は不要です。
この設定は既存workspaceの`strictNullChecks`を維持しています。
さらに厳しい検査が必要なprojectでは`strict: true`を選び、JS側の診断にも対応してください。

JSDocで設定型を書く場合は、宣言が公開する名前を使います。
`AgentConfig`は`noe:agent`のmodule exportではなく`NOEAgent` namespaceの型です。

```js
import { run_agent } from "noe:agent";

/** @type {NOEAgent.AgentConfig} */
const config = {
  provider: "openai-chatgpt",
  model: "gpt-6.1-sol",
  effort: "high",
};
```

`allowDelegation`、`includeAnswer`、`read_task_result`等も正式宣言から参照できます。
型宣言の存在は、現在のProfileやWorkerでそのAPIが使える保証ではありません。
APIの条件は[内蔵docs](reference.md)で確認します。

## 指示ファイルを実行する

既存の`.zed/run-task.js`があれば継続して使えます。runnerのない新しいcheckoutでは、
repositoryルートで次のように[起動例](../examples/agent/try-agent.js)をコピーします。

```sh
mkdir -p .zed
cp examples/agent/try-agent.js .zed/run-task.js
```

コピーした`const config`の直前に上記の`/** @type {NOEAgent.AgentConfig} */`を付けます。
この例は1タスクで終了します。同じagentへ後からqueueを送る待受が必要なら、
末尾の実行・結果処理を次へ変更します。

```js
const result = await run_agent(config, task, { keepAlive: true });
if (result.reason !== "interrupted" && result.lastResult?.state !== "completed") {
  throw new Error(result.lastResult?.error ?? `agent ended with ${result.reason}`);
}
if (result.lastResult?.state === "completed") console.log(result.lastResult.answer);
```

待受はCtrl+Cか`noe interrupt --agent <agent-id>`で終了します。
別terminalからのqueue等は[agentの操作ガイド](api.md#実行中taskへの指示と停止)を参照してください。

`.zed/tasks.json`の作成例です。既存のタスク一覧があれば、この項目を追加します。

```json
[
  {
    "label": "NOE: GPT-6.1 Sol (ChatGPT, high)",
    "command": "./noe",
    "args": [
      "run", "--observe",
      ".zed/run-task.js",
      "--provider", "openai-chatgpt",
      "--model", "gpt-6.1-sol",
      "--effort", "high",
      "--task-file", "$ZED_FILE"
    ],
    "cwd": "$ZED_WORKTREE_ROOT",
    "save": "current",
    "reveal": "always",
    "hide": "never"
  }
]
```

先に[認証](agents.md)を設定し、repositoryルートに対応版の`./noe`を用意します。
指示を書くUTF-8ファイルを開いてタスクを選ぶと、そのファイルを保存してrunnerへ渡します。
JSファイルをそのまま実行するタスクとは異なります。
モデル・provider・effortは利用環境に合わせてタスクの引数で変更できます。

## 型検査と再利用

既存のTypeScriptがある環境では、repositoryルートで次を実行します。

```sh
tsc --project .zed/jsconfig.json --pretty false
./noe run .zed/run-task.js --help
```

前者は型検査、後者はrunnerのhelpだけを表示し、どちらもモデルへ要求を送りません。
Zedでの補完・診断表示はエディタ側のJavaScript/TypeScript環境にも依存します。
型検査の成功と、実providerへの接続・taskの成功は別々に確認してください。

別workspaceで使う場合は、対応するNOE版の2つの宣言ファイルとrunnerを配置し、
`files`の相対path、`include`、バイナリの`command`と作業ディレクトリを合わせます。
手元用に宣言内容を再編集する代わりに、NOE更新時に同じ版の宣言へ揃えます。
