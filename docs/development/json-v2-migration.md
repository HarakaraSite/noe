# JSON v2 migration

NOE-owned JSON processing uses `encoding/json/v2` with its native defaults.
The CLI, provider adapters, control and task RPC, event logs, process helpers,
stdlib module loader, and agent library share this policy. Go 1.27.2 is already
the project's minimum toolchain, so the migration adds no dependency or build
experiment. Upstream SDKs retain their own serialization implementations.

The migration changes the API used by NOE itself. Although Go 1.27 already
implements `encoding/json` on the v2 engine, that API selects legacy behavior.
NOE does not use `DefaultOptionsV1` or a wrapper that reinstates those defaults.

## Native behavior

JSON processing rejects duplicate object names and invalid UTF-8 by default.
Struct fields match JSON names case-sensitively. Nil collections normally
encode as empty arrays or objects; ordinary output does not require HTML
escaping or deterministic map-key ordering. Input types use explicit JSON
tags where the public field name differs from the Go name.

Object ordering is not a transport contract. Tests comparing retry payloads
compare JSON structure while preserving number spellings and string contents.
Map serialization is deterministic only where the serialized bytes identify
a provider or image profile cache entry.

## Preserved contracts

| Contract | Implementation |
| --- | --- |
| An explicit zero admission limit differs from absence. | Native v2 already retains numeric zero and false through nonnil pointers. Their existing pointer representation is preserved. |
| An explicit empty answer or hook collection differs from absence. | Presence-sensitive pointers to empty strings or collections use `omitzero` alone, retaining supplied empty values. |
| Explicit result data or configuration `null`, `{}`, and `[]` must remain present. | Presence-sensitive `RawMessage` fields use `omitzero` alone; `omitempty` would also omit an empty JSON representation. |
| Sequence counters and opaque numeric parameters must not round through float64. | `internal/jsonutil.PreserveNumbers()` supplies a v2 unmarshaler for exact interface numbers, leaving other v2 defaults intact. |
| A nil process environment inherits the parent's environment; an explicit empty slice replaces it. | The supervisor IPC representation omits only nil `Config.Env`. |
| Event records are complete JSONL writes, including when a renderer receives them. | The event writer marshals a detached record, appends one newline, and writes the complete record once. |
| Event durations are nanoseconds. | Only the detached wire record converts its top-level `time.Duration` value to an `int64`; Go observer events are unchanged. |
| Native output can contain arbitrary bytes. | Raw output remains byte-preserving. Its JSON text projection replaces invalid UTF-8 at the byte-to-text boundary and retains valid characters split across writes or capture buffers. |

Public Go fields retain the `encoding/json.RawMessage` and `encoding/json.Number`
types where replacing them would break existing callers. These are data-type
compatibility choices, not v1 encode/decode operations. Existing custom
`MarshalJSON` and `UnmarshalJSON` methods now perform their work through v2.
Consumer fixtures that use the old package continue to exercise interoperability.

Go's JSON migration does not change QuickJS's JavaScript `JSON.parse` or
`JSON.stringify` implementation.

## Verification

Focused regressions cover strict syntax, exact generic numbers, raw-value and
pointer-zero presence, supervisor environment inheritance, complete event
records, duration units, and valid/invalid split UTF-8. Integration verification
also exercises public Go consumers, provider fixtures, Workers, process helpers,
and local CLI execution.

Whole-tree unit/local CLI E2E and broad race runs covered the production
changes, including external Go consumers. Three verification assumptions
initially failed: context-byte estimates still used v1 formatting, an
observation assertion required a particular object-member order, and one
socket shutdown check did not join its native operation before checking Busy.
Those tests were corrected and the affected normal/race cases passed; passing
results for unchanged production sources were reused. Full vet and vet for
the adjusted test packages passed.

A bounded independent review found the supervisor environment inheritance
regression described above. Its fix and an end-to-end nil/empty environment
test were independently checked; no actionable findings remain.

Implementation evidence, full-run logs, focused reruns, and source hashes are
retained at `/tmp/noe-json-v2-migration-0rw5q7sj`.

## Live-provider JSON verification

Six real-provider cases passed through the shared agent library: OpenCodeGo
Chat and Responses, OpenRouter Chat, OpenAI API Responses, ChatGPT OAuth
Responses, and a public Go `agent.Host`/`Client.Run` consumer using OpenRouter.
The cases completed 14 model calls and 12 `run_js` executions. Tool results,
hook modifications, context and execution snapshots, JSONL events, usage, and
typed Go task-result retrieval retained the expected JSON values.

Coverage includes explicit top-level `null`, `[]`, and `{}`, zero and false,
Japanese text and emoji, escaped characters, fractions, and the largest safe
JavaScript integer. A larger integer was tested as text. Random proof values
added by `afterTool` were returned by the models, confirming the effective
JSON tool result reached each provider's continuation request.

All cases use `noe -> agentlib -> provider`; the direct Go case changes the
entry point, and API-key versus ChatGPT OAuth changes provider authentication.
These are successful dummy-task exchanges, not a claim about every provider
response, arbitrary-precision JavaScript numbers, or long-running agents.
Live evidence is retained at `/tmp/noe-json-v2-live-sam0z9ch/results.json`.

## References

- [Go 1.27 release notes](https://go.dev/doc/go1.27)
- [JSON v2 API](https://pkg.go.dev/encoding/json/v2)
- [Official migration guide](https://go.dev/doc/jsonv2-migration)
