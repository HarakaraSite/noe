# 公式MCP SDKとのローカル接続確認

通常のGoテストや配布バイナリには含めない検証用ソースです。公式`@modelcontextprotocol/sdk` **1.32.1**のClientを使い、transportだけをNOEの`spawn`とWeb Streamsで実装します。SDK本体へのパッチはありません。Node/npm/esbuildは一時的なbundle作成時だけ使います。実行はNOEです。

repository rootから、外部の作業ディレクトリへ依存とbundleを作ります。

```sh
go build -o /tmp/noe-process-io ./cmd/noe
work=$(mktemp -d /tmp/noe-process-mcp.XXXXXX)
cp scripts/spikes/process-io-mcp/{package.json,client.js,server.js} "$work/"
cd "$work"
npm install --ignore-scripts --no-audit --no-fund
./node_modules/.bin/esbuild client.js --bundle --format=esm --platform=neutral \
  --main-fields=module,main '--external:noe:*' '--external:node:*' \
  --outfile=client.bundle.js
/tmp/noe-process-io run --timeout 20s "$work/client.bundle.js" "$work/server.js"
```

2026-10-18、Linux上の実行結果：

```json
{"sdk":"1.32.1","initialized":true,"tools":1,"concurrentCalls":3,"largeInputBytes":270000}
```

initialize、initialized通知、tools/list、3件の並行tools/callとID対応、27万バイトのUTF-8入力、stdin EOF後の終了・回収を確認しました。外部provider・認証は使いません。

`server.js`はecho toolだけのローカルfixtureです。MCPの汎用製品実装・外部モジュール解決の追加を示すものではありません。URL importやnpm解決をNOEに追加せず、準備済みbundleをローカルESMとして読みます。esbuildはCommonJS依存もbundleへ含めるため`--main-fields=module,main`が必要です。
