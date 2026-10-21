# `noe:model`

`noe:model` exposes a model catalog and one-shot chat generation through the
configured model service. It does not start an agent loop. The module is
available only when the host installs a model binding; the embedded catalog
documents the API even in a profile where it is not installed. The binding
also re-exports `decide` and `generate_image`; see the `noe:decision` and
`noe:image` reference topics for those operations.

## generate

```js
import { generate } from "noe:model";
```

`generate({provider, model, messages, tools?, cacheKey?, maxTokens?, effort?})`
returns a provider response object with `message`, `finishReason`, and
optional `usage`. `provider` and `model` must be nonempty strings. `messages`
must be a nonempty array of JSON message objects. `tools` is an optional array
of provider-compatible tool definitions. `maxTokens`, when present, must be a
nonnegative integer; `effort` and `cacheKey` are forwarded to the selected
provider. The response message and usage retain their provider-compatible JSON
shape; handle provider-specific fields according to the selected adapter.

The configured model service receives this request directly. A custom
provider that does not implement the operation returns its unsupported error;
NOE does not silently choose another provider. In a managed agent task, the
call belongs to the task's scope and cancellation and is subject to its shared
group usage limits.

```js
import { generate } from "noe:model";

const response = await generate({
  provider: "openrouter",
  model: "provider/model-id",
  messages: [{role: "user", content: "Say hello in one sentence."}],
  maxTokens: 128,
});
console.log(response.message);
```

## list_models

```js
import { list_models } from "noe:model";
```

`list_models({provider?, kind?} = {}) -> Promise<Model[]>` returns configured
catalog metadata without loading credentials or contacting a provider. `kind`
is `agent`, `decision`, or `image`. Each item contains `provider`, `model`,
`kind`, and any available `description`, `tags`, `capabilities`,
`contextWindow`, `maxOutputTokens`, and `efforts`. Missing metadata means
unknown. The catalog is selection guidance, not a benchmark or allowlist;
directly specified models are not limited to listed entries.

```js
import { list_models } from "noe:model";

for (const model of await list_models({kind: "image"})) {
  console.log(model.provider, model.model, model.capabilities);
}
```
