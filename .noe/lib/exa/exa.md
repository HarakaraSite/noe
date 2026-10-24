# Exa — 検索と選択したURLの本文取得

NOEの標準APIだけで動く外部JSライブラリです。npmやSDKは不要です。
検索で候補を絞り、必要なページだけ本文を取得します。

```js
import {resolve} from "node:path";
const {search, getContents} = await import(resolve(".noe/lib/exa/exa.js"));
export default await search("Go microVM library");
```

パスは起動時のcwd（通常はworkspace）基準です。import mapは不要です。
`run_js`でも、上記のように絶対パスを組み立ててimportします。

## 取得内容と上限

| 呼出し | 入力 | 返却内容 | 既定件数 | 最大件数 |
| --- | --- | --- | ---: | ---: |
| `search(query)` | 検索文字列 | タイトル・URL・highlights | 5 | 20 |
| `search(query, {mode: "links"})` | 検索文字列 | タイトル・URLのみ | 5 | 50 |
| `getContents(url)` | 選択したURLを1件 | タイトル・URL・本文 | 1 | 1 |

highlightsは1ページ1,000文字、本文は最大20,000文字をExaへ指定します。
highlightsの文字数指定は厳密な返却上限ではなく、実測では超える抜粋も返っています。
ライブラリ側では追加の切り詰めを行いません。
本文は`includeHtmlTags: false`の**タグなしMarkdown**です。独自のMarkdown変換・
平文化はせず、Exaの`text`をそのまま返します。整ったMarkdownであることには
依存せず文字列として扱います。文字数上限があるため、ページ全文の取得は保証しません。

上限はライブラリ呼出し1回ごとです。複数クエリを呼ぶと件数・本文量は合算されます。
返却JSON全体には、さらに`run_js`の共通出力予算（既定64 KiB・最大256 KiB）が適用されます。
文字数とUTF-8 byte数は同じではありません。

## search(query, options?)

`POST https://api.exa.ai/search`を1回呼びます。既定モードは`highlights`です。

```js
export default await search("Go microVM library", {numResults: 10});
```

```js
export default await search("Go microVM library", {mode: "links", numResults: 50});
```

- `query`：必須の空でない文字列。`options.query`より第1引数を優先します。
- `mode`：`"highlights"`（既定）、または`"links"`。
- `numResults`：既定5。highlightsでは1〜20、linksでは1〜50の整数。
- 検索フィルタ：`type`、`category`、`includeDomains`、`excludeDomains`、
  `startPublishedDate`、`endPublishedDate`、`startCrawlDate`、`endCrawlDate`、
  `includeText`、`excludeText`、`userLocation`をExaへ転送します。
- highlightsの鮮度指定：`maxAgeHours`・`livecrawlTimeout`を指定できます。
  Exaへの送信時には`contents`内へ置きます。linksでは本文を取得しないため指定できません。
- `apiKeyFile`：任意のキーファイルパス。下記参照。

返却値は`{mode, results, requestId?, resolvedSearchType?, searchTime?, costDollars?, statuses?}`。
resultsの各項目は`{title, url, highlights}`、linksでは`{title, url}`です。
Exaが要求件数より少ない結果を返す場合もあります。

**旧版の`contents`直接指定は受け付けません。** 本文、summary、subpages等を検索へ
混ぜず、モードを選んでください。件数超過・不正モード・未対応optionは、APIキーの
読込みやHTTP送信より前にエラーになります。

旧`search(query, {contents: {highlights: true}})`は、`search(query)`へ変更します。
旧`search(query, {contents: {text: true}})`は、検索で候補URLを選び、次の本文取得へ分けます。

## getContents(url, options?)

`POST https://api.exa.ai/contents`へURLを1件だけ送ります。配列や追加ページの巡回は
受け付けません。検索結果から必要なURLを選んで使います。

```js
export default await getContents("https://example.com/report", {maxCharacters: 10000});
```

- `url`：HTTP(S)のURL文字列を1件。
- `maxCharacters`：既定20,000、1〜20,000の整数。小さい範囲も指定できます。
- `maxAgeHours`・`livecrawlTimeout`：Exaの鮮度・クロール待ち時間指定。省略時はExa既定。
- `apiKeyFile`：任意のキーファイルパス。

返却値の形はsearchと共通で、`mode: "text"`、resultsは最大1項目の
`{title, url, text}`です。`statuses`があればURLごとの成功・失敗も確認できます。
HTMLタグ、summary、highlights、subpagesの追加指定は受け付けません。

## 認証とエラー

既定のキーファイルは`~/.config/noe/credentials/exa-api-key`です。
`apiKeyFile: "/path/to/key"`で変更でき、先頭の`~/`も展開します。
ファイル内容は前後の空白を除き、`x-api-key`ヘッダーへ設定します。
キーをソースやリポジトリへ保存しないでください。

importだけではキーの読込み・通信は起きません。ライブラリ独自の自動リトライ・
キャッシュはありません（Exa側のキャッシュとは別です）。

不正引数は`TypeError`または`RangeError`。キー読込み・通信・JSON解析・HTTPエラーは
`ExaSearchError`です。HTTPエラーには`status`・`requestId`・`body`、元の失敗が
ある場合は`cause`を保持します。APIキーの値は例外メッセージへ含めません。

## 検証と公式資料

`noe run .noe/lib/exa/test.js`で送信内容・上限・返却形式・エラーを検証します。
HTTPを差し替えるため、実APIキー・ネットワーク通信・API課金は不要です。

- [Exa Search](https://exa.ai/docs/reference/search)
- [Exa Contents](https://exa.ai/docs/reference/get-contents)
- [本文はMarkdown、highlightsは関連箇所の抜粋](https://exa.ai/docs/contents/quickstart)
