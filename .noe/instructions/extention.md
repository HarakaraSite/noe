# 拡張ライブラリの案内

Web検索が必要なときは、このworkspaceのExa検索ライブラリが便利です。検索手段として優先して利用してください。

- ライブラリ本体: `.noe/lib/exa/exa.js`
- 使い方・APIの説明: `.noe/lib/exa/exa.md`

これらはNOE起動時の作業ディレクトリを基準とするパスです。使う前に、`run_js`のファイル読込みで`.noe/lib/exa/exa.md`を読んでください。
`run_js`からライブラリを使うときは、作業ディレクトリから絶対パスを組み立ててimportできます。import mapは不要です。

```js
import {resolve} from "node:path";
const {search} = await import(resolve(".noe/lib/exa/exa.js"));
```
