# 拡張ライブラリの案内

## リポジトリの見取り図

配置を把握したいときは`.noe/lib/repo-map/repo-map.js`の`repoMap(options?)`を使えます。言語に依存せず、ファイル本文を読まずにtreeと配下件数を返します。

```js
import {resolve} from "node:path";
const {repoMap} = await import(resolve(".noe/lib/repo-map/repo-map.js"));
export default await repoMap();
```

既定は深さ2・表示120項目・返却JSON全体8,000 bytes。必要な枝は`repoMap({path: "src", depth: 3})`のように展開します。Git管理下ではignore設定を使い、Gitなしでは通常走査へ切り替えます。`complete`・`scanComplete`・`limitedBy`で省略を確認できます。詳細は`.noe/lib/repo-map/repo-map.md`へ。構造を把握してから、必要な場所を下記の`grep()`で検索できます。

## ローカルファイルの内容検索

内容検索には`.noe/lib/search/search.js`の`grep(options)`を優先して使えます。既定は正規表現検索で、`foo|bar`で複数候補を検索できます。rgがなければGNU互換grepへ切り替わります。結果にはbackend・complete・limitedByが付きます。省略されたら対象pathやglobを絞ってください。

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

これらはNOE起動時の作業ディレクトリを基準とするパスです。使う前に、`run_js`のファイル読込みで`.noe/lib/exa/exa.md`を読んでください。
`run_js`からライブラリを使うときは、作業ディレクトリから絶対パスを組み立ててimportできます。import mapは不要です。

```js
import {resolve} from "node:path";
const {search} = await import(resolve(".noe/lib/exa/exa.js"));
```
