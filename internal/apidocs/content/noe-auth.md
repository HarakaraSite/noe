# `noe:auth`

### API information

Public exports: `save_chatgpt_credentials`.

Reference: `noe:auth`. Owner: noeagentlib.

Type declarations: `agent/types/noeagentlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires agent/auth.Bind. NOE's CLI installs it in the main runtime and omits it from Workers; an embedding host selects its bindings explicitly.

Public-definition source: agent/auth/binding.go, internal/credentials/js/auth.js. Reading this reference evaluates no target module and makes no provider request.

`noe:auth` exports `save_chatgpt_credentials`. It completes persistence of a ChatGPT sign-in result produced by the authentication helper; it is not a general-purpose OAuth client. The module is available in the main runtime, not Workers. Saving validates the sign-in over OpenID discovery/JWKS requests and writes a private local credential record. No access or refresh token is returned to JavaScript.

## save_chatgpt_credentials

```js
import { save_chatgpt_credentials } from "noe:auth";
```

`save_chatgpt_credentials(result) -> Promise<SavedAccount>` accepts an object with the following shape:

```js
{
  file: "path/to/credentials.json",
  clientId: "...",
  hostId: "...",
  nonce: "...",
  tokens: {
    access_token: "...",
    refresh_token: "...",
    id_token: "...",
    token_type: "Bearer",
    expires_in: 3600,
    scope: "chatgpt.tokens.use.direct ..."
  }
}
```

The result is validated, including the ID-token signature, issuer, audience, nonce, expiry, and required `chatgpt.tokens.use.direct` scope. The `file` path may be relative to the runtime's working directory. On success the Promise returns `{file, email?, expiresAt}`. The credential record is written by the Go host; token values are not returned. An existing credential file for a different account or client is rejected; select a separate file for another account.

Wrong argument shape throws `TypeError`. Incomplete or invalid sign-in data, discovery/JWKS network failures, account mismatch, or filesystem errors reject the Promise. Cancellation follows the current evaluation's lifetime. This operation performs external network access and a local credential-file write.

```js
import { save_chatgpt_credentials } from "noe:auth";

// `result` is supplied by the existing sign-in helper.
const account = await save_chatgpt_credentials(result);
console.log(account.email, account.expiresAt);
```
