# HTTP/HTTPSのESMをimportする

URL importを含む現在のソースからビルドしたNOEで利用できます。既存の配布版へ機能が遡って追加されるわけではありません。

CLIのrun/eval、agentのrun_js、ローカル入口のmodule Workerで、HTTP/HTTPS URLをESMの依存としてimportできます。CLIは既定で有効、Goへ組み込む場合はホストが明示的に選びます。

## 直接import・dynamic import・import map

```js
import { chunk } from "https://esm.sh/lodash-es@4.17.21";
console.log(chunk([1, 2, 3], 2));

const lib = await import("https://esm.sh/lodash-es@4.17.21");
```

URL末尾の.js/.mjsは必須ではありません。別の拡張子も扱いますが、本文は常にJavaScriptのESMです。.ts/.jsxというURLを指定しても、その構文を変換しません。

完全一致bare名のtargetに絶対URLを設定できます。

```json
{
  "imports": {
    "lodash": "https://esm.sh/lodash-es@4.17.21"
  }
}
```

```js
import { chunk } from "lodash";
```

```sh
./noe run --import-map examples/url-import/import-map.json examples/url-import/mapped.js
```

mapは起動時に読み、対応表をWorker・親子agent・各run_jsへ継承します。未使用targetには接続しません。map内の相対targetは引き続きローカルmap文書のディレクトリ基準です。prefix/scopes/URL keyのremap等は[import mapガイド](import-map.md)の制限を維持します。

[公開ESMの実行例](../examples/url-import/README.md)は外部サービスに接続します。npm探索・package管理・任意ライブラリの動作保証とは別です。

## 依存の解決とmoduleの同一性

- リモートmodule内の./・../・/・//・query/fragment参照はそのmoduleのURLを基準に解決します。/dep.jsはOSのrootではなく、配信元originのpathです。
- redirect後は最終URLをmodule IDと相対importの基準にします。同じ最終IDへ到達する元URL・alias・直接URLは、同じruntimeで同じnamespaceになります。
- scheme/hostは小文字化し、既定portを除去、空pathを/へ、literal dot segmentを解決します。queryの順序・pathのescapeの意味は変えません。ブラウザのURL正規化全体やsymlink等の同一性を保証するものではありません。
- query違い、非空fragment違いは別moduleです。fragmentはHTTP request targetへ送りません。空fragmentはfragmentなしと同じIDです。
- redirect Locationがfragmentを省略すれば前のfragmentを継承し、#を明示すれば空を含め置換します。
- 未登録bare名はエラーです。CDNの出力にbare依存が残る場合はaliasを明示するか、依存がURL化されたESMを使います。npm/node_modulesの暗黙探索はありません。
- 明示的なローカルfile URLはリモートmoduleからも解決しますが、実際に読めるのは選択されたFSの範囲だけです。組込みAPIの可否も実行Profileに従います。

成功したsourceとmoduleはruntime内で保持し、副作用はmoduleごとに一度実行します。未知のredirect URLについては、最終IDを知るための通信が必要です。最終IDをすでに保持している場合は、最初のsourceを使い続けます。

**ダウンロードキャッシュ・依存固定はありません。** 別run_js、Worker、runtimeでは再取得します。URL中のバージョン指定やmapのsnapshotは、リモート本文のハッシュ固定ではありません。依存graph全体を同時snapshotする機能もありません。配信内容の変更・停止に影響されます。

## HTTPとソース形式

GETの2xxレスポンスを扱い、JavaScript MIMEとtext/plainを受理します。Content-Typeは大小文字を区別せず、text/plain; charset=utf-8等のパラメータを扱います。JavaScript MIMEはHTMLのJavaScript MIME essence集合です。

Content-Type欠落・不正・他のMIME、非2xx、ネットワーク失敗、本文上限、不正UTF-8は読込みエラーです。拡張子による救済や本文sniffing、TS/JSON/Wasmへの自動切替はしません。

sourceはUTF-8として読み、先頭のUTF-8 BOMは除去します。charset宣言による別encodingへの変換はしません。構文・評価エラーはJavaScriptのエラーとして、元のURL/referrer等とともに診断します。

標準設定は1要求30秒のtimeout、展開後の本文32MiB上限です。Goホストはclientと上限を変更できます。NOEは独自retryや条件付きGETを行わず、provider/authのAPI keyを自動送信しません。

## 同期読込みと取消し

module loaderは同期です。**ダウンロード中は同じruntimeのJS job/timerが進みません。** 同じruntimeのsetTimeoutやAbortControllerだけでmodule取得を中断しようとしないでください。

要求と本文読取りはGoの実行contextへ接続します。Runtime.Cancel、task取消し、Worker停止などのホスト側の取消しで停止できます。別runtime/WorkerのJS状態は独立しています。

## 実行入口と権限

- noe run https://...は未対応です。入口はローカルファイルのまま、依存をURLでimportします。
- 標準loaderのWorker入口はローカルです。直接URLとURLを指すaliasは入口用HTTP取得前に拒否します。同じWorker内の依存importはHTTP/HTTPSを利用できます。
- global Workerの既存local target alias入口は維持します。node:worker_threads.Workerの文字列path形式は変更しません。
- URLコードも実行先Profileの権限で動きます。CLIは起動ユーザーのOS権限を持つ既存モデルです。sandbox・origin隔離・コードの安全性保証ではありません。信頼する配信元だけを使ってください。

## Goホストで明示的に有効にする

既存のNewLoader: stdlib.FileLoader(...)はローカル専用のままです。Modules.Fetchを選んだだけではURL importは有効になりません。逆にURL importを選んでもJSのfetch()やFSを自動で有効にはしません。

```go
profile := stdlib.Profile{
    Environment: stdlib.Environment{Cwd: cwd},
    // ローカル依存も読む場合だけFSを選ぶ。
    Modules: stdlib.Modules{
        FS: &stdlib.FileSystemOptions{ReadOnly: true},
    },
    Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{
        Imports: map[string]string{
            "app/local": "./lib/local.mjs",
            "lodash": "https://esm.sh/lodash-es@4.17.21",
        },
        HTTP: &stdlib.ModuleHTTPOptions{
            Client: &http.Client{Timeout: 30 * time.Second},
        },
    }),
}
result, err := stdlib.Execute(ctx, source, profile)
```

上の断片ではctx、cwd、source、net/http、time等は呼出し元で用意します。出力を受け取りたい場合はEnvironment.Stdout/Stderrを設定します。[Go embeddingガイド](go-embedding.md)も参照してください。

| 設定 | 意味 |
| --- | --- |
| Profile.Loader | loader生成と能力metadataを一体で持つModuleLoaderProvider |
| LoaderOptions.Imports | 完全一致map。省略しても絶対URLを直接import可能 |
| LoaderOptions.HTTP | nilならURL import無効、非nilならHTTP/HTTPSを有効化 |
| ModuleHTTPOptions.Client | 借用HTTP client。nilは30秒timeout付きclient。提供clientの設定を変更・closeしない |
| ModuleHTTPOptions.Allow | 初回要求と各redirect先への許可callback。ホストclientのCheckRedirectも尊重 |
| ModuleHTTPOptions.MaxBodyBytes | 0は32MiB、負数は上限なし、正数は展開後本文の上限 |
| Profile.NewLoader | 既存関数型factory。Loaderとの同時指定は設定エラー |

標準providerはoption値とmapをcopyし、runtimeごとにsource状態を新しくします。client/callback/providerは借用であり、並行runtimeから使う場合はホストも並行利用可能にしてください。独自ModuleLoaderProviderはNewLoader(context, Origin)とURLImports()を実装します。独自loader内部のI/Oはホストの責任です。

## agentへの能力通知

Goのagent.HostConfig.Runtimeへ上のProfileを渡せば、Hostがsnapshotした設定から可否を自動通知します。毎回の指示文や親agentのspawn引数に有効化設定を書く必要はありません。

stdlib.URLImportCapability(profile)は次を返します。

- URLImportsEnabled：providerが有効と申告。標準providerはHTTP非nilの場合。
- URLImportsDisabled：providerが無効と申告、またはloaderなし。
- URLImportsUnknown：metadataのない旧NewLoader、または独自providerの不明な申告。

旧FileLoaderは実際にはURLを拒否しますが、関数型factoryの正体からmetadataを推測しないため、通知上はunknownです。明示的なdisabled通知を得るにはLoader: stdlib.ModuleLoader(stdlib.LoaderOptions{})を使えます。

有効という通知は、全URLの許可・任意ライブラリ互換を意味しません。map全文、秘密URL、HTTP header/clientの設定は能力factsに含めません。親agentの指示文だけでホストの無効設定を有効にすることはできません。

[import map](import-map.md) · [Go agent embedding](go-agent-embedding.md) · [設計・実装記録](development/url-import-implementation.md)
