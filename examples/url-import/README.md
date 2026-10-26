# 公開ESMをURLからimportする

[実行例一覧へ戻る](../README.md)

NOE v2.8.0以降を使います。リポジトリのルートで実行してください。実行には配信元CDNへのHTTPS接続が必要です。Node/npmによる事前準備やAPIキーは不要です。

## 小さな外部ライブラリを実際に使う

[durations.js](durations.js)は、時間文字列とミリ秒を相互変換する
[`@lukeed/ms`](https://github.com/lukeed/ms)をjsDelivrから直接importします。

```sh
./noe run examples/url-import/durations.js
```

出力：

```text
5s => 5000 ms => 5s (5 seconds)
2 minutes => 120000 ms => 2m (2 minutes)
2h => 7200000 ms => 2h (2 hours)
1 day => 86400000 ms => 1d (1 day)
```

使用するURLは次のとおりです。最新版を追従せず、`2.0.2`に固定しています。

```js
import { parse, format } from "https://cdn.jsdelivr.net/npm/@lukeed/ms@2.0.2/dist/index.mjs";
```

### 選定理由とWebで確認した情報

- **小さい**：公式READMEでは414Bと紹介されているユーティリティです。この数値はCDNから取得する非圧縮ESMファイルのサイズではありません。
- **依存なし・ESM配布あり**：依存パッケージがなく、公式配布物の`dist/index.mjs`をそのまま使えます。CDNでの変換やbundleは不要です。
- **利用実績**：[npmの公開ページ](https://www.npmjs.com/package/@lukeed/ms)で週あたり数百万回以上のダウンロードが確認できる、2020年公開のライブラリです。ダウンロード数は変動します。
- **MITライセンス**：[公式リポジトリ](https://github.com/lukeed/ms)でAPI・配布形式・ライセンスを確認できます。

`parse()`の入力単位は英語です。`format()`は整数に丸めて表示するため、厳密な時間計算には変換後のミリ秒値を使ってください。

## static・dynamic importとURL alias

以下の例はesm.shへのHTTPS接続を使います。

```sh
./noe run examples/url-import/main.js
./noe run --import-map examples/url-import/import-map.json examples/url-import/mapped.js
# どちらも [[1,2],[3,4],[5]]
```

- main.jsは拡張子のない公開ESM URLからchunkをstatic importします。
- mapped.jsは完全一致aliasからのstatic importと同じURLへのdynamic importを使い、namespaceが同じことも確認します。
- ライブラリ内の相対importは配信URLを基準に解決します。
- JavaScript MIMEとtext/plainを受理しますが、npm探索、TypeScript変換、任意のNode/browser APIの互換保証はありません。
- 別runtimeでは再取得します。URL中のバージョン指定は本文のハッシュ固定ではなく、配信元更新・停止の影響を受けます。
- 同期ダウンロード中は同じruntimeのJS timer/jobが進みません。取消しはGo側の実行contextから接続されます。
- URLのコードは起動ユーザー/実行Profileの権限で動きます。信頼するコードだけを利用してください。sandboxやorigin隔離ではありません。

Go組込みの明示的な有効化、対応範囲、redirect・module IDの規則は[URL importガイド](../../docs/url-import.md)を参照してください。
