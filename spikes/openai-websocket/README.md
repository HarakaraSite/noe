# OpenAI Responses WebSocket spike

Checked on 2026-11-18 JST, on top of `dd166c1` and installed NOE v2.28.0.
Status: retained experiment and measurements. The later
[managed-run implementation](../../docs/development/agentlib-openai-websocket-idea.md#managed-run-implementation--2026-11-18)
uses the validated transport without adding a standalone session API. The
measurements below describe the spike, not that production agent loop.

## OAuth result

The existing `openai-chatgpt` OAuth plan-sharing credential works with the
official Responses WebSocket endpoint, using the already pinned OpenAI Go SDK
v3.98.0. Both NOE-style custom `run_js` text calls and function tools complete
with only the new tool output in the next input array. Reconnect, trimmed
history, and controlled recovery after a stale response ID also work.

The small transport comparison is promising:

| Three paired trials, three tool rounds per conversation | HTTP/SSE | WebSocket |
| --- | ---: | ---: |
| Median total time, including connection establishment | 11.555 s | 7.500 s |
| Minimum–maximum total time | 11.022–13.095 s | 6.646–8.431 s |
| Request JSON bytes per conversation | 71,975 | 19,660 |
| JSON bytes per tool-result continuation | 17,829–18,488 | 703–730 |

The median time is about 35.1% lower and request JSON about 72.7% smaller.
These are three synthetic paired trials, not a general latency guarantee or
a measurement of full NOE agent runs. The same prompt/tool-result values and
model settings are reused within a pair; order alternates SSE/WS, WS/SSE,
SSE/WS. HTTP uses its ordinary reusable client/transport. WebSocket time
includes opening a new socket, about 1.00–1.08 s in these trials.

Each conversation contains 16,384 bytes of synthetic reference data and three
synthetic tool outputs. Tools do not execute JavaScript or external effects.
Both transports use `gpt-6.1-sol`, low reasoning, `store=false`, and requested
`service_tier=priority`. Both report `default`; that field alone does not
determine actual Fast routing or billing. Both use the same raw event assembly.

Input token counts match on **every corresponding turn**. WebSocket reduces
history uploaded by the client, not the model's logical context or input-token
count. Cached-token counts vary, and output is intentionally short; caching,
server load, and network conditions affect the timings. JSON counts exclude
headers, WebSocket frames, TLS, and response traffic. First-event latency may
measure response creation; first-text latency is absent for pure tool calls.

See [results.json](results.json) for compact sanitized results, individual
trials, cache/token counts, and the retained failed exploration. Full sanitized
reports and verification are under `/var/tmp/noe-openai-websocket-whkqvrsv`.

## API-key follow-up

The user separately authorized live `openai-api` measurements. The existing
API key also completes WebSocket continuation with function tools (that
provider's default) and custom run_js text tools. The same pinned SDK and public
endpoint are used. No authentication fallback is implemented.

| Three paired trials, three tool rounds per conversation | HTTP/SSE | WebSocket |
| --- | ---: | ---: |
| Median total time, including connection establishment | 6.348 s | 5.618 s |
| Minimum–maximum total time | 5.942–8.210 s | 5.244–6.238 s |
| Median request JSON bytes per conversation | 71,884 | 19,776 |
| JSON bytes per tool-result continuation | 17,828–18,416 | 724–758 |

The median is about 11.5% shorter and request JSON 72.5% smaller. Individual
paired time reductions are 36.1%, 1.7%, and 5.4%; the difference is small in
two pairs. Input-token counts again match on every corresponding turn, while
cached-token counts vary. These are small synthetic samples. The two
authentication cohorts use their provider's default tool format and were run
at different times; they do not establish which authentication route is faster.

Both transports request priority and all successful API-key responses report
fast. OAuth responses in the earlier cohort report default. This spike records
those values without inferring OAuth Fast routing or billing from that field.
The API comparison uses the same 16 KiB reference context, low reasoning,
store=false, model, and alternating trial order as the earlier experiment.
WebSocket opening takes about 0.68–0.76 s in these trials.

Reconnect/full replay, trim on an existing socket, and deliberate stale-ID
recovery also pass. API-key authentication returns status 400 with
code=previous_response_not_found, matching the documented case; the prototype's
known-code recovery succeeds. The OAuth endpoint instead returned the generic
missing-code error documented below. Each recovery retains the completed
synthetic tool output and does not execute the tool again.

See [results-api.json](results-api.json). The API-key follow-up made 33
response.create attempts: 32 completed generations and one deliberately
rejected stale ID. Full sanitized reports and verification are under
`/var/tmp/noe-openai-websocket-api-s12hj6ak`. Credentials are never included.

## Lifecycle findings

- A connection belongs to one sequential conversation in this prototype.
  Distinct conversations use distinct sockets. It retains the latest response
  ID, complete native output items, and an owned copy of the accepted history.
- An unchanged configuration plus an exact history extension sends only new
  input items with the previous ID. Model/tool/configuration changes or history
  replacement send the complete current history without a previous ID. The
  per-turn tool_choice may change without invalidating history.
- Normal reconnect clears the prior ID and resends the current full context,
  including already completed tool work. The live reconnect test obtains a
  new response without executing its synthetic tool again. Trim starts a new
  chain on the existing socket.
- The OAuth stale-ID test returns HTTP-style status 400 in an error event with
  `type=invalid_request_error` and message `Invalid previous_response_id`,
  **without** a `previous_response_not_found` code. Code matching alone cannot
  implement recovery for this endpoint. The first exploration records that
  mismatch; the final experiment records the rejection, then explicitly sends
  retained history on a fresh connection. The generic error is not retried by
  the conversation helper. Its known-code retry is covered by a local fixture.
- Unknown delivery/send/read failures return to the caller and invalidate the
  connection. They are not replayed automatically. Completed tool output stays
  in caller-owned history for an explicit next request.
- SDK RecvRaw cancellation cancels only the receive wait; the socket remains
  usable. The spike's running-request cancellation additionally calls Abort
  and releases the socket. Both behaviors are checked against real local
  WebSocket fixtures. Remote inference cancellation/billing was not measured.
- Native reasoning and unknown fields survive raw output assembly and replay
  in fixtures, including an empty terminal output array with output_item.done
  fallback. The short live probes produced no reasoning output items, so live
  opaque-reasoning replay is not claimed.

Each endpoint/authentication result is specific to the tested credential,
model, date, and route. The API-key follow-up uses its explicitly selected
credential. No alternative ChatGPT backend, automatic account switch, or
OpenCode Go transport was used.

## Reproduction

From the repository root, with the existing ChatGPT plan-sharing credential:

```sh
go test ./spikes/openai-websocket
go test -race ./spikes/openai-websocket
go vet ./spikes/openai-websocket

# Live OAuth requests; each smoke runs two generations.
go run ./spikes/openai-websocket -scenario smoke -tool custom
go run ./spikes/openai-websocket -scenario smoke -tool function

# Six conversations, four generations each; three alternating trial pairs.
go run ./spikes/openai-websocket -scenario compare -pairs 3 -rounds 3 -context-bytes 16384

# Reconnect, trim, deliberate stale ID, and explicit full-history recovery.
go run ./spikes/openai-websocket -scenario lifecycle

# Explicit API-key selection; these use API billing, with no OAuth fallback.
go run ./spikes/openai-websocket -auth api-key -scenario smoke
go run ./spikes/openai-websocket -auth api-key -tool custom -scenario smoke
go run ./spikes/openai-websocket -auth api-key -scenario compare -pairs 3 -rounds 3 -context-bytes 16384
go run ./spikes/openai-websocket -auth api-key -scenario lifecycle
```

`-out FILE` saves the sanitized JSON report. Other flags select model, effort,
tier, and the total deadline. The fixed endpoint is
`https://api.openai.com/v1/responses`; provider configuration overrides are not
loaded. The default `-auth chatgpt` uses `NOE_OPENAI_CHATGPT_CREDENTIALS_FILE`,
or the standard user config directory's `noe/credentials/openai-chatgpt.json`.
The existing locked OAuth resolver runs for each handshake/reconnect and SSE
request; normal refresh may update its record. Explicit `-auth api-key` reads
`NOE_OPENAI_API_KEY_FILE`, or `noe/credentials/openai-api-key` in that same
config directory, and does not access the OAuth record. It omits the OAuth
session_id header. The tool default is custom for OAuth and function for API
keys; -tool overrides it. Credentials are never logged, and neither mode falls
back to the other. Tests make no live requests.

## Integration recommendation

The subsequent managed-run implementation follows this recommendation.
The initial owner is a managed run's conversation, closed on completion/cancellation,
with parent and child conversations separate. Keep SDK clients/auth transports
shared; keep persistent sockets out of the shared provider registry.

Reuse NOE's configured request conversion, raw Responses assembly,
observations, error classification, and native output preservation. A changed
history/configuration or reconnect should proactively start from full current
history. Recover the observed missing-code error only when the client can
establish the stale-ID condition, rather than treating every generic 400 as
retryable. Explicitly abort interrupted generation; cancelling Recv is not a
resource-lifetime policy.

The user selected standalone model.Service.Generate connection reuse as a
memo-only future candidate. Initial integration covers managed-run ownership;
it does not add a standalone session API or change existing Generate's SSE
behavior. See the [deferred note](../../docs/development/agentlib-openai-websocket-idea.md#deferred-standalone-model-service-reuse).
This spike is sequential and does not implement production concurrency,
transport selection, automatic deadline renewal, multiplexing, or cross-run
persistence.

Retry ownership also needs integration with NOE's existing generateWithRetry.
Its logical request can retry transport/stream failures without rerunning local
tools. The spike exposes uncertain failure to its driver to make attempts
observable; that is not a proposal to remove NOE's existing retry behavior.
A retry after connection loss should use the retained current history and a
fresh chain, rather than replaying a stale raw WebSocket command.

## References

- [Official WebSocket guide](https://developers.openai.com/api/docs/guides/websocket-mode): incremental input, store=false recovery, compaction, and connection limits. The guide also supports multiple named lanes; this prototype uses one default lane.
- [Pinned SDK connection implementation](https://github.com/openai/openai-go/blob/v3.98.0/responses/responses_websocket.go): raw send/receive, explicit recovery, Abort, and caller-owned lifetime.
- [Original integration memo](../../docs/development/agentlib-openai-websocket-idea.md), [current provider](../../internal/agent/provider/openai.go), [shared registry](../../internal/agent/provider/config.go), and [Go service](../../agent/model/service.go).
