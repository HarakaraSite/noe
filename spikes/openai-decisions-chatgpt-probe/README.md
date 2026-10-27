# ChatGPT OAuth versus OpenAI Decisions probe

## Scope
Two separately authorized minimal requests to the official POST https://api.openai.com/v1/decisions endpoint: first existing ChatGPT OAuth, then the existing OpenAI API key. Each authentication mode was explicitly selected; neither request used fallback. No account switching, credential dumping or product implementation.

The Go probe reuses internal/credentials.ChatGPT.Token, including normal locked refresh if needed. It never prints or stores access/refresh/ID tokens. Only the fixed official Decisions endpoint receives the access token; redirects are not followed. Result files contain selected non-secret response fields.

## Observation
At 2026-10-26T13:05:13Z, gpt-6-luna with one predicate question returned:
- HTTP 401
- error.type: rejected_by_access_enforcement
- error.code: no_matching_rule
- error.message: Unauthorized
- request ID: req_e15ed80bd5fe4798a59e296abc284d99
- authenticatedDecisionObserved: false

See result.json.

## Control
A minimal native openai-chatgpt / gpt-6-luna Responses task returned exactly OK and completed successfully at 2026-10-26T13:07:51Z. See responses-control.json.

An earlier direct noe:model call was rejected locally by the owning-agent/model guard before HTTP. It was not an OpenAI authentication failure and is recorded as zero HTTP requests. The control then used an authorized native child task; there was no attempt to bypass the guard.

## Interpretation
The tested existing ChatGPT OAuth path works for Responses but was rejected for Decisions. This supports using existing openai-api credentials for the initial Decisions integration, not advertising openai-chatgpt support on the basis of Responses access. It is not a proof about every account, OAuth scope or future server policy. A separately authorized API-key follow-up then succeeded, as recorded below.

## API-key follow-up
At 2026-10-26T13:13:00Z, the identical one-question payload with the existing OpenAI API key returned HTTP 200:
- model: gpt-6-luna
- answer: predicate / flag_true / probability 1
- usage: 158 input tokens, 0 output tokens
- authenticatedDecisionObserved: true
- request ID: req_224367147ccc4607a44e2801e2d4806f

See api-key-result.json. This confirms the endpoint/model/request works with the configured API key while the tested ChatGPT OAuth request was rejected. No key material was printed or saved in probe results.

## Reproduction
From the NOE source root:
```sh
go run ./spikes/openai-decisions-chatgpt-probe
# Explicitly use the API key instead (not a fallback):
go run ./spikes/openai-decisions-chatgpt-probe --api-key
```
Each command makes one live Decisions request. The default uses NOE_OPENAI_CHATGPT_CREDENTIALS_FILE or the standard XDG OAuth path; --api-key uses NOE_OPENAI_API_KEY_FILE or the standard XDG openai-api-key file. Standard OAuth refresh may update that credential record. The program prints only the sanitized report; result.json is the retained report from the observed run, not automatically overwritten by this command.

No commits or pushes were performed for this probe.
