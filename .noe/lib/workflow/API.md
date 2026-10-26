# Portable workflow v1 — bootstrap API

This folder has no dependencies on other workspace libraries. Copy it unchanged.
The controller is **standalone-only**: launch an awaited NOE subprocess with the
target Git repository root as its actual cwd. Importing `runWorkflow` inside a
managed `run_js` task does not provide steer-aware continuation. Completion-only
waits are intentional; no polling, Decision API, automatic resume, or live
reconfiguration channel is provided.

## Slice 1 entry points

- `workflow.js`: `runWorkflow(config, {runtime?, stopAfter?})`,
  `planWorkflow({config, requirements, rules, ids, pool, store})`,
  `PLAN_CONTRACT`, `reviewContract(mode, revision)`.
- `runtime.js`: `createRuntime({cwd?, agents?, execute?})`,
  `prepareRepository(runtime, config)`, `git(runtime, args)`,
  `createArtifactStore(base)`, `createAgentPool(runtime, store)`,
  `WORKER_BOUNDARY`.
- `protocol.js`: `validateConfig`, `requirementInventory`, `parseResult`,
  `validatePlan`, `validateReview`, `validateImplementation`,
  `validateCommand`, `repositoryPath`, `implementationRole`,
  `DEFAULT_ROLES`, `DOCUMENT_REQUIREMENT_ID`, `WorkflowError`.

Only `stopAfter: "planning"` is implemented (and is currently the default).
Success returns `{status: "planned", plan, reviews, artifactDirectory, baseline}`.
It does **not** mean implementation completed. Other phases reject. Once a run
artifact store exists, failures persist an outcome and rethrow the original error
with `artifactDirectory`; cancellation is never approval. Configuration and Git
preflight failures occur before a run starts and may have no artifact directory.

Configuration is JSON, version 1:

```json
{
  "version": 1,
  "requirementsFile": "requirements.md",
  "git": {
    "branch": "feature",
    "remote": "origin",
    "pushBranch": "feature",
    "branchPushSafe": true,
    "releaseSafetyReason": "Verified branch pushes do not trigger releases"
  }
}
```

Ordinary English/Japanese Markdown needs no IDs or source edits. If there are no
recognized ID declarations and no explicit inventory, `requirementInventory`
returns the fixed `DOCUMENT` group (`DOCUMENT_REQUIREMENT_ID`). This group anchors
the **entire unchanged source**, not a planner-generated summary. The planner must
enumerate every required behavior as a separate acceptance entry, identifying
its source section or quoted wording in `behavior` and its actual `usagePath`.
Both independent reviewers receive the entire original source on every revision
and must report any omitted behavior. A generic "implement the document" entry
cannot approve. Semantic source coverage is reviewed, not claimed to be solved
by a generic Markdown parser.

Recognized declarations such as `- R1: ...` or `## R1: ...` remain a stronger
automatic ID-indexed path. Optional `requirementIds` provides a complete
caller-owned index for custom ID formats; these IDs must occur in the source.
Duplicate declarations or omitted declared IDs fail closed on that explicit-ID
path. Only recognized declarations create automatic IDs: body references,
examples and record IDs such as `R42` in `- R1: Import record R42 unchanged`
do not create phantom requirements or need inclusion in an explicit `["R1"]`
inventory. Mixed/unrecognized declaration formatting remains original-source
prose for coverage review, not an inferred ID. In either mode, IDs are only an
index: reviewers must audit **all** required source behaviors, including prose
outside numbered declarations.
The planner cannot change the inventory or requirements.

When `rulesFiles` is omitted, an existing root `AGENTS.md` is loaded; if absent,
the run records that fact and proceeds with no root rule file. Explicit
`rulesFiles` entries (including `["AGENTS.md"]`) are required reads: a missing or
unreadable configured file fails before agent work. `rulesFiles: []` supplies no
root files. All worker/reviewer system prompts still require discovering and
following applicable nested `AGENTS.md` and other repository work rules before
inspecting, planning or changing their paths. Missing root rules never imply no
nested rules.

Optional `roles` overrides any of `general, planning, review, routine, difficult`
with `provider, model, effort` fields. Defaults are Sol/high for general/planning
and difficult work, Sol/xhigh for every review, Luna/max for routine work.
Per-slice difficulty requires a reason; escalation requires nonempty reason and
concrete evidence. No model selection contacts a provider.

## Exact agent protocol

`PLAN_CONTRACT` is the complete plan shape; sample values are not defaults.
Every requirement maps to acceptance behavior, an actual usage path and one or
more slices; the reverse mapping must agree. Dependencies refer only to earlier
slices. Every slice owns literal repository-relative paths and nonempty focused
verification commands. Integration commands are mandatory; regression commands
may be empty only with an impact rationale. Explicit optional live checks have
`{reason, command}` entries; no live checks execute during slice 1.

Commands are `{argv: ["executable", "arg"], cwd: ".", purpose: "..."}`, not shell
strings. Shells are usable only by explicitly naming one in argv.
A review is exactly `{type:"review", version:1, revision, mode, findings:[]}`,
where mode is normal or critical. Findings are
`{id, requirementIds, evidence, action}`. Only two validated empty findings lists
on the same revision approve. Both independent reviewer conversations and the
planner are retained across repairs; no arbitrary revision cap applies.
Malformed, incomplete, unknown-field, stale, blocked or failed outputs cannot
approve. A genuine blocker is `{type:"blocked", version:1, reason, evidence}`.

Agents use `spawn_agent(config, instruction, {keepAlive:true,
allowDelegation:false})`, queued related tasks and `wait_task`. Shutdown requests
are issued for every retained worker, including on failure. Worker/reviewer
instructions forbid staging, commits, push, release and requirement changes.
This is a trusted-agent workflow, not a filesystem sandbox. Injected runtime
objects provide `cwd`, `agents`, `read(path)`, `exists(path)` and `command(command)`;
`exists` is used only to distinguish an absent implicit root rules file.

## Preparation and slice-2 continuation

Git preflight rejects detached/wrong branches, unknown/ambiguous remote push
destinations, unconfirmed release safety, and existing tracked/index edits.
It snapshots HEAD and **all** initial untracked paths, including ignored logs.
Artifacts default to `git rev-parse --git-path noe-workflow`, including linked
worktrees. A custom `artifactsDir` must be outside the worktree or in Git metadata.
Run artifacts include inputs, config, baseline, full prompts/outcomes, plan/review
revisions, JSONL stage records and final status. The command recorder captures
stdout/stderr separately and preserves nonzero exit as mechanical failure data.

Slice 2 should reuse the existing baseline/store/pool within one owning call.
Implement worker repair + actual command execution + independent code re-review,
then scoped local commits; final requirement-level integration/repair correction
commits; finally one normal explicit branch push. The worker result shape already
exists in `validateImplementation`; worker claims cannot waive failed commands.
Extend review identity for code/integration without treating plan approval as code
approval. Stage only literal owned paths, exclude initial untracked files and
artifacts, detect unexpected tracked edits, and never use repository-wide add.
Record commit IDs and committed-but-not-pushed outcomes. No force/tags/release or
history rewriting. CLI/example/finished usage documentation remain slice 3.

## Offline checks

`noe run .noe/lib/workflow/test.js` uses injected agent operations, temporary real
Git repositories and local bare remotes. It makes no network/provider calls.
`biome lint .noe/lib/workflow` checks saved JavaScript. Type checking can use the
host's NOE stdlib/agent declarations; those are development checks, not runtime
dependencies.
