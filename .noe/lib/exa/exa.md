# exa — NOE向け小さなExa検索ライブラリ

NOE（go-quickjs）の標準APIだけで動くExa検索クライアントです。npm依存や公式SDKは
使いません。REST API `POST https://api.exa.ai/search` をグローバルの`fetch`で
呼びます。

- ライブラリ本体: `<workspace>/.noe/lib/exa/exa.js`
- この説明書: `<workspace>/.noe/lib/exa/exa.md`

`<workspace>`はNOE起動時の作業ディレクトリです。この配置ではimport mapは不要です。

## 起動と最小例

```js
// task.js
import {resolve} from "node:path";
const {search} = await import(resolve(".noe/lib/exa/exa.js"));

const result = await search("Exaの検索APIの概要", {
  type: "auto",                         // Exaの検索モード
  numResults: 5,                        // 取得件数
  contents: { highlights: true },       // 結果に含める内容
});

console.log(result.requestId);
console.log(result.costDollars);
for (const item of result.results) {
  console.log(item.title, item.url);
}
```

```sh
# workspaceを作業ディレクトリとして実行
noe run task.js
```

`run_js`からも上記と同じ方法で利用できます。`run_js`の生成JSは一時ディレクトリに置かれるため、
`import "./.noe/lib/exa/exa.js"`ではなく、起動時の作業ディレクトリから絶対パスを組み立てて読み込みます。

## 公開API

### `search(query, options = {})`

Exa Searchを1回実行し、レスポンスJSONを返します。自動リトライやキャッシュは
行いません。

- `query`（必須）: 空でない文字列。空白だけの文字列は`TypeError`になります。
  送信時はこの第1引数が常に使われます。
- `options`（任意）: Exaの検索パラメータをそのまま指定します。`numResults`、
  `type`、`category`、`contents`、`includeDomains`、`excludeDomains`、
  `startPublishedDate` などを`query`と一緒にJSON bodyへ送ります。
- `options.apiKeyFile`（任意・ライブラリ独自）: APIキーのファイルパス。
  JSON bodyには含めません。省略時は
  `~/.config/noe/credentials/exa-api-key`（`os.homedir()`と`path.join`で
  解決）を使います。先頭の`~/`はホームディレクトリに展開します。
  ファイル内容は前後の空白を`trim`してから使います。

キーの読み込みは`search()`の実行時だけです。モジュールの`import`だけでは
ファイル読み込みもネットワーク通信も起きません。

認証は`x-api-key`ヘッダー、本文は`Content-Type: application/json`で送信します。
APIキーの値はログや例外メッセージには含めません。

## 戻り値

Exaが返したJSONを加工せずそのまま返します（`requestId`、`resolvedSearchType`、
`results`、`costDollars`などのフィールドをすべて保持）。

## エラー

| 状況 | 例外 |
| --- | --- |
| `query`が空・文字列以外、`options`がobject以外、`apiKeyFile`が不正 | `TypeError` |
| キーファイルを読めない | `Error`（`name: "ExaSearchError"`、`.cause`に読取りエラー、メッセージにパス） |
| キーファイルが空 | `Error`（`name: "ExaSearchError"`、メッセージにパス） |
| 通信失敗（DNS・接続など） | `Error`（`name: "ExaSearchError"`、`.cause`に元の失敗） |
| HTTP 4xx・5xx | `Error`（`name: "ExaSearchError"`、`.status`（HTTP status）、`.requestId`（取得できれば）、`.body`（パースできたエラーJSON）。メッセージにstatusとExaのエラー文） |

HTTPエラーの例:

```js
try {
  await search("...", { apiKeyFile: "~/.config/noe/credentials/exa-api-key" });
} catch (error) {
  if (error.name === "ExaSearchError") {
    console.error(error.status, error.requestId, error.message);
  }
}
```

## キーの置き場所

既定では次のファイルを読みます（前後の空白は除去）。

```text
~/.config/noe/credentials/exa-api-key
```

別の場所を使う場合:

```js
const result = await search("query", { apiKeyFile: "/path/to/exa-api-key" });
```

キーをスクリプトへ埋め込んだり、リポジトリへcommitしたりしないでください。
