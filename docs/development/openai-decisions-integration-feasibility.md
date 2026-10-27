# Integrating OpenAI into noe:decision: feasibility

## Conclusion
Integration is feasible. Keep the existing module and decide() operation; do not create a separate OpenAI decision API. The current architecture already routes a DecisionRequest through a provider registry and a DecisionProvider interface. OpenAI needs a provider adapter, not a second task/execution/gate framework.

This is a feasibility recommendation, not approved API requirements or implementation. No source code, tests, provider calls, commits or pushes were performed.

## Verified local seams
- internal/decision/decision.go: shared Request, Response and Provider interface. Request validation currently hardcodes openrouter; response validation currently expects a named answers object.
- internal/decision/js/decision.js: public decide({provider,model,state,questions}); JS also hardcodes openrouter.
- agent/types/noeagentlib.d.ts: DecisionOptions fixes provider to openrouter; DecisionResponse fixes answers to a record.
- internal/agent/provider/config.go: Registry.Decide already resolves the selected provider and invokes its decision.Provider implementation.
- internal/agent/provider/decision.go: existing OpenRouter transport retains the original response JSON and forwards unknown question fields.
- agent/model/service.go and internal/agent/models.go: existing model service, task ownership/cancellation, provider admission, counters and decision events can remain shared.
- docs/openai.md: existing openai-api provider, API-key credential file and NOE_OPENAI_API_KEY_FILE are available. Do not introduce a new openai provider ID merely for Decisions.

## Contract comparison
OpenRouter/Jev:
- state; named questions object.
- noul with instructions and optional true/false criteria.
- choice with criteria as named options.
- score with ordered criteria.
- named answers object; choice/score probabilities are maps.
- raw provider response fields are part of the current public contract.

OpenAI:
- input; questions array, with name for identification.
- predicate; choice with choices array; score with levels array.
- answers array; probability distributions are arrays.
- per-question refusal, model and usage metadata.
- choice values may be booleans as well as strings; common named-object option keys cannot preserve native boolean values by silently stringifying them.
- score is a probability-weighted zero-based level index, not an integer category.

Official sources:
- https://openrouter.ai/docs/api/api-reference/alphadecisions/submit-a-decisions-request
- https://openrouter.ai/docs/guides/community/jev-tutorial
- https://developers.openai.com/api/docs/guides/decisions
- https://developers.openai.com/api/reference/typescript/resources/decisions/methods/create/index.md

## Integration levels considered
1. Same decide() entry, native provider-specific arguments/results. Smallest adapter, but callers still branch on request/answer shapes. Feasible, though weaker integration.
2. Same entry plus a bounded common text/JSON and basic-question surface, with provider adapters. Recommended: existing Jev call sites remain usable and workflow code can retain result.answers.<question>.choice.
3. Completely hide every provider difference, including all multimodal/history/cache/extension features. Not recommended for this revision: unnecessary abstraction and potential semantic loss.

## Recommended bounded common surface
Retain decide({provider, model, state, questions}) and named answers for ordinary requests.
- OpenRouter path remains behaviorally unchanged, including raw/unknown field retention.
- New openai-api path translates the common request to OpenAI's native form and normalizes its answers before returning through the shared service.
- Preserve the original OpenAI response separately (exact public field name to be settled) so normalization does not discard native answer fields, refusal details, usage or future fields.
- Questions map keys become native OpenAI question names and map back to the same answer keys.
- Existing choice criteria become choices; their string values/descriptions are retained. Normalize probability entries back to a map for the common string-choice surface.
- Existing score criteria become ordered levels with the same zero-based meaning; preserve order. OpenAI's level/option/question limits apply to OpenAI calls, not retrospectively to Jev.
- Existing noul can map to predicate; preserve the logical meaning, including both true and false criteria rather than dropping one. Map predicate probability to the documented common yes-probability field.
- Per-question refusal remains an explicit refusal answer, not false/zero/proceed.
- Text and ordinary JSON evidence can be translated, but the exact JSON encoding must be documented. Do not infer that data objects containing role/image_url fields are executable message/image parts.
- Native multimodal arrays, boolean choices, conversation history, cache options and unknown provider-specific question fields are not automatically interchangeable. Preserve existing Jev behavior; explicitly scope or document OpenAI support rather than silently dropping or coercing unsupported inputs.

This does not require a new strict agent-result protocol, Git controller or workflow state machine. The adapter only bridges external API schemas; the parent agent still owns the workflow judgments.

## Compatibility and honesty
Existing OpenRouter scripts must not need edits or lose unknown response fields. Normalizing the existing Jev path or replacing its response envelope globally would violate its current raw-response contract. Normalize only the newly introduced OpenAI path for the common surface, with access to native response; state that distinction in documentation.

Identical request shapes do not imply interchangeable model behavior, latency or confidence calibration. Preserve vendor-provided numbers, do not rescale them into a supposed universal confidence score, and do not set a global workflow pass threshold.

## Authentication boundary
Use existing openai-api API-key credentials as the documented baseline. The official Sign in with ChatGPT inference documentation specifies the public Responses endpoint for ChatGPT-plan usage and Responses scopes; it does not establish Decisions access:
https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference
https://developers.openai.com/siwc/token-sharing-open-source

Do not advertise openai-chatgpt Decisions support or silently fall back to API-key billing. Parent/implementation/review agents may continue using openai-chatgpt while decision calls explicitly use openai-api.

## Requirements decisions remaining
- Approve the bounded common-surface approach or choose provider-native shapes under the same entry instead.
- Define the exact common request/normalized/native-response access contract and the initial supported input/question surface.
- Define workflow stage usage separately from transport integration: where a call is required and how parent judgment handles refusal, insufficient evidence or disagreement.

## Follow-up empirical OAuth probe
At 2026-10-26T13:05:13Z, one minimal gpt-6-luna Decisions request using existing ChatGPT OAuth returned HTTP 401, rejected_by_access_enforcement / no_matching_rule. A minimal native openai-chatgpt Responses control succeeded with OK. The tested ChatGPT OAuth path therefore cannot currently be advertised for Decisions; use openai-api as the documented initial integration route unless further official support is established. This conclusion concerns the tested credential/account, not all possible scopes or future policies. No API-key fallback was attempted. Details: [probe record](../../spikes/openai-decisions-chatgpt-probe/README.md).

## Follow-up empirical API-key probe
At 2026-10-26T13:13:00Z, the identical minimal gpt-6-luna Decisions request explicitly using the configured OpenAI API key returned HTTP 200, predicate flag_true with probability 1, and usage of 158 input / 0 output tokens. No fallback was used. The documented openai-api route is now verified in this environment, while the separately tested ChatGPT OAuth route returned HTTP 401. Result: [api-key-result.json](../../spikes/openai-decisions-chatgpt-probe/api-key-result.json).
