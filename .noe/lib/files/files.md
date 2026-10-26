# ファイルの一覧・読込み・編集

`files.js`はNOEで動く外部JSライブラリです。`ls()`で配置を確認し、`read_file()`で本文を読み、`write_file()`・`edit_file()`で作成・編集します。名前で対象を絞って探す場合は[find()](../find/find.md)、本文を検索する場合は[grep()](../search/search.md)を使います。

```js
import {resolve} from "node:path";
const {ls, read_file, write_file, edit_file} = await import(resolve(".noe/lib/files/files.js"));
export default await ls();
```

## ls(path?, options?)

`path`はcwd基準で、既定は`"."`（通常はworkspace）です。既定では直下だけを名前順に返します。

```js
export default await ls("internal", {tree: true, depth: 2});
// 続きがあれば、同じpath・tree・depthでnextOffsetをoffsetに渡す。
export default await ls("internal", {tree: true, depth: 2, offset: 200, limit: 200});
```

| option | 既定 | 内容 |
| --- | --- | --- |
| `tree` | `false` | サブディレクトリも辿る |
| `depth` | `2` | treeで辿る深さ。直下が1 |
| `offset` | `0` | 先頭から飛ばす項目数 |
| `limit` | `200` | 返す項目数 |

返却値は`{path, entries, total, nextOffset?}`。`path`はrootの絶対パス、各entryは`{path, type, bytes, depth}`です。entryのpathはrootからの相対パス、typeは`file`・`directory`・`symlink`・`other`、bytesはlstatのサイズです。directoryのbytesは配下の合計ではありません。

treeは名前順で各directoryの直後にその子を並べます。隠しファイル・Gitのignore対象・空directoryも含め、symlinkの先は辿りません。`total`は指定した深さまでの項目数です。ページ指定でもその深さまでは一覧を走査するため、大きなtreeでは対象pathを絞ります。読取りエラーはthrowします。走査中にファイルが変わると、ページ間の並びも変わり得ます。

## read_file(path, options?)

UTF-8テキストを**既定で全文**返します。改行（LF・CRLF）と末尾の改行有無を保ちます。

```js
export default await read_file("README.md");
export default await read_file("README.md", {offset: 101, limit: 80});
```

`offset`は1始まりの行番号（既定1）、`limit`は最大行数（省略時は末尾まで）です。返却値は`{path, text, offset, lines, totalLines, nextOffset?}`。pathは絶対パス、linesは返した行数、続きがある場合だけnextOffsetが付きます。空ファイルは0行、末尾の改行の後に空行を追加して数えることはありません。EOFより後は空textを返します。

ライブラリ独自のbyte制限はありません。`run_js`へ返す合計が既定64 KiBを超える場合は、行範囲を指定するか、必要な呼出しの先頭コメントに`// @noe: {"outputBytes": 131072}`を置いて予算を増やします。指定範囲は1024〜262144 bytesです。予算はstdout・stderrの捕捉にも連動し、metadataとJSONエスケープを含むモデル向けの結果全体に適用されます。この関数は既存ファイルを読むもので、過去に省略されたstdoutを復元するものではありません。

## write_file(path, text)

UTF-8テキストを新規作成、または既存ファイルへ上書きします。親directoryは再帰的に作成します。返却値は`{path, bytes}`（絶対パスと書き込んだUTF-8 byte数）。textは文字列で、空文字列なら空ファイルになります。

```js
export default await write_file("notes/result.txt", "調査結果\n");
```

## edit_file(path, oldText, newText, options?)

既存ファイル内の文字列を完全一致で置換します。変更箇所の前後だけを渡せるので、ファイル全文を再生成する必要はありません。正規表現やunified diffのparserではありません。

```js
export default await edit_file("app.js", "const limit = 10;", "const limit = 20;");
// 意図して全箇所を変える場合
export default await edit_file("app.js", "oldName", "newName", {all: true});
```

既定では一致が**1か所**の場合だけ書き込みます。0件、または複数件ならthrowし、ファイルは変更しません。複数件のときはoldTextに周囲の文脈を含めて対象を特定するか、`all: true`で全置換します。oldTextは空にできません。newTextは空文字列も指定でき、その箇所を削除します。置換文字列中の`$&`等もそのまま書き込みます。

oldTextには実ファイルと同じ改行（LF・CRLF）を含めてください。変更箇所以外の本文・改行・末尾改行は保持します。対象ファイルが存在しない場合はthrowします。返却値は`{path, replacements, bytes}`（絶対パス・非重複の置換件数・編集後のUTF-8 byte数）。書込み前に現在のファイルを読みますが、他の処理との同時編集を調停する機能はありません。

検証：`noe run .noe/lib/files/test.js`。
