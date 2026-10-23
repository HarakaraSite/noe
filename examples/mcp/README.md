# JSからMCPを利用する

[実行例一覧](../README.md) · [Exa RESTクライアント](../exa/README.md)

公式MCP SDKのClientとStreamable HTTP transportをJSへbundleし、NOEから使う例です。
実利用の接続先は[Exa公式MCPサーバー](https://exa.ai/mcp)の
`https://mcp.exa.ai/mcp`です。ExaはこのURLでAPIキーなしの接続を案内しています。
サーバーを手元で起動する必要はありません。

## SDKの準備

repositoryルートで次を実行します。Node/npm/esbuildはこの準備だけに使います。

```sh
cd examples/mcp
npm ci --ignore-scripts --no-audit --no-fund
npm run build
cd ../..
```

`@modelcontextprotocol/sdk` 1.32.1、esbuild 0.25.12とlockfileを固定しています。
`--conditions=browser`はSDK依存のWeb API向けexportを選ぶための設定です。
生成する`mcp-sdk.bundle.mjs`と`node_modules/`はGit管理外です。

実行時に必要なのはNOE、生成済みbundle、利用するJSとimport mapです。
生成済みbundleを別環境へ渡す場合も、同じ配置かimport mapの相対pathを保ちます。
NOE自身がnpm packageを取得する機構はありません。

## 外部サービスなしで試す

```sh
./noe run --import-map examples/mcp/import-map.json examples/mcp/local.js
```

NOEで一時的なloopback HTTPサーバーを起動し、公式Clientで初期化、tool一覧取得、
日本語を含む3件の並列echo呼出しを行います。結果は`hello`、`東京の雪`、`third`です。
tool応答はSSEで返し、JSON応答とSSE応答の両方を使います。
終了時にclientとサーバーを閉じます。認証・モデル・外部APIは使いません。
このサーバーは動作確認用の最小fixtureです。

## Exa公式サーバーを使う

tool一覧を取得します。引数省略時も`list`です。

```sh
./noe run --import-map examples/mcp/import-map.json examples/mcp/exa.js list
```

検索は`search`を指定した時だけ実行します。

```sh
./noe run --import-map examples/mcp/import-map.json examples/mcp/exa.js \
  search "JavaScriptでJSONLをstream処理する実装例"
```

2026-10-24の一覧取得で`web_search_exa`と`web_fetch_exa`を確認しました。
この例は`web_search_exa`へ、入力文を`query`と`objective`の両方、`numResults: 3`として渡します。
検索結果のJSONはそのままstdoutとdefault exportへ返します。
tool結果が`isError: true`なら終了コード1です。通信・protocolの失敗は例外になります。

任意のAPIキーを使う場合は、既存のキーファイルを指定します。

```sh
EXA_API_KEY_FILE="$HOME/.config/noe/credentials/exa-api-key" \
  ./noe run --import-map examples/mcp/import-map.json examples/mcp/exa.js \
  search "JavaScript streaming JSONL"
```

指定時だけファイルを読み、前後の空白を除いて`x-api-key`ヘッダーへ渡します。
キーをURLへ埋め込みません。キー指定での検索はExa側の利用枠・課金に従います。
ヘッダー方式は[Exa公式実装の案内](https://github.com/exa-labs/exa-mcp-server/blob/main/api/mcp.ts)に基づきます。

## 自分のJSから利用する

同じimport mapを指定し、`client.js`の`withMCP`へ接続先と処理を渡します。
以下はrepositoryルートに置くJSの例です。

```js
import { withMCP } from "./examples/mcp/client.js";

const result = await withMCP("https://mcp.exa.ai/mcp", async client => {
  return await client.listTools();
});
console.log(JSON.stringify(result));
export default result;
```

`withMCP(url, operation, headers?)`は接続後にcallbackを実行し、成功・失敗のどちらでも
`client.close()`を待ちます。OAuthの対話や接続先registryはこの例の範囲に含めていません。
既存の[stdioクライアントスパイク](../../scripts/spikes/process-io-mcp/README.md)は、
子プロセスとして起動するMCPサーバーを利用する別の構成です。

[Streamable HTTPの仕様](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports)と
[初期化の仕様](https://modelcontextprotocol.io/specification/2025-06-18/basic/lifecycle)も参照してください。
