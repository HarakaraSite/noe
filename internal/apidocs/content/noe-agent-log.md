# `noe:agent-log`

### API information

Public exports: `analyze_log`.

Reference: `noe:agent-log`. Owner: noeagentlib.

Type declarations: `agent/types/noeagentlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires agent/log.Bind. NOE's CLI installs it in the main runtime and omits it from Workers; analysis reads only caller-supplied data.

Public-definition source: agent/log/binding.go, internal/logbinding/binding.go. Reading this reference evaluates no target module and makes no provider request.

`noe:agent-log` reconstructs one run from caller-supplied JSONL event data. It
does not open files, poll for changes, wait for an agent, call a provider, or
create a Host. The module is available when the runtime installs the log
binding; the embedded catalog is complete regardless of selected profile
capabilities.

## analyze_log

```js
import { analyze_log } from "noe:agent-log";
```

`analyze_log(input, options) -> Snapshot` accepts a complete event snapshot as
a string or `Uint8Array`. `options.runId` is required. Optional fields are
`cursor`, `includeThinking`, and `includeOutput`; unknown options reject.
`cursor` has the shape `{executionId, seq}` and can be copied from a previous
snapshot. Thinking and captured process output are excluded from returned
events by default.

The parser computes run state from every complete line in the supplied input.
An unterminated final line is ignored. A cursor filters only the returned
`events` delta; it does not change status, final text, context, usage, or the
next cursor. Pass the full updated log snapshot on each call. Read the file
with the selected filesystem or another caller-owned source before calling
`analyze_log`.

```js
import { readFile } from "node:fs/promises";
import { analyze_log } from "noe:agent-log";

const text = await readFile(".noe/events/latest.jsonl", "utf8");
const first = analyze_log(text, {runId: "run-id", includeOutput: true});
const updatedText = await readFile(".noe/events/latest.jsonl", "utf8");
const next = analyze_log(updatedText, {runId: "run-id", cursor: first.cursor});
console.log(next.status, next.events);
```

The returned snapshot includes reconstructed `status`, `phase`, `action`,
`lastMessage`, optional `finalText`, `error`, `usage`, `lastToolResult`, a
`cursor`, and the selected `events` delta. An unknown run returns status
`unknown` with an empty event list. Ambiguous run IDs across executions and a
cursor from a different execution reject.

S4 results preserve lastToolResult.diagnostics regardless of includeOutput. Returned data and captured output are included only when includeOutput is true; explicit null data remains distinct from absent data. execution.diagnostic events preserve agent-task VM failures separately from stderr. An event without a runId belongs to the instance/execution log and is not invented into a run-specific snapshot.
