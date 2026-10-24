# リポジトリの見取り図

`repo-map.js`の`repoMap(options?)`は、ディレクトリ・ファイルの配置と配下のファイル数を返します。ファイル内容を読まず、特定の言語・framework・リポジトリ構成に依存しません。実行環境はNOEです。内容検索の`search.js`とは独立したライブラリです。

```js
import {resolve} from "node:path";
const {repoMap} = await import(resolve(".noe/lib/repo-map/repo-map.js"));
export default await repoMap();
```

最初に浅い階層を見てから、必要なディレクトリを指定して展開します。

```js
export default await repoMap({path: "src", depth: 3});
```

| 引数 | 既定 | 意味 |
| --- | --- | --- |
| `path` | `"."` | 探索のrootディレクトリ。cwd基準 |
| `depth` | `2` | 表示する階層。rootは0、その直下は1。0ならrootの集計のみ |
| `maxEntries` | `120` | 表示するファイル・ディレクトリの合計上限。rootを除く |
| `maxBytes` | `8000` | metadataを含む返却JSON全体のUTF-8 bytes上限 |
| `maxFiles` | `50000` | 集計するファイル数の上限。超えたら走査停止 |
| `backend` | `"auto"` | 通常は自動。`"git"`または`"fs"`へ固定も可能 |

返却値は`{root, backend, files, directories, scanComplete, complete, limitedBy, tree, warnings}`です。`tree`は例えば次の形です。

```text
. (19 files)
├─ src/ (12 files; collapsed)
├─ tests/ (6 files; collapsed)
└─ README.md
```

- `files`と各ディレクトリのファイル数は、表示した行数ではなく、そのbackendで集計した配下全体の件数です。`directories`はrootを除く集計済みディレクトリ数です。
- `depth`による折り畳みは`collapsed`と表示します。指定した深さを全て返せた場合は`complete: true`です。
- 表示候補は浅い階層から選びます。ひとつの大きな枝を表示している間に、他の上位ディレクトリが消えることを避けます。ディレクトリを先に、その中では名前順に表示します。
- `maxEntries`・`maxBytes`で省略すると`complete: false`、`limitedBy`配列に該当上限を載せます。各枝の`+N entries`は、集計済みの直接の子のうち表示していない数です。完全なJSONを返します。
- `maxFiles`で走査を止めた場合は`scanComplete: false`でもあり、ファイル数は下限です。treeでも`N+ files`と表示します。複数上限へ達した場合は全て`limitedBy`へ載せます。
- `maxBytes`にrootのmetadataすら収まらなければエラーにします。複数のmapを一度に返す場合は、合計をrun_jsの出力予算内に調整してください。
- ファイル名中の改行等はescapeしてtreeの行へ混ぜません。symlinkは`@`を付け、配下を辿りません。Gitのsubmoduleは`gitlink`の境界として表示し、内部は集計しません。

## 対象ファイルとフォールバック

Git管理下では`git ls-files`から、追跡済みファイルとignoreされていない未追跡ファイルを集計します。隠しファイルも対象です。追跡済みであっても作業treeから削除されたファイルは含めません。対象`path`より外側を表示しません。Gitで管理されない空ディレクトリは表示しません。

Gitがインストールされていない、または対象がGitリポジトリ内でない場合、通常のfilesystem走査へ切り替えます。この場合はignore fileを読まず、`.git`ディレクトリだけを除外します。差は`backend: "fs"`と`warnings`で明示します。filesystem走査では空ディレクトリも表示します。それ以外のコマンドエラーや読取りエラーはthrowします。

使い方：見取り図で場所を把握 → `path`を指定して展開 → `search.js`の`grep()`で内容を検索、の順で絞れます。

検証：`noe run .noe/lib/repo-map/test.js`。通常走査はGitなしでも動きますが、検証にはGitが必要です。
