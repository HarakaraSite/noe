# noebot: concept discussion

Discussion date: 2026-10-27. The name "noebot" is provisional.

[Instruction-source discussion](instruction-source-selection-discussion.md) ·
[Development guide](README.md)

## Status and selected direction

The user selected the NOE + JavaScript approach and limited channels to
Web UI and Telegram. This is a concept record, not settled implementation
requirements or authorization to build, deploy, install, or call services.

Build a thin application around existing NOE agents rather than porting
nanobot's Python implementation or rebuilding its model/tool engine.

A Go application embedding the public `stdlib` and `agent` packages remains
a fallback for a concrete host-integration need, such as a required SDK or
service facility that is awkward in NOE JavaScript. More conversations or
agents alone are not a reason to switch to Go.

## Product scope

The intended initial experience:

- Talk through Web UI or Telegram.
- Continue a live conversation while separate work is running.
- Request a task, inspect progress, add instructions, or cancel it.
- Save and consult ordinary Markdown notes.
- Schedule one-off and recurring work and receive its results.

Do not initially include a permanent searchable chat archive, automatic
Dream-like memory processing, a vector database, a generic delivery
platform, every chat channel, or a new workflow/state-machine framework.

A trusted single-user deployment and initially fixed workspace were
recommended as a small starting scope, not agreed multi-user or workspace
isolation requirements. NOE is not an OS sandbox.

## Architecture

```text
Long-lived NOE JavaScript application
  ├─ Web UI adapter
  ├─ Telegram adapter
  ├─ conversation instances
  ├─ independently owned task/job instances
  ├─ application-owned scheduling
  └─ ordinary Markdown notes and necessary job/notification state
          │
          └─ existing NOE agent/model/tool execution
```

Web UI over HTTP/SSE and Telegram HTTP long polling are plausible initial
transports, not finalized protocol choices. They avoid assuming arbitrary
Node.js SDK compatibility or requiring a new runtime communication API.

## Conversation and work are separate

A conversation is the place where the user interacts; it can refer to
multiple tasks. A long task must not prevent further conversation.

Do not spawn a separate worker for every short reply. Use live conversation
instances and separate workers when actual work benefits from independence.
Existing queue, wait/result, observation, steer and cancellation operations
provide the execution controls; no second agent engine is needed.

The long-lived application must own conversation agents and independent
job agents as siblings. A normal child spawned inside a conversation's
tool execution belongs to that parent run and is cancelled/joined when its
owner ends. `keepAlive` does not detach it.

Conversation agents can request job creation through an app-controlled
dispatch path. UI actions and schedules use the same job-starting path.
The concrete dispatch API is not decided.

Closing a browser, completing a conversational turn, or replacing a
conversation instance should not define the lifetime of independent work.
Surviving application shutdown/restart is a different, undecided requirement;
ordinary NOE task ownership does not provide durable job resumption.

## Conversation context is not a permanent chat archive

Keep useful conversation context in live NOE instances during the service
lifetime. A permanent transcript store and history search are not required.

The proposed simple restart behavior is to create a new conversation and
read relevant notes, not reconstruct the exact old provider/tool history.
This remains a proposed trade-off to confirm before implementation.

Native event logs can be an opt-in diagnostic facility. Do not make them
the app's conversation-resumption format or a mandatory chat archive.

Possible controls such as new conversation, stop task and task status were
suggested; exact commands and Web UI interactions are not finalized.

## Memory means ordinary notes

The user prefers "write it in a memo" over a separate automatic memory
system. Support reading, editing and deleting inspectable Markdown notes.
Do not assume every fact should be stored or automatically promoted to
persistent memory.

Keep notes in application-owned storage independent of the working
repository. An example could be `$XDG_DATA_HOME/noebot/notes/`; the final
path is not decided. Do not automatically load all notes into every worker's
system instructions.

At new conversation startup or when relevant, the app/agent reads selected
notes as ordinary context. Saving a note does not require mutating a live
system message. A promise that something was remembered is not a substitute
for actually writing the file.

Keep stable application instructions separate:
`$XDG_CONFIG_HOME/noebot/instructions/` is the proposed app-owned location.
See the [instruction-source discussion](instruction-source-selection-discussion.md)
for explicit source selection and why live reload is deferred.

## Schedules and notifications

Scheduling/recurring execution is desired and merits saved definitions
independently of chat history. Proposed task forms are one-off time,
interval and cron-like recurrence.

A definition needs the work to do, its timing/enabled state, and the
conversation/channel to which results belong. Ordinary application code
waits for due times and dispatches jobs; do not spend model calls waiting
for time or use routine decision-model approvals.

"Delivery state" was clarified to mean only the necessary distinction
between work finishing and notifying the user:

- Where should the result be sent?
- Did the send succeed or fail?

Keep this with the job/schedule result initially. No read receipts, general
outbox framework or elaborate retry service is presently required. A
platform accepting a send is not proof the user read it.

The desired flow is:
due schedule -> app-owned job -> NOE execution -> result notification.
The system must not confuse model/task completion with successful message
delivery.

## Development work

Normal requests execute using NOE's existing JavaScript/tool capabilities.
Do not send ordinary conversation through the development workflow or
require parent/worker/reviewer ceremonies for every task.

A development request can invoke the existing standalone workflow as a
separate managed job when that procedure is appropriate. Its actual target
cwd and instruction source selection must be correct; merely mentioning a
path in a prompt does not change command/file resolution.

The workflow keeps its own instructions, independent reviews and Git
responsibilities. Bot persona/memory are not automatically inherited.
No mandatory Jev or other model-based stage approval is part of this bot
concept.

## Reference: what nanobot's memory does

The source README, architecture, memory and automation docs were inspected
on 2026-10-27. Current-source nanobot is broader than the proposed initial
app: channels, WebUI, tools, sessions, memory and automation.

Its documented memory design distinguishes live conversation, summarized
`memory/history.jsonl`, and durable `SOUL.md`, `USER.md` and `MEMORY.md`.
Consolidation produces summaries; scheduled/manual Dream uses those summaries
to update durable files. This is model-assisted memory selection, not just
mechanical transcript storage. A vector database is not the premise of
that documented design.

Borrow the separation of concerns, not the full feature set. Dream,
automatic memory curation, permanent transcript storage, voice/Office
support and multi-channel packaging are not initial requirements here.

Primary references:
- [nanobot overview](https://github.com/HKUDS/nanobot)
- [Architecture](https://github.com/HKUDS/nanobot/blob/main/docs/architecture.md)
- [Memory](https://github.com/HKUDS/nanobot/blob/main/docs/memory.md)
- [Automations](https://github.com/HKUDS/nanobot/blob/main/docs/automations.md)
- [Chat channels](https://github.com/HKUDS/nanobot/blob/main/docs/chat-apps.md)

These links describe inspected current source, not a verified deployed
nanobot release or an implementation dependency of noebot.

## Decisions still needed before implementation

- Exact Web UI/Telegram interactions, conversation identity and job dispatch.
- Which instructions are app-wide versus conversation/worker-specific.
- Final app config, notes and schedule-storage paths.
- Timezone handling, downtime/missed runs and overlapping recurring jobs.
- Required persistence of task states/results; whether running work needs
  any restart handling beyond reporting interruption.
- Notification failure behavior and how users retrieve a completed result.
- Trusted-user admission and the concrete workspace/OS-permission boundary.

Do not use these open questions to expand the initial app into a generic
platform. No code, dependencies, live channel tests or deployment were
created by this concept discussion.
