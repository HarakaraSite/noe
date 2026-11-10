# OpenAI Responses WebSocket continuation

Recorded 2026-11-10. Status: deferred improvement idea; implementation has not
started.

## Purpose and scope

Use a persistent OpenAI Responses WebSocket connection to reduce repeated
history uploads and continuation overhead in agent tasks with many model/tool
round trips. Start with one connection per conversation and send new input
items with `previous_response_id` when continuing an unchanged history.

The initial scope is OpenAI Responses. OpenCode Go stays on its existing
HTTP/SSE routes: its Responses compatibility does not establish support for
WebSocket continuation. Service support for that transport was not confirmed
in the [OpenCode documentation](https://opencode.ai/docs/providers/#websockets).

The local checkout already selects OpenAI Go SDK v3.98.0, which provides
connection management and raw JSON send/receive methods. NOE can reuse its
request conversion and Responses event assembly. The separate
[SDK update and request encoding optimization](agentlib-sdk-request-optimization.md)
is complete locally; this idea is additional work.

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
- Give connections an explicit owner and shutdown path, including standalone
  Go model-service use and borrowed services. The registry currently shares
  provider clients, while the public model service has no connection-lifetime
  contract. Select that ownership design before adding persistent sockets.
- Preserve authentication refresh, cancellation, thinking/text observations,
  usage, provider errors, and cleanup. SDK receive cancellation only ends the
  wait; the transport must also stop an interrupted generation and release its
  resources through the chosen lifecycle.

A shared socket multiplexing several conversations is a later option. The
first implementation should establish correct reuse within one conversation.

## First validation when resumed

First establish that the intended OpenAI endpoint and authentication mode can
complete a text request, a `run_js` tool call, and a continuation containing
only the new tool output. SDK availability alone does not establish support
for every configured endpoint or authentication mode.

Use local fixtures to check conversation isolation, prefix/configuration
changes, trim, cancellation, reconnect, and closure. Recovery must preserve
already completed tool work rather than rerun it. Compare actual uploaded
bytes and model/tool round-trip latency against the existing HTTP/SSE path
before selecting the default transport. Expected benefits are workload
dependent; no NOE WebSocket performance measurement exists yet.

## References

- [OpenAI WebSocket mode](https://developers.openai.com/api/docs/guides/websocket-mode), including `store=false` continuation and recovery after cache loss.
- [OpenAI Go SDK WebSocket API](https://github.com/openai/openai-go/blob/v3.98.0/responses/responses_websocket.go).
- [Current OpenAI adapter](../../internal/agent/provider/openai.go), [provider registry](../../internal/agent/provider/config.go), [Responses conversion](../../internal/agent/provider/responses.go), and [event assembly](../../internal/agent/provider/responses_stream.go).
- [Model service ownership](../../agent/model/service.go) and [borrowed binding lifetime](../../agent/model/binding.go).
