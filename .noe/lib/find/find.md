# ファイル名・ディレクトリ名の検索

`.noe/lib/find/find.js`はNOEから使える`find(path?, options?)`を提供します。
PATH上の`fd`を優先し、実行ファイルが見つからない場合だけGNU互換`find`へフォールバックします。
実行エラーや不正な検索条件では切り替えません。npm依存・シェル実行はなく、importだけではコマンドを起動しません。

```js
import {resolve} from "node:path";
const {find} = await import(resolve(".noe/lib/find/find.js"));
export default await find("examples", {name: "*import*", type: "directory"});
// results: ["import-map", "url-import"]（順序は不定）
```

`ls()`は構成を眺めるとき、`find()`は名前で対象を探すとき、
`grep()`はファイル本文を検索するときに使います。`find()`は本文を読みません。

## 引数

`path`はcwd基準の探索rootで、既定は`"."`です。ディレクトリを指定してください。
ファイルやsymlinkをrootに指定するとエラーになります。

| option | 既定 | 意味 |
| --- | --- | --- |
| `name` | `"*"` | ファイル名・ディレクトリ名全体に一致させるglob。正規表現や単なる部分文字列ではない |
| `type` | `"all"` | `all`・`file`・`directory`・`symlink`。fileは通常ファイルのみ |
| `ignoreCase` | `false` | 大文字・小文字を区別しない |
| `maxDepth` | 制限なし | 探索深さ。直下が1。正の整数 |
| `maxResults` | `300` | 返す項目数の上限。正の整数 |
| `maxBytes` | `32000` | 返却objectをJSON化した全体のUTF-8 bytes上限。正の整数 |
| `backend` | `"auto"` | 通常は指定不要。検証用に`"fd"`・`"find"`へ固定可能 |

`name`にはbasenameのglob（`*`・`?`・`[]`）を使います。
部分一致なら`"*import*"`、JSなら`"*.js"`、完全一致なら`"README.md"`です。
パスglob、`**`、brace展開、backslash、NUL、改行は受理しません。
階層を絞るには`path`・`maxDepth`を使います。
文字クラスや非ASCII文字の大小比較などの細部はbackend/locale依存です。
両backendで共通する単純なglobを推奨します。

```js
export default await find(".noe/lib", {name: "*.js", type: "file", maxDepth: 2});
```

## 結果と走査範囲

```json
{"backend":"fd","path":"/workspace/examples","results":["import-map","url-import"],"complete":true,"limitedBy":null,"warnings":[]}
```

- `path`はrootの絶対パス、`results`は**rootからの相対パス文字列**です。root自身は結果に含みません。順序は固定しません。
- **両backendとも隠しファイル・ignore対象を含みます。** fd本来の既定とは異なり、`--hidden --no-ignore`を指定します。
- 名前が`.git`の項目は結果から除外し、配下も走査しません。その他のignore fileは適用しません。大きなtreeではrootを絞ってください。
- symlink自体は検索できますが、その先は辿りません。`all`には通常ファイル・directory・symlink以外の種類も含みます。
- 一致なしは`results: [], complete: true`です。
- 上限を**超える一致**を見つけると`complete: false`、`limitedBy: "maxResults" | "maxBytes"`を返して子processを回収します。ちょうど件数上限までで検索が完了すれば`complete: true`です。
- パスは途中で切りません。上限に収まらなかった項目は含めず、総件数や継続offsetは返しません。未完了ならrootや条件を絞ってください。
- 完了状態のmetadata用にもbyte予算を予約します。metadataすら収まらない`maxBytes`はエラーです。複数の結果や本文をまとめて返す場合は、別途run_jsの出力予算に注意してください。
- stdoutはNUL区切りでstream処理するため、空白・改行を含むファイル名も扱えます。UTF-8としてdecodeします。
- 通常終了以外の終了code、起動失敗、stderrの診断はthrowします。fdには`--show-errors`を指定します。上限停止後に発生するはずだった走査エラーまでは確認できません。
- fallback時は`backend: "find"`で分かります。共通の走査方針なのでfallbackだけを理由とするwarningは付きません。

GNU find互換の`-mindepth`・`-maxdepth`・`-iname`・`-print0`等を利用するため、
最小機能のfindすべてを対象とするものではありません。`fdfind`名の自動探索は行いません。

検証：`./noe run .noe/lib/find/test.js`（fdとGNU互換findが必要）。
