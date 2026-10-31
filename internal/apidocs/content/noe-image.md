# `noe:image`

### API information

Public exports: `edit_image`, `generate_image`, `read_image`.

Reference: `noe:image`. Owner: noeagentlib.

Type declarations: `agent/types/noeagentlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires the model/image binding in the main runtime. Vision attachments additionally require an agent tool context; file access follows the selected filesystem.

Public-definition source: internal/image/js/image.js, internal/modelbinding/binding.go. Reading this reference evaluates no target module and makes no provider request.

`noe:image` exports `read_image`, `generate_image`, and `edit_image`. The module is available in the main runtime, not Workers. Image bytes stay in the Go host; JavaScript receives metadata. Image generation makes an external provider request and writes the generated file to the requested output path.

## read_image

```js
import { read_image } from "noe:image";
```

`read_image(path, options?) -> Promise<ImageMetadata>` resolves `path` against the current working directory. `options` supports only `detail`, which is `"auto"` by default and may be `"low"` or `"high"`. Supported input formats are PNG, JPEG, and WebP.

In an agent's `run_js` call, the image is attached to the next model request after the tool completes. The JavaScript result contains metadata such as `path`, `mimeType`, `bytes`, `width`, `height`, and `detail`; it does not contain image bytes or base64. A regular `noe run`/`eval` or Worker has no model vision context to receive the attachment.

```js
import { read_image } from "noe:image";

const metadata = await read_image("artifacts/diagram.png", {detail: "high"});
console.log(metadata.width, metadata.height, metadata.mimeType);
```

## generate_image

`generate_image({provider, model, prompt, output, parameters?}) -> Promise<GeneratedImage>` requires nonempty `provider`, `model`, `prompt`, and `output`. The provider and model must be explicit; use `list_models({kind: "image"})` to inspect configured candidates. A relative output path is resolved from the current working directory. Optional `parameters` must be a JSON object of provider-specific generation options.

This sends an external provider request and writes the generated image to the requested output path.

Image parameters cannot override transport or identity fields such as `model`, `prompt`, `provider`, `output`, `headers`, `api_key`, `authorization`, `n`, or image input fields. Unknown top-level options, reserved parameter names, non-JSON values, and cyclic data reject with an `ImageError` whose `code` is `"input"`.

The returned object contains image metadata and provider information, including `path`, `mimeType`, `bytes`, `width`, `height`, `provider`, and `model`; `responseModel`, `requestId`, or `usage` may also be present. It does not expose generated image bytes. Provider errors reject with `ImageError`; fields may include `code`, `providerCode`, `providerMessage`, `statusCode`, and `requestId`. The API does not choose credentials or a provider implicitly.

```js
import { generate_image } from "noe:image";

const result = await generate_image({
  provider: "openai-api",
  model: "provider/image-model",
  prompt: "A simple blue circle on a white background",
  output: "artifacts/circle.png",
  parameters: {quality: "low"},
});
console.log(result.path, result.width, result.height);
```

## ImageError

Image failures reject with an `Error` named `ImageError`. `message` describes the failure and `code` classifies it; optional provider metadata is exposed on the error object. Do not treat provider message text or optional fields as a stable schema. Calls made by a managed agent belong to the current run and are cancelled with it.

## edit_image

`edit_image({provider, model, prompt, images, output, parameters?}) -> Promise<GeneratedImage>`
sends actual input-image bytes for editing and saves one result. `images` is a
nonempty ordered array of local file paths in the execution filesystem. Inputs
are snapshotted before the provider call. Output uses the same metadata and
atomic saving contract as generation; use a different output path to retain
both the original and revision. The output parent directory must exist.

```js
import { edit_image, read_image } from "noe:image";
const revised = await edit_image({
  provider: "openai-codex-images", model: "gpt-image-2",
  images: ["images/original.png"], prompt: "Make the square orange.",
  output: "images/revised.png",
});
await read_image(revised.path); // Requires an active agent run_js tool call.
```

The built-in editing route is `openai-codex-images` using explicitly selected
ChatGPT subscription credentials. Existing `openai-chatgpt` plan-sharing
credentials are a separate route. API-key/OpenRouter editing and mask editing
are unsupported; NOE does not substitute text regeneration or another account.
PNG was live-verified for the direct route. The local codec accepts PNG/JPEG/WebP;
service acceptance of an additional format or input count must be confirmed.
Options are provider-specific, and returned dimensions are decoded from the
actual output rather than copied from requested size. Editing does not guarantee
pixel-exact preservation outside the requested change.

Image errors additionally carry `operation` (`generate`, `edit`, `read`) and
`stage` (`input`, `authentication`, `request`, `response`, `save`) when known.
Known usage and request identity survive cancellation and provider/response/save
errors. Cancellation does not start a later save or erase known consumption.
No automatic retry occurs after ambiguous transport failure or quota/refusal.
