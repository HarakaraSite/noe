# agentの画像読み取り・生成

`noe:image`はローカル画像をagentへ添付し、または対応providerで一枚の画像を生成して保存するAPIです。JavaScriptとhookにはmetadataだけを返し、画像bytesはGo host内で扱います。

## 画像をagentに読ませる

`read_image(path, options?)`はagentの`run_js`内で使います。呼び出し時点でpathのファイルを読み、そのtool callに画像を関連付けて添付します。成功時に返る値は`imageId`、絶対path、MIME type、byte数、幅・高さ、detailなどのmetadataです。モデルにはtool完了後の次の要求で画像が渡るため、戻り値を`console.log`する必要はありません。

```js
// Attach a local screenshot to this agent's current model step
const { read_image } = await import("noe:image");
await read_image("./tmp/screenshot.png", { detail: "auto" });
```

相対pathは`run_js`プロセスの呼び出し時点のworking directory基準です。`detail`は`auto`（既定値）、`low`、`high`です。対応する入力形式はPNG、JPEG、WebPです。実際のファイル内容から形式を判定し、SVGのラスタライズ、URL読込み、形式変換はしません。読み取ったbytesは固定snapshotとして保持されます。

通常のトップレベル`noe run`・`eval`スクリプトには画像を見るmodel stepがないため、そこから`read_image`を呼ぶことはできません。トップレベルJSでは`run_agent`を起動し、instructionにpathを渡してください。agentが`run_js`から読み取ります。モデルにvision能力があるだけではNOEの画像添付は有効になりません。

## 画像を生成する

`generate_image`は通常のJavaScriptとagentの`run_js`の両方から呼べます。provider・model・prompt・outputを毎回指定します。agent modelのproviderやmodelを引き継ぎません。

```js
import { generate_image } from "noe:image";

const image = await generate_image({
  provider: "openrouter",
  model: "openai/gpt-image-2.5-flare",
  prompt: "A small red sailboat on a calm blue sea, no text.",
  output: "./tmp/sailboat.png",
  parameters: { quality: "low", size: "1024x1024" },
});
console.log(image); // metadata only
```

初期対応の組み込み経路は、OpenRouterの`openai/gpt-image-2.5-flare`とOpenAI APIキーの`gpt-image-2.5-flare`です。ChatGPTサインイン経路とOpenCode Goは画像生成に対応していません。非対応や失敗時に別provider/modelへ自動fallbackしません。生成はproviderへのAPI要求で、利用料が発生する場合があります。

一回の呼び出しで生成する画像は一枚です。`output`の親directoryは呼び出し側で用意してください。生成応答の画像を検証してから一時ファイル経由で保存先を置き換えるため、応答検証・書込みが失敗したとき既存ファイルを途中までの内容にしません。成功結果には絶対path、MIME、byte数、寸法、要求provider/model、取得できたresponse model・request ID・usageを含みます。画像bytesやBase64は返しません。生成した画像をagentに見せるときは、agentの`run_js`で`read_image(result.path)`を呼びます。

## 画像modelの選択

`list_models({kind: "image"})`は設定済みcatalogから画像model候補を返します。catalogは選択のためのmetadataで、認証情報を読み込まず、providerへ接続もしません。catalogが空でも明示したprovider/modelを使えます。候補例を確認する場合は次のように呼びます。

```js
import { list_models } from "noe:agent";
console.log(await list_models({ kind: "image" }));
```

`parameters`はprovider固有のoption用です。`provider`、`model`、`prompt`、`n`、保存先、認証やheaderなどNOEが管理する値は上書きできません。

入力・認証・利用枠・拒否・非対応・通信・応答・保存・取消しの失敗は`ImageError`としてPromiseをrejectします。`name`は`ImageError`、`code`と`message`は常にあり、provider code/message、HTTP status、request IDは取得できた場合に含みます。

```js
try {
  await generate_image({ provider, model, prompt, output });
} catch (error) {
  if (error.name === "ImageError") {
    console.error(error.code, error.message, error.statusCode, error.requestId);
  }
  throw error;
}
```

## Hookで添付を選ぶ

`afterTool`では通常のtool結果を`event.result`、その呼び出しで読んだ添付metadataを`event.images`で受け取ります。画像のmetadataにもbytesやBase64は含みません。既存の`undefined`または`{result}`返却は添付を保持します。`{result, imageIds}`で、現在の候補から残す画像だけを選べます。

```js
export async function afterTool(event) {
  if (event.toolName !== "run_js") return;
  const keep = event.images.slice(0, 1).map(image => image.imageId);
  return { result: event.result, imageIds: keep };
}
```

`imageIds: []`はそのtoolの添付をすべて除外します。部分選択は元の順序を保ちます。重複ID、他toolやrunのID、未知のID、前のhookで除外したIDの再追加はエラーになります。画像を除外する場合も通常の`result`は個別に維持・置換できます。

通常の`afterTool`例外や不正な`imageIds`は、その登録の置換だけを不採用にして後続hookと本体を続けます。元の画像、または先行hookで成立した画像選択・結果を維持し、回復したhook失敗だけを理由に画像を破棄しません。すでに除外した画像を復活させるわけではありません。tool自身の実行失敗・取消しや、checkpointを確定できない障害による破棄とは別です。[失敗通知と保持結果](hooks.md#失敗の局所化と通知)を参照してください。

## 履歴・queue・context

モデルへ渡した画像は、tool結果をすべて確定した後のuser image messageとして履歴に記録されます。履歴にはimage IDを置き、画像bytesはhostが保持します。同じagent instanceの`queue_agent`は、確定し履歴に残っている画像を含む会話を引き継ぎます。新しいchild agentは独立した履歴を持つため、画像bytesは暗黙に共有されません。必要なchildにはpathを伝え、そのchild自身に`read_image`させてください。

自動context trimmingはassistant/tool/imageを一つの完全blockとして扱い、受理されたtrimで削除block全体を外します。最新の完全blockは通常の履歴trimと同様に保護されます。checkpointがhostに受理される前に失敗した画像は履歴へ確定せず、破棄されます。受理済みtrimで除かれた画像参照は解放されます。読取り元や生成先の利用者ファイルは削除しません。

画像tokensは`imageTokens`としてテキストのJSON bytes/4概算とは別に表示します。model固有tokenizer値ではなく、次の明示的fallbackです。

```text
ceil(ceil(width / 32) * ceil(height / 32) * 1.2)
```

`detail: "low"`は最大辺を512pxに縮小した寸法で計算します。`auto`と`high`は原寸を使います。画像tokenizer・provider側のresize・請求額とは一致しないことがあり、料金計算には使えません。

## JSONLとlive observation

`image.read`は画像のimage ID・path・MIME・byte数・寸法を、生成の`image.started` / `image.completed` / `image.failed` / `image.cancelled`はpath・model・duration・取得できたusageやエラーmetadataを記録します。`tool.finished`ではhook処理前の`rawImages`と採用後の`images`を通常のresultと分けて記録します。イベントに画像Base64は入りません。保存したJSONLはmetadataと実行経過を観察できますが、画像データの完全な再生記録ではありません。

```sh
./noe run --observe --events=images.jsonl examples/image/read.js ./tmp/screenshot.png
./noe observe images.jsonl
```

`--observe`は読み取り・生成の進行を短く表示します。APIを含む利用例は[examples/image](../examples/image/README.md)、イベント表示の一般仕様は[観察ガイド](observe.md)を参照してください。
