# OpenAI Fast mode: configuration and live validation

Checked on 2026-11-16 with NOE v2.26.1. Source probes used revision
`ae631a9dd28272b7883cd45f1d35357b02b03446`. The
[usage guide](../openai.md#fast-mode) is the configuration entry point;
[sanitized results](openai-fast-mode-results.json) preserve the measurements.

## Result

NOE's existing provider `parameters` can request Fast without a source change.
For `openai-chatgpt`, use `service_tier: "priority"`. The tested account's model
catalog advertised `priority` as Fast for `gpt-6.1-sol`. The literal `fast`
was rejected with HTTP 400, while `priority` completed successfully.

The comparison observed lower latency with `priority` on the existing public
ChatGPT plan-sharing route. All responses nevertheless reported `default`.
The earlier interpretation that this response value proved Fast was inactive
is withdrawn. These measurements support using the configuration; they do not
establish an authoritative served-tier signal or a universal speed guarantee.

## Configuration and transport

The local configuration contains:

```json
{
  "providers": {
    "openai-chatgpt": {
      "parameters": {"service_tier": "priority"}
    }
  }
}
```

The [provider configuration](../providers.md) merges this into the embedded
defaults. Its static parameters become the Responses request body through
[`configuredResponsesRequestBody`](../../internal/agent/provider/responses.go).
The [OpenAI adapter](../../internal/agent/provider/openai.go) passes that JSON
to the SDK. No additional agent option is required. A fresh host loads the
updated file; an existing host retains its loaded configuration.

| Path | Authentication | Responses endpoint | Tier spelling |
| --- | --- | --- | --- |
| NOE `openai-chatgpt`, verified here | Existing ChatGPT plan-sharing OAuth | `https://api.openai.com/v1/responses` | `priority` |
| NOE `openai-api`, documented only | API key | `https://api.openai.com/v1/responses` | `fast` or `priority` per the API guide |
| Native Codex, source reference | Native ChatGPT OAuth | `https://chatgpt.com/backend-api/codex/responses` by default | Catalog ID `priority` for Fast |

Native Codex's endpoint and routing behavior are described in its
[provider implementation](https://github.com/openai/codex/blob/4fc37acc87fd9eacbc7fd6bf099a4acbdd2252cc/codex-rs/model-provider-info/src/lib.rs#L472-L486)
and [routing-hint change](https://github.com/openai/codex/pull/37345).
NOE kept its public endpoint and plan-sharing credential. No native Codex
credential, API-key fallback, or production routing-hint override was used.

## Initial probes and the response-tier interpretation

All probes explicitly selected `openai-chatgpt / gpt-6.1-sol`.

| Probe | Observed result | What it establishes |
| --- | --- | --- |
| Request `service_tier: "fast"` | HTTP 400, `Unsupported service_tier: fast` | This literal was rejected on the tested authentication path. |
| Request `service_tier: "priority"` | HTTP 200, answer `OK`; created tier `auto`, completed tier `default` | The request succeeded; the echo alone does not settle Fast routing. |
| Read the account's model catalog | HTTP 200; Fast ID `priority`, description `2x speed, increased usage` | The catalog advertised Fast; the description is not a measured guarantee. |
| Add `x-codex-routing-hint: model=gpt-6.1-sol;tier=priority` in a temporary configuration | HTTP 200, answer `OK`, completed tier `default` | This short probe did not change the echo; it did not measure speed. |

A short `OK` response is a connectivity check, not a useful throughput test.
Interpreting its completed tier as proof of inactive Fast was too strong.

The [OpenAI API guide](https://developers.openai.com/api/docs/guides/fast-mode)
documents the returned service tier as the processing tier for API requests.
However, the [Codex contributor explanation](https://github.com/openai/codex/issues/14204#issuecomment-4033184620)
states that ChatGPT-authenticated Codex handles Fast through server-side routing
and that a final `default` does not mean Fast was ignored. That explanation
concerns native Codex, rather than a complete contract for the newer public
plan-sharing route. The NOE comparison below supplies evidence for the actual
NOE path instead of assuming either route has identical response semantics.

## Comparison on the real provider

The public Go `agent/model` service used NOE's provider adapter and existing
ChatGPT credential. Requests ran sequentially in ABBA order: `default`,
`priority`, `priority`, `default`. Only the tier configuration changed between
modes; each request used a fresh session/cache key and no tools.

- Model: `gpt-6.1-sol`; effort: `low`; `maxTokens`: 1500.
- Transport: HTTP/SSE to the public Responses endpoint; User-Agent: `noe/v2.26.1`.
- Prompt: `Output exactly the integers 0001 through 0256 in ascending order, separated by single spaces. No prose, no code fence, no omissions, no line breaks.`
- Every run: 40 input tokens, 771 output tokens, zero reasoning tokens and zero cached tokens.
- Every output matched the requested integer sequence after trimming surrounding whitespace.
- No `x-codex-routing-hint` header was sent in this comparison.

| Run | Requested tier | Total time, s | First text, s | First-to-last text, s | Completed tier |
| --- | --- | ---: | ---: | ---: | --- |
| 1 | `default` | 18.344020 | 3.241319 | 15.041330 | `default` |
| 2 | `priority` | 13.556203 | 2.937748 | 10.540451 | `default` |
| 3 | `priority` | 13.854099 | 3.245946 | 10.519743 | `default` |
| 4 | `default` | 18.942746 | 3.810721 | 15.066572 | `default` |

Mean total time was 18.643383 s for `default` and 13.705151 s for `priority`:
an observed speed ratio of 1.3603. Mean first-to-last text time was 15.053951 s
and 10.530097 s respectively, a ratio of 1.4296.

There are only two samples per mode. This is evidence of an observed latency
difference under these conditions, not a statistically established or universal
speed guarantee. The short input and low effort do not represent long-context
agent work. First-to-last text time measures stream delivery and can be affected
by buffering; total time includes prefill, scheduling, network, and completion
overhead. The checks did not measure the plan-sharing billing multiplier or
establish an authoritative per-response Fast confirmation field.

## Installed-agent verification

The installed `./noe` reported v2.26.1. A separate `noe run --observe` execution
started `run_agent` with `openai-chatgpt / gpt-6.1-sol`, effort `low`,
`maxTokens: 2048`, and `maxSteps: 4`, using the normal configuration above.

The task instructed the agent to invoke `run_js` once with
`console.log(19 * 23)` and return only the result. The observation showed
`run_js` succeeding in 21 ms, final answer `437`, task state `completed`, and
process exit code 0. The script asserted the completed state and exact answer.
This verifies the installed agent's tool cycle with the real provider. It is
separate from the Go-service comparison and does not independently confirm the
served tier or a speed multiplier.

## External evidence and social reports

Sources were checked on 2026-11-16. Their scope and evidence differ:

| Source | Finding and limits |
| --- | --- |
| [Codex contributor comment, March 10](https://github.com/openai/codex/issues/14204#issuecomment-4033184620) | Explains why native ChatGPT-authenticated Codex can return `default` while Fast is requested through routing. |
| [pi-fast 0.4.2, published November 12](https://pi.dev/packages/pi-fast) | Documents the same public endpoint with ChatGPT OAuth, uses `priority`, and reports `fast` rejected while `priority` succeeds and echoes `default`. Its live checks establish request acceptance, not delivered speed or billing. |
| [OpenClaw SIWC wrapper](https://github.com/openclaw/openclaw/blob/cf74e641eb9caac27f5ae5579eeac4d0d2ef8545/extensions/openai/responses-stream.runtime.ts#L16) | The implementation's SIWC tier allowlist includes `priority` and excludes the literal `fast`. This is a third-party implementation, not an OpenAI service contract. |
| [Senpi investigation, July 30](https://github.com/code-yeongyu/senpi/issues/545) and [merged fix](https://github.com/code-yeongyu/senpi/pull/547) | Corrects an earlier subscription-unavailable conclusion based on the `default` echo and enables the existing priority request path. The older latency evidence is weak. |
| [Codex Sol/Astra comparison, October 15](https://github.com/openai/codex/issues/54321) | Reports little Sol benefit but faster Astra while both echo `default`. A November 9 follow-up also shows why text-delta timing can describe buffered release rather than decoding. These are user measurements, not a universal performance finding. |
| [Codex non-interactive performance reports](https://github.com/openai/codex/issues/32191) | Include controlled runs with no observed speed benefit. They leave account, model, version, and serving-condition differences unresolved. |
| [Reddit, September 21](https://www.reddit.com/r/OpenAI/comments/1wlylki/service_tierfast_with_chatgpt_subscription/) | The original poster reports the rejected `fast` literal. A reply generalizes that subscriptions cannot use Fast; that inference conflicts with the priority-compatible implementations above. |
| [Reddit speed discussion](https://www.reddit.com/r/codex/comments/1wyq42w/so_whats_everyones_experience_so_far_after_the/) | Experiences vary and mix baseline speed updates, queueing, reasoning, and Fast selection; they do not isolate NOE tier behavior. |

The [related X post](https://x.com/thsottiaux/status/2107158998495748264) returned
HTTP 403 during direct retrieval. Search summaries of its text were not treated
as independently verified evidence.

## Retained state

The local `openai-chatgpt` override remains `service_tier: "priority"`.
The source code, built-in provider defaults, installed executables, production
endpoint, and running services were unchanged by the investigation. API-key
access was documented from official sources, not exercised. The documentation
reuses the completed real-provider checks; it does not require another live
request, source test suite, or release.
