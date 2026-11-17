# OpenAI Responses WebSocket continuation

Recorded 2026-11-10; updated 2026-11-18. Status: managed-run implementation
complete locally after the isolated spike. Standalone session APIs remain
deferred. See the [implementation](#managed-run-implementation--2026-11-18),
[spike and measurements](../../spikes/openai-websocket/README.md) and
[OAuth results](../../spikes/openai-websocket/results.json) /
[API-key results](../../spikes/openai-websocket/results-api.json).

## Spike outcome — 2026-11-18

The public OpenAI Responses endpoint accepts the existing openai-chatgpt OAuth
credential over WebSocket. Custom run_js and function-tool continuations both
work. Reconnect replays the current complete history without rerunning finished
tool work; trim starts a new chain on the existing socket.

With 16 KiB of reference context, three tool rounds, and three alternating
SSE/WebSocket pairs, median total time was 11.555 s versus 7.500 s including
opening. Request JSON was 71,975 versus 19,660 bytes. Corresponding input-token
counts match; cached-token counts vary. This small synthetic comparison is
evidence for integration, not a universal speed or token-cost claim.

The live store=false stale-ID probe returns status 400 with an
invalid_request_error and no previous_response_not_found code. The spike's
final driver records that rejection and explicitly replays known retained
history; its general helper does not retry generic API errors. Production
recovery must account for this observed format and uncertain delivery.

SDK receive cancellation alone leaves the socket usable. Request cancellation
must abort or otherwise cancel its owned generation/socket; local fixtures
confirm the difference. Connection isolation, history/configuration changes,
opaque native output preservation, cache loss, credential rereading after
reconnect, and cleanup pass focused tests and race checks. Live opaque reasoning
was not produced by these short probes and remains fixture-verified.

During the spike, only experiment files and investigation documentation
changed. Provider defaults,
public APIs, dependencies, installed binaries, services, and releases remain
unchanged in that recorded phase. The user separately authorized the API-key
follow-up below and then the managed-run production implementation.

## API-key follow-up — 2026-11-18

The existing openai-api key also works with Responses WebSocket continuation,
for function tools and custom run_js tools. Under the same 16 KiB / three-tool-
round comparison with three alternating pairs, median total times are 6.348 s
for SSE and 5.618 s for WebSocket, about 11.5% lower. Median request JSON is
71,884 versus 19,776 bytes, about 72.5% smaller. Input-token counts match per
turn; cache counts vary. Individual paired time differences are 36.1%, 1.7%,
and 5.4%, so this sample is evidence of feasibility rather than a general
latency guarantee. It is not a controlled API-key versus OAuth comparison.

API-key responses report fast for requested priority. Reconnect, trim, and
cache-loss recovery pass. This authentication route returns the documented
previous_response_not_found code, whereas the earlier OAuth route returns a
generic invalid_request_error without that code. Keep both observed cases in
the production recovery design.

The follow-up explicitly selects the API key, never falls back to OAuth, and
performs 33 response.create attempts: 32 completed generations and one
deliberate cache-miss rejection. Focused race/vet and authentication-selection
fixtures pass. Evidence: `/var/tmp/noe-openai-websocket-api-s12hj6ak`.

## Purpose and scope

Use a persistent OpenAI Responses WebSocket connection to reduce repeated
history uploads and continuation overhead in agent tasks with many model/tool
round trips. Start with one connection per conversation and send new input
items with `previous_response_id` when continuing an unchanged history.

The initial scope is OpenAI Responses. OpenCode Go stays on its existing
HTTP/SSE routes: its Responses compatibility does not establish support for
WebSocket continuation. Service support for that transport was not confirmed
in the [OpenCode documentation](https://opencode.ai/docs/providers/#websockets).

The user selected standalone `model.Service.Generate()` connection reuse as a
memo-only future candidate on 2026-11-18. Initial production integration is
limited to connections owned by managed agent runs. It does not add a public
standalone session API or change standalone Generate's existing HTTP/SSE path.
See the [deferred standalone note](#deferred-standalone-model-service-reuse).

The local checkout already selects OpenAI Go SDK v3.98.0, which provides
connection management and raw JSON send/receive methods. NOE can reuse its
request conversion and Responses event assembly. The separate
[SDK update and request encoding optimization](agentlib-sdk-request-optimization.md)
is included in v2.26.0 and later; WebSocket integration is additional work.

## Implementation size and integration

This is a medium change spanning the provider adapter and conversation
lifetime management. The main work is state ownership and continuation,
rather than implementing the WebSocket protocol or replacing the agent loop.

- Associate each connection with the stable conversation/session identity.
  Keep parent and child conversations separate in the initial implementation.
- Retain the completed response ID and the history/configuration needed to
  establish whether the next request extends the same chain. Preserve native
  reasoning, custom/function tool calls, and their outputs.
- After trimming, history replacement, or loss of connection-local state,
  start a new chain from the current complete history. NOE currently uses
  `store=false`; reconnecting does not restore the server's previous state.
- Give each managed run's connection an explicit owner and shutdown path.
  Registry provider clients remain shared without owning persistent conversation
  sockets. Standalone model-service reuse is deferred and is not a prerequisite
  for this managed-run integration.
- Preserve authentication refresh, cancellation, thinking/text observations,
  usage, provider errors, and cleanup. SDK receive cancellation only ends the
  wait; the transport must also stop an interrupted generation and release its
  resources through the chosen lifecycle.
- Integrate recovery with the existing logical-request retry owner. The spike
  exposes uncertain delivery to its driver; it does not propose removing
  generateWithRetry. Preserve completed local tool work in authoritative
  history when a fresh connection starts another attempt.

A shared socket multiplexing several conversations is a later option. The
first implementation should establish correct reuse within one conversation.

## Managed-run implementation — 2026-11-18

The user approved production implementation while retaining standalone
model.Service.Generate reuse as a memo-only candidate. Built-in OpenAI
providers now select transport=auto: managed Responses calls use WebSocket,
other APIs use SSE, and standalone Generate keeps SSE. Custom provider
definitions default to SSE. An explicit transport=sse preserves the previous
managed transport; explicit websocket requires OpenAI Responses. Provider
configuration is documented in the [guide](../providers.md#openai-responses-transport).

Each agent run creates an internal provider RunScope, carried only through its
main model call's context. The existing public model service forwards that
context to the adapter. No public Service/Request/session API is added, and
independent model/tool calls do not acquire the agent loop's connection. Run
cancellation closes its scope synchronously; the scope also follows owner
context cancellation. Finishing, closing the operation/Host, and interruption
release owned sockets. Queued runs and parent/child runs have distinct scopes.
Closing a run does not close a borrowed model service or HTTP client.

The per-adapter resource serializes generation with cancellable acquisition.
Its separate state mutex lets owner shutdown abort a dial/receive/idle socket
without waiting for the generation gate or Observe callback. Accepted history
uses compact per-item fingerprints plus the latest response ID, rather than a
second retained transcript. Canonicalization sorts object keys and preserves
JSON numbers, so map order does not spuriously defeat reuse or round large
values. Configuration/history mismatch sends full current input without an ID.

Request conversion, native output assembly and observations reuse the existing
Responses helpers. Valid completed responses alone advance the chain. Both the
documented cache-miss code and the observed exact OAuth missing-code error can
recover once from full current history after an incremental request. Ordinary
transport/stream/rate failures use the existing logical-request retry owner,
starting a fresh chain. Completed local tool work remains in caller history.
Quota, authentication, malformed responses, and cancellation keep their prior
safe error categories; EOF is not completion.

The SDK upgrade path uses the configured client's explicit WebSocket
capability. Middleware applies endpoint/header/session configuration once
before Client.Do, preserving path-scoped cookies and redirect callbacks.
The borrowed transport, opening timeout, Jar and legacy cancellation policy
remain in force, and HTTP 101 bodies remain writable. A captured handshake
response preserves 401/403/402/429/400 error classification. See the helper
in [source](../../internal/agent/provider/openai_websocket_http.go).

Independent review found and resolved a close/history-slice race and handshake
adapter issues involving legacy cancellation, redirects/cookies, and rejection
status classification. One fix-only re-review reported no remaining findings.
Actual GoHost and JavaScript-entry fixtures verify custom/function tools,
delta-only input with context notifications, queued-run connection boundaries,
active cancellation, and standalone SSE before/after borrowed Host shutdown.
External checkout and standalone-artifact consumers exercise both public modes.

A production-shaped GoHost live check completed 19*23 through one actual run_js
execution and two model calls for each of openai-chatgpt and openai-api. Each
case used one GET/101 exchange and no Responses POST. These are functional
smokes, not new comparative performance measurements. Final HTTP policy fixes
were verified with focused fixtures; the successful normal live path is reused.

Implementation evidence: `/var/tmp/noe-managed-websocket-nc1xtl0n`. Source
hashes and verification records permit reuse for unchanged source. Dependencies
and public Go/JS signatures do not change. No commit, push, release, installation
or service restart is included in this implementation slice.

## Deferred standalone model service reuse

User decision, 2026-11-18: retain this as a future note only. Do not implement
a standalone WebSocket/session API as part of the current integration.

Standalone `model.Service.Generate()` is useful for Go applications that need
summarization, classification, structured extraction, or calls inside an
existing workflow without an agent loop or JavaScript runtime. It reuses NOE's
provider selection and authentication. Those uses already work through the
existing service; they do not require a new public API.

Persistent WebSocket reuse could benefit a caller that owns a multi-turn
conversation or tool workflow. A future optional session abstraction would
need an explicit close/cancellation lifetime and per-conversation isolation.
The shared model service and borrowed bindings must retain their ownership:
`Binding.Close()` must not close a borrowed service. No exported session name
or signature is selected by this note.

Revisit this candidate when a concrete standalone caller needs persistent
conversation reuse. Keep the existing standalone Generate API and HTTP/SSE
behavior unchanged in the meantime; managed agent runs can adopt owned
connections independently.

## Validation before production integration

First establish that the intended OpenAI endpoint and authentication mode can
complete a text request, a `run_js` tool call, and a continuation containing
only the new tool output. SDK availability alone does not establish support
for every configured endpoint or authentication mode.

Use local fixtures to check conversation isolation, prefix/configuration
changes, trim, cancellation, reconnect, and closure. Recovery must preserve
already completed tool work rather than rerun it. Compare actual uploaded
bytes and model/tool round-trip latency against the existing HTTP/SSE path
before selecting the default transport. Expected benefits are workload
dependent. The spike now measures one synthetic transport workload; full
NOE agent runs and longer workloads remain unmeasured.

## References

- [OpenAI WebSocket mode](https://developers.openai.com/api/docs/guides/websocket-mode), including `store=false` continuation and recovery after cache loss.
- [OpenAI Go SDK WebSocket API](https://github.com/openai/openai-go/blob/v3.98.0/responses/responses_websocket.go).
- [Current OpenAI adapter](../../internal/agent/provider/openai.go), [provider registry](../../internal/agent/provider/config.go), [Responses conversion](../../internal/agent/provider/responses.go), and [event assembly](../../internal/agent/provider/responses_stream.go).
- [Model service ownership](../../agent/model/service.go) and [borrowed binding lifetime](../../agent/model/binding.go).
