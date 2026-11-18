# Provider retries and measured context estimates

Local implementation on top of v2.30.0, 2026-11-18. The user selected the
retry and context-estimation candidates after comparing the installed
`@earendil-works/pi` 1.5.2 implementation. Tool-output storage remains a future
agentlib candidate: stdlib would provide its file I/O, while agentlib would
own capture, storage references, and lifetime. LLM summarization/compaction
was explicitly rejected and is not part of the selected design.

## Provider retries

Generation retains one logical request and a maximum of three transport
attempts. SDK retries remain disabled. Hooks, completed tools, admission
counters, terminal events, and final usage keep their existing logical-request
semantics. Failed attempts never contribute partial tool calls to execution
or partial responses to conversation history.

The provider boundary preserves recognized machine error codes and private
retry metadata from HTTP responses, Responses SSE failures, and WebSocket
failures/upgrades. Known usage/billing exhaustion is not transient, including
quota failures sent as HTTP 429. Temporary ChatGPT usage/user lookup failures
are transient, including `subscription_sharing_usage_unavailable` and
`subscription_sharing_user_unavailable`. Classification does not depend on
free-form provider message text or expose response bodies.
HTTP error-code capture is bounded to the first 64 KiB; unavailable or unknown
codes retain the HTTP-status fallback. Authentication status takes precedence
over a conflicting transient code, including WebSocket error events.

Valid retry-after-ms takes precedence over Retry-After (seconds or an HTTP
date). A server delay up to 60 seconds is honored without jitter. A longer
server delay returns the classified error instead of retrying early.
Malformed, negative and nonfinite values fall back to another valid hint or
local backoff. Without a usable hint, exponential backoff is 1.5–2 seconds
before attempt two and 3–4 seconds before attempt three. Waits are cancellable.
The x-should-retry false directive stops retries; true does not override
authentication, quota, or cancellation failures. No retry settings or public
Go/JavaScript function signatures are added.

## Measured context estimates

A successful model request with a positive, safely representable normalized
prompt/input token count establishes a measurement of the exact input sent.
Cached input remains included; it is never subtracted from context usage.
Zero, missing, invalid, failed, or cancelled usage cannot establish a new
measurement. The previous-request fields still report only that request's
actual measurements, including null when unavailable.

For an unchanged input prefix, the next estimate is its measured input count
plus the JSON UTF-8 byte/4 and image-estimate delta for subsequent history.
The previous assistant response, tool results, steering, images, and the new
context-status message are additions. Completion/output usage is not added a
second time. The estimate method gains a `provider_usage+` prefix. It remains
an approximation, not a tokenizer or a billing forecast.

The measurement stores an input-prefix digest/count, the original heuristic
total, model configuration identity, and copied image-cost numbers. It retains
no transcript or image bytes. The digest reorders JSON object members without
rounding raw numbers: Go and JavaScript may reconstruct an equal status object
with different member order. Tools, provider/model/effort/output configuration,
and existing image costs must still match. New image costs belong to the delta.
Deletion, changed/replaced content, changed tools/configuration, or changed
old image costs invalidate the measurement. An unchanged copy of the same
input is equivalent. After trim, the existing heuristic applies until a new
successful request supplies usage.

The displayed total and automatic-trim budget use the same calibrated amount.
Because providers do not attribute input usage to individual messages, the
calibrated total is distributed proportionally over the existing heuristic
protected/history weights. Block weights are scaled cumulatively to avoid
rounding drift. The current status message remains transient; protected
instructions and the newest complete block retain the existing deletion
rules and acknowledgement protocol. No summary model call is introduced.

Repeated managed preparation reuses prefix validation through the existing
revision/tools/image analysis cache. Explicit histories retain their full
validation path. Immutable measurements may continue across queue tasks of
the same instance; different instances own separate state. Run settlement
releases run-owned measurement and preparation references. Model services
continue to receive request-owned bytes.

## Verification

Focused regressions cover actual request-to-usage association, repeated
preparation, cached-token inclusion, raw-number/object-order handling, image
deltas, invalidation, missing/invalid/failed usage, integer bounds, changed trim
decisions, and trim acknowledgement. The external public agent consumer checks
measured estimates after an actual run_js/child cycle and the next queued task.
Whole-tree results pass by package: the initial broad run passed all other
packages, while two trim E2E cases still used a constant 100-token fixture.
Those fixtures now report input-size-dependent usage, and the entire E2E
package passes afterward. Existing default fixture usage is unchanged elsewhere.
Affected core/public/bridge/executor race checks and provider race checks pass.
Whole-tree vet, including E2E sources, public type fixtures and lint pass.
The final provider SSE top-level-error/auth-precedence delta is covered by
provider unit/race/vet, without repeating unaffected checks.

The final checkout and standalone module-artifact consumers pass in portable
and GoHost modes. A temporary CGO-disabled CLI exercises a real loopback
429 retry with a 5 ms hint, one run_js tool, measured-context continuation,
quota fail-fast, and embedded member references. Three transport attempts
produce two logical model calls in the retry/tool case; quota produces one.
No paid provider call occurred. Independent bounded review of context/trim
accounting found no actionable findings; provider code and throughput
measurements were outside that review.

Source hashes, logs, module manifest, native fixture output and review records
are at `/var/tmp/noe-agentlib-pi-9y8pf8ac`. This change is on `main` and awaits
a release. Final state is recorded in the
[session handoff](stdlib-handoff.md#current-continuation--provider-retries-and-measured-context).

## References

- Installed pi 1.5.2: `/home/agent/.pi/agent/install/releases/1.5.2/node_modules/@earendil-works/`.
- [Provider retry hints](https://github.com/earendil-works/pi/blob/v1.5.2/packages/ai/src/utils/provider-retry.ts).
- [Transient and quota classification](https://github.com/earendil-works/pi/blob/v1.5.2/packages/ai/src/utils/retry.ts).
- [Measured context estimates](https://github.com/earendil-works/pi/blob/v1.5.2/packages/coding-agent/src/core/compaction/compaction.ts).
- [Existing revision-scoped preparation reuse](agentlib-optimization.md#revision-scoped-preparation-reuse--2026-11-17).
