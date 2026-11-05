# Agent library optimization: conversation appends and SSE assembly

Implemented locally on top of `08fedd6`, 2026-11-05, in the requested order:
conversation checkpoint appends, then incremental provider-response assembly.
The earlier [context serialization optimization](agentlib-optimization.md)
remains in place. Public Go and JavaScript APIs are unchanged.

## Conversation append protocol

Managed tasks previously sent the complete conversation snapshot after every
tool exchange, final-answer candidate, and steer application. Go decoded,
validated, compared, and copied the accumulated prefix again each time.

The private protocol now sends `append: {baseRevision, entries}`. Go checks
the existing operation/run ownership and lifecycle gates, then validates and
copies only the new entries. Previously committed entry payloads remain
immutable and are shared by the new snapshot; its outer entry slice is new.
The host derives the next revision and sequence from its committed state.

The append path rejects stale revisions, duplicate entry IDs, malformed
messages, unexpected entry kinds, and incomplete tool exchanges. Tool IDs,
recorded results, and image selection still pass their existing commit gates.
Final candidates remain committed before a pending steer rejects the answer.
Final/steer replay checks remain idempotent without appending twice. Steer
instructions must still match the delivered batch and acceptance order.

Initial restore and trim approval retain complete snapshots. Trim therefore
keeps its existing protected-entry, complete-block, revision, image, and
interruption rules. Full-snapshot updates also remain available to existing
direct internal callers; managed JavaScript uses appends. The public API has
no new update type or configuration option.

Implementation:
[append validation/ownership](../../internal/agent/conversation_append.go),
[tool checkpoint](../../internal/agent/conversation.go),
[final/steer gates](../../internal/agent/broker_steer.go),
[JS history](../../internal/agent/js/history.js), and
[task loop](../../internal/agent/js/task.js).

### Measurements

A TaskHistory byte-count probe appended 128 complete tool exchanges, each
with one 4 KiB result, without trimming or runtime status messages. It counted
serialized JSON payloads for the former snapshot and implemented append forms:

| Internal JSON payloads | Complete snapshots | Appends |
| --- | ---: | ---: |
| Cumulative bytes across 128 updates | 35,903,355 | 564,178 |

The final snapshot was 556,447 bytes. This is a serialization-size comparison,
not a measurement of provider requests, model tokens, or network latency.

A separate Go microbenchmark starts with 256 existing 4 KiB assistant entries
and receives one new 4 KiB entry. It measures request decoding, validation,
and creation of an owned candidate through the shared append helper. It
excludes JavaScript serialization and tool/image-specific completion gates.
Each value is the median of five 250 ms runs on Go 1.27.2, Linux amd64,
CGO disabled, Intel Core i9-13900HX.

| Receiver-helper cost per update | Snapshot form | Append form |
| --- | ---: | ---: |
| Cumulative allocated bytes | 7,473,172 | 35,876 |
| Allocations | 6,480 | 19 |
| Time | 6.95 ms | 25.9 us |

The [benchmark and regressions](../../internal/agent/conversation_append_test.go)
remain available in the repository:

```sh
CGO_ENABLED=0 go test ./internal/agent -run '^$' \
  -bench '^BenchmarkConversationCheckpointUpdate$' -benchmem -benchtime=250ms -count=5
```

## Incremental SSE assembly

The shared HTTP body observer previously retained every SSE payload until the
SDK finished, then replayed the collection through a second response assembly
pass. It now assembles complete events as bytes arrive:

- Chat accumulates content, reasoning, refusals, tool names/arguments, usage,
  and opaque fields directly into the existing response accumulator.
- Responses retains completed items by output index and the terminal envelope.
  Terminal output overrides matching earlier items, while items available
  only in `response.output_item.done` remain intact. Text deltas are discarded
  after observation. The envelope does not retain a duplicate raw output array.
- Native usage-limit codes remain available without retaining error-event
  history, including when an earlier assembly error has occurred.

The OpenAI and OpenRouter SDK iterators still validate and drive their normal
stream types. `[DONE]`, terminal completion, read errors, cancellation, error
classification, retries, and response-body closure retain their existing
adapter gates. Observers run outside the body lock and retain their event
order. Completed data-line references are cleared; partial framing remains
bounded by the current event, without a new response-size limit.

Implementation: [body parser](../../internal/agent/provider/sse.go),
[Chat accumulator](../../internal/agent/provider/stream_accumulator.go),
[Responses accumulator](../../internal/agent/provider/responses_stream.go),
[OpenAI adapter](../../internal/agent/provider/openai.go), and
[OpenRouter adapter](../../internal/agent/provider/openrouter.go).

### Measurements

The synthetic Chat stream has 16,384 deltas of 32 text bytes each, totaling
512 KiB of text. Retained-heap diagnostics use repeated frames, force GC
before and after feeding one live body, and keep that body reachable through
the measurement. The median of five trials was:

| Live body diagnostic | Before | After |
| --- | ---: | ---: |
| Increase in retained Go heap | 3,312,584 bytes | 563,264 bytes |

This measures retained Go heap at that point, including runtime/pool effects;
it is not process RSS or a whole-request peak. Before the change, assembly had
not yet replayed the retained events; after it, the body already contains the
assembled text. The measured reduction was approximately 83%.

A separate assembly microbenchmark reads the same complete stream and obtains
the final response, with observation callbacks disabled. SDK parsing, HTTP,
and provider latency are excluded. Medians of five 250 ms runs were:

| Body parsing and response assembly | Before | After |
| --- | ---: | ---: |
| Cumulative allocated bytes | 24,820,779 | 21,465,066 |
| Time | 37.1 ms | 35.5 ms |

The improvement primarily removes retained raw history. Final answer text,
opaque response items, and the largest current event still require memory.
The current [assembly benchmark](../../internal/agent/provider/sse_test.go)
can be repeated with:

```sh
CGO_ENABLED=0 go test ./internal/agent/provider -run '^$' \
  -bench '^BenchmarkObservedBodyChatAssembly$' -benchmem -benchtime=250ms -count=5
```

## Verification

The checkpoint slice passed agent core/public Client/runtime tests, race
checks for those three packages, focused CLI context/output E2E, vet, and JS
lint before the SSE slice started. Those seven source/test file hashes remain
unchanged by the SSE slice.

After both changes, the following passed:

```sh
CGO_ENABLED=0 go test ./internal/agent/... ./agent/... \
  ./internal/agentbridge ./internal/agentexec ./internal/runtime -count=1 -timeout=4m
CGO_ENABLED=1 GORACE=atexit_sleep_ms=0 go test -race \
  ./internal/agent/provider ./agent/model ./internal/agentbridge -count=1 -timeout=4m
CGO_ENABLED=0 go test -tags=e2e ./tests -count=1 -timeout=5m
go vet ./internal/agent/... ./agent/... \
  ./internal/agentbridge ./internal/agentexec ./internal/runtime
biome lint internal/agent/js/history.js internal/agent/js/task.js
git diff --check
```

The later addition of the repository SSE benchmark was checked by a focused
provider test/benchmark run and provider vet; completed checks for unchanged
production sources were reused.

Coverage includes external Go consumers; queue, steer, trim, interruption,
images, hooks and CLI observations; both SDKs' Chat/Responses fixtures;
fragmented BOM/CRLF/multiline framing and UTF-8; incremental text and final
item retention; opaque/encrypted reasoning and usage; malformed input,
incomplete streams, quota failures, cancellation, and closure. All provider
requests in this work use local fixtures.

Raw logs, before-source snapshots, diagnostic overlays, measurement summaries,
and final changed-source hashes are retained at
`/tmp/noe-agentlib-checkpoint-sse-t57yl1gy/results.json`. Both slices are
committed locally. Release and executable placement remain separate work.
