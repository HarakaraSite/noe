# 画像の読み取りと生成

[実行例一覧へ戻る](../README.md) · [画像ガイド](../../docs/agent-images.md)

コマンドはリポジトリのルートで実行します。例を使う前に`./noe`をビルドし、利用するproviderの認証情報を登録してください。

## agentに画像を見せる

`read.js`は`run_agent`を起動し、画像pathをinstructionに含めます。agentは`run_js`の中で`read_image(path)`を呼び、画像について回答します。既定provider/modelはChatGPT認証の`openai-chatgpt` / `gpt-6-luna`です。

```sh
./noe run examples/image/read.js ./tmp/screenshot.png
```

別のvision対応agent modelを指定する場合はproviderとmodelを続けて渡します。

```sh
./noe run examples/image/read.js ./tmp/screenshot.png openrouter openai/gpt-6-luna
```

今回の[モデル比較](../../docs/agent-images-integration.md#実送信bytesの照合とモデル比較)では、4色画像をLunaが誤認し、Solは正答しました。Solを試す場合は次のように指定できます。

```sh
./noe run examples/image/read.js ./tmp/screenshot.png openai-api gpt-6.1-sol
```

読取り可能な形式はPNG、JPEG、WebPです。画像bytesはhost側で扱われ、JavaScriptの戻り値はpath・MIME・寸法などのmetadataだけです。

## JavaScriptから画像を生成する

`generate.js`は通常のJavaScriptから`generate_image`を直接呼びます。providerとmodelは引数または例の既定値で明示し、出力先ディレクトリは例が作成します。生成された一枚の画像を保存し、metadataを表示します。

```sh
./noe run examples/image/generate.js
```

既定値は`openrouter` / `openai/gpt-image-2.5-flare`です。OpenAI APIキーを使う場合は対応するmodel IDを指定します。

```sh
./noe run examples/image/generate.js openai-api gpt-image-2.5-flare ./tmp/image.png
```

画像生成はproviderのAPIを呼ぶため、利用料が発生する場合があります。ChatGPTサインインとOpenCode Goは画像生成に対応していません。エラー時に別providerへ自動で切り替えません。APIの制約・model選択・履歴と観察については[画像ガイド](../../docs/agent-images.md)を参照してください。
