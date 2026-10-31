# Instruction-source selection: discussion and critical review

Discussion date: 2026-10-27.

[Current instruction contract](../instructions.md) ·
[noebot concept](noebot-concept-discussion.md) ·
[Development guide](README.md)

## Status

The user supported the revised direction after two independent read-only
design reviews, requested this memo, and subsequently selected CLI startup
options as the initial entry point. This is a discussion record, not fully
settled implementation requirements or authorization to implement.
No new options, APIs, reload mechanism or application discovery have been
implemented by this discussion. The current contract remains authoritative.

## Problem and intended separation

The current CLI automatically includes common instructions and instructions
under its starting working directory. That is convenient for development
assistance, but couples application behavior to where the process works.

An application such as noebot needs its own guidance independently of its
working repository. A separate development workflow needs different guidance.
Putting either application's identity into the common directory contaminates
other uses; putting it into a project directory ties it to that project.

The user's proposed application-owned locations are appropriate:

- Common NOE preferences: `$XDG_CONFIG_HOME/noe/instructions/`.
- noebot preferences: `$XDG_CONFIG_HOME/noebot/instructions/`.
- Workflow preferences: `$XDG_CONFIG_HOME/noeworkflow/instructions/`.
- Optional project preferences: `<starting cwd>/.noe/instructions/`.

When XDG_CONFIG_HOME is unset or empty, the examples use `~/.config/`.
These application directories are a proposed convention, not directories
currently discovered by NOE.

## Current behavior verified in source

- CLI composition is built-in runtime/API facts, common instructions,
  workspace instructions, then explicit `config.system`.
- Workspace means the actual starting cwd, not Git-root or ancestor discovery.
- Immediate lowercase `.md` files are read in filename order; no recursion.
  Contents concatenate; identical filenames do not override earlier files.
- `config.system` appends text. It cannot subtract automatically loaded
  common/workspace instructions.
- Every instance snapshots instructions at its first actual task. Queued
  tasks reuse that snapshot and conversation. New children independently
  read the host source; parent `system` is not inherited automatically.
- Custom Go hosts already select their source through
  `HostConfig.Instructions`; directory discovery is not automatic there.
- The managed CLI path installs the common/workspace source unconditionally.

Relevant source:
[instruction reader](../../internal/noeprofile/instructions.go),
[managed host construction](../../internal/runtime/managed.go),
[instance snapshots](../../internal/agent/host.go),
[system composition and conversation validation](../../internal/agent/conversation.go),
[public Go host](../../agent/host.go).

## Preferred minimal direction

Expose one launch/managed-host selection primitive: an explicit, ordered
list of external instruction directories.

| Selection | Proposed meaning |
| --- | --- |
| Unspecified | Preserve today's common + starting-cwd workspace defaults |
| Explicit list | Read exactly those directories in order, replacing defaults |
| Explicit empty list | Read no external instruction directories |

Built-in runtime/API facts and explicitly supplied `system` remain.
An explicit list can include or omit common as well as workspace; common
is useful, not a mandatory hidden extra when the caller selects a list.

Examples, not API syntax:

| Application | Selected directories |
| --- | --- |
| Ordinary NOE | Existing defaults unless explicitly changed |
| noebot | Common + noebot; normally omit project instructions |
| Development workflow | Common + optional workflow preferences + project instructions when wanted |

Keep the current directory-reading semantics rather than adding priorities,
recursive search, filename overrides or tagged source objects.
Initially support directory selection only: callers can already read an
individual file into explicit `system` or task text.

The selected locations belong to application/host startup. Preserve
per-instance first-task snapshots of their contents. A source list is not
an instruction-copy inheritance mechanism and does not make existing
instances hot-reloadable.

Do not initially add:

- Separate common/workspace/additional selection frameworks.
- A NOE-wide application registry or special handling of application names.
- Per-agent source overrides with new inheritance rules.
- Implicit application identification from cwd.
- Any change to filesystem or OS permissions: selecting instructions is
  guidance composition, not sandboxing.

### Follow-up decision: use CLI startup options

The user selected CLI startup options as the initial source-selection entry
point. Applications or their launch definitions can supply those options.
Do not initially add a JavaScript API that mutates host configuration.

The current fine-grained Markdown instruction files are intentional and
remain unchanged. Their number is not itself a problem; this work concerns
selecting reading locations, not merging files or changing their contents.

Do not introduce a new centralized config file or extend the current
provider-oriented config.json for this change. Consolidation into
config.json may be considered later if there is a concrete need.

Exact CLI spelling, repeated/list syntax, explicit-empty representation and
application launch ergonomics remain to be specified. Users should not
have to reconstruct an application's directory list manually on every run.

## Application-wide is not role-wide

Use host-selected directories only for guidance suitable for all agents
in that application. Give conversation agents, implementation workers,
reviewers and workflow parents their role-specific instructions explicitly
through `system` or task text.

The current workflow already reads its adjacent
[workflow.md](../../.noe/lib/workflow/workflow.md) from the
[module-relative launcher](../../.noe/lib/workflow/workflow.js).
Keep that parent procedure explicit; it need not move into a mandatory
user config directory. A workflow config directory can hold optional user
preferences.

A separately launched workflow selects its own host/source set. It should
not automatically inherit the bot's persona or full memory just because
noebot requested the work.

Common must contain genuinely cross-application preferences. Later text
does not reliably cancel conflicting earlier instructions.

## Revised view on memory and live reload

The earlier suggestion to put all remembered facts into an automatically
loaded instruction directory is not the preferred initial design.

Reading notes at application/conversation startup is not the same as
making them system instructions. Store notes as ordinary application-owned
Markdown; read relevant contents when a conversation starts or needs them.
An active task's changed request can use existing steering.

Defer live system replacement. The current system message is a protected
conversation entry, queued tasks reuse native conversation state, and
append validation requires old entries to remain unchanged. A coherent
reload would need native revision changes, JavaScript history consistency,
an in-flight-work boundary, and a session/cache policy. It is not just
another filesystem read. Provider incompatibility is not established by
this observation and should not be asserted.

For the initial app:

- Updated instructions apply to new instances.
- Replace an affected conversation instance when its baseline must change.
- Read mutable notes as ordinary files.
- Use steer for the current task.

Instance replacement is an explicit conversation reset, not transparent
history restoration. App-owned independent jobs need not be cancelled by
that reset.

Do not initially add live reload, automatic watching, append-only
instruction accumulation or automatic updates of existing children.
If a later concrete use requires live replacement, specify its semantics
separately rather than treating it as a prerequisite for noebot.

## Review outcome and remaining decisions

Two independent reviewers inspected actual code and docs. Both recommended
the single launch-scoped replacement list and deferring live reload. Their
main corrections were source-selection versus content-loading, host-wide
versus role-specific guidance, mutable notes versus system mutation, and
application-owned job lifetimes. No implementation or tests were run by
those reviews.

Before implementation, settle only the consequential details:

1. Exact CLI syntax for the selected startup entry point, including repeated/list options and explicit-empty.
2. Path resolution and the list's unset/empty distinction, while preserving
   the existing missing-directory and unreadable-file behavior.
3. Which app guidance belongs to every agent versus particular roles.
4. Whether conversation reset is sufficient when baseline guidance changes.
