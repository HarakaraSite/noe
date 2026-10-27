# OpenAI Decisions API research

Research date: 2026-10-26. This is a research note, not settled implementation requirements. No workflow, implementation, authenticated Decisions request, commit or push was performed.

## Primary sources
1. Guide: https://developers.openai.com/api/docs/guides/decisions
   Full official Markdown was fetched directly: https://developers.openai.com/api/docs/guides/decisions.md
2. API reference: https://developers.openai.com/api/reference/typescript/resources/decisions/methods/create/index.md
3. Release notes: https://developers.openai.com/api/docs/changelog
   Full Markdown: https://developers.openai.com/api/docs/changelog.md
4. Pricing: https://developers.openai.com/api/docs/pricing
5. ChatGPT/API billing separation: https://help.openai.com/en/articles/9039756-managing-billing-for-chatgpt-and-the-api-platform

Official documentation was discovered through web search, then the guide and release notes were fetched from the official host. The API reference was retrieved through Exa page contents. No third-party report is required for the conclusions below.

## Confirmed availability
OpenAI has a dedicated Decisions API. It is not merely Structured Outputs or a renamed Evals/Graders API.
- Endpoint: POST https://api.openai.com/v1/decisions
- Public beta: October 6, 2026.
- General availability: October 22, 2026.
- Currently supported model: gpt-6-luna only.
- OpenAI describes typed answers as approximately 10x faster than Responses. This is a vendor claim, not a measured guarantee for NOE workflows.

## Request and answer contract
Required request fields are model, input, questions.
- input is shared evidence: a text string or ordered messages/tool-call history. User content can contain input_text, input_image and input_json.
- questions is an array of 1-400 questions. Question names identify answers.
- predicate: estimated probability (0-1) that the supplied condition is true.
- choice: choose among 2-255 distinct supplied values (string or boolean); returns choice, option probabilities and confidence.
- score: evaluate 2-10 ordered levels; returns probability-weighted level-index score, per-level probabilities and confidence. This is not necessarily an integer category.
- answers is an array in question order. The response also carries model and usage metadata.
- Any question may return type refusal instead of an answer, even when other questions receive answers.

Conversation/tool history is evidence; Decisions does not execute tools or run an agent loop. Input text/images/JSON may carry requirements, reviews, diff excerpts and command outcomes. It cannot independently inspect a repository or establish that a reported test actually ran.

The native response has no general-purpose written-rationale field. Use parent/reviewer agents for explanations. The published request parameter list has no effort or reasoning.effort setting; do not automatically apply the workflow's Luna/max configuration to this endpoint.

## Uncertainty and limits
The guide recommends labeled application examples and thresholds selected according to false-positive/false-negative costs. Confidence and probability are not mechanical proof of correctness. A fixed threshold should not be invented as a universal workflow completion rule.

Images may use base64 data URLs or publicly accessible HTTP(S) URLs. File IDs, audio and item references are not supported. Hosted images, input_json, caching and up to 400 questions are GA features. Advanced image/conversation/cache/enterprise capabilities need not all become initial workflow-revision requirements.

## Pricing and authentication
The guide lists uncached Decisions input at USD 0.10 per million tokens with gpt-6-luna and no charge for output tokens. Cache reads/writes follow standard Luna cache rates; regional processing premiums and long-context multipliers apply. Responses using the same model have their own applicable pricing.

Official examples authenticate using an OpenAI API key (Authorization: Bearer OPENAI_API_KEY). ChatGPT subscriptions and API billing are separate. Compatibility between NOE's existing openai-chatgpt OAuth path and /v1/decisions was not established by this research. Do not assume ChatGPT subscription access pays for or authenticates this API; the API-key route is the documented baseline.

## Implications for NOE and the agent-first workflow
The existing local noe:decision interface was previously verified as OpenRouter-only. OpenAI's native contract is different:
- OpenAI uses input and a questions array; existing OpenRouter calls use state and a named questions object.
- OpenAI's condition type is predicate; the Jev/OpenRouter example uses noul.
- OpenAI returns an answers array; the existing Jev workflow reads named answers.
Changing only the provider/model string is insufficient. Choose the public NOE request/response contract deliberately during requirements definition rather than silently imposing an adapter that changes semantics or loses metadata.

OpenAI Decisions is model judgment expressed in typed answers, not deterministic validation. The agent-first principle remains compatible with exposing it as a tool the parent can call. Parent/review agents still provide evidence, interpret refusals/uncertainty and explain decisions. Do not rebuild the rejected Git/state-machine/schema framework in order to consume this API.

## Candidate requirements directions, not yet agreed
- Add a documented callable OpenAI Decisions capability to NOE, with the documented API-key authentication route and basic native question/answer handling.
- Make it available to the workflow parent for the intended stage-completion judgments, supplying requirements plus actual review/test/diff evidence.
- Return refusal/uncertainty/provider errors honestly to the parent; do not silently substitute Jev or Responses.
- Keep implementation/review/Git coordination agent-led. Do not add mandatory JSON protocols for those agents or a custom mechanical gate engine.
- Validate transport/native result handling with focused tests and a minimal real API call when credentials are configured; use the existing first workflow to implement the settled revision requirements.

Before settling requirements, decide:
1. Whether every named stage must request a Decisions judgment, or the parent uses it only when useful; how the parent handles disagreement or insufficient evidence.
2. Whether the scope includes both reusable NOE API support and workflow instructions/examples (rather than a workflow-local HTTP call only).
3. The supported input/question surface and credential configuration for the initial revision.

## Follow-up empirical OAuth probe
At 2026-10-26T13:05:13Z, one minimal gpt-6-luna Decisions request using existing ChatGPT OAuth returned HTTP 401, rejected_by_access_enforcement / no_matching_rule. A minimal native openai-chatgpt Responses control succeeded with OK. The tested ChatGPT OAuth path therefore cannot currently be advertised for Decisions; use openai-api as the documented initial integration route unless further official support is established. This conclusion concerns the tested credential/account, not all possible scopes or future policies. No API-key fallback was attempted. Details: [probe record](../../spikes/openai-decisions-chatgpt-probe/README.md).

## Follow-up empirical API-key probe
At 2026-10-26T13:13:00Z, the identical minimal gpt-6-luna Decisions request explicitly using the configured OpenAI API key returned HTTP 200, predicate flag_true with probability 1, and usage of 158 input / 0 output tokens. No fallback was used. The documented openai-api route is now verified in this environment, while the separately tested ChatGPT OAuth route returned HTTP 401. Result: [api-key-result.json](../../spikes/openai-decisions-chatgpt-probe/api-key-result.json).
