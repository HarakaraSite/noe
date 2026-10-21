# JavaScriptの静的検査と整形

この環境ではBiome 2.5.15をユーザー共通の`~/.local/bin/biome`へ導入済みで、`biome`で呼べます。Node.jsやnpmは不要です。このworkspaceの設定はルートの`biome.json`にあります。

保存するJSを作成・修正したときは、対象ファイルを検査してください。

```sh
biome lint task.js
```

設定は構文エラー、未定義変数、constへの再代入、到達不能コード、重複したキー・引数・宣言などの検出に絞っています。`serve`などのNOE固有グローバルも登録しています。

長いrun_jsなど、実行前に検査したいスニペットは一時ディレクトリの`run-js-snippet.js`へ保存し、workspaceの作業ディレクトリから`biome lint /その一時ディレクトリ/run-js-snippet.js`で検査できます。このファイル名に限り、run_jsの事前束縛（run_agent、wait_taskなど）も認識します。通常のtask.jsではagent APIを`noe:agent`からimportしてください。短いファイル読込みや検査用の呼出し自体を再帰的に検査する必要はありません。

run_jsからは、`node:child_process`の`execFile("biome", ["lint", path])`で実行できます。非ゼロ終了時の例外に含まれる`stdout`・`stderr`を確認して修正してください。

整形したい場合だけ、対象ファイルへ`biome format --write task.js`を使えます。検査には`lint`を使い、整形の差だけを理由に修正範囲を広げないでください。

TypeScript 5.9.3もユーザー共通の`~/.local/bin/tsc`へ導入済みです。Zedが使うTypeScriptと同じバージョンに揃えています。`tsc`はNode.jsで実行するCLIで、NOEのJS自体をTypeScriptへ書き換える必要はありません。

NOEの型定義は`.zed/noe.d.ts`、共有する型検査設定は`.zed/jsconfig.json`です。workspaceのルートから次を実行すると、`.zed/`内のJSについてAPI名・引数・戻り値の型を検査します。JSは実行せず、生成ファイルも作りません。

```sh
tsc --noEmit --project .zed/jsconfig.json
```

この設定の対象は`.zed/`内だけです。他の場所のJSを検査する場合は、`.zed/jsconfig.json`をextendsし、`files`へ対象JSと`.zed/noe.d.ts`の絶対パス、`include: []`を指定した一時設定を作って`tsc --noEmit --project /その一時設定.json`で検査できます。元の設定のincludeが相対パスのまま継承されるため、対象を限定する一時設定ではincludeも明示的に上書きしてください。run_jsの事前束縛はこの型定義のグローバルには含めていません。

run_jsからは`execFile("tsc", ["--noEmit", "--project", ".zed/jsconfig.json"])`で呼べます。非ゼロ終了時は例外の`stdout`・`stderr`の両方を確認してください。Biomeのlintとtscの型検査は別の検査です。

型定義はNOE固有の公開11モジュールとfs/path/process/child_processの主要な標準APIを対象とし、すべてのstdlibモジュールを宣言しているわけではありません。型定義がないモジュールの診断だけで「NOEでは未対応」と判断せず、`noe docs`または`noe:docs`でAPI契約を確認し、必要な動作確認も行ってください。Web APIにはTypeScript標準のDOM型を利用しているため、補完されるすべてのブラウザAPIがNOEで動くことも保証しません。
