# MCP・デーモンの利用例 — 2026-10-24

[開発記録](README.md) · [MCP例](../../examples/mcp/README.md) · [デーモン例](../../examples/daemon/README.md)

利用者の実装指示と「公式サーバーはexaにする？」を受け、MCPはExa公式の
Streamable HTTPサーバーを接続先に選んだ。JS・examples・利用案内の変更に限る。

## 成果物

- MCP：公式SDK 1.32.1のClientとHTTP transportを、固定したesbuild 0.25.12で
  ESMへbundleする。lockfile、生成物のignore、import map、接続/終了を担う
  `withMCP`、Exaの一覧/検索CLI、ローカルJSON/SSE fixtureを追加した。
  Node/npmはbundle準備時だけ使い、NOE実行時は生成済みJSを読む。
- Exa：公開URL`https://mcp.exa.ai/mcp`へ接続する。既定は一覧取得で、明示した
  `search`だけがtoolを呼ぶ。任意の`EXA_API_KEY_FILE`は`x-api-key`ヘッダーへ渡す。
  実際の公開tool schemaで検索の`objective`必須を確認し、queryと同じ入力を渡す。
  tool結果の`isError`は結果を保持したままCLI終了コード1へ反映する。
- デーモン：JSでloopback TCPのJSONL要求を受け、ping/echo/sumへ応答する。
  接続ごとにUTF-8 decoderと未完了行を保持し、複数接続を並行処理する。
  SIGTERMでlistenerと接続を閉じ、処理とsignal購読を回収して終了する。
  外部shellからの起動・要求・停止と、端末内の待受の手順を用意した。

既存のstdio MCPスパイクは変更していない。MCPサーバーの製品組込みや
接続先registry、上流・Go製品コード・go.mod/go.sumへの変更は含めない。

## 確認結果

Linux amd64の公開済みNOE v2.3.0を使用した。現行ソースとの差は今回のJS実行に
影響しない単独observeの取消し修正と文書だけなので、ランタイムの再ビルドはしていない。

| 対象 | 結果 |
| --- | --- |
| 固定npm依存とbundle | 手元のnpm cacheからoffline install/ciが成功。lockfile維持と`npm run build`を確認 |
| MCPローカル例 | 初期化→initialized通知→一覧→日本語を含む3件の並列tool呼出し、JSON/SSE応答、終了が成功 |
| Exaエントリのローカルfixture | 実エントリのfetchだけをloopbackへ向け、query/objective/件数、ダミーkeyのheader、tool成功とisErrorの終了コードを確認 |
| 実Exa公式サーバー | 匿名で初期化・一覧取得が成功。`web_search_exa`・`web_fetch_exa`と公開入力schemaを確認 |
| デーモンのCLI client | ping/echo/sum、不正要求の終了コード1、停止後の接続失敗を確認 |
| JSONL・並行処理 | 3並行接続、UTF-8の途中で分割した要求、同一接続の複数要求、不正JSON後の継続が成功 |
| SIGTERM | 応答確認後も開いた接続と未完了行のある接続を残して停止し、終了コード0・connections 0・接続閉鎖を確認 |
| listener解放 | OSの再利用条件を合わせて同portへbindできた |
| 静的確認 | 新規JS 6ファイルのBiome lint、文書リンク、JSONと差分空白を確認 |

Exaで実行したのは初期化と一覧取得だけで、実検索・web_fetch等のtoolは呼んでいない。
検索の実結果・課金・APIキーでの実認証は検証していない。
実providerへの要求、Go全体suite・race・配布ビルド、他OSのnative実行も行っていない。

最初のHTTP SDK bundleはneutral条件だけではpkce-challengeのexportを選べず、
browser条件を加えて解決した。SDKや上流sourceの変更はない。
デーモン確認の初回は停止後に通常socketでbindし、TCPのTIME_WAITによるEADDRINUSEに
なったため、Goのlistenerと同じSO_REUSEADDR条件で確認し直した。
成功済みのMCP確認は再実行せず、該当するデーモン確認だけを再実行した。

結果・fixture・bundle準備ログ・実Exaの一覧は`/var/tmp/noe-mcp-daemon-h5txb0i7`へ保存した。
生成済みSDK bundleは`examples/mcp/`へ配置済み（Git管理外）。利用者の追加指示により、利用例・手順・検証記録をコミットする。push・公開は未実施。
MCPの使い方をagentの初期contextへ案内する整備は、利用者が今回は不要としたため対象外。
