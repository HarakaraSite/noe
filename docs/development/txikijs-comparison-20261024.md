# txiki.jsとNOEの機能比較 — 2026-10-24

[ドキュメント一覧](../README.md) · [開発ガイド](README.md) · [次の機能候補](../future-candidates.md)

今後のNOE改善の参考として、txiki.jsの公開ドキュメントとNOEの現行API・ソースを比較した記録。
機能差と改善候補を整理する資料であり、実装の採用・優先順位を決定した計画ではない。

## 比較対象と確認範囲

- 調査日：2026-10-24。
- txiki.js：下記の公式Webドキュメントを調査日に参照。特定のリリースタグに固定した比較ではなく、txiki.js自体の実行検証は行っていない。
- NOE：`428c2ad`時点のソース・内蔵API資料と、ローカルのv2.3.0バイナリ。主要な未対応グローバルと`crypto.subtle`のメソッドを実機で確認した。
- 対応の記載は、その機能や公開APIがあることを示す。同名APIの全仕様・アルゴリズム・オプションの互換性を保証しない。
- 速度、メモリ使用量、バイナリサイズ、ECMAScriptの適合率は測定していない。
- 「未対応」はNOE標準の公開機能としての状態。外部JS、外部コマンド、Goホスト側の追加実装による代替まで不可能という意味ではない。

## 全体像

txiki.jsはWeb標準API、ネットワーク、SQLite・FFI・Wasm、JSアプリの配布まで、汎用JSランタイムとしての機能範囲が広い。
NOEは基本的なJS実行環境に、Goへの組込みとエージェント実行・観測を加えた構成になっている。
Fetch・Streams・Worker・ファイル・プロセス・HTTPなどの土台はかなり重なる。

この整理は機能構成からの評価であり、性能や品質の優劣を示すものではない。

## Web Platform APIs

参照：[txiki.js Web Platform APIs](https://txikijs.org/docs/features/web-platform-apis/)、[NOE globals](../../internal/apidocs/content/stdlib-globals.md)。

| 機能 | txiki.js | NOE |
| --- | --- | --- |
| Fetch・Request・Response・Headers | 対応 | 対応。入出力のストリーミングも実装済み |
| Web Streams・文字コード変換 | 対応 | 対応 |
| Blob・File・FormData | 対応 | 対応 |
| URL・URLSearchParams・URLPattern | 対応 | 対応 |
| Worker・MessageChannel・BroadcastChannel | 対応 | 対応 |
| structuredClone・転送可能オブジェクト | 対応 | 対応。対応対象は各実装の範囲による |
| CompressionStream・DecompressionStream | 対応 | 対応 |
| console・performance・タイマー・queueMicrotask・atob/btoa | 対応 | 対応 |
| EventTarget・DOMException | 対応 | 対応 |
| AbortSignal | `abort`・`timeout`・`any` | `abort`・`timeout`。`any`は未対応 |
| WebSocket | 対応。接続時の独自ヘッダー指定も可能 | 対応。独自ヘッダー指定の公開APIはない |
| WebSocketStream | 対応 | 未対応 |
| EventSource・XMLHttpRequest・FileReader | 対応 | 未対応 |
| localStorage・sessionStorage | 対応。localStorageは通常SQLiteへ永続化 | 未対応 |
| Navigator.userAgentData | 対応 | 未対応 |
| WebAssembly | WAMRによる対応。一部制約あり | 未対応 |

txiki.jsはWinterTC準拠を目標としている。これは、全Web APIの完全実装を意味しない。
NOEにもFetchのbody上限、リダイレクト、HTTP duplexなど固有の制約があり、外部SDKの利用可否は必要なAPIと挙動で判断する。

### Web Crypto

同じ`crypto`・`crypto.subtle`という名前でも、対応範囲の差が大きい。

| 項目 | txiki.js | NOE |
| --- | --- | --- |
| 乱数・UUID・ハッシュ | 対応 | 対応 |
| HMAC・PBKDF2・HKDF | 対応 | 対応 |
| AES暗号化・RSA・ECDSA・Ed25519など | 対応 | 未対応 |
| 鍵の生成・ラップ／アンラップ | 対応 | 未対応 |
| 鍵の入出力 | 複数形式に対応 | HMAC・PBKDF2・HKDFのraw形式に限定 |

txiki.jsはSubtleCryptoの12メソッドを提供する。NOEで確認できたのは次の7メソッド。

```text
deriveBits deriveKey digest exportKey importKey sign verify
```

NOEの`sign`・`verify`はHMAC用であり、公開鍵署名には対応しない。`deriveKey`も対応する鍵種別の範囲に限られる。
署名や暗号化を要求する外部ライブラリでは、この差が利用可否に直結する。

参照：[txiki.js SubtleCrypto](https://txikijs.org/docs/features/web-platform-apis/#web-crypto-subtlecrypto)、[NOE crypto実装](../../internal/stdlib/crypto.go)、[NOE crypto資料](../../internal/apidocs/content/stdlib-crypto.md)。

## ランタイム・OS機能

参照：[txiki.js Runtime Features](https://txikijs.org/docs/features/runtime-features/)、[NOE API](../api.md)。

| 機能 | txiki.js | NOE |
| --- | --- | --- |
| ファイル操作 | 対応 | `node:fs`・`node:fs/promises`・`noe:fs-handle` |
| 子プロセス・標準入出力 | 対応 | `node:child_process`・`noe:process`・`noe:io` |
| OSシグナル | 対応 | `noe:signals`。OSごとの制約あり |
| DNS | 対応 | `node:dns`・`node:dns/promises` |
| HTTPサーバー | 対応 | `serve`・`noe:http`。WebSocket upgradeも対応 |
| TCP・Unixソケット | 対応 | `noe:sockets`で対応 |
| 生TLS・UDPソケット | 対応 | 未対応。HTTPS/WSS通信は利用可能 |
| 組込みテストランナー | あり | なし。assertやJSスクリプトでの検証は可能 |

ソケットは双方ともWeb Streamsを使うが、API形状が異なる。
txiki.jsは`TCPSocket`などのクラスと`opened`・`closed`を持つモデル、NOEは`connect()`・`listen()`と接続・listenerの明示的なcloseが中心。

参照：[txiki.js Networking](https://txikijs.org/docs/guides/networking/)、[NOE sockets](../../internal/apidocs/content/noe-sockets.md)、[NOE低水準I/O](../low-level-io.md)。

## モジュール・開発・配布

| 項目 | txiki.js | NOE |
| --- | --- | --- |
| ローカルESM | 対応 | 対応 |
| HTTP/HTTPSからのimport | 対応 | 未対応 |
| JSON・text・bytesのimport attributes | 対応 | 公開ローダーは`.js`・`.mjs`のみ。ファイルAPIで読む |
| import map | 完全一致・prefix・scopes、URLへの割当て | ローカルJSへの完全一致のみ |
| JSアプリの単体実行ファイル化 | `tjs compile`、複数ファイル用TPKあり | 専用コマンドなし。Go組込みでの配布は可能 |
| REPL | あり | なし。過去に削除済み |
| TypeScript直接実行 | なし。事前変換が必要 | 同じく事前変換が必要 |
| 型定義 | `@txikijs/types` npmパッケージ | ソース配布にstdlib・agent用の宣言ファイルを同梱 |

txiki.jsの`tjs compile`は単一JSファイルをランタイムとまとめる機能で、複数ソースのバンドル自体は別工程。
NOEのMCP例でも外部SDKを事前にバンドルしているが、NOEがnpmの依存解決やパッケージ管理を行うわけではない。

参照：[txiki.js Modules](https://txikijs.org/docs/guides/modules/)、[Standalone Executables](https://txikijs.org/docs/guides/standalone-executables/)、[TypeScript](https://txikijs.org/docs/typescript/)、[NOE import map](../import-map.md)、[NOE JS検査](../javascript-checks.md)、[MCP例](../../examples/mcp/README.md)。

## 標準ライブラリ

txiki.jsは`tjs:`モジュール、NOEは26のNode形式モジュールと`noe:`拡張を提供する。
分割方法が違うため、モジュール数だけでは機能の広さを比較できない。
NOEの`node:`は対応APIの提供形式であり、Node.js全体の互換性を意味しない。

| txiki.js | NOEでの対応 |
| --- | --- |
| `tjs:assert`・`tjs:path` | `node:assert`・`node:path`。API差あり |
| `tjs:hashing` | `node:crypto`。MD5・SHA-2・SHA-3など、逐次ハッシュも可能 |
| `tjs:utils` | `node:util`などに相当機能 |
| `tjs:uuid` | `crypto.randomUUID()`はある。UUID専用の生成・検証モジュールはない |
| `tjs:getopts` | 専用の引数解析APIなし |
| `tjs:ipaddr` | 専用のIP解析・操作APIなし |
| `tjs:readline` | 専用の対話入力・行編集APIなし |
| `tjs:posix-socket` | 同等の低水準POSIX APIなし。TCP/Unixの通常通信は可能 |
| `tjs:sqlite` | 組込みSQLiteなし |
| `tjs:ffi` | JSからネイティブライブラリを呼ぶ汎用FFIなし |
| `tjs:wasi` | WASIなし |

txiki.jsのSQLiteとWebAssembly/WASIは標準ビルドでは有効だが、ビルド設定によって省略される。

参照：[txiki.js Standard Library](https://txikijs.org/docs/features/standard-library/)、[NOE標準モジュール一覧](../../internal/apidocs/stdlib.go)、[NOE型定義](../../stdlib/types/noestdlib.d.ts)。

## 実装基盤とNOE側の特色

| 項目 | txiki.js | NOE |
| --- | --- | --- |
| JSエンジン・ホスト基盤 | QuickJS-ng＋libuv | pure Goのgo-quickjs＋NOE管理のstdlib |
| NOE標準ライブラリの由来 | 対象外 | go-quickjs v0.27.0由来のスナップショットを独自管理。エンジン依存はv0.28.0 |
| Goアプリへの組込み | 今回の資料では評価していない | 公開`stdlib`・`agent` APIあり。Cツールチェーンなしで通常ビルド可能 |

NOEには次の仕組みがある。今回参照したtxiki.jsの資料には、対応するエージェント機能の記載はない。

- `run_agent`、モデル接続、子エージェント、フック、コンテキスト管理。
- `run_js`の構造化データ・診断情報。
- JSONLイベント記録と`noe observe`。
- 実行バージョンに対応した`noe docs`・`noe:docs`。
- Go組込み時の実行単位の環境・機能設定。

エンジンが異なるため、言語仕様への適合や不具合も同一ではない。
NOEで確認済みのES module内のブロック関数宣言の問題は、[上流修正待ちの記録](dependency-updates.md#上流修正待ちes-module内のブロック関数宣言--2026-10-24)を参照する。
今回の調査でtxiki.jsに同じ再現コードを実行したわけではない。

参照：[txiki.js Getting Started](https://txikijs.org/docs/getting-started/)、[NOE README](../../README.md)、[stdlib由来](../../internal/stdlib/UPSTREAM.md)、[Go組込み](../go-embedding.md)、[agent組込み](../go-agent-embedding.md)。

## 改善を検討するときの着眼点

以下は調査からの提案であり、採用済み要件や作業順ではない。

| 候補 | 期待する利用上の効果 | 採用判断に必要な具体的な用途 |
| --- | --- | --- |
| `AbortSignal.any()` | 複数の取消し条件をまとめる標準的な書き方に対応 | タイムアウトと利用者取消しを合成する処理、同APIを要求するSDK |
| Web Cryptoの拡充 | 外部SDKや認証・署名・暗号化処理への対応範囲を拡大 | 必要なアルゴリズム、鍵形式、メソッドを使う実際のライブラリ |
| SQLite | JSだけでローカルの永続データを検索・更新 | agentの状態保存、収集データ、ローカルサービスのDB |
| import attributes・import mapの拡充 | 既存ESMや設定・テンプレートを扱いやすくする | 現行のファイル読込みや完全一致mapで負担が出る構成 |
| WebSocket独自ヘッダー・EventSource等 | 特定の外部サービスやWeb SDKとの接続を簡単にする | 現行のWebSocketやFetchベースの実装で支障が出る接続先 |
| TLS/UDP・Wasm/WASI・FFI・実行ファイル化 | 新しい通信・ライブラリ・配布用途を可能にする | その機能を必要とする具体的なアプリ |

MCPクライアントとTCPデーモンの利用例は、NOEの既存機能で成立している。
この用途を続けるために、比較で挙がった差分をすべて埋める必要はない。
REPLは意図して削除した機能なので、差分があることだけを再導入の根拠にしない。
今後の選定では、実際の課題・SDK・データ処理が何を要求するかと、pure Go・Go組込み・既存APIへの影響を合わせて判断する。

## 実機確認の記録

NOE v2.3.0で、対象グローバルの`typeof`、`typeof AbortSignal.any`、`Object.getOwnPropertyNames(crypto.subtle)`を取得した。
API名の有無を確認する軽い検査であり、各APIの動作適合テストではない。

```text
BroadcastChannel: function
MessageChannel: function
URLPattern: function
FileReader: undefined
EventSource: undefined
XMLHttpRequest: undefined
WebSocketStream: undefined
localStorage: undefined
sessionStorage: undefined
WebAssembly: undefined
navigator: undefined
AbortSignal.any: undefined
crypto.subtle: deriveBits, deriveKey, digest, exportKey, importKey, sign, verify
```
