# agentの不変インストラクション

agentの各task run開始時に、次の順で指示を結合します。

1. NOE組込みの実行環境・toolの説明。
2. 共通ディレクトリの`*.md`。
3. workspaceディレクトリの`*.md`。
4. 呼出し時に明示した`config.system`。

本文を追記する方式です。同名ファイルでも両方を読み、明示した`system`で自動読込み分を置き換えません。

## 探索場所

| 区分 | ディレクトリ |
| --- | --- |
| 共通 | `$XDG_CONFIG_HOME/noe/instructions/`。変数が空・未設定なら`~/.config/noe/instructions/` |
| workspace | `<NOE起動時の作業ディレクトリ>/.noe/instructions/` |

共通側の設定ディレクトリは、既存のクレデンシャルと同じ規則で解決します。
workspaceはscriptの場所や一時生成されたagent JSの場所ではなく、ホストNOEの作業ディレクトリです。
Git rootや祖先ディレクトリの探索は行いません。子agentも同じworkspaceを使います。

各ディレクトリの直下にある、小文字の拡張子`.md`のファイルをファイル名の昇順で読みます。
文字列順なので、順序を明示したい場合は`10-tools.md`、`20-project.md`のように名前を付けます。
サブディレクトリは再帰しません。通常ファイルを指すシンボリックリンクも読めます。
ディレクトリが存在しない場合は追加指示なしとして進みます。既存の読込み対象や
探索ディレクトリを読めない場合は、対象パスを含むtask失敗として報告し、モデルは呼びません。

## ライブラリの利用案内

例えば、共通の`10-exa.md`へ次のように書けます。

```md
Web検索には、run_jsから利用できるExaライブラリがあります。
使う前に /home/agent/.config/noe/lib/exa.md を読んでください。
import {search} from "exa" で利用できます。
```

import mapは別に起動時の`--import-map`で指定します。Markdownを置くだけで
ライブラリのインストールやimport mapの自動探索が行われるわけではありません。

## 任意のファイルもsystemへ追加する

`system`は文字列です。複数ファイルは呼出し元のJSで指定順に結合できます。

```js
import {readFile} from "node:fs/promises";
import {run_agent} from "noe:agent";

const files = ["./AGENTS.md", "./rules.md"];
const system = (
  await Promise.all(files.map(path => readFile(path, "utf8")))
).join("\n\n");

const result = await run_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
  system,
}, "README.mdを3点に要約してください。");
console.log(result.lastResult);
```

`AGENTS.md`自体の自動探索は行いません。ファイルパスの配列を`system`へ直接渡すこともできません。

## 読込みのタイミング

自動読込みは各runで一度だけ、hookとモデル要求より前に行います。
そのrunの途中でファイルを書き換えても、保持する指示と後続モデル要求には反映しません。
moduleのimportやtaskなしのidle起動では読みません。

子agentの新しいrunや、keepAlive instanceの次のqueue taskではその時点のファイルを読みます。
親の自動読込み済み本文を子へ重ねて継承することはありません。
明示した`system`はinstance設定なので、同じinstanceの各taskに保持されます。
子の`system`はこれまでどおり明示した場合だけ適用され、親から自動継承しません。

結合した本文は不変のsystemメッセージとして保持します。動的な予算・使用量通知は
[hook・contextガイド](hooks.md)の要求限定情報として別に扱い、元の指示へ蓄積しません。
自動管理時は、予算に達すると古い完了済みの実行履歴がまとめて切り捨てられる場合があります。
system、元のtask指示、適用済みsteerは保持します。直近の完了blockも残ります。
それより古いblockは削除対象となり、削除時もassistantのtool callと対応する全tool結果を一体として扱います。
必要な情報が後でも役立つ場合だけ、通常のtask作業として既知のファイルへ保存できます。保存は任意で、
保存しなくても処理は続きます。設定と予算の意味は[hook・contextガイド](hooks.md)を参照してください。
