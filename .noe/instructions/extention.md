# 拡張ライブラリの案内

## ファイルの一覧・読込み・編集

ファイルの一覧・読込み・作成・部分編集には、原則として`.noe/lib/files/files.js`の`ls()`・`read_file()`・`write_file()`・`edit_file()`を使ってください。

```js
import {resolve} from "node:path";
const {ls, read_file, write_file, edit_file} = await import(resolve(".noe/lib/files/files.js"));
export default await ls();
```

`ls()`はcwd（通常はworkspace）の直下を既定で最大200項目表示します。`limit`で返す項目数を変更できます。`ls("internal", {tree: true, depth: 2})`でtree相当の一覧、`nextOffset`を`offset`へ渡すと続きが取得できます。隠しファイル・ignore対象も含み、symlinkの先は辿りません。

`read_file("README.md")`は全文、`read_file("README.md", {offset: 101, limit: 80})`は1始まりの行範囲を返します。結果の`text`が本文です。必要な本文が大きければrun_jsの`outputBytes`を増やせます（既定65536・最大262144 bytes）。

`write_file("path/to/new.txt", "本文")`は親directoryを作り全文を書き込みます。既存ファイルは上書きします。部分編集は`edit_file("app.js", "変更前の文字列", "変更後の文字列")`。一致が1か所のときだけ置換し、0件・複数件なら書き込まずthrowします。複数件を全置換したい場合だけ第4引数に`{all: true}`を指定します。正規表現ではなく完全一致なので、必要な前後の文脈と改行を含めて指定してください。返すのはpath・bytes・置換件数などの要約で、全文の再出力は不要です。詳細は`.noe/lib/files/files.md`へ。内容検索は下記の`grep()`で絞れます。

## ファイル名・ディレクトリ名の検索

名前でファイルやディレクトリを探すときは、原則として`.noe/lib/find/find.js`の`find(path?, options?)`を使ってください。構成の確認は`ls()`、本文の検索は下記の`grep()`です。

`fd`を優先し、実行ファイルが見つからない場合だけGNU互換`find`へ切り替えます。

```js
import {resolve} from "node:path";
const {find} = await import(resolve(".noe/lib/find/find.js"));
export default await find("examples", {name: "*import*", type: "directory"});
```

`name`はbasename全体に対するglob（`*`・`?`・`[]`）です。部分一致には`"*import*"`を使います。`type`は`all`（既定）・`file`・`directory`・`symlink`、`maxDepth`で深さを指定できます。結果の`results`はrootからの相対パス文字列で、`backend`・`complete`・`limitedBy`が付きます。既定上限は300件・返却JSON全体32,000 bytesです。両backendとも隠しファイル・ignore対象を含み、`.git`は除外し、symlinkの先は辿りません。未完了ならrootや条件を絞ってください。詳細は`.noe/lib/find/find.md`へ。

## ローカルファイルの内容検索

ローカルファイルの内容検索は、原則として`.noe/lib/search/search.js`の`grep(options)`を使ってください。必要な機能がこのライブラリにない場合に、外部コマンドのrg・grepを使ってください。

既定は正規表現検索で、`foo|bar`で複数候補を検索できます。rgがなければライブラリがGNU互換grepへ切り替わります。結果にはbackend・complete・limitedByが付きます。省略されたら対象pathやglobを絞ってください。

```js
import {resolve} from "node:path";
const {grep} = await import(resolve(".noe/lib/search/search.js"));
export default await grep({pattern: "outputLimit", paths: ["internal/agent"], mode: "files"});
```

`mode`は`files`（一致するファイル名）・`lines`（既定、一致行）・`count`（ファイルごとの一致行数）。`glob: ["*.go", "!*_test.go"]`で絞込み、`regex: false`で文字列一致、`ignoreCase: true`で大小文字を無視できます。ドットや括弧等をそのまま探す場合は`regex: false`を使います。既定上限は50件・返却JSON全体8,000 bytesです。必要なら`maxResults`・`maxBytes`を指定します。基本の使い方はこの説明だけで足ります。詳細やgrep fallbackの差は`.noe/lib/search/search.md`を参照してください。

## Web検索

Web検索が必要なときは、このworkspaceのExa検索ライブラリが便利です。検索手段として優先して利用してください。

- ライブラリ本体: `.noe/lib/exa/exa.js`
- 使い方・APIの説明: `.noe/lib/exa/exa.md`

これらはNOE起動時の作業ディレクトリを基準とするパスです。import mapは不要です。

```js
import {resolve} from "node:path";
const {search, getContents} = await import(resolve(".noe/lib/exa/exa.js"));
export default await search("Go microVM library");
```

`search(query)`は既定5件のタイトル・URL・highlightsを返します。Exaへ1ページ1,000文字を指定しますが、実際の返却は超える場合があります。`numResults`は最大20件。タイトル・URLだけ必要なら`search(query, {mode: "links", numResults: 50})`で最大50件まで取得できます。件数上限は1クエリごとです。

本文が必要ならURLを選び、`getContents(url)`で1件取得してください。タグなしMarkdownを最大20,000文字、`maxCharacters`でより短く指定できます。検索へ`contents`・本文・subpages等を直接指定する旧形式はエラーになります。基本の使い方はこの説明で足ります。検索フィルタ・戻り値・エラーの詳細は`.noe/lib/exa/exa.md`を参照してください。
