# `noe:agent`

`noe:agent` exports `run_agent`, `spawn_agent`, `observe_agent`, `queue_agent`, `steer_agent`, `interrupt_agent`, and `list_models`. These are managed-agent operations, not aliases for an autonomous object API. The module is available in the main runtime, not Workers. Root launches require explicit `provider`, `model`, and `effort`; providers and models are selected by the caller. Model inference uses the configured provider, and provider calls may incur cost.

A managed agent owns its task runtime and descendants. A returned snapshot is a point-in-time plain object, not a live handle. Use `observe_agent` to refresh it. Accepted task failures are represented in the result or snapshot; ordinary task failure does not make `run_agent` reject. Input, configuration, or acceptance failures reject. Cancellation follows ownership and recovers work owned by the current evaluation.

### Public exports

| Export | Purpose |
| --- | --- |
| `run_agent` | Start a task and wait for its instance to stop. |
| `spawn_agent` | Start an instance and return its ID after launch and initial-task acceptance. |
| `observe_agent` | Read a live snapshot or inspect an existing event log. |
| `queue_agent` | Submit a future task to a keep-alive instance. |
| `steer_agent` | Add guidance to an active task. |
| `interrupt_agent` | Cancel one task or stop an instance and its queue. |
| `list_models` | List configured catalog entries without contacting a provider. |

## AgentConfig

`run_agent(config, instruction, options?)` and `spawn_agent(config, instruction?, options?)` accept the same config. Supported fields are:

| Field | Meaning |
| --- | --- |
| `provider`, `model` | Required for a root agent. A child inherits each omitted value from its parent. |
| `effort` | `auto`, `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, or `max`; required on a root and inherited by a child when omitted. `auto` leaves provider defaults unchanged. |
| `name` | Display name; defaults to `agent`. Not inherited by a child. |
| `system` | Additional system instructions; defaults to empty and is not inherited by a child. |
| `maxTokens` | Nonnegative output-token limit; `0` leaves the provider default. Inherited by a child when omitted. |
| `maxSteps` | Nonnegative model-call limit for one task; `0` is unlimited. Not inherited by a child; each queued task gets a fresh counter and uses the instance's configured value. |
| `cacheKey` | Optional provider session/cache identifier. Omitted uses the run ID; an explicit value is reused for each queued task, while `""` omits the provider session ID. It does not save or restore conversation history. |
| `hooks` | Ordered ESM module paths or import-map aliases. Children inherit when omitted; `[]` clears inherited hooks. See [Hooks](#hooks). |
| `contextBudget` | `{inputTokens}` sets a positive safe-integer total-input allowance; `null` clears the inherited value. |
| `contextPolicy` | `{retainRatio?, responseReserveTokens?}` controls automatic trimming. Omitted or `{}` enables it; `null` disables trimming. `retainRatio` is between 0 and 1, default `0.1`; reserve tokens must be positive. A reserve affects window-derived budgeting, not provider output limits. |
| `executionLimits` | Root-only group limits: `{providerCalls?, agentStarts?, queuedTasks?, hookCalls?}`. Each is a nonnegative safe integer; omitted dimensions are unlimited and `0` admits no work for that dimension. Children share the root limits. |

The `contextBudget.inputTokens` scale includes fixed instructions. It is different from the execution-history allowance used by `contextPolicy`. If both model-window metadata and an explicit budget are unknown, NOE uses a 1,000,000-token input-budget fallback; this is not a model-capacity guarantee. A root's execution limits are shared by its descendants and owned decision calls.

## run_agent

`run_agent(config, instruction, options?) -> Promise<AgentSnapshot>` starts one task and waits until its instance stops and cleanup completes. `instruction` must be a nonempty string. `options` supports only `{keepAlive?: boolean}`; the default is `false`.

With `keepAlive: true`, `run_agent` remains pending until that instance is stopped. Use `spawn_agent` when you need to manage or queue work while it is running. Task outcomes with `state: "failed"` or `"cancelled"` are returned in `snapshot.lastResult`; the Promise is for task acceptance/lifecycle, not task success alone.

```js
import { run_agent } from "noe:agent";

const snapshot = await run_agent({
  provider: "openrouter",
  model: "provider/model-id",
  effort: "auto",
  maxSteps: 4,
}, "Summarize README.md in three bullets.");
if (snapshot.lastResult?.state === "completed") {
  console.log(snapshot.lastResult.answer);
}
```

## spawn_agent

`spawn_agent(config?, instruction?, options?) -> Promise<{agentId}>` returns after the instance is created and an initial task, if supplied, is accepted. It does not wait for the first model response or task completion. If `keepAlive` is false (the default), `instruction` is required. With `keepAlive: true`, omit `instruction` to start an idle instance, or provide an initial task.

The new instance belongs to the current owner. A fresh instance has its own conversation and system instructions. A new child inherits provider, model, effort, maxTokens, hooks, contextBudget, and contextPolicy when those fields are omitted; it does not inherit name, system, maxSteps, or cacheKey. Root execution limits remain shared and cannot be overridden by a child.

## observe_agent

`observe_agent({agentId}) -> Promise<AgentSnapshot>` returns the current live snapshot immediately. Live observation accepts only `agentId`.

For an existing event log, pass `{logPath, runId, cursor?, wait?, includeThinking?, includeOutput?}`. Relative `logPath` is resolved from the runtime working directory. `wait` defaults to `false`; when true, observation waits for a terminal run state. The result contains the observed state, event differences, any settled final text or error, and a cursor for the next read. Pass that cursor on later reads to request only new events. Reading a log does not make a model request.

## queue_agent

`queue_agent({agentId, instruction, commandId?}) -> Promise<{commandId, status}>` submits a nonempty instruction to a `keepAlive` instance in `starting`, `running`, or `idle` state. A queued task receives a new run ID and starts after earlier queued work. It continues the instance's confirmed conversation after context trimming; trimmed history is not restored. Its runtime, hook-module state, cancellation scope, and `maxSteps` counter are new.

If omitted, `commandId` is generated. Repeating the same ID with the same instruction returns its current status without submitting a duplicate; reusing that ID with different text is an error. Finished instances and instances started without `keepAlive` reject queue requests.

## steer_agent

`steer_agent({runId, instruction, commandId?}) -> Promise<{commandId, status}>` adds a nonempty instruction to an active task without interrupting its current work. The steer is applied at a model-decision boundary; acceptance does not mean it has already been applied. A supplied command ID makes retries idempotent for the same instruction.

## interrupt_agent

`interrupt_agent({runId})` cancels one managed task. `interrupt_agent({agentId})` stops an entire instance, including queued tasks. Specify exactly one of `runId` or `agentId`. The Promise resolves when cancellation is accepted, not when cleanup finishes; use `observe_agent` to confirm the resulting state. Ownership scope is enforced, and an owned runtime reclaims its work when its owner ends.

## list_models

`list_models({provider?, kind?} = {}) -> Promise<ModelInfo[]>` reads the merged catalog of curated model metadata without loading credentials or contacting a provider. `kind` is `agent`, `decision`, or `image`. Entries are sorted by provider and model and contain `provider`, `model`, `kind`, and available `description`, `tags`, `capabilities`, `contextWindow`, `maxOutputTokens`, and `efforts` fields. Missing metadata means unknown; the list is selection guidance, not a benchmark or allowlist. Directly specified models are not restricted to listed entries.

## AgentSnapshot

Live calls return a point-in-time object with `agentId`, `parentRunId`, `name`, `state`, `reason`, `keepAlive`, `currentRunId`, `queueLength`, and `lastResult`. Optional fields include `groupId`, `activityId`, `causedBy`, `origin`, `execution`, and `context`. State progresses through values such as `starting`, `running`, `idle`, `stopping`, `stopped`, and `failed`; use the latest snapshot for current state.

## AgentSnapshot.lastResult

The latest task outcome contains `runId`, `commandId`, `state`, and `answer`; `error` and `errorDetails` may be present. Successful task answer text is in `answer`. A normal task failure is reported as `state: "failed"`, and cancellation as `state: "cancelled"`; inspect the error fields instead of assuming every accepted task completes successfully.

## hooks

`hooks` names an ordered list of local ESM modules or import-map aliases. Relative paths resolve from the host working directory; imports inside a hook resolve from that module. Modules are loaded for each task and are not reloaded during the task. The child inherits the list when omitted and replaces it when set; `hooks: []` clears it.

Each module may export any subset of `beforeModelRequest(event, api)`, `afterModelResponse(event, api)`, `beforeTool(event, api)`, and `afterTool(event, api)`. Handlers run in registration order for a phase. `beforeModelRequest` runs once per logical model step; `afterModelResponse` runs once for each completed model response; tool hooks run for each tool call. Event objects are JSON snapshots; mutating them does not change the original provider request, tool result, or another callback.

Model hooks' return values are ignored. A tool hook returns `undefined` for no change. `beforeTool` may return `{arguments}` and `afterTool` may return `{result}`. For `run_js`, `afterTool` may also return `{result, imageIds}` to select a unique subset of the current `event.images` IDs. It cannot change tool identity or success state. Hook exceptions fail the task; completed work is not replayed. Interrupt can stop a handler before it finishes.

```js
// hooks.js
export async function beforeModelRequest(event, api) {
  const context = await api.context.get();
  if (context.executionContext?.nearBudget) {
    await api.context.setRuntimeInfo({key: "budget", text: "Prefer a concise response."});
  }
}

export async function beforeTool(event) {
  if (event.toolName === "run_js") return {arguments: event.arguments};
}
```

## HookAPI.agents

The callback's `api.agents` object exposes `run_agent`, `spawn_agent`, `observe_agent`, `queue_agent`, `steer_agent`, `interrupt_agent`, and `list_models` with the same argument and return contracts as this module. It is bound to that hook invocation, preserving its ownership and cause information. Requests made through the callback API are cancelled when the callback returns; an agent started by a hook remains owned by the run and is recovered with it. Prefer `api.agents` when the action should be attributed to that hook invocation.

## HookAPI.context.get

`await api.context.get() -> ContextSnapshot` reads the current run's request-budget and context snapshot. It may include the previous request measurement, model-window metadata, next-request estimate, execution-history budget, and trimming state. Unknown measurements remain absent or `null`; estimates are byte-based approximations, not provider-tokenizer measurements.

## HookAPI.context.setRuntimeInfo

`await api.context.setRuntimeInfo({key, text})` adds request-only text during `beforeModelRequest`. Both values must be strings and `key` must be nonempty. Setting the same key replaces its prior value for the logical step. This does not append to conversation history and is unavailable in other hook phases.

## HookAPI.execution.get

`await api.execution.get() -> ExecutionSnapshot` returns the owning group summary and the current hook invocation's cause information. The summary includes `groupId`, `counters`, `limits`, and `elapsedMs`, plus `usage` or `stop` when available. Counters include provider calls, agent starts, queued tasks, hook calls, and active work. Reading the snapshot does not call a model.
