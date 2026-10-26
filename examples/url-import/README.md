# 公開ESMをURLからimportする

URL importを含む現在のソースからビルドしたNOEを使います。リポジトリのルートで実行してください。実行にはesm.shへのHTTPS接続が必要です。Node/npmによる事前準備やAPIキーは不要です。

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
