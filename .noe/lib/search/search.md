# ローカルファイルの内容検索

`.noe/lib/search/search.js`はNOEから使える`grep(options)`を提供します。追加のnpm依存はなく、PATH上の`rg`を使い、実行ファイルが見つからない場合だけGNU互換`grep`へフォールバックします。検索条件や実行のエラーでは切り替えません。import自体はコマンドを起動しません。

```js
import {resolve} from "node:path";
const {grep} = await import(resolve(".noe/lib/search/search.js"));
export default await grep({
  pattern: "outputLimit",
  paths: ["internal/agent"],
  glob: ["*.go", "!*_test.go"],
  mode: "lines"
});
```

| 引数 | 既定 | 意味 |
| --- | --- | --- |
| `pattern` | 必須 | 検索文字列。改行・NULは不可。空文字列は全行に一致 |
| `paths` | `["."]` | ファイルまたはディレクトリの配列。cwd基準 |
| `mode` | `"lines"` | `lines`: 一致行、`files`: 一致するファイル名、`count`: ファイルごとの一致行数 |
| `regex` | `true` | 既定は正規表現。`foo|bar`で複数候補を検索。文字列一致は`false`を指定 |
| `ignoreCase` | `false` | 大文字・小文字を区別しない |
| `glob` | `[]` | ファイルの絞込み。例`["*.js", "!*.test.js"]`。rgのglob構文 |
| `maxResults` | `50` | 返す行数またはファイル数の上限。正の整数 |
| `maxBytes` | `8000` | **返却objectをJSON化した全体**のUTF-8 bytes上限。正の整数。metadataすら収まらなければエラー |
| `backend` | `"auto"` | 通常は指定不要。検証等で`"rg"`、`"grep"`へ固定可能 |

結果例：

```json
{"backend":"rg","mode":"lines","results":[{"path":"src/main.js","line":12,"text":"const answer = 42;"}],"complete":true,"limitedBy":null,"warnings":[]}
```

- `lines`の各要素は`{path,line,text}`。lineは1始まり。行末のLFだけを除きます。同じ行に複数一致しても1件です。
- `files`の各要素はpath文字列。**内容に一致があるファイル**の一覧で、ファイル名そのものの検索ではありません。名前で探す場合は[find()](../find/find.md)を使います。
- `count`の各要素は`{path,count}`。countは一致**行**数で、一致箇所の数ではありません。0件のファイルは含めません。
- 一致なしは`results: []`, `complete: true`です。終了コード1を例外にしません。
- `pattern: "foo|bar"`は既定で正規表現のORです。ドットや括弧等をそのまま探す場合は`regex: false`を指定してください（例：`{pattern: "a.b", regex: false}`）。不正な正規表現はthrowし、文字列一致へ自動変更しません。
- 上限を超える結果があれば`complete: false`、`limitedBy: "maxResults" | "maxBytes"`を返して検索を停止します。総件数は未確定です。ちょうど件数上限までで検索が完了した場合は`complete: true`です。
- 長い一致行は、path・lineと収まる先頭本文に`textTruncated: true`を付けて返し、その時点で`complete: false`になります。path等すら収まらない場合はその要素を含めません。途中までのJSON文字列は返しません。
- stdoutをstream処理し、上限で子processを回収します。複数検索の結果をまとめる場合は、合計がrun_jsの出力予算内に収まるよう調整してください。上限停止後に発生するはずだった走査エラーまでは確認できません。
- 通常の終了コード2以上や起動失敗はthrowします。診断本文は短く制限します。結果の順序は固定しません。出力はUTF-8としてdecodeします。
- `lines`でrgがバイナリ一致の通知を返した場合は、一致行に混ぜず`warnings`へ記録します。

## grepフォールバックの差

この環境のGNU grep 3.11で確認しています。`--null`、`--include`等を使うため、最小機能のgrepすべてを対象にするものではありません。

- `backend: "grep"`と`warnings`で使用を明示します。`.git`ディレクトリは除外しますが、`.gitignore`等のignore fileは読まず、隠しファイルも検索対象です。
- 既定の正規表現検索はgrepの拡張POSIX正規表現になります。rgの正規表現との完全互換ではありません。backendをまたぐ検索では共通構文か`regex: false`の文字列一致を使ってください。
- `glob`はbasenameの`*`、`?`、`[]`と先頭`!`による除外のみです。パスglob、`**`、brace展開は明示的にエラーにします。include/excludeが競合するときの厳密な優先順もbackend依存です。
- 両backendとも通常はバイナリを検索対象から外し、ディレクトリ走査でsymlinkを辿りません。明示したファイル等の扱いは各コマンドに従います。

まず`mode: "files"`で対象を見つけ、対象pathと`glob`を絞って`mode: "lines"`で読むと出力を抑えられます。件数だけ必要なら`mode: "count"`を使います。

検証：`noe run .noe/lib/search/test.js`（rgとGNU互換grepが必要）。
