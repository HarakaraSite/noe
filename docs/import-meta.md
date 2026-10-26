# import.meta.urlでmodule自身の場所を知る

NOE v2.8.1以降で利用できます。v2.8.0以前では`import.meta.url`がundefinedになります。

ES Module内の`import.meta.url`は、そのmodule自身のURLを返します。
entryだけでなく、static/dynamic importされた各moduleとmodule Workerでも利用できます。

```js
// このmoduleと同じ場所にあるasset.txtのURLを計算する。
const assetURL = new URL("./asset.txt", import.meta.url);
```

これはURLの計算だけで、assetの取得は行いません。
ローカルファイルなら選択されたfilesystemのAPI、HTTPなら許可されたfetch等で別途読みます。
`process.cwd()`や通常のファイル操作の相対パスを変更する機能ではありません。

## 値の規則

| module | import.meta.url |
| --- | --- |
| ローカルファイル | 解決済み絶対パスのfile URL。空白・日本語・#・?・%等はURLエンコードする |
| HTTP/HTTPS | redirect後のcanonical module ID。query/fragmentも含む |
| rooted filesystem | host上の物理rootではなく、実行環境内のパスをfile URLにする |
| agent run_js等の`Source.Virtual: true` entry | cwd配下の合成`<virtual-entry>.mjs`のfile URL |
| CLI eval | 現在の合成entry名であるcwd配下の`<eval>.js`のfile URL |
| Worker eval | cwd配下の合成`<virtual-entry>.mjs`のfile URL |

たとえば`/work/lib/a b.mjs`は`file:///work/lib/a%20b.mjs`となります。
HTTPのalias・redirect前URLからimportしても、実行される最終moduleのURLになります。
fragmentをHTTP request targetへ送らない等、既存の[URL module identity](url-import.md)は変わりません。

仮想entryのURLは相対参照の基準を提供するための合成値です。
**対応するファイルが存在することを意味しません。**
仮想entryの相対importと`new URL("./asset.txt", import.meta.url)`はどちらもcwd基準になります。
元の`Source.Name`は診断・stack用に保持し、metadataのために書き換えません。

file URLはURL文字列であり、そのままOSのファイルパスとして扱うものではありません。
Windowsのdrive pathは`file:///C:/...`、UNC pathは`file://server/share/...`形式で表現します。
URLを提供しても、標準loaderの非local file authority等の既存制限は変わりません。

## Go embedding・host定義module

`stdlib.Execute`/`NewRuntime`と公開`Attach`は同じmetadata初期化を使います。
独自loaderや`Bindings.DefineModule`で提供するmoduleには次を適用します。

- 絶対filesystem ID: file URL。
- 絶対URI ID: そのIDを保持。
- その他のbare/opaqueなID: `noe:module/`にURLエスケープしたIDを続ける。実在ファイルを表さず、相対asset参照の基準になる保証はない。

ホストが別の契約を提供する場合は、`Profile.Initialize`内、または`Attach`後に
`b.Runtime().OnImportMeta(...)`でhookを上書きできます。
NOEの標準hookはInitializeより前に登録し、Prepareで再登録しません。

## 対象外

- `import.meta.resolve`・`main`・`dirname`・`filename`は追加しません。
- TypeScript変換、npm探索、CommonJS対応を追加するものではありません。
- filesystem・HTTP等の権限を増やしません。
- `import "./dep.js"`の相対importは従来どおりloaderが解決します。`import.meta.url`を経由させる変更ではありません。
- `new URL`自体やfile URL変換helperの全Node.js/Web互換性を保証するものではありません。

[URL import](url-import.md) · [Go embedding](go-embedding.md) · [実装・検証記録](development/import-meta-url-implementation.md)
