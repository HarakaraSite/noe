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

Existing API-key generation routes include OpenRouter's `openai/gpt-image-2.5-flare`
and OpenAI API's `gpt-image-2.5-flare`. The public plan-sharing `openai-chatgpt`
route and OpenCode Go do not support image generation; the separate
`openai-codex-images` subscription route is described below. NOE does not switch
provider/model after an unsupported operation or failure. API-key generation
can incur provider charges; subscription images consume their own included limits.

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

## Subscription generation and original-image editing

`openai-codex-images` provides the Codex-compatible ChatGPT subscription image
route. It is separate from `openai-chatgpt` public plan-sharing authentication.
Create a dedicated NOE-owned login with:

```sh
noe run --no-inst scripts/auth-codex-images.js login
```

The helper uses `${XDG_CONFIG_HOME:-$HOME/.config}/noe/credentials/openai-codex-images.json`
unless an explicit file or `NOE_CODEX_IMAGE_CREDENTIALS_FILE` is selected.
Run these commands with the NOE distribution root as the actual cwd. Keep
`login` running in VM terminal A and open its authorization URL in the Mac
browser. After signing in, copy the full redirected
`http://127.0.0.1:PORT/auth/callback?...` URL from the browser's address bar,
even if that page cannot connect. In VM terminal B, run:

```sh
noe run --no-inst scripts/auth-codex-images.js complete
```

Paste the URL into standard input, press Enter, then Ctrl+D on the empty line.
The helper relays the callback to terminal A; check terminal A for credential
save success. No SSH port forwarding, Python or separate curl command is
required. `Callback delivered` only confirms delivery; native code exchange,
identity validation and saving still belong to terminal A.

The helper uses the same `login` / `complete` steps and terminal messages as
the conversation helper. Image login tries port 1455, then registered alternate
1457 automatically if the first is unavailable. It does not stop the existing
listener. If both are occupied, finish a previous login and retry. Copy the
actual browser redirect URL, including its selected port. The fixed image
OAuth client has a registered redirect set; the conversation helper's dynamic
registration supports arbitrary available loopback ports. Do not substitute an
arbitrary image callback port based on the conversation flow's contract.

To select another image credential file, use the same file in both commands:

```sh
noe run --no-inst scripts/auth-codex-images.js login /path/to/image-credentials.json
noe run --no-inst scripts/auth-codex-images.js complete /path/to/image-credentials.json
```

Both commands also honor `NOE_CODEX_IMAGE_CREDENTIALS_FILE`. The adjacent
`<credential-file>.pending/` directory has mode 0700. Each native attempt owns
one mode-0600 JSON record containing only its loopback receiver; its random
attempt ID is the filename. `complete` selects the newest published record.
These records contain no authorization code or tokens. The helper checks the
callback origin/path before delivery, and native auth verifies state and the
sign-in result. Normal completion and reported
login failures remove that attempt's pending record. Abrupt interruption can
leave stale pending metadata: start a fresh `login`, which publishes a new
record, then use the new browser redirect URL. `complete` explains missing pending metadata,
URL mismatch and an unreachable/rejecting listener without printing the pasted
URL or private response data.

NOE stores and refreshes its own grant; it does not import the active Codex
application's refresh token. Host-supplied OAuth options can select a trusted
issuer/client/callback profile. Local browser callbacks and optional SSH
forwarding continue to work as before.

For an explicitly selected existing Codex login, configure both its path and
read-only mode in a separate provider configuration:

```json
{
  "providers": {
    "openai-codex-images": {
      "oauthMode": "external-readonly",
      "oauthFile": "/absolute/path/to/codex/auth.json"
    }
  }
}
```

External mode never refreshes or writes the selected file; renew an expired
login through its owner. Managed mode serializes refresh/rotation across NOE
processes. A 401 may recover once using a newer same-identity credential.
Generation/editing otherwise has no automatic retries or account/model fallback.
Subscription limits are distinct from API-key charges and are not unlimited.

```js
import { generate_image, edit_image } from "noe:image";
const original = await generate_image({
  provider: "openai-codex-images", model: "gpt-image-2",
  prompt: "A mint green square on white.", output: "images/original.png",
  parameters: { quality: "low", size: "1024x1024" },
});
const revised = await edit_image({
  provider: "openai-codex-images", model: "gpt-image-2",
  images: [original.path], prompt: "Make the square orange.",
  output: "images/revised.png",
});
console.log(revised.path, revised.mimeType, revised.width, revised.height);
```

Callers create the output directory. Inputs are ordered local file snapshots
read through the invoking filesystem. Source bytes are sent to the editing
service; this is not text regeneration. A separate output path retains both
files. Actual output dimensions/MIME/bytes are decoded from the response; the
requested size need not equal the result. The direct PNG probe returned
1254x1254 from a 1024x1024 request. Editing does not guarantee exact preservation
of all other pixels. PNG is the live-verified input format; JPEG/WebP and
multiple input images require service-specific validation beyond local decoding.
Masks and API-key/OpenRouter editing are not implemented by this slice.

For model inspection, call `read_image(original.path)` inside an actual agent
`run_js`, wait for its next model turn, then edit in a later tool call and use
`read_image(revised.path)` again. Ordinary run/eval generation/editing does not
itself create vision context. No new conversation-model tool is introduced.

`ImageError.operation` identifies generate/edit/read and `stage` distinguishes
input/authentication/request/response/save. Existing `code` categories remain.
Known provider usage/request identity is retained on cancellation and errors,
with consumption accounted once. Files already atomically committed before
cancellation remain; a cancelled operation does not start a later save.
