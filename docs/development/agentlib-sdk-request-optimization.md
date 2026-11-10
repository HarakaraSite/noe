# Agent SDK updates and Responses request encoding

Completed locally on top of `8caefb2`, 2026-11-10. The user requested updating
both provider SDKs and removing the discarded Chat request encoding from the
Responses path. Public Go/JavaScript APIs and provider configuration are unchanged.

Published and installed in [v2.26.0](../releases/v2.26.0.md); see the
[publication checkpoint](../releases/release-checkpoints.md#v2260-publication-and-installation--2026-11-10).

## Dependencies

| Module | Before | After |
| --- | --- | --- |
| `github.com/openai/openai-go/v3` | v3.79.0 | v3.98.0 |
| `github.com/OpenRouterTeam/go-sdk` | v0.9.61 | v0.9.116 |

Both versions were confirmed by fresh `@latest` module queries. Only these two
requirements change in NOE's go.mod. Go remains 1.27.2, go-quickjs remains
v0.37.1 with JIT disabled, and existing provider endpoints and authentication
remain in use. Upstream additions are described in the
[OpenAI changelog](https://github.com/openai/openai-go/blob/v3.98.0/CHANGELOG.md)
and [OpenRouter comparison](https://github.com/OpenRouterTeam/go-sdk/compare/v0.9.61...v0.9.116).

The module graph also selects upstream Azure/AWS dependency updates and drops
OpenRouter's testify/yaml test dependencies. These graph changes are recorded in
`dependency-changes.json` in the evidence directory. go.sum contains the checksums
needed by `go mod tidy`, including Azure azcore v1.23.3, Azure internal v1.13.0,
and x/net v0.60.0. No additional direct SDK, transport, or provider is introduced.

## Request encoding

`configuredResponsesRequestBody` previously called `makeChatRequestBody` for
validation, discarding its complete serialized Chat payload before constructing
the actual Responses payload. With long histories this encoded the complete
message/tool collection unnecessarily on every generation.

The shared `validateGenerationRequest` helper now checks the request and returns
its normalized effort without encoding a payload. Chat still encodes its own
payload, while Responses constructs only its own representation and reuses the
normalized effort. Invalid model/cache-key UTF-8 remains rejected explicitly;
the discarded JSON encoder previously supplied those checks. Existing object,
duplicate-name, UTF-8, effort, token-limit, and error-classification behavior is
preserved. Request messages and tools are not modified.

The change is shared by OpenAI and OpenRouter Responses adapters. Chat JSON,
raw request forwarding, opaque reasoning/tool history, authentication, SSE
assembly, retries, cancellation, and response-body ownership remain unchanged.

## Measurements

The benchmark measures complete Responses request construction with synthetic
text history, one function tool, a session/cache key, and high effort. It excludes
HTTP, SDK response parsing, model execution, and tool execution.

Before/after test binaries contain identical SDK versions and benchmark source.
Five process pairs alternate which binary runs first; each benchmark runs for
250 ms. Values below are medians on Linux amd64, Go 1.27.2, CGO disabled,
Intel Core i9-13900HX. Allocated bytes are cumulative per operation, not retained
heap or process RSS.

| History content | Time before | Time after | Allocated bytes before | Allocated bytes after |
| --- | ---: | ---: | ---: | ---: |
| 4 x 4 KiB | 103.2 us | 83.0 us | 105,260 | 86,267 |
| 256 x 4 KiB | 9.22 ms | 7.93 ms | 14,927,254 | 10,928,036 |
| 2 x 512 KiB | 5.10 ms | 3.73 ms | 6,634,776 | 5,376,628 |

The many-message 1 MiB case reduces request-construction time by 14.0% and
allocated bytes by 26.8%. These measurements establish a local encoding
improvement, not an end-to-end agent speedup.

The repository benchmark can be rerun with:

```sh
CGO_ENABLED=0 go test ./internal/agent/provider -run '^$' \
  -bench '^BenchmarkResponsesRequestBody$' -benchmem -benchtime=250ms -count=5
```

## Verification

| Check | Result |
| --- | --- |
| New validation regressions on the old implementation | Passed before production changes, with both new SDKs |
| Provider suite | Passed, 182 tests/subtests; no skips |
| Remaining agent core/public packages, bridge and executor | Passed, 299 tests/subtests; no skips; the already verified provider package was excluded |
| Focused CLI E2E | Passed: tool-history preservation and truncated tool output reaching hooks/model/events |
| Vet, including E2E sources | Passed for agent/provider/public packages, bridge, executor and CLI tests |
| External public agent consumer | Checkout and independent module-artifact builds/runs passed in portable and Go-host modes; both resolve OpenAI v3.98.0 and OpenRouter v0.9.116 |
| Module integrity and dependency hygiene | `go mod verify` passed; final `go mod tidy -diff` is empty |

Regression coverage includes invalid model/cache-key strings, invalid message
and tool objects, duplicate names and invalid UTF-8, effort/token limits,
Chat/Responses wire fields, native/custom tool cycles and reasoning, image
generation, Decisions, authentication, retries, cancellation and error handling.
Existing successful implementation results are reused for unchanged code.

Evidence: `/var/tmp/noe-sdk-request-a5qnx1o2`. It contains commands, full logs,
before-source snapshots, module graph changes, external consumer manifests,
test-binary build metadata, individual benchmark samples and medians, and final
source hashes. Implementation verification used no live provider requests or
cross-platform execution. Publication and installation are recorded in the
linked release checkpoint; running services were not changed.
